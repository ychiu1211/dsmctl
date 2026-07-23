package weblogin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/flynn/noise"
)

func TestGatewayEnrollmentKeepsPKCEVerifierServerSideAndValidatesState(t *testing.T) {
	requests := 0
	dsm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		_ = req.ParseForm()
		if req.Form.Get("type") != "code" || req.Form.Get("code") != "one-time-code" || req.Form.Get("code_verifier") == "" || req.Form.Get("ik_message") == "" {
			t.Errorf("exchange form = %#v", req.Form)
		}
		fmt.Fprint(w, `{"success":true,"data":{"account":"operator","sid":"web-sid","synotoken":"web-token","device_id":"web-device"}}`)
	}))
	defer dsm.Close()

	enrollment, start, err := BeginEnrollment(dsm.URL, "https://gateway.example/admin/", Options{HTTPClient: dsm.Client()})
	if err != nil {
		t.Fatal(err)
	}
	loginURL, err := url.Parse(start.LoginURL)
	if err != nil {
		t.Fatal(err)
	}
	if loginURL.Query().Get("code_challenge") == "" || loginURL.Query().Get("code_verifier") != "" || loginURL.Query().Get("opener") != "https://gateway.example/admin/" {
		t.Fatalf("login URL query = %#v", loginURL.Query())
	}
	// force_login makes DSM run the code grant (and return a code) instead of
	// loading the desktop when the browser already holds a DSM session.
	if loginURL.Query().Get("force_login") != "yes" {
		t.Fatalf("login URL must force login to obtain a code: %q", start.LoginURL)
	}
	suite := noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2b)
	serverKey, err := suite.GenerateKeypair(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rs := base64.RawURLEncoding.EncodeToString(serverKey.Public)
	if _, err := enrollment.Complete(context.Background(), "one-time-code", rs, "wrong-state"); err == nil || requests != 0 {
		t.Fatalf("state mismatch error=%v requests=%d", err, requests)
	}
	result, err := enrollment.Complete(context.Background(), "one-time-code", rs, start.State)
	if err != nil {
		t.Fatal(err)
	}
	if result.SID != "web-sid" || result.SynoToken != "web-token" || result.Account != "operator" || len(result.LocalPrivateKey) == 0 {
		t.Fatalf("enrollment result = %#v", result)
	}
}
