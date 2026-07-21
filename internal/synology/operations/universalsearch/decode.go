package universalsearch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/universalsearch"
)

// finishedState is the DSM index sub-state string reported when the index is
// idle (fully built). Any other non-empty value means the index is working.
const finishedState = "finished"

// flexInt decodes a JSON number that DSM may return as a quoted string. A
// missing or null value leaves the target untouched.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	if s == "" || s == "null" {
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt(n)
		return nil
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		*f = flexInt(fl)
		return nil
	}
	return nil
}

// unmarshalObject requires the response to be a JSON object so a malformed or
// wrong-shaped response is an error, never a silently-empty success.
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

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// folderEntry is decoded tolerantly across Universal Search versions.
type folderEntry struct {
	Path                 *string `json:"path"`
	Name                 *string `json:"name"`
	Owner                *string `json:"owner"`
	Group                *string `json:"group"`
	Paused               bool    `json:"paused"`
	Privileged           bool    `json:"privileged"`
	SharePathBeforePause *string `json:"share_path_before_pause"`
	Audio                bool    `json:"audio"`
	Video                bool    `json:"video"`
	Photo                bool    `json:"photo"`
	Document             bool    `json:"document"`
}

func (e folderEntry) toDomain() universalsearch.IndexedFolder {
	return universalsearch.IndexedFolder{
		Path:                 deref(e.Path),
		Name:                 deref(e.Name),
		Owner:                deref(e.Owner),
		Group:                deref(e.Group),
		Paused:               e.Paused,
		Privileged:           e.Privileged,
		SharePathBeforePause: deref(e.SharePathBeforePause),
		ContentTypes: universalsearch.ContentTypes{
			Audio:    e.Audio,
			Video:    e.Video,
			Photo:    e.Photo,
			Document: e.Document,
		},
	}
}

// decodeFolders decodes SYNO.Finder.FileIndexing.Folder list:
// {"folder": [ {...}, ... ], "total": N, "offset": 0}. The "folder" array is
// required (an absent array is a malformed response, not an empty index); an
// empty array is a valid empty index.
func decodeFolders(data json.RawMessage) (universalsearch.IndexedFolders, error) {
	var resp struct {
		Folder *[]folderEntry `json:"folder"`
		Total  *flexInt       `json:"total"`
	}
	if err := unmarshalObject(data, "Universal Search indexed folders", &resp); err != nil {
		return universalsearch.IndexedFolders{}, err
	}
	if resp.Folder == nil {
		return universalsearch.IndexedFolders{}, errors.New("decode Universal Search indexed folders: required field \"folder\" is missing")
	}
	folders := make([]universalsearch.IndexedFolder, 0, len(*resp.Folder))
	for _, entry := range *resp.Folder {
		folders = append(folders, entry.toDomain())
	}
	total := len(folders)
	if resp.Total != nil {
		total = int(*resp.Total)
	}
	return universalsearch.IndexedFolders{Total: total, Folders: folders}, nil
}

// decodeStatus decodes SYNO.Finder.FileIndexing.Status get:
// {"status": {"index": "finished", "term": "finished"}}. The nested "status"
// object is required; the two sub-states are DSM's raw strings and an optional
// progress percentage is captured when a running index reports it.
func decodeStatus(data json.RawMessage) (universalsearch.IndexStatus, error) {
	var resp struct {
		Status *struct {
			Index    *string  `json:"index"`
			Term     *string  `json:"term"`
			Progress *flexInt `json:"progress"`
		} `json:"status"`
	}
	if err := unmarshalObject(data, "Universal Search index status", &resp); err != nil {
		return universalsearch.IndexStatus{}, err
	}
	if resp.Status == nil {
		return universalsearch.IndexStatus{}, errors.New("decode Universal Search index status: required field \"status\" is missing")
	}
	index := deref(resp.Status.Index)
	term := deref(resp.Status.Term)
	status := universalsearch.IndexStatus{
		Index:    index,
		Term:     term,
		Indexing: isWorking(index) || isWorking(term),
	}
	if resp.Status.Progress != nil {
		progress := int(*resp.Status.Progress)
		status.Progress = &progress
	}
	return status, nil
}

// isWorking reports whether an index sub-state indicates active work. An empty
// (unreported) state is treated as not working so an unknown value never falsely
// claims the index is busy.
func isWorking(state string) bool {
	return state != "" && !strings.EqualFold(state, finishedState)
}
