package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/storage"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/storageinventory"
)

type StorageState = storage.State
type StorageCapabilities = storage.Capabilities

func (c *Client) StorageState(ctx context.Context) (StorageState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, storageinventory.APINames()...); err != nil {
		return StorageState{}, fmt.Errorf("prepare storage inventory target: %w", err)
	}
	state, _, err := storageinventory.Execute(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return StorageState{}, fmt.Errorf("get storage inventory: %w", err)
	}
	c.target.AddCapability(storageinventory.CapabilityName)
	return state, nil
}

func (c *Client) StorageCapabilities(ctx context.Context) (StorageCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, storageinventory.APINames()...); err != nil {
		return StorageCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare storage capabilities target: %w", err)
	}
	selection, err := storageinventory.Select(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return StorageCapabilities{}, CompatibilityReport{}, fmt.Errorf("select storage inventory backend: %w", err)
	}
	supported := selection.Supported
	if supported {
		c.target.AddCapability(storageinventory.CapabilityName)
	}
	capabilities := StorageCapabilities{
		InventoryRead: supported,
		DiskStatus:    supported,
		PoolStatus:    supported,
		VolumeStatus:  supported,
		PoolCreate:    false,
		VolumeCreate:  false,
		Mutations:     false,
	}
	c.updateDerivedCapabilitiesLocked()
	return capabilities, c.target.Report(selection), nil
}
