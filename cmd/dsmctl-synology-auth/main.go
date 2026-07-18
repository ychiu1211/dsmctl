package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
	"github.com/ychiu1211/dsmctl/internal/synologyauth"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("Synology authentication adapter stopped", "error", err)
		os.Exit(1)
	}
}

func run(arguments []string, logger *slog.Logger) error {
	flags := flag.NewFlagSet("dsmctl-synology-auth", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:18766", "private loopback listen address")
	backendValue := flags.String("backend", "http://127.0.0.1:18765", "private gateway backend URL")
	keyPath := flags.String("assertion-key-file", "", "32-byte platform assertion key file")
	audience := flags.String("audience", platformauth.DefaultAudience, "platform assertion audience")
	authenticatePath := flags.String("authenticate-cgi", "/usr/syno/synoman/webman/modules/authenticate.cgi", "DSM session authenticator path")
	idPath := flags.String("id-command", "/usr/bin/id", "DSM identity group command path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	address, err := net.ResolveTCPAddr("tcp", *listen)
	if err != nil || address.IP == nil || !address.IP.IsLoopback() {
		return errors.New("Synology authentication adapter must listen on an explicit loopback address")
	}
	backend, err := url.Parse(*backendValue)
	if err != nil {
		return fmt.Errorf("parse gateway backend: %w", err)
	}
	key, err := platformauth.ReadKey(*keyPath)
	if err != nil {
		return err
	}
	signer, err := platformauth.NewSigner(key, *audience)
	for index := range key {
		key[index] = 0
	}
	if err != nil {
		return err
	}
	handler, err := synologyauth.New(synologyauth.Options{
		Backend: backend, Signer: signer, Logger: logger, RequireLoopback: true,
		Validator: synologyauth.CommandValidator{AuthenticatePath: *authenticatePath, IDPath: *idPath},
	})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *listen, err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
