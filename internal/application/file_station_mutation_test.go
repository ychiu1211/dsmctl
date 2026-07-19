package application

import (
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

func TestValidateFileChange(t *testing.T) {
	tests := []struct {
		name    string
		request filestation.ChangeRequest
		wantErr string
	}{
		{
			name:    "create_folder ok",
			request: filestation.ChangeRequest{Action: filestation.ActionCreateFolder, CreateFolder: &filestation.CreateFolderChange{Parent: "/home", Name: "dir"}},
		},
		{
			name:    "create_folder relative parent",
			request: filestation.ChangeRequest{Action: filestation.ActionCreateFolder, CreateFolder: &filestation.CreateFolderChange{Parent: "home", Name: "dir"}},
			wantErr: "absolute",
		},
		{
			name:    "rename with separator in name",
			request: filestation.ChangeRequest{Action: filestation.ActionRename, Rename: &filestation.RenameChange{Path: "/home/a", NewName: "b/c"}},
			wantErr: "base name",
		},
		{
			name:    "delete volume root refused",
			request: filestation.ChangeRequest{Action: filestation.ActionDelete, Delete: &filestation.DeleteChange{Paths: []string{"/home"}}},
			wantErr: "root",
		},
		{
			name:    "delete nested ok",
			request: filestation.ChangeRequest{Action: filestation.ActionDelete, Delete: &filestation.DeleteChange{Paths: []string{"/home/dir/file"}}},
		},
		{
			name:    "copy needs sources",
			request: filestation.ChangeRequest{Action: filestation.ActionCopy, Transfer: &filestation.TransferChange{DestFolder: "/home/d"}},
			wantErr: "at least one source",
		},
		{
			name:    "compress literal password refused",
			request: filestation.ChangeRequest{Action: filestation.ActionCompress, Compress: &filestation.CompressChange{Sources: []string{"/home/a"}, DestArchive: "/home/a.zip", PasswordRef: "secret"}},
			wantErr: "env:NAME",
		},
		{
			name:    "sharelink create ok",
			request: filestation.ChangeRequest{Action: filestation.ActionShareLinkCreate, ShareLink: &filestation.ShareLinkChange{Path: "/home/a", PasswordRef: "env:LINK_PW"}},
		},
		{
			name:    "sharelink delete needs id",
			request: filestation.ChangeRequest{Action: filestation.ActionShareLinkDelete, ShareLink: &filestation.ShareLinkChange{}},
			wantErr: "link_id",
		},
		{
			name:    "unknown action",
			request: filestation.ChangeRequest{Action: "explode"},
			wantErr: "unsupported",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFileChange(test.request)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestActionSupportedAndPasswordRef(t *testing.T) {
	caps := synology.FileStationCapabilities{CreateFolder: true, Sharing: true}
	if !actionSupported(caps, filestation.ActionCreateFolder) || !actionSupported(caps, filestation.ActionShareLinkCreate) {
		t.Fatalf("expected create_folder and sharelink_create supported")
	}
	if actionSupported(caps, filestation.ActionDelete) {
		t.Fatalf("delete should be unsupported when capability is false")
	}
	ref := passwordRefFor(filestation.ChangeRequest{Action: filestation.ActionShareLinkCreate, ShareLink: &filestation.ShareLinkChange{PasswordRef: "env:PW"}})
	if ref != "env:PW" {
		t.Fatalf("passwordRefFor = %q", ref)
	}
}

func TestFileResourceIDIsSortedAndStable(t *testing.T) {
	targets := []filestation.PathObservation{{Path: "/home/b"}, {Path: "/home/a"}}
	dest := &filestation.PathObservation{Path: "/home/dst"}
	id := fileResourceID(targets, dest)
	if id != "/home/a\n/home/b\n/home/dst" {
		t.Fatalf("resource id = %q", id)
	}
}
