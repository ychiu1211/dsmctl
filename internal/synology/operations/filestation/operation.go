// Package filestation implements read operations for the Synology FileStation
// WebAPI: FileStation service information (SYNO.FileStation.Info), shared-folder
// and directory listings and per-entry info (SYNO.FileStation.List), file search
// (SYNO.FileStation.Search), directory-size (SYNO.FileStation.DirSize) and MD5
// (SYNO.FileStation.MD5) computations, mounted virtual folders
// (SYNO.FileStation.VirtualFolder), and write-permission checks
// (SYNO.FileStation.CheckPermission).
//
// FileStation is a core DSM surface, discovered through SYNO.API.Info like any
// other WebAPI, so operations are gated only on the advertised API version — no
// installed-package evidence is involved. Search, DirSize, and MD5 are
// asynchronous: each starts a background task, polls it to completion, and
// cleans it up inside a single operation so callers receive one completed
// result.
package filestation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	InfoAPIName            = "SYNO.FileStation.Info"
	ListAPIName            = "SYNO.FileStation.List"
	SearchAPIName          = "SYNO.FileStation.Search"
	DirSizeAPIName         = "SYNO.FileStation.DirSize"
	MD5APIName             = "SYNO.FileStation.MD5"
	VirtualFolderAPIName   = "SYNO.FileStation.VirtualFolder"
	CheckPermissionAPIName = "SYNO.FileStation.CheckPermission"

	InfoReadCapabilityName            = "file.info.read"
	ListReadCapabilityName            = "file.list.read"
	SearchReadCapabilityName          = "file.search.read"
	DirSizeReadCapabilityName         = "file.dirsize.read"
	MD5ReadCapabilityName             = "file.md5.read"
	VirtualFolderReadCapabilityName   = "file.virtualfolder.read"
	CheckPermissionReadCapabilityName = "file.checkpermission.read"
)

// listAdditional is the set of per-entry detail fields requested from DSM. It is
// encoded as a JSON array because FileStation v2 expects that form.
var listAdditional = []string{"real_path", "size", "owner", "time", "perm", "type", "mount_point_type"}

// Asynchronous task polling. Small scratch paths finish almost immediately, so
// the first attempts poll quickly to catch a task that DSM computes and frees in
// a few milliseconds (a DirSize of a tiny folder), then the cadence relaxes for
// long-running tasks. The attempt cap protects against a task that never
// completes when the caller's context has no deadline.
//
// transientRetries tolerates DSM reporting "no such task" (code 599) around a
// task's start: DirSize and MD5 return a taskid from start but the task is not
// queryable for a brief window, and a trivial task may complete and be freed
// before the first status read. Early poll errors are retried before being
// surfaced.
const (
	pollFastInterval = 40 * time.Millisecond
	pollFastAttempts = 25
	pollInterval     = 300 * time.Millisecond
	pollAttempts     = 400
	transientRetries = 25
	// asyncRestartRounds retries the whole start→status sequence for DirSize and
	// MD5. A trivially small target can complete and be freed by DSM before its
	// first status read, which returns code 599 for the life of that task id;
	// restarting with a fresh task usually catches a subsequent run in time.
	asyncRestartRounds = 5
)

// Input types carry stable, normalized parameters. The operation package maps
// them onto DSM WebAPI parameter names.

type InfoInput struct{}

type ListShareInput struct {
	Offset        int
	Limit         int
	OnlyWritable  bool
	SortBy        string
	SortDirection string
}

type ListInput struct {
	Path          string
	Offset        int
	Limit         int
	SortBy        string
	SortDirection string
	Pattern       string
	FileType      string // all, file, or dir
}

type GetInfoInput struct {
	Paths []string
}

type SearchInput struct {
	Path      string
	Pattern   string
	Extension string
	FileType  string // all, file, or dir
	Recursive bool
}

type DirSizeInput struct {
	Paths []string
}

type MD5Input struct {
	Path string
}

type VirtualFolderInput struct {
	Offset int
	Limit  int
}

type CheckPermissionInput struct {
	Path          string
	Filename      string
	Overwrite     bool
	CreateParents bool
}

func boolValue(v bool) string { return strconv.FormatBool(v) }

