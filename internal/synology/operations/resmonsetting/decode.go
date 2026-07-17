package resmonsetting

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/resmon"
)

func decode(data json.RawMessage) (Settings, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return Settings{}, fmt.Errorf("decode resource monitor setting: %w", err)
	}
	if root == nil {
		return Settings{}, fmt.Errorf("decode resource monitor setting: response is not an object")
	}
	raw, ok := root[RecordingField]
	if !ok {
		return Settings{}, fmt.Errorf("decode resource monitor setting: response did not report %q", RecordingField)
	}
	return Settings{
		Recording: resmon.RecordingSetting{Enabled: boolValue(raw)},
		Raw:       root,
	}, nil
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed != 0
		}
	}
	return false
}
