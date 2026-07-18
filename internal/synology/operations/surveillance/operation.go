// Package surveillance implements read operations for the Synology Surveillance
// Station package: system info (SYNO.SurveillanceStation.Info GetInfo) and the
// camera inventory (SYNO.SurveillanceStation.Camera List). Every variant is
// gated on the installed SurveillanceStation package so a NAS without it fails
// closed. Field names stay behind this package and are decoded tolerantly across
// Surveillance versions.
package surveillance

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/surveillance"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// PackageID is the DSM package that owns the Surveillance Station APIs.
const PackageID = "SurveillanceStation"

const (
	InfoAPIName   = "SYNO.SurveillanceStation.Info"
	CameraAPIName = "SYNO.SurveillanceStation.Camera"

	InfoReadCapabilityName   = "surveillance.info.read"
	CameraReadCapabilityName = "surveillance.camera.read"
)

// baselinePackage gates every variant on Surveillance Station 1.x+, covering the
// stable Info/Camera surface (verified on 9.2.x). A future major with a verified
// difference adds a higher-priority variant with a narrower range.
var baselinePackage = compatibility.PackageVersionRange(
	PackageID, compatibility.ParsePackageVersion("1.0"), compatibility.PackageVersion{},
)

type Input struct{}

var infoOperation = compatibility.Operation[Input, surveillance.Info]{
	Name: InfoReadCapabilityName,
	Variants: []compatibility.Variant[Input, surveillance.Info]{
		{
			Name: "surveillance-info-v1", API: InfoAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(InfoAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (surveillance.Info, error) {
				// Version 0 asks the executor for the API's discovered max version;
				// older GetInfo versions omit hostname/timezone.
				data, err := executor.Execute(ctx, compatibility.Request{API: InfoAPIName, Version: 0, Method: "GetInfo"})
				if err != nil {
					return surveillance.Info{}, fmt.Errorf("call %s.GetInfo: %w", InfoAPIName, err)
				}
				return decodeInfo(data)
			},
		},
	},
}

var cameraOperation = compatibility.Operation[Input, surveillance.Cameras]{
	Name: CameraReadCapabilityName,
	Variants: []compatibility.Variant[Input, surveillance.Cameras]{
		{
			Name: "surveillance-camera-v1", API: CameraAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(CameraAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (surveillance.Cameras, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: CameraAPIName, Version: 0, Method: "List",
					JSONParameters: map[string]any{"limit": 1000, "offset": 0},
				})
				if err != nil {
					return surveillance.Cameras{}, fmt.Errorf("call %s.List: %w", CameraAPIName, err)
				}
				return decodeCameras(data)
			},
		},
	},
}

func APINames() []string { return []string{InfoAPIName, CameraAPIName} }

func SelectInfo(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := infoOperation.Select(target)
	return selection, err
}

func SelectCamera(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := cameraOperation.Select(target)
	return selection, err
}

func ExecuteInfo(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (surveillance.Info, compatibility.Selection, error) {
	return infoOperation.Run(ctx, target, executor, Input{})
}

func ExecuteCamera(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (surveillance.Cameras, compatibility.Selection, error) {
	return cameraOperation.Run(ctx, target, executor, Input{})
}
