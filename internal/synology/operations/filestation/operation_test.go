package filestation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type routeExecutor struct {
	t      *testing.T
	routes map[string]string
	// fail names the "API method" keys that must return an API error instead of
	// a body, so negative paths (for example a denied permission probe) can be
	// exercised.
	fail map[string]bool
}

func (e routeExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	key := request.API + " " + request.Method
	if e.fail[key] {
		return nil, fmt.Errorf("simulated API error for %q", key)
	}
	body, ok := e.routes[key]
	if !ok {
		e.t.Fatalf("unexpected request %q", key)
	}
	return json.RawMessage(body), nil
}

func fsTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(InfoAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(ListAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(SearchAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(DirSizeAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(MD5APIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(VirtualFolderAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(CheckPermissionAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	return target
}

// Shapes below mirror the documented DSM 7 FileStation payloads; a file's size
// is returned as a quoted string to exercise flexInt64.
const (
	listBody = `{"total":2,"offset":0,"files":[
		{"path":"/home/dir","isdir":true,"name":"dir","additional":{"real_path":"/volume1/home/dir","size":0,"time":{"atime":1600000000,"mtime":1600000100,"ctime":1600000200,"crtime":1600000300},"owner":{"user":"deryck","group":"users","uid":1024,"gid":100},"perm":{"posix":755,"is_acl_mode":false},"type":"dir"}},
		{"path":"/home/file.txt","isdir":false,"name":"file.txt","additional":{"real_path":"/volume1/home/file.txt","size":"1048576","time":{"mtime":1600000100},"owner":{"user":"deryck"},"perm":{"posix":644,"is_acl_mode":false},"type":"text"}}
	]}`
	listShareBody = `{"total":1,"offset":0,"shares":[{"path":"/home","isdir":true,"name":"home","additional":{"real_path":"/volume1/home","perm":{"share_right":"RW","posix":777,"is_acl_mode":false,"is_share_readonly":false},"mount_point_type":"","owner":{"user":"deryck"}}}]}`
	vfolderBody   = `{"total":1,"offset":0,"folders":[{"path":"/mnt/remote","isdir":true,"name":"remote","additional":{"mount_point_type":"cifs"}}]}`
	searchStart   = `{"taskid":"FileStation_search_1"}`
	searchList    = `{"total":1,"offset":0,"finished":true,"files":[{"path":"/home/file.txt","isdir":false,"name":"file.txt","additional":{"size":10}}]}`
	dirSizeStart  = `{"taskid":"dirsize_1"}`
	dirSizeStatus = `{"finished":true,"num_dir":3,"num_file":10,"total_size":123456}`
	md5Start      = `{"taskid":"md5_1"}`
	md5Status     = `{"finished":true,"md5":"d41d8cd98f00b204e9800998ecf8427e"}`
	serviceBody   = `{"is_manager":true,"support_sharing":true,"support_virtual_protocol":["cifs","nfs"],"hostname":"nas"}`
	emptyObject   = `{}`
)

func TestListDecodesEntriesWithAdditional(t *testing.T) {
	listing, selection, err := ExecuteList(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.List list": listBody,
	}}, ListInput{Path: "/home"})
	if err != nil {
		t.Fatalf("ExecuteList() error = %v", err)
	}
	if !selection.Supported || selection.API != ListAPIName || selection.Version != 2 {
		t.Fatalf("selection = %#v", selection)
	}
	if listing.Path != "/home" || listing.Total != 2 || len(listing.Entries) != 2 {
		t.Fatalf("listing = %#v", listing)
	}
	dir := listing.Entries[0]
	if !dir.IsDir || dir.Name != "dir" || dir.RealPath != "/volume1/home/dir" {
		t.Fatalf("dir entry = %#v", dir)
	}
	if dir.Time == nil || dir.Time.Modified != 1600000100 || dir.Owner == nil || dir.Owner.User != "deryck" || dir.Owner.UID != 1024 {
		t.Fatalf("dir metadata = %#v", dir)
	}
	if dir.Permission == nil || dir.Permission.POSIX != 755 {
		t.Fatalf("dir perm = %#v", dir.Permission)
	}
	file := listing.Entries[1]
	if file.IsDir || file.Size != 1048576 {
		t.Fatalf("file entry (size as quoted string) = %#v", file)
	}
}

func TestListShareDecodesSharesKey(t *testing.T) {
	listing, _, err := ExecuteListShare(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.List list_share": listShareBody,
	}}, ListShareInput{})
	if err != nil {
		t.Fatalf("ExecuteListShare() error = %v", err)
	}
	if listing.Total != 1 || len(listing.Entries) != 1 {
		t.Fatalf("listing = %#v", listing)
	}
	share := listing.Entries[0]
	if share.Name != "home" || !share.IsDir || share.Permission == nil || share.Permission.ShareRight != "RW" {
		t.Fatalf("share = %#v perm = %#v", share, share.Permission)
	}
}

func TestVirtualFolderDecodesFoldersKey(t *testing.T) {
	listing, _, err := ExecuteVirtualFolder(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.VirtualFolder list": vfolderBody,
	}}, VirtualFolderInput{})
	if err != nil {
		t.Fatalf("ExecuteVirtualFolder() error = %v", err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].MountType != "cifs" {
		t.Fatalf("listing = %#v", listing)
	}
}

func TestSearchPollsToFinishedAndCleans(t *testing.T) {
	result, _, err := ExecuteSearch(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.Search start": searchStart,
		"SYNO.FileStation.Search list":  searchList,
		"SYNO.FileStation.Search clean": emptyObject,
	}}, SearchInput{Path: "/home", Pattern: "*.txt"})
	if err != nil {
		t.Fatalf("ExecuteSearch() error = %v", err)
	}
	if !result.Finished || result.Total != 1 || len(result.Entries) != 1 || result.Entries[0].Name != "file.txt" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDirSizePollsToFinished(t *testing.T) {
	result, _, err := ExecuteDirSize(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.DirSize start":  dirSizeStart,
		"SYNO.FileStation.DirSize status": dirSizeStatus,
		"SYNO.FileStation.DirSize stop":   emptyObject,
	}}, DirSizeInput{Paths: []string{"/home"}})
	if err != nil {
		t.Fatalf("ExecuteDirSize() error = %v", err)
	}
	if !result.Finished || result.NumDir != 3 || result.NumFile != 10 || result.TotalSize != 123456 {
		t.Fatalf("result = %#v", result)
	}
}

