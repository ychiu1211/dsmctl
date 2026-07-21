package admin

import (
	"context"
	"net/http"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/gateway/state"
)

// TestVaultProvisionSinkStoresRevealablePassword proves the gateway-specific
// glue: a provisioned password persisted through the sink lands in the vault
// bound to the profile's DSM account and is then returned by the same
// StoredPassword read the human-gated reveal uses.
func TestVaultProvisionSinkStoresRevealablePassword(t *testing.T) {
	_, repository, manager, _ := newTestHandler(t)
	defer manager.Close(context.Background())
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, state.ProfileInput{Name: "fresh", URL: "https://fresh.example:5001"}); err != nil {
		t.Fatal(err)
	}

	sink := &vaultProvisionSink{repository: repository, manager: manager}
	const generated = "generated-admin-password-9f3"
	if err := sink.PersistProvisioned(ctx, application.ProvisionPersist{Name: "fresh", URL: "https://fresh.example:5001", Username: "operator", Password: generated}); err != nil {
		t.Fatalf("PersistProvisioned() error = %v", err)
	}

	stored, err := repository.StoredPassword(ctx, "fresh")
	if err != nil || stored != generated {
		t.Fatalf("StoredPassword() = %q, %v; want the generated password", stored, err)
	}
	profile, err := repository.Profile(ctx, "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if !profile.PasswordStored || profile.Username != "operator" {
		t.Fatalf("profile after provision = %#v; want password_stored and account bound", profile)
	}
}

func TestProvisionEndpointValidatesInput(t *testing.T) {
	handler, repository, manager, adminSession := newTestHandler(t)
	defer manager.Close(context.Background())
	if _, err := repository.CreateProfile(context.Background(), state.ProfileInput{Name: "fresh", URL: "https://fresh.example:5001"}); err != nil {
		t.Fatal(err)
	}

	unauth := performJSON(handler, http.MethodPost, "/admin/api/profiles/fresh/provision", `{"admin_user":"operator"}`, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated provision status = %d", unauth.Code)
	}
	missing := performJSON(handler, http.MethodPost, "/admin/api/profiles/fresh/provision", `{"admin_user":"  "}`, adminSession)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing admin_user status = %d body=%s", missing.Code, missing.Body.String())
	}
	wrongMethod := performJSON(handler, http.MethodGet, "/admin/api/profiles/fresh/provision", "", adminSession)
	if wrongMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET provision status = %d", wrongMethod.Code)
	}
}

func TestProvisionAuditActionIsDistinct(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/admin/api/profiles/fresh/provision", nil)
	if got := adminAuditAction(req); got != "profile.provision" {
		t.Fatalf("adminAuditAction = %q, want profile.provision", got)
	}
}
