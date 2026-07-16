package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func promptSecret(cmd *cobra.Command, label string) (string, error) {
	input, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return "", errors.New("secret input requires an interactive terminal")
	}
	if _, err := fmt.Fprint(cmd.ErrOrStderr(), label); err != nil {
		return "", err
	}
	secret, err := term.ReadPassword(int(input.Fd()))
	_, _ = fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	value := string(secret)
	for i := range secret {
		secret[i] = 0
	}
	if value == "" {
		return "", errors.New("secret cannot be empty")
	}
	return value, nil
}

func terminalOTPProvider(cmd *cobra.Command) runtime.OTPProvider {
	return func(ctx context.Context, profileName string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return promptSecret(cmd, fmt.Sprintf("One-time password for NAS %q: ", profileName))
	}
}
