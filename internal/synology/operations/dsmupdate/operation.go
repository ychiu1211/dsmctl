// Package dsmupdate implements independently selectable, read-only DSM
// operations for the Control Panel → Update & Restore surface: the local DSM
// update status, the update-server offered-update check, the auto-update
// policy, and the configuration-backup status/history. Each area reads a
// distinct DSM API family; an area whose API is absent is reported unsupported
// without affecting the others.
//
// Every read shape below was live-verified on DSM 7.3 (see WI-074's wire map):
//
//   - status:        SYNO.Core.Upgrade v1/v2 "status"
//     -> {"allow_upgrade":true,"status":"none"}
//   - available:     SYNO.Core.Upgrade.Server v1-4 "check"
//     -> v1 {"available":false}; v2+ {"update":{"available":false,"rss_result":"success"}}
//   - policy:        SYNO.Core.Upgrade.Setting v1-4 "get"
//     -> v2 {"autoupdate_enable":true,"autoupdate_type":"hotfix-security",
//     "schedule":{"hour":5,"minute":5,"week_day":"0"},"smart_nano_enabled":true,
//     "upgrade_type":"hotfix"}; v1 {"auto_download":true,"upgrade_type":"hotfix"}
//   - config backup: SYNO.Backup.Config.AutoBackup v1 "get"/"list"
//     -> get {"enable":false,"enc_method":"manual","last_status":"none",
//     "myds_account":"…","pwd":""}; list {"total":1,"versions":[…]}
//
// Installing a DSM update (SYNO.Core.Upgrade.Server.Download / .Patch),
// downloading, and restoring a configuration backup
// (SYNO.Backup.Config.Restore) are deliberately NOT implemented here: they
// reboot or overwrite the whole system and are deferred with reason (WI-074).
// The update-server "check" is a network egress; a reachability failure is
// treated as unknown by the facade rather than erroring the module.
package dsmupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ychiu1211/dsmctl/internal/domain/dsmupdate"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	UpgradeAPI       = "SYNO.Core.Upgrade"
	UpgradeServerAPI = "SYNO.Core.Upgrade.Server"
	UpgradeSetAPI    = "SYNO.Core.Upgrade.Setting"
	AutoBackupAPI    = "SYNO.Backup.Config.AutoBackup"

	StatusReadCapabilityName       = "dsmupdate.status.read"
	AvailableReadCapabilityName    = "dsmupdate.available.read"
	PolicyReadCapabilityName       = "dsmupdate.policy.read"
	ConfigBackupReadCapabilityName = "dsmupdate.configbackup.read"
)

// Input is the empty request the read operations take.
type Input struct{}

// localStatus is the update state decoded from SYNO.Core.Upgrade "status"; the
// installed DSM version is merged in by the facade from the discovered target.
type localStatus struct {
	AllowUpgrade bool
	State        string
}

// configSettings is the config-backup settings decoded from AutoBackup "get";
// the destination password is never decoded.
type configSettings struct {
	Enabled          bool
	Account          string
	EncryptionMethod string
	LastStatus       string
}

func readVariant[O any](name, api string, version, priority int, method string, decode func(json.RawMessage) (O, error)) compatibility.Variant[Input, O] {
	return compatibility.Variant[Input, O]{
		Name:     name,
		API:      api,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(api, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (O, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: method, ReadOnly: true})
			if err != nil {
				var zero O
				return zero, fmt.Errorf("call %s.%s v%d: %w", api, method, version, err)
			}
			return decode(data)
		},
	}
}

var statusOp = compatibility.Operation[Input, localStatus]{
	Name: StatusReadCapabilityName,
	Variants: []compatibility.Variant[Input, localStatus]{
		readVariant("dsmupdate-status-v2", UpgradeAPI, 2, 20, "status", decodeLocalStatus),
		readVariant("dsmupdate-status-v1", UpgradeAPI, 1, 10, "status", decodeLocalStatus),
	},
}

