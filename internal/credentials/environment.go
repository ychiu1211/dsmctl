package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/ychiu1211/dsmctl/internal/config"
)

type Resolver interface {
	Password(ctx context.Context, profileName string, profile config.Profile) (string, error)
}

type Environment struct {
	lookup func(string) (string, bool)
}

func NewEnvironment() *Environment {
	return &Environment{lookup: os.LookupEnv}
}

func DefaultEnvironmentVariable(profileName string) string {
	var builder strings.Builder
	builder.WriteString("DSMCTL_PASSWORD_")
	for _, r := range profileName {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToUpper(r))
		} else {
			builder.WriteRune('_')
		}
	}
	return builder.String()
}

// Status returns the variable name Password would consult for this profile
// and whether it is currently set to a non-empty value. The value itself is
// never returned.
func (e *Environment) Status(profileName string, profile config.Profile) (string, bool) {
	name := profile.PasswordEnv
	if name == "" {
		name = DefaultEnvironmentVariable(profileName)
	}
	value, ok := e.lookup(name)
	return name, ok && value != ""
}

func (e *Environment) Password(ctx context.Context, profileName string, profile config.Profile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	name := profile.PasswordEnv
	if name == "" {
		name = DefaultEnvironmentVariable(profileName)
	}
	password, ok := e.lookup(name)
	if !ok || password == "" {
		return "", fmt.Errorf("password for NAS %q is unavailable; set environment variable %s", profileName, name)
	}
	return password, nil
}
