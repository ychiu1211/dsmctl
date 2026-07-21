package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
)

// NewReadOnly returns the MCP surface permitted by the developer HTTP
// gateway. Planning and applying are deliberately omitted until the remote
// authorization and approval boundary is implemented by WI-016. LAN discovery
// is also omitted: a remote caller must not be able to trigger a broadcast scan
// of, or enumerate devices on, the gateway host's local network.
func NewReadOnly(service *application.Service, version string) *mcp.Server {
	server := New(service, version)
	server.RemoveTools(
		"discover_lan_devices",
		// FileStation content transfer and mutations never reach the read-only
		// gateway: get_filestation_file_content / get_filestation_thumbnail would
		// exfiltrate file bytes to a remote caller, and the plan/apply pair
		// mutates the NAS. get_drive_log_export is likewise a bulk content
		// transfer.
		"get_filestation_file_content",
		"get_filestation_thumbnail",
		"get_drive_log_export",
		// Certificate export extracts private-key material to a local file; the
		// plan/apply pair mutates the certificate store (every write is high risk).
		"get_certificate_export",
		// run_security_scan is a load-heavy NAS action (not a read); the
		// schedule/baseline plan/apply pair mutates DSM. All three are stripped
		// from the read-only gateway.
		"run_security_scan",
		"plan_security_advisor_schedule_change",
		"apply_security_advisor_schedule_plan",
		"plan_account_protection_autoblock_change",
		"apply_account_protection_autoblock_plan",
		"plan_account_protection_list_change",
		"apply_account_protection_list_plan",
		"plan_account_protection_thresholds_change",
		"apply_account_protection_thresholds_plan",
		"plan_account_protection_enforce_2fa_change",
		"apply_account_protection_enforce_2fa_plan",
		// Terminal & SNMP guarded writes (WI-071). Enabling SSH/Telnet or disabling
		// SSH is high risk; the SNMP set carries the community secret. Both pairs are
		// stripped from the read-only gateway.
		"plan_terminal_change",
		"apply_terminal_plan",
		"plan_snmp_change",
		"apply_snmp_plan",
		// Login Portal guarded writes (WI-070). A DSM web-service change changes how
		// DSM itself is reached (high risk); the application-portal and reverse-proxy
		// writes change external exposure. All are stripped from the read-only gateway.
		"plan_login_portal_dsm_change",
		"apply_login_portal_dsm_plan",
		"plan_login_portal_application_change",
		"apply_login_portal_application_plan",
		"plan_login_portal_reverse_proxy_create",
		"plan_login_portal_reverse_proxy_delete",
		"apply_login_portal_reverse_proxy_plan",
		"plan_certificate_change",
		"apply_certificate_plan",
		"plan_filestation_change",
		"apply_filestation_plan",
		"plan_account_change",
		"plan_control_panel_time_change",
		"plan_download_station_settings_change",
		"plan_download_station_task_change",
		"plan_drive_config_change",
		"plan_drive_connection_kick",
		"plan_drive_restore",
		"plan_drive_team_folder_change",
		"plan_external_access_quickconnect_change",
		"plan_external_access_quickconnect_config_change",
		"plan_external_access_quickconnect_permission_change",
		"plan_external_access_ddns_change",
		"plan_file_service_change",
		"plan_ftp_service_change",
		"plan_nfs_export_change",
		"plan_office_change",
		"plan_package_change",
		"plan_package_install",
		"plan_package_local_install",
		"plan_package_update",
		"plan_photos_change",
		"plan_resource_recording_change",
		"plan_rsync_service_change",
		"plan_san_change",
		"plan_service_discovery_change",
		"plan_share_change",
		"plan_storage_change",
		"plan_surveillance_home_mode_change",
		"plan_tftp_service_change",
		"apply_account_plan",
		"apply_control_panel_time_plan",
		"apply_download_station_settings_plan",
		"apply_download_station_task_plan",
		"apply_drive_config_plan",
		"apply_drive_connection_kick_plan",
		"apply_drive_restore_plan",
		"apply_drive_team_folder_plan",
		"apply_external_access_quickconnect_plan",
		"apply_external_access_quickconnect_config_plan",
		"apply_external_access_quickconnect_permission_plan",
		"apply_external_access_ddns_plan",
		"apply_file_service_plan",
		"apply_ftp_service_plan",
		"apply_nfs_export_plan",
		"apply_office_plan",
		"apply_package_plan",
		"apply_package_install_plan",
		"apply_package_local_install_plan",
		"apply_photos_plan",
		"apply_resource_recording_plan",
		"apply_rsync_service_plan",
		"apply_san_plan",
		"apply_service_discovery_plan",
		"apply_share_plan",
		"apply_storage_plan",
		"apply_surveillance_home_mode_plan",
		"apply_tftp_service_plan",
	)
	return server
}
