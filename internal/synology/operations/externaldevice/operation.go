// Package externaldevice implements independently selectable, read-only DSM
// operations for the Control Panel External Devices surface.
//
// Three independent DSM API families are read, each gated in isolation so a NAS
// that advertises one but not the others fails closed only for the missing area:
//
//   - USB storage:   SYNO.Core.ExternalDevice.Storage.USB   v1 `list` -> {devices:[...]}
//   - eSATA storage: SYNO.Core.ExternalDevice.Storage.eSATA v1 `list` -> {devices:[...]}
//   - Printers:      SYNO.Core.ExternalDevice.Printer       v1 `list` -> {printers:[...]}
//     with the global Bonjour/AirPrint sharing toggle read independently from
//     SYNO.Core.ExternalDevice.Printer.BonjourSharing v1 `get` -> {enable_bonjour_support}.
//
// The API names, `list`/`get` methods, v1 versions, and top-level envelope keys
// were live-verified against the lab (DS3018xs, DSM 7.3); `get` on the storage
// families and `list`/`get` variants that DSM does not expose return code 103.
// No external disk or printer was attached, so each list returned empty — the
// no-device path is the live-verified path and the per-item field shapes are
// decoded tolerantly (see decode.go). This module is read-only; guarded eject /
// printer set / spooler clear are WI-076 Slice B. UPS lives in the hardware
// module (WI-075), not here.
package externaldevice

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/externaldevice"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	USBStorageAPI     = "SYNO.Core.ExternalDevice.Storage.USB"
	ESATAStorageAPI   = "SYNO.Core.ExternalDevice.Storage.eSATA"
	PrinterAPI        = "SYNO.Core.ExternalDevice.Printer"
	PrinterBonjourAPI = "SYNO.Core.ExternalDevice.Printer.BonjourSharing"

	USBStorageReadCapabilityName     = "external_device.storage.usb.read"
	ESATAStorageReadCapabilityName   = "external_device.storage.esata.read"
	PrinterReadCapabilityName        = "external_device.printer.read"
	PrinterSharingReadCapabilityName = "external_device.printer.sharing.read"
)

// Input is the empty request every External Devices read takes.
type Input struct{}

var usbStorageOp = compatibility.Operation[Input, externaldevice.ExternalStorageArea]{
	Name: USBStorageReadCapabilityName,
	Variants: []compatibility.Variant[Input, externaldevice.ExternalStorageArea]{
		{
			Name: "core-externaldevice-storage-usb-list-v1", API: USBStorageAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(USBStorageAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (externaldevice.ExternalStorageArea, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: USBStorageAPI, Version: 1, Method: "list", ReadOnly: true,
				})
				if err != nil {
					return externaldevice.ExternalStorageArea{}, fmt.Errorf("call %s.list v1: %w", USBStorageAPI, err)
				}
				return decodeStorageArea(data)
			},
		},
	},
}

var esataStorageOp = compatibility.Operation[Input, externaldevice.ExternalStorageArea]{
	Name: ESATAStorageReadCapabilityName,
	Variants: []compatibility.Variant[Input, externaldevice.ExternalStorageArea]{
		{
			Name: "core-externaldevice-storage-esata-list-v1", API: ESATAStorageAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(ESATAStorageAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (externaldevice.ExternalStorageArea, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: ESATAStorageAPI, Version: 1, Method: "list", ReadOnly: true,
				})
				if err != nil {
					return externaldevice.ExternalStorageArea{}, fmt.Errorf("call %s.list v1: %w", ESATAStorageAPI, err)
				}
				return decodeStorageArea(data)
			},
		},
	},
}

var printerOp = compatibility.Operation[Input, []externaldevice.Printer]{
	Name: PrinterReadCapabilityName,
	Variants: []compatibility.Variant[Input, []externaldevice.Printer]{
		{
			Name: "core-externaldevice-printer-list-v1", API: PrinterAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(PrinterAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]externaldevice.Printer, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: PrinterAPI, Version: 1, Method: "list", ReadOnly: true,
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v1: %w", PrinterAPI, err)
				}
				return decodePrinters(data)
			},
		},
	},
}

