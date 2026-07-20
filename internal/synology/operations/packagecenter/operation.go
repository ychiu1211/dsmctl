// Package packagecenter implements independently selectable Package Center
// operations: installed-package inventory, global settings read, and guarded
// start/stop and uninstall. DSM WebAPI names and versions stay behind this
// package so the shared domain, CLI, and MCP contracts remain stable.
//
// Settings SET covers only the automatic-update policy, which the base
// SYNO.Core.Package.Setting `set` writes (verified on DSM 7.3: the write is
// applied even though the response echoes only the notification/channel fields).
// Publisher trust level is read-only: no DSM API accepts a trust-level write and
// the base `set` silently ignores it. Default install volume
// (SYNO.Core.Package.Setting.Volume) and the update channel are separate
// follow-ups.
//
// Install is backed by the online catalog (SYNO.Core.Package.Server) plus the
// asynchronous download+install task (SYNO.Core.Package.Installation.install with
// status polling); see catalog.go and install.go. Update (upgrade) is still
// modeled as a variant-less operation and fails closed until its backend ships.
package packagecenter

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// DSM WebAPI anchors. The inventory and settings APIs are the well-known
// Package Center reads. The control and uninstall API names are best-effort and
// gate on discovery: if a name is not advertised by the target, the matching
// operation reports unsupported and fails closed rather than issuing a wrong
// request. Confirm the enabled mutation APIs with read-only `SYNO.API.Info`
// discovery (`dsmctl package capabilities`).
const (
	InventoryAPIName = "SYNO.Core.Package"
	SettingAPIName   = "SYNO.Core.Package.Setting"
	ControlAPIName   = "SYNO.Core.Package.Control"
	UninstallAPIName = "SYNO.Core.Package.Uninstallation"

	InventoryCapabilityName    = "packagecenter.inventory.read"
	SettingsReadCapabilityName = "packagecenter.settings.read"
	SettingsSetCapabilityName  = "packagecenter.settings.set"
	ControlCapabilityName      = "packagecenter.control"
	UninstallCapabilityName    = "packagecenter.uninstall"
	InstallCapabilityName      = "packagecenter.install"
	UpdateCapabilityName       = "packagecenter.update"
)

// inventoryAdditional requests the optional per-package fields the decoder reads
// on top of the always-present id/name/version. These keys are validated against
// DSM 7.3 `SYNO.Core.Package.list`; that API rejects the whole request (error
// 120) if any requested key is unknown, so only confirmed keys are listed here.
// `stoppable`, `removable`, and `installing` are NOT valid keys; stop/uninstall
// eligibility is derived from `startable`, the run status, and `install_type`.
const inventoryAdditional = `["status","beta","startable","install_type"]`

// Input is the empty input for read operations.
type Input struct{}

// ControlInput selects a start or stop action for one package.
type ControlInput struct {
	Action    string
	PackageID string
}

// UninstallInput identifies the package to uninstall.
type UninstallInput struct {
	PackageID string
}

