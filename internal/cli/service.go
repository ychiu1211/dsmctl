package cli

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

// webLoginResolver is the credential resolver used now that web login is the
// only sign-in method. The runtime prefers a stored web-login session; this
// resolver is only reached when no session exists, so it declines with an
// actionable message instead of asking for a password.
type webLoginResolver struct{}

func (webLoginResolver) Password(_ context.Context, profileName string, _ config.Profile) (string, error) {
	return "", fmt.Errorf("not signed in to NAS %q; run 'dsmctl auth login --nas %s'", profileName, profileName)
}

func loadService(configPath string, otpProvider runtime.OTPProvider) (*application.Service, error) {
	cfg, err := config.NewStore(configPath).Load()
	if err != nil {
		return nil, err
	}
	secrets := credentials.NewSecureStore()
	manager := runtime.NewManager(
		cfg,
		webLoginResolver{},
		runtime.WithDeviceStore(secrets),
		runtime.WithSessionStore(secrets),
		runtime.WithOTPProvider(otpProvider),
	)
	return application.NewService(cfg, manager, application.WithCredentialStore(secrets)), nil
}
