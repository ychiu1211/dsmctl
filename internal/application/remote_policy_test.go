package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

type recordingApplyAuthorizer struct {
	calls    int
	tokenID  string
	nas      string
	revision uint64
	hash     string
	risk     string
}

func (a *recordingApplyAuthorizer) AdmitRemoteApply(_ context.Context, tokenID, nas string, revision uint64, hash, risk string) error {
	a.calls++
	a.tokenID, a.nas, a.revision, a.hash, a.risk = tokenID, nas, revision, hash, risk
	return nil
}

func TestRemoteApplyAdmissionIsAdditiveAndRechecksPrincipal(t *testing.T) {
	authorizer := &recordingApplyAuthorizer{}
	service := &Service{remoteApply: authorizer}
	if err := service.authorizeRemoteApply(context.Background(), "office", 7, "hash", "high"); err != nil || authorizer.calls != 0 {
		t.Fatalf("local admission err=%v calls=%d", err, authorizer.calls)
	}

	readOnly := remotepolicy.WithPrincipal(context.Background(), remotepolicy.Principal{TokenID: "reader", Scopes: map[string]struct{}{remotepolicy.ScopeRead: {}}, NAS: map[string]struct{}{"office": {}}})
	if err := service.authorizeRemoteApply(readOnly, "office", 7, "hash", "high"); !errors.Is(err, remotepolicy.ErrDenied) || authorizer.calls != 0 {
		t.Fatalf("read-only admission err=%v calls=%d", err, authorizer.calls)
	}

	operator := remotepolicy.WithPrincipal(context.Background(), remotepolicy.Principal{TokenID: "operator", Scopes: map[string]struct{}{remotepolicy.ScopeApply: {}}, NAS: map[string]struct{}{"office": {}}})
	if err := service.authorizeRemoteApply(operator, "office", 7, "hash", "high"); err != nil {
		t.Fatal(err)
	}
	if authorizer.calls != 1 || authorizer.tokenID != "operator" || authorizer.nas != "office" || authorizer.revision != 7 || authorizer.hash != "hash" || authorizer.risk != "high" {
		t.Fatalf("authorizer = %#v", authorizer)
	}
}

func TestRemoteProfileFilteringAndExplicitTargetAuthorization(t *testing.T) {
	cfg := &config.Config{DefaultNAS: "hidden", NAS: map[string]config.Profile{"allowed": {URL: "https://allowed.example"}, "hidden": {URL: "https://hidden.example"}}}
	service := &Service{config: cfg, configSource: config.StaticSource{Config: cfg}}
	principal := remotepolicy.Principal{TokenID: "reader", Scopes: map[string]struct{}{remotepolicy.ScopeRead: {}}, NAS: map[string]struct{}{"allowed": {}}}
	ctx := remotepolicy.WithPrincipal(context.Background(), principal)
	summaries, err := service.ListNASContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Name != "allowed" {
		t.Fatalf("summaries = %#v", summaries)
	}
	if _, err := service.AuthorizeRemoteTarget(ctx, "hidden"); !errors.Is(err, remotepolicy.ErrDenied) {
		t.Fatalf("hidden target = %v", err)
	}
	if _, err := service.AuthorizeRemoteTarget(ctx, ""); err == nil || !strings.Contains(err.Error(), "explicit nas") {
		t.Fatalf("omitted target = %v", err)
	}
	if name, err := service.AuthorizeRemoteTarget(ctx, "allowed"); err != nil || name != "allowed" {
		t.Fatalf("allowed target=%q err=%v", name, err)
	}
}
