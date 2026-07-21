package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/externaldevice"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	externaldeviceops "github.com/ychiu1211/dsmctl/internal/synology/operations/externaldevice"
)

type ExternalStorageState = externaldevice.StorageState
type ExternalPrinterState = externaldevice.PrinterState
type ExternalDeviceCapabilities = externaldevice.Capabilities

// ExternalStorage reads attached external disks on both buses — USB and eSATA —
// each gated independently so a model with no eSATA port still returns the USB
// list. A bus whose DSM API is absent is omitted (nil sub-area) rather than
// erroring the whole read. The API is present even when no disk is attached; the
// state then reports an empty device list.
func (c *Client) ExternalStorage(ctx context.Context) (ExternalStorageState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externaldeviceops.APINames()...); err != nil {
		return ExternalStorageState{}, fmt.Errorf("prepare external storage target: %w", err)
	}
	state, _, err := externaldeviceops.ReadStorage(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ExternalStorageState{}, fmt.Errorf("read external storage: %w", err)
	}
	if state.USB != nil {
		c.target.AddCapability(externaldeviceops.USBStorageReadCapabilityName)
	}
	if state.ESATA != nil {
		c.target.AddCapability(externaldeviceops.ESATAStorageReadCapabilityName)
	}
	return state, nil
}

// ExternalPrinters reads the connected printers plus the global Bonjour/AirPrint
// sharing toggle, each gated independently. The sharing toggle is omitted when
// its DSM API is absent. The printer API is present even when no printer is
// attached; the state then reports an empty printer list.
func (c *Client) ExternalPrinters(ctx context.Context) (ExternalPrinterState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externaldeviceops.APINames()...); err != nil {
		return ExternalPrinterState{}, fmt.Errorf("prepare external printer target: %w", err)
	}
	state, selection, err := externaldeviceops.ReadPrinters(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ExternalPrinterState{}, fmt.Errorf("read external printers: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(externaldeviceops.PrinterReadCapabilityName)
	}
	if state.Sharing != nil {
		c.target.AddCapability(externaldeviceops.PrinterSharingReadCapabilityName)
	}
	return state, nil
}

// ExternalDeviceCapabilitiesState reports which External Devices read areas this
// NAS exposes, each selected independently so one missing API family does not
// disable the others. UPS is deliberately not reported here; it belongs to the
// Hardware & Power module.
func (c *Client) ExternalDeviceCapabilitiesState(ctx context.Context) (ExternalDeviceCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, externaldeviceops.APINames()...); err != nil {
		return ExternalDeviceCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare external device capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{externaldeviceops.SelectUSBStorage, externaldeviceops.USBStorageReadCapabilityName},
		{externaldeviceops.SelectESATAStorage, externaldeviceops.ESATAStorageReadCapabilityName},
		{externaldeviceops.SelectPrinter, externaldeviceops.PrinterReadCapabilityName},
		{externaldeviceops.SelectPrinterSharing, externaldeviceops.PrinterSharingReadCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return ExternalDeviceCapabilities{}, CompatibilityReport{}, fmt.Errorf("select external device backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := ExternalDeviceCapabilities{
		USBStorage:     selections[0].Supported,
		ESATAStorage:   selections[1].Supported,
		Printer:        selections[2].Supported,
		PrinterSharing: selections[3].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
