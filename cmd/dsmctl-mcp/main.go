package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/buildinfo"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/mcpserver"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func main() {
	configPath := flag.String("config", config.DefaultPath(), "configuration file path")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Fprintf(os.Stdout, "dsmctl-mcp %s\n", buildinfo.Version)
		return
	}

	cfg, err := config.NewStore(*configPath).Load()
	if err != nil {
		fatal(err)
	}
	secrets := credentials.NewSecureStore()
	// Prefer the stored web-login session, exactly like the CLI. The password
	// resolver stays as the automation fallback (environment variable, or a
	// password stored by an older release).
	manager := runtime.NewManager(cfg, secrets,
		runtime.WithDeviceStore(secrets),
		runtime.WithSessionStore(secrets),
	)
	service := application.NewService(cfg, manager,
		application.WithCredentialStore(secrets),
		application.WithDiscoveryStore(application.DiscoveryStorePath(*configPath)),
	)
	server := mcpserver.New(service, buildinfo.Version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	runErr := server.Run(ctx, &mcp.StdioTransport{})
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	closeErr := service.Close(closeCtx)
	if runErr != nil {
		fatal(runErr)
	}
	if closeErr != nil {
		fmt.Fprintln(os.Stderr, "dsmctl-mcp: close sessions:", closeErr)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "dsmctl-mcp:", err)
	os.Exit(1)
}
