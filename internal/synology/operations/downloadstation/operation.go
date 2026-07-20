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
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

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

	// The detailed settings live on the newer DownloadStation2 API generation
	// (all served from entry.cgi).
	SettingsGlobalAPIName         = "SYNO.DownloadStation2.Settings.Global"
	SettingsBTAPIName             = "SYNO.DownloadStation2.Settings.BT"
	SettingsEmuleAPIName          = "SYNO.DownloadStation2.Settings.Emule"
	SettingsEmuleLocationAPIName  = "SYNO.DownloadStation2.Settings.Emule.Location"
	SettingsFtpHttpAPIName        = "SYNO.DownloadStation2.Settings.FtpHttp"
	SettingsNzbAPIName            = "SYNO.DownloadStation2.Settings.Nzb"
	SettingsAutoExtractionAPIName = "SYNO.DownloadStation2.Settings.AutoExtraction"
	SettingsLocationAPIName       = "SYNO.DownloadStation2.Settings.Location"
	SettingsRssAPIName            = "SYNO.DownloadStation2.Settings.Rss"
	SettingsSchedulerAPIName      = "SYNO.DownloadStation2.Settings.Scheduler"

	ServiceReadCapabilityName   = "download.service.read"
	TaskReadCapabilityName      = "download.task.read"
	StatisticReadCapabilityName = "download.statistic.read"
	SettingsReadCapabilityName  = "download.settings.read"
	TaskWriteCapabilityName     = "download.task.write"
	TaskEditCapabilityName      = "download.task.edit"
	SettingsWriteCapabilityName = "download.settings.write"
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

// getSetting fetches and decodes one DownloadStation2.Settings.* API.
func getSetting[T any](ctx context.Context, executor compatibility.Executor, api string, version int, decode func(json.RawMessage) (T, error)) (T, error) {
	var zero T
	data, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: "get"})
	if err != nil {
		return zero, fmt.Errorf("call %s.get: %w", api, err)
	}
	return decode(data)
}

