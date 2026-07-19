// Package downloadstation implements read operations for the Synology Download
// Station package: service configuration (SYNO.DownloadStation.Info +
// .Schedule), the download task list (SYNO.DownloadStation.Task list), and
// transfer statistics (SYNO.DownloadStation.Statistic). Every variant is gated
// on the installed DownloadStation package so a NAS without it fails closed. The
// legacy SYNO.DownloadStation.* APIs are used because they are stable and
// publicly documented; each is served from its own CGI path, which the client
// resolves from the discovered API registry.
package downloadstation

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// PackageID is the DSM package that owns the Download Station APIs.
const PackageID = "DownloadStation"

const (
	InfoAPIName      = "SYNO.DownloadStation.Info"
	ScheduleAPIName  = "SYNO.DownloadStation.Schedule"
	StatisticAPIName = "SYNO.DownloadStation.Statistic"
	TaskAPIName      = "SYNO.DownloadStation.Task"

	ServiceReadCapabilityName   = "download.service.read"
	TaskReadCapabilityName      = "download.task.read"
	StatisticReadCapabilityName = "download.statistic.read"
)

// baselinePackage gates every variant on Download Station 3.x+, covering the
// stable legacy Info/Task/Statistic/Schedule surface (verified on 4.1.2).
var baselinePackage = compatibility.PackageVersionRange(
	PackageID, compatibility.ParsePackageVersion("3.0"), compatibility.PackageVersion{},
)

type Input struct{}

var serviceOperation = compatibility.Operation[Input, downloadstation.ServiceState]{
	Name: ServiceReadCapabilityName,
	Variants: []compatibility.Variant[Input, downloadstation.ServiceState]{
		{
			Name: "downloadstation-service-v1", API: InfoAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(InfoAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (downloadstation.ServiceState, error) {
				infoData, err := executor.Execute(ctx, compatibility.Request{API: InfoAPIName, Version: 1, Method: "getinfo"})
				if err != nil {
					return downloadstation.ServiceState{}, fmt.Errorf("call %s.getinfo: %w", InfoAPIName, err)
				}
				info, err := decodeInfo(infoData)
				if err != nil {
					return downloadstation.ServiceState{}, err
				}
				configData, err := executor.Execute(ctx, compatibility.Request{API: InfoAPIName, Version: 1, Method: "getconfig"})
				if err != nil {
					return downloadstation.ServiceState{}, fmt.Errorf("call %s.getconfig: %w", InfoAPIName, err)
				}
				config, err := decodeConfig(configData)
				if err != nil {
					return downloadstation.ServiceState{}, err
				}
				scheduleData, err := executor.Execute(ctx, compatibility.Request{API: ScheduleAPIName, Version: 1, Method: "getconfig"})
				if err != nil {
					return downloadstation.ServiceState{}, fmt.Errorf("call %s.getconfig: %w", ScheduleAPIName, err)
				}
				schedule, err := decodeSchedule(scheduleData)
				if err != nil {
					return downloadstation.ServiceState{}, err
				}
				return downloadstation.ServiceState{
					Version:   info.Version,
					IsManager: info.IsManager,
					Config:    config,
					Schedule:  schedule,
				}, nil
			},
		},
	},
}

var taskOperation = compatibility.Operation[Input, downloadstation.Tasks]{
	Name: TaskReadCapabilityName,
	Variants: []compatibility.Variant[Input, downloadstation.Tasks]{
		{
			Name: "downloadstation-task-list-v1", API: TaskAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(TaskAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (downloadstation.Tasks, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: TaskAPIName, Version: 1, Method: "list",
					Parameters: url.Values{"additional": {"detail,transfer"}},
				})
				if err != nil {
					return downloadstation.Tasks{}, fmt.Errorf("call %s.list: %w", TaskAPIName, err)
				}
				return decodeTasks(data)
			},
		},
	},
}

var statisticOperation = compatibility.Operation[Input, downloadstation.Statistics]{
	Name: StatisticReadCapabilityName,
	Variants: []compatibility.Variant[Input, downloadstation.Statistics]{
		{
			Name: "downloadstation-statistic-v1", API: StatisticAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(StatisticAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (downloadstation.Statistics, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: StatisticAPIName, Version: 1, Method: "getinfo"})
				if err != nil {
					return downloadstation.Statistics{}, fmt.Errorf("call %s.getinfo: %w", StatisticAPIName, err)
				}
				return decodeStatistics(data)
			},
		},
	},
}

func APINames() []string {
	return []string{InfoAPIName, ScheduleAPIName, StatisticAPIName, TaskAPIName}
}

func SelectService(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := serviceOperation.Select(target)
	return selection, err
}

func SelectTask(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := taskOperation.Select(target)
	return selection, err
}

func SelectStatistic(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := statisticOperation.Select(target)
	return selection, err
}

func ExecuteService(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (downloadstation.ServiceState, compatibility.Selection, error) {
	return serviceOperation.Run(ctx, target, executor, Input{})
}

func ExecuteTask(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (downloadstation.Tasks, compatibility.Selection, error) {
	return taskOperation.Run(ctx, target, executor, Input{})
}

func ExecuteStatistic(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (downloadstation.Statistics, compatibility.Selection, error) {
	return statisticOperation.Run(ctx, target, executor, Input{})
}
