// Package office implements the Synology Office settings operations behind
// SYNO.Office.Info (get v1), SYNO.Office.Setting (get/set v1),
// SYNO.Office.Setting.System (get/set v1), and SYNO.Office.Setting.Font
// (list v1). Every variant is gated on the installed Spreadsheet package (the
// DSM id of Synology Office) so a NAS without it, or with an untested older
// version, fails closed instead of receiving an unverified request. DSM field
// names (history_prune, formula_preview, ...) stay behind this package.
package office

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/office"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// PackageID is the DSM package that owns the Synology Office APIs.
const PackageID = "Spreadsheet"

const (
	InfoAPIName    = "SYNO.Office.Info"
	SettingAPIName = "SYNO.Office.Setting"
	SystemAPIName  = "SYNO.Office.Setting.System"
	FontAPIName    = "SYNO.Office.Setting.Font"

	InfoReadCapabilityName        = "office.info.read"
	SystemReadCapabilityName      = "office.system.read"
	SystemSetCapabilityName       = "office.system.set"
	PreferencesReadCapabilityName = "office.preferences.read"
	PreferencesSetCapabilityName  = "office.preferences.set"
	FontsReadCapabilityName       = "office.fonts.read"
	FontsSetCapabilityName        = "office.fonts.set"
)

// baselinePackage gates every variant on Synology Office 3.x, the family whose
// settings surface was verified live (3.7.2) and against the Office 3.4-3.7
// WebAPI definitions. A future major with a verified difference adds a
// higher-priority variant with a narrower range.
var baselinePackage = compatibility.PackageVersionRange(
	PackageID, compatibility.ParsePackageVersion("3.0"), compatibility.PackageVersion{},
)

type Input struct{}

// MutationResult records the selected backend for one set.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var infoReadOperation = compatibility.Operation[Input, office.Info]{
	Name: InfoReadCapabilityName,
	Variants: []compatibility.Variant[Input, office.Info]{
		{
			Name: "office-info-v1", API: InfoAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(InfoAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (office.Info, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: InfoAPIName, Version: 1, Method: "get"})
				if err != nil {
					return office.Info{}, fmt.Errorf("call %s.get v1: %w", InfoAPIName, err)
				}
				return decodeInfo(data)
			},
		},
	},
}

var systemReadOperation = compatibility.Operation[Input, office.SystemSettings]{
	Name: SystemReadCapabilityName,
	Variants: []compatibility.Variant[Input, office.SystemSettings]{
		{
			Name: "office-setting-system-v1", API: SystemAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SystemAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (office.SystemSettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SystemAPIName, Version: 1, Method: "get"})
				if err != nil {
					return office.SystemSettings{}, fmt.Errorf("call %s.get v1: %w", SystemAPIName, err)
				}
				return decodeSystemSettings(data)
			},
		},
	},
}

var systemSetOperation = compatibility.Operation[office.SystemChange, MutationResult]{
	Name: SystemSetCapabilityName,
	Variants: []compatibility.Variant[office.SystemChange, MutationResult]{
		{
			Name: "office-setting-system-v1", API: SystemAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SystemAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, change office.SystemChange) (MutationResult, error) {
				parameters := encodeSystemChange(change)
				// DSM accepts an empty set as a silent no-op; reject it here so a
				// buggy caller cannot report success without a change.
				if len(parameters) == 0 {
					return MutationResult{}, fmt.Errorf("office system set: empty patch")
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: SystemAPIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", SystemAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var preferencesReadOperation = compatibility.Operation[Input, office.Preferences]{
	Name: PreferencesReadCapabilityName,
	Variants: []compatibility.Variant[Input, office.Preferences]{
		{
			Name: "office-setting-v1", API: SettingAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SettingAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (office.Preferences, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SettingAPIName, Version: 1, Method: "get"})
				if err != nil {
					return office.Preferences{}, fmt.Errorf("call %s.get v1: %w", SettingAPIName, err)
				}
				return decodePreferences(data)
			},
		},
	},
}

var preferencesSetOperation = compatibility.Operation[office.PreferencesChange, MutationResult]{
	Name: PreferencesSetCapabilityName,
	Variants: []compatibility.Variant[office.PreferencesChange, MutationResult]{
		{
			Name: "office-setting-v1", API: SettingAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(SettingAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, change office.PreferencesChange) (MutationResult, error) {
				parameters := encodePreferencesChange(change)
				if len(parameters) == 0 {
					return MutationResult{}, fmt.Errorf("office preferences set: empty patch")
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: SettingAPIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", SettingAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

var fontsReadOperation = compatibility.Operation[Input, []office.Font]{
	Name: FontsReadCapabilityName,
	Variants: []compatibility.Variant[Input, []office.Font]{
		{
			Name: "office-setting-font-v1", API: FontAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(FontAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]office.Font, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: FontAPIName, Version: 1, Method: "list"})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v1: %w", FontAPIName, err)
				}
				return decodeFonts(data)
			},
		},
	},
}

var fontsSetOperation = compatibility.Operation[office.FontChange, MutationResult]{
	Name: FontsSetCapabilityName,
	Variants: []compatibility.Variant[office.FontChange, MutationResult]{
		{
			Name: "office-setting-font-v1", API: FontAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(FontAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, change office.FontChange) (MutationResult, error) {
				if len(change.Names) == 0 {
					return MutationResult{}, fmt.Errorf("office fonts %s: no font names", change.Action)
				}
				method := string(change.Action)
				switch change.Action {
				case office.FontActionAdd, office.FontActionEnable, office.FontActionDisable, office.FontActionDelete:
				default:
					return MutationResult{}, fmt.Errorf("office fonts: unknown action %q", change.Action)
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: FontAPIName, Version: 1, Method: method, JSONParameters: encodeFontChange(change)}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.%s v1: %w", FontAPIName, method, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

func APINames() []string {
	return []string{InfoAPIName, SettingAPIName, SystemAPIName, FontAPIName}
}

func SelectInfoRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := infoReadOperation.Select(target)
	return selection, err
}

func SelectSystemRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := systemReadOperation.Select(target)
	return selection, err
}

func SelectSystemSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := systemSetOperation.Select(target)
	return selection, err
}

func SelectPreferencesRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := preferencesReadOperation.Select(target)
	return selection, err
}

func SelectPreferencesSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := preferencesSetOperation.Select(target)
	return selection, err
}

func SelectFontsRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := fontsReadOperation.Select(target)
	return selection, err
}

func SelectFontsSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := fontsSetOperation.Select(target)
	return selection, err
}

func ExecuteInfoRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (office.Info, compatibility.Selection, error) {
	return infoReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSystemRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (office.SystemSettings, compatibility.Selection, error) {
	return systemReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSystemSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change office.SystemChange) (MutationResult, compatibility.Selection, error) {
	result, selection, err := systemSetOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecutePreferencesRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (office.Preferences, compatibility.Selection, error) {
	return preferencesReadOperation.Run(ctx, target, executor, Input{})
}

func ExecutePreferencesSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change office.PreferencesChange) (MutationResult, compatibility.Selection, error) {
	result, selection, err := preferencesSetOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteFontsRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]office.Font, compatibility.Selection, error) {
	return fontsReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteFontsSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change office.FontChange) (MutationResult, compatibility.Selection, error) {
	result, selection, err := fontsSetOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, string(change.Action)
	}
	return result, selection, err
}
