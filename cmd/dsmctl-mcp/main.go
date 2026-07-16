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
	flag.Parse()

	cfg, err := config.NewStore(*configPath).Load()
	if err != nil {
		fatal(err)
	}
	secrets := credentials.NewSecureStore()
	manager := runtime.NewManager(cfg, secrets, runtime.WithDeviceStore(secrets))
	service := application.NewService(cfg, manager)
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
