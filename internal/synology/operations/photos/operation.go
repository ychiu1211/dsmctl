// Package photos implements the Synology Photos administration operations behind
// SYNO.Foto.Setting.Admin (get/set v1). Every variant is gated on the installed
// SynologyPhotos package so a NAS without it, or with an untested older version,
// fails closed instead of receiving an unverified request. DSM field names
// (enable_person, display_photo_info_to_guest, ...) stay behind this package.
package photos

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/photos"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// PackageID is the DSM package that owns the Photos administration API.
const PackageID = "SynologyPhotos"

const (
	AdminAPIName = "SYNO.Foto.Setting.Admin"

	AdminReadCapabilityName = "photos.admin.read"
	AdminSetCapabilityName  = "photos.admin.set"
)

// baselinePackage gates every variant on Synology Photos 1.x, the family whose
// SYNO.Foto.Setting.Admin shape was verified live (1.9.1). A future major with a
// verified difference adds a higher-priority variant with a narrower range.
var baselinePackage = compatibility.PackageVersionRange(
	PackageID, compatibility.ParsePackageVersion("1.0"), compatibility.PackageVersion{},
)

type Input struct{}

// MutationResult records the selected backend for one set.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var adminReadOperation = compatibility.Operation[Input, photos.AdminSettings]{
	Name: AdminReadCapabilityName,
	Variants: []compatibility.Variant[Input, photos.AdminSettings]{
		{
			Name: "foto-setting-admin-v1", API: AdminAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(AdminAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (photos.AdminSettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: AdminAPIName, Version: 1, Method: "get"})
				if err != nil {
					return photos.AdminSettings{}, fmt.Errorf("call %s.get v1: %w", AdminAPIName, err)
				}
				return decodeAdminSettings(data)
			},
		},
	},
}

var adminSetOperation = compatibility.Operation[photos.AdminChange, MutationResult]{
	Name: AdminSetCapabilityName,
	Variants: []compatibility.Variant[photos.AdminChange, MutationResult]{
		{
			Name: "foto-setting-admin-v1", API: AdminAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(AdminAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, change photos.AdminChange) (MutationResult, error) {
				parameters := encodeAdminChange(change)
				if len(parameters) == 0 {
					return MutationResult{}, fmt.Errorf("photos admin set: empty patch")
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: AdminAPIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", AdminAPIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

func APINames() []string { return []string{AdminAPIName} }

func SelectAdminRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := adminReadOperation.Select(target)
	return selection, err
}

func SelectAdminSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := adminSetOperation.Select(target)
	return selection, err
}

func ExecuteAdminRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (photos.AdminSettings, compatibility.Selection, error) {
	return adminReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteAdminSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change photos.AdminChange) (MutationResult, compatibility.Selection, error) {
	result, selection, err := adminSetOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}
