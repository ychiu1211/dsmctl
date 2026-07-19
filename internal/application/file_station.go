package application

import (
	"context"
	"fmt"
	"io"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

type FileStationCapabilitiesResult struct {
	NAS          string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.FileStationCapabilities `json:"capabilities" jsonschema:"FileStation reads currently exposed by dsmctl"`
	Report       synology.CompatibilityReport     `json:"report" jsonschema:"Discovered APIs and selected FileStation backends"`
}

type FileStationServiceResult struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Service synology.FileStationService `json:"service" jsonschema:"FileStation service information for the current session"`
}

type FileStationListingResult struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	Listing synology.FileStationListing `json:"listing" jsonschema:"Shared-folder, directory, or virtual-folder listing"`
}

type FileStationInfoResult struct {
	NAS  string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Info synology.FileStationInfo `json:"info" jsonschema:"Requested entry details"`
}

type FileStationSearchResult struct {
	NAS    string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Result synology.FileStationSearchResult `json:"result" jsonschema:"Completed search result"`
}

type FileStationDirSizeResult struct {
	NAS     string                      `json:"nas" jsonschema:"NAS profile used for the request"`
	DirSize synology.FileStationDirSize `json:"dir_size" jsonschema:"Aggregate directory size"`
}

type FileStationMD5Result struct {
	NAS string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	MD5 synology.FileStationMD5 `json:"md5" jsonschema:"Computed MD5 digest"`
}

type FileStationPermissionCheckResult struct {
	NAS        string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Permission synology.FileStationPermissionCheck `json:"permission" jsonschema:"Write-permission probe result"`
}

func (s *Service) GetFileStationCapabilities(ctx context.Context, requestedNAS string) (FileStationCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationCapabilitiesResult{}, err
	}
	capabilities, report, err := client.FileStationCapabilities(ctx)
	if err != nil {
		return FileStationCapabilitiesResult{}, authenticationError(name, err)
	}
	return FileStationCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetFileStationInfo(ctx context.Context, requestedNAS string) (FileStationServiceResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationServiceResult{}, err
	}
	service, err := client.FileStationInfoState(ctx)
	if err != nil {
		return FileStationServiceResult{}, authenticationError(name, err)
	}
	return FileStationServiceResult{NAS: name, Service: service}, nil
}

func (s *Service) ListFileStationShares(ctx context.Context, requestedNAS string, query filestation.ListShareQuery) (FileStationListingResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationListingResult{}, err
	}
	listing, err := client.FileStationListShares(ctx, query)
	if err != nil {
		return FileStationListingResult{}, authenticationError(name, err)
	}
	return FileStationListingResult{NAS: name, Listing: listing}, nil
}

func (s *Service) ListFileStationDirectory(ctx context.Context, requestedNAS string, query filestation.ListQuery) (FileStationListingResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationListingResult{}, err
	}
	listing, err := client.FileStationList(ctx, query)
	if err != nil {
		return FileStationListingResult{}, authenticationError(name, err)
	}
	return FileStationListingResult{NAS: name, Listing: listing}, nil
}

func (s *Service) GetFileStationEntryInfo(ctx context.Context, requestedNAS string, query filestation.GetInfoQuery) (FileStationInfoResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationInfoResult{}, err
	}
	info, err := client.FileStationGetInfo(ctx, query)
	if err != nil {
		return FileStationInfoResult{}, authenticationError(name, err)
	}
	return FileStationInfoResult{NAS: name, Info: info}, nil
}

func (s *Service) SearchFileStation(ctx context.Context, requestedNAS string, query filestation.SearchQuery) (FileStationSearchResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationSearchResult{}, err
	}
	result, err := client.FileStationSearch(ctx, query)
	if err != nil {
		return FileStationSearchResult{}, authenticationError(name, err)
	}
	return FileStationSearchResult{NAS: name, Result: result}, nil
}

func (s *Service) GetFileStationDirSize(ctx context.Context, requestedNAS string, query filestation.DirSizeQuery) (FileStationDirSizeResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationDirSizeResult{}, err
	}
	result, err := client.FileStationDirSize(ctx, query)
	if err != nil {
		return FileStationDirSizeResult{}, authenticationError(name, err)
	}
	return FileStationDirSizeResult{NAS: name, DirSize: result}, nil
}

