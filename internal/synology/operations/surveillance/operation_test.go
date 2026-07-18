package surveillance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (executor *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.requests = append(executor.requests, request)
	if r, ok := executor.responses[request.API]; ok {
		return r, nil
	}
	return json.RawMessage(`{}`), nil
}

func surveillanceTarget(packageVersion string) compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(InfoAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 8})
	target.SetAPI(CameraAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 9})
	if packageVersion != "" {
		target.SetInstalledPackages([]compatibility.InstalledPackage{
			{ID: PackageID, Version: compatibility.ParsePackageVersion(packageVersion), Running: true},
		})
	}
	return target
}

func TestInfoDecodesLiveShape(t *testing.T) {
	// Shape confirmed live on Surveillance 9.2.5: version parts are strings.
	target := surveillanceTarget("9.2.5-11979")
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		InfoAPIName: json.RawMessage(`{
			"cameraNumber":0,"maxCameraSupport":75,"liscenseNumber":2,
			"hostname":"Derek_3018xs","timezone":"Taipei",
			"version":{"major":"9","minor":"2","small":"5","build":"11979"}
		}`),
	}}
	info, selection, err := ExecuteInfo(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteInfo() error = %v", err)
	}
	if selection.Backend != "surveillance-info-v1" || info.Version != "9.2.5-11979" || info.Hostname != "Derek_3018xs" ||
		info.CameraNumber != 0 || info.MaxCameraSupport != 75 || info.LicenseNumber != 2 || info.Timezone != "Taipei" {
		t.Fatalf("info = %#v", info)
	}
	// The read must ask for the API's max version (Version 0).
	if executor.requests[0].Version != 0 || executor.requests[0].Method != "GetInfo" {
		t.Fatalf("info request = %#v", executor.requests[0])
	}
}

func TestCameraDecodesList(t *testing.T) {
	target := surveillanceTarget("9.2.5-11979")
	executor := &capturingExecutor{responses: map[string]json.RawMessage{
		CameraAPIName: json.RawMessage(`{"total":1,"cameras":[{"id":3,"newName":"Front Door","ip":"10.0.0.5","vendor":"Synology","model":"BC500","enabled":true,"status":1}]}`),
	}}
	cams, _, err := ExecuteCamera(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteCamera() error = %v", err)
	}
	if cams.Total != 1 || len(cams.Cameras) != 1 {
		t.Fatalf("cameras = %#v", cams)
	}
	cam := cams.Cameras[0]
	if cam.ID != 3 || cam.Name != "Front Door" || cam.IP != "10.0.0.5" || cam.Vendor != "Synology" || cam.Model != "BC500" || !cam.Enabled {
		t.Fatalf("camera = %#v", cam)
	}
}

func TestSelectFailsClosedWithoutPackage(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(InfoAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 8})
	target.SetInstalledPackages(nil)
	if selection, err := SelectInfo(target); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectInfo() without package = %#v, %v", selection, err)
	}
}
