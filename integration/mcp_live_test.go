package integration

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPGetSystemInfoLive is opt-in because it uses the caller's real config
// and OS credential store. It exercises the actual stdio process boundary,
// unlike the in-process mock contract tests.
func TestMCPGetSystemInfoLive(t *testing.T) {
	binary := os.Getenv("DSMCTL_MCP_BINARY")
	nas := os.Getenv("DSMCTL_LIVE_NAS")
	if binary == "" || nas == "" {
		t.Skip("set DSMCTL_MCP_BINARY and DSMCTL_LIVE_NAS to run the live MCP smoke test")
	}

	args := []string{}
	if configPath := os.Getenv("DSMCTL_LIVE_CONFIG"); configPath != "" {
		args = append(args, "--config", configPath)
	}
	command := exec.Command(binary, args...)
	client := mcp.NewClient(&mcp.Implementation{Name: "dsmctl-live-test", Version: "0.1.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		t.Fatalf("connect to MCP server: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_system_info",
		Arguments: map[string]any{"nas": nas},
	})
	if err != nil {
		t.Fatalf("call get_system_info: %v", err)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("encode structured result: %v", err)
	}
	var output struct {
		NAS    string `json:"nas"`
		System struct {
			Model string `json:"model"`
		} `json:"system"`
	}
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("decode structured result: %v", err)
	}
	if output.NAS != nas || output.System.Model == "" {
		t.Fatalf("unexpected result: nas=%q model=%q", output.NAS, output.System.Model)
	}

	capabilities, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_capabilities",
		Arguments: map[string]any{"nas": nas},
	})
	if err != nil {
		t.Fatalf("call get_capabilities: %v", err)
	}
	capabilityData, err := json.Marshal(capabilities.StructuredContent)
	if err != nil {
		t.Fatalf("encode capability result: %v", err)
	}
	var capabilityOutput struct {
		Report struct {
			Operations []struct {
				Operation string `json:"operation"`
				Supported bool   `json:"supported"`
				Backend   string `json:"backend"`
			} `json:"operations"`
		} `json:"report"`
	}
	if err := json.Unmarshal(capabilityData, &capabilityOutput); err != nil {
		t.Fatalf("decode capability result: %v", err)
	}
	var systemSupported bool
	for _, operation := range capabilityOutput.Report.Operations {
		if operation.Operation == "system.info" {
			systemSupported = operation.Supported && operation.Backend != ""
			break
		}
	}
	if !systemSupported {
		t.Fatalf("unexpected capability result: %#v", capabilityOutput.Report.Operations)
	}

	accountState, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_account_state",
		Arguments: map[string]any{"nas": nas},
	})
	if err != nil {
		t.Fatalf("call get_account_state: %v", err)
	}
	accountData, err := json.Marshal(accountState.StructuredContent)
	if err != nil {
		t.Fatalf("encode account result: %v", err)
	}
	var accountOutput struct {
		Identity struct {
			Users  []any `json:"users"`
			Groups []any `json:"groups"`
		} `json:"identity"`
	}
	if err := json.Unmarshal(accountData, &accountOutput); err != nil {
		t.Fatalf("decode account result: %v", err)
	}
	if len(accountOutput.Identity.Users) == 0 || len(accountOutput.Identity.Groups) == 0 {
		t.Fatalf("unexpected account result: users=%d groups=%d", len(accountOutput.Identity.Users), len(accountOutput.Identity.Groups))
	}

	shareState, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_share_state",
		Arguments: map[string]any{"nas": nas, "include_permissions": true},
	})
	if err != nil {
		t.Fatalf("call get_share_state: %v", err)
	}
	shareData, err := json.Marshal(shareState.StructuredContent)
	if err != nil {
		t.Fatalf("encode share result: %v", err)
	}
	var shareOutput struct {
		Shares struct {
			Shares []struct {
				Permissions []any `json:"permissions"`
			} `json:"shares"`
			PermissionsIncluded bool `json:"permissions_included"`
		} `json:"shares"`
	}
	if err := json.Unmarshal(shareData, &shareOutput); err != nil {
		t.Fatalf("decode share result: %v", err)
	}
	permissionCount := 0
	for _, folder := range shareOutput.Shares.Shares {
		permissionCount += len(folder.Permissions)
	}
	if len(shareOutput.Shares.Shares) == 0 || !shareOutput.Shares.PermissionsIncluded || permissionCount == 0 {
		t.Fatalf("unexpected share result: shares=%d permissions_included=%t permissions=%d", len(shareOutput.Shares.Shares), shareOutput.Shares.PermissionsIncluded, permissionCount)
	}
}