// availableOp checks the update server for an offered update. The UI passes
// need_auto_smallupdate:true, so this matches the DSM WebUI's own check. The
// decoder tolerates both the flat (v1) and the wrapped (v2+) response shapes.
func availableVariant(name string, version, priority int) compatibility.Variant[Input, dsmupdate.AvailableUpdate] {
	return compatibility.Variant[Input, dsmupdate.AvailableUpdate]{
		Name:     name,
		API:      UpgradeServerAPI,
		Version:  version,
		Priority: priority,
		Match:    compatibility.APIVersion(UpgradeServerAPI, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (dsmupdate.AvailableUpdate, error) {
			data, err := executor.Execute(ctx, compatibility.Request{
				API: UpgradeServerAPI, Version: version, Method: "check", ReadOnly: true,
				JSONParameters: map[string]any{"need_auto_smallupdate": true},
			})
			if err != nil {
				return dsmupdate.AvailableUpdate{}, fmt.Errorf("call %s.check v%d: %w", UpgradeServerAPI, version, err)
			}
			return decodeAvailable(data)
		},
	}
}

var availableOp = compatibility.Operation[Input, dsmupdate.AvailableUpdate]{
	Name: AvailableReadCapabilityName,
	Variants: []compatibility.Variant[Input, dsmupdate.AvailableUpdate]{
		availableVariant("dsmupdate-server-check-v3", 3, 30),
		availableVariant("dsmupdate-server-check-v2", 2, 20),
		availableVariant("dsmupdate-server-check-v1", 1, 10),
	},
}

var policyOp = compatibility.Operation[Input, dsmupdate.AutoUpdatePolicy]{
	Name: PolicyReadCapabilityName,
	Variants: []compatibility.Variant[Input, dsmupdate.AutoUpdatePolicy]{
		readVariant("dsmupdate-setting-get-v2", UpgradeSetAPI, 2, 20, "get", decodePolicyV2),
		readVariant("dsmupdate-setting-get-v1", UpgradeSetAPI, 1, 10, "get", decodePolicyV1),
	},
}

var configOp = compatibility.Operation[Input, configSettings]{
	Name: ConfigBackupReadCapabilityName,
	Variants: []compatibility.Variant[Input, configSettings]{
		readVariant("dsmupdate-autobackup-get-v1", AutoBackupAPI, 1, 10, "get", decodeConfigSettings),
	},
}

// configHistoryOp enriches the config-backup read with the stored backup
// history. It is optional: config-backup settings stay readable when the list
// method is unavailable.
var configHistoryOp = compatibility.Operation[Input, []dsmupdate.ConfigBackupVersion]{
	Name: "dsmupdate.configbackup.history.read",
	Variants: []compatibility.Variant[Input, []dsmupdate.ConfigBackupVersion]{
		readVariant("dsmupdate-autobackup-list-v1", AutoBackupAPI, 1, 10, "list", decodeConfigVersions),
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them in a single query before selecting any area.
func APINames() []string {
	unique := map[string]struct{}{}
	for _, names := range [][]string{
		statusOp.APINames(), availableOp.APINames(),
		policyOp.APINames(), configOp.APINames(), configHistoryOp.APINames(),
	} {
		for _, name := range names {
			unique[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Select* report each area's primary read selection, so capabilities can be
// described without a read.
func SelectStatus(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := statusOp.Select(target)
	return selection, err
}

func SelectAvailable(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := availableOp.Select(target)
	return selection, err
}

func SelectPolicy(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := policyOp.Select(target)
	return selection, err
}

func SelectConfigBackup(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := configOp.Select(target)
	return selection, err
}

// ReadStatus reads the local DSM update state. The installed DSM version is
// supplied by the facade from the discovered target, since the status method
// does not return it.
func ReadStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor, installedVersion string) (dsmupdate.UpdateStatus, compatibility.Selection, error) {
	status, selection, err := statusOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return dsmupdate.UpdateStatus{}, selection, err
	}
	return dsmupdate.UpdateStatus{
		InstalledVersion: installedVersion,
		AllowUpgrade:     status.AllowUpgrade,
		State:            status.State,
	}, selection, nil
}

// ReadAvailable checks the update server for an offered update. The caller
// (facade) treats an execution failure as "unknown" (Checked=false) rather
// than erroring the module, because the check is a network egress to Synology's
// update server.
func ReadAvailable(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (dsmupdate.AvailableUpdate, compatibility.Selection, error) {
	return availableOp.Run(ctx, target, executor, Input{})
}

// ReadPolicy reads the DSM auto-update policy.
func ReadPolicy(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (dsmupdate.AutoUpdatePolicy, compatibility.Selection, error) {
	return policyOp.Run(ctx, target, executor, Input{})
}

// ReadConfigBackup reads the configuration-backup settings (required) and
// enriches them with the stored backup history when the list method is
// available. The destination password is never decoded.
func ReadConfigBackup(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (dsmupdate.ConfigBackup, compatibility.Selection, error) {
	settings, selection, err := configOp.Run(ctx, target, executor, Input{})
	if err != nil {
		return dsmupdate.ConfigBackup{}, selection, err
	}
	state := dsmupdate.ConfigBackup{
		Enabled:          settings.Enabled,
		Account:          settings.Account,
		EncryptionMethod: settings.EncryptionMethod,
		LastStatus:       settings.LastStatus,
		Versions:         []dsmupdate.ConfigBackupVersion{},
	}
	// The stored-backup history is a secondary enrichment: a NAS whose list
	// method is absent or fails must still return the settings, so a history
	// failure is intentionally non-fatal (the settings above are what the
	// caller relies on; the malformed-response rejection is unit-tested on the
	// decoder directly).
	if _, historySelection, err := configHistoryOp.Select(target); err == nil && historySelection.Supported {
		if versions, _, err := configHistoryOp.Run(ctx, target, executor, Input{}); err == nil {
			state.Versions = versions
		}
	}
	return state, selection, nil
}