// settingsOperation composes the detailed DownloadStation2.Settings.* reads into
// one normalized Settings value. It is gated on the Settings.Global API (which
// the DownloadStation package always registers) plus the package baseline.
var settingsOperation = compatibility.Operation[Input, downloadstation.Settings]{
	Name: SettingsReadCapabilityName,
	Variants: []compatibility.Variant[Input, downloadstation.Settings]{
		{
			Name: "downloadstation2-settings-v1", API: SettingsGlobalAPIName, Version: 2, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SettingsGlobalAPIName, 2), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (downloadstation.Settings, error) {
				var s downloadstation.Settings
				var err error
				if s.Global, err = getSetting(ctx, executor, SettingsGlobalAPIName, 2, decodeGlobalSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				if s.BT, err = getSetting(ctx, executor, SettingsBTAPIName, 1, decodeBTSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				emuleEnabled, err := getSetting(ctx, executor, SettingsEmuleAPIName, 1, decodeEmuleSettings)
				if err != nil {
					return downloadstation.Settings{}, err
				}
				emuleDest, err := getSetting(ctx, executor, SettingsEmuleLocationAPIName, 1, func(d json.RawMessage) (string, error) {
					return decodeDefaultDestination(d, "Download Station eMule location settings")
				})
				if err != nil {
					return downloadstation.Settings{}, err
				}
				s.Emule = downloadstation.EmuleSettings{Enabled: emuleEnabled, DefaultDestination: emuleDest}
				if s.FtpHttp, err = getSetting(ctx, executor, SettingsFtpHttpAPIName, 1, decodeFtpHttpSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				if s.Nzb, err = getSetting(ctx, executor, SettingsNzbAPIName, 1, decodeNzbSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				if s.AutoExtraction, err = getSetting(ctx, executor, SettingsAutoExtractionAPIName, 1, decodeAutoExtractionSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				if s.Location, err = getSetting(ctx, executor, SettingsLocationAPIName, 1, decodeLocationSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				if s.Rss, err = getSetting(ctx, executor, SettingsRssAPIName, 1, decodeRssSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				if s.Scheduler, err = getSetting(ctx, executor, SettingsSchedulerAPIName, 1, decodeSchedulerSettings); err != nil {
					return downloadstation.Settings{}, err
				}
				return s, nil
			},
		},
	},
}

func APINames() []string {
	return []string{
		InfoAPIName, ScheduleAPIName, StatisticAPIName, TaskAPIName,
		SettingsGlobalAPIName, SettingsBTAPIName, SettingsEmuleAPIName, SettingsEmuleLocationAPIName,
		SettingsFtpHttpAPIName, SettingsNzbAPIName, SettingsAutoExtractionAPIName, SettingsLocationAPIName,
		SettingsRssAPIName, SettingsSchedulerAPIName,
	}
}

func SelectSettings(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := settingsOperation.Select(target)
	return selection, err
}

func ExecuteSettings(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (downloadstation.Settings, compatibility.Selection, error) {
	return settingsOperation.Run(ctx, target, executor, Input{})
}

// taskWriteOp performs a guarded task mutation via the legacy Task API v1
// (methods create/pause/resume/delete, params live-verified on 4.1.2).
var taskWriteOp = compatibility.Operation[downloadstation.TaskChange, downloadstation.TaskMutationResult]{
	Name: TaskWriteCapabilityName,
	Variants: []compatibility.Variant[downloadstation.TaskChange, downloadstation.TaskMutationResult]{
		{
			Name: "downloadstation-task-write-v1", API: TaskAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(TaskAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, change downloadstation.TaskChange) (downloadstation.TaskMutationResult, error) {
				result := downloadstation.TaskMutationResult{API: TaskAPIName, Version: 1, AffectedIDs: []string{}}
				switch change.Action {
				case downloadstation.TaskActionCreate:
					params := url.Values{"uri": {strings.Join(change.URIs, ",")}}
					if strings.TrimSpace(change.Destination) != "" {
						params.Set("destination", strings.TrimSpace(change.Destination))
					}
					if _, err := executor.Execute(ctx, compatibility.Request{API: TaskAPIName, Version: 1, Method: "create", Parameters: params}); err != nil {
						return downloadstation.TaskMutationResult{}, fmt.Errorf("call %s.create: %w", TaskAPIName, err)
					}
					result.Method = "create"
					return result, nil
				case downloadstation.TaskActionPause, downloadstation.TaskActionResume:
					method := string(change.Action)
					data, err := executor.Execute(ctx, compatibility.Request{API: TaskAPIName, Version: 1, Method: method, Parameters: url.Values{"id": {strings.Join(change.TaskIDs, ",")}}})
					if err != nil {
						return downloadstation.TaskMutationResult{}, fmt.Errorf("call %s.%s: %w", TaskAPIName, method, err)
					}
					affected, err := decodeTaskControlResult(data)
					if err != nil {
						return downloadstation.TaskMutationResult{}, err
					}
					result.Method, result.AffectedIDs = method, affected
					return result, nil
				case downloadstation.TaskActionDelete:
					force := "false"
					if change.ForceComplete {
						force = "true"
					}
					data, err := executor.Execute(ctx, compatibility.Request{API: TaskAPIName, Version: 1, Method: "delete", Parameters: url.Values{"id": {strings.Join(change.TaskIDs, ",")}, "force_complete": {force}}})
					if err != nil {
						return downloadstation.TaskMutationResult{}, fmt.Errorf("call %s.delete: %w", TaskAPIName, err)
					}
					affected, err := decodeTaskControlResult(data)
					if err != nil {
						return downloadstation.TaskMutationResult{}, err
					}
					result.Method, result.AffectedIDs = "delete", affected
					return result, nil
				default:
					return downloadstation.TaskMutationResult{}, fmt.Errorf("unsupported task action %q", change.Action)
				}
			},
		},
	},
}

func SelectTaskWrite(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := taskWriteOp.Select(target)
	return selection, err
}

// taskEditOp re-targets tasks to a new destination via the legacy Task API
// method `edit`, which exists from version 2 (live-verified on 4.1.2: v1
// returns error 103). It is a separate operation so a NAS advertising only
// Task v1 reports edit unsupported instead of receiving an untested request.
var taskEditOp = compatibility.Operation[downloadstation.TaskChange, downloadstation.TaskMutationResult]{
	Name: TaskEditCapabilityName,
	Variants: []compatibility.Variant[downloadstation.TaskChange, downloadstation.TaskMutationResult]{
		{
			Name: "downloadstation-task-edit-v2", API: TaskAPIName, Version: 2, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(TaskAPIName, 2), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, change downloadstation.TaskChange) (downloadstation.TaskMutationResult, error) {
				params := url.Values{
					"id":          {strings.Join(change.TaskIDs, ",")},
					"destination": {strings.TrimSpace(change.Destination)},
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: TaskAPIName, Version: 2, Method: "edit", Parameters: params})
				if err != nil {
					return downloadstation.TaskMutationResult{}, fmt.Errorf("call %s.edit: %w", TaskAPIName, err)
				}
				// edit responds without a per-task result list when everything
				// succeeded; fall back to the requested ids.
				affected, err := decodeTaskControlResult(data)
				if err != nil || len(affected) == 0 {
					affected = change.TaskIDs
				}
				return downloadstation.TaskMutationResult{API: TaskAPIName, Version: 2, Method: "edit", AffectedIDs: affected}, nil
			},
		},
	},
}

func SelectTaskEdit(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := taskEditOp.Select(target)
	return selection, err
}

func ExecuteTaskEdit(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change downloadstation.TaskChange) (downloadstation.TaskMutationResult, compatibility.Selection, error) {
	result, selection, err := taskEditOp.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
}

// btGetOp reads the current BitTorrent settings so a guarded write can merge a
// patch into the complete object (the set is a full-object replace).
var btGetOp = compatibility.Operation[Input, downloadstation.BTSettings]{
	Name: "download.settings.bt.get",
	Variants: []compatibility.Variant[Input, downloadstation.BTSettings]{
		{
			Name: "downloadstation2-settings-bt-get-v1", API: SettingsBTAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SettingsBTAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (downloadstation.BTSettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SettingsBTAPIName, Version: 1, Method: "get"})
				if err != nil {
					return downloadstation.BTSettings{}, fmt.Errorf("call %s.get: %w", SettingsBTAPIName, err)
				}
				return decodeBTSettings(data)
			},
		},
	},
}

// btSetOp writes the full BitTorrent settings object via Settings.BT set (method
// and full-object form encoding live-verified on 4.1.2).
var btSetOp = compatibility.Operation[downloadstation.BTSettings, downloadstation.SettingsMutationResult]{
	Name: SettingsWriteCapabilityName,
	Variants: []compatibility.Variant[downloadstation.BTSettings, downloadstation.SettingsMutationResult]{
		{
			Name: "downloadstation2-settings-bt-set-v1", API: SettingsBTAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SettingsBTAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired downloadstation.BTSettings) (downloadstation.SettingsMutationResult, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{API: SettingsBTAPIName, Version: 1, Method: "set", Parameters: encodeBTSettings(desired)}); err != nil {
					return downloadstation.SettingsMutationResult{}, fmt.Errorf("call %s.set: %w", SettingsBTAPIName, err)
				}
				return downloadstation.SettingsMutationResult{API: SettingsBTAPIName, Version: 1, Method: "set", Group: "bt"}, nil
			},
		},
	},
}

func encodeBTSettings(bt downloadstation.BTSettings) url.Values {
	boolStr := func(b bool) string {
		if b {
			return "true"
		}
		return "false"
	}
	v := url.Values{}
	v.Set("tcp_port", strconv.Itoa(bt.TCPPort))
	v.Set("dht_port", strconv.Itoa(bt.DHTPort))
	v.Set("enable_dht", boolStr(bt.EnableDHT))
	v.Set("enable_port_forwarding", boolStr(bt.EnablePortForwarding))
	v.Set("enable_preview", boolStr(bt.EnablePreview))
	v.Set("encrypt", bt.Encryption)
	v.Set("max_download_rate", strconv.Itoa(bt.MaxDownloadRate))
	v.Set("max_upload_rate", strconv.Itoa(bt.MaxUploadRate))
	v.Set("max_peer", strconv.Itoa(bt.MaxPeer))
	v.Set("seeding_ratio", strconv.Itoa(bt.SeedingRatio))
	v.Set("seeding_interval", strconv.Itoa(bt.SeedingInterval))
	v.Set("enable_seeding_auto_remove", boolStr(bt.EnableSeedingAutoRemove))
	return v
}

var ftpHttpGetOp = compatibility.Operation[Input, downloadstation.FtpHttpSettings]{
	Name: "download.settings.ftphttp.get",
	Variants: []compatibility.Variant[Input, downloadstation.FtpHttpSettings]{
		{
			Name: "downloadstation2-settings-ftphttp-get-v1", API: SettingsFtpHttpAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SettingsFtpHttpAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (downloadstation.FtpHttpSettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SettingsFtpHttpAPIName, Version: 1, Method: "get"})
				if err != nil {
					return downloadstation.FtpHttpSettings{}, fmt.Errorf("call %s.get: %w", SettingsFtpHttpAPIName, err)
				}
				return decodeFtpHttpSettings(data)
			},
		},
	},
}

