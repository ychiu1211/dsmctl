package externaldevice

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/externaldevice"
)

// decodeStorageArea normalizes a SYNO.Core.ExternalDevice.Storage.{USB,eSATA}
// `list` response. The top-level {devices:[...]} envelope was live-verified
// (empty on the lab); the per-device and per-partition fields were not
// observable (no disk attached) and are read tolerantly through their known DSM
// key alternates. A malformed top-level object is an error; unknown item fields
// are simply omitted, never fabricated.
func decodeStorageArea(data json.RawMessage) (externaldevice.ExternalStorageArea, error) {
	root, err := decodeObject(data)
	if err != nil {
		return externaldevice.ExternalStorageArea{}, fmt.Errorf("decode external storage: %w", err)
	}
	area := externaldevice.ExternalStorageArea{Devices: []externaldevice.ExternalStorageDevice{}}
	for _, item := range objectList(root, "devices", "device") {
		area.Devices = append(area.Devices, decodeStorageDevice(item))
	}
	return area, nil
}

func decodeStorageDevice(item map[string]any) externaldevice.ExternalStorageDevice {
	device := externaldevice.ExternalStorageDevice{
		DevID:       stringValue(item, "dev_id", "id"),
		DevPath:     stringValue(item, "dev_path", "path"),
		Type:        stringValue(item, "dev_type", "type"),
		Title:       strings.TrimSpace(stringValue(item, "dev_title", "title", "name")),
		Product:     strings.TrimSpace(stringValue(item, "product", "dev_product", "model")),
		Vendor:      strings.TrimSpace(stringValue(item, "vendor", "producer", "dev_vendor", "manufacturer")),
		Serial:      strings.TrimSpace(stringValue(item, "serial", "dev_serial", "serial_num")),
		Status:      stringValue(item, "status", "dev_status"),
		TotalSizeMB: intValueOr(item, 0, "total_size_mb", "dev_size_mb", "total_size"),
		Partitions:  []externaldevice.ExternalStoragePartition{},
	}
	for _, part := range objectList(item, "partitions", "partition") {
		device.Partitions = append(device.Partitions, externaldevice.ExternalStoragePartition{
			Name:        stringValue(part, "part_title", "name_id", "name", "dev_path", "dev_fsname"),
			Filesystem:  stringValue(part, "filesystem", "dev_fstype", "fstype", "format"),
			TotalSizeMB: intValueOr(part, 0, "total_size_mb", "partition_size_mb", "total_size"),
			UsedSizeMB:  intValueOr(part, 0, "used_size_mb", "used_size"),
			MountPoint:  stringValue(part, "mount_point", "mountpoint"),
			ShareName:   stringValue(part, "share_name", "sharename"),
			Status:      stringValue(part, "status", "part_status"),
		})
	}
	return device
}

// decodePrinters normalizes the SYNO.Core.ExternalDevice.Printer `list`
// response. The top-level {printers:[...]} envelope was live-verified (empty on
// the lab); per-printer fields were not observable and are read tolerantly.
func decodePrinters(data json.RawMessage) ([]externaldevice.Printer, error) {
	root, err := decodeObject(data)
	if err != nil {
		return nil, fmt.Errorf("decode printers: %w", err)
	}
	printers := make([]externaldevice.Printer, 0)
	for _, item := range objectList(root, "printers", "printer") {
		printers = append(printers, externaldevice.Printer{
			ID:           stringValue(item, "id", "printer_id"),
			Name:         strings.TrimSpace(stringValue(item, "name", "printer_name", "title")),
			Type:         stringValue(item, "type", "printer_type", "connection"),
			Status:       stringValue(item, "status", "spooler_status", "printer_status"),
			Manager:      stringValue(item, "manager", "owner"),
			Default:      boolValue(item, "default", "is_default"),
			SpoolerCount: intValueOr(item, 0, "spooler_count", "job_count", "queue_count"),
		})
	}
	return printers, nil
}

// decodePrinterSharing normalizes SYNO.Core.ExternalDevice.Printer.BonjourSharing.get.
// The enable_bonjour_support flag was live-verified on the lab.
func decodePrinterSharing(data json.RawMessage) (externaldevice.PrinterSharing, error) {
	root, err := decodeObject(data)
	if err != nil {
		return externaldevice.PrinterSharing{}, fmt.Errorf("decode printer sharing: %w", err)
	}
	return externaldevice.PrinterSharing{
		BonjourEnabled: boolValue(root, "enable_bonjour_support", "enable_bonjour", "bonjour_enable"),
	}, nil
}

// --- tolerant decode helpers (kept local to the package, mirroring the
// hardware module) ---

func decodeObject(data json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func objectList(values map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		items, ok := values[key].([]any)
		if !ok {
			continue
		}
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if object, ok := item.(map[string]any); ok {
				result = append(result, object)
			}
		}
		return result
	}
	return nil
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(typed)
		}
	}
	return ""
}

// intValue reports the first present integer-valued key and whether one was
// found, so a caller can distinguish an absent field from a real zero.
func intValue(values map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			if result, err := strconv.Atoi(typed.String()); err == nil {
				return result, true
			}
			if result, err := typed.Float64(); err == nil {
				return int(result), true
			}
		case float64:
			return int(typed), true
		case string:
			if result, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return result, true
			}
		}
	}
	return 0, false
}

func intValueOr(values map[string]any, fallback int, keys ...string) int {
	if result, ok := intValue(values, keys...); ok {
		return result
	}
	return fallback
}

func boolValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case bool:
			return typed
		case json.Number:
			return typed.String() != "0" && typed.String() != ""
		case float64:
			return typed != 0
		case string:
			result, _ := strconv.ParseBool(typed)
			return result || typed == "1"
		}
	}
	return false
}
