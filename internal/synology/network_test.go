package synology

import (
	"fmt"
	"testing"

	netops "github.com/ychiu1211/dsmctl/internal/synology/operations/network"
)

// TestIsRouteNotConfigured proves the facade recognizes the DSM "advanced routing
// not configured" signal (code 4302) — even wrapped by the operation framework's
// error chain — and does not mistake an ordinary API error for it. This is what
// lets NetworkRoutes return an empty, not-configured table instead of failing the
// module on a NAS (like the lab) with no static routes.
func TestIsRouteNotConfigured(t *testing.T) {
	notConfigured := &APIError{API: netops.StaticRouteAPIName, Method: "get", Code: netops.RouteFeatureNotConfiguredCode}
	wrapped := fmt.Errorf("execute network.routes.read: call %s.get: %w", netops.StaticRouteAPIName, notConfigured)
	if !isRouteNotConfigured(wrapped) {
		t.Fatalf("wrapped 4302 APIError should be recognized as not-configured")
	}

	other := fmt.Errorf("call %s.get: %w", netops.StaticRouteAPIName, &APIError{API: netops.StaticRouteAPIName, Method: "get", Code: 119})
	if isRouteNotConfigured(other) {
		t.Fatalf("a non-4302 APIError must not be treated as not-configured")
	}
	if isRouteNotConfigured(fmt.Errorf("plain transport failure")) {
		t.Fatalf("a non-API error must not be treated as not-configured")
	}
}
