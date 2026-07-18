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

type fakeCredentialStore struct{}

func (fakeCredentialStore) HasPassword(context.Context, string) (bool, error) { return false, nil }

func (fakeCredentialStore) HasTrustedDevice(context.Context, string) (bool, error) {
	return false, nil
}

func (fakeCredentialStore) DeleteSession(context.Context, string) (bool, error) {
	return false, nil
}

func (fakeCredentialStore) PasswordEnvironment(profileName string, _ config.Profile) (string, bool) {
	return credentials.DefaultEnvironmentVariable(profileName), false
}

func (fakeCredentialStore) SessionMeta(context.Context, string) (credentials.SessionMeta, error) {
	return credentials.SessionMeta{}, nil
}

func TestNewRegistersToolSchemas(t *testing.T) {
	cfg := config.New()
	manager := runtime.NewManager(cfg, credentials.NewEnvironment())
	service := application.NewService(cfg, manager, application.WithCredentialStore(fakeCredentialStore{}))
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
	if len(tools.Tools) != 49 {
		t.Fatalf("ListTools() returned %d tools, want 49", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if scope, ok := ToolScope(tool.Name); !ok || scope == "" {
			t.Errorf("MCP tool %q has no enforceable remote authorization class", tool.Name)
		}
	}
	readOnlyTools := map[string]bool{
		"discover_lan_devices":                false,
		"explain_effective_access":            false,
		"get_auth_status":                     false,
		"get_logs":                            false,
		"get_log_capabilities":                false,
		"get_account_capabilities":            false,
		"get_account_state":                   false,
		"get_control_panel_time_capabilities": false,
		"get_control_panel_time_state":        false,
		"get_file_service_capabilities":       false,
		"get_smb_state":                       false,
		"get_nfs_state":                       false,
		"get_san_capabilities":                false,
		"get_san_state":                       false,
		"get_share_capabilities":              false,
		"get_share_state":                     false,
		"get_storage_capabilities":            false,
		"get_storage_state":                   false,
		"plan_account_change":                 false,
		"plan_control_panel_time_change":      false,
		"plan_san_change":                     false,
		"plan_share_change":                   false,
		"plan_storage_change":                 false,
		"plan_file_service_change":            false,
		"get_package_capabilities":            false,
		"get_package_state":                   false,
		"get_package_settings":                false,
		"plan_package_change":                 false,
		"get_resource_monitor_capabilities":   false,
		"get_resource_monitor_state":          false,
		"get_resource_monitor_history":        false,
		"get_resource_monitor_setting":        false,
		"plan_resource_recording_change":      false,
		"get_drive_admin_capabilities":        false,
		"get_drive_admin_status":              false,
		"get_drive_admin_connections":         false,
		"get_drive_admin_team_folders":        false,
		"get_drive_admin_logs":                false,
	}
	mutationTools := map[string]bool{
		"apply_account_plan":            false,
		"apply_control_panel_time_plan": false,
		"apply_san_plan":                false,
		"apply_share_plan":              false,
		"apply_storage_plan":            false,
		"apply_file_service_plan":       false,
		"apply_package_plan":            false,
		"apply_resource_recording_plan": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := mutationTools[tool.Name]; ok {
			if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.IdempotentHint || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Errorf("tool %s mutation annotations = %#v", tool.Name, tool.Annotations)
			}
			mutationTools[tool.Name] = true
			continue
		}
		if _, ok := readOnlyTools[tool.Name]; !ok {
			continue
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint {
			t.Errorf("tool %s annotations = %#v", tool.Name, tool.Annotations)
		}
		readOnlyTools[tool.Name] = true
	}
	for name, found := range readOnlyTools {
		if !found {
			t.Errorf("tool %s was not registered", name)
		}
	}
	for name, found := range mutationTools {
		if !found {
			t.Errorf("tool %s was not registered", name)
		}
	}
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_nas", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(list_nas) error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool(list_nas) returned tool error: %#v", result.Content)
	}
	authStatus, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_auth_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(get_auth_status) error = %v", err)
	}
	if authStatus.IsError {
		t.Fatalf("CallTool(get_auth_status) returned tool error: %#v", authStatus.Content)
	}
}
