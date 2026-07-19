package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

func TestOAuthClientRegistrationValidatesPublicRedirects(t *testing.T) {
	repository := openOAuthTestRepository(t, nil)
	ctx := context.Background()
	client, err := repository.RegisterOAuthClient(ctx, OAuthClientInput{
		Name: "Codex desktop", RedirectURIs: []string{"http://127.0.0.1:32123/callback", "https://client.example/oauth/callback"},
		GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.ID == "" || client.TokenEndpointAuthMethod != "none" || len(client.RedirectURIs) != 2 {
		t.Fatalf("unexpected registered client: %#v", client)
	}
	loaded, err := repository.OAuthClient(ctx, client.ID)
	if err != nil || loaded.Name != "Codex desktop" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	for _, invalid := range []string{
		"http://client.example/callback",
		"https://user:password@client.example/callback",
		"https://client.example/callback#fragment",
		"file:///tmp/callback",
	} {
		if _, err := repository.RegisterOAuthClient(ctx, OAuthClientInput{Name: "bad", RedirectURIs: []string{invalid}}); err == nil {
			t.Fatalf("accepted invalid redirect URI %q", invalid)
		}
	}
}

func TestOAuthTokenIssueAndRefreshRotateAtomically(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repository := openOAuthTestRepository(t, func() time.Time { return now })
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://10.0.0.20:5001", TLSMode: TLSSystemCA}); err != nil {
		t.Fatal(err)
	}
	client, err := repository.RegisterOAuthClient(ctx, OAuthClientInput{Name: "Codex desktop", RedirectURIs: []string{"http://127.0.0.1:32123/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := repository.IssueOAuthTokenSet(ctx, OAuthTokenInput{
		Name: "OAuth: Codex desktop", ClientID: client.ID, Resource: "https://nas.example/dsmctl/mcp",
		Scopes: []string{remotepolicy.ScopeRead, remotepolicy.ScopeApply}, NASAllowlist: []string{"office"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if issued.AccessToken == "" || issued.RefreshToken == "" || issued.ExpiresIn != int64(OAuthAccessTokenTTL/time.Second) {
		t.Fatalf("unexpected token set: %#v", issued)
	}
	principal, err := repository.AuthenticateMCPToken(ctx, issued.AccessToken)
	if err != nil || principal.TokenID != issued.Token.ID || !principal.HasScope(remotepolicy.ScopeApply) {
		t.Fatalf("principal=%#v err=%v", principal, err)
	}
	rotated, err := repository.RefreshOAuthTokenSet(ctx, issued.RefreshToken, client.ID, "https://nas.example/dsmctl/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Token.ID != issued.Token.ID || rotated.AccessToken == issued.AccessToken || rotated.RefreshToken == issued.RefreshToken {
		t.Fatalf("refresh did not preserve identity and rotate secrets: old=%#v new=%#v", issued.Token, rotated.Token)
	}
	if _, err := repository.AuthenticateMCPToken(ctx, issued.AccessToken); !errors.Is(err, ErrTokenUnauthorized) {
		t.Fatalf("old access token remained valid: %v", err)
	}
	if _, err := repository.RefreshOAuthTokenSet(ctx, issued.RefreshToken, client.ID, "https://nas.example/dsmctl/mcp"); !errors.Is(err, ErrOAuthUnauthorized) {
		t.Fatalf("old refresh token remained valid: %v", err)
	}
	if _, err := repository.AuthenticateMCPToken(ctx, rotated.AccessToken); err != nil {
		t.Fatalf("rotated access token rejected: %v", err)
	}
	if _, err := repository.ExpireMCPToken(ctx, rotated.Token.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RefreshOAuthTokenSet(ctx, rotated.RefreshToken, client.ID, "https://nas.example/dsmctl/mcp"); !errors.Is(err, ErrOAuthUnauthorized) {
		t.Fatalf("manual expiry did not invalidate refresh grant: %v", err)
	}
}

func TestOAuthRefreshRejectsClientAndResourceMismatchWithoutConsuming(t *testing.T) {
	repository := openOAuthTestRepository(t, nil)
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://10.0.0.20:5001", TLSMode: TLSSystemCA}); err != nil {
		t.Fatal(err)
	}
	client, _ := repository.RegisterOAuthClient(ctx, OAuthClientInput{Name: "client", RedirectURIs: []string{"http://localhost:32123/callback"}})
	issued, err := repository.IssueOAuthTokenSet(ctx, OAuthTokenInput{Name: "OAuth client", ClientID: client.ID, Resource: "https://nas.example/mcp", Scopes: []string{remotepolicy.ScopeRead}, NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.RefreshOAuthTokenSet(ctx, issued.RefreshToken, client.ID+"x", "https://nas.example/mcp"); !errors.Is(err, ErrOAuthUnauthorized) {
		t.Fatalf("client mismatch: %v", err)
	}
	if _, err := repository.RefreshOAuthTokenSet(ctx, issued.RefreshToken, client.ID, "https://other.example/mcp"); !errors.Is(err, ErrOAuthUnauthorized) {
		t.Fatalf("resource mismatch: %v", err)
	}
	if _, err := repository.RefreshOAuthTokenSet(ctx, issued.RefreshToken, client.ID, "https://nas.example/mcp"); err != nil {
		t.Fatalf("mismatch consumed the refresh token: %v", err)
	}
}

func TestOAuthRefreshLifetimeIsAbsolute(t *testing.T) {
	issuedAt := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	now := issuedAt
	repository := openOAuthTestRepository(t, func() time.Time { return now })
	ctx := context.Background()
	if _, err := repository.CreateProfile(ctx, ProfileInput{Name: "office", URL: "https://10.0.0.20:5001", TLSMode: TLSSystemCA}); err != nil {
		t.Fatal(err)
	}
	client, err := repository.RegisterOAuthClient(ctx, OAuthClientInput{Name: "client", RedirectURIs: []string{"http://localhost:32123/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := repository.IssueOAuthTokenSet(ctx, OAuthTokenInput{Name: "OAuth client", ClientID: client.ID, Resource: "https://nas.example/mcp", Scopes: []string{remotepolicy.ScopeRead}, NASAllowlist: []string{"office"}})
	if err != nil {
		t.Fatal(err)
	}
	now = issuedAt.Add(OAuthRefreshTokenTTL - time.Second)
	rotated, err := repository.RefreshOAuthTokenSet(ctx, issued.RefreshToken, client.ID, "https://nas.example/mcp")
	if err != nil {
		t.Fatalf("refresh immediately before absolute expiry: %v", err)
	}
	now = issuedAt.Add(OAuthRefreshTokenTTL + time.Second)
	if _, err := repository.RefreshOAuthTokenSet(ctx, rotated.RefreshToken, client.ID, "https://nas.example/mcp"); !errors.Is(err, ErrOAuthUnauthorized) {
		t.Fatalf("rotation extended the absolute refresh lifetime: %v", err)
	}
}

func openOAuthTestRepository(t *testing.T, now func() time.Time) *Repository {
	t.Helper()
	options := OpenOptions{Now: now, PasswordHashParameters: &PasswordHashParameters{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}}
	repository, err := OpenWithOptions(filepath.Join(t.TempDir(), "gateway.db"), make([]byte, 32), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository
}
