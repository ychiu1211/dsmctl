package securityadvisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/securityadvisor"
)

// The decoder is strict about the response envelope and about severity values
// (an unrecognized severity errors rather than being silently coerced), and
// lenient about optional numeric fields that vary across releases. It carries
// only descriptive audit output into the model — no session identity.

// severityFromDSM maps DSM's raw severity strings to the stable domain enum. An
// unrecognized value is an error, per the module contract, so a taxonomy change
// in a future DSM surfaces loudly instead of being coerced to a safe-looking
// value.
func severityFromDSM(raw string) (securityadvisor.Severity, error) {
	switch strings.TrimSpace(raw) {
	case "safe":
		return securityadvisor.SeveritySafe, nil
	case "info":
		return securityadvisor.SeverityInfo, nil
	case "warning":
		return securityadvisor.SeverityWarning, nil
	case "outOfDate":
		return securityadvisor.SeverityOutOfDate, nil
	case "risk":
		return securityadvisor.SeverityRisk, nil
	case "danger":
		return securityadvisor.SeverityDanger, nil
	default:
		return "", fmt.Errorf("unrecognized security-advisor severity %q", raw)
	}
}

func decodeStatus(data json.RawMessage) (securityadvisor.Status, error) {
	root, err := decodeObject(data, "security advisor status")
	if err != nil {
		return securityadvisor.Status{}, err
	}
	items, ok := root["items"].(map[string]any)
	if !ok {
		return securityadvisor.Status{}, fmt.Errorf("decode security advisor status: no items object among %s", availableKeys(root))
	}

	overall, err := severityFromDSM(stringValue(root, "sysStatus"))
	if err != nil {
		return securityadvisor.Status{}, fmt.Errorf("decode security advisor status: %w", err)
	}

	status := securityadvisor.Status{
		Progress:        intValue(root, "sysProgress"),
		OverallSeverity: overall,
		StartTime:       stringValue(root, "startTime"),
		LastScanTime:    parseUnix(stringValue(root, "lastScanTime")),
		Categories:      make([]securityadvisor.CategoryResult, 0, len(items)),
	}

	for name, raw := range items {
		object, ok := raw.(map[string]any)
		if !ok {
			return securityadvisor.Status{}, fmt.Errorf("decode security advisor status: category %q is not an object", name)
		}
		category, err := decodeCategory(name, object)
		if err != nil {
			return securityadvisor.Status{}, err
		}
		status.Categories = append(status.Categories, category)
		status.TotalChecks += category.Total
		status.TotalFindings += category.Findings
		status.Counts.Danger += category.Counts.Danger
		status.Counts.Risk += category.Counts.Risk
		status.Counts.Warning += category.Counts.Warning
		status.Counts.OutOfDate += category.Counts.OutOfDate
		status.Counts.Info += category.Counts.Info
		if category.Progress < 100 || category.WaitNum > 0 || category.RunningItem != "" {
			status.Running = true
		}
	}
	// A scan in progress is authoritatively signalled by the overall progress; a
	// non-empty start time is a secondary indicator DSM sets while scanning.
	if status.Progress < 100 || status.StartTime != "" {
		status.Running = true
	}

	sortCategories(status.Categories)
	return status, nil
}

func decodeCategory(name string, object map[string]any) (securityadvisor.CategoryResult, error) {
	failSeverity, err := severityFromDSM(stringValue(object, "failSeverity"))
	if err != nil {
		return securityadvisor.CategoryResult{}, fmt.Errorf("decode security advisor category %q: %w", name, err)
	}
	category := stringValue(object, "category")
	if category == "" {
		category = name
	}
	result := securityadvisor.CategoryResult{
		Category:     category,
		Total:        intValue(object, "total"),
		FailSeverity: failSeverity,
		Progress:     intValue(object, "progress"),
		RunningItem:  stringValue(object, "runningItem"),
		WaitNum:      intValue(object, "waitNum"),
	}
	if fail, ok := object["fail"].(map[string]any); ok {
		result.Counts = securityadvisor.SeverityCounts{
			Danger:    intValue(fail, "danger"),
			Risk:      intValue(fail, "risk"),
			Warning:   intValue(fail, "warning"),
			OutOfDate: intValue(fail, "outOfDate"),
			Info:      intValue(fail, "info"),
		}
	}
	result.Findings = result.Counts.Total()
	result.Passed = result.Total - result.Findings
	if result.Passed < 0 {
		result.Passed = 0
	}
	return result, nil
}

func decodeConfiguration(data json.RawMessage) (securityadvisor.Configuration, error) {
	root, err := decodeObject(data, "security advisor configuration")
	if err != nil {
		return securityadvisor.Configuration{}, err
	}
	baseline := stringValue(root, "defaultGroup")
	if baseline == "" {
		return securityadvisor.Configuration{}, fmt.Errorf("decode security advisor configuration: no defaultGroup among %s", availableKeys(root))
	}
	enabled, _ := boolValue(root, "enableSchedule")
	return securityadvisor.Configuration{
		Baseline: baseline,
		Schedule: securityadvisor.Schedule{
			Enabled: enabled,
			Hour:    intValue(root, "hour"),
			Minute:  intValue(root, "minute"),
			Weekday: stringValue(root, "weekday"),
			TaskID:  intValue(root, "scheduleTaskId"),
		},
	}, nil
}

// sortCategories orders categories by descending severity then ascending name so
// the most severe findings surface first and the order is deterministic.
func sortCategories(categories []securityadvisor.CategoryResult) {
	sort.Slice(categories, func(i, j int) bool {
		ri, rj := categories[i].FailSeverity.Rank(), categories[j].FailSeverity.Rank()
		if ri != rj {
			return ri > rj
		}
		return categories[i].Category < categories[j].Category
	})
}

func parseUnix(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return parsed
	}
	return 0
}

func decodeObject(data json.RawMessage, what string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode %s: %w", what, err)
	}
	if root == nil {
		return nil, fmt.Errorf("decode %s: response is not an object", what)
	}
	return root, nil
}

func stringValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func intValue(values map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return int(parsed)
			}
		case float64:
			return int(typed)
		}
	}
	return 0
}

func boolValue(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		if typed, ok := value.(bool); ok {
			return typed, true
		}
	}
	return false, false
}

func availableKeys(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return "[" + strings.Join(keys, ", ") + "]"
}
