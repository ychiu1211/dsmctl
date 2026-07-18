package platformauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	HeaderName       = "X-Dsmctl-Platform-Assertion"
	DefaultAudience  = "dsmctl-synology-admin"
	MaxTTL           = 60 * time.Second
	DefaultTTL       = 45 * time.Second
	clockSkew        = 30 * time.Second
	maxAssertion     = 4096
	maxReplayEntries = 4096
)

var ErrUnauthorized = errors.New("platform administrator assertion is invalid")

type Identity struct {
	Subject string
}

type payload struct {
	Subject       string `json:"sub"`
	Administrator bool   `json:"admin"`
	Audience      string `json:"aud"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
	ID            string `json:"jti"`
}

type Signer struct {
	key      [sha256.Size]byte
	audience string
	ttl      time.Duration
	now      func() time.Time
}

type Verifier struct {
	key      [sha256.Size]byte
	audience string
	now      func() time.Time
	mu       sync.Mutex
	seen     map[string]time.Time
}

func ReadKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("platform assertion key file is required")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read platform assertion key: %w", err)
	}
	if len(value) != sha256.Size {
		return nil, errors.New("platform assertion key must contain exactly 32 bytes")
	}
	return value, nil
}

func NewSigner(key []byte, audience string) (*Signer, error) {
	return newSigner(key, audience, DefaultTTL, time.Now)
}

func newSigner(key []byte, audience string, ttl time.Duration, now func() time.Time) (*Signer, error) {
	if len(key) != sha256.Size {
		return nil, errors.New("platform assertion key must be exactly 32 bytes")
	}
	audience = strings.TrimSpace(audience)
	if audience == "" {
		return nil, errors.New("platform assertion audience is required")
	}
	if ttl <= 0 || ttl > MaxTTL {
		return nil, errors.New("platform assertion TTL must be between one second and one minute")
	}
	result := &Signer{audience: audience, ttl: ttl, now: now}
	copy(result.key[:], key)
	return result, nil
}

func NewVerifier(key []byte, audience string) (*Verifier, error) {
	return newVerifier(key, audience, time.Now)
}

func newVerifier(key []byte, audience string, now func() time.Time) (*Verifier, error) {
	if len(key) != sha256.Size {
		return nil, errors.New("platform assertion key must be exactly 32 bytes")
	}
	audience = strings.TrimSpace(audience)
	if audience == "" {
		return nil, errors.New("platform assertion audience is required")
	}
	result := &Verifier{audience: audience, now: now, seen: make(map[string]time.Time)}
	copy(result.key[:], key)
	return result, nil
}

func (s *Signer) Sign(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" || len(subject) > 256 || strings.ContainsAny(subject, "\r\n\x00") {
		return "", errors.New("platform administrator subject is invalid")
	}
	idBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, idBytes); err != nil {
		return "", fmt.Errorf("create platform assertion ID: %w", err)
	}
	now := s.now().UTC().Truncate(time.Second)
	claims := payload{Subject: subject, Administrator: true, Audience: s.audience, IssuedAt: now.Unix(), ExpiresAt: now.Add(s.ttl).Unix(), ID: hex.EncodeToString(idBytes)}
	encoded, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	message := base64.RawURLEncoding.EncodeToString(encoded)
	mac := hmac.New(sha256.New, s.key[:])
	_, _ = mac.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (v *Verifier) Verify(ctx context.Context, assertion string) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	if len(assertion) == 0 || len(assertion) > maxAssertion || strings.TrimSpace(assertion) != assertion {
		return Identity{}, ErrUnauthorized
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 2 {
		return Identity{}, ErrUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return Identity{}, ErrUnauthorized
	}
	mac := hmac.New(sha256.New, v.key[:])
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return Identity{}, ErrUnauthorized
	}
	encoded, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Identity{}, ErrUnauthorized
	}
	var claims payload
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&claims); err != nil {
		return Identity{}, ErrUnauthorized
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Identity{}, ErrUnauthorized
	}
	now := v.now().UTC()
	issuedAt, expiresAt := time.Unix(claims.IssuedAt, 0), time.Unix(claims.ExpiresAt, 0)
	if strings.TrimSpace(claims.Subject) == "" || len(claims.Subject) > 256 || strings.ContainsAny(claims.Subject, "\r\n\x00") ||
		!claims.Administrator || claims.Audience != v.audience || len(claims.ID) != 32 ||
		expiresAt.Sub(issuedAt) <= 0 || expiresAt.Sub(issuedAt) > MaxTTL ||
		issuedAt.After(now.Add(clockSkew)) || !expiresAt.After(now) {
		return Identity{}, ErrUnauthorized
	}
	if _, err := hex.DecodeString(claims.ID); err != nil {
		return Identity{}, ErrUnauthorized
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, expiry := range v.seen {
		if !expiry.After(now) {
			delete(v.seen, id)
		}
	}
	if _, exists := v.seen[claims.ID]; exists {
		return Identity{}, ErrUnauthorized
	}
	if len(v.seen) >= maxReplayEntries {
		return Identity{}, ErrUnauthorized
	}
	v.seen[claims.ID] = expiresAt
	return Identity{Subject: claims.Subject}, nil
}
