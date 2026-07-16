package synology

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
			if got := r.Form.Get("version"); got != "6" {
				t.Errorf("auth version = %q, want 6", got)
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
