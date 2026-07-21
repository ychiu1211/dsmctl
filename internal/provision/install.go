package provision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PatDownloadBase is Synology's public DSM release download host. The full path
// is <base>/release/<version>/<build>/DSM_<model>_<build>.pat.
const PatDownloadBase = "https://global.synologydownload.com/download/DSM"

// This file implements the DSM Web Assistant online-install protocol used to
// bring up never-installed or crashed hardware (WI-085). Unlike first-admin
// creation these calls carry no password, so they are allowed over plain http
// (the assistant's port 5000) as well as https. All endpoints are /webman/*.cgi,
// reverse-engineered from the assistant's main.js and live-verified.

// DeviceState is the non-secret device/install state reported by get_state.cgi.
// It tells whether DSM is installed and, if not, which install kinds are
// available online.
type DeviceState struct {
	HasDisk        bool   `json:"has_disk"`
	Status         string `json:"status"`
	Model          string `json:"model"`
	Serial         string `json:"serial"`
	MAC            string `json:"mac_addr"`
	IP             string `json:"ip_addr"`
	Hostname       string `json:"hostname"`
	BuildVersion   string `json:"build_ver"`
	DiskCount      int    `json:"disk_count"`
	DiskSizeEnough bool   `json:"disk_size_enough"`
	IsInstalling   bool   `json:"is_installing"`
	HTTPSAdminPort string `json:"https_admin_port"`

	InternetInstallOK   bool `json:"internet_install_ok"`
	InternetReinstallOK bool `json:"internet_reinstall_ok"`
	InternetMigrateOK   bool `json:"internet_migrate_ok"`

	InternetInstallVersion   string `json:"internet_install_version"`
	InternetReinstallVersion string `json:"internet_reinstall_version"`
	InternetMigrateVersion   string `json:"internet_migrate_version"`
}

// Installed reports whether DSM is already installed and running. The assistant
// only serves get_state.cgi while DSM is NOT running, so any recognized
// pre-install status means an install/repair is needed.
func (s DeviceState) Installed() bool {
	switch s.Status {
	case "not_install", "sys_crash", "sys_migrat":
		return false
	default:
		return s.Status == "" // an empty status means the assistant did not report a pre-install state
	}
}

// NeedsInstall reports the pre-install condition, or "" when none applies.
func (s DeviceState) NeedsInstall() string {
	switch s.Status {
	case "not_install", "sys_crash", "sys_migrat":
		return s.Status
	default:
		return ""
	}
}

// OnlineInstall describes how to online-install for the current state: the
// install.cgi status parameter, whether the device reports it can reach
// Synology for that kind of install, and the DSM version offered. A zero value
// (empty Status) means the state does not support a plain online install.
type OnlineInstall struct {
	Status    string // install.cgi status parameter
	Available bool   // the device can reach Synology for this install kind
	Version   string // DSM version offered online
	Kind      string // human label: install / reinstall / migrate
}

// OnlineInstallPlan maps the detected state to the online install.cgi request it
// requires, mirroring the assistant's cgiParams/isSupportAutoDownload logic.
func (s DeviceState) OnlineInstallPlan() OnlineInstall {
	switch s.Status {
	case "not_install":
		return OnlineInstall{Status: "not_install", Available: s.InternetInstallOK, Version: s.InternetInstallVersion, Kind: "install"}
	case "sys_crash":
		return OnlineInstall{Status: "sys_crash", Available: s.InternetReinstallOK, Version: s.InternetReinstallVersion, Kind: "reinstall"}
	case "sys_migrat":
		return OnlineInstall{Status: "sys_migrat", Available: s.InternetMigrateOK, Version: s.InternetMigrateVersion, Kind: "migrate"}
	default:
		return OnlineInstall{}
	}
}

type getStateResponse struct {
	Success bool `json:"success"`
	Data    struct {
		HasDisk bool            `json:"has_disk"`
		DSInfo  json.RawMessage `json:"dsinfo"`
	} `json:"data"`
}

// GetState reads the assistant's device/install state. It is read-only and
// changes nothing on the device.
func GetState(ctx context.Context, target Target) (DeviceState, error) {
	body, err := webmanGet(ctx, target, "get_state", nil)
	if err != nil {
		return DeviceState{}, err
	}
	var parsed getStateResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		// get_state.cgi serves JSON only while DSM is not installed. Once DSM is
		// installed and running it takes over the port and returns HTML, so a
		// non-JSON body almost always means the device is already set up.
		return DeviceState{}, fmt.Errorf("the Web Assistant did not return install state; DSM is likely already installed on this device")
	}
	if !parsed.Success {
		return DeviceState{}, fmt.Errorf("device did not report install state (DSM may already be installed)")
	}
	var state DeviceState
	if len(parsed.Data.DSInfo) > 0 {
		if err := json.Unmarshal(parsed.Data.DSInfo, &state); err != nil {
			return DeviceState{}, fmt.Errorf("decode dsinfo: %w", err)
		}
	}
	state.HasDisk = parsed.Data.HasDisk
	return state, nil
}

// InstallProgress is the install.cgi progress reported during an online install.
// The device reports {"data":{"stage":"download","progress":"37"}} — stage moves
// through download → install (and the like) and progress is a 0-100 string.
type InstallProgress struct {
	Stage   string
	Percent int
	Raw     string
}

type installProgressResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Stage    string `json:"stage"`
		Progress string `json:"progress"`
	} `json:"data"`
}