var ftpHttpSetOp = compatibility.Operation[downloadstation.FtpHttpSettings, downloadstation.SettingsMutationResult]{
	Name: "download.settings.ftphttp.set",
	Variants: []compatibility.Variant[downloadstation.FtpHttpSettings, downloadstation.SettingsMutationResult]{
		{
			Name: "downloadstation2-settings-ftphttp-set-v1", API: SettingsFtpHttpAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SettingsFtpHttpAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired downloadstation.FtpHttpSettings) (downloadstation.SettingsMutationResult, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{API: SettingsFtpHttpAPIName, Version: 1, Method: "set", Parameters: encodeFtpHttpSettings(desired)}); err != nil {
					return downloadstation.SettingsMutationResult{}, fmt.Errorf("call %s.set: %w", SettingsFtpHttpAPIName, err)
				}
				return downloadstation.SettingsMutationResult{API: SettingsFtpHttpAPIName, Version: 1, Method: "set", Group: "ftp_http"}, nil
			},
		},
	},
}

func encodeFtpHttpSettings(f downloadstation.FtpHttpSettings) url.Values {
	v := url.Values{}
	v.Set("enable_ftp_max_conn", boolParam(f.EnableMaxConn))
	v.Set("ftp_http_max_download_rate", strconv.Itoa(f.MaxDownloadRate))
	v.Set("ftp_max_conn", strconv.Itoa(f.MaxConn))
	return v
}

