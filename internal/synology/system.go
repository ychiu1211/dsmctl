package synology

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/systeminfo"
)

type SystemInfo = systeminfo.Info

type lockedExecutor struct {
	client *Client
}

func (executor lockedExecutor) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return executor.client.executeLocked(ctx, request)
}

func (executor lockedExecutor) ExecuteScript(ctx context.Context, request compatibility.Request) ([]byte, error) {
	return executor.client.executeScriptLocked(ctx, request)
}

func (c *Client) SystemInfo(ctx context.Context) (SystemInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.systemInfoLocked(ctx)
}

const networkAPIName = "SYNO.Core.Network"

// GetServerName reads the DSM server name (hostname) via SYNO.Core.Network.get.
// SYNO.Core.System.info does not carry server_name on current DSM builds, so the
// network module is the authority for both reading and writing the name.
func (c *Client) GetServerName(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverNameLocked(ctx)
}

func (c *Client) serverNameLocked(ctx context.Context) (string, error) {
	if err := c.discoverAPIsLocked(ctx, networkAPIName); err != nil {
		return "", fmt.Errorf("discover %s: %w", networkAPIName, err)
	}
	data, err := (lockedExecutor{client: c}).Execute(ctx, compatibility.Request{
		API: networkAPIName, Version: 1, Method: "get", ReadOnly: true,
	})
	if err != nil {
		return "", fmt.Errorf("read server name via %s.get: %w", networkAPIName, err)
	}
	var payload struct {
		ServerName string `json:"server_name"`
		Hostname   string `json:"hostname"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("decode %s.get: %w", networkAPIName, err)
	}
	if payload.ServerName != "" {
		return payload.ServerName, nil
	}
	return payload.Hostname, nil
}

// SetServerName sets the DSM server name (hostname) via SYNO.Core.Network.set
// server_name — the call the DSM setup wizard and dsmctl provisioning use — then
// re-reads it via GetServerName and returns the persisted value so the caller can
// verify the postcondition. It never resets any other network field.
func (c *Client) SetServerName(ctx context.Context, name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.discoverAPIsLocked(ctx, networkAPIName); err != nil {
		return "", fmt.Errorf("discover %s: %w", networkAPIName, err)
	}
	if _, err := (lockedExecutor{client: c}).Execute(ctx, compatibility.Request{
		API:            networkAPIName,
		Version:        1,
		Method:         "set",
		JSONParameters: map[string]any{"server_name": name},
	}); err != nil {
		return "", fmt.Errorf("set server name via %s.set: %w", networkAPIName, err)
	}
	return c.serverNameLocked(ctx)
}

func (c *Client) systemInfoLocked(ctx context.Context) (SystemInfo, error) {
	if err := c.discoverAPIsLocked(ctx, systeminfo.APINames()...); err != nil {
		return SystemInfo{}, fmt.Errorf("discover system info APIs: %w", err)
	}
	info, initialSelection, err := systeminfo.Execute(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SystemInfo{}, fmt.Errorf("get system info: %w", err)
	}
	if info.DSMVersion != "" {
		c.target.DSM = compatibility.ParseDSMVersion(info.DSMVersion)
	}
	// DSM release is learned through this bootstrap read. If discovering it
	// activates a higher-priority release override, execute that backend once
	// and return its normalized result.
	finalSelection, selectionErr := systeminfo.Select(c.target)
	if selectionErr != nil {
		return SystemInfo{}, fmt.Errorf("select system info backend after DSM discovery: %w", selectionErr)
	}
	if finalSelection.Backend != initialSelection.Backend {
		info, _, err = systeminfo.Execute(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return SystemInfo{}, fmt.Errorf("get system info with DSM-specific backend: %w", err)
		}
		if info.DSMVersion != "" {
			c.target.DSM = compatibility.ParseDSMVersion(info.DSMVersion)
		}
	}
	c.updateDerivedCapabilitiesLocked()
	return info, nil
}
