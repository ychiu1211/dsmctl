package cli

import (
	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func loadService(configPath string, otpProvider runtime.OTPProvider) (*application.Service, error) {
	cfg, err := config.NewStore(configPath).Load()
	if err != nil {
		return nil, err
	}
	secrets := credentials.NewSecureStore()
	manager := runtime.NewManager(
		cfg,
		secrets,
		runtime.WithDeviceStore(secrets),
		runtime.WithOTPProvider(otpProvider),
	)
	return application.NewService(cfg, manager), nil
}