// stringParam encodes a string value for the DownloadStation2 form-encoded
// JSON-request APIs. A bare empty form value is treated as "not provided" by
// DSM (live-verified: username="" left the stored name untouched), so an empty
// string is sent as the JSON literal "" to actually clear the field; non-empty
// values stay raw, the form live-verified writes have always used.
func stringParam(value string) string {
	if value == "" {
		return `""`
	}
	return value
}

func boolParam(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func ExecuteFtpHttpGet(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (downloadstation.FtpHttpSettings, compatibility.Selection, error) {
	return ftpHttpGetOp.Run(ctx, target, executor, Input{})
}

func ExecuteFtpHttpSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired downloadstation.FtpHttpSettings) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	result, selection, err := ftpHttpSetOp.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
}

// settingsGetOp / settingsSetOp build the get/set operations shared by the
// simple single-API DownloadStation2.Settings.* groups.
func settingsGetOp[T any](name, api string, version int, decode func(json.RawMessage) (T, error)) compatibility.Operation[Input, T] {
	return compatibility.Operation[Input, T]{
		Name: name,
		Variants: []compatibility.Variant[Input, T]{{
			Name: name, API: api, Version: version, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(api, version), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (T, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: "get"})
				if err != nil {
					var zero T
					return zero, fmt.Errorf("call %s.get: %w", api, err)
				}
				return decode(data)
			},
		}},
	}
}