func TestMD5PollsToFinished(t *testing.T) {
	result, _, err := ExecuteMD5(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.MD5 start":  md5Start,
		"SYNO.FileStation.MD5 status": md5Status,
	}}, MD5Input{Path: "/home/file.txt"})
	if err != nil {
		t.Fatalf("ExecuteMD5() error = %v", err)
	}
	if !result.Finished || result.MD5 != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("result = %#v", result)
	}
}

func TestInfoDecodes(t *testing.T) {
	service, _, err := ExecuteInfo(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.Info get": serviceBody,
	}})
	if err != nil {
		t.Fatalf("ExecuteInfo() error = %v", err)
	}
	if !service.IsManager || !service.SupportSharing || service.Hostname != "nas" || len(service.SupportVirtualProtocols) != 2 {
		t.Fatalf("service = %#v", service)
	}
}

func TestCheckPermissionWritableAndDenied(t *testing.T) {
	writable, _, err := ExecuteCheckPermission(context.Background(), fsTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.CheckPermission write": emptyObject,
	}}, CheckPermissionInput{Path: "/home", Filename: "x"})
	if err != nil {
		t.Fatalf("ExecuteCheckPermission() writable error = %v", err)
	}
	if !writable.Writable {
		t.Fatalf("expected writable, got %#v", writable)
	}
	denied, _, err := ExecuteCheckPermission(context.Background(), fsTarget(), routeExecutor{t: t,
		routes: map[string]string{},
		fail:   map[string]bool{"SYNO.FileStation.CheckPermission write": true},
	}, CheckPermissionInput{Path: "/etc"})
	if err != nil {
		t.Fatalf("ExecuteCheckPermission() denied returned error = %v", err)
	}
	if denied.Writable {
		t.Fatalf("expected not writable, got %#v", denied)
	}
}

func TestDecodersRejectMalformedShapes(t *testing.T) {
	tests := []struct {
		name string
		fn   func(json.RawMessage) error
		data string
		want string
	}{
		{name: "listing not object", fn: func(d json.RawMessage) error { _, e := decodeListing(d, "x"); return e }, data: `[]`, want: "expected an object"},
		{name: "listing no array", fn: func(d json.RawMessage) error { _, e := decodeListing(d, "x"); return e }, data: `{"total":0}`, want: "no files, shares, or folders"},
		{name: "taskid missing", fn: func(d json.RawMessage) error { _, e := decodeTaskID(d); return e }, data: `{}`, want: "taskid"},
		{name: "service not object", fn: func(d json.RawMessage) error { _, e := decodeService(d); return e }, data: `1`, want: "expected an object"},
		{name: "dirsize not object", fn: func(d json.RawMessage) error { _, e := decodeDirSize(d); return e }, data: `[]`, want: "expected an object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.fn(json.RawMessage(test.data)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestAPINamesCoverSurface(t *testing.T) {
	got := APINames()
	want := map[string]bool{
		InfoAPIName: true, ListAPIName: true, SearchAPIName: true, DirSizeAPIName: true,
		MD5APIName: true, VirtualFolderAPIName: true, CheckPermissionAPIName: true,
		UploadAPIName: true, DownloadAPIName: true, ThumbAPIName: true,
	}
	if len(got) != len(want) {
		t.Fatalf("APINames() = %#v", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("unexpected API name %q", name)
		}
	}
}

func TestSelectTransferBackends(t *testing.T) {
	target := fsTarget()
	target.SetAPI(UploadAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(DownloadAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	up, err := SelectUpload(target)
	if err != nil || !up.Supported || up.API != UploadAPIName || up.Version != 2 {
		t.Fatalf("SelectUpload = %#v, err = %v", up, err)
	}
	down, err := SelectDownload(target)
	if err != nil || !down.Supported || down.API != DownloadAPIName || down.Version != 2 {
		t.Fatalf("SelectDownload = %#v, err = %v", down, err)
	}
}
