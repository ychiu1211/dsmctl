package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/derekvery666/dsmctl/internal/gateway"
	gatewaystate "github.com/derekvery666/dsmctl/internal/gateway/state"
)

func TestRecoveryDirectoryRequiresManagedState(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := run([]string{"--recovery-dir", filepath.Join(t.TempDir(), "backups")}, logger)
	if err == nil || !strings.Contains(err.Error(), "recovery directory requires managed gateway state") {
		t.Fatalf("run() error = %v", err)
	}
}

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

func TestResolveAdministratorModeFailsClosedForDSM(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		keyPath string
		want    administratorMode
		wantErr bool
	}{
		{name: "generic default", value: "auto", want: administratorModeLocal},
		{name: "legacy SPK auto detection", value: "auto", keyPath: "/run/secrets/dsm-sso.key", want: administratorModeDSM},
		{name: "explicit SPK", value: "dsm", keyPath: "/run/secrets/dsm-sso.key", want: administratorModeDSM},
		{name: "SPK missing assertion key", value: "dsm", wantErr: true},
		{name: "local with assertion key", value: "local", keyPath: "/run/secrets/dsm-sso.key", wantErr: true},
		{name: "unknown", value: "passwordless", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolveAdministratorMode(test.value, test.keyPath)
			if (err != nil) != test.wantErr {
				t.Fatalf("resolveAdministratorMode() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("resolveAdministratorMode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestManagedReadinessRequiresLocalAdministratorAndMountedKey(t *testing.T) {
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
	ready := managedReadiness(repository, masterPath, sha256.Sum256(masterKey), false)
	if err := ready(context.Background()); err == nil {
		t.Fatal("managed readiness accepted an uninitialized administrator")
	}
	if _, _, err := repository.CreateAdministrator(context.Background(), "owner", "correct horse battery staple"); err != nil {
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

func TestManagedReadinessAcceptsExternalAdministratorProvider(t *testing.T) {
	directory := t.TempDir()
	masterPath := filepath.Join(directory, "master.key")
	masterKey := bytes.Repeat([]byte{7}, 32)
	if err := os.WriteFile(masterPath, masterKey, 0o600); err != nil {
		t.Fatal(err)
	}
	repository, err := gatewaystate.Open(filepath.Join(directory, "gateway.db"), masterKey)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	ready := managedReadiness(repository, masterPath, sha256.Sum256(masterKey), true)
	if err := ready(context.Background()); err != nil {
		t.Fatalf("managed readiness with external provider = %v", err)
	}
}
