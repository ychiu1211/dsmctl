package synology

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/syslogread"
)

type LogState = syslog.State
type LogCapabilities = syslog.Capabilities
type LogStateQuery = syslog.StateQuery

const (
	defaultLogLimit = 100
	maxLogLimit     = 1000
)

// LogState reads a page of normalized DSM system log entries. Keyword and log
// type are applied by DSM; the optional severity level is applied here because
// DSM exposes no stable server-side level filter.
func (c *Client) LogState(ctx context.Context, query syslog.StateQuery) (LogState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, syslogread.APINames()...); err != nil {
		return LogState{}, fmt.Errorf("prepare log target: %w", err)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultLogLimit
	}
	if limit > maxLogLimit {
		limit = maxLogLimit
	}
	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	state, selection, err := syslogread.Execute(ctx, c.target, lockedExecutor{client: c}, syslogread.Input{
		Limit: limit, Offset: offset, Keyword: strings.TrimSpace(query.Keyword), LogType: strings.TrimSpace(query.LogType),
		DateFrom: query.From, DateTo: query.To,
	})
	if err != nil {
		return LogState{}, fmt.Errorf("read DSM logs: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(syslogread.CapabilityName)
	}
	if level := normalizeLogLevel(query.Level); level != "" {
		filtered := make([]syslog.Entry, 0, len(state.Entries))
		for _, entry := range state.Entries {
			if entry.Level == level {
				filtered = append(filtered, entry)
			}
		}
		state.Entries = filtered
	}
	return state, nil
}

// LogCapabilities reports whether DSM log reading is available and the selected
// backend.
func (c *Client) LogCapabilities(ctx context.Context) (LogCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, syslogread.APINames()...); err != nil {
		return LogCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare log capabilities target: %w", err)
	}
	selection, err := syslogread.Select(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return LogCapabilities{}, CompatibilityReport{}, fmt.Errorf("select log backend: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(syslogread.CapabilityName)
	}
	return LogCapabilities{Read: selection.Supported}, c.target.Report(selection), nil
}

func normalizeLogLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "info", "information", "notice":
		return syslog.LevelInfo
	case "warn", "warning":
		return syslog.LevelWarning
	case "err", "error", "crit", "critical":
		return syslog.LevelError
	default:
		return strings.TrimSpace(value)
	}
}