// MutationResult reports the selected DSM backend for a completed mutation. The
// mutation variants never decode the DSM write response for correctness; the
// application layer proves the change with a fresh read.
type MutationResult struct {
	Action    string `json:"action" jsonschema:"Applied action"`
	PackageID string `json:"package_id,omitempty" jsonschema:"Affected package identifier when applicable"`
	Backend   string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API       string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version   int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method    string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var inventoryOperation = compatibility.Operation[Input, packagecenter.State]{
	Name: InventoryCapabilityName,
	Variants: []compatibility.Variant[Input, packagecenter.State]{
		inventoryVariant("core-package-v2", 2, 20),
		inventoryVariant("core-package-v1", 1, 10),
	},
}

var settingsReadOperation = compatibility.Operation[Input, packagecenter.Settings]{
	Name: SettingsReadCapabilityName,
	Variants: []compatibility.Variant[Input, packagecenter.Settings]{
		settingsReadVariant("core-package-setting-v2", 2, 20),
		settingsReadVariant("core-package-setting-v1", 1, 10),
	},
}

// settingsSetOperation writes the automatic-update policy through the base
// SYNO.Core.Package.Setting `set`. Trust level is not written (no DSM endpoint
// accepts it); the encoder omits it.
var settingsSetOperation = compatibility.Operation[packagecenter.Settings, MutationResult]{
	Name: SettingsSetCapabilityName,
	Variants: []compatibility.Variant[packagecenter.Settings, MutationResult]{
		settingsSetVariant("core-package-setting-v2", 2, 20),
		settingsSetVariant("core-package-setting-v1", 1, 10),
	},
}

var controlOperation = compatibility.Operation[ControlInput, MutationResult]{
	Name: ControlCapabilityName,
	Variants: []compatibility.Variant[ControlInput, MutationResult]{
		controlVariant("core-package-control-v1", 1, 10),
	},
}

var uninstallOperation = compatibility.Operation[UninstallInput, MutationResult]{
	Name: UninstallCapabilityName,
	Variants: []compatibility.Variant[UninstallInput, MutationResult]{
		uninstallVariant("core-package-uninstallation-v1", 1, 10),
	},
}

// updateOperation reports whether the guarded update is available. An update
// runs through the same online download+install backend as install (defined in
// install.go); the version-bound planning and downgrade refusal live in the
// application layer.
var updateOperation = compatibility.Operation[Input, MutationResult]{
	Name: UpdateCapabilityName,
	Variants: []compatibility.Variant[Input, MutationResult]{
		{
			Name: "core-package-installation-download-v1", API: InstallationAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(InstallationAPIName, 1),
			Execute: func(context.Context, compatibility.Executor, Input) (MutationResult, error) {
				return MutationResult{}, fmt.Errorf("package update executes through the guarded install path, not this operation")
			},
		},
	},
}

// APINames lists every DSM API this package may use, so the facade can discover
// them in one call before selecting variants.
func APINames() []string {
	return []string{InventoryAPIName, SettingAPIName, ControlAPIName, UninstallAPIName, ServerAPIName, InstallationAPIName}
}

func SelectInventory(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := inventoryOperation.Select(target)
	return selection, err
}

func SelectSettingsRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := settingsReadOperation.Select(target)
	return selection, err
}

func SelectSettingsSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := settingsSetOperation.Select(target)
	return selection, err
}

func SelectControl(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := controlOperation.Select(target)
	return selection, err
}

func SelectUninstall(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := uninstallOperation.Select(target)
	return selection, err
}

func SelectInstall(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := downloadOperation.Select(target)
	return selection, err
}

func SelectUpdate(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := updateOperation.Select(target)
	return selection, err
}

// Select returns every Package Center operation selection in a stable order for
// capability reporting.
func Select(target compatibility.Target) ([]compatibility.Selection, error) {
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		SelectInventory, SelectSettingsRead, SelectSettingsSet,
		SelectControl, SelectUninstall, SelectInstall, SelectUpdate,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return nil, err
		}
		selections = append(selections, selection)
	}
	return selections, nil
}

