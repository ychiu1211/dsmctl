package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type TaskSchedulerCapabilitiesResult struct {
	NAS          string                             `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.TaskSchedulerCapabilities `json:"capabilities" jsonschema:"Task Scheduler read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport       `json:"report" jsonschema:"Discovered APIs and selected Task Scheduler compatibility backends"`
}

type TaskSchedulerScheduledResult struct {
	NAS   string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.TaskSchedulerScheduledTasks `json:"tasks" jsonschema:"Scheduled-task inventory metadata; never a task's command or script body"`
}

type TaskSchedulerTriggeredResult struct {
	NAS   string                               `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.TaskSchedulerTriggeredTasks `json:"tasks" jsonschema:"Triggered-task inventory metadata; never a task's command or script body"`
}

func (s *Service) GetTaskSchedulerCapabilities(ctx context.Context, requestedNAS string) (TaskSchedulerCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return TaskSchedulerCapabilitiesResult{}, err
	}
	capabilities, report, err := client.TaskSchedulerCapabilities(ctx)
	if err != nil {
		return TaskSchedulerCapabilitiesResult{}, authenticationError(name, err)
	}
	return TaskSchedulerCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetTaskSchedulerScheduled(ctx context.Context, requestedNAS string) (TaskSchedulerScheduledResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return TaskSchedulerScheduledResult{}, err
	}
	tasks, err := client.TaskSchedulerScheduled(ctx)
	if err != nil {
		return TaskSchedulerScheduledResult{}, authenticationError(name, err)
	}
	return TaskSchedulerScheduledResult{NAS: name, Tasks: tasks}, nil
}

func (s *Service) GetTaskSchedulerTriggered(ctx context.Context, requestedNAS string) (TaskSchedulerTriggeredResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return TaskSchedulerTriggeredResult{}, err
	}
	tasks, err := client.TaskSchedulerTriggered(ctx)
	if err != nil {
		return TaskSchedulerTriggeredResult{}, authenticationError(name, err)
	}
	return TaskSchedulerTriggeredResult{NAS: name, Tasks: tasks}, nil
}
