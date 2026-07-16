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

func (c *Client) SystemInfo(ctx context.Context) (SystemInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.systemInfoLocked(ctx)
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
