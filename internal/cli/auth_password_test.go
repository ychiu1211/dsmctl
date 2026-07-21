package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
)

func stubTerminals(t *testing.T, stdin, stdout bool) {
	t.Helper()
	previousIn, previousOut := stdinIsTerminal, stdoutIsTerminal
	stdinIsTerminal = func() bool { return stdin }
	stdoutIsTerminal = func() bool { return stdout }
	t.Cleanup(func() { stdinIsTerminal, stdoutIsTerminal = previousIn, previousOut })
}

func writePasswordTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.New()
	cfg.NAS["office"] = config.Profile{URL: "https://office.example.test:5001", Username: "automation"}
	cfg.DefaultNAS = "office"
	if err := config.NewStore(path).Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return path
}

func TestAuthPasswordRevealRefusesNonInteractiveOutput(t *testing.T) {
	stubTerminals(t, true, false)
	command := newAuthPasswordRevealCommand(&options{configPath: writePasswordTestConfig(t)})
	command.SetIn(strings.NewReader(""))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(nil)
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("Execute() error = %v, want interactive-terminal refusal", err)
	}
}

func TestAuthPasswordSetRequiresTerminalWithoutStdinFlag(t *testing.T) {
	stubTerminals(t, false, false)
	command := newAuthPasswordSetCommand(&options{configPath: writePasswordTestConfig(t)})
	command.SetIn(strings.NewReader("piped-password\n"))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(nil)
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--password-stdin") {
		t.Fatalf("Execute() error = %v, want --password-stdin guidance", err)
	}
}

func TestAuthPasswordSetRequiresAccountName(t *testing.T) {
	stubTerminals(t, true, true)
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.New()
	cfg.NAS["office"] = config.Profile{URL: "https://office.example.test:5001"}
	cfg.DefaultNAS = "office"
	if err := config.NewStore(path).Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	command := newAuthPasswordSetCommand(&options{configPath: path})
	command.SetIn(strings.NewReader(""))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs(nil)
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--account") {
		t.Fatalf("Execute() error = %v, want missing-account guidance", err)
	}
}

func TestReadPasswordFromStdinTrimsLineEndings(t *testing.T) {
	password, err := readPasswordFromStdin(strings.NewReader("secret-value\r\n"))
	if err != nil || password != "secret-value" {
		t.Fatalf("readPasswordFromStdin() = %q, %v", password, err)
	}
	if _, err := readPasswordFromStdin(strings.NewReader("\n")); err == nil {
		t.Fatal("readPasswordFromStdin() accepted an empty password")
	}
}

func TestPasswordSourceLabel(t *testing.T) {
	if got := passwordSourceLabel(application.AuthStatus{PasswordStored: true}); got != "stored" {
		t.Fatalf("stored label = %q", got)
	}
	if got := passwordSourceLabel(application.AuthStatus{PasswordEnv: "DSMCTL_PASSWORD_OFFICE", PasswordEnvSet: true}); got != "env:DSMCTL_PASSWORD_OFFICE" {
		t.Fatalf("env label = %q", got)
	}
	if got := passwordSourceLabel(application.AuthStatus{PasswordEnv: "DSMCTL_PASSWORD_OFFICE"}); got != "none" {
		t.Fatalf("none label = %q", got)
	}
}

func TestResolveCredentialProfileNameKeepsExplicitOrphan(t *testing.T) {
	path := writePasswordTestConfig(t)
	name, err := resolveCredentialProfileName(&options{configPath: path, nas: "retired"})
	if err != nil || name != "retired" {
		t.Fatalf("resolveCredentialProfileName(retired) = %q, %v", name, err)
	}
	name, err = resolveCredentialProfileName(&options{configPath: path})
	if err != nil || name != "office" {
		t.Fatalf("resolveCredentialProfileName(default) = %q, %v", name, err)
	}
	if _, err := resolveCredentialProfileName(&options{configPath: path, nas: "-bad-"}); err == nil {
		t.Fatal("resolveCredentialProfileName accepted an invalid explicit name")
	}
}
