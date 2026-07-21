package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/taskscheduler"
	tsops "github.com/ychiu1211/dsmctl/internal/synology/operations/taskscheduler"
)

type TaskSchedulerScheduledTasks = taskscheduler.ScheduledTasks
type TaskSchedulerTriggeredTasks = taskscheduler.TriggeredTasks
type TaskSchedulerCapabilities = taskscheduler.Capabilities

// TaskSchedulerScheduled reads the scheduled-task inventory (Control Panel > Task
// Scheduler): user-defined scripts, service-control tasks, and built-in
// maintenance tasks. It returns inventory metadata only — never a task's command
// or script body — and decodes no credential.
func (c *Client) TaskSchedulerScheduled(ctx context.Context) (TaskSchedulerScheduledTasks, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, tsops.APINames()...); err != nil {
		return TaskSchedulerScheduledTasks{}, fmt.Errorf("prepare task scheduler target: %w", err)
	}
	tasks, _, err := tsops.ExecuteScheduled(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return TaskSchedulerScheduledTasks{}, fmt.Errorf("get scheduled tasks: %w", err)
	}
	c.target.AddCapability(tsops.ScheduledReadCapabilityName)
	return tasks, nil
}

// TaskSchedulerTriggered reads the triggered-task inventory (boot-up, shutdown,
// and event-triggered tasks). Metadata only; no command body or credential.
func (c *Client) TaskSchedulerTriggered(ctx context.Context) (TaskSchedulerTriggeredTasks, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, tsops.APINames()...); err != nil {
		return TaskSchedulerTriggeredTasks{}, fmt.Errorf("prepare task scheduler target: %w", err)
	}
	tasks, _, err := tsops.ExecuteTriggered(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return TaskSchedulerTriggeredTasks{}, fmt.Errorf("get triggered tasks: %w", err)
	}
	c.target.AddCapability(tsops.TriggeredReadCapabilityName)
	return tasks, nil
}

// TaskSchedulerCapabilities reports which Task Scheduler reads dsmctl exposes for
// the selected NAS. Scheduled and triggered tasks are independent DSM API
// families, each selected on its own so one being absent still reports the other.
func (c *Client) TaskSchedulerCapabilities(ctx context.Context) (TaskSchedulerCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, tsops.APINames()...); err != nil {
		return TaskSchedulerCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare task scheduler capabilities target: %w", err)
	}

	scheduled, err := selectSupported(tsops.SelectScheduled, c.target)
	if err != nil {
		return TaskSchedulerCapabilities{}, CompatibilityReport{}, fmt.Errorf("select scheduled tasks backend: %w", err)
	}
	triggered, err := selectSupported(tsops.SelectTriggered, c.target)
	if err != nil {
		return TaskSchedulerCapabilities{}, CompatibilityReport{}, fmt.Errorf("select triggered tasks backend: %w", err)
	}

	if scheduled.Supported {
		c.target.AddCapability(tsops.ScheduledReadCapabilityName)
	}
	if triggered.Supported {
		c.target.AddCapability(tsops.TriggeredReadCapabilityName)
	}

	capabilities := TaskSchedulerCapabilities{
		Module:        taskscheduler.ModuleName,
		ScheduledRead: scheduled.Supported,
		TriggeredRead: triggered.Supported,
		// The scheduled-task detail (get) method lives on the same
		// SYNO.Core.TaskScheduler API as the list, so its presence tracks the
		// scheduled read. Detail (the script body) is a deferred follow-on and is
		// not surfaced this pass; this only advertises that the backend exists.
		DetailAvailable:          scheduled.Supported,
		TaskFieldsWireUnverified: true,
	}
	return capabilities, c.target.Report(scheduled, triggered), nil
}
