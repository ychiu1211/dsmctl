package utilizationread

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
)

// kbToBytes converts DSM's kilobyte memory counters to bytes.
const kbToBytes = 1024

func decodeUtilization(data json.RawMessage) (resmon.Utilization, error) {
	root, err := rootObject(data, "utilization")
	if err != nil {
		return resmon.Utilization{}, err
	}
	cpu, hasCPU := object(root, "cpu")
	memory, hasMemory := object(root, "memory")
	if !hasCPU && !hasMemory {
		return resmon.Utilization{}, fmt.Errorf("decode utilization: response has no cpu or memory group")
	}

	state := resmon.Utilization{
		RecordingEnabled: boolValue(root, "enable_history"),
	}
	if hasCPU {
		state.CPU = resmon.CPUUtilization{
			Device:        stringValue(cpu, "device"),
			UserPercent:   intValue(cpu, "user_load"),
			SystemPercent: intValue(cpu, "system_load"),
			OtherPercent:  intValue(cpu, "other_load"),
			LoadAverage1:  intValue(cpu, "1min_load"),
			LoadAverage5:  intValue(cpu, "5min_load"),
			LoadAverage15: intValue(cpu, "15min_load"),
		}
	}
	if hasMemory {
		state.Memory = resmon.MemoryUtilization{
			RealUsagePercent: intValue(memory, "real_usage"),
			SwapUsagePercent: intValue(memory, "swap_usage"),
			TotalRealBytes:   int64Value(memory, "total_real") * kbToBytes,
			AvailRealBytes:   int64Value(memory, "avail_real") * kbToBytes,
			CachedBytes:      int64Value(memory, "cached") * kbToBytes,
			BufferBytes:      int64Value(memory, "buffer") * kbToBytes,
			TotalSwapBytes:   int64Value(memory, "total_swap") * kbToBytes,
			AvailSwapBytes:   int64Value(memory, "avail_swap") * kbToBytes,
		}
	}
	state.Network = decodeNetwork(array(root, "network"))
	if disk, ok := object(root, "disk"); ok {
		state.Disk = decodeDisk(disk)
	}
	if space, ok := object(root, "space"); ok {
		state.Volumes = decodeVolumes(array(space, "volume"))
	}
	return state, nil
}

func decodeNetwork(items []map[string]any) []resmon.NetworkInterface {
	interfaces := make([]resmon.NetworkInterface, 0, len(items))
	for _, item := range items {
		interfaces = append(interfaces, resmon.NetworkInterface{
			Device:        stringValue(item, "device"),
			TxBytesPerSec: int64Value(item, "tx"),
			RxBytesPerSec: int64Value(item, "rx"),
		})
	}
	return interfaces
}

func decodeDisk(disk map[string]any) resmon.DiskUtilization {
	utilization := resmon.DiskUtilization{Disks: make([]resmon.NamedDiskIO, 0)}
	if total, ok := object(disk, "total"); ok {
		utilization.Total = decodeDiskIO(total)
	}
	for _, item := range array(disk, "disk") {
		utilization.Disks = append(utilization.Disks, resmon.NamedDiskIO{
			Device:      stringValue(item, "device"),
			DisplayName: stringValue(item, "display_name"),
			DiskIO:      decodeDiskIO(item),
		})
	}
	return utilization
}

func decodeVolumes(items []map[string]any) []resmon.VolumeIO {
	volumes := make([]resmon.VolumeIO, 0, len(items))
	for _, item := range items {
		volumes = append(volumes, resmon.VolumeIO{
			Device:      stringValue(item, "device"),
			DisplayName: stringValue(item, "display_name"),
			DiskIO:      decodeDiskIO(item),
		})
	}
	return volumes
}

func decodeDiskIO(values map[string]any) resmon.DiskIO {
	return resmon.DiskIO{
		ReadOpsPerSec:      int64Value(values, "read_access"),
		WriteOpsPerSec:     int64Value(values, "write_access"),
		ReadBytesPerSec:    int64Value(values, "read_byte"),
		WriteBytesPerSec:   int64Value(values, "write_byte"),
		UtilizationPercent: intValue(values, "utilization"),
	}
}

