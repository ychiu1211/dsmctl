// Package provision brings a factory-fresh or recovery-mode Synology NAS up to a
// working DSM with a first administrator account. It is deliberately
// self-contained and sessionless: during the DSM setup window the WebAPI is
// unauthenticated, so unlike every other dsmctl module it does not go through
// the authenticated compatibility client. It talks raw HTTP to the setup
// WebAPI, mirroring exactly what the DSM setup wizard (WelcomeApp) does.
//
// The administrator password travels as a plaintext WebAPI parameter. DSM's own
// setup wizard sends it in the clear over HTTPS too (its transport encryption
// only engages on cleartext http), so provision refuses a non-https target.
package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Target is a NAS reachable for provisioning. The caller supplies the HTTP
// client so it owns the TLS policy for the NAS's fresh self-signed certificate
// (pin or, for an explicitly isolated lab box, skip-verify).
type Target struct {
	BaseURL    string
	HTTPClient *http.Client
}

// AdminRequest describes the first administrator to create. Username is
// operator-decided and never generated; Password is dsmctl-generated and never
// logged.
type AdminRequest struct {
	Username      string
	Password      string
	DeviceName    string
	ShareLocation bool // opt-in location telemetry; default false keeps the agent stopped
}

// entryResponse is the shape of a DSM WebAPI reply, including the compound
// (SYNO.Entry.Request) form that reports per-step failure via has_fail and a
// per-step result array carrying each sub-request's own success/error.
type entryResponse struct {
	Success bool     `json:"success"`
	Error   *apiCode `json:"error"`
	Data    struct {
		HasFail *bool        `json:"has_fail"`
		Result  []stepResult `json:"result"`
	} `json:"data"`
}

type stepResult struct {
	Success bool     `json:"success"`
	Error   *apiCode `json:"error"`
}

type apiCode struct {
	Code int `json:"code"`
}

// CreateFirstAdmin creates the operator's administrator during the DSM setup
// window: a sequential, stop-on-error compound that creates the user and adds it
// to the administrators group. It verifies DSM accepted every step (has_fail)
// so a partially applied setup is reported as an error rather than silently
// succeeding. Hardening (disabling the built-in admin, auto-block, telemetry) is
// applied separately by Harden so a model-specific hardening quirk cannot fail
// the essential account creation.
func CreateFirstAdmin(ctx context.Context, target Target, req AdminRequest) error {
	if err := requireHTTPS(target.BaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		return fmt.Errorf("provision requires an administrator username and password")
	}
	compound := []map[string]any{
		{"api": "SYNO.Core.User", "method": "create", "version": 1, "name": req.Username, "password": req.Password},
		{"api": "SYNO.Core.Group.Member", "method": "add", "version": 1, "group": "administrators", "name": []string{req.Username}},
	}
	resp, err := postCompound(ctx, target, compound, true)
	if err != nil {
		return err
	}
	if err := checkEntry(resp); err != nil {
		return fmt.Errorf("create administrator %q: %w", req.Username, err)
	}
	if resp.Data.HasFail != nil && *resp.Data.HasFail {
		return fmt.Errorf("create administrator %q: %s", req.Username, describeFailedStep(resp, []string{"create user", "add to administrators group"}))
	}
	return nil
}

// describeFailedStep finds the first failed sub-request in a compound response
// and names it with its DSM error code, so a caller can tell why setup did not
// complete without exposing any credential value.
func describeFailedStep(resp *entryResponse, stepNames []string) string {
	for i, r := range resp.Data.Result {
		if r.Success {
			continue
		}
		code := 0
		if r.Error != nil {
			code = r.Error.Code
		}
		name := "step"
		if i < len(stepNames) {
			name = stepNames[i]
		}
		return fmt.Sprintf("step %q failed with DSM error code %d", name, code)
	}
	return "a setup step failed without a reported code"
}

// Harden applies the DSM wizard's post-account setup steps that the NEW
// administrator is allowed to perform: harden auto-block, set the device name,
// and stop the location telemetry agent unless opted in. Disabling the built-in
// admin is NOT done here — DSM rejects a User.set on the reserved admin from any
// other administrator with error 105, so it must run in the setup (admin)
// session via DisableBuiltinAdmin. Every step is best-effort; the returned error
// is advisory and never means the administrator was not created.
func Harden(ctx context.Context, target Target, req AdminRequest) error {
	if err := requireHTTPS(target.BaseURL); err != nil {
		return err
	}
	agentAction := "stop"
	if req.ShareLocation {
		agentAction = "start"
	}
	compound := []map[string]any{
		{"api": "SYNO.Core.Security.AutoBlock", "method": "set", "version": 1, "attempts": 10, "enable": true, "enable_expire": false, "within_mins": 5},
		{"api": "SYNO.Core.Service", "method": "control", "version": 1, "service": []map[string]any{{"service_id": "synoagentregisterd", "action": agentAction}}},
	}
	if name := strings.TrimSpace(req.DeviceName); name != "" {
		compound = append([]map[string]any{
			{"api": "SYNO.Core.Network", "method": "set", "version": 1, "server_name": name},
		}, compound...)
	}
	resp, err := postCompound(ctx, target, compound, false)
	if err != nil {
		return err
	}
	if err := checkEntry(resp); err != nil {
		return err
	}
	if resp.Data.HasFail != nil && *resp.Data.HasFail {
		return fmt.Errorf("one or more hardening steps did not apply")
	}
	return nil
}

