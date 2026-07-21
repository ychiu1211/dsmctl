package provision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func testTarget(t *testing.T, handler http.HandlerFunc) Target {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return Target{BaseURL: srv.URL, HTTPClient: srv.Client()}
}

func TestCreateFirstAdminSendsSequentialStopOnErrorCompound(t *testing.T) {
	var form url.Values
	var compound []map[string]any
	target := testTarget(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.Form
		_ = json.Unmarshal([]byte(r.Form.Get("compound")), &compound)
		_, _ = w.Write([]byte(`{"success":true,"data":{"has_fail":false,"result":[{"success":true},{"success":true}]}}`))
	})

	err := CreateFirstAdmin(context.Background(), target, AdminRequest{Username: "testuser", Password: "S3cret-Pass-1234"})
	if err != nil {
		t.Fatalf("CreateFirstAdmin error = %v", err)
	}
	if form.Get("api") != "SYNO.Entry.Request" || form.Get("method") != "request" {
		t.Fatalf("wrong entry api/method: %v", form)
	}
	if form.Get("mode") != "sequential" || form.Get("stop_when_error") != "true" {
		t.Fatalf("compound must be sequential and stop-on-error: mode=%q stop=%q", form.Get("mode"), form.Get("stop_when_error"))
	}
	if len(compound) != 2 {
		t.Fatalf("compound length = %d, want 2", len(compound))
	}
	create := compound[0]
	if create["api"] != "SYNO.Core.User" || create["method"] != "create" || create["name"] != "testuser" || create["password"] != "S3cret-Pass-1234" {
		t.Fatalf("create step = %v", create)
	}
	member := compound[1]
	if member["api"] != "SYNO.Core.Group.Member" || member["method"] != "add" || member["group"] != "administrators" {
		t.Fatalf("group-member step = %v", member)
	}
	names, ok := member["name"].([]any)
	if !ok || len(names) != 1 || names[0] != "testuser" {
		t.Fatalf("group-member name = %v", member["name"])
	}
}

func TestCreateFirstAdminReportsHasFail(t *testing.T) {
	target := testTarget(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"has_fail":true,"result":[{"success":false,"error":{"code":402}}]}}`))
	})
	if err := CreateFirstAdmin(context.Background(), target, AdminRequest{Username: "admin", Password: "S3cret-Pass-1234"}); err == nil {
		t.Fatal("CreateFirstAdmin error = nil, want has_fail error")
	}
}

func TestCreateFirstAdminReportsTopLevelError(t *testing.T) {
	target := testTarget(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":105}}`))
	})
	if err := CreateFirstAdmin(context.Background(), target, AdminRequest{Username: "testuser", Password: "S3cret-Pass-1234"}); err == nil {
		t.Fatal("CreateFirstAdmin error = nil, want DSM error")
	}
}

func TestProvisionRefusesCleartextTarget(t *testing.T) {
	err := CreateFirstAdmin(context.Background(), Target{BaseURL: "http://10.0.0.1:5000"}, AdminRequest{Username: "testuser", Password: "S3cret-Pass-1234"})
	if err == nil {
		t.Fatal("CreateFirstAdmin error = nil, want https rejection")
	}
}

func TestLoginSendsCredentials(t *testing.T) {
	var form url.Values
	target := testTarget(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.Form
		_, _ = w.Write([]byte(`{"success":true,"data":{"sid":"abc"}}`))
	})
	if err := Login(context.Background(), target, "testuser", "pw-value"); err != nil {
		t.Fatalf("Login error = %v", err)
	}
	if form.Get("api") != "SYNO.API.Auth" || form.Get("method") != "login" || form.Get("account") != "testuser" || form.Get("passwd") != "pw-value" {
		t.Fatalf("login form = %v", form)
	}
}

func TestLoginReportsRejection(t *testing.T) {
	target := testTarget(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":400}}`))
	})
	if err := Login(context.Background(), target, "testuser", "wrong"); err == nil {
		t.Fatal("Login error = nil, want rejection")
	}
}
