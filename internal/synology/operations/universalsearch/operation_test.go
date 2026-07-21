package universalsearch

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

// universalSearchTarget builds a target that advertises the Finder APIs and,
// when packageVersion is non-empty, the installed SynoFinder package. Fixtures
// use only generic names/paths per the WI-002 evidence policy.
func universalSearchTarget(packageVersion string, apis ...string) compatibility.Target {
	target := compatibility.NewTarget()
	if len(apis) == 0 {
		apis = []string{FolderAPIName, StatusAPIName}
	}
	for _, name := range apis {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	}
	if packageVersion != "" {
		target.SetInstalledPackages([]compatibility.InstalledPackage{
			{ID: PackageID, Version: compatibility.ParsePackageVersion(packageVersion), Running: true},
		})
	}
	return target
}

// twoFolderList mirrors the live SYNO.Finder.FileIndexing.Folder list shape
// (SynoFinder 1.9.0), sanitized: one active Drive home and one paused share that
// indexes only photo/video content.
const twoFolderList = `{"folder":[
	{"audio":false,"document":true,"group":"SYNO.SDS.Drive.Application:drive:displayname","name":"Synology Drive (testuser)","owner":"SynologyDrive","path":"/homes/testuser","paused":false,"photo":false,"privileged":true,"share_path_before_pause":"","video":false,"volume_to_be_clean":""},
	{"audio":false,"document":false,"group":"","name":"Media","owner":"","path":"/test-share/media","paused":true,"photo":true,"privileged":false,"share_path_before_pause":"/test-share/media","video":true,"volume_to_be_clean":""}
],"offset":0,"total":2}`

func TestDecodeFoldersLiveShape(t *testing.T) {
	target := universalSearchTarget("1.9.0-0900")
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == FolderAPIName && request.Method == "list" {
			return json.RawMessage(twoFolderList), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	folders, selection, err := ExecuteFolders(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteFolders() error = %v", err)
	}
	if selection.Backend != "finder-fileindexing-folder-list-v1" || folders.Total != 2 || len(folders.Folders) != 2 {
		t.Fatalf("folders = %#v (selection %#v)", folders, selection)
	}
	drive := folders.Folders[0]
	if drive.Path != "/homes/testuser" || drive.Owner != "SynologyDrive" || drive.Paused || !drive.Privileged || !drive.ContentTypes.Document {
		t.Fatalf("drive folder = %#v", drive)
	}
	media := folders.Folders[1]
	if media.Path != "/test-share/media" || !media.Paused || media.Privileged ||
		!media.ContentTypes.Photo || !media.ContentTypes.Video || media.ContentTypes.Audio ||
		media.SharePathBeforePause != "/test-share/media" {
		t.Fatalf("media folder = %#v", media)
	}
	// The read must be marked read-only.
	if request := lastFolderRequest(t, target); !request {
		t.Fatalf("folder list request must set ReadOnly")
	}
}

// lastFolderRequest re-runs the folder read with a capturing executor to assert
// the request is flagged read-only.
func lastFolderRequest(t *testing.T, target compatibility.Target) bool {
	t.Helper()
	var readOnly bool
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		readOnly = request.ReadOnly
		return json.RawMessage(twoFolderList), nil
	})
	if _, _, err := ExecuteFolders(context.Background(), target, executor); err != nil {
		t.Fatal(err)
	}
	return readOnly
}

// TestDecodeFoldersQuotedTotal proves a total returned as a quoted string (the
// recurring DSM flexInt quirk) decodes without erroring.
func TestDecodeFoldersQuotedTotal(t *testing.T) {
	target := universalSearchTarget("1.9.0-0900")
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"folder":[{"path":"/test-share/docs","paused":false}],"total":"1"}`), nil
	})
	folders, _, err := ExecuteFolders(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteFolders() error = %v", err)
	}
	if folders.Total != 1 || len(folders.Folders) != 1 || folders.Folders[0].Path != "/test-share/docs" {
		t.Fatalf("folders = %#v", folders)
	}
}

// TestDecodeFoldersMissingArrayRejected proves an envelope without the "folder"
// array is an error, never a silently-empty success.
func TestDecodeFoldersMissingArrayRejected(t *testing.T) {
	target := universalSearchTarget("1.9.0-0900")
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"total":0}`), nil
	})
	if _, _, err := ExecuteFolders(context.Background(), target, executor); err == nil {
		t.Fatal("expected an error for a response missing the folder array")
	}
}

func TestDecodeStatusIdle(t *testing.T) {
	target := universalSearchTarget("1.9.0-0900")
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == StatusAPIName && request.Method == "get" {
			return json.RawMessage(`{"status":{"index":"finished","term":"finished"}}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	status, selection, err := ExecuteStatus(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	if selection.Backend != "finder-fileindexing-status-get-v1" || status.Index != "finished" || status.Term != "finished" || status.Indexing {
		t.Fatalf("status = %#v (selection %#v)", status, selection)
	}
}

// TestDecodeStatusIndexing proves a non-finished sub-state marks the index as
// working and an optional progress percentage is captured (incl. as a string).
func TestDecodeStatusIndexing(t *testing.T) {
	target := universalSearchTarget("1.9.0-0900")
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{"status":{"index":"indexing","term":"finished","progress":"42"}}`), nil
	})
	status, _, err := ExecuteStatus(context.Background(), target, executor)
	if err != nil {
		t.Fatalf("ExecuteStatus() error = %v", err)
	}
	if !status.Indexing || status.Index != "indexing" {
		t.Fatalf("status = %#v", status)
	}
	if status.Progress == nil || *status.Progress != 42 {
		t.Fatalf("progress = %#v", status.Progress)
	}
}

// TestDecodeStatusMissingObjectRejected proves an envelope without the "status"
// object is an error.
func TestDecodeStatusMissingObjectRejected(t *testing.T) {
	target := universalSearchTarget("1.9.0-0900")
	executor := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})
	if _, _, err := ExecuteStatus(context.Background(), target, executor); err == nil {
		t.Fatal("expected an error for a response missing the status object")
	}
}

// TestSelectFailsClosedWithoutPackage proves both reads fail closed when the
// SynoFinder package is not installed, even though the APIs are advertised.
func TestSelectFailsClosedWithoutPackage(t *testing.T) {
	target := universalSearchTarget("") // APIs present, no package
	if selection, err := SelectFolders(target); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectFolders() without package = %#v, %v", selection, err)
	}
	if selection, err := SelectStatus(target); err == nil || selection.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("SelectStatus() without package = %#v, %v", selection, err)
	}
}

// TestSelectIndependentGating proves each area selects its own backend so a
// missing API family fails closed only for its own area.
func TestSelectIndependentGating(t *testing.T) {
	target := universalSearchTarget("1.9.0-0900", FolderAPIName) // folder only
	if selection, _ := SelectFolders(target); !selection.Supported {
		t.Fatalf("folder read should be supported: %#v", selection)
	}
	if selection, _ := SelectStatus(target); selection.Supported {
		t.Fatalf("status read should be unsupported without the Status API: %#v", selection)
	}
}
