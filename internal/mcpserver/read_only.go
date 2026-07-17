package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
)

// NewReadOnly returns the MCP surface permitted by the developer HTTP
// gateway. Planning and applying are deliberately omitted until the remote
// authorization and approval boundary is implemented by WI-016.
func NewReadOnly(service *application.Service, version string) *mcp.Server {
	server := New(service, version)
	server.RemoveTools(
		"plan_account_change",
		"plan_control_panel_time_change",
		"plan_file_service_change",
		"plan_package_change",
		"plan_resource_recording_change",
		"plan_san_change",
		"plan_share_change",
		"plan_storage_change",
		"apply_account_plan",
		"apply_control_panel_time_plan",
		"apply_file_service_plan",
		"apply_package_plan",
		"apply_resource_recording_plan",
		"apply_san_plan",
		"apply_share_plan",
		"apply_storage_plan",
	)
	return server
}