func (s *Service) GetFileStationMD5(ctx context.Context, requestedNAS string, query filestation.MD5Query) (FileStationMD5Result, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationMD5Result{}, err
	}
	result, err := client.FileStationMD5(ctx, query)
	if err != nil {
		return FileStationMD5Result{}, authenticationError(name, err)
	}
	return FileStationMD5Result{NAS: name, MD5: result}, nil
}

func (s *Service) ListFileStationVirtualFolders(ctx context.Context, requestedNAS string, query filestation.VirtualFolderQuery) (FileStationListingResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationListingResult{}, err
	}
	listing, err := client.FileStationVirtualFolders(ctx, query)
	if err != nil {
		return FileStationListingResult{}, authenticationError(name, err)
	}
	return FileStationListingResult{NAS: name, Listing: listing}, nil
}

// FileStationDownloadResult reports the metadata of a completed download. The
// bytes are streamed to the caller-provided destination, not held here.
type FileStationDownloadResult struct {
	NAS         string `json:"nas" jsonschema:"NAS profile used for the request"`
	Path        string `json:"path" jsonschema:"Downloaded NAS path"`
	Size        int64  `json:"size" jsonschema:"Number of bytes streamed"`
	ContentType string `json:"content_type,omitempty" jsonschema:"Content type reported by DSM"`
	Filename    string `json:"filename,omitempty" jsonschema:"File name reported by DSM"`
}

// DownloadFileStationFile streams a NAS file to dst. Downloading reads the NAS
// and writes to a local destination the caller controls; it does not mutate the
// NAS, so it is exempt from plan/apply.
func (s *Service) DownloadFileStationFile(ctx context.Context, requestedNAS, path string, dst io.Writer) (FileStationDownloadResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationDownloadResult{}, err
	}
	content, err := client.DownloadFile(ctx, path)
	if err != nil {
		return FileStationDownloadResult{}, authenticationError(name, err)
	}
	defer content.Body.Close()
	written, err := io.Copy(dst, content.Body)
	if err != nil {
		return FileStationDownloadResult{}, fmt.Errorf("stream download of %q from NAS %q: %w", path, name, err)
	}
	return FileStationDownloadResult{
		NAS:         name,
		Path:        path,
		Size:        written,
		ContentType: content.ContentType,
		Filename:    content.Filename,
	}, nil
}

// ReadFileStationFile downloads a NAS file fully into memory, up to limit bytes.
// It is for callers that must return the content inline (for example an MCP
// tool); a file larger than the limit is refused so a large transfer is not
// buffered. limit must be positive.
func (s *Service) ReadFileStationFile(ctx context.Context, requestedNAS, path string, limit int64) ([]byte, FileStationDownloadResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return nil, FileStationDownloadResult{}, err
	}
	content, err := client.DownloadFile(ctx, path)
	if err != nil {
		return nil, FileStationDownloadResult{}, authenticationError(name, err)
	}
	defer content.Body.Close()
	data, err := io.ReadAll(io.LimitReader(content.Body, limit+1))
	if err != nil {
		return nil, FileStationDownloadResult{}, fmt.Errorf("read download of %q from NAS %q: %w", path, name, err)
	}
	if int64(len(data)) > limit {
		return nil, FileStationDownloadResult{}, fmt.Errorf("file %q exceeds the %d-byte inline download limit; use the CLI 'file get' to stream it", path, limit)
	}
	result := FileStationDownloadResult{
		NAS:         name,
		Path:        path,
		Size:        int64(len(data)),
		ContentType: content.ContentType,
		Filename:    content.Filename,
	}
	return data, result, nil
}

func (s *Service) CheckFileStationPermission(ctx context.Context, requestedNAS string, query filestation.CheckPermissionQuery) (FileStationPermissionCheckResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationPermissionCheckResult{}, err
	}
	result, err := client.FileStationCheckPermission(ctx, query)
	if err != nil {
		return FileStationPermissionCheckResult{}, authenticationError(name, err)
	}
	return FileStationPermissionCheckResult{NAS: name, Permission: result}, nil
}