var printerSharingOp = compatibility.Operation[Input, externaldevice.PrinterSharing]{
	Name: PrinterSharingReadCapabilityName,
	Variants: []compatibility.Variant[Input, externaldevice.PrinterSharing]{
		{
			Name: "core-externaldevice-printer-bonjoursharing-get-v1", API: PrinterBonjourAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(PrinterBonjourAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (externaldevice.PrinterSharing, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: PrinterBonjourAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return externaldevice.PrinterSharing{}, fmt.Errorf("call %s.get v1: %w", PrinterBonjourAPI, err)
				}
				return decodePrinterSharing(data)
			},
		},
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them in a single query before selecting any area.
func APINames() []string {
	return []string{USBStorageAPI, ESATAStorageAPI, PrinterAPI, PrinterBonjourAPI}
}

// SelectUSBStorage reports the USB-storage backend selection.
func SelectUSBStorage(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := usbStorageOp.Select(target)
	return selection, err
}

// SelectESATAStorage reports the eSATA-storage backend selection.
func SelectESATAStorage(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := esataStorageOp.Select(target)
	return selection, err
}

// SelectPrinter reports the printer-list backend selection.
func SelectPrinter(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := printerOp.Select(target)
	return selection, err
}

// SelectPrinterSharing reports the Bonjour/AirPrint sharing backend selection.
func SelectPrinterSharing(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := printerSharingOp.Select(target)
	return selection, err
}

// ReadStorage reads external disks on both buses — USB and eSATA — each gated
// independently. A missing bus is skipped (nil sub-area) so a model with no
// eSATA port still returns the USB list. The returned selection is the USB
// area's; capabilities report both buses individually.
func ReadStorage(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externaldevice.StorageState, compatibility.Selection, error) {
	usbSelection, err := SelectUSBStorage(target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return externaldevice.StorageState{}, usbSelection, err
	}

	state := externaldevice.StorageState{}
	if usbSelection.Supported {
		area, _, err := usbStorageOp.Run(ctx, target, executor, Input{})
		if err != nil {
			return externaldevice.StorageState{}, usbSelection, err
		}
		state.USB = &area
	}
	if area, ok, err := readOptionalESATA(ctx, target, executor); err != nil {
		return externaldevice.StorageState{}, usbSelection, err
	} else if ok {
		state.ESATA = &area
	}
	return state, usbSelection, nil
}

func readOptionalESATA(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externaldevice.ExternalStorageArea, bool, error) {
	selection, err := SelectESATAStorage(target)
	if err != nil {
		if compatibility.IsUnsupported(err) {
			return externaldevice.ExternalStorageArea{}, false, nil
		}
		return externaldevice.ExternalStorageArea{}, false, err
	}
	if !selection.Supported {
		return externaldevice.ExternalStorageArea{}, false, nil
	}
	area, _, err := esataStorageOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return externaldevice.ExternalStorageArea{}, false, err
	}
	return area, true, nil
}

// ReadPrinters reads the connected printers plus the global Bonjour/AirPrint
// sharing toggle, each gated independently. The returned selection is the
// printer-list area's; the sharing toggle is omitted (nil) when its API is
// absent.
func ReadPrinters(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externaldevice.PrinterState, compatibility.Selection, error) {
	printers, selection, err := printerOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return externaldevice.PrinterState{}, selection, err
	}
	state := externaldevice.PrinterState{Printers: printers}
	if sharing, ok, err := readOptionalPrinterSharing(ctx, target, executor); err != nil {
		return externaldevice.PrinterState{}, selection, err
	} else if ok {
		state.Sharing = &sharing
	}
	return state, selection, nil
}

func readOptionalPrinterSharing(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (externaldevice.PrinterSharing, bool, error) {
	selection, err := SelectPrinterSharing(target)
	if err != nil {
		if compatibility.IsUnsupported(err) {
			return externaldevice.PrinterSharing{}, false, nil
		}
		return externaldevice.PrinterSharing{}, false, err
	}
	if !selection.Supported {
		return externaldevice.PrinterSharing{}, false, nil
	}
	sharing, _, err := printerSharingOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return externaldevice.PrinterSharing{}, false, err
	}
	return sharing, true, nil
}