func additionalValue(fields []string) string {
	encoded, err := json.Marshal(fields)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

func pollTask(ctx context.Context, poll func() (bool, error)) error {
	for attempt := 0; attempt < pollAttempts; attempt++ {
		finished, err := poll()
		if err != nil {
			// Tolerate the start/status registration race for a bounded number of
			// early attempts; a persistent error is still surfaced.
			if attempt >= transientRetries {
				return err
			}
		} else if finished {
			return nil
		}
		interval := pollInterval
		if attempt < pollFastAttempts {
			interval = pollFastInterval
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	return nil
}

var infoOperation = compatibility.Operation[InfoInput, filestation.Service]{
	Name: InfoReadCapabilityName,
	Variants: []compatibility.Variant[InfoInput, filestation.Service]{
		{
			Name: "filestation-info-v2", API: InfoAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(InfoAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ InfoInput) (filestation.Service, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: InfoAPIName, Version: 2, Method: "get"})
				if err != nil {
					return filestation.Service{}, fmt.Errorf("call %s.get: %w", InfoAPIName, err)
				}
				return decodeService(data)
			},
		},
	},
}

var listShareOperation = compatibility.Operation[ListShareInput, filestation.Listing]{
	Name: ListReadCapabilityName,
	Variants: []compatibility.Variant[ListShareInput, filestation.Listing]{
		{
			Name: "filestation-list-share-v2", API: ListAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(ListAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ListShareInput) (filestation.Listing, error) {
				params := url.Values{
					"additional":   {additionalValue(listAdditional)},
					"onlywritable": {boolValue(input.OnlyWritable)},
				}
				if input.Limit > 0 {
					params.Set("limit", strconv.Itoa(input.Limit))
				}
				if input.Offset > 0 {
					params.Set("offset", strconv.Itoa(input.Offset))
				}
				if input.SortBy != "" {
					params.Set("sort_by", input.SortBy)
				}
				if input.SortDirection != "" {
					params.Set("sort_direction", input.SortDirection)
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: ListAPIName, Version: 2, Method: "list_share", Parameters: params})
				if err != nil {
					return filestation.Listing{}, fmt.Errorf("call %s.list_share: %w", ListAPIName, err)
				}
				return decodeListing(data, "shared folders")
			},
		},
	},
}

var listOperation = compatibility.Operation[ListInput, filestation.Listing]{
	Name: ListReadCapabilityName,
	Variants: []compatibility.Variant[ListInput, filestation.Listing]{
		{
			Name: "filestation-list-v2", API: ListAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(ListAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ListInput) (filestation.Listing, error) {
				params := url.Values{
					"folder_path": {input.Path},
					"additional":  {additionalValue(listAdditional)},
				}
				if input.Limit > 0 {
					params.Set("limit", strconv.Itoa(input.Limit))
				}
				if input.Offset > 0 {
					params.Set("offset", strconv.Itoa(input.Offset))
				}
				if input.SortBy != "" {
					params.Set("sort_by", input.SortBy)
				}
				if input.SortDirection != "" {
					params.Set("sort_direction", input.SortDirection)
				}
				if input.Pattern != "" {
					params.Set("pattern", input.Pattern)
				}
				if input.FileType != "" {
					params.Set("filetype", input.FileType)
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: ListAPIName, Version: 2, Method: "list", Parameters: params})
				if err != nil {
					return filestation.Listing{}, fmt.Errorf("call %s.list: %w", ListAPIName, err)
				}
				listing, err := decodeListing(data, "directory entries")
				if err != nil {
					return filestation.Listing{}, err
				}
				listing.Path = input.Path
				return listing, nil
			},
		},
	},
}

var getInfoOperation = compatibility.Operation[GetInfoInput, filestation.Info]{
	Name: ListReadCapabilityName,
	Variants: []compatibility.Variant[GetInfoInput, filestation.Info]{
		{
			Name: "filestation-getinfo-v2", API: ListAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(ListAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input GetInfoInput) (filestation.Info, error) {
				params := url.Values{
					"path":       {encodePathList(input.Paths)},
					"additional": {additionalValue(listAdditional)},
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: ListAPIName, Version: 2, Method: "getinfo", Parameters: params})
				if err != nil {
					return filestation.Info{}, fmt.Errorf("call %s.getinfo: %w", ListAPIName, err)
				}
				listing, err := decodeListing(data, "file information")
				if err != nil {
					return filestation.Info{}, err
				}
				return filestation.Info{Entries: listing.Entries}, nil
			},
		},
	},
}

var virtualFolderOperation = compatibility.Operation[VirtualFolderInput, filestation.Listing]{
	Name: VirtualFolderReadCapabilityName,
	Variants: []compatibility.Variant[VirtualFolderInput, filestation.Listing]{
		{
			Name: "filestation-virtualfolder-v2", API: VirtualFolderAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(VirtualFolderAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input VirtualFolderInput) (filestation.Listing, error) {
				params := url.Values{"additional": {additionalValue(listAdditional)}}
				if input.Limit > 0 {
					params.Set("limit", strconv.Itoa(input.Limit))
				}
				if input.Offset > 0 {
					params.Set("offset", strconv.Itoa(input.Offset))
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: VirtualFolderAPIName, Version: 2, Method: "list", Parameters: params})
				if err != nil {
					return filestation.Listing{}, fmt.Errorf("call %s.list: %w", VirtualFolderAPIName, err)
				}
				return decodeListing(data, "virtual folders")
			},
		},
	},
}

