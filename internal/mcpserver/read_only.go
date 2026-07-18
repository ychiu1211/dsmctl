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
		"plan_account_change",
		"plan_control_panel_time_change",
		"plan_drive_config_change",
		"plan_file_service_change",
		"plan_ftp_service_change",
		"plan_nfs_export_change",
		"plan_package_change",
		"plan_photos_change",
		"plan_resource_recording_change",
		"plan_rsync_service_change",
		"plan_san_change",
		"plan_service_discovery_change",
		"plan_share_change",
		"plan_storage_change",
		"plan_tftp_service_change",
		"apply_account_plan",
		"apply_control_panel_time_plan",
		"apply_drive_config_plan",
		"apply_file_service_plan",
		"apply_ftp_service_plan",
		"apply_nfs_export_plan",
		"apply_package_plan",
		"apply_photos_plan",
		"apply_resource_recording_plan",
		"apply_rsync_service_plan",
		"apply_san_plan",
		"apply_service_discovery_plan",
		"apply_share_plan",
		"apply_storage_plan",
		"apply_tftp_service_plan",
	)
	return server
}
