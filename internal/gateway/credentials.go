package gateway

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
)

// EnvironmentCredentials adapts environment-only credentials to the
// application status interface. The gateway does not depend on a desktop
// keyring or D-Bus service.
type EnvironmentCredentials struct {
	environment *credentials.Environment
}

func NewEnvironmentCredentials() *EnvironmentCredentials {
	return &EnvironmentCredentials{environment: credentials.NewEnvironment()}
}

func (c *EnvironmentCredentials) Password(ctx context.Context, profileName string, profile config.Profile) (string, error) {
	return c.environment.Password(ctx, profileName, profile)
}

func (*EnvironmentCredentials) HasPassword(context.Context, string) (bool, error) {
	return false, nil
}

func (*EnvironmentCredentials) HasTrustedDevice(context.Context, string) (bool, error) {
	return false, nil
}

func (*EnvironmentCredentials) DeleteSession(context.Context, string) (bool, error) {
	return false, nil
}

func (c *EnvironmentCredentials) PasswordEnvironment(profileName string, profile config.Profile) (string, bool) {
	return c.environment.Status(profileName, profile)
}

func (*EnvironmentCredentials) SessionMeta(context.Context, string) (credentials.SessionMeta, error) {
	return credentials.SessionMeta{}, nil
}