func ExecuteInventory(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (packagecenter.State, compatibility.Selection, error) {
	return inventoryOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSettingsRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (packagecenter.Settings, compatibility.Selection, error) {
	return settingsReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSettingsSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired packagecenter.Settings) (MutationResult, compatibility.Selection, error) {
	result, selection, err := settingsSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Action, result.Backend, result.API, result.Version, result.Method = packagecenter.KindSettings, selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteControl(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ControlInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := controlOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Action, result.PackageID, result.Backend, result.API, result.Version, result.Method = input.Action, input.PackageID, selection.Backend, selection.API, selection.Version, input.Action
	}
	return result, selection, err
}

func ExecuteUninstall(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input UninstallInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := uninstallOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Action, result.PackageID, result.Backend, result.API, result.Version, result.Method = packagecenter.ActionUninstall, input.PackageID, selection.Backend, selection.API, selection.Version, "uninstall"
	}
	return result, selection, err
}

func inventoryVariant(name string, version, priority int) compatibility.Variant[Input, packagecenter.State] {
	return compatibility.Variant[Input, packagecenter.State]{
		Name: name, API: InventoryAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(InventoryAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (packagecenter.State, error) {
			data, err := executor.Execute(ctx, compatibility.Request{
				API:        InventoryAPIName,
				Version:    version,
				Method:     "list",
				Parameters: url.Values{"additional": {inventoryAdditional}},
			})
			if err != nil {
				return packagecenter.State{}, fmt.Errorf("call %s.list v%d: %w", InventoryAPIName, version, err)
			}
			packages, err := decodePackages(data)
			if err != nil {
				return packagecenter.State{}, err
			}
			return packagecenter.State{Packages: packages}, nil
		},
	}
}

func settingsReadVariant(name string, version, priority int) compatibility.Variant[Input, packagecenter.Settings] {
	return compatibility.Variant[Input, packagecenter.Settings]{
		Name: name, API: SettingAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(SettingAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (packagecenter.Settings, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: SettingAPIName, Version: version, Method: "get"})
			if err != nil {
				return packagecenter.Settings{}, fmt.Errorf("call %s.get v%d: %w", SettingAPIName, version, err)
			}
			return decodeSettings(data)
		},
	}
}

func settingsSetVariant(name string, version, priority int) compatibility.Variant[packagecenter.Settings, MutationResult] {
	return compatibility.Variant[packagecenter.Settings, MutationResult]{
		Name: name, API: SettingAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(SettingAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, desired packagecenter.Settings) (MutationResult, error) {
			parameters := encodeSettings(desired)
			if _, err := executor.Execute(ctx, compatibility.Request{API: SettingAPIName, Version: version, Method: "set", JSONParameters: parameters}); err != nil {
				return MutationResult{}, fmt.Errorf("call %s.set v%d: %w", SettingAPIName, version, err)
			}
			return MutationResult{}, nil
		},
	}
}

func controlVariant(name string, version, priority int) compatibility.Variant[ControlInput, MutationResult] {
	return compatibility.Variant[ControlInput, MutationResult]{
		Name: name, API: ControlAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(ControlAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, input ControlInput) (MutationResult, error) {
			if input.Action != packagecenter.ActionStart && input.Action != packagecenter.ActionStop {
				return MutationResult{}, fmt.Errorf("unsupported control action %q", input.Action)
			}
			if input.PackageID == "" {
				return MutationResult{}, fmt.Errorf("control action requires a package id")
			}
			if _, err := executor.Execute(ctx, compatibility.Request{
				API: ControlAPIName, Version: version, Method: input.Action,
				Parameters: url.Values{"id": {input.PackageID}},
			}); err != nil {
				return MutationResult{}, fmt.Errorf("call %s.%s v%d: %w", ControlAPIName, input.Action, version, err)
			}
			return MutationResult{}, nil
		},
	}
}

func uninstallVariant(name string, version, priority int) compatibility.Variant[UninstallInput, MutationResult] {
	return compatibility.Variant[UninstallInput, MutationResult]{
		Name: name, API: UninstallAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(UninstallAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, input UninstallInput) (MutationResult, error) {
			if input.PackageID == "" {
				return MutationResult{}, fmt.Errorf("uninstall requires a package id")
			}
			if _, err := executor.Execute(ctx, compatibility.Request{
				API: UninstallAPIName, Version: version, Method: "uninstall",
				Parameters: url.Values{"id": {input.PackageID}},
			}); err != nil {
				return MutationResult{}, fmt.Errorf("call %s.uninstall v%d: %w", UninstallAPIName, version, err)
			}
			return MutationResult{}, nil
		},
	}
}
