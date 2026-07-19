package filestation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
)

// flexInt64 decodes a JSON number that some DSM builds return as a quoted
// string. A missing or null value decodes to zero.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	if s == "" || s == "null" {
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt64(n)
		return nil
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		*f = flexInt64(fl)
		return nil
	}
	return nil
}

func unmarshalObject(data json.RawMessage, what string, destination any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("decode %s: empty response", what)
	}
	if trimmed[0] != '{' {
		return fmt.Errorf("decode %s: expected an object", what)
	}
	if err := json.Unmarshal(trimmed, destination); err != nil {
		return fmt.Errorf("decode %s: %w", what, err)
	}
	return nil
}

// encodePathList renders one or more FileStation paths as a DSM path parameter:
// a single path stays a plain string; multiple paths become a JSON array, which
// FileStation accepts for its multi-path methods.
func encodePathList(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		return ""
	}
	return string(encoded)
}

type rawOwner struct {
	User  *string `json:"user"`
	Group *string `json:"group"`
	UID   *int    `json:"uid"`
	GID   *int    `json:"gid"`
}

type rawTime struct {
	Atime  flexInt64 `json:"atime"`
	Mtime  flexInt64 `json:"mtime"`
	Ctime  flexInt64 `json:"ctime"`
	Crtime flexInt64 `json:"crtime"`
}

type rawAdditional struct {
	RealPath       *string         `json:"real_path"`
	Size           flexInt64       `json:"size"`
	Type           *string         `json:"type"`
	MountPointType *string         `json:"mount_point_type"`
	Owner          *rawOwner       `json:"owner"`
	Time           *rawTime        `json:"time"`
	Perm           json.RawMessage `json:"perm"`
}

type rawEntry struct {
	Path       *string        `json:"path"`
	Name       *string        `json:"name"`
	IsDir      *bool          `json:"isdir"`
	Additional *rawAdditional `json:"additional"`
}

func (e rawEntry) toDomain() filestation.Entry {
	entry := filestation.Entry{
		Path:  strings.TrimSpace(deref(e.Path)),
		Name:  strings.TrimSpace(deref(e.Name)),
		IsDir: e.IsDir != nil && *e.IsDir,
	}
	if e.Additional == nil {
		return entry
	}
	entry.RealPath = strings.TrimSpace(deref(e.Additional.RealPath))
	entry.Size = int64(e.Additional.Size)
	entry.ContentType = strings.TrimSpace(deref(e.Additional.Type))
	entry.MountType = strings.TrimSpace(deref(e.Additional.MountPointType))
	if o := e.Additional.Owner; o != nil {
		entry.Owner = &filestation.Owner{
			User:  strings.TrimSpace(deref(o.User)),
			Group: strings.TrimSpace(deref(o.Group)),
			UID:   derefInt(o.UID),
			GID:   derefInt(o.GID),
		}
	}
	if t := e.Additional.Time; t != nil {
		entry.Time = &filestation.Time{
			Access:   int64(t.Atime),
			Modified: int64(t.Mtime),
			Changed:  int64(t.Ctime),
			Created:  int64(t.Crtime),
		}
	}
	if perm := decodePermission(e.Additional.Perm); perm != nil {
		entry.Permission = perm
	}
	return entry
}

// decodePermission tolerates DSM returning perm as an object or, on some
// entries, a non-object it cannot classify (which is ignored).
func decodePermission(raw json.RawMessage) *filestation.Permission {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var perm struct {
		Posix           *int    `json:"posix"`
		IsACLMode       *bool   `json:"is_acl_mode"`
		ShareRight      *string `json:"share_right"`
		IsShareReadonly *bool   `json:"is_share_readonly"`
	}
	if err := json.Unmarshal(trimmed, &perm); err != nil {
		return nil
	}
	return &filestation.Permission{
		POSIX:      derefInt(perm.Posix),
		ACLMode:    perm.IsACLMode != nil && *perm.IsACLMode,
		ShareRight: strings.TrimSpace(deref(perm.ShareRight)),
		ReadOnly:   perm.IsShareReadonly != nil && *perm.IsShareReadonly,
	}
}

