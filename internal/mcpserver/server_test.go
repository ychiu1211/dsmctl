package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
	"github.com/ychiu1211/dsmctl/internal/runtime"
)

func TestNewRegistersToolSchemas(t *testing.T) {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := application.NewService(cfg, manager)
	server := New(service, "test")
	if server == nil {
		t.Fatal("New() returned nil")
	}

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
	if len(tools.Tools) != 3 {
		t.Fatalf("ListTools() returned %d tools, want 3", len(tools.Tools))
	}
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_nas", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(list_nas) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(list_nas) returned tool error: %#v", result.Content)
	}
}
