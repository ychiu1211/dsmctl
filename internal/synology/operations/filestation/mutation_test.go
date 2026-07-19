package filestation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

func mutTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(CreateFolderAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(RenameAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(CopyMoveAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	target.SetAPI(DeleteAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(CompressAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	target.SetAPI(ExtractAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(FavoriteAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 2})
	target.SetAPI(SharingAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	target.SetAPI(BackgroundTaskAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	return target
}

func TestCreateFolderReturnsCreatedPath(t *testing.T) {
	result, _, err := ExecuteCreateFolder(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.CreateFolder create": `{"folders":[{"path":"/home/new","name":"new","isdir":true}]}`,
	}}, CreateFolderInput{Parent: "/home", Name: "new"})
	if err != nil {
		t.Fatalf("ExecuteCreateFolder() error = %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "/home/new" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRenameReturnsNewPath(t *testing.T) {
	result, _, err := ExecuteRename(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.Rename rename": `{"files":[{"path":"/home/renamed.txt","name":"renamed.txt","isdir":false}]}`,
	}}, RenameInput{Path: "/home/old.txt", NewName: "renamed.txt"})
	if err != nil {
		t.Fatalf("ExecuteRename() error = %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "/home/renamed.txt" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCopyMovePollsToFinished(t *testing.T) {
	result, _, err := ExecuteCopyMove(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.CopyMove start":  `{"taskid":"copymove_1"}`,
		"SYNO.FileStation.CopyMove status": `{"finished":true,"progress":1}`,
	}}, TransferInput{Sources: []string{"/home/a"}, DestFolder: "/home/dst", Move: true})
	if err != nil {
		t.Fatalf("ExecuteCopyMove() error = %v", err)
	}
	if result.TaskID != "copymove_1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDeletePollsToFinished(t *testing.T) {
	result, _, err := ExecuteDelete(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.Delete start":  `{"taskid":"del_1"}`,
		"SYNO.FileStation.Delete status": `{"finished":true}`,
	}}, DeleteInput{Paths: []string{"/home/x", "/home/y"}})
	if err != nil {
		t.Fatalf("ExecuteDelete() error = %v", err)
	}
	if result.TaskID != "del_1" || len(result.Paths) != 2 {
		t.Fatalf("result = %#v", result)
	}
}

func TestSharingCreateAndListDecode(t *testing.T) {
	created, _, err := ExecuteSharingCreate(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.Sharing create": `{"links":[{"id":"AbC123","url":"https://gofile.me/x/AbC123"}]}`,
	}}, SharingCreateInput{Path: "/home/f.txt"})
	if err != nil {
		t.Fatalf("ExecuteSharingCreate() error = %v", err)
	}
	if created.URL != "https://gofile.me/x/AbC123" || len(created.Paths) != 1 || created.Paths[0] != "AbC123" {
		t.Fatalf("created = %#v", created)
	}
	links, _, err := ExecuteSharingList(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.Sharing list": `{"total":1,"links":[{"id":"AbC123","name":"f.txt","path":"/home/f.txt","url":"https://gofile.me/x/AbC123","isFolder":false,"has_password":true,"status":"valid"}]}`,
	}})
	if err != nil {
		t.Fatalf("ExecuteSharingList() error = %v", err)
	}
	if links.Total != 1 || len(links.Links) != 1 || links.Links[0].ID != "AbC123" || !links.Links[0].HasPassword {
		t.Fatalf("links = %#v", links)
	}
}

func TestFavoritesAndBackgroundTasksDecode(t *testing.T) {
	favorites, _, err := ExecuteFavoriteList(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.Favorite list": `{"total":1,"favorites":[{"name":"home","path":"/home","status":"valid"}]}`,
	}})
	if err != nil {
		t.Fatalf("ExecuteFavoriteList() error = %v", err)
	}
	if favorites.Total != 1 || favorites.Favorites[0].Path != "/home" {
		t.Fatalf("favorites = %#v", favorites)
	}
	tasks, _, err := ExecuteBackgroundTaskList(context.Background(), mutTarget(), routeExecutor{t: t, routes: map[string]string{
		"SYNO.FileStation.BackgroundTask list": `{"total":1,"tasks":[{"taskid":"bt_1","api":"SYNO.FileStation.CopyMove","finished":false,"processing_path":"/home/big"}]}`,
	}})
	if err != nil {
		t.Fatalf("ExecuteBackgroundTaskList() error = %v", err)
	}
	if tasks.Total != 1 || tasks.Tasks[0].TaskID != "bt_1" || tasks.Tasks[0].Finished {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestMutationDecodersRejectMalformed(t *testing.T) {
	tests := []struct {
		name string
		fn   func(json.RawMessage) error
		data string
		want string
	}{
		{name: "sharing links not object", fn: func(d json.RawMessage) error { _, e := decodeSharingLinks(d); return e }, data: `[]`, want: "expected an object"},
		{name: "sharing create no link", fn: func(d json.RawMessage) error { _, e := decodeSharingCreate(d); return e }, data: `{"links":[]}`, want: "no link"},
		{name: "finished not object", fn: func(d json.RawMessage) error { _, e := decodeFinished(d); return e }, data: `1`, want: "expected an object"},
		{name: "favorites not object", fn: func(d json.RawMessage) error { _, e := decodeFavorites(d); return e }, data: `"x"`, want: "expected an object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.fn(json.RawMessage(test.data)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}