func settingsSetOp[T any](name, api string, version int, group string, encode func(T) url.Values) compatibility.Operation[T, downloadstation.SettingsMutationResult] {
	return compatibility.Operation[T, downloadstation.SettingsMutationResult]{
		Name: name,
		Variants: []compatibility.Variant[T, downloadstation.SettingsMutationResult]{{
			Name: name, API: api, Version: version, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(api, version), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired T) (downloadstation.SettingsMutationResult, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: "set", Parameters: encode(desired)}); err != nil {
					return downloadstation.SettingsMutationResult{}, fmt.Errorf("call %s.set: %w", api, err)
				}
				return downloadstation.SettingsMutationResult{API: api, Version: version, Method: "set", Group: group}, nil
			},
		}},
	}
}

func runSettingsGet[T any](ctx context.Context, op compatibility.Operation[Input, T], target compatibility.Target, executor compatibility.Executor) (T, compatibility.Selection, error) {
	return op.Run(ctx, target, executor, Input{})
}

func runSettingsSet[T any](ctx context.Context, op compatibility.Operation[T, downloadstation.SettingsMutationResult], target compatibility.Target, executor compatibility.Executor, desired T) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	result, selection, err := op.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
}

var rssGetOp = settingsGetOp("download.settings.rss.get", SettingsRssAPIName, 1, decodeRssSettings)
var rssSetOp = settingsSetOp("download.settings.rss.set", SettingsRssAPIName, 1, "rss", encodeRssSettings)
var locationGetOp = settingsGetOp("download.settings.location.get", SettingsLocationAPIName, 1, decodeLocationSettings)
var locationSetOp = settingsSetOp("download.settings.location.set", SettingsLocationAPIName, 1, "location", encodeLocationSettings)
var schedulerGetOp = settingsGetOp("download.settings.scheduler.get", SettingsSchedulerAPIName, 1, decodeSchedulerSettings)
var schedulerSetOp = settingsSetOp("download.settings.scheduler.set", SettingsSchedulerAPIName, 1, "scheduler", encodeSchedulerSettings)
var globalGetOp = settingsGetOp("download.settings.global.get", SettingsGlobalAPIName, 2, decodeGlobalSettings)
var globalSetOp = settingsSetOp("download.settings.global.set", SettingsGlobalAPIName, 2, "global", encodeGlobalSettings)
var autoExtractionGetOp = settingsGetOp("download.settings.auto_extraction.get", SettingsAutoExtractionAPIName, 1, decodeAutoExtractionSettings)
var autoExtractionSetOp = settingsSetOp("download.settings.auto_extraction.set", SettingsAutoExtractionAPIName, 1, "auto_extraction", encodeAutoExtractionSetInput)
var nzbGetOp = settingsGetOp("download.settings.nzb.get", SettingsNzbAPIName, 1, decodeNzbSettings)
var nzbSetOp = settingsSetOp("download.settings.nzb.set", SettingsNzbAPIName, 1, "nzb", encodeNzbSetInput)

func encodeRssSettings(r downloadstation.RssSettings) url.Values {
	v := url.Values{}
	v.Set("update_interval", strconv.Itoa(r.UpdateIntervalMinutes))
	return v
}

// encodeAutoExtractionChange builds a PARTIAL set: only the fields present in
// the patch are sent. The AutoExtraction handler reads each parameter with a
// HasParam guard and leaves unspecified fields (including the passwords it never
// returns) untouched, so a non-secret patch never disturbs stored passwords. The
// on-the-wire unzip_location parameter is a boolean (extract to the local
// folder), distinct from the string the read returns.
// AutoExtractionSetInput carries the patch plus the archive password list
// resolved from a credential reference at apply time. The plaintext exists
// only in memory for the DSM call; it is never part of a plan or log.
type AutoExtractionSetInput struct {
	Change    downloadstation.AutoExtractionSettingsChange
	Passwords *[]string
}