// decodeListing decodes list, list_share, getinfo, and virtual-folder responses,
// which carry their entries under "files", "shares", or "folders".
func decodeListing(data json.RawMessage, what string) (filestation.Listing, error) {
	var resp struct {
		Total   *int       `json:"total"`
		Offset  *int       `json:"offset"`
		Files   []rawEntry `json:"files"`
		Shares  []rawEntry `json:"shares"`
		Folders []rawEntry `json:"folders"`
	}
	if err := unmarshalObject(data, what, &resp); err != nil {
		return filestation.Listing{}, err
	}
	rawEntries := resp.Files
	if rawEntries == nil {
		rawEntries = resp.Shares
	}
	if rawEntries == nil {
		rawEntries = resp.Folders
	}
	if rawEntries == nil {
		return filestation.Listing{}, fmt.Errorf("decode %s: response contained no files, shares, or folders array", what)
	}
	entries := make([]filestation.Entry, 0, len(rawEntries))
	for _, raw := range rawEntries {
		entries = append(entries, raw.toDomain())
	}
	listing := filestation.Listing{Entries: entries, Total: len(entries)}
	if resp.Total != nil {
		listing.Total = *resp.Total
	}
	if resp.Offset != nil {
		listing.Offset = *resp.Offset
	}
	return listing, nil
}

func decodeSearch(data json.RawMessage) (filestation.SearchResult, error) {
	var resp struct {
		Total    *int       `json:"total"`
		Offset   *int       `json:"offset"`
		Finished *bool      `json:"finished"`
		Files    []rawEntry `json:"files"`
	}
	if err := unmarshalObject(data, "search results", &resp); err != nil {
		return filestation.SearchResult{}, err
	}
	entries := make([]filestation.Entry, 0, len(resp.Files))
	for _, raw := range resp.Files {
		entries = append(entries, raw.toDomain())
	}
	result := filestation.SearchResult{Entries: entries, Total: len(entries)}
	if resp.Total != nil {
		result.Total = *resp.Total
	}
	if resp.Offset != nil {
		result.Offset = *resp.Offset
	}
	result.Finished = resp.Finished != nil && *resp.Finished
	return result, nil
}

func decodeDirSize(data json.RawMessage) (filestation.DirSize, error) {
	var resp struct {
		Finished  *bool     `json:"finished"`
		NumDir    flexInt64 `json:"num_dir"`
		NumFile   flexInt64 `json:"num_file"`
		TotalSize flexInt64 `json:"total_size"`
	}
	if err := unmarshalObject(data, "directory size", &resp); err != nil {
		return filestation.DirSize{}, err
	}
	return filestation.DirSize{
		Finished:  resp.Finished != nil && *resp.Finished,
		NumDir:    int64(resp.NumDir),
		NumFile:   int64(resp.NumFile),
		TotalSize: int64(resp.TotalSize),
	}, nil
}

func decodeMD5(data json.RawMessage) (filestation.MD5, error) {
	var resp struct {
		Finished *bool   `json:"finished"`
		MD5      *string `json:"md5"`
	}
	if err := unmarshalObject(data, "MD5 result", &resp); err != nil {
		return filestation.MD5{}, err
	}
	digest := strings.TrimSpace(deref(resp.MD5))
	// DSM reports finished implicitly by returning a non-empty digest on some
	// builds; treat a populated digest as completion.
	finished := (resp.Finished != nil && *resp.Finished) || digest != ""
	return filestation.MD5{Finished: finished, MD5: digest}, nil
}

func decodeTaskID(data json.RawMessage) (string, error) {
	var resp struct {
		TaskID *string `json:"taskid"`
	}
	if err := unmarshalObject(data, "task id", &resp); err != nil {
		return "", err
	}
	taskID := strings.TrimSpace(deref(resp.TaskID))
	if taskID == "" {
		return "", fmt.Errorf("decode task id: DSM did not return a taskid")
	}
	return taskID, nil
}

func decodeService(data json.RawMessage) (filestation.Service, error) {
	var resp struct {
		Hostname               *string         `json:"hostname"`
		IsManager              *bool           `json:"is_manager"`
		SupportSharing         *bool           `json:"support_sharing"`
		SupportVirtualProtocol json.RawMessage `json:"support_virtual_protocol"`
	}
	if err := unmarshalObject(data, "FileStation info", &resp); err != nil {
		return filestation.Service{}, err
	}
	service := filestation.Service{
		Hostname:       strings.TrimSpace(deref(resp.Hostname)),
		IsManager:      resp.IsManager != nil && *resp.IsManager,
		SupportSharing: resp.SupportSharing != nil && *resp.SupportSharing,
	}
	service.SupportVirtualProtocols = decodeStringList(resp.SupportVirtualProtocol)
	return service, nil
}

// decodeStringList tolerates DSM returning support_virtual_protocol as an array
// of strings, an array of mixed values, or a bool (older builds).
func decodeStringList(raw json.RawMessage) []string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		var s string
		if err := json.Unmarshal(value, &s); err == nil && strings.TrimSpace(s) != "" {
			result = append(result, strings.TrimSpace(s))
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
