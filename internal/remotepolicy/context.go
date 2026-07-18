// Package remotepolicy contains transport-neutral identity and admission
// metadata for requests that entered through the remote gateway. Local CLI and
// stdio calls deliberately carry no Principal and keep their existing policy.
package remotepolicy

import (
	"context"
	"errors"
	"time"
)

const (
	ScopeRead  = "nas.read"
	ScopePlan  = "nas.plan"
	ScopeApply = "nas.apply"
	ScopeAdmin = "nas.admin"
)

var ErrDenied = errors.New("remote request is not authorized")

type Principal struct {
	TokenID string
	Name    string
	Scopes  map[string]struct{}
	NAS     map[string]struct{}
}

// AuditEvent intentionally has a closed, scalar schema: callers cannot attach
// request bodies, headers, credentials, DSM responses, or ciphertext.
type AuditEvent struct {
	ID            string    `json:"id"`
	Time          time.Time `json:"time"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	ActorType     string    `json:"actor_type"`
	ActorID       string    `json:"actor_id,omitempty"`
	Action        string    `json:"action"`
	Tool          string    `json:"tool,omitempty"`
	NAS           string    `json:"nas,omitempty"`
	Outcome       string    `json:"outcome"`
	Reason        string    `json:"reason,omitempty"`
}

type Auditor interface {
	AppendAudit(context.Context, AuditEvent) error
}

func (p Principal) HasScope(scope string) bool {
	_, ok := p.Scopes[scope]
	return ok
}

func (p Principal) AllowsNAS(name string) bool {
	_, ok := p.NAS[name]
	return ok
}

type principalKey struct{}
type correlationKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok && principal.TokenID != ""
}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}
