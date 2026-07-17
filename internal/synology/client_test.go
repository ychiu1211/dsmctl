package synology

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

func TestRequestScriptUsesCookieAndHeaderWithoutCredentialQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/webapi/entry.cgi" {
			t.Errorf("request = %s %s", request.Method, request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("api") != "SYNO.Core.Desktop.Defs" || query.Get("method") != "getjs" {
			t.Errorf("query = %#v", query)
		}
		if query.Get("_sid") != "" || query.Get("SynoToken") != "" {
			t.Errorf("session credentials leaked into query: %#v", query)
		}
		cookie, err := request.Cookie("id")
		if err != nil || cookie.Value != "session-secret" || request.Header.Get("X-SYNO-TOKEN") != "token-secret" {
			t.Errorf("cookie=%#v token=%q err=%v", cookie, request.Header.Get("X-SYNO-TOKEN"), err)
		}
		fmt.Fprint(w, `var _SYNOINFODEF={"supportext4":"yes"};`)
	}))
	defer server.Close()

	client, err := NewClient(Options{BaseURL: server.URL, Username: "test", Password: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	client.sid = "session-secret"
	client.synoToken = "token-secret"
	body, err := client.requestScriptLocked(context.Background(), "entry.cgi", url.Values{
		"api": {"SYNO.Core.Desktop.Defs"}, "version": {"1"}, "method": {"getjs"},
	}, "SYNO.Core.Desktop.Defs")
	if err != nil || len(body) == 0 {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestClientSystemInfoLoginAndLogout(t *testing.T) {
	var loginCount, logoutCount, infoCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		api := r.Form.Get("api")
		method := r.Form.Get("method")
		w.Header().Set("Content-Type", "application/json")
		switch api + "." + method {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`)
		case "SYNO.API.Auth.login":
			loginCount++
			if got := r.Form.Get("version"); got != "7" {
				t.Errorf("auth version = %q, want 7", got)
			}
			if got := r.Form.Get("session"); got != dsmctlSession {
				t.Errorf("auth session = %q, want %q", got, dsmctlSession)
			}
			if got := r.Form.Get("passwd"); got != "secret" {
				t.Errorf("passwd = %q", got)
			}
			if r.URL.Query().Get("passwd") != "" {
				t.Error("password was placed in URL query")
			}
			fmt.Fprint(w, `{"success":true,"data":{"sid":"test-sid","synotoken":"test-token"}}`)
		case "SYNO.Core.System.info":
			infoCount++
			if r.Form.Get("_sid") != "test-sid" || r.Form.Get("SynoToken") != "test-token" {
				t.Errorf("missing session credentials: %#v", r.Form)
			}
			cookie, err := r.Cookie("id")
			if err != nil || cookie.Value != "test-sid" || r.Header.Get("X-SYNO-TOKEN") != "test-token" {
				t.Errorf("missing session cookie/header: cookie=%#v err=%v token=%q", cookie, err, r.Header.Get("X-SYNO-TOKEN"))
			}
			fmt.Fprint(w, `{"success":true,"data":{"hostname":"office-nas","model":"DS923+","serial":"SERIAL","firmware_ver":"DSM 7.2","cpu_vendor":"AMD","cpu_series":"R1600","cpu_cores":"2","ram_size":4096,"up_time":"3 days","time_zone":"Asia/Taipei","sys_temp":41,"sys_tempwarn":false}}`)
		case "SYNO.API.Auth.logout":
			logoutCount++
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected request %s.%s", api, method)
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "automation",
		Password:   "secret",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	info, err := client.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info.Model != "DS923+" || info.Hostname != "office-nas" || info.MemoryMiB != 4096 {
		t.Fatalf("SystemInfo() = %#v", info)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if loginCount != 1 || infoCount != 1 || logoutCount != 1 {
		t.Fatalf("request counts login=%d info=%d logout=%d", loginCount, infoCount, logoutCount)
	}
}

func TestClientClosePreservesSeededSessionWithoutContactingDSM(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:                server.URL,
		SessionID:              "stored-sid",
		SynoToken:              "stored-token",
		HTTPClient:             server.Client(),
		PreserveSessionOnClose: true,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("Close() contacted DSM %d times; a preserved session must not be logged out", requests)
	}
	if client.HasSession() {
		t.Fatal("Close() must drop the in-memory session even when preserving it server-side")
	}
}

func TestClientLogoutRevokesEvenWhenPreservingOnClose(t *testing.T) {
	logoutCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
		case "SYNO.API.Auth.logout":
			logoutCount++
			if r.Form.Get("_sid") != "stored-sid" {
				t.Errorf("logout SID = %q, want stored-sid", r.Form.Get("_sid"))
			}
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected request %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:                server.URL,
		SessionID:              "stored-sid",
		HTTPClient:             server.Client(),
		PreserveSessionOnClose: true,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.Logout(context.Background()); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if logoutCount != 1 {
		t.Fatalf("logout called %d times, want 1; Logout is the explicit revocation verb", logoutCount)
	}
	if client.HasSession() {
		t.Fatal("Logout() must drop the in-memory session")
	}
}

func TestClientRefreshesSeededSessionAfterExpiry(t *testing.T) {
	staleAttempts, freshAttempts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`)
		case "SYNO.Core.System.info":
			switch r.Form.Get("_sid") {
			case "stale-sid":
				staleAttempts++
				fmt.Fprint(w, `{"success":false,"error":{"code":119}}`)
			case "fresh-sid":
				freshAttempts++
				fmt.Fprint(w, `{"success":true,"data":{"model":"DS923+"}}`)
			default:
				t.Errorf("system info SID = %q", r.Form.Get("_sid"))
				fmt.Fprint(w, `{"success":false,"error":{"code":119}}`)
			}
		case "SYNO.API.Auth.login":
			t.Error("login must not be called; the client has no password")
			fmt.Fprint(w, `{"success":false,"error":{"code":400}}`)
		default:
			t.Errorf("unexpected request %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:                server.URL,
		SessionID:              "stale-sid",
		HTTPClient:             server.Client(),
		PreserveSessionOnClose: true,
		Resume: func(context.Context) (string, string, error) {
			return "fresh-sid", "fresh-token", nil
		},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	info, err := client.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info.Model != "DS923+" {
		t.Fatalf("SystemInfo() model = %q", info.Model)
	}
	if staleAttempts != 1 || freshAttempts != 1 {
		t.Fatalf("attempts stale=%d fresh=%d, want one rejected then one refreshed retry", staleAttempts, freshAttempts)
	}
}

func TestPreferredVersionClampsToAdvertisedRange(t *testing.T) {
	for _, test := range []struct {
		name string
		info APIInfo
		want int
	}{
		{name: "current DSM", info: APIInfo{MinVersion: 1, MaxVersion: 7}, want: 7},
		{name: "older DSM", info: APIInfo{MinVersion: 1, MaxVersion: 6}, want: 6},
		{name: "newer minimum", info: APIInfo{MinVersion: 8, MaxVersion: 9}, want: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := preferredVersion(test.info, 7); got != test.want {
				t.Fatalf("preferredVersion() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestClientOTPChallengeSavesTrustedDevice(t *testing.T) {
	var loginCount, otpCount int
	var savedDeviceID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`)
		case "SYNO.API.Auth.login":
			loginCount++
			if got := r.Form.Get("version"); got != "7" {
				t.Errorf("auth version = %q, want 7", got)
			}
			if got := r.Form.Get("session"); got != dsmctlSession {
				t.Errorf("auth session = %q, want %q", got, dsmctlSession)
			}
			if got := r.Form.Get("passwd"); got != "secret" {
				t.Errorf("passwd = %q", got)
			}
			if r.URL.Query().Get("passwd") != "" || r.URL.Query().Get("otp_code") != "" {
				t.Error("password or OTP was placed in URL query")
			}
			if loginCount == 1 {
				if r.Form.Get("otp_code") != "" {
					t.Error("first login unexpectedly included an OTP")
				}
				fmt.Fprint(w, `{"success":false,"error":{"code":403}}`)
				return
			}
			if got := r.Form.Get("otp_code"); got != "123456" {
				t.Errorf("otp_code = %q", got)
			}
			if r.Form.Get("enable_device_token") != "yes" || r.Form.Get("device_name") != "dsmctl@test-host" {
				t.Errorf("trusted device parameters = %#v", r.Form)
			}
			fmt.Fprint(w, `{"success":true,"data":{"sid":"mfa-sid","synotoken":"mfa-token","did":"trusted-device-id"}}`)
		case "SYNO.Core.System.info":
			fmt.Fprint(w, `{"success":true,"data":{"model":"DS1821+"}}`)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected request %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "mfa-user",
		Password:   "secret",
		DeviceName: "dsmctl@test-host",
		OTPProvider: func(context.Context) (string, error) {
			otpCount++
			return "123456", nil
		},
		SaveDeviceID: func(_ context.Context, deviceID string) error {
			savedDeviceID = deviceID
			return nil
		},
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.SystemInfo(context.Background()); err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if loginCount != 2 || otpCount != 1 || savedDeviceID != "trusted-device-id" {
		t.Fatalf("loginCount=%d otpCount=%d savedDeviceID=%q", loginCount, otpCount, savedDeviceID)
	}
}

func TestClientUsesTrustedDeviceWithoutOTP(t *testing.T) {
	var otpCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
		case "SYNO.API.Auth.login":
			if r.Form.Get("device_name") != "dsmctl@test-host" || r.Form.Get("device_id") != "trusted-device-id" {
				t.Errorf("trusted device parameters = %#v", r.Form)
			}
			if r.Form.Get("otp_code") != "" {
				t.Error("trusted-device login included an OTP")
			}
			fmt.Fprint(w, `{"success":true,"data":{"sid":"trusted-sid"}}`)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected request %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "mfa-user",
		Password:   "secret",
		DeviceName: "dsmctl@test-host",
		DeviceID:   "trusted-device-id",
		OTPProvider: func(context.Context) (string, error) {
			otpCalled = true
			return "", errors.New("OTP should not be requested")
		},
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if otpCalled {
		t.Fatal("OTP provider was called for a trusted-device login")
	}
}

func TestClientReportsOTPRequiredWithoutProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("api") == "SYNO.API.Info" {
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
			return
		}
		fmt.Fprint(w, `{"success":false,"error":{"code":403}}`)
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "mfa-user",
		Password:   "secret",
		DeviceName: "dsmctl@test-host",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	err = client.Authenticate(context.Background())
	if !IsOTPRequired(err) {
		t.Fatalf("Authenticate() error = %v, want OTPRequiredError", err)
	}
}

func TestClientRetriesInvalidOTP(t *testing.T) {
	codes := []string{"111111", "222222", "333333"}
	providerCalls := 0
	loginCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("api") == "SYNO.API.Info" {
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7}}}`)
			return
		}
		loginCalls++
		if loginCalls == 1 {
			fmt.Fprint(w, `{"success":false,"error":{"code":403}}`)
			return
		}
		if got, want := r.Form.Get("otp_code"), codes[loginCalls-2]; got != want {
			t.Errorf("otp_code = %q, want %q", got, want)
		}
		if loginCalls < 4 {
			fmt.Fprint(w, `{"success":false,"error":{"code":404}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"sid":"retry-sid","did":"retry-did"}}`)
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "mfa-user",
		Password:   "secret",
		DeviceName: "dsmctl@test-host",
		OTPProvider: func(context.Context) (string, error) {
			code := codes[providerCalls]
			providerCalls++
			return code, nil
		},
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if providerCalls != 3 || loginCalls != 4 {
		t.Fatalf("providerCalls=%d loginCalls=%d", providerCalls, loginCalls)
	}
}

func TestClientCompatibilityReportSelectsBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3,"requestFormat":"JSON"}}}`)
		case "SYNO.API.Auth.login":
			fmt.Fprint(w, `{"success":true,"data":{"sid":"compatibility-sid","synotoken":"compatibility-token"}}`)
		case "SYNO.Core.System.info":
			if r.Form.Get("version") != "3" {
				t.Errorf("system info version = %q, want 3", r.Form.Get("version"))
			}
			fmt.Fprint(w, `{"success":true,"data":{"model":"DS1621+","firmware_ver":"DSM 7.3.2-86009 Update 1"}}`)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected request %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "automation",
		Password:   "secret",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	report, err := client.Compatibility(context.Background())
	if err != nil {
		t.Fatalf("Compatibility() error = %v", err)
	}
	if report.DSM.Major != 7 || report.DSM.Minor != 3 || report.DSM.Build != 86009 {
		t.Fatalf("DSM version = %#v", report.DSM)
	}
	var systemSelection compatibility.Selection
	for _, selection := range report.Operations {
		if selection.Operation == "system.info" {
			systemSelection = selection
			break
		}
	}
	if !systemSelection.Supported || systemSelection.Backend != "core-system-v3" || systemSelection.Version != 3 {
		t.Fatalf("system selection = %#v; operations = %#v", systemSelection, report.Operations)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
