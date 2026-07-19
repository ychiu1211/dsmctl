package downloadstation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/downloadstation"
)

// flexInt64 / flexInt decode a JSON number that some Download Station releases
// return as a quoted string. A missing or null value decodes to zero.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	n, ok := parseFlexInt(b)
	if !ok {
		return nil
	}
	*f = flexInt64(n)
	return nil
}

type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	n, ok := parseFlexInt(b)
	if !ok {
		return nil
	}
	*f = flexInt(n)
	return nil
}

func parseFlexInt(b []byte) (int64, bool) {
	s := strings.TrimSpace(strings.Trim(string(b), `"`))
	if s == "" || s == "null" {
		return 0, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	if fl, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(fl), true
	}
	return 0, false
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

type infoResult struct {
	Version   string
	IsManager bool
}

func decodeInfo(data json.RawMessage) (infoResult, error) {
	var resp struct {
		IsManager     *bool   `json:"is_manager"`
		VersionString *string `json:"version_string"`
	}
	if err := unmarshalObject(data, "Download Station info", &resp); err != nil {
		return infoResult{}, err
	}
	if resp.IsManager == nil {
		return infoResult{}, errors.New("decode Download Station info: required field \"is_manager\" is missing")
	}
	version := ""
	if resp.VersionString != nil {
		version = strings.TrimSpace(*resp.VersionString)
	}
	return infoResult{Version: version, IsManager: *resp.IsManager}, nil
}

func decodeConfig(data json.RawMessage) (downloadstation.Config, error) {
	var resp struct {
		DefaultDestination  *string `json:"default_destination"`
		EmuleEnabled        *bool   `json:"emule_enabled"`
		UnzipServiceEnabled *bool   `json:"unzip_service_enabled"`
		BTMaxDownload       flexInt `json:"bt_max_download"`
		BTMaxUpload         flexInt `json:"bt_max_upload"`
		EmuleMaxDownload    flexInt `json:"emule_max_download"`
		EmuleMaxUpload      flexInt `json:"emule_max_upload"`
		FTPMaxDownload      flexInt `json:"ftp_max_download"`
		HTTPMaxDownload     flexInt `json:"http_max_download"`
		NZBMaxDownload      flexInt `json:"nzb_max_download"`
	}
	if err := unmarshalObject(data, "Download Station config", &resp); err != nil {
		return downloadstation.Config{}, err
	}
	if resp.EmuleEnabled == nil {
		return downloadstation.Config{}, errors.New("decode Download Station config: required field \"emule_enabled\" is missing")
	}
	destination := ""
	if resp.DefaultDestination != nil {
		destination = strings.TrimSpace(*resp.DefaultDestination)
	}
	return downloadstation.Config{
		DefaultDestination:  destination,
		EmuleEnabled:        *resp.EmuleEnabled,
		UnzipServiceEnabled: resp.UnzipServiceEnabled != nil && *resp.UnzipServiceEnabled,
		BTMaxDownloadKBs:    int(resp.BTMaxDownload),
		BTMaxUploadKBs:      int(resp.BTMaxUpload),
		EmuleMaxDownloadKBs: int(resp.EmuleMaxDownload),
		EmuleMaxUploadKBs:   int(resp.EmuleMaxUpload),
		FTPMaxDownloadKBs:   int(resp.FTPMaxDownload),
		HTTPMaxDownloadKBs:  int(resp.HTTPMaxDownload),
		NZBMaxDownloadKBs:   int(resp.NZBMaxDownload),
	}, nil
}

func decodeSchedule(data json.RawMessage) (downloadstation.Schedule, error) {
	var resp struct {
		Enabled      *bool `json:"enabled"`
		EmuleEnabled *bool `json:"emule_enabled"`
	}
	if err := unmarshalObject(data, "Download Station schedule", &resp); err != nil {
		return downloadstation.Schedule{}, err
	}
	if resp.Enabled == nil {
		return downloadstation.Schedule{}, errors.New("decode Download Station schedule: required field \"enabled\" is missing")
	}
	return downloadstation.Schedule{
		Enabled:      *resp.Enabled,
		EmuleEnabled: resp.EmuleEnabled != nil && *resp.EmuleEnabled,
	}, nil
}

func decodeStatistics(data json.RawMessage) (downloadstation.Statistics, error) {
	var resp struct {
		SpeedDownload *flexInt `json:"speed_download"`
		SpeedUpload   *flexInt `json:"speed_upload"`
	}
	if err := unmarshalObject(data, "Download Station statistics", &resp); err != nil {
		return downloadstation.Statistics{}, err
	}
	if resp.SpeedDownload == nil {
		return downloadstation.Statistics{}, errors.New("decode Download Station statistics: required field \"speed_download\" is missing")
	}
	stats := downloadstation.Statistics{SpeedDownload: int(*resp.SpeedDownload)}
	if resp.SpeedUpload != nil {
		stats.SpeedUpload = int(*resp.SpeedUpload)
	}
	return stats, nil
}

func decodeTasks(data json.RawMessage) (downloadstation.Tasks, error) {
	var resp struct {
		Total *int         `json:"total"`
		Tasks *[]taskEntry `json:"tasks"`
	}
	if err := unmarshalObject(data, "Download Station tasks", &resp); err != nil {
		return downloadstation.Tasks{}, err
	}
	if resp.Tasks == nil {
		return downloadstation.Tasks{}, errors.New("decode Download Station tasks: required field \"tasks\" is missing")
	}
	tasks := make([]downloadstation.Task, 0, len(*resp.Tasks))
	for _, entry := range *resp.Tasks {
		tasks = append(tasks, entry.toDomain())
	}
	total := len(tasks)
	if resp.Total != nil {
		total = *resp.Total
	}
	return downloadstation.Tasks{Total: total, Tasks: tasks}, nil
}

// taskEntry is decoded tolerantly across Download Station versions.
type taskEntry struct {
	ID         *string `json:"id"`
	Type       *string `json:"type"`
	Username   *string `json:"username"`
	Title      *string `json:"title"`
	Size       flexInt64
	Status     *string `json:"status"`
	Additional *struct {
		Detail *struct {
			Destination *string `json:"destination"`
		} `json:"detail"`
		Transfer *struct {
			SizeDownloaded flexInt64 `json:"size_downloaded"`
			SizeUploaded   flexInt64 `json:"size_uploaded"`
			SpeedDownload  flexInt   `json:"speed_download"`
			SpeedUpload    flexInt   `json:"speed_upload"`
		} `json:"transfer"`
	} `json:"additional"`
}

// UnmarshalJSON is custom so "size" can use the flexInt64 tolerance without
// colliding with the anonymous-struct tag set.
func (e *taskEntry) UnmarshalJSON(b []byte) error {
	type alias taskEntry
	aux := struct {
		Size flexInt64 `json:"size"`
		*alias
	}{alias: (*alias)(e)}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	e.Size = aux.Size
	return nil
}

func (e taskEntry) toDomain() downloadstation.Task {
	task := downloadstation.Task{
		ID:       strings.TrimSpace(deref(e.ID)),
		Type:     strings.TrimSpace(deref(e.Type)),
		Username: strings.TrimSpace(deref(e.Username)),
		Title:    strings.TrimSpace(deref(e.Title)),
		Size:     int64(e.Size),
		Status:   strings.TrimSpace(deref(e.Status)),
	}
	if e.Additional != nil {
		if e.Additional.Detail != nil {
			task.Destination = strings.TrimSpace(deref(e.Additional.Detail.Destination))
		}
		if e.Additional.Transfer != nil {
			task.Transfer = downloadstation.TaskTransfer{
				SizeDownloaded: int64(e.Additional.Transfer.SizeDownloaded),
				SizeUploaded:   int64(e.Additional.Transfer.SizeUploaded),
				SpeedDownload:  int(e.Additional.Transfer.SpeedDownload),
				SpeedUpload:    int(e.Additional.Transfer.SpeedUpload),
			}
		}
	}
	return task
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