// historyDimensions maps each DSM history response group to a canonical
// dimension. deviceListKey names the sub-array of per-device objects when the
// group is an object wrapper (disk and space mirror the current-snapshot
// shape); an empty deviceListKey means the group is either a flat metric object
// (cpu, memory) or a top-level device array (network).
var historyDimensions = []struct {
	key           string
	dimension     string
	deviceListKey string
}{
	{"cpu", resmon.DimensionCPU, ""},
	{"memory", resmon.DimensionMemory, ""},
	{"network", resmon.DimensionNetwork, ""},
	{"disk", resmon.DimensionDisk, "disk"},
	{"space", resmon.DimensionVolume, "volume"},
}

// decodeHistory normalizes DSM's recorded-history response into per-metric
// series. DSM returns bare fixed-interval value arrays per metric (grouped by
// resource, with per-device arrays for disk/network/space) and no timestamps,
// so each metric becomes an ordered value slice. The shape is verified against
// DSM 7.3 (WI-021); the decoder stays defensive about which groups are present.
func decodeHistory(data json.RawMessage, input HistoryInput) (resmon.History, error) {
	root, err := rootObject(data, "utilization history")
	if err != nil {
		return resmon.History{}, err
	}
	history := resmon.History{Period: input.Period, Series: make([]resmon.HistorySeries, 0)}

	recognizedGroup := false
	for _, dimension := range historyDimensions {
		value, ok := root[dimension.key]
		if !ok || value == nil {
			continue
		}
		recognizedGroup = true
		switch typed := value.(type) {
		case map[string]any:
			if dimension.deviceListKey != "" {
				for _, device := range array(typed, dimension.deviceListKey) {
					name := stringValue(device, "device", "display_name")
					history.Series = append(history.Series, seriesFromObject(dimension.dimension, name, device)...)
				}
				continue
			}
			history.Series = append(history.Series, seriesFromObject(dimension.dimension, "", typed)...)
		case []any:
			for _, element := range typed {
				device, deviceOK := element.(map[string]any)
				if !deviceOK {
					continue
				}
				name := stringValue(device, "device", "display_name")
				history.Series = append(history.Series, seriesFromObject(dimension.dimension, name, device)...)
			}
		}
	}
	// A response with none of the requested resource groups is malformed. An
	// enabled-but-not-yet-populated window legitimately yields recognized groups
	// with empty value arrays, so an empty series list is only an error when no
	// group was present at all.
	if !recognizedGroup {
		return resmon.History{}, fmt.Errorf("decode utilization history: response carried no recognized resource group")
	}
	return history, nil
}

// seriesFromObject builds one HistorySeries per numeric-array field of an
// object. Non-array fields (device labels, scalars) are skipped.
func seriesFromObject(dimension, device string, values map[string]any) []resmon.HistorySeries {
	metrics := make([]string, 0, len(values))
	for key, value := range values {
		if _, ok := value.([]any); ok {
			metrics = append(metrics, key)
		}
	}
	sort.Strings(metrics)
	series := make([]resmon.HistorySeries, 0, len(metrics))
	for _, metric := range metrics {
		series = append(series, resmon.HistorySeries{
			Dimension: dimension,
			Device:    device,
			Metric:    metric,
			Values:    valuesFrom(values[metric].([]any)),
		})
	}
	return series
}

func valuesFrom(raw []any) []float64 {
	values := make([]float64, 0, len(raw))
	for _, element := range raw {
		if value, ok := numberValue(element); ok {
			values = append(values, value)
		}
	}
	return values
}

// --- shared decoding helpers -------------------------------------------------

func rootObject(data json.RawMessage, label string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	if root == nil {
		return nil, fmt.Errorf("decode %s: response is not an object", label)
	}
	return root, nil
}

func object(values map[string]any, key string) (map[string]any, bool) {
	value, ok := values[key].(map[string]any)
	return value, ok
}

// array returns the named field as a slice of objects, ignoring non-object
// elements.
func array(values map[string]any, key string) []map[string]any {
	raw, ok := values[key].([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(raw))
	for _, element := range raw {
		if object, ok := element.(map[string]any); ok {
			items = append(items, object)
		}
	}
	return items
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func intValue(values map[string]any, keys ...string) int {
	return int(int64Value(values, keys...))
}

func int64Value(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := numberValue(values[key]); ok {
			return int64(value)
		}
	}
	return 0
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed, true
		}
	case float64:
		return typed, true
	}
	return 0, false
}

func boolValue(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case bool:
			return typed
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return parsed != 0
			}
		}
	}
	return false
}
