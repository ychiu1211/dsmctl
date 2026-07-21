package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func TestProvisionToolScopeIsDistinct(t *testing.T) {
	for _, name := range []string{"provision_nas", "provision_discovered_nas", "install_discovered_nas"} {
		scope, known := ToolScope(name)
		if !known || scope != remotepolicy.ScopeProvision {
			t.Fatalf("ToolScope(%s) = %q, %v; want nas.provision", name, scope, known)
		}
		if scope == remotepolicy.ScopeApply {
			t.Fatalf("%s must not be classified as nas.apply", name)
		}
	}
}

func TestDiscoveredProvisionRequiresScopeButNotAllowlist(t *testing.T) {
	service := &application.Service{}
	call := func(scopes map[string]struct{}) (bool, error) {
		called := false
		next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
			called = true
			return &mcp.CallToolResult{}, nil
		}
		handler := remotePolicyMiddleware(service, nil)(next)
		principal := remotepolicy.Principal{TokenID: "t", Scopes: scopes, NAS: map[string]struct{}{}}
		ctx := remotepolicy.WithPrincipal(context.Background(), principal)
		request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "provision_discovered_nas", Arguments: json.RawMessage(`{"url":"https://10.0.0.9:5001","admin_user":"operator"}`)}}
		_, err := handler(ctx, "tools/call", request)
		return called, err
	}
	// Without the provision scope: denied, handler never invoked.
	if called, err := call(map[string]struct{}{remotepolicy.ScopeRead: {}}); called || err == nil {
		t.Fatalf("without nas.provision: called=%v err=%v (want denied)", called, err)
	}
	// With the provision scope: admitted even though the NAS allowlist is empty
	// (a discovered device is outside every allowlist by construction).
	if called, err := call(map[string]struct{}{remotepolicy.ScopeProvision: {}}); !called || err != nil {
		t.Fatalf("with nas.provision: called=%v err=%v (want admitted)", called, err)
	}
}

func TestReadOnlyGatewayStripsProvisionTool(t *testing.T) {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := application.NewService(cfg, manager, application.WithCredentialStore(fakeCredentialStore{}))
	server := NewReadOnly(service, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "provision_nas" || tool.Name == "provision_discovered_nas" || tool.Name == "install_discovered_nas" {
			t.Fatalf("read-only developer gateway must not expose %q", tool.Name)
		}
	}
}

func TestFullSurfaceExposesProvisionTool(t *testing.T) {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := application.NewService(cfg, manager, application.WithCredentialStore(fakeCredentialStore{}))
	server := New(service, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "provision_nas" {
			found = true
			if strings.Contains(strings.ToLower(tool.Description), "return") && strings.Contains(strings.ToLower(tool.Description), "password") {
				// Description must state the password is never returned.
				if !strings.Contains(tool.Description, "NEVER returned") {
					t.Fatalf("provision_nas description must state the password is never returned: %q", tool.Description)
				}
			}
		}
	}
	if !found {
		t.Fatal("full MCP surface must register provision_nas")
	}
}