var searchOperation = compatibility.Operation[SearchInput, filestation.SearchResult]{
	Name: SearchReadCapabilityName,
	Variants: []compatibility.Variant[SearchInput, filestation.SearchResult]{
		{
			Name: "filestation-search-v2", API: SearchAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(SearchAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input SearchInput) (filestation.SearchResult, error) {
				startParams := url.Values{
					"folder_path": {input.Path},
					"recursive":   {boolValue(input.Recursive)},
				}
				if input.Pattern != "" {
					startParams.Set("pattern", input.Pattern)
				}
				if input.Extension != "" {
					startParams.Set("extension", input.Extension)
				}
				if input.FileType != "" {
					startParams.Set("filetype", input.FileType)
				}
				startData, err := executor.Execute(ctx, compatibility.Request{API: SearchAPIName, Version: 2, Method: "start", Parameters: startParams})
				if err != nil {
					return filestation.SearchResult{}, fmt.Errorf("call %s.start: %w", SearchAPIName, err)
				}
				taskID, err := decodeTaskID(startData)
				if err != nil {
					return filestation.SearchResult{}, err
				}
				var result filestation.SearchResult
				pollErr := pollTask(ctx, func() (bool, error) {
					listParams := url.Values{
						"taskid":     {taskID},
						"additional": {additionalValue(listAdditional)},
					}
					listData, listErr := executor.Execute(ctx, compatibility.Request{API: SearchAPIName, Version: 2, Method: "list", Parameters: listParams})
					if listErr != nil {
						return false, fmt.Errorf("call %s.list: %w", SearchAPIName, listErr)
					}
					result, listErr = decodeSearch(listData)
					if listErr != nil {
						return false, listErr
					}
					return result.Finished, nil
				})
				// Best-effort cleanup regardless of poll outcome.
				cleanParams := url.Values{"taskid": {taskID}}
				_, _ = executor.Execute(ctx, compatibility.Request{API: SearchAPIName, Version: 2, Method: "clean", Parameters: cleanParams})
				if pollErr != nil {
					return filestation.SearchResult{}, pollErr
				}
				return result, nil
			},
		},
	},
}

var dirSizeOperation = compatibility.Operation[DirSizeInput, filestation.DirSize]{
	Name: DirSizeReadCapabilityName,
	Variants: []compatibility.Variant[DirSizeInput, filestation.DirSize]{
		{
			Name: "filestation-dirsize-v2", API: DirSizeAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(DirSizeAPIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input DirSizeInput) (filestation.DirSize, error) {
				startParams := url.Values{"path": {encodePathList(input.Paths)}}
				var result filestation.DirSize
				var lastErr error
				for round := 0; round < asyncRestartRounds; round++ {
					startData, err := executor.Execute(ctx, compatibility.Request{API: DirSizeAPIName, Version: 2, Method: "start", Parameters: startParams})
					if err != nil {
						return filestation.DirSize{}, fmt.Errorf("call %s.start: %w", DirSizeAPIName, err)
					}
					taskID, err := decodeTaskID(startData)
					if err != nil {
						return filestation.DirSize{}, err
					}
					pollErr := pollTask(ctx, func() (bool, error) {
						statusData, statusErr := executor.Execute(ctx, compatibility.Request{API: DirSizeAPIName, Version: 2, Method: "status", Parameters: url.Values{"taskid": {taskID}}})
						if statusErr != nil {
							return false, fmt.Errorf("call %s.status: %w", DirSizeAPIName, statusErr)
						}
						result, statusErr = decodeDirSize(statusData)
						if statusErr != nil {
							return false, statusErr
						}
						return result.Finished, nil
					})
					_, _ = executor.Execute(ctx, compatibility.Request{API: DirSizeAPIName, Version: 2, Method: "stop", Parameters: url.Values{"taskid": {taskID}}})
					if pollErr == nil {
						return result, nil
					}
					lastErr = pollErr
				}
				return filestation.DirSize{}, lastErr
			},
		},
	},
}

