package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func TestNewReadOnlyOmitsPlanAndApplyTools(t *testing.T) {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := application.NewService(cfg, manager, application.WithCredentialStore(fakeCredentialStore{}))
	server := NewReadOnly(service, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("read-only server registered no tools")
	}
	// Content-exfiltration reads are stripped even though they are get_* reads:
	// they would stream file/thumbnail bytes to a remote caller.
	exfiltration := map[string]bool{
		"get_filestation_file_content": false,
		"get_filestation_thumbnail":    false,
		// Certificate export extracts private-key material to a local file.
		"get_certificate_export": false,
	}
	// Actions that are neither plan_ nor apply_ prefixed but still mutate or
	// load the NAS must be explicitly stripped from the read-only gateway.
	strippedActions := map[string]bool{
		"run_security_scan": false,
	}
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "plan_") || strings.HasPrefix(tool.Name, "apply_") {
			t.Errorf("read-only server registered %q", tool.Name)
		}
		if tool.Name == "discover_lan_devices" {
			t.Errorf("read-only gateway surface must not expose LAN discovery (%q)", tool.Name)
		}
		if _, ok := exfiltration[tool.Name]; ok {
			t.Errorf("read-only gateway must not expose content transfer (%q)", tool.Name)
		}
		if _, ok := strippedActions[tool.Name]; ok {
			t.Errorf("read-only gateway must not expose the NAS action (%q)", tool.Name)
		}
	}
}

func TestNewExposesLANDiscovery(t *testing.T) {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := application.NewService(cfg, manager, application.WithCredentialStore(fakeCredentialStore{}))
	server := New(service, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	found := false
	for _, tool := range tools.Tools {
		if tool.Name == "discover_lan_devices" {
			found = true
			break
		}
	}
	if !found {
		t.Error("local MCP server must expose discover_lan_devices")
	}
}
