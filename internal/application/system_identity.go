package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// serverNameHostPattern is the DSM server-name (hostname) grammar: 1–63
// characters, letters/digits/hyphen, not starting or ending with a hyphen.
var serverNameHostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

type serverNameClient interface {
	GetServerName(context.Context) (string, error)
	SetServerName(context.Context, string) (string, error)
}

// ServerNameResult reports a server-name (hostname) change.
type ServerNameResult struct {
	NAS        string `json:"nas" jsonschema:"NAS profile used for the change"`
	Previous   string `json:"previous" jsonschema:"Server name observed before the change"`
	ServerName string `json:"server_name" jsonschema:"Server name persisted and verified after the change"`
}

// SetServerName sets the DSM server name (hostname) and verifies the change by
// re-reading it. It validates the name syntactically, refuses a no-op, and fails
// closed if DSM does not report the requested name after the set.
func (s *Service) SetServerName(ctx context.Context, requestedNAS, name string) (ServerNameResult, error) {
	name = strings.TrimSpace(name)
	if err := validateServerName(name); err != nil {
		return ServerNameResult{}, err
	}
	nas, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return ServerNameResult{}, err
	}
	client, ok := generic.(serverNameClient)
	if !ok {
		return ServerNameResult{}, fmt.Errorf("NAS client does not implement server-name management")
	}
	before, err := client.GetServerName(ctx)
	if err != nil {
		return ServerNameResult{}, authenticationError(nas, err)
	}
	if strings.EqualFold(strings.TrimSpace(before), name) {
		return ServerNameResult{}, fmt.Errorf("server name is already %q", name)
	}
	after, err := client.SetServerName(ctx, name)
	if err != nil {
		return ServerNameResult{}, authenticationError(nas, err)
	}
	if strings.TrimSpace(after) != name {
		return ServerNameResult{}, fmt.Errorf("server name is %q after the set, want %q", after, name)
	}
	return ServerNameResult{NAS: nas, Previous: before, ServerName: after}, nil
}

func validateServerName(name string) error {
	if name == "" {
		return fmt.Errorf("server name must not be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("server name %q exceeds 63 characters", name)
	}
	if !serverNameHostPattern.MatchString(name) {
		return fmt.Errorf("server name %q must be 1–63 letters, digits, or hyphens and may not start or end with a hyphen", name)
	}
	return nil
}
