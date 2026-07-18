package surveillance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ychiu1211/dsmctl/internal/domain/surveillance"
)

func decodeInfo(data json.RawMessage) (surveillance.Info, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return surveillance.Info{}, err
	}
	return surveillance.Info{
		Version:          decodeVersion(raw),
		Hostname:         firstString(raw, "hostname", "host_name"),
		CameraNumber:     firstInt(raw, "cameraNumber", "camera_number"),
		MaxCameraSupport: firstInt(raw, "maxCameraSupport", "max_camera_support"),
		LicenseNumber:    firstInt(raw, "liscenseNumber", "licenseNumber", "license_number"),
		Timezone:         firstString(raw, "timezone", "time_zone"),
	}, nil
}

func decodeCameras(data json.RawMessage) (surveillance.Cameras, error) {
	raw, err := decodeObject(data)
	if err != nil {
		return surveillance.Cameras{}, err
	}
	list := surveillance.Cameras{Cameras: []surveillance.Camera{}}
	list.Total = firstInt(raw, "total")
	if camsRaw, ok := raw["cameras"]; ok {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(camsRaw, &items); err == nil {
			for _, item := range items {
				list.Cameras = append(list.Cameras, surveillance.Camera{
					ID:      firstInt(item, "id", "cameraId"),
					Name:    firstString(item, "newName", "name", "camName"),
					IP:      firstString(item, "ip", "host"),
					Port:    firstInt(item, "port"),
					Vendor:  firstString(item, "vendor"),
					Model:   firstString(item, "model"),
					Status:  firstInt(item, "status"),
					Enabled: firstBool(item, "enabled"),
					RecTime: int64(firstInt(item, "recStatus", "recording_status")),
				})
			}
		}
	}
	if list.Total == 0 {
		list.Total = len(list.Cameras)
	}
	return list, nil
}

// decodeVersion renders the version object as "major.minor.small-build". Live
// DSM 7.3.2 / Surveillance 9.2.5 returns the parts as strings ({"major":"9",
// "minor":"2","small":"5","build":"11979"}); a plain string value is also
// tolerated.
func decodeVersion(raw map[string]json.RawMessage) string {
	value, ok := raw["version"]
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil && text != "" {
		return text
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(value, &obj); err != nil {
		return ""
	}
	major := numericString(obj, "major")
	minor := numericString(obj, "minor")
	small := numericString(obj, "small")
	build := numericString(obj, "build")
	if major == "" && minor == "" && small == "" && build == "" {
		return ""
	}
	base := major + "." + minor
	if small != "" {
		base += "." + small
	}
	if build != "" {
		base += "-" + build
	}
	return base
}

// numericString reads a field that may be a JSON string or number as a string.
func numericString(raw map[string]json.RawMessage, name string) string {
	value, ok := raw[name]
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return text
	}
	var n int
	if err := json.Unmarshal(value, &n); err == nil {
		return strconv.Itoa(n)
	}
	return ""
}

func decodeObject(data json.RawMessage) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("decode Surveillance response: expected a non-empty object")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode Surveillance response: %w", err)
	}
	return raw, nil
}

func firstString(raw map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			var text string
			if err := json.Unmarshal(value, &text); err == nil && text != "" {
				return text
			}
		}
	}
	return ""
}

func firstInt(raw map[string]json.RawMessage, names ...string) int {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			var n int
			if err := json.Unmarshal(value, &n); err == nil {
				return n
			}
		}
	}
	return 0
}

func firstBool(raw map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if value, ok := raw[name]; ok {
			var b bool
			if err := json.Unmarshal(value, &b); err == nil {
				return b
			}
			var n int
			if err := json.Unmarshal(value, &n); err == nil {
				return n == 1
			}
		}
	}
	return false
}
