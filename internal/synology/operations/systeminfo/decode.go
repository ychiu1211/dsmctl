package systeminfo

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type fieldAliases struct {
	hostname    []string
	version     []string
	memory      []string
	uptime      []string
	timeZone    []string
	temperature []string
}

var currentFields = fieldAliases{
	hostname:    []string{"hostname", "server_name"},
	version:     []string{"firmware_ver", "version_string"},
	memory:      []string{"ram_size", "memory_size"},
	uptime:      []string{"up_time", "uptime"},
	timeZone:    []string{"time_zone", "timezone"},
	temperature: []string{"sys_temp", "temperature"},
}

var legacyFields = fieldAliases{
	hostname:    []string{"server_name", "hostname"},
	version:     []string{"version_string", "firmware_ver"},
	memory:      []string{"memory_size", "ram_size"},
	uptime:      []string{"uptime", "up_time"},
	timeZone:    []string{"timezone", "time_zone"},
	temperature: []string{"temperature", "sys_temp"},
}

func decode(data json.RawMessage, fields fieldAliases) (Info, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return Info{}, fmt.Errorf("decode system info: %w", err)
	}

	cpuParts := compactStrings(
		stringValue(raw, "cpu_vendor"),
		stringValue(raw, "cpu_family"),
		stringValue(raw, "cpu_series"),
	)
	temperature, hasTemperature := floatValue(raw, fields.temperature...)
	var temperaturePointer *float64
	if hasTemperature {
		temperaturePointer = &temperature
	}

	return Info{
		Hostname:        stringValue(raw, fields.hostname...),
		Model:           stringValue(raw, "model"),
		Serial:          stringValue(raw, "serial"),
		DSMVersion:      stringValue(raw, fields.version...),
		CPU:             strings.Join(cpuParts, " "),
		CPUCores:        int(int64Value(raw, "cpu_cores")),
		MemoryMiB:       int64Value(raw, fields.memory...),
		Uptime:          stringValue(raw, fields.uptime...),
		TimeZone:        stringValue(raw, fields.timeZone...),
		TemperatureC:    temperaturePointer,
		TemperatureWarn: boolValue(raw, "sys_tempwarn", "temperature_warning"),
	}, nil
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

func int64Value(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch typed := values[key].(type) {
		case json.Number:
			result, _ := typed.Int64()
			return result
		case float64:
			return int64(typed)
		case string:
			result, _ := strconv.ParseInt(typed, 10, 64)
			return result
		}
	}
	return 0
}

func floatValue(values map[string]any, keys ...string) (float64, bool) {
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
		case string:
			result, _ := strconv.ParseBool(typed)
			return result
		}
	}
	return false
}

func compactStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(result) > 0 && result[len(result)-1] == value {
			continue
		}
		result = append(result, value)
	}
	return result
}
