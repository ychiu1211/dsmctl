package credentials

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var environmentReferenceName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type ReferenceResolver interface {
	ResolveSecret(ctx context.Context, reference string) (string, error)
}

type EnvironmentReferenceResolver struct{}

func NewEnvironmentReferenceResolver() *EnvironmentReferenceResolver {
	return &EnvironmentReferenceResolver{}
}

// ResolveSecret deliberately supports references rather than literal secret
// values. The first implementation accepts env:NAME so CLI automation and MCP
// hosts can inject a password without transporting it in a tool argument or
// persisting it in a plan.
func (*EnvironmentReferenceResolver) ResolveSecret(ctx context.Context, reference string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	reference = strings.TrimSpace(reference)
	if !strings.HasPrefix(reference, "env:") {
		return "", errors.New("credential reference must use env:NAME")
	}
	name := strings.TrimPrefix(reference, "env:")
	if !environmentReferenceName.MatchString(name) {
		return "", errors.New("credential environment variable name is invalid")
	}
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return "", fmt.Errorf("credential environment variable %s is unavailable or empty", name)
	}
	return value, nil
}
