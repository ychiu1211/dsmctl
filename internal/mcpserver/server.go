package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type listNASInput struct{}

type listNASOutput struct {
	NAS []config.Summary `json:"nas" jsonschema:"Configured NAS profiles"`
}

type getSystemInfoInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getSystemInfoOutput struct {
	NAS    string              `json:"nas" jsonschema:"NAS profile used for the request"`
	System synology.SystemInfo `json:"system" jsonschema:"System information returned by DSM"`
}

type getCapabilitiesInput struct {
	NAS string `json:"nas,omitempty" jsonschema:"NAS profile name; omit to use the configured default"`
}

type getCapabilitiesOutput struct {
	NAS    string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Report synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs, capabilities, quirks, and selected operation backends"`
}

func New(service *application.Service, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "dsmctl", Version: version}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_nas",
		Description: "List configured Synology NAS connection profiles. Passwords are never returned.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ listNASInput) (*mcp.CallToolResult, listNASOutput, error) {
		return nil, listNASOutput{NAS: service.ListNAS()}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_system_info",
		Description: "Log in to a configured Synology NAS and return basic DSM system information.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getSystemInfoInput) (*mcp.CallToolResult, getSystemInfoOutput, error) {
		result, err := service.GetSystemInfo(ctx, input.NAS)
		if err != nil {
			return nil, getSystemInfoOutput{}, err
		}
		return nil, getSystemInfoOutput{NAS: result.NAS, System: result.System}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_capabilities",
		Description: "Discover the DSM target and report supported capabilities plus the version-specific backend selected for each operation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input getCapabilitiesInput) (*mcp.CallToolResult, getCapabilitiesOutput, error) {
		result, err := service.GetCompatibility(ctx, input.NAS)
		if err != nil {
			return nil, getCapabilitiesOutput{}, err
		}
		return nil, getCapabilitiesOutput{NAS: result.NAS, Report: result.Report}, nil
	})

	return server
}