// GetInstallProgress reads the current online-install progress. An empty body
// means no install is running (either it has not started or the device has begun
// rebooting).
func GetInstallProgress(ctx context.Context, target Target) (InstallProgress, error) {
	body, err := webmanGet(ctx, target, "get_install_progress", nil)
	if err != nil {
		return InstallProgress{}, err
	}
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return InstallProgress{Raw: ""}, nil
	}
	var parsed installProgressResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return InstallProgress{Raw: raw}, nil
	}
	percent, _ := strconv.Atoi(strings.TrimSpace(parsed.Data.Progress))
	return InstallProgress{Stage: parsed.Data.Stage, Percent: percent, Raw: raw}, nil
}

// InstallOnline triggers an online DSM install: the device downloads the DSM
// image from Synology and installs it, then reboots. status is the value from
// DeviceState.OnlineInstallPlan().Status. This is destructive: it writes the OS
// to the device's disks. It carries no password.
func InstallOnline(ctx context.Context, target Target, status string) error {
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("online install requires a device status")
	}
	now := time.Now().Unix()
	query := url.Values{
		"upload":          {"false"},
		"status":          {status},
		"localinstallreq": {"false"},
		"utctime":         {strconv.FormatInt(now, 10)},
		"_dc":             {strconv.FormatInt(now*1000, 10)},
	}
	body, err := webmanPostForm(ctx, target, "install", query, url.Values{"upload": {"false"}})
	if err != nil {
		return err
	}
	return checkWebmanSuccess(body, "trigger online install")
}

// Pingpong is the assistant liveness probe used to detect when the device has
// finished rebooting after an install.
func Pingpong(ctx context.Context, target Target) error {
	_, err := webmanGet(ctx, target, "pingpong", url.Values{"action": {"cors"}})
	return err
}

// DefaultPatURL builds the Synology download URL for the DSM image that matches
// the device's own flash build, so an offline install uses exactly the version
// the bootloader expects. It derives version/build from build_ver ("7.3.2-86009"
// → 7.3.2 / 86009) and the model, e.g.
// https://.../release/7.3.2/86009/DSM_DS918%2B_86009.pat.
func DefaultPatURL(state DeviceState) (string, string, error) {
	model := strings.TrimSpace(state.Model)
	buildVer := strings.TrimSpace(state.BuildVersion)
	if model == "" || buildVer == "" {
		return "", "", fmt.Errorf("device did not report a model and build to locate a DSM image")
	}
	parts := strings.SplitN(buildVer, "-", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse DSM build %q (want <version>-<build>)", buildVer)
	}
	version, build := parts[0], parts[1]
	filename := "DSM_" + model + "_" + build + ".pat"
	pathModel := strings.ReplaceAll(model, "+", "%2B")
	return PatDownloadBase + "/release/" + version + "/" + build + "/DSM_" + pathModel + "_" + build + ".pat", filename, nil
}

// InstallLocal uploads a .pat image and installs it (the offline path used when
// the device itself cannot reach Synology; the host downloads the image and
// streams it here). The image is sent as a single multipart field "filename",
// mirroring the assistant. size is the image length: the multipart body is sent
// with an exact Content-Length, because DSM's install.cgi rejects a chunked
// upload with error_upload (live-verified). Destructive: it writes the OS to the
// device's disks. It carries no password.
func InstallLocal(ctx context.Context, target Target, status, filename string, pat io.Reader, size int64) error {
	if strings.TrimSpace(status) == "" {
		return fmt.Errorf("install requires a device status")
	}
	if size <= 0 {
		return fmt.Errorf("install requires the image size for a Content-Length upload")
	}
	now := time.Now().Unix()
	query := url.Values{
		"upload":          {"true"},
		"status":          {status},
		"localinstallreq": {"false"},
		"utctime":         {strconv.FormatInt(now, 10)},
		"_dc":             {strconv.FormatInt(now*1000, 10)},
	}
	// Build the multipart framing up front so its exact byte length is known,
	// then stream [part header][image][closing boundary] with a fixed
	// Content-Length instead of chunked transfer-encoding.
	var header bytes.Buffer
	writer := multipart.NewWriter(&header)
	if _, err := writer.CreateFormFile("filename", filename); err != nil {
		return err
	}
	closing := "\r\n--" + writer.Boundary() + "--\r\n"
	total := int64(header.Len()) + size + int64(len(closing))
	body := io.MultiReader(bytes.NewReader(header.Bytes()), pat, strings.NewReader(closing))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webmanURL(target, "install", query), body)
	if err != nil {
		return err
	}
	req.ContentLength = total
	req.Header.Set("Content-Type", writer.FormDataContentType())
	respBody, err := webmanDo(target, req, "install")
	if err != nil {
		return err
	}
	return checkWebmanSuccess(respBody, "upload and install DSM image")
}

func webmanURL(target Target, name string, query url.Values) string {
	endpoint := strings.TrimRight(target.BaseURL, "/") + "/webman/" + name + ".cgi"
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	return endpoint
}

func webmanGet(ctx context.Context, target Target, name string, query url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webmanURL(target, name, query), nil)
	if err != nil {
		return nil, err
	}
	return webmanDo(target, req, name)
}

func webmanPostForm(ctx context.Context, target Target, name string, query, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webmanURL(target, name, query), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return webmanDo(target, req, name)
}

func webmanDo(target Target, req *http.Request, name string) ([]byte, error) {
	client := target.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("contact web assistant %s.cgi at %s: %w", name, target.BaseURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read %s.cgi response: %w", name, err)
	}
	return body, nil
}

func checkWebmanSuccess(body []byte, action string) error {
	var parsed struct {
		Success bool `json:"success"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("%s: unexpected response %q", action, strings.TrimSpace(string(body)))
	}
	if !parsed.Success {
		code := 0
		if parsed.Error != nil {
			code = parsed.Error.Code
		}
		return fmt.Errorf("%s: device reported failure (code %d)", action, code)
	}
	return nil
}
