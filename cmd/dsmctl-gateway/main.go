package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/buildinfo"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/gateway"
	"github.com/ychiu1211/dsmctl/internal/gateway/admin"
	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
	gatewaystate "github.com/ychiu1211/dsmctl/internal/gateway/state"
	"github.com/ychiu1211/dsmctl/internal/mcpserver"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck(os.Args[2:]))
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	flags := flag.NewFlagSet("dsmctl-gateway", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", config.DefaultPath(), "configuration file path")
	statePath := flags.String("state", "", "managed gateway state database path; enables dynamic administration")
	masterKeyPath := flags.String("master-key-file", "", "32-byte managed gateway vault key file")
	bootstrapPath := flags.String("bootstrap-file", "", "one-time generic Linux administrator bootstrap token file")
	platformAssertionKeyPath := flags.String("platform-assertion-key-file", "", "Synology platform administrator assertion key file; disables generic bootstrap")
	platformAssertionAudience := flags.String("platform-assertion-audience", platformauth.DefaultAudience, "required audience for Synology administrator assertions")
	adminPublicURL := flags.String("admin-public-url", "", "public gateway origin used as the DSM web-login opener")
	listenAddress := flags.String("listen", "127.0.0.1:18765", "HTTP listen address")
	tokenPath := flags.String("dev-read-only-token-file", "", "required local bearer-token file for explicit read-only developer mode")
	allowedHosts := flags.String("allowed-hosts", "localhost,127.0.0.1,::1", "comma-separated allowed HTTP Host names or addresses")
	allowedOrigins := flags.String("allowed-origins", "", "comma-separated browser origins; requests without Origin remain allowed")
	trustedProxies := flags.String("trusted-proxies", "", "comma-separated trusted reverse-proxy CIDR prefixes")
	maxConcurrent := flags.Int("max-concurrent", 8, "maximum concurrent MCP requests")
	maxBodyBytes := flags.Int64("max-body-bytes", 1<<20, "maximum MCP request body size")
	shutdownTimeout := flags.Duration("shutdown-timeout", 10*time.Second, "HTTP drain and DSM session close timeout")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *maxConcurrent < 1 {
		return errors.New("max-concurrent must be at least 1")
	}
	if *maxBodyBytes < 1 {
		return errors.New("max-body-bytes must be at least 1")
	}

	managed := strings.TrimSpace(*statePath) != ""
	platformManaged := strings.TrimSpace(*platformAssertionKeyPath) != ""
	if platformManaged && !managed {
		return errors.New("platform assertion authentication requires managed gateway state")
	}
	if platformManaged && strings.TrimSpace(*bootstrapPath) != "" {
		return errors.New("platform assertion authentication cannot be combined with generic bootstrap")
	}
	var token string
	var tokenDigest [sha256.Size]byte
	var err error
	if !managed {
		token, err = gateway.ReadDevelopmentToken(*tokenPath)
		if err != nil {
			return err
		}
		tokenDigest = gateway.DevelopmentTokenDigest(token)
	}
	proxies, err := parsePrefixes(splitCSV(*trustedProxies))
	if err != nil {
		return err
	}

	var (
		cfg              *config.Config
		manager          *runtime.Manager
		service          *application.Service
		adminHandler     http.Handler
		ready            func(context.Context) error
		closeState       func() error
		mode             string
		repository       *gatewaystate.Repository
		platformVerifier *platformauth.Verifier
		platformDigest   [sha256.Size]byte
	)
	if managed {
		masterKey, err := gatewaystate.ReadMasterKey(*masterKeyPath)
		if err != nil {
			return err
		}
		masterDigest := sha256.Sum256(masterKey)
		repository, err = gatewaystate.Open(*statePath, masterKey)
		for index := range masterKey {
			masterKey[index] = 0
		}
		if err != nil {
			return err
		}
		closeState = repository.Close
		if platformManaged {
			platformKey, err := platformauth.ReadKey(*platformAssertionKeyPath)
			if err != nil {
				_ = repository.Close()
				return err
			}
			platformDigest = sha256.Sum256(platformKey)
			platformVerifier, err = platformauth.NewVerifier(platformKey, *platformAssertionAudience)
			for index := range platformKey {
				platformKey[index] = 0
			}
			if err != nil {
				_ = repository.Close()
				return err
			}
			if err := repository.EnablePlatformAdministration(context.Background()); err != nil {
				_ = repository.Close()
				return err
			}
		} else {
			health, err := repository.Health(context.Background())
			if err != nil {
				_ = repository.Close()
				return err
			}
			if !health.Initialized {
				bootstrap, err := readBootstrapToken(*bootstrapPath)
				if err != nil {
					_ = repository.Close()
					return err
				}
				if err := repository.ConfigureBootstrap(context.Background(), bootstrap); err != nil {
					_ = repository.Close()
					return err
				}
			}
		}
		cfg, err = repository.Snapshot(context.Background())
		if err != nil {
			_ = repository.Close()
			return err
		}
		manager = runtime.NewManager(cfg, repository,
			runtime.WithConfigSource(repository),
			runtime.WithDeviceStore(repository),
			runtime.WithSessionStore(repository),
		)
		service = application.NewService(cfg, manager,
			application.WithConfigSource(repository),
			application.WithCredentialStore(repository),
			application.WithSecretReferenceResolver(repository),
			application.WithRemoteApplyAuthorizer(repository),
		)
		adminApplication, err := admin.New(admin.Options{Repository: repository, Manager: manager, PublicURL: *adminPublicURL, PlatformVerifier: platformVerifier})
		if err != nil {
			_ = service.Close(context.Background())
			_ = repository.Close()
			return err
		}
		adminHandler = adminApplication
		ready = managedReadiness(repository, *masterKeyPath, masterDigest)
		mode = "managed"
		if platformManaged {
			ready = platformReadiness(repository, *masterKeyPath, masterDigest, *platformAssertionKeyPath, platformDigest)
			mode = "managed-synology"
		}
	} else {
		cfg, err = loadRequiredConfig(*configPath)
		if err != nil {
			return err
		}
		secrets := gateway.NewEnvironmentCredentials()
		manager = runtime.NewManager(cfg, secrets)
		service = application.NewService(cfg, manager, application.WithCredentialStore(secrets))
		ready = localReadiness(*configPath, *tokenPath, tokenDigest)
		mode = "development-read-only"
	}
	var mcpServer = mcpserver.NewReadOnly(service, buildinfo.Version)
	if managed {
		mcpServer = mcpserver.NewRemote(service, buildinfo.Version, repository)
	}
	gatewayOptions := gateway.Options{
		MCPServer:      mcpServer,
		AdminHandler:   adminHandler,
		BearerToken:    token,
		AllowedHosts:   splitCSV(*allowedHosts),
		AllowedOrigins: splitCSV(*allowedOrigins),
		TrustedProxies: proxies,
		MaxBodyBytes:   *maxBodyBytes,
		MaxConcurrent:  *maxConcurrent,
		Ready:          ready,
		Close: func(ctx context.Context) error {
			serviceErr := service.Close(ctx)
			var stateErr error
			if closeState != nil {
				stateErr = closeState()
			}
			return errors.Join(serviceErr, stateErr)
		},
		Logger:          logger,
		ShutdownTimeout: *shutdownTimeout,
	}
	if managed {
		gatewayOptions.MCPAuthenticator = repository
		gatewayOptions.MCPAuditor = repository
	}
	httpServer, err := gateway.New(gatewayOptions)
	if err != nil {
		_ = service.Close(context.Background())
		if closeState != nil {
			_ = closeState()
		}
		return err
	}
	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		_ = service.Close(context.Background())
		if closeState != nil {
			_ = closeState()
		}
		return fmt.Errorf("listen on %s: %w", *listenAddress, err)
	}
	logger.Info("gateway listening",
		"address", listener.Addr().String(),
		"version", buildinfo.Version,
		"mode", mode,
		"profiles", len(cfg.NAS),
	)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return httpServer.Serve(ctx, listener)
}

