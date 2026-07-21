// Package taskscheduler implements independently selectable, read-only DSM
// operations for the Control Panel > Task Scheduler surface. Scheduled tasks and
// triggered tasks are two distinct DSM API families and two separate
// compatibility boundaries: a NAS missing one is reported unsupported for that
// area without disabling the other, and absence fails closed for that area.
//
// Live-verified on DSM 7.3 (lab), WI-073 Slice A:
//   - Scheduled inventory: SYNO.Core.TaskScheduler (versions 1-3), method "list",
//     data envelope {"tasks": [...], "total": N}. The task detail path
//     (method "get", keyed by integer "id") is advertised on the same API but is
//     NOT surfaced this pass (the script body is the deferred, HIGH-risk
//     follow-on); its presence is only capability-detected.
//   - Triggered inventory: SYNO.Core.EventScheduler (version 1), method "list",
//     data is a BARE JSON array (no {tasks,total} envelope).
//
// The lab had no tasks configured, so the envelopes are live-verified but the
// per-item field names are sourced from DSM's stable token vocabulary and decoded
// tolerantly; see the domain package doc and Capabilities.TaskFieldsWireUnverified.
// No write, run, create, edit, or delete operation is implemented here.
package taskscheduler

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/taskscheduler"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	// ScheduledAPIName is the scheduled-task family (Control Panel > Task
	// Scheduler > scheduled tasks). Advertised versions 1-3 on the lab.
	ScheduledAPIName = "SYNO.Core.TaskScheduler"
	// TriggeredAPIName is the triggered-task family (boot-up / shutdown / event
	// tasks). Advertised version 1 only on the lab.
	TriggeredAPIName = "SYNO.Core.EventScheduler"

	ScheduledReadCapabilityName = "task_scheduler.scheduled.read"
	TriggeredReadCapabilityName = "task_scheduler.triggered.read"
	DetailReadCapabilityName    = "task_scheduler.detail.read"
)

// Input is the empty request the inventory reads take.
type Input struct{}

var scheduledListOp = compatibility.Operation[Input, taskscheduler.ScheduledTasks]{
	Name: ScheduledReadCapabilityName,
	Variants: []compatibility.Variant[Input, taskscheduler.ScheduledTasks]{
		scheduledListVariant("task-scheduler-list-v3", 3, 30),
		scheduledListVariant("task-scheduler-list-v2", 2, 20),
		scheduledListVariant("task-scheduler-list-v1", 1, 10),
	},
}

func scheduledListVariant(name string, version, priority int) compatibility.Variant[Input, taskscheduler.ScheduledTasks] {
	return compatibility.Variant[Input, taskscheduler.ScheduledTasks]{
		Name: name, API: ScheduledAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(ScheduledAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (taskscheduler.ScheduledTasks, error) {
			data, err := executor.Execute(ctx, compatibility.Request{
				API: ScheduledAPIName, Version: version, Method: "list", ReadOnly: true,
			})
			if err != nil {
				return taskscheduler.ScheduledTasks{}, fmt.Errorf("call %s.list v%d: %w", ScheduledAPIName, version, err)
			}
			return decodeScheduledTasks(data)
		},
	}
}

var triggeredListOp = compatibility.Operation[Input, taskscheduler.TriggeredTasks]{
	Name: TriggeredReadCapabilityName,
	Variants: []compatibility.Variant[Input, taskscheduler.TriggeredTasks]{
		{
			Name: "event-scheduler-list-v1", API: TriggeredAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(TriggeredAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (taskscheduler.TriggeredTasks, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: TriggeredAPIName, Version: 1, Method: "list", ReadOnly: true,
				})
				if err != nil {
					return taskscheduler.TriggeredTasks{}, fmt.Errorf("call %s.list v1: %w", TriggeredAPIName, err)
				}
				return decodeTriggeredTasks(data)
			},
		},
	},
}

// APINames lists every DSM API this module reads so the facade can discover them
// in one query before selecting any area.
func APINames() []string {
	return []string{ScheduledAPIName, TriggeredAPIName}
}

// SelectScheduled reports the scheduled-inventory read selection.
func SelectScheduled(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := scheduledListOp.Select(target)
	return selection, err
}

// SelectTriggered reports the triggered-inventory read selection.
func SelectTriggered(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := triggeredListOp.Select(target)
	return selection, err
}

// ExecuteScheduled reads the scheduled-task inventory.
func ExecuteScheduled(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (taskscheduler.ScheduledTasks, compatibility.Selection, error) {
	return scheduledListOp.Run(ctx, target, executor, Input{})
}

// ExecuteTriggered reads the triggered-task inventory.
func ExecuteTriggered(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (taskscheduler.TriggeredTasks, compatibility.Selection, error) {
	return triggeredListOp.Run(ctx, target, executor, Input{})
}
