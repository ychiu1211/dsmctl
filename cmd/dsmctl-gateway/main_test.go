package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/gateway"
	gatewaystate "github.com/ychiu1211/dsmctl/internal/gateway/state"
)

func TestLocalReadinessDetectsInvalidConfigAndSecret(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	tokenPath := filepath.Join(directory, "token")
	token := "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(configPath, []byte(`{"nas":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	ready := localReadiness(configPath, tokenPath, gateway.DevelopmentTokenDigest(token))
	if err := ready(context.Background()); err != nil {
		t.Fatalf("ready() error = %v", err)
	}

	if err := os.WriteFile(configPath, []byte(`{"nas":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ready(context.Background()); err == nil {
		t.Fatal("ready() accepted invalid config")
	}
	if err := os.WriteFile(configPath, []byte(`{"nas":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	if err := ready(context.Background()); err == nil {
		t.Fatal("ready() accepted missing token")
	}
}

func TestParsePrefixesRejectsAddressesWithoutMask(t *testing.T) {
	if _, err := parsePrefixes([]string{"127.0.0.1"}); err == nil {
		t.Fatal("parsePrefixes() accepted an address without CIDR mask")
	}
	prefixes, err := parsePrefixes([]string{"127.0.0.1/32", "::1/128"})
	if err != nil {
		t.Fatalf("parsePrefixes() error = %v", err)
	}
	if len(prefixes) != 2 {
		t.Fatalf("len(prefixes) = %d", len(prefixes))
	}
}

func TestManagedReadinessRequiresBootstrapAndMountedKeys(t *testing.T) {
	directory := t.TempDir()
	masterPath := filepath.Join(directory, "master.key")
	masterKey := bytes.Repeat([]byte{5}, 32)
	if err := os.WriteFile(masterPath, masterKey, 0o600); err != nil {
		t.Fatal(err)
	}
	repository, err := gatewaystate.Open(filepath.Join(directory, "gateway.db"), masterKey)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	bootstrap := "bootstrap-token-0123456789abcdef0123456789"
	if err := repository.ConfigureBootstrap(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}
	ready := managedReadiness(repository, masterPath, sha256.Sum256(masterKey))
	if err := ready(context.Background()); err == nil {
		t.Fatal("managed readiness accepted an unbootstrapped administrator")
	}
	if _, err := repository.EstablishAdministrator(context.Background(), bootstrap); err != nil {
		t.Fatal(err)
	}
	if err := ready(context.Background()); err != nil {
		t.Fatalf("managed readiness = %v", err)
	}
	if err := os.WriteFile(masterPath, bytes.Repeat([]byte{6}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ready(context.Background()); err == nil {
		t.Fatal("managed readiness accepted a changed master key file")
	}
}
