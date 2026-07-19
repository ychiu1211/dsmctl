package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	filestationops "github.com/ychiu1211/dsmctl/internal/synology/operations/filestation"
)

type FileStationService = filestation.Service
type FileStationListing = filestation.Listing
type FileStationInfo = filestation.Info
type FileStationSearchResult = filestation.SearchResult
type FileStationDirSize = filestation.DirSize
type FileStationMD5 = filestation.MD5
type FileStationPermissionCheck = filestation.PermissionCheck
type FileStationCapabilities = filestation.Capabilities

type FileStationListShareQuery = filestation.ListShareQuery
type FileStationListQuery = filestation.ListQuery
type FileStationGetInfoQuery = filestation.GetInfoQuery
type FileStationSearchQuery = filestation.SearchQuery
type FileStationDirSizeQuery = filestation.DirSizeQuery
type FileStationMD5Query = filestation.MD5Query
type FileStationVirtualFolderQuery = filestation.VirtualFolderQuery
type FileStationCheckPermissionQuery = filestation.CheckPermissionQuery

// FileStationInfoState reads FileStation-wide service information.
func (c *Client) FileStationInfoState(ctx context.Context) (FileStationService, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationService{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	service, _, err := filestationops.ExecuteInfo(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return FileStationService{}, fmt.Errorf("get FileStation info: %w", err)
	}
	c.target.AddCapability(filestationops.InfoReadCapabilityName)
	return service, nil
}

// FileStationListShares lists shared folders visible to the current session.
func (c *Client) FileStationListShares(ctx context.Context, query FileStationListShareQuery) (FileStationListing, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationListing{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	listing, _, err := filestationops.ExecuteListShare(ctx, c.target, lockedExecutor{client: c}, filestationops.ListShareInput{
		Offset:        query.Offset,
		Limit:         query.Limit,
		OnlyWritable:  query.OnlyWritable,
		SortBy:        query.SortBy,
		SortDirection: query.SortDirection,
	})
	if err != nil {
		return FileStationListing{}, fmt.Errorf("list shared folders: %w", err)
	}
	c.target.AddCapability(filestationops.ListReadCapabilityName)
	return listing, nil
}

// FileStationList lists one folder's entries.
func (c *Client) FileStationList(ctx context.Context, query FileStationListQuery) (FileStationListing, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationListing{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	listing, _, err := filestationops.ExecuteList(ctx, c.target, lockedExecutor{client: c}, filestationops.ListInput{
		Path:          query.Path,
		Offset:        query.Offset,
		Limit:         query.Limit,
		SortBy:        query.SortBy,
		SortDirection: query.SortDirection,
		Pattern:       query.Pattern,
		FileType:      query.FileType,
	})
	if err != nil {
		return FileStationListing{}, fmt.Errorf("list folder %q: %w", query.Path, err)
	}
	c.target.AddCapability(filestationops.ListReadCapabilityName)
	return listing, nil
}

// FileStationGetInfo reads detail for one or more entries.
func (c *Client) FileStationGetInfo(ctx context.Context, query FileStationGetInfoQuery) (FileStationInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationInfo{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	info, _, err := filestationops.ExecuteGetInfo(ctx, c.target, lockedExecutor{client: c}, filestationops.GetInfoInput{Paths: query.Paths})
	if err != nil {
		return FileStationInfo{}, fmt.Errorf("get file information: %w", err)
	}
	c.target.AddCapability(filestationops.ListReadCapabilityName)
	return info, nil
}

// FileStationSearch searches a folder subtree and returns completed results.
func (c *Client) FileStationSearch(ctx context.Context, query FileStationSearchQuery) (FileStationSearchResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationSearchResult{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	result, _, err := filestationops.ExecuteSearch(ctx, c.target, lockedExecutor{client: c}, filestationops.SearchInput{
		Path:      query.Path,
		Pattern:   query.Pattern,
		Extension: query.Extension,
		FileType:  query.FileType,
		Recursive: query.Recursive,
	})
	if err != nil {
		return FileStationSearchResult{}, fmt.Errorf("search folder %q: %w", query.Path, err)
	}
	c.target.AddCapability(filestationops.SearchReadCapabilityName)
	return result, nil
}

// FileStationDirSize computes the aggregate size of one or more folders.
func (c *Client) FileStationDirSize(ctx context.Context, query FileStationDirSizeQuery) (FileStationDirSize, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationDirSize{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	result, _, err := filestationops.ExecuteDirSize(ctx, c.target, lockedExecutor{client: c}, filestationops.DirSizeInput{Paths: query.Paths})
	if err != nil {
		return FileStationDirSize{}, fmt.Errorf("compute directory size: %w", err)
	}
	c.target.AddCapability(filestationops.DirSizeReadCapabilityName)
	return result, nil
}

// FileStationMD5 computes a file's MD5 digest.
func (c *Client) FileStationMD5(ctx context.Context, query FileStationMD5Query) (FileStationMD5, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationMD5{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	result, _, err := filestationops.ExecuteMD5(ctx, c.target, lockedExecutor{client: c}, filestationops.MD5Input{Path: query.Path})
	if err != nil {
		return FileStationMD5{}, fmt.Errorf("compute MD5 of %q: %w", query.Path, err)
	}
	c.target.AddCapability(filestationops.MD5ReadCapabilityName)
	return result, nil
}

// FileStationVirtualFolders lists mounted virtual folders.
func (c *Client) FileStationVirtualFolders(ctx context.Context, query FileStationVirtualFolderQuery) (FileStationListing, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationListing{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	listing, _, err := filestationops.ExecuteVirtualFolder(ctx, c.target, lockedExecutor{client: c}, filestationops.VirtualFolderInput{
		Offset: query.Offset,
		Limit:  query.Limit,
	})
	if err != nil {
		return FileStationListing{}, fmt.Errorf("list virtual folders: %w", err)
	}
	c.target.AddCapability(filestationops.VirtualFolderReadCapabilityName)
	return listing, nil
}

// FileStationCheckPermission probes whether the current session may write.
func (c *Client) FileStationCheckPermission(ctx context.Context, query FileStationCheckPermissionQuery) (FileStationPermissionCheck, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, filestationops.APINames()...); err != nil {
		return FileStationPermissionCheck{}, fmt.Errorf("prepare FileStation target: %w", err)
	}
	result, _, err := filestationops.ExecuteCheckPermission(ctx, c.target, lockedExecutor{client: c}, filestationops.CheckPermissionInput{
		Path:          query.Path,
		Filename:      query.Filename,
		Overwrite:     query.Overwrite,
		CreateParents: query.CreateParents,
	})
	if err != nil {
		return FileStationPermissionCheck{}, fmt.Errorf("check write permission for %q: %w", query.Path, err)
	}
	c.target.AddCapability(filestationops.CheckPermissionReadCapabilityName)
	return result, nil
}

// FileStationCapabilities reports the FileStation reads dsmctl exposes, each
// selected independently against the discovered DSM APIs.
func (c *Client) FileStationCapabilities(ctx context.Context) (FileStationCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareCompatibilityTargetLocked(ctx, allFileStationAPINames()...); err != nil {
		return FileStationCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare FileStation capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		filestationops.SelectInfo,
		filestationops.SelectList,
		filestationops.SelectSearch,
		filestationops.SelectDirSize,
		filestationops.SelectMD5,
		filestationops.SelectVirtualFolder,
		filestationops.SelectCheckPermission,
		filestationops.SelectDownload,
		filestationops.SelectUpload,
		filestationops.SelectCreateFolder,
		filestationops.SelectRename,
		filestationops.SelectCopyMove,
		filestationops.SelectDelete,
		filestationops.SelectCompress,
		filestationops.SelectExtract,
		filestationops.SelectFavorite,
		filestationops.SelectSharing,
		filestationops.SelectBackgroundTask,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return FileStationCapabilities{}, CompatibilityReport{}, fmt.Errorf("select FileStation backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilityNames := []string{
		filestationops.InfoReadCapabilityName,
		filestationops.ListReadCapabilityName,
		filestationops.SearchReadCapabilityName,
		filestationops.DirSizeReadCapabilityName,
		filestationops.MD5ReadCapabilityName,
		filestationops.VirtualFolderReadCapabilityName,
		filestationops.CheckPermissionReadCapabilityName,
		filestationops.DownloadCapabilityName,
		filestationops.UploadCapabilityName,
		filestationops.CreateFolderCapabilityName,
		filestationops.RenameCapabilityName,
		filestationops.CopyMoveCapabilityName,
		filestationops.DeleteCapabilityName,
		filestationops.CompressCapabilityName,
		filestationops.ExtractCapabilityName,
		filestationops.FavoriteCapabilityName,
		filestationops.SharingCapabilityName,
		filestationops.BackgroundTaskCapabilityName,
	}
	for index, name := range capabilityNames {
		if supported(index) {
			c.target.AddCapability(name)
		}
	}
	capabilities := FileStationCapabilities{
		Module:            filestation.ModuleName,
		InfoRead:          supported(0),
		ListRead:          supported(1),
		SearchRead:        supported(2),
		DirSizeRead:       supported(3),
		MD5Read:           supported(4),
		VirtualFolderRead: supported(5),
		PermissionCheck:   supported(6),
		Download:          supported(7),
		Upload:            supported(8),
		CreateFolder:      supported(9),
		Rename:            supported(10),
		Copy:              supported(11),
		Move:              supported(11),
		Delete:            supported(12),
		Compress:          supported(13),
		Extract:           supported(14),
		Favorite:          supported(15),
		Sharing:           supported(16),
		BackgroundTask:    supported(17),
	}
	return capabilities, c.target.Report(selections...), nil
}
