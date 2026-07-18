package platformauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAssertionVerificationAndReplayProtection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	key := []byte("0123456789abcdef0123456789abcdef")
	signer, err := newSigner(key, "gateway-admin", 45*time.Second, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := newVerifier(key, "gateway-admin", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	assertion, err := signer.Sign("DSM Admin")
	if err != nil {
		t.Fatal(err)
	}
	identity, err := verifier.Verify(context.Background(), assertion)
	if err != nil || identity.Subject != "DSM Admin" {
		t.Fatalf("Verify() = %#v, %v", identity, err)
	}
	if _, err := verifier.Verify(context.Background(), assertion); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("replay Verify() = %v", err)
	}
}

func TestAssertionFailsClosed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	key := []byte("0123456789abcdef0123456789abcdef")
	tests := []struct {
		name      string
		signKey   []byte
		signAud   string
		signTime  time.Time
		verifyKey []byte
		verifyAud string
		mutate    func(string) string
	}{
		{name: "forged", signKey: key, signAud: "admin", signTime: now, verifyKey: []byte("abcdef0123456789abcdef0123456789"), verifyAud: "admin"},
		{name: "wrong audience", signKey: key, signAud: "other", signTime: now, verifyKey: key, verifyAud: "admin"},
		{name: "expired", signKey: key, signAud: "admin", signTime: now.Add(-2 * time.Minute), verifyKey: key, verifyAud: "admin"},
		{name: "unknown claim", signKey: key, signAud: "admin", signTime: now, verifyKey: key, verifyAud: "admin", mutate: addUnknownClaim},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signer, _ := newSigner(test.signKey, test.signAud, 45*time.Second, func() time.Time { return test.signTime })
			assertion, _ := signer.Sign("admin")
			if test.mutate != nil {
				assertion = test.mutate(assertion)
			}
			verifier, _ := newVerifier(test.verifyKey, test.verifyAud, func() time.Time { return now })
			if _, err := verifier.Verify(context.Background(), assertion); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("Verify() = %v", err)
			}
		})
	}
}

func addUnknownClaim(assertion string) string {
	parts := strings.Split(assertion, ".")
	encoded, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var value map[string]any
	_ = json.Unmarshal(encoded, &value)
	value["role"] = "admin"
	encoded, _ = json.Marshal(value)
	// This deliberately leaves the old signature in place. It covers both
	// strict claim parsing and integrity failure without exposing a signing seam.
	return base64.RawURLEncoding.EncodeToString(encoded) + "." + parts[1]
}