// NzbSetInput carries the patch plus the news-server password resolved from a
// credential reference at apply time.
type NzbSetInput struct {
	Change   downloadstation.NzbSettingsChange
	Password *string
}

func encodeAutoExtractionSetInput(input AutoExtractionSetInput) url.Values {
	v := encodeAutoExtractionChange(input.Change)
	if input.Passwords != nil {
		encoded, err := json.Marshal(*input.Passwords)
		if err == nil {
			v.Set("passwords", string(encoded))
		}
	}
	return v
}

func encodeNzbSetInput(input NzbSetInput) url.Values {
	v := encodeNzbChange(input.Change)
	if input.Password != nil {
		v.Set("password", stringParam(*input.Password))
	}
	return v
}

func encodeAutoExtractionChange(c downloadstation.AutoExtractionSettingsChange) url.Values {
	v := url.Values{}
	if c.EnableUnzip != nil {
		v.Set("enable_unzip", boolParam(*c.EnableUnzip))
	}
	if c.CreateSubfolder != nil {
		v.Set("create_subfolder", boolParam(*c.CreateSubfolder))
	}
	if c.DeleteArchive != nil {
		v.Set("delete_archive", boolParam(*c.DeleteArchive))
	}
	if c.UnzipOverwrite != nil {
		v.Set("unzip_overwrite", boolParam(*c.UnzipOverwrite))
	}
	if c.UnzipToLocal != nil {
		v.Set("unzip_location", boolParam(*c.UnzipToLocal))
	}
	if c.UnzipToPath != nil {
		v.Set("unzip_to_path", stringParam(*c.UnzipToPath))
	}
	return v
}

// encodeNzbChange builds a PARTIAL set: only the fields present in the patch are
// sent. The NZB handler adds each parameter to a commit queue only when it is
// provided and handles the news-server password separately, so a non-secret
// patch never disturbs the stored password.
func encodeNzbChange(c downloadstation.NzbSettingsChange) url.Values {
	v := url.Values{}
	if c.Server != nil {
		v.Set("server", stringParam(*c.Server))
	}
	if c.Port != nil {
		v.Set("port", strconv.Itoa(*c.Port))
	}
	if c.Username != nil {
		v.Set("username", stringParam(*c.Username))
	}
	if c.EnableAuth != nil {
		v.Set("enable_auth", boolParam(*c.EnableAuth))
	}
	if c.EnableEncryption != nil {
		v.Set("enable_encryption", boolParam(*c.EnableEncryption))
	}
	if c.EnableParchive != nil {
		v.Set("enable_parchive", boolParam(*c.EnableParchive))
	}
	if c.EnableRemoveParfiles != nil {
		v.Set("enable_remove_parfiles", boolParam(*c.EnableRemoveParfiles))
	}
	if c.ConnPerDownload != nil {
		v.Set("conn_per_download", strconv.Itoa(*c.ConnPerDownload))
	}
	if c.MaxDownloadRate != nil {
		v.Set("max_download_rate", strconv.Itoa(*c.MaxDownloadRate))
	}
	return v
}

func encodeLocationSettings(l downloadstation.LocationSettings) url.Values {
	v := url.Values{}
	v.Set("default_destination", stringParam(l.DefaultDestination))
	v.Set("enable_torrent_nzb_watch", boolParam(l.EnableTorrentNzbWatch))
	v.Set("enable_delete_torrent_nzb_watch", boolParam(l.EnableDeleteTorrentNzbWatch))
	v.Set("torrent_nzb_watch_folder", stringParam(l.TorrentNzbWatchFolder))
	return v
}

// jsonStringParam quotes a value so DSM's entry.cgi JSON param parser reads it
// as a string. Without the quotes an all-digit value like the 168-char schedule
// bitmap parses as a JSON number and fails a Param.String type check (code 120).
func jsonStringParam(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	return string(encoded)
}

