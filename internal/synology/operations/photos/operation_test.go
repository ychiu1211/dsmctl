package photos

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/photos"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type capturingExecutor struct {
	request  compatibility.Request
	response json.RawMessage
}

func (executor *capturingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	executor.request = request
	if executor.response != nil {
		return executor.response, nil
	}
	return json.RawMessage(`{}`), nil
}

func photosTarget(packageVersion string, running bool) compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(AdminAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	if packageVersion != "" {
		target.SetInstalledPackages([]compatibility.InstalledPackage{
			{ID: PackageID, Version: compatibility.ParsePackageVersion(packageVersion), Running: running},
		})
	} else {
		target.SetInstalledPackages(nil)
	}
	return target
}

func TestAdminReadDecodesLiveShape(t *testing.T) {
	target := photosTarget("1.9.1-10928", true)
	executor := &capturingExecutor{response: json.RawMessage(`{
		"default_thumbnail_size":"sm","display_photo_info_to_guest":true,"enable_concept":true,
		"enable_converted_original_jpeg":false,"enable_person":true,"enable_personal_dsm_recycle_bin":false,
		"enable_shared_dsm_recycle_bin":false,"enable_similar":true,"enable_user_sharing":true,
		"exclude_extension":["tmp"],"need_hevc":false,"package_version":"1.9.1-10928"
	}`)}

	settings, selection, err := ExecuteAdminRead(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteAdminRead() error = %v", err)
	}
	if selection.Backend != "foto-setting-admin-v1" || !settings.FaceRecognition || !settings.ConceptGrouping ||
		!settings.SimilarGrouping || !settings.UserSharing || !settings.ShowInfoToGuest ||
		settings.DefaultThumbnailSize != "sm" || settings.PackageVersion != "1.9.1-10928" ||
		!reflect.DeepEqual(settings.ExcludeExtensions, []string{"tmp"}) {
		t.Fatalf("admin settings = %#v", settings)
	}
}

func TestAdminSetSendsOnlyPatchedFieldsWithDSMNames(t *testing.T) {
	target := photosTarget("1.9.1-10928", true)
	executor := &capturingExecutor{}
	face := false
	thumb := "m"
	if _, _, err := ExecuteAdminSet(context.Background(), target, executor, photos.AdminChange{FaceRecognition: &face, DefaultThumbnailSize: &thumb}); err != nil {
		t.Fatalf("ExecuteAdminSet() error = %v", err)
	}
	want := map[string]any{"enable_person": false, "default_thumbnail_size": "m"}
	if executor.request.Method != "set" || !reflect.DeepEqual(executor.request.JSONParameters, want) {
		t.Fatalf("admin set request = %#v, want %#v", executor.request, want)
	}
}

func TestSelectFailsClosedWithoutPackage(t *testing.T) {
	if selection, err := SelectAdminRead(photosTarget("", false)); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectAdminRead() without package = %#v, %v", selection, err)
	}
	if selection, err := SelectAdminSet(photosTarget("", false)); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectAdminSet() without package = %#v, %v", selection, err)
	}
}

func TestDecodeRejectsMissingCoreField(t *testing.T) {
	if _, err := decodeAdminSettings(json.RawMessage(`{"enable_concept":true}`)); err == nil {
		t.Fatal("decodeAdminSettings() accepted a response missing enable_person")
	}
}