func managedReadiness(repository *gatewaystate.Repository, masterKeyPath string, expectedMasterKey [sha256.Size]byte) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := repository.Ready(ctx); err != nil {
			return err
		}
		masterKey, err := gatewaystate.ReadMasterKey(masterKeyPath)
		if err != nil {
			return err
		}
		actualMasterKey := sha256.Sum256(masterKey)
		for index := range masterKey {
			masterKey[index] = 0
		}
		if subtle.ConstantTimeCompare(expectedMasterKey[:], actualMasterKey[:]) != 1 {
			return errors.New("master key file changed; restart the gateway")
		}
		return nil
	}
}

func platformReadiness(repository *gatewaystate.Repository, masterKeyPath string, expectedMasterKey [sha256.Size]byte, platformKeyPath string, expectedPlatformKey [sha256.Size]byte) func(context.Context) error {
	managedReady := managedReadiness(repository, masterKeyPath, expectedMasterKey)
	return func(ctx context.Context) error {
		if err := managedReady(ctx); err != nil {
			return err
		}
		platformKey, err := platformauth.ReadKey(platformKeyPath)
		if err != nil {
			return err
		}
		actualPlatformKey := sha256.Sum256(platformKey)
		for index := range platformKey {
			platformKey[index] = 0
		}
		if subtle.ConstantTimeCompare(expectedPlatformKey[:], actualPlatformKey[:]) != 1 {
			return errors.New("platform assertion key file changed; restart the gateway")
		}
		return nil
	}
}

func readBootstrapToken(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("bootstrap file is required until the first gateway administrator is established")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read bootstrap file: %w", err)
	}
	if len(data) > 4096 {
		return "", errors.New("bootstrap file is too large")
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return "", errors.New("bootstrap token must be at least 32 bytes and contain no whitespace")
	}
	return token, nil
}

func localReadiness(configPath, tokenPath string, expectedToken [32]byte) func(context.Context) error {
	return func(context.Context) error {
		if _, err := loadRequiredConfig(configPath); err != nil {
			return err
		}
		current, err := gateway.ReadDevelopmentToken(tokenPath)
		if err != nil {
			return err
		}
		if !gateway.DevelopmentTokenMatches(expectedToken, current) {
			return errors.New("development token file changed; restart the gateway")
		}
		return nil
	}
}

func loadRequiredConfig(path string) (*config.Config, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("required gateway config %s: %w", path, err)
	}
	cfg, err := config.NewStore(path).Load()
	if err != nil {
		return nil, err
	}
	if err := gateway.ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("validate gateway config %s: %w", path, err)
	}
	return cfg, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func parsePrefixes(values []string) ([]netip.Prefix, error) {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q must be a CIDR prefix: %w", value, err)
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func runHealthcheck(arguments []string) int {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	endpoint := flags.String("url", "http://127.0.0.1:18765/healthz", "health endpoint URL")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *endpoint, nil)
	if err != nil {
		return 1
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(req)
	if err != nil {
		return 1
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
