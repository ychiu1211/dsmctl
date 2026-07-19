package state

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	bolt "go.etcd.io/bbolt"
)

const (
	oauthClientPrefix  = "dsmctl_oauth_client_"
	oauthRefreshPrefix = "dsmctl_oauth_refresh_"
)

// OAuthClient is a public OAuth client registered for authorization-code
// grants. It contains no client secret; exact redirect URIs are the durable
// trust boundary used by the authorization endpoint.
type OAuthClient struct {
	ID                      string    `json:"client_id"`
	Name                    string    `json:"client_name"`
	RedirectURIs            []string  `json:"redirect_uris"`
	GrantTypes              []string  `json:"grant_types"`
	ResponseTypes           []string  `json:"response_types"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method"`
	CreatedAt               time.Time `json:"created_at"`
}

type OAuthClientInput struct {
	Name                    string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

type oauthRefreshRecord struct {
	TokenID   string    `json:"token_id"`
	ClientID  string    `json:"client_id"`
	Resource  string    `json:"resource"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type OAuthTokenInput struct {
	Name         string
	Scopes       []string
	NASAllowlist []string
	ClientID     string
	Resource     string
}

type OAuthTokenSet struct {
	AccessToken  string
	RefreshToken string
	Token        MCPToken
	ExpiresIn    int64
}

func (r *Repository) RegisterOAuthClient(ctx context.Context, input OAuthClientInput) (OAuthClient, error) {
	if err := ctx.Err(); err != nil {
		return OAuthClient{}, err
	}
	input, err := normalizeOAuthClientInput(input)
	if err != nil {
		return OAuthClient{}, err
	}
	id, err := randomToken(24)
	if err != nil {
		return OAuthClient{}, err
	}
	client := OAuthClient{
		ID: oauthClientPrefix + id, Name: input.Name,
		RedirectURIs: input.RedirectURIs, GrantTypes: input.GrantTypes,
		ResponseTypes: input.ResponseTypes, TokenEndpointAuthMethod: input.TokenEndpointAuthMethod,
		CreatedAt: r.now().UTC(),
	}
	encoded, err := json.Marshal(client)
	if err != nil {
		return OAuthClient{}, err
	}
	err = r.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketOAuthClients)
		if bucket.Stats().KeyN >= MaxOAuthClients {
			return fmt.Errorf("at most %d OAuth clients may be registered", MaxOAuthClients)
		}
		return bucket.Put([]byte(client.ID), encoded)
	})
	return client, err
}

func (r *Repository) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	if err := ctx.Err(); err != nil {
		return OAuthClient{}, err
	}
	var client OAuthClient
	err := r.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketOAuthClients).Get([]byte(strings.TrimSpace(id)))
		if value == nil || json.Unmarshal(value, &client) != nil {
			return ErrNotFound
		}
		return nil
	})
	return client, err
}

func (r *Repository) IssueOAuthTokenSet(ctx context.Context, input OAuthTokenInput) (OAuthTokenSet, error) {
	if err := ctx.Err(); err != nil {
		return OAuthTokenSet{}, err
	}
	input.ClientID = strings.TrimSpace(input.ClientID)
	input.Resource = strings.TrimSpace(input.Resource)
	if input.ClientID == "" || input.Resource == "" {
		return OAuthTokenSet{}, ErrOAuthUnauthorized
	}
	if _, err := validateOAuthResource(input.Resource); err != nil {
		return OAuthTokenSet{}, err
	}
	now := r.now().UTC()
	expiresAt := now.Add(OAuthAccessTokenTTL)
	tokenInput, err := normalizeMCPTokenInput(MCPTokenInput{
		Name: input.Name, Scopes: input.Scopes, NASAllowlist: input.NASAllowlist, ExpiresAt: &expiresAt,
	}, now)
	if err != nil {
		return OAuthTokenSet{}, err
	}
	accessRaw, accessRecord, err := newMCPTokenRecord(tokenInput, now)
	if err != nil {
		return OAuthTokenSet{}, err
	}
	accessRecord.AuthMode = "oauth"
	accessRecord.OAuthClientID = input.ClientID
	refreshSecret, err := randomToken(32)
	if err != nil {
		return OAuthTokenSet{}, err
	}
	refreshRaw := oauthRefreshPrefix + refreshSecret
	refreshDigest := sha256.Sum256([]byte(refreshRaw))
	refreshRecord := oauthRefreshRecord{
		TokenID: accessRecord.ID, ClientID: input.ClientID, Resource: input.Resource,
		CreatedAt: now, ExpiresAt: now.Add(OAuthRefreshTokenTTL),
	}
	err = r.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(bucketOAuthClients).Get([]byte(input.ClientID)) == nil {
			return ErrOAuthUnauthorized
		}
		if err := putMCPTokenRecord(tx, accessRecord); err != nil {
			return err
		}
		encoded, err := json.Marshal(refreshRecord)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketOAuthRefresh).Put(refreshDigest[:], encoded)
	})
	if err != nil {
		return OAuthTokenSet{}, err
	}
	return OAuthTokenSet{
		AccessToken: accessRaw, RefreshToken: refreshRaw, Token: accessRecord.MCPToken,
		ExpiresIn: int64(OAuthAccessTokenTTL / time.Second),
	}, nil
}

// RefreshOAuthTokenSet rotates both credentials atomically while preserving
// the MCP token ID used by policy and audit attribution.
func (r *Repository) RefreshOAuthTokenSet(ctx context.Context, rawRefresh, clientID, resource string) (OAuthTokenSet, error) {
	if err := ctx.Err(); err != nil {
		return OAuthTokenSet{}, err
	}
	clientID = strings.TrimSpace(clientID)
	resource = strings.TrimSpace(resource)
	if !strings.HasPrefix(rawRefresh, oauthRefreshPrefix) || clientID == "" || resource == "" {
		return OAuthTokenSet{}, ErrOAuthUnauthorized
	}
	oldRefreshDigest := sha256.Sum256([]byte(rawRefresh))
	rawRefresh = ""
	newAccessSecret, err := randomToken(32)
	if err != nil {
		return OAuthTokenSet{}, err
	}
	newAccessRaw := mcpTokenPrefix + newAccessSecret
	newAccessDigest := sha256.Sum256([]byte(newAccessRaw))
	newRefreshSecret, err := randomToken(32)
	if err != nil {
		return OAuthTokenSet{}, err
	}
	newRefreshRaw := oauthRefreshPrefix + newRefreshSecret
	newRefreshDigest := sha256.Sum256([]byte(newRefreshRaw))
	now := r.now().UTC()
	var token MCPToken
	err = r.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketOAuthRefresh)
		value := bucket.Get(oldRefreshDigest[:])
		var refresh oauthRefreshRecord
		if value == nil || json.Unmarshal(value, &refresh) != nil ||
			refresh.ClientID != clientID || refresh.Resource != resource || !now.Before(refresh.ExpiresAt) {
			return ErrOAuthUnauthorized
		}
		record, err := readMCPToken(tx, refresh.TokenID)
		if err != nil || record.RevokedAt != nil {
			return ErrOAuthUnauthorized
		}
		if err := tx.Bucket(bucketTokenDigests).Delete(record.Digest); err != nil {
			return err
		}
		record.Digest = append([]byte(nil), newAccessDigest[:]...)
		record.UpdatedAt = now
		expiresAt := now.Add(OAuthAccessTokenTTL)
		record.ExpiresAt = &expiresAt
		token = record.MCPToken
		if err := putMCPTokenRecord(tx, record); err != nil {
			return err
		}
		if err := bucket.Delete(oldRefreshDigest[:]); err != nil {
			return err
		}
		// Rotation does not extend the original grant lifetime. An actively used
		// refresh token still reaches its absolute 365-day expiry.
		encoded, err := json.Marshal(refresh)
		if err != nil {
			return err
		}
		return bucket.Put(newRefreshDigest[:], encoded)
	})
	if err != nil {
		return OAuthTokenSet{}, ErrOAuthUnauthorized
	}
	return OAuthTokenSet{
		AccessToken: newAccessRaw, RefreshToken: newRefreshRaw, Token: token,
		ExpiresIn: int64(OAuthAccessTokenTTL / time.Second),
	}, nil
}

func deleteOAuthRefreshTokensForMCPToken(tx *bolt.Tx, tokenID string) error {
	bucket := tx.Bucket(bucketOAuthRefresh)
	var keys [][]byte
	if err := bucket.ForEach(func(key, value []byte) error {
		var record oauthRefreshRecord
		if json.Unmarshal(value, &record) == nil && record.TokenID == tokenID {
			keys = append(keys, append([]byte(nil), key...))
		}
		return nil
	}); err != nil {
		return err
	}
	for _, key := range keys {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

func normalizeOAuthClientInput(input OAuthClientInput) (OAuthClientInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = "MCP client"
	}
	if len(input.Name) > 128 || strings.IndexFunc(input.Name, unicode.IsControl) >= 0 {
		return OAuthClientInput{}, errors.New("client_name must be 1 to 128 bytes without control characters")
	}
	if len(input.RedirectURIs) == 0 || len(input.RedirectURIs) > 8 {
		return OAuthClientInput{}, errors.New("redirect_uris must contain 1 to 8 entries")
	}
	seen := make(map[string]struct{}, len(input.RedirectURIs))
	redirects := make([]string, 0, len(input.RedirectURIs))
	for _, raw := range input.RedirectURIs {
		redirect, err := validateOAuthRedirectURI(raw)
		if err != nil {
			return OAuthClientInput{}, err
		}
		if _, duplicate := seen[redirect]; duplicate {
			continue
		}
		seen[redirect] = struct{}{}
		redirects = append(redirects, redirect)
	}
	input.RedirectURIs = redirects
	if len(input.GrantTypes) == 0 {
		input.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	grants, err := normalizeOAuthValues(input.GrantTypes, map[string]struct{}{"authorization_code": {}, "refresh_token": {}})
	if err != nil || !containsString(grants, "authorization_code") {
		return OAuthClientInput{}, errors.New("grant_types must include authorization_code and may include refresh_token")
	}
	input.GrantTypes = grants
	if len(input.ResponseTypes) == 0 {
		input.ResponseTypes = []string{"code"}
	}
	responses, err := normalizeOAuthValues(input.ResponseTypes, map[string]struct{}{"code": {}})
	if err != nil || !containsString(responses, "code") {
		return OAuthClientInput{}, errors.New("response_types must contain only code")
	}
	input.ResponseTypes = responses
	input.TokenEndpointAuthMethod = strings.TrimSpace(input.TokenEndpointAuthMethod)
	if input.TokenEndpointAuthMethod == "" {
		input.TokenEndpointAuthMethod = "none"
	}
	if input.TokenEndpointAuthMethod != "none" {
		return OAuthClientInput{}, errors.New("only public clients with token_endpoint_auth_method none are supported")
	}
	return input, nil
}

func validateOAuthRedirectURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("redirect_uri must be an absolute URI without credentials or fragment")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		host := strings.ToLower(parsed.Hostname())
		if host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return "", errors.New("http redirect_uri is allowed only for localhost or loopback IPs")
		}
	default:
		return "", errors.New("redirect_uri must use https or loopback http")
	}
	return parsed.String(), nil
}

func validateOAuthResource(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return "", errors.New("resource must be an absolute http or https URI without credentials, query, or fragment")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("resource must use http or https")
	}
	return parsed.String(), nil
}

func normalizeOAuthValues(values []string, allowed map[string]struct{}) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := allowed[value]; !ok {
			return nil, fmt.Errorf("unsupported OAuth value %q", value)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
