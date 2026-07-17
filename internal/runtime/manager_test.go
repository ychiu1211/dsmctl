package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
)

type resolverFunc func(context.Context, string, config.Profile) (string, error)

func (f resolverFunc) Password(ctx context.Context, name string, profile config.Profile) (string, error) {
	return f(ctx, name, profile)
}

type fakeSessionStore struct {
	sessions map[string]credentials.SessionCredential
}

func (f *fakeSessionStore) Session(_ context.Context, name string) (credentials.SessionCredential, error) {
	return f.sessions[name], nil
}

func (f *fakeSessionStore) SaveSession(_ context.Context, name string, session credentials.SessionCredential) error {
	f.sessions[name] = session
	return nil
}

func (f *fakeSessionStore) DeleteSession(_ context.Context, name string) (bool, error) {
	_, ok := f.sessions[name]
	delete(f.sessions, name)
	return ok, nil
}

func TestManagerMaintainsIndependentClientsPerNAS(t *testing.T) {
	office := newDSMServer(t, "DS923+", "office-sid")
	defer office.Close()
	lab := newDSMServer(t, "DS224+", "lab-sid")
	defer lab.Close()

	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: office.URL, Username: "user"}
	cfg.NAS["lab"] = config.Profile{URL: lab.URL, Username: "user"}
	manager := NewManager(cfg, resolverFunc(func(_ context.Context, name string, _ config.Profile) (string, error) {
		return name + "-password", nil
	}))

	_, officeClient, err := manager.Client(context.Background(), "office")
	if err != nil {
		t.Fatalf("office Client() error = %v", err)
	}
	_, labClient, err := manager.Client(context.Background(), "lab")
	if err != nil {
		t.Fatalf("lab Client() error = %v", err)
	}
	officeInfo, err := officeClient.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("office SystemInfo() error = %v", err)
	}
	labInfo, err := labClient.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("lab SystemInfo() error = %v", err)
	}
	if officeInfo.Model != "DS923+" || labInfo.Model != "DS224+" {
		t.Fatalf("models office=%q lab=%q", officeInfo.Model, labInfo.Model)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestSessionInfoReportsStateWithoutResolvingCredentials(t *testing.T) {
	server := newDSMServer(t, "DS923+", "office-sid")
	defer server.Close()

	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: server.URL, Username: "user"}
	resolutions := 0
	manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
		resolutions++
		return "password", nil
	}))

	if info := manager.SessionInfo("office"); info != (SessionInfo{}) {
		t.Fatalf("SessionInfo before Client() = %#v", info)
	}
	if resolutions != 0 {
		t.Fatalf("SessionInfo resolved credentials %d times", resolutions)
	}
	_, client, err := manager.Client(context.Background(), "office")
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if info := manager.SessionInfo("office"); !info.ClientCached || info.SessionHeld {
		t.Fatalf("SessionInfo after Client() = %#v", info)
	}
	if _, err := client.SystemInfo(context.Background()); err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info := manager.SessionInfo("office"); !info.ClientCached || !info.SessionHeld {
		t.Fatalf("SessionInfo after login = %#v", info)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if info := manager.SessionInfo("office"); info != (SessionInfo{}) {
		t.Fatalf("SessionInfo after Close() = %#v", info)
	}
}

func TestManagerReusesStoredSessionWithoutLoginOrPassword(t *testing.T) {
	loginCount, logoutCount := 0, 0
	server := newCountingDSMServer(t, "DS923+", "stored-sid", &loginCount, &logoutCount)
	defer server.Close()

	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: server.URL, Username: "user"}

	store := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"office": {SID: "stored-sid", SynoToken: "stored-token"},
	}}
	resolutions := 0
	manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
		resolutions++
		return "password", nil
	}), WithSessionStore(store))

	_, client, err := manager.Client(context.Background(), "office")
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	info, err := client.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info.Model != "DS923+" {
		t.Fatalf("SystemInfo() model = %q", info.Model)
	}
	if loginCount != 0 {
		t.Fatalf("login called %d times; a stored session must be reused without logging in", loginCount)
	}
	if resolutions != 0 {
		t.Fatalf("password resolved %d times; a stored session must be reused without a password", resolutions)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if logoutCount != 0 {
		t.Fatalf("logout called %d times on Close; the persisted session must stay valid for later processes", logoutCount)
	}
}

