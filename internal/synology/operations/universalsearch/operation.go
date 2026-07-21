// Package universalsearch implements read operations for the Synology Universal
// Search package (internal name Finder / SynoFinder): the file-index folder list
// (SYNO.Finder.FileIndexing.Folder list) and the overall index daemon status
// (SYNO.Finder.FileIndexing.Status get). Every variant is gated on the installed
// SynoFinder package so a NAS without it fails closed.
//
// The API namespace, methods, versions, field names, and package id below were
// live-verified against the DSM 7.3 lab (SynoFinder 1.9.0-0900) with a throwaway
// raw probe because codesearch is OAuth-blocked. Both APIs are served from
// entry.cgi and advertise only version 1.
package universalsearch

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/universalsearch"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// PackageID is the DSM package that owns the Universal Search (Finder) APIs.
const PackageID = "SynoFinder"

const (
	FolderAPIName = "SYNO.Finder.FileIndexing.Folder"
	StatusAPIName = "SYNO.Finder.FileIndexing.Status"

	FolderReadCapabilityName = "universal-search.folder.read"
	StatusReadCapabilityName = "universal-search.status.read"
)

// baselinePackage gates every variant on Universal Search 1.x+, covering the
// stable SYNO.Finder.FileIndexing.* surface (verified on 1.9.0). A future major
// with a verified difference adds a higher-priority variant with a narrower
// range.
var baselinePackage = compatibility.PackageVersionRange(
	PackageID, compatibility.ParsePackageVersion("1.0"), compatibility.PackageVersion{},
)

// Input is the empty request the NAS-wide reads take.
type Input struct{}

// folderOperation lists every folder in the Universal Search file index.
var folderOperation = compatibility.Operation[Input, universalsearch.IndexedFolders]{
	Name: FolderReadCapabilityName,
	Variants: []compatibility.Variant[Input, universalsearch.IndexedFolders]{
		{
			Name: "finder-fileindexing-folder-list-v1", API: FolderAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(FolderAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (universalsearch.IndexedFolders, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: FolderAPIName, Version: 1, Method: "list", ReadOnly: true,
				})
				if err != nil {
					return universalsearch.IndexedFolders{}, fmt.Errorf("call %s.list v1: %w", FolderAPIName, err)
				}
				return decodeFolders(data)
			},
		},
	},
}

// statusOperation reads the overall index daemon status.
var statusOperation = compatibility.Operation[Input, universalsearch.IndexStatus]{
	Name: StatusReadCapabilityName,
	Variants: []compatibility.Variant[Input, universalsearch.IndexStatus]{
		{
			Name: "finder-fileindexing-status-get-v1", API: StatusAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(StatusAPIName, 1), baselinePackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (universalsearch.IndexStatus, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: StatusAPIName, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return universalsearch.IndexStatus{}, fmt.Errorf("call %s.get v1: %w", StatusAPIName, err)
				}
				return decodeStatus(data)
			},
		},
	},
}

// APINames returns every DSM API this module may read so the facade can discover
// them in a single query before selecting any area.
func APINames() []string { return []string{FolderAPIName, StatusAPIName} }

func SelectFolders(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := folderOperation.Select(target)
	return selection, err
}

func SelectStatus(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := statusOperation.Select(target)
	return selection, err
}

func ExecuteFolders(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (universalsearch.IndexedFolders, compatibility.Selection, error) {
	return folderOperation.Run(ctx, target, executor, Input{})
}

func ExecuteStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (universalsearch.IndexStatus, compatibility.Selection, error) {
	return statusOperation.Run(ctx, target, executor, Input{})
}
