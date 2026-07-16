package storageinventory

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/storage"
)

func decode(data json.RawMessage) (storage.State, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return storage.State{}, fmt.Errorf("decode storage inventory: %w", err)
	}

	state := storage.State{
		Disks:   make([]storage.Disk, 0),
		Pools:   make([]storage.Pool, 0),
		Volumes: make([]storage.Volume, 0),
	}
	for _, item := range objectList(raw, "disks", "drives") {
		state.Disks = append(state.Disks, decodeDisk(item))
	}
	for _, item := range objectList(raw, "storagePools", "storage_pools", "pools") {
		state.Pools = append(state.Pools, decodePool(item))
	}
	for _, item := range objectList(raw, "volumes") {
		state.Volumes = append(state.Volumes, decodeVolume(item))
	}
	return state, nil
}

func decodeDisk(raw map[string]any) storage.Disk {
	temperature, hasTemperature := float64Value(raw, "temp", "temperature", "temperature_c")
	var temperaturePointer *float64
	if hasTemperature {
		temperaturePointer = &temperature
	}
	status := stringValue(raw, "status", "disk_status")
	return storage.Disk{
		ID:           stringValue(raw, "id", "disk_id", "device"),
		Name:         stringValue(raw, "name", "longName", "display_name"),
		Device:       stringValue(raw, "device", "dev_name", "path"),
		Slot:         stringValue(raw, "slot", "slot_id", "bay", "bay_id", "num_id"),
		Vendor:       stringValue(raw, "vendor", "brand"),
		Model:        stringValue(raw, "model", "model_name"),
		Serial:       stringValue(raw, "serial", "serial_number"),
		Firmware:     stringValue(raw, "firm", "firmware", "firmware_version"),
		Type:         stringValue(raw, "type", "disk_type", "media_type", "diskType"),
		Interface:    stringValue(raw, "portType", "port_type", "interface", "diskType"),
		Status:       status,
		Health:       firstNonEmpty(stringValue(raw, "health", "health_status", "overview_status"), status),
		SMARTStatus:  stringValue(raw, "smart_status", "smartStatus", "smart"),
		SizeBytes:    uint64Value(raw, "size_total", "total_size", "sizeTotal", "totalSize", "capacity"),
		TemperatureC: temperaturePointer,
	}
}

func decodePool(raw map[string]any) storage.Pool {
	status := stringValue(raw, "status", "pool_status")
	size, used, available := sizeValues(raw)
	if available == 0 && size >= used {
		available = size - used
	}
	return storage.Pool{
		ID:             stringValue(raw, "id", "pool_id", "pool_path", "num_id"),
		Name:           stringValue(raw, "name", "display_name", "desc"),
		RAIDType:       stringValue(raw, "raidType", "raid_type", "raid", "device_type"),
		Status:         status,
		Health:         firstNonEmpty(stringValue(raw, "health", "health_status", "overview_status"), status),
		SizeBytes:      size,
		UsedBytes:      used,
		AvailableBytes: available,
		DiskIDs:        stringList(raw, "disk_ids", "diskIds", "disks", "drives"),
	}
}

func decodeVolume(raw map[string]any) storage.Volume {
	status := stringValue(raw, "status", "volume_status")
	size, used, available := sizeValues(raw)
	if available == 0 && size >= used {
		available = size - used
	}
	return storage.Volume{
		ID:             stringValue(raw, "id", "volume_id", "volume_path", "num_id"),
		Name:           stringValue(raw, "name", "display_name", "desc"),
		PoolID:         stringValue(raw, "pool_id", "poolId", "pool_path", "storage_pool_id"),
		FileSystem:     stringValue(raw, "fs_type", "fsType", "file_system", "filesystem"),
		Status:         status,
		Health:         firstNonEmpty(stringValue(raw, "health", "health_status", "overview_status"), status),
		SizeBytes:      size,
		UsedBytes:      used,
		AvailableBytes: available,
		ReadOnly:       boolValue(raw, "read_only", "readonly", "is_read_only"),
	}
}

func sizeValues(raw map[string]any) (total, used, available uint64) {
	total = uint64Value(raw, "size_total", "size_total_byte", "total_size", "sizeTotal", "totalSize", "capacity")
	used = uint64Value(raw, "size_used", "size_used_byte", "used_size", "sizeUsed", "used")
	available = uint64Value(raw, "size_free", "size_free_byte", "available_size", "sizeAvailable", "available", "free")
	if nested, ok := raw["size"].(map[string]any); ok {
		if total == 0 {
			total = uint64Value(nested, "total", "size_total", "total_size")
		}
		if used == 0 {
			used = uint64Value(nested, "used", "size_used", "used_size")
		}
		if available == 0 {
			available = uint64Value(nested, "free", "available", "size_free", "available_size")
		}
	}
	return total, used, available
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
		}
	}
	return ""
}

func uint64Value(values map[string]any, keys ...string) uint64 {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			if result, err := strconv.ParseUint(typed.String(), 10, 64); err == nil {
				return result
			}
			if result, err := typed.Float64(); err == nil && result >= 0 {
				return uint64(result)
			}
		case float64:
			if typed >= 0 {
				return uint64(typed)
			}
		case string:
			if result, err := strconv.ParseUint(typed, 10, 64); err == nil {
				return result
			}
			if result, err := strconv.ParseFloat(typed, 64); err == nil && result >= 0 {
				return uint64(result)
			}
		}
	}
	return 0
}

func float64Value(values map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			result, err := typed.Float64()
			return result, err == nil
		case float64:
			return typed, true
		case string:
			result, err := strconv.ParseFloat(typed, 64)
			return result, err == nil
		}
	}
	return 0, false
}

func boolValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case bool:
			return typed
		case json.Number:
			return typed.String() == "1"
		case float64:
			return typed != 0
		case string:
			result, _ := strconv.ParseBool(typed)
			return result || typed == "1"
		}
	}
	return false
}

func stringList(values map[string]any, keys ...string) []string {
	result := make([]string, 0)
	for _, key := range keys {
		items, ok := values[key].([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			switch typed := item.(type) {
			case string:
				result = append(result, typed)
			case map[string]any:
				if id := stringValue(typed, "id", "disk_id", "device"); id != "" {
					result = append(result, id)
				}
			}
		}
		break
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
