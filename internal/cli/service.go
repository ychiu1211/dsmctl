package cli

import (
	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func loadService(opts *options) (*application.Service, error) {
	cfg, err := config.NewStore(opts.configPath).Load()
	if err != nil {
		return nil, err
	}
	// The SecureStore is the runtime's password resolver. It resolves the account
	// password keyring-first — including one stored by 'dsmctl provision' or
	// 'dsmctl auth password set' — and falls back to the profile's password
	// environment variable, so a provisioned NAS is usable by every command
	// without a separate 'dsmctl auth login'. The runtime still prefers a
	// resumable web-login session and only consults this resolver when no session
	// exists or a seeded one can no longer be resumed.
	secrets := credentials.NewSecureStore()
	managerOptions := []runtime.Option{
		runtime.WithDeviceStore(secrets),
		runtime.WithSessionStore(secrets),
	}
	if logger := buildLogger(opts.logLevel); logger != nil {
		managerOptions = append(managerOptions, runtime.WithLogger(logger))
	}
	manager := runtime.NewManager(cfg, secrets, managerOptions...)
	return application.NewService(cfg, manager,
		application.WithCredentialStore(secrets),
		application.WithDiscoveryStore(application.DiscoveryStorePath(opts.configPath)),
	), nil
}