var md5Operation = compatibility.Operation[MD5Input, filestation.MD5]{
	Name: MD5ReadCapabilityName,
	Variants: []compatibility.Variant[MD5Input, filestation.MD5]{
		{
			Name: "filestation-md5-v2", API: MD5APIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(MD5APIName, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, input MD5Input) (filestation.MD5, error) {
				var result filestation.MD5
				var lastErr error
				for round := 0; round < asyncRestartRounds; round++ {
					startData, err := executor.Execute(ctx, compatibility.Request{API: MD5APIName, Version: 2, Method: "start", Parameters: url.Values{"file_path": {input.Path}}})
					if err != nil {
						return filestation.MD5{}, fmt.Errorf("call %s.start: %w", MD5APIName, err)
					}
					taskID, err := decodeTaskID(startData)
					if err != nil {
						return filestation.MD5{}, err
					}
					pollErr := pollTask(ctx, func() (bool, error) {
						statusData, statusErr := executor.Execute(ctx, compatibility.Request{API: MD5APIName, Version: 2, Method: "status", Parameters: url.Values{"taskid": {taskID}}})
						if statusErr != nil {
							return false, fmt.Errorf("call %s.status: %w", MD5APIName, statusErr)
						}
						result, statusErr = decodeMD5(statusData)
						if statusErr != nil {
							return false, statusErr
						}
						return result.Finished, nil
					})
					if pollErr == nil {
						return result, nil
					}
					lastErr = pollErr
				}
				return filestation.MD5{}, lastErr
			},
		},
	},
}

var checkPermissionOperation = compatibility.Operation[CheckPermissionInput, filestation.PermissionCheck]{
	Name: CheckPermissionReadCapabilityName,
	Variants: []compatibility.Variant[CheckPermissionInput, filestation.PermissionCheck]{
		{
			Name: "filestation-checkpermission-v3", API: CheckPermissionAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(CheckPermissionAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, input CheckPermissionInput) (filestation.PermissionCheck, error) {
				params := url.Values{
					"path":           {input.Path},
					"create_parents": {boolValue(input.CreateParents)},
					"overwrite":      {boolValue(input.Overwrite)},
				}
				if input.Filename != "" {
					params.Set("filename", input.Filename)
				}
				// DSM returns a success envelope when writable and an API error
				// otherwise; the write probe never mutates.
				_, err := executor.Execute(ctx, compatibility.Request{API: CheckPermissionAPIName, Version: 3, Method: "write", Parameters: params})
				writable := err == nil
				if err != nil && !compatibility.IsUnsupported(err) {
					// A permission-denied error is a valid negative answer, not a
					// transport failure; surface writable=false without an error.
					return filestation.PermissionCheck{Path: input.Path, Writable: false}, nil
				}
				return filestation.PermissionCheck{Path: input.Path, Writable: writable}, nil
			},
		},
	},
}

// APINames lists every FileStation API (reads plus the Upload/Download binary
// transports) so the façade can discover them in one round trip before selecting
// operations.
func APINames() []string {
	return []string{
		InfoAPIName, ListAPIName, SearchAPIName, DirSizeAPIName,
		MD5APIName, VirtualFolderAPIName, CheckPermissionAPIName,
		UploadAPIName, DownloadAPIName,
	}
}

func SelectInfo(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := infoOperation.Select(target)
	return selection, err
}

func SelectList(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := listOperation.Select(target)
	return selection, err
}

func SelectSearch(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := searchOperation.Select(target)
	return selection, err
}

func SelectDirSize(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := dirSizeOperation.Select(target)
	return selection, err
}

func SelectMD5(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := md5Operation.Select(target)
	return selection, err
}

func SelectVirtualFolder(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := virtualFolderOperation.Select(target)
	return selection, err
}

func SelectCheckPermission(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := checkPermissionOperation.Select(target)
	return selection, err
}

func ExecuteInfo(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (filestation.Service, compatibility.Selection, error) {
	return infoOperation.Run(ctx, target, executor, InfoInput{})
}

func ExecuteListShare(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ListShareInput) (filestation.Listing, compatibility.Selection, error) {
	return listShareOperation.Run(ctx, target, executor, input)
}

func ExecuteList(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ListInput) (filestation.Listing, compatibility.Selection, error) {
	return listOperation.Run(ctx, target, executor, input)
}

func ExecuteGetInfo(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input GetInfoInput) (filestation.Info, compatibility.Selection, error) {
	return getInfoOperation.Run(ctx, target, executor, input)
}

func ExecuteSearch(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input SearchInput) (filestation.SearchResult, compatibility.Selection, error) {
	return searchOperation.Run(ctx, target, executor, input)
}

func ExecuteDirSize(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input DirSizeInput) (filestation.DirSize, compatibility.Selection, error) {
	return dirSizeOperation.Run(ctx, target, executor, input)
}

func ExecuteMD5(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input MD5Input) (filestation.MD5, compatibility.Selection, error) {
	return md5Operation.Run(ctx, target, executor, input)
}

func ExecuteVirtualFolder(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input VirtualFolderInput) (filestation.Listing, compatibility.Selection, error) {
	return virtualFolderOperation.Run(ctx, target, executor, input)
}

func ExecuteCheckPermission(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input CheckPermissionInput) (filestation.PermissionCheck, compatibility.Selection, error) {
	return checkPermissionOperation.Run(ctx, target, executor, input)
}
