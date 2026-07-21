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
	if len(tools.Tools) != 222 {
		t.Fatalf("ListTools() returned %d tools, want 222", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		if scope, ok := ToolScope(tool.Name); !ok || scope == "" {
			t.Errorf("MCP tool %q has no enforceable remote authorization class", tool.Name)
		}
	}
	if scope, ok := ToolScope("discover_lan_devices"); !ok || scope != "lan.discover" {
		t.Fatalf("discover_lan_devices scope = %q, %v", scope, ok)
	}
	readOnlyTools := map[string]bool{
		"discover_lan_devices":                  false,
		"explain_effective_access":              false,
		"get_auth_status":                       false,
		"get_logs":                              false,
		"get_log_capabilities":                  false,
		"get_account_capabilities":              false,
		"get_account_state":                     false,
		"get_control_panel_time_capabilities":   false,
		"get_control_panel_time_state":          false,
		"get_file_service_capabilities":         false,
		"get_smb_state":                         false,
		"get_nfs_state":                         false,
		"get_nfs_export_capabilities":           false,
		"get_nfs_export_state":                  false,
		"get_service_discovery_capabilities":    false,
		"get_service_discovery_state":           false,
		"get_ftp_service_capabilities":          false,
		"get_ftp_service_state":                 false,
		"get_rsync_service_capabilities":        false,
		"get_rsync_service_state":               false,
		"get_tftp_service_capabilities":         false,
		"get_tftp_service_state":                false,
		"get_photos_capabilities":               false,
		"get_photos_settings":                   false,
		"get_office_capabilities":               false,
		"get_office_info":                       false,
		"get_office_settings":                   false,
		"get_office_preferences":                false,
		"get_office_fonts":                      false,
		"plan_office_change":                    false,
		"get_san_capabilities":                  false,
		"get_san_state":                         false,
		"get_share_capabilities":                false,
		"get_share_state":                       false,
		"get_storage_capabilities":              false,
		"get_storage_state":                     false,
		"plan_account_change":                   false,
		"plan_control_panel_time_change":        false,
		"plan_san_change":                       false,
		"plan_share_change":                     false,
		"plan_storage_change":                   false,
		"plan_file_service_change":              false,
		"plan_nfs_export_change":                false,
		"plan_service_discovery_change":         false,
		"plan_ftp_service_change":               false,
		"plan_rsync_service_change":             false,
		"plan_tftp_service_change":              false,
		"plan_photos_change":                    false,
		"get_package_capabilities":              false,
		"get_package_state":                     false,
		"get_package_settings":                  false,
		"plan_package_change":                   false,
		"get_resource_monitor_capabilities":     false,
		"get_resource_monitor_state":            false,
		"get_resource_monitor_history":          false,
		"get_resource_monitor_setting":          false,
		"plan_resource_recording_change":        false,
		"get_drive_admin_capabilities":          false,
		"get_drive_admin_status":                false,
		"get_drive_admin_connections":           false,
		"get_drive_admin_team_folders":          false,
		"get_drive_admin_logs":                  false,
		"get_drive_config":                      false,
		"plan_drive_config_change":              false,
		"get_surveillance_capabilities":         false,
		"get_surveillance_info":                 false,
		"get_surveillance_cameras":              false,
		"get_surveillance_home_mode":            false,
		"plan_surveillance_home_mode_change":    false,
		"get_terminal_snmp_capabilities":        false,
		"get_terminal_state":                    false,
		"get_snmp_state":                        false,
		"plan_terminal_change":                  false,
		"plan_snmp_change":                      false,
		"get_security_advisor_capabilities":     false,
		"get_security_advisor_status":           false,
		"get_security_advisor_schedule":         false,
		"plan_security_advisor_schedule_change": false,
		"get_account_protection_capabilities":   false,
		"get_account_protection_autoblock":      false,
		"get_account_protection_autoblock_list": false,
		"get_account_protection":                false,
		"get_account_protection_enforce_2fa":    false,
		"plan_account_protection_autoblock_change":    false,
		"plan_account_protection_list_change":         false,
		"plan_account_protection_thresholds_change":   false,
		"plan_account_protection_enforce_2fa_change":  false,
		"get_firewall_capabilities":             false,
		"get_firewall_status":                   false,
		"get_firewall_profiles":                 false,
		"get_firewall_rules":                    false,
		"get_network_capabilities":              false,
		"get_network_general":                   false,
		"get_network_interfaces":                false,
		"get_network_bonds":                     false,
		"get_network_routes":                    false,
		"plan_firewall_profile_change":          false,
		"plan_firewall_enable_change":           false,
		"get_login_portal_capabilities":         false,
		"get_login_portal_dsm":                  false,
		"get_login_portal_applications":         false,
		"get_login_portal_reverse_proxy":        false,
		"get_hyper_backup_capabilities":         false,
		"get_hyper_backup_tasks":                false,
		"get_hyper_backup_task":                 false,
		"get_hyper_backup_versions":             false,
		"get_hyper_backup_logs":                 false,
		"get_hyper_backup_vault":                false,
		"plan_hyper_backup_task_change":         false,
	}
	mutationTools := map[string]bool{
		"apply_account_plan":                   false,
		"apply_control_panel_time_plan":        false,
		"apply_san_plan":                       false,
		"apply_share_plan":                     false,
		"apply_storage_plan":                   false,
		"apply_file_service_plan":              false,
		"apply_nfs_export_plan":                false,
		"apply_service_discovery_plan":         false,
		"apply_ftp_service_plan":               false,
		"apply_rsync_service_plan":             false,
		"apply_tftp_service_plan":              false,
		"apply_photos_plan":                    false,
		"apply_office_plan":                    false,
		"apply_drive_config_plan":              false,
		"apply_surveillance_home_mode_plan":    false,
		"apply_package_plan":                   false,
		"apply_resource_recording_plan":        false,
		"apply_security_advisor_schedule_plan": false,
		"apply_account_protection_autoblock_plan":     false,
		"apply_account_protection_list_plan":          false,
		"apply_account_protection_thresholds_plan":    false,
		"apply_account_protection_enforce_2fa_plan":   false,
		"apply_terminal_plan":                         false,
		"apply_snmp_plan":                             false,
		"apply_firewall_profile_plan":                 false,
		"apply_firewall_enable_plan":                  false,
		"apply_hyper_backup_task_plan":                false,
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
	// run_security_scan is a load-heavy action: not read-only, not idempotent,
	// but not destructive (it changes no configuration), and it must carry the
	// apply authorization scope so a read-only token cannot invoke it.
	var foundRunScan bool
	for _, tool := range tools.Tools {
		if tool.Name != "run_security_scan" {
			continue
		}
		foundRunScan = true
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint || tool.Annotations.IdempotentHint ||
			tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Errorf("run_security_scan action annotations = %#v", tool.Annotations)
		}
		if scope, ok := ToolScope(tool.Name); !ok || scope != "nas.apply" {
			t.Errorf("run_security_scan scope = %q, %v", scope, ok)
		}
	}
	if !foundRunScan {
		t.Error("tool run_security_scan was not registered")
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
