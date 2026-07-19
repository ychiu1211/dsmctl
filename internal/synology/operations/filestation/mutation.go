package filestation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	CreateFolderAPIName = "SYNO.FileStation.CreateFolder"
	RenameAPIName       = "SYNO.FileStation.Rename"
	CopyMoveAPIName     = "SYNO.FileStation.CopyMove"
	DeleteAPIName       = "SYNO.FileStation.Delete"
	CompressAPIName     = "SYNO.FileStation.Compress"
	ExtractAPIName      = "SYNO.FileStation.Extract"
	FavoriteAPIName     = "SYNO.FileStation.Favorite"

	CreateFolderCapabilityName = "file.createfolder"
	RenameCapabilityName       = "file.rename"
	CopyMoveCapabilityName     = "file.copymove"
	DeleteCapabilityName       = "file.delete"
	CompressCapabilityName     = "file.compress"
	ExtractCapabilityName      = "file.extract"
	FavoriteCapabilityName     = "file.favorite"
)

// MutationResult is the normalized outcome of a FileStation mutation: the async
// task id when one was used, the paths the operation created or targeted, and
// the public URL when a sharing link was created.
type MutationResult struct {
	TaskID string   `json:"task_id,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	URL    string   `json:"url,omitempty"`
}

type CreateFolderInput struct {
	Parent        string
	Name          string
	CreateParents bool
}

type RenameInput struct {
	Path    string
	NewName string
}

type TransferInput struct {
	Sources    []string
	DestFolder string
	Overwrite  bool
	Move       bool
}

type DeleteInput struct {
	Paths []string
}

type CompressInput struct {
	Sources     []string
	DestArchive string
	Format      string
	Level       string
	Password    string
}

type ExtractInput struct {
	Archive    string
	DestFolder string
	Password   string
	Overwrite  bool
}

type FavoriteAddInput struct {
	Path string
	Name string
}

type FavoriteDeleteInput struct {
	Path string
}

// startAndPollTask starts an asynchronous FileStation operation and polls its
// status until DSM reports it finished, returning the task id.
func startAndPollTask(ctx context.Context, executor compatibility.Executor, api string, version int, startParams url.Values) (string, error) {
	startData, err := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: "start", Parameters: startParams})
	if err != nil {
		return "", fmt.Errorf("call %s.start: %w", api, err)
	}
	taskID, err := decodeTaskID(startData)
	if err != nil {
		return "", err
	}
	pollErr := pollTask(ctx, func() (bool, error) {
		statusData, statusErr := executor.Execute(ctx, compatibility.Request{API: api, Version: version, Method: "status", Parameters: url.Values{"taskid": {taskID}}})
		if statusErr != nil {
			return false, fmt.Errorf("call %s.status: %w", api, statusErr)
		}
		return decodeFinished(statusData)
	})
	if pollErr != nil {
		return taskID, pollErr
	}
	return taskID, nil
}

var createFolderOperation = compatibility.Operation[CreateFolderInput, MutationResult]{
	Name: CreateFolderCapabilityName,
	Variants: []compatibility.Variant[CreateFolderInput, MutationResult]{
		{
			Name: "filestation-createfolder-v2", API: CreateFolderAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(CreateFolderAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input CreateFolderInput) (MutationResult, error) {
				params := url.Values{
					"folder_path":  {input.Parent},
					"name":         {input.Name},
					"force_parent": {strconv.FormatBool(input.CreateParents)},
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: CreateFolderAPIName, Version: 2, Method: "create", Parameters: params})
				if err != nil {
					return MutationResult{}, fmt.Errorf("call %s.create: %w", CreateFolderAPIName, err)
				}
				listing, err := decodeListing(data, "created folder")
				if err != nil {
					return MutationResult{}, err
				}
				return MutationResult{Paths: entryPaths(listing.Entries)}, nil
			},
		},
	},
}

var renameOperation = compatibility.Operation[RenameInput, MutationResult]{
	Name: RenameCapabilityName,
	Variants: []compatibility.Variant[RenameInput, MutationResult]{
		{
			Name: "filestation-rename-v2", API: RenameAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(RenameAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input RenameInput) (MutationResult, error) {
				params := url.Values{"path": {input.Path}, "name": {input.NewName}}
				data, err := executor.Execute(ctx, compatibility.Request{API: RenameAPIName, Version: 2, Method: "rename", Parameters: params})
				if err != nil {
					return MutationResult{}, fmt.Errorf("call %s.rename: %w", RenameAPIName, err)
				}
				listing, err := decodeListing(data, "renamed entry")
				if err != nil {
					return MutationResult{}, err
				}
				return MutationResult{Paths: entryPaths(listing.Entries)}, nil
			},
		},
	},
}

var copyMoveOperation = compatibility.Operation[TransferInput, MutationResult]{
	Name: CopyMoveCapabilityName,
	Variants: []compatibility.Variant[TransferInput, MutationResult]{
		{
			Name: "filestation-copymove-v3", API: CopyMoveAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(CopyMoveAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, input TransferInput) (MutationResult, error) {
				params := url.Values{
					"path":              {encodePathList(input.Sources)},
					"dest_folder_path":  {input.DestFolder},
					"overwrite":         {strconv.FormatBool(input.Overwrite)},
					"remove_src":        {strconv.FormatBool(input.Move)},
					"accurate_progress": {"true"},
				}
				taskID, err := startAndPollTask(ctx, executor, CopyMoveAPIName, 3, params)
				if err != nil {
					return MutationResult{}, err
				}
				return MutationResult{TaskID: taskID}, nil
			},
		},
	},
}

var deleteOperation = compatibility.Operation[DeleteInput, MutationResult]{
	Name: DeleteCapabilityName,
	Variants: []compatibility.Variant[DeleteInput, MutationResult]{
		{
			Name: "filestation-delete-v2", API: DeleteAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(DeleteAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input DeleteInput) (MutationResult, error) {
				params := url.Values{
					"path":              {encodePathList(input.Paths)},
					"recursive":         {"true"},
					"accurate_progress": {"true"},
				}
				taskID, err := startAndPollTask(ctx, executor, DeleteAPIName, 2, params)
				if err != nil {
					return MutationResult{}, err
				}
				return MutationResult{TaskID: taskID, Paths: input.Paths}, nil
			},
		},
	},
}

var compressOperation = compatibility.Operation[CompressInput, MutationResult]{
	Name: CompressCapabilityName,
	Variants: []compatibility.Variant[CompressInput, MutationResult]{
		{
			Name: "filestation-compress-v3", API: CompressAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(CompressAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, input CompressInput) (MutationResult, error) {
				params := url.Values{
					"path":           {encodePathList(input.Sources)},
					"dest_file_path": {input.DestArchive},
				}
				if input.Format != "" {
					params.Set("format", input.Format)
				}
				if input.Level != "" {
					params.Set("level", input.Level)
				}
				if input.Password != "" {
					params.Set("password", input.Password)
				}
				taskID, err := startAndPollTask(ctx, executor, CompressAPIName, 3, params)
				if err != nil {
					return MutationResult{}, err
				}
				return MutationResult{TaskID: taskID, Paths: []string{input.DestArchive}}, nil
			},
		},
	},
}

var extractOperation = compatibility.Operation[ExtractInput, MutationResult]{
	Name: ExtractCapabilityName,
	Variants: []compatibility.Variant[ExtractInput, MutationResult]{
		{
			Name: "filestation-extract-v2", API: ExtractAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(ExtractAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ExtractInput) (MutationResult, error) {
				params := url.Values{
					"file_path":        {input.Archive},
					"dest_folder_path": {input.DestFolder},
					"overwrite":        {strconv.FormatBool(input.Overwrite)},
				}
				if input.Password != "" {
					params.Set("password", input.Password)
				}
				taskID, err := startAndPollTask(ctx, executor, ExtractAPIName, 2, params)
				if err != nil {
					return MutationResult{}, err
				}
				return MutationResult{TaskID: taskID, Paths: []string{input.DestFolder}}, nil
			},
		},
	},
}

var favoriteListOperation = compatibility.Operation[struct{}, filestation.Favorites]{
	Name: FavoriteCapabilityName,
	Variants: []compatibility.Variant[struct{}, filestation.Favorites]{
		{
			Name: "filestation-favorite-list-v2", API: FavoriteAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(FavoriteAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ struct{}) (filestation.Favorites, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: FavoriteAPIName, Version: 2, Method: "list"})
				if err != nil {
					return filestation.Favorites{}, fmt.Errorf("call %s.list: %w", FavoriteAPIName, err)
				}
				return decodeFavorites(data)
			},
		},
	},
}

var favoriteAddOperation = compatibility.Operation[FavoriteAddInput, struct{}]{
	Name: FavoriteCapabilityName,
	Variants: []compatibility.Variant[FavoriteAddInput, struct{}]{
		{
			Name: "filestation-favorite-add-v2", API: FavoriteAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(FavoriteAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input FavoriteAddInput) (struct{}, error) {
				params := url.Values{"path": {input.Path}}
				if input.Name != "" {
					params.Set("name", input.Name)
				}
				_, err := executor.Execute(ctx, compatibility.Request{API: FavoriteAPIName, Version: 2, Method: "add", Parameters: params})
				if err != nil {
					return struct{}{}, fmt.Errorf("call %s.add: %w", FavoriteAPIName, err)
				}
				return struct{}{}, nil
			},
		},
	},
}

var favoriteDeleteOperation = compatibility.Operation[FavoriteDeleteInput, struct{}]{
	Name: FavoriteCapabilityName,
	Variants: []compatibility.Variant[FavoriteDeleteInput, struct{}]{
		{
			Name: "filestation-favorite-delete-v2", API: FavoriteAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(FavoriteAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input FavoriteDeleteInput) (struct{}, error) {
				_, err := executor.Execute(ctx, compatibility.Request{API: FavoriteAPIName, Version: 2, Method: "delete", Parameters: url.Values{"path": {input.Path}}})
				if err != nil {
					return struct{}{}, fmt.Errorf("call %s.delete: %w", FavoriteAPIName, err)
				}
				return struct{}{}, nil
			},
		},
	},
}

func entryPaths(entries []filestation.Entry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Path != "" {
			paths = append(paths, entry.Path)
		}
	}
	return paths
}

func decodeFinished(data json.RawMessage) (bool, error) {
	var resp struct {
		Finished *bool `json:"finished"`
	}
	if err := unmarshalObject(data, "task status", &resp); err != nil {
		return false, err
	}
	return resp.Finished != nil && *resp.Finished, nil
}

func decodeFavorites(data json.RawMessage) (filestation.Favorites, error) {
	var resp struct {
		Total     *int `json:"total"`
		Favorites []struct {
			Name   *string `json:"name"`
			Path   *string `json:"path"`
			Status *string `json:"status"`
		} `json:"favorites"`
	}
	if err := unmarshalObject(data, "favorites", &resp); err != nil {
		return filestation.Favorites{}, err
	}
	favorites := make([]filestation.Favorite, 0, len(resp.Favorites))
	for _, entry := range resp.Favorites {
		favorites = append(favorites, filestation.Favorite{
			Name:   deref(entry.Name),
			Path:   deref(entry.Path),
			Status: deref(entry.Status),
		})
	}
	total := len(favorites)
	if resp.Total != nil {
		total = *resp.Total
	}
	return filestation.Favorites{Total: total, Favorites: favorites}, nil
}

// MutationAPINames lists every FileStation mutation API so the façade discovers
// them in one round trip before selecting.
func MutationAPINames() []string {
	return []string{
		CreateFolderAPIName, RenameAPIName, CopyMoveAPIName, DeleteAPIName,
		CompressAPIName, ExtractAPIName, FavoriteAPIName,
		SharingAPIName, BackgroundTaskAPIName,
	}
}

func SelectCreateFolder(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := createFolderOperation.Select(target)
	return selection, err
}

func SelectRename(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := renameOperation.Select(target)
	return selection, err
}

func SelectCopyMove(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := copyMoveOperation.Select(target)
	return selection, err
}

func SelectDelete(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := deleteOperation.Select(target)
	return selection, err
}

func SelectCompress(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := compressOperation.Select(target)
	return selection, err
}

func SelectExtract(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := extractOperation.Select(target)
	return selection, err
}

func SelectFavorite(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := favoriteListOperation.Select(target)
	return selection, err
}

func ExecuteCreateFolder(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input CreateFolderInput) (MutationResult, compatibility.Selection, error) {
	return createFolderOperation.Run(ctx, target, executor, input)
}

func ExecuteRename(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input RenameInput) (MutationResult, compatibility.Selection, error) {
	return renameOperation.Run(ctx, target, executor, input)
}

func ExecuteCopyMove(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input TransferInput) (MutationResult, compatibility.Selection, error) {
	return copyMoveOperation.Run(ctx, target, executor, input)
}

func ExecuteDelete(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input DeleteInput) (MutationResult, compatibility.Selection, error) {
	return deleteOperation.Run(ctx, target, executor, input)
}

func ExecuteCompress(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input CompressInput) (MutationResult, compatibility.Selection, error) {
	return compressOperation.Run(ctx, target, executor, input)
}

func ExecuteExtract(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ExtractInput) (MutationResult, compatibility.Selection, error) {
	return extractOperation.Run(ctx, target, executor, input)
}

func ExecuteFavoriteList(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (filestation.Favorites, compatibility.Selection, error) {
	return favoriteListOperation.Run(ctx, target, executor, struct{}{})
}

func ExecuteFavoriteAdd(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input FavoriteAddInput) (compatibility.Selection, error) {
	_, selection, err := favoriteAddOperation.Run(ctx, target, executor, input)
	return selection, err
}

func ExecuteFavoriteDelete(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input FavoriteDeleteInput) (compatibility.Selection, error) {
	_, selection, err := favoriteDeleteOperation.Run(ctx, target, executor, input)
	return selection, err
}
