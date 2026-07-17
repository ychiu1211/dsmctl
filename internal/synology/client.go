package synology

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	authAPI        = "SYNO.API.Auth"
	maxBodySize    = 8 << 20
	maxOTPAttempts = 3
	dsmctlSession  = "DSMCTL"
)

type OTPProvider func(ctx context.Context) (string, error)

type DeviceIDSaver func(ctx context.Context, deviceID string) error

type Options struct {
	BaseURL      string
	Username     string
	Password     string
	DeviceName   string
	DeviceID     string
	OTPProvider  OTPProvider
	SaveDeviceID DeviceIDSaver
	HTTPClient   *http.Client

	// SessionID and SynoToken seed the client with a session obtained elsewhere
	// (for example a web login). When SessionID is set the client reuses it
	// instead of logging in, so a password is not required. If that session
	// later expires the client can only re-authenticate when a password is also
	// configured.
	SessionID string
	SynoToken string
}

type APIInfo = compatibility.APIInfo

type Client struct {
	baseURL      *url.URL
	username     string
	password     string
	deviceName   string
	deviceID     string
	otp          OTPProvider
	saveDeviceID DeviceIDSaver
	httpClient   *http.Client

	mu         sync.Mutex
	target     compatibility.Target
	apiChecked map[string]bool
	sid        string
	synoToken  string
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error,omitempty"`
}

func NewClient(options Options) (*Client, error) {
	baseURL, err := url.Parse(options.BaseURL)
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, errors.New("base URL must be an absolute http or https URL")
	}
	if strings.TrimSpace(options.Username) == "" {
		return nil, errors.New("username is required")
	}
	if options.Password == "" && options.SessionID == "" {
		return nil, errors.New("a password or an existing session is required")
	}
	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:      baseURL,
		username:     options.Username,
		password:     options.Password,
		deviceName:   options.DeviceName,
		deviceID:     options.DeviceID,
		otp:          options.OTPProvider,
		saveDeviceID: options.SaveDeviceID,
		httpClient:   httpClient,
		target:       compatibility.NewTarget(),
		apiChecked:   make(map[string]bool),
		sid:          options.SessionID,
		synoToken:    options.SynoToken,
	}, nil
}

