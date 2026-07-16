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
}
