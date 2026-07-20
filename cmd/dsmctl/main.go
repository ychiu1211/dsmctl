package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/ychiu1211/dsmctl/internal/buildinfo"
	"github.com/ychiu1211/dsmctl/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli.Execute(ctx, buildinfo.Version); err != nil {
		fmt.Fprintln(os.Stderr, cli.FormatError(err))
		os.Exit(cli.ExitCode(err))
	}
}
