package application

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ychiu1211/dsmctl/internal/provision"
)

// InstallRequest asks to detect a discovered device's install state and,
// optionally, trigger an online DSM install on it. It targets an un-enrolled LAN
// device by url (like provision_discovered_nas), so it is scoped by nas.provision
// and bounded to LAN/VPN addresses.
type InstallRequest struct {
	URL     string `json:"url" jsonschema:"Web Assistant url of a discovered device, e.g. http://10.0.0.9:5000"`
	Trigger bool   `json:"trigger,omitempty" jsonschema:"Actually start the online install (DESTRUCTIVE — erases the disks). Default false only detects state."`
}

// InstallStatus reports a device's install state and, when triggered, that an
// online install was started. It carries no secret and never blocks on the
// multi-minute install/reboot: a triggered install runs on the device and the
// caller re-detects until DSM is up, then provisions.
type InstallStatus struct {
	URL             string `json:"url" jsonschema:"Device url"`
	Model           string `json:"model,omitempty" jsonschema:"DSM model"`
	Serial          string `json:"serial,omitempty" jsonschema:"Device serial"`
	State           string `json:"state" jsonschema:"Reported state: not_install, sys_crash, sys_migrat, or empty when DSM is installed"`
	DSMInstalled    bool   `json:"dsm_installed" jsonschema:"DSM is already installed and running"`
	InstallKind     string `json:"install_kind,omitempty" jsonschema:"install, reinstall, or migrate"`
	OnlineAvailable bool   `json:"online_available" jsonschema:"The device can reach Synology to install online"`
	OnlineVersion   string `json:"online_version,omitempty" jsonschema:"DSM version offered online"`
	InstallStarted  bool   `json:"install_started" jsonschema:"An online install was triggered by this call"`
	Note            string `json:"note,omitempty" jsonschema:"Human-readable next step"`
}

// InstallDiscoveredNAS detects the install state of a discovered LAN device and,
// with Trigger, starts an online DSM install (fire-and-forget: it does not wait
// for the device to download DSM and reboot, which exceeds an MCP call). The
// offline .pat path (host downloads the image) is intentionally CLI-only. No
// password is involved, so http is allowed; the target is bounded to LAN/VPN
// addresses.
func (s *Service) InstallDiscoveredNAS(ctx context.Context, req InstallRequest) (InstallStatus, error) {
	targetURL := strings.TrimSpace(req.URL)
	if targetURL == "" {
		return InstallStatus{}, errors.New("install requires the device url")
	}
	if _, err := validateLANURL(ctx, targetURL, false); err != nil {
		return InstallStatus{}, err
	}
	target := provision.Target{BaseURL: targetURL, HTTPClient: installProbeClient()}
	state, err := provision.GetState(ctx, target)
	if err != nil {
		return InstallStatus{}, err
	}
	status := InstallStatus{URL: targetURL, Model: state.Model, Serial: state.Serial, State: state.Status}
	if state.Installed() {
		status.DSMInstalled = true
		status.Note = "DSM is already installed; create the first administrator with provision_discovered_nas."
		return status, nil
	}
	plan := state.OnlineInstallPlan()
	if plan.Status == "" {
		return status, fmt.Errorf("state %q does not support a plain install over MCP", state.Status)
	}
	status.InstallKind = plan.Kind
	status.OnlineAvailable = plan.Available
	status.OnlineVersion = plan.Version
	if !req.Trigger {
		status.Note = "Detected. Pass trigger=true to start the online install (DESTRUCTIVE)."
		return status, nil
	}
	if !plan.Available {
		return status, fmt.Errorf("online %s is not available on this device (no internet route); the offline .pat path is CLI-only", plan.Kind)
	}
	if err := provision.InstallOnline(ctx, target, plan.Status); err != nil {
		return status, err
	}
	status.InstallStarted = true
	status.Note = "Online install started; the device downloads DSM and reboots (minutes). Re-call to check state, then provision once DSM is up."
	return status, nil
}

// installProbeClient talks to the fresh device's Web Assistant. It skips TLS
// verification (a factory-fresh device is self-signed and the install channel
// carries no password) and has no overall timeout beyond the request context.
func installProbeClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // fresh-device install channel carries no secret
	transport.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: transport}
}