func TestSeededClientPicksUpRenewedStoredSessionAfterExpiry(t *testing.T) {
	staleAttempts, freshAttempts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`)
		case "SYNO.Core.System.info":
			if r.Form.Get("_sid") == "stale-sid" {
				staleAttempts++
				fmt.Fprint(w, `{"success":false,"error":{"code":119}}`)
				return
			}
			freshAttempts++
			if r.Form.Get("_sid") != "fresh-sid" {
				t.Errorf("system info SID = %q", r.Form.Get("_sid"))
			}
			fmt.Fprint(w, `{"success":true,"data":{"model":"DS923+"}}`)
		case "SYNO.API.Auth.login":
			t.Error("login must not be called; renewal comes from the session store")
			fmt.Fprint(w, `{"success":false,"error":{"code":400}}`)
		default:
			t.Errorf("unexpected API call %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer server.Close()

	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: server.URL, Username: "user"}
	store := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"office": {SID: "stale-sid", SynoToken: "stale-token"},
	}}
	manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
		t.Error("a seeded client must not resolve a password")
		return "", nil
	}), WithSessionStore(store))

	_, client, err := manager.Client(context.Background(), "office")
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	// Another process signs in again and stores a fresh session while this
	// long-running process still holds the stale one.
	store.sessions["office"] = credentials.SessionCredential{SID: "fresh-sid", SynoToken: "fresh-token"}

	info, err := client.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info.Model != "DS923+" {
		t.Fatalf("SystemInfo() model = %q", info.Model)
	}
	if staleAttempts != 1 || freshAttempts != 1 {
		t.Fatalf("attempts stale=%d fresh=%d, want one rejected then one renewed retry", staleAttempts, freshAttempts)
	}
}

func TestRevokeStoredSessionLogsOutServerSide(t *testing.T) {
	loginCount, logoutCount := 0, 0
	server := newCountingDSMServer(t, "DS923+", "stored-sid", &loginCount, &logoutCount)
	defer server.Close()

	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: server.URL, Username: "user"}
	store := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"office": {SID: "stored-sid", SynoToken: "stored-token"},
	}}
	manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
		t.Error("revocation must not resolve a password")
		return "", nil
	}), WithSessionStore(store))

	revoked, err := manager.RevokeStoredSession(context.Background(), "office")
	if err != nil {
		t.Fatalf("RevokeStoredSession() error = %v", err)
	}
	if !revoked {
		t.Fatal("RevokeStoredSession() = false, want true")
	}
	if logoutCount != 1 || loginCount != 0 {
		t.Fatalf("request counts login=%d logout=%d, want 0 and 1", loginCount, logoutCount)
	}
	if _, ok := store.sessions["office"]; !ok {
		t.Fatal("RevokeStoredSession() must not delete the stored entry; that is the caller's decision")
	}
}

func TestRevokeStoredSessionReportsNothingToRevoke(t *testing.T) {
	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: "http://127.0.0.1:9", Username: "user"}

	for name, store := range map[string]*fakeSessionStore{
		"no session stored":      {sessions: map[string]credentials.SessionCredential{}},
		"resume keys but no SID": {sessions: map[string]credentials.SessionCredential{"office": {LocalPrivateKey: []byte{1}, ServerPublicKey: []byte{2}}}},
	} {
		t.Run(name, func(t *testing.T) {
			manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
				return "", nil
			}), WithSessionStore(store))
			revoked, err := manager.RevokeStoredSession(context.Background(), "office")
			if err != nil || revoked {
				t.Fatalf("RevokeStoredSession() = %v, %v; want false, nil", revoked, err)
			}
		})
	}

	t.Run("profile not configured", func(t *testing.T) {
		store := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
			"retired": {SID: "stored-sid"},
		}}
		manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
			return "", nil
		}), WithSessionStore(store))
		revoked, err := manager.RevokeStoredSession(context.Background(), "retired")
		if err != nil || revoked {
			t.Fatalf("RevokeStoredSession() = %v, %v; want false, nil", revoked, err)
		}
	})

	t.Run("no session store", func(t *testing.T) {
		manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
			return "", nil
		}))
		revoked, err := manager.RevokeStoredSession(context.Background(), "office")
		if err != nil || revoked {
			t.Fatalf("RevokeStoredSession() = %v, %v; want false, nil", revoked, err)
		}
	})
}

func TestRevokeStoredSessionReportsUnreachableNAS(t *testing.T) {
	server := newDSMServer(t, "DS923+", "stored-sid")
	url := server.URL
	server.Close()

	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: url, Username: "user"}
	store := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{
		"office": {SID: "stored-sid"},
	}}
	manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
		return "", nil
	}), WithSessionStore(store))

	revoked, err := manager.RevokeStoredSession(context.Background(), "office")
	if err == nil || revoked {
		t.Fatalf("RevokeStoredSession() = %v, %v; want false and an error for an unreachable NAS", revoked, err)
	}
}

func TestManagerFallsBackToPasswordWithoutStoredSession(t *testing.T) {
	loginCount, logoutCount := 0, 0
	server := newCountingDSMServer(t, "DS224+", "login-sid", &loginCount, &logoutCount)
	defer server.Close()

	cfg := config.New()
	cfg.DefaultNAS = "office"
	cfg.NAS["office"] = config.Profile{URL: server.URL, Username: "user"}

	store := &fakeSessionStore{sessions: map[string]credentials.SessionCredential{}}
	resolutions := 0
	manager := NewManager(cfg, resolverFunc(func(context.Context, string, config.Profile) (string, error) {
		resolutions++
		return "password", nil
	}), WithSessionStore(store))

	_, client, err := manager.Client(context.Background(), "office")
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	info, err := client.SystemInfo(context.Background())
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info.Model != "DS224+" {
		t.Fatalf("SystemInfo() model = %q", info.Model)
	}
	if loginCount != 1 {
		t.Fatalf("expected one password login, loginCount = %d", loginCount)
	}
	if resolutions != 1 {
		t.Fatalf("expected one password resolution, resolutions = %d", resolutions)
	}
}

func newCountingDSMServer(t *testing.T, model, sid string, loginCount, logoutCount *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`)
		case "SYNO.API.Auth.login":
			*loginCount++
			fmt.Fprintf(w, `{"success":true,"data":{"sid":%q}}`, sid)
		case "SYNO.Core.System.info":
			if r.Form.Get("_sid") != sid {
				t.Errorf("system info SID = %q, want %q", r.Form.Get("_sid"), sid)
			}
			fmt.Fprintf(w, `{"success":true,"data":{"model":%q}}`, model)
		case "SYNO.API.Auth.logout":
			*logoutCount++
			if r.Form.Get("_sid") != sid {
				t.Errorf("logout SID = %q, want %q", r.Form.Get("_sid"), sid)
			}
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected API call %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
}

func newDSMServer(t *testing.T, model, sid string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Info.query":
			fmt.Fprint(w, `{"success":true,"data":{"SYNO.API.Auth":{"path":"entry.cgi","minVersion":1,"maxVersion":7},"SYNO.Core.System":{"path":"entry.cgi","minVersion":1,"maxVersion":3}}}`)
		case "SYNO.API.Auth.login":
			fmt.Fprintf(w, `{"success":true,"data":{"sid":%q}}`, sid)
		case "SYNO.Core.System.info":
			if r.Form.Get("_sid") != sid {
				t.Errorf("system info SID = %q, want %q", r.Form.Get("_sid"), sid)
			}
			fmt.Fprintf(w, `{"success":true,"data":{"model":%q}}`, model)
		case "SYNO.API.Auth.logout":
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			t.Errorf("unexpected API call %s.%s", r.Form.Get("api"), r.Form.Get("method"))
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
}