// DisableBuiltinAdmin scrambles the built-in "admin" account's password and
// expires it, mirroring the trailing steps of the DSM setup wizard's own
// account compound. It MUST run on the setup (admin) session — DSM permits the
// reserved admin to be modified only by itself, rejecting any other
// administrator with error 105 (which is why provisioning does this before
// logging in as the new administrator). Disabling the built-in admin is also
// what flips DSM's server-side admin_configured flag, so the first-run welcome
// wizard stops being presented on login.
//
// The Target client MUST already hold the admin setup session (or the caller
// must run EstablishSetupSession first). Leaving the account expired AND
// scrambled means it is neither loginable nor holds an empty password if revived.
func DisableBuiltinAdmin(ctx context.Context, target Target, scramble string) error {
	if err := requireHTTPS(target.BaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(scramble) == "" {
		return fmt.Errorf("disable built-in admin requires a scramble password")
	}
	compound := []map[string]any{
		{"api": "SYNO.Core.User", "method": "set", "version": 1, "name": "admin", "password": scramble},
		{"api": "SYNO.Core.User", "method": "set", "version": 1, "name": "admin", "expired": "now"},
	}
	resp, err := postCompound(ctx, target, compound, true)
	if err != nil {
		return err
	}
	if err := checkEntry(resp); err != nil {
		return fmt.Errorf("disable built-in admin: %w", err)
	}
	if resp.Data.HasFail != nil && *resp.Data.HasFail {
		return fmt.Errorf("disable built-in admin: %s", describeFailedStep(resp, []string{"scramble admin password", "expire admin"}))
	}
	return nil
}

// Login authenticates as the given account over the target and, because the
// Target client carries a cookie jar, leaves that session cookie in the jar so
// subsequent calls (CompleteSetup, Harden) act as this account. It doubles as
// the provision postcondition: a successful login proves the freshly created
// administrator works.
func Login(ctx context.Context, target Target, username, password string) error {
	if err := requireHTTPS(target.BaseURL); err != nil {
		return err
	}
	form := url.Values{
		"api":     {"SYNO.API.Auth"},
		"version": {"6"},
		"method":  {"login"},
		"account": {username},
		"passwd":  {password},
		"format":  {"cookie"},
	}
	resp, err := postForm(ctx, target, form)
	if err != nil {
		return err
	}
	if err := checkEntry(resp); err != nil {
		return fmt.Errorf("verify administrator %q: %w", username, err)
	}
	return nil
}

// SetupOptions configures the remaining first-run wizard steps CompleteSetup
// applies after the administrator exists.
type SetupOptions struct {
	// AutoUpdate is the DSM update policy: "security" (auto-install important
	// security hotfixes — DSM's own default), "all" (auto-install every update),
	// or "notify" (notify only, never auto-install). Empty means "security".
	AutoUpdate string
	// Analytics enables Synology device analytics (Universal Data Collection and
	// Active Insight). Default false keeps the box from phoning home.
	Analytics bool
}

// CompleteSetup runs the remaining DSM first-run wizard steps as the current
// administrator (the Target client must already hold that session): it sets the
// update policy and analytics preference (best-effort), then marks the setup
// wizard finished so DSM stops presenting it on login. Only the wizard-finish
// step is required; preference steps that a given model or package set does not
// support are tolerated.
func CompleteSetup(ctx context.Context, target Target, opts SetupOptions) error {
	if err := requireHTTPS(target.BaseURL); err != nil {
		return err
	}

	var prefs []map[string]any
	switch opts.AutoUpdate {
	case "", "security":
		prefs = append(prefs,
			map[string]any{"api": "SYNO.Core.Upgrade.Setting", "method": "set", "version": 4, "autoupdate_type": "hotfix-security"},
			map[string]any{"api": "SYNO.Core.Package.Setting", "method": "set", "version": 1, "enable_autoupdate": true, "autoupdateimportant": true})
	case "all":
		prefs = append(prefs,
			map[string]any{"api": "SYNO.Core.Upgrade.Setting", "method": "set", "version": 4, "autoupdate_type": "hotfix"},
			map[string]any{"api": "SYNO.Core.Package.Setting", "method": "set", "version": 1, "enable_autoupdate": true, "autoupdateall": true})
	case "notify":
		prefs = append(prefs,
			map[string]any{"api": "SYNO.Core.Upgrade.Setting", "method": "set", "version": 4, "autoupdate_type": "notify"},
			map[string]any{"api": "SYNO.Core.Package.Setting", "method": "set", "version": 1, "enable_autoupdate": false})
	default:
		return fmt.Errorf("unknown auto-update policy %q (want security, all, or notify)", opts.AutoUpdate)
	}
	prefs = append(prefs,
		map[string]any{"api": "SYNO.Core.DataCollect", "method": "set", "version": 1, "enable": opts.Analytics},
		map[string]any{"api": "SYNO.ActiveInsight.Setting", "method": "set", "version": 1, "monitoring_service": opts.Analytics})
	// Preferences are best-effort: a model without ActiveInsight, for example,
	// must not block finishing the wizard, so do not stop on error and tolerate
	// has_fail here.
	if _, err := postCompound(ctx, target, prefs, false); err != nil {
		return err
	}

	// The one required step: mark the welcome wizard finished so DSM no longer
	// presents it after login.
	finish := []map[string]any{
		{"api": "SYNO.Core.QuickStart.Info", "method": "hide_welcome", "version": 1, "welcome_upgrade_step": false},
	}
	resp, err := postCompound(ctx, target, finish, true)
	if err != nil {
		return err
	}
	if err := checkEntry(resp); err != nil {
		return fmt.Errorf("mark setup wizard finished: %w", err)
	}
	if resp.Data.HasFail != nil && *resp.Data.HasFail {
		return fmt.Errorf("mark setup wizard finished: DSM rejected hide_welcome")
	}
	return nil
}

// EstablishSetupSession logs in as the built-in administrator to obtain the DSM
// setup session cookie that authorizes account creation.
//
// On a fresh DSM inside its first-run setup window the built-in "admin" account
// is enabled with an empty password, and the whole setup runs as an
// authenticated "admin" session (authType local, no SynoToken required). The
// account-creation compound is a mutation and DSM rejects it with error 119
// (invalid session) unless it carries that session cookie. This logs in as
// admin with an empty password so the Target client's cookie jar captures the
// session; the compound then reuses it, exactly as the browser wizard does. The
// wizard — and Harden — scramble and disable this built-in admin at the end.
//
// The Target client MUST carry a cookie jar.
func EstablishSetupSession(ctx context.Context, target Target) error {
	if err := requireHTTPS(target.BaseURL); err != nil {
		return err
	}
	form := url.Values{
		"api":     {"SYNO.API.Auth"},
		"version": {"6"},
		"method":  {"login"},
		"account": {"admin"},
		"passwd":  {""},
		"format":  {"cookie"},
	}
	resp, err := postForm(ctx, target, form)
	if err != nil {
		return err
	}
	if err := checkEntry(resp); err != nil {
		return fmt.Errorf("log in as the built-in admin to start the setup session: %w", err)
	}
	return nil
}

func postCompound(ctx context.Context, target Target, compound []map[string]any, stopOnError bool) (*entryResponse, error) {
	encoded, err := json.Marshal(compound)
	if err != nil {
		return nil, fmt.Errorf("encode setup compound: %w", err)
	}
	form := url.Values{
		"api":             {"SYNO.Entry.Request"},
		"version":         {"1"},
		"method":          {"request"},
		"mode":            {"sequential"},
		"stop_when_error": {boolParam(stopOnError)},
		"compound":        {string(encoded)},
	}
	return postForm(ctx, target, form)
}

func postForm(ctx context.Context, target Target, form url.Values) (*entryResponse, error) {
	client := target.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := strings.TrimRight(target.BaseURL, "/") + "/webapi/entry.cgi"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("contact NAS at %s: %w", target.BaseURL, err)
	}
	defer httpResp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read DSM response: %w", err)
	}
	var parsed entryResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode DSM response (%d bytes): %w", len(body), err)
	}
	return &parsed, nil
}

func checkEntry(resp *entryResponse) error {
	if resp.Error != nil {
		return fmt.Errorf("DSM error code %d", resp.Error.Code)
	}
	if !resp.Success {
		return fmt.Errorf("DSM did not accept the request")
	}
	return nil
}

func requireHTTPS(baseURL string) error {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(baseURL)), "https://") {
		return fmt.Errorf("provision requires an https target so the administrator password is not sent in cleartext, got %q", baseURL)
	}
	return nil
}

func boolParam(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
