package synology

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sessionDiscoveryBody = `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`

const systemInfoBody = `{"success":true,"data":{"hostname":"office-nas","model":"DS923+","serial":"SERIAL","firmware_ver":"DSM 7.2","cpu_vendor":"AMD","cpu_series":"R1600","cpu_cores":"2","ram_size":4096,"up_time":"3 days","time_zone":"Asia/Taipei","sys_temp":41,"sys_tempwarn":false}}`

func TestClientReusesInjectedSessionWithoutLogin(t *testing.T) {
	var loginCount, infoCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, sessionDiscoveryBody)
		case "SYNO.API.Auth.login":
			loginCount++
			t.Error("login must not be called when a session is injected")
			fmt.Fprint(w, `{"success":false,"error":{"code":400}}`)
		case "SYNO.Core.System.info":
			infoCount++
			if r.Form.Get("_sid") != "injected-sid" {
				t.Errorf("_sid = %q, want injected-sid", r.Form.Get("_sid"))
			}
			if cookie, err := r.Cookie("id"); err != nil || cookie.Value != "injected-sid" {
				t.Errorf("session cookie = %#v, err = %v", cookie, err)
			}
			fmt.Fprint(w, systemInfoBody)
		default:
			t.Errorf("unexpected request %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "automation",
		SessionID:  "injected-sid",
		SynoToken:  "injected-token",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	info, err := client.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info.Model != "DS923+" {
		t.Fatalf("SystemInfo() = %#v", info)
	}
	if loginCount != 0 {
		t.Fatalf("loginCount = %d, want 0", loginCount)
	}
	if infoCount < 1 {
		t.Fatalf("infoCount = %d, want >= 1", infoCount)
	}
}

func TestNewClientRequiresPasswordOrSession(t *testing.T) {
	if _, err := NewClient(Options{BaseURL: "https://nas.example.com:5001", Username: "admin"}); err == nil {
		t.Fatal("NewClient() without password or session returned nil error")
	}
	// A session alone is sufficient; a password alone is sufficient.
	if _, err := NewClient(Options{BaseURL: "https://nas.example.com:5001", Username: "admin", SessionID: "sid"}); err != nil {
		t.Fatalf("NewClient(session only) error = %v", err)
	}
	if _, err := NewClient(Options{BaseURL: "https://nas.example.com:5001", Username: "admin", Password: "pw"}); err != nil {
		t.Fatalf("NewClient(password only) error = %v", err)
	}
}

func TestValidateSession(t *testing.T) {
	infoResponse := systemInfoBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, sessionDiscoveryBody)
		case "SYNO.Core.System.info":
			fmt.Fprint(w, infoResponse)
		default:
			t.Errorf("unexpected request %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "admin",
		SessionID:  "some-sid",
		SynoToken:  "some-token",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// A live session probes successfully.
	if ok, err := client.ValidateSession(context.Background()); err != nil || !ok {
		t.Fatalf("ValidateSession(valid) = %v, %v", ok, err)
	}

	// A session error (119: SID not found) is reported as invalid, not an error.
	infoResponse = `{"success":false,"error":{"code":119}}`
	if ok, err := client.ValidateSession(context.Background()); err != nil || ok {
		t.Fatalf("ValidateSession(expired) = %v, %v, want false, nil", ok, err)
	}

	// A non-session API failure surfaces as an error.
	infoResponse = `{"success":false,"error":{"code":102}}`
	if ok, err := client.ValidateSession(context.Background()); err == nil || ok {
		t.Fatalf("ValidateSession(api error) = %v, %v, want false, error", ok, err)
	}

	// With no session held, validation is false without contacting DSM.
	client.sid = ""
	if ok, err := client.ValidateSession(context.Background()); err != nil || ok {
		t.Fatalf("ValidateSession(no session) = %v, %v, want false, nil", ok, err)
	}
}

func TestInjectedSessionExpiryWithoutPasswordErrorsCleanly(t *testing.T) {
	var loginCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, sessionDiscoveryBody)
		case "SYNO.API.Auth.login":
			loginCount++
			t.Error("login must not be attempted without a configured password")
			fmt.Fprint(w, `{"success":false,"error":{"code":400}}`)
		case "SYNO.Core.System.info":
			fmt.Fprint(w, `{"success":false,"error":{"code":119}}`)
		default:
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(Options{
		BaseURL:    server.URL,
		Username:   "admin",
		SessionID:  "expired-sid",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.SystemInfo(context.Background())
	if err == nil {
		t.Fatal("SystemInfo() with expired session and no recovery returned nil error")
	}
	if !IsSessionExpired(err) {
		t.Fatalf("an expired injected session with no recovery should be a detectable SessionExpiredError: %v", err)
	}
	if !strings.Contains(err.Error(), "dsmctl auth login") {
		t.Fatalf("error should direct the user to sign in again: %v", err)
	}
	if loginCount != 0 {
		t.Fatalf("loginCount = %d, want 0 (no empty-password login attempt)", loginCount)
	}
}