func encodeSchedulerSettings(s downloadstation.SchedulerSettings) url.Values {
	v := url.Values{}
	v.Set("enable_schedule", boolParam(s.EnableSchedule))
	v.Set("download_rate", strconv.Itoa(s.DownloadRate))
	v.Set("upload_rate", strconv.Itoa(s.UploadRate))
	v.Set("max_tasks", strconv.Itoa(s.MaxTasks))
	v.Set("order", s.Order)
	v.Set("schedule", jsonStringParam(s.ScheduleBitmap))
	return v
}

func encodeGlobalSettings(g downloadstation.GlobalSettings) url.Values {
	v := url.Values{}
	v.Set("download_volume", g.DownloadVolume)
	v.Set("enable_emule", boolParam(g.EmuleEnabled))
	v.Set("enable_unzip_service", boolParam(g.UnzipServiceEnabled))
	return v
}

func ExecuteRssGet(ctx context.Context, t compatibility.Target, e compatibility.Executor) (downloadstation.RssSettings, compatibility.Selection, error) {
	return runSettingsGet(ctx, rssGetOp, t, e)
}
func ExecuteRssSet(ctx context.Context, t compatibility.Target, e compatibility.Executor, d downloadstation.RssSettings) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	return runSettingsSet(ctx, rssSetOp, t, e, d)
}
func ExecuteLocationGet(ctx context.Context, t compatibility.Target, e compatibility.Executor) (downloadstation.LocationSettings, compatibility.Selection, error) {
	return runSettingsGet(ctx, locationGetOp, t, e)
}
func ExecuteLocationSet(ctx context.Context, t compatibility.Target, e compatibility.Executor, d downloadstation.LocationSettings) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	return runSettingsSet(ctx, locationSetOp, t, e, d)
}
func ExecuteSchedulerGet(ctx context.Context, t compatibility.Target, e compatibility.Executor) (downloadstation.SchedulerSettings, compatibility.Selection, error) {
	return runSettingsGet(ctx, schedulerGetOp, t, e)
}
func ExecuteSchedulerSet(ctx context.Context, t compatibility.Target, e compatibility.Executor, d downloadstation.SchedulerSettings) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	return runSettingsSet(ctx, schedulerSetOp, t, e, d)
}
func ExecuteGlobalGet(ctx context.Context, t compatibility.Target, e compatibility.Executor) (downloadstation.GlobalSettings, compatibility.Selection, error) {
	return runSettingsGet(ctx, globalGetOp, t, e)
}
func ExecuteGlobalSet(ctx context.Context, t compatibility.Target, e compatibility.Executor, d downloadstation.GlobalSettings) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	return runSettingsSet(ctx, globalSetOp, t, e, d)
}
func ExecuteAutoExtractionGet(ctx context.Context, t compatibility.Target, e compatibility.Executor) (downloadstation.AutoExtractionSettings, compatibility.Selection, error) {
	return runSettingsGet(ctx, autoExtractionGetOp, t, e)
}
func ExecuteAutoExtractionSet(ctx context.Context, t compatibility.Target, e compatibility.Executor, input AutoExtractionSetInput) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	return runSettingsSet(ctx, autoExtractionSetOp, t, e, input)
}
func ExecuteNzbGet(ctx context.Context, t compatibility.Target, e compatibility.Executor) (downloadstation.NzbSettings, compatibility.Selection, error) {
	return runSettingsGet(ctx, nzbGetOp, t, e)
}
func ExecuteNzbSet(ctx context.Context, t compatibility.Target, e compatibility.Executor, input NzbSetInput) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	return runSettingsSet(ctx, nzbSetOp, t, e, input)
}

func SelectSettingsWrite(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := btSetOp.Select(target)
	return selection, err
}

func ExecuteBTGet(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (downloadstation.BTSettings, compatibility.Selection, error) {
	return btGetOp.Run(ctx, target, executor, Input{})
}

func ExecuteBTSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired downloadstation.BTSettings) (downloadstation.SettingsMutationResult, compatibility.Selection, error) {
	result, selection, err := btSetOp.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
}

func ExecuteTaskWrite(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change downloadstation.TaskChange) (downloadstation.TaskMutationResult, compatibility.Selection, error) {
	result, selection, err := taskWriteOp.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend = selection.Backend
	}
	return result, selection, err
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