func (c *Client) discoverAPIsLocked(ctx context.Context, names ...string) error {
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if !c.apiChecked[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	params := url.Values{
		"api":     {"SYNO.API.Info"},
		"version": {"1"},
		"method":  {"query"},
		"query":   {strings.Join(missing, ",")},
	}
	data, err := c.requestLocked(ctx, "entry.cgi", params, "SYNO.API.Info", "query")
	if err != nil {
		return fmt.Errorf("discover Synology APIs: %w", err)
	}
	var discovered map[string]APIInfo
	if err := json.Unmarshal(data, &discovered); err != nil {
		return fmt.Errorf("decode Synology API discovery: %w", err)
	}
	for _, name := range missing {
		c.apiChecked[name] = true
		info, ok := discovered[name]
		if !ok || info.Path == "" || info.MaxVersion == 0 {
			continue
		}
		c.target.SetAPI(name, info)
	}
	c.updateDerivedCapabilitiesLocked()
	return nil
}

func (c *Client) ensureAPIsLocked(ctx context.Context, names ...string) error {
	if err := c.discoverAPIsLocked(ctx, names...); err != nil {
		return err
	}
	for _, name := range names {
		if _, ok := c.target.API(name); !ok {
			return fmt.Errorf("Synology API %s is not available on this NAS", name)
		}
	}
	return nil
}

func (c *Client) loginLocked(ctx context.Context) error {
	if c.sid != "" {
		return nil
	}
	if c.password == "" {
		return errors.New("DSM session is unavailable and no password is configured to re-authenticate; run 'dsmctl auth login' to sign in again")
	}
	if err := c.ensureAPIsLocked(ctx, authAPI); err != nil {
		return err
	}
	info, _ := c.target.API(authAPI)
	// DSM 7.3 grants privileged control-plane mutations to Auth v7 sessions.
	// preferredVersion keeps older DSM releases on their highest advertised
	// version instead of duplicating the login implementation per release.
	version := preferredVersion(info, 7)
	params := url.Values{
		"api":     {authAPI},
		"version": {strconv.Itoa(version)},
		"method":  {"login"},
		"account": {c.username},
		"passwd":  {c.password},
		"session": {dsmctlSession},
		"format":  {"sid"},
	}
	if version >= 6 {
		params.Set("enable_syno_token", "yes")
		if c.deviceID != "" && c.deviceName != "" {
			params.Set("device_name", c.deviceName)
			params.Set("device_id", c.deviceID)
		}
	}

	data, err := c.requestLocked(ctx, info.Path, params, authAPI, "login")
	if isOTPChallenge(err) {
		data, err = c.loginWithOTPLocked(ctx, info.Path, version, params, err)
	}
	if err != nil {
		return fmt.Errorf("log in to DSM: %w", err)
	}
	return c.acceptLoginLocked(ctx, data)
}

func (c *Client) loginWithOTPLocked(ctx context.Context, path string, version int, base url.Values, challenge error) (json.RawMessage, error) {
	if c.otp == nil {
		return nil, &OTPRequiredError{Cause: challenge}
	}
	var lastErr error
	for attempt := 0; attempt < maxOTPAttempts; attempt++ {
		code, err := c.otp(ctx)
		if err != nil {
			return nil, fmt.Errorf("obtain one-time password: %w", err)
		}
		code = strings.TrimSpace(code)
		if code == "" {
			return nil, errors.New("one-time password cannot be empty")
		}
		params := cloneValues(base)
		params.Del("device_id")
		params.Set("otp_code", code)
		if version >= 6 && c.deviceName != "" {
			params.Set("enable_device_token", "yes")
			params.Set("device_name", c.deviceName)
		}
		data, requestErr := c.requestLocked(ctx, path, params, authAPI, "login")
		if requestErr == nil {
			return data, nil
		}
		lastErr = requestErr
		if !isInvalidOTP(requestErr) {
			return nil, requestErr
		}
	}
	return nil, fmt.Errorf("DSM rejected the one-time password after %d attempts: %w", maxOTPAttempts, lastErr)
}

func (c *Client) acceptLoginLocked(ctx context.Context, data json.RawMessage) error {
	var result struct {
		SID        string `json:"sid"`
		SynoToken  string `json:"synotoken"`
		DID        string `json:"did"`
		DeviceIDV7 string `json:"device_id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("decode DSM login: %w", err)
	}
	if result.SID == "" {
		return errors.New("DSM login response did not contain a session ID")
	}
	c.sid = result.SID
	c.synoToken = result.SynoToken
	deviceID := result.DID
	if deviceID == "" {
		deviceID = result.DeviceIDV7
	}
	if deviceID != "" {
		c.deviceID = deviceID
		if c.saveDeviceID != nil {
			if err := c.saveDeviceID(ctx, deviceID); err != nil {
				return fmt.Errorf("save DSM trusted device: %w", err)
			}
		}
	}
	return nil
}

// Authenticate establishes a DSM session without calling a management API.
func (c *Client) Authenticate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked(ctx)
}

// ValidateSession reports whether the session currently held by the client is
// still accepted by DSM. It issues one cheap authenticated request and, unlike
// the normal request path, never tries to re-authenticate: an expired or
// rejected session is reported as (false, nil), a missing session as
// (false, nil), and only transport or unexpected API failures return an error.
// It is the authoritative, online counterpart to HasSession.
func (c *Client) ValidateSession(ctx context.Context) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sid == "" {
		return false, nil
	}
	if err := c.probeSessionLocked(ctx); err != nil {
		if isSessionError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// probeSessionLocked issues a single authenticated read that any DSM release
// exposes, without the session-retry that executeLocked performs, so callers
// can observe a session failure instead of silently re-authenticating.
func (c *Client) probeSessionLocked(ctx context.Context) error {
	const probeAPI = "SYNO.Core.System"
	if err := c.ensureAPIsLocked(ctx, probeAPI); err != nil {
		return err
	}
	info, _ := c.target.API(probeAPI)
	params := url.Values{
		"api":     {probeAPI},
		"version": {strconv.Itoa(info.MaxVersion)},
		"method":  {"info"},
		"_sid":    {c.sid},
	}
	if c.synoToken != "" {
		params.Set("SynoToken", c.synoToken)
	}
	_, err := c.requestLocked(ctx, info.Path, params, probeAPI, "info")
	return err
}

func (c *Client) executeLocked(ctx context.Context, call compatibility.Request) (json.RawMessage, error) {
	if err := c.ensureAPIsLocked(ctx, call.API); err != nil {
		return nil, err
	}
	if err := c.loginLocked(ctx); err != nil {
		return nil, err
	}
	info, _ := c.target.API(call.API)
	version := call.Version
	if version == 0 {
		version = info.MaxVersion
	}
	if !info.Supports(version) {
		return nil, fmt.Errorf("Synology API %s does not support requested version %d (available %d-%d)", call.API, version, info.MinVersion, info.MaxVersion)
	}
	params := cloneValues(call.Parameters)
	var err error
	if call.JSONParameters != nil {
		params, err = c.encodeJSONParametersLocked(ctx, call.JSONParameters, call.EncryptedParameters)
		if err != nil {
			return nil, fmt.Errorf("prepare JSON parameters for %s.%s: %w", call.API, call.Method, err)
		}
	} else if len(call.EncryptedParameters) != 0 {
		return nil, fmt.Errorf("encrypted parameters require typed JSON parameters")
	}
	params.Set("api", call.API)
	params.Set("version", strconv.Itoa(version))
	params.Set("method", call.Method)
	params.Set("_sid", c.sid)
	if c.synoToken != "" {
		params.Set("SynoToken", c.synoToken)
	}

	data, err := c.requestLocked(ctx, info.Path, params, call.API, call.Method)
	if isSessionError(err) {
		c.sid = ""
		c.synoToken = ""
		if loginErr := c.loginLocked(ctx); loginErr != nil {
			return nil, loginErr
		}
		params.Set("_sid", c.sid)
		params.Del("SynoToken")
		if c.synoToken != "" {
			params.Set("SynoToken", c.synoToken)
		}
		return c.requestLocked(ctx, info.Path, params, call.API, call.Method)
	}
	return data, err
}

func (c *Client) executeScriptLocked(ctx context.Context, call compatibility.Request) ([]byte, error) {
	if err := c.ensureAPIsLocked(ctx, call.API); err != nil {
		return nil, err
	}
	if err := c.loginLocked(ctx); err != nil {
		return nil, err
	}
	info, _ := c.target.API(call.API)
	version := call.Version
	if version == 0 {
		version = info.MaxVersion
	}
	if !info.Supports(version) {
		return nil, fmt.Errorf("Synology API %s does not support requested version %d (available %d-%d)", call.API, version, info.MinVersion, info.MaxVersion)
	}
	params := cloneValues(call.Parameters)
	params.Set("api", call.API)
	params.Set("version", strconv.Itoa(version))
	params.Set("method", call.Method)
	return c.requestScriptLocked(ctx, info.Path, params, call.API)
}

func (c *Client) requestScriptLocked(ctx context.Context, apiPath string, params url.Values, api string) ([]byte, error) {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/webapi/" + strings.TrimLeft(apiPath, "/")
	endpoint.RawQuery = params.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/javascript, text/javascript, */*;q=0.1")
	request.Header.Set("User-Agent", "dsmctl/0.1")
	if c.sid != "" {
		request.AddCookie(&http.Cookie{Name: "id", Value: c.sid})
	}
	if c.synoToken != "" {
		request.Header.Set("X-SYNO-TOKEN", c.synoToken)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", endpoint.Redacted(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("request %s returned HTTP %s", endpoint.Redacted(), response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s script response: %w", api, err)
	}
	if len(body) > maxBodySize {
		return nil, fmt.Errorf("%s script response exceeds %d bytes", api, maxBodySize)
	}
	return body, nil
}

func (c *Client) requestLocked(ctx context.Context, apiPath string, params url.Values, api, method string) (json.RawMessage, error) {
	endpoint := *c.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/webapi/" + strings.TrimLeft(apiPath, "/")

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewBufferString(params.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "dsmctl/0.1")
	// Some DSM Core APIs only recognize session credentials from the same
	// locations used by the DSM web UI. Keep request parameters for documented
	// WebAPI compatibility and also send the equivalent secure cookie/header.
	if c.sid != "" {
		request.AddCookie(&http.Cookie{Name: "id", Value: c.sid})
	}
	if c.synoToken != "" {
		request.Header.Set("X-SYNO-TOKEN", c.synoToken)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", endpoint.Redacted(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("request %s returned HTTP %s", endpoint.Redacted(), response.Status)
	}

	decoder := json.NewDecoder(io.LimitReader(response.Body, maxBodySize))
	var result envelope
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", api, err)
	}
	if !result.Success {
		code := 0
		if result.Error != nil {
			code = result.Error.Code
		}
		return nil, &APIError{API: api, Method: method, Code: code}
	}
	return result.Data, nil
}

// HasSession reports whether this client currently holds a DSM session ID
// from an earlier login. It never contacts the NAS, so the session may have
// expired server-side.
func (c *Client) HasSession() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sid != ""
}

func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sid == "" {
		return nil
	}
	if err := c.ensureAPIsLocked(ctx, authAPI); err != nil {
		return err
	}
	info, _ := c.target.API(authAPI)
	params := url.Values{
		"api":     {authAPI},
		"version": {strconv.Itoa(info.MaxVersion)},
		"method":  {"logout"},
		"session": {dsmctlSession},
		"_sid":    {c.sid},
	}
	_, err := c.requestLocked(ctx, info.Path, params, authAPI, "logout")
	c.sid = ""
	c.synoToken = ""
	if err != nil {
		return fmt.Errorf("log out from DSM: %w", err)
	}
	return nil
}

func cloneValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, items := range values {
		clone[key] = append([]string(nil), items...)
	}
	return clone
}

func preferredVersion(info APIInfo, preferred int) int {
	if preferred > info.MaxVersion {
		return info.MaxVersion
	}
	if preferred < info.MinVersion {
		return info.MinVersion
	}
	return preferred
}
