package cli

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

// credentialResolver resolves the account password from the profile's
// environment variable (for example DSMCTL_PASSWORD_LAB). The runtime prefers a
// stored web-login session and only reaches this resolver when no session
// exists or a seeded session can no longer be resumed, so it acts as an
// automatic, non-interactive fallback: dsmctl re-authenticates with the
// environment password instead of forcing a browser sign-in. When no password
// is available it declines with a message pointing at both recovery paths.
type credentialResolver struct {
	env *credentials.Environment
}

func (r credentialResolver) Password(ctx context.Context, profileName string, profile config.Profile) (string, error) {
	password, err := r.env.Password(ctx, profileName, profile)
	if err == nil {
		return password, nil
	}
	varName, _ := r.env.Status(profileName, profile)
	return "", fmt.Errorf("not signed in to NAS %q and no password available; run 'dsmctl auth login --nas %s' or set %s", profileName, profileName, varName)
}

func loadService(configPath string) (*application.Service, error) {
	cfg, err := config.NewStore(configPath).Load()
	if err != nil {
		return nil, err
	}
	secrets := credentials.NewSecureStore()
	manager := runtime.NewManager(
		cfg,
		credentialResolver{env: credentials.NewEnvironment()},
		runtime.WithDeviceStore(secrets),
		runtime.WithSessionStore(secrets),
	)
	return application.NewService(cfg, manager, application.WithCredentialStore(secrets)), nil
}
