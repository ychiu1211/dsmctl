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
			Destination *string   `json:"destination"`
			URI         *string   `json:"uri"`
			CreateTime  flexInt64 `json:"create_time"`
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
			task.URI = strings.TrimSpace(deref(e.Additional.Detail.URI))
			task.CreateTime = int64(e.Additional.Detail.CreateTime)
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

// decodeTaskControlResult parses the [{id, error}] array returned by
// pause/resume/delete. It collects the ids DSM accepted (error 0) and fails if
// any id reported a non-zero error, so a partial failure is never silent.
func decodeTaskControlResult(data json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, errors.New("decode task control result: expected an array")
	}
	var entries []struct {
		ID    string `json:"id"`
		Error int    `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return nil, fmt.Errorf("decode task control result: %w", err)
	}
	affected := make([]string, 0, len(entries))
	var failed []string
	for _, entry := range entries {
		if entry.Error != 0 {
			failed = append(failed, fmt.Sprintf("%s (error %d)", entry.ID, entry.Error))
			continue
		}
		affected = append(affected, entry.ID)
	}
	if len(failed) > 0 {
		return affected, fmt.Errorf("DSM reported task action failures: %s", strings.Join(failed, ", "))
	}
	return affected, nil
}

func decodeGlobalSettings(data json.RawMessage) (downloadstation.GlobalSettings, error) {
	var resp struct {
		DownloadVolume     *string `json:"download_volume"`
		EnableEmule        *bool   `json:"enable_emule"`
		EnableUnzipService *bool   `json:"enable_unzip_service"`
	}
	if err := unmarshalObject(data, "Download Station global settings", &resp); err != nil {
		return downloadstation.GlobalSettings{}, err
	}
	if resp.EnableEmule == nil {
		return downloadstation.GlobalSettings{}, errors.New("decode Download Station global settings: required field \"enable_emule\" is missing")
	}
	return downloadstation.GlobalSettings{
		DownloadVolume:      strings.TrimSpace(deref(resp.DownloadVolume)),
		EmuleEnabled:        *resp.EnableEmule,
		UnzipServiceEnabled: resp.EnableUnzipService != nil && *resp.EnableUnzipService,
	}, nil
}

func decodeBTSettings(data json.RawMessage) (downloadstation.BTSettings, error) {
	var resp struct {
		TCPPort                 *int    `json:"tcp_port"`
		DHTPort                 int     `json:"dht_port"`
		EnableDHT               bool    `json:"enable_dht"`
		EnablePortForwarding    bool    `json:"enable_port_forwarding"`
		EnablePreview           bool    `json:"enable_preview"`
		Encrypt                 *string `json:"encrypt"`
		MaxDownloadRate         int     `json:"max_download_rate"`
		MaxUploadRate           int     `json:"max_upload_rate"`
		MaxPeer                 int     `json:"max_peer"`
		SeedingRatio            int     `json:"seeding_ratio"`
		SeedingInterval         int     `json:"seeding_interval"`
		EnableSeedingAutoRemove bool    `json:"enable_seeding_auto_remove"`
	}
	if err := unmarshalObject(data, "Download Station BT settings", &resp); err != nil {
		return downloadstation.BTSettings{}, err
	}
	if resp.TCPPort == nil {
		return downloadstation.BTSettings{}, errors.New("decode Download Station BT settings: required field \"tcp_port\" is missing")
	}
	return downloadstation.BTSettings{
		TCPPort:                 *resp.TCPPort,
		DHTPort:                 resp.DHTPort,
		EnableDHT:               resp.EnableDHT,
		EnablePortForwarding:    resp.EnablePortForwarding,
		EnablePreview:           resp.EnablePreview,
		Encryption:              strings.TrimSpace(deref(resp.Encrypt)),
		MaxDownloadRate:         resp.MaxDownloadRate,
		MaxUploadRate:           resp.MaxUploadRate,
		MaxPeer:                 resp.MaxPeer,
		SeedingRatio:            resp.SeedingRatio,
		SeedingInterval:         resp.SeedingInterval,
		EnableSeedingAutoRemove: resp.EnableSeedingAutoRemove,
	}, nil
}

func decodeEmuleSettings(data json.RawMessage) (bool, error) {
	var resp struct {
		EnableEmule *bool `json:"enable_emule"`
	}
	if err := unmarshalObject(data, "Download Station eMule settings", &resp); err != nil {
		return false, err
	}
	if resp.EnableEmule == nil {
		return false, errors.New("decode Download Station eMule settings: required field \"enable_emule\" is missing")
	}
	return *resp.EnableEmule, nil
}

func decodeDefaultDestination(data json.RawMessage, what string) (string, error) {
	var resp struct {
		DefaultDestination *string `json:"default_destination"`
	}
	if err := unmarshalObject(data, what, &resp); err != nil {
		return "", err
	}
	return strings.TrimSpace(deref(resp.DefaultDestination)), nil
}

func decodeFtpHttpSettings(data json.RawMessage) (downloadstation.FtpHttpSettings, error) {
	var resp struct {
		EnableMaxConn   *bool `json:"enable_ftp_max_conn"`
		MaxDownloadRate int   `json:"ftp_http_max_download_rate"`
		MaxConn         int   `json:"ftp_max_conn"`
	}
	if err := unmarshalObject(data, "Download Station FTP/HTTP settings", &resp); err != nil {
		return downloadstation.FtpHttpSettings{}, err
	}
	if resp.EnableMaxConn == nil {
		return downloadstation.FtpHttpSettings{}, errors.New("decode Download Station FTP/HTTP settings: required field \"enable_ftp_max_conn\" is missing")
	}
	return downloadstation.FtpHttpSettings{
		MaxDownloadRate: resp.MaxDownloadRate,
		EnableMaxConn:   *resp.EnableMaxConn,
		MaxConn:         resp.MaxConn,
	}, nil
}

func decodeNzbSettings(data json.RawMessage) (downloadstation.NzbSettings, error) {
	var resp struct {
		Server               *string `json:"server"`
		Port                 *int    `json:"port"`
		Username             *string `json:"username"`
		EnableAuth           bool    `json:"enable_auth"`
		EnableEncryption     bool    `json:"enable_encryption"`
		EnableParchive       bool    `json:"enable_parchive"`
		EnableRemoveParfiles bool    `json:"enable_remove_parfiles"`
		ConnPerDownload      int     `json:"conn_per_download"`
		MaxDownloadRate      int     `json:"max_download_rate"`
	}
	if err := unmarshalObject(data, "Download Station NZB settings", &resp); err != nil {
		return downloadstation.NzbSettings{}, err
	}
	if resp.Port == nil {
		return downloadstation.NzbSettings{}, errors.New("decode Download Station NZB settings: required field \"port\" is missing")
	}
	return downloadstation.NzbSettings{
		Server:               strings.TrimSpace(deref(resp.Server)),
		Port:                 *resp.Port,
		Username:             strings.TrimSpace(deref(resp.Username)),
		EnableAuth:           resp.EnableAuth,
		EnableEncryption:     resp.EnableEncryption,
		EnableParchive:       resp.EnableParchive,
		EnableRemoveParfiles: resp.EnableRemoveParfiles,
		ConnPerDownload:      resp.ConnPerDownload,
		MaxDownloadRate:      resp.MaxDownloadRate,
	}, nil
}

func decodeAutoExtractionSettings(data json.RawMessage) (downloadstation.AutoExtractionSettings, error) {
	var resp struct {
		EnableUnzip        *bool    `json:"enable_unzip"`
		EnableUnzipService bool     `json:"enable_unzip_service"`
		CreateSubfolder    bool     `json:"create_subfolder"`
		DeleteArchive      bool     `json:"delete_archive"`
		UnzipOverwrite     bool     `json:"unzip_overwrite"`
		UnzipLocation      *string  `json:"unzip_location"`
		UnzipToPath        *string  `json:"unzip_to_path"`
		Passwords          []string `json:"passwords"`
	}
	if err := unmarshalObject(data, "Download Station auto-extraction settings", &resp); err != nil {
		return downloadstation.AutoExtractionSettings{}, err
	}
	if resp.EnableUnzip == nil {
		return downloadstation.AutoExtractionSettings{}, errors.New("decode Download Station auto-extraction settings: required field \"enable_unzip\" is missing")
	}
	return downloadstation.AutoExtractionSettings{
		EnableUnzip:        *resp.EnableUnzip,
		EnableUnzipService: resp.EnableUnzipService,
		CreateSubfolder:    resp.CreateSubfolder,
		DeleteArchive:      resp.DeleteArchive,
		UnzipOverwrite:     resp.UnzipOverwrite,
		UnzipLocation:      strings.TrimSpace(deref(resp.UnzipLocation)),
		UnzipToPath:        strings.TrimSpace(deref(resp.UnzipToPath)),
		PasswordConfigured: len(resp.Passwords) > 0,
	}, nil
}

func decodeLocationSettings(data json.RawMessage) (downloadstation.LocationSettings, error) {
	var resp struct {
		DefaultDestination          *string `json:"default_destination"`
		EnableTorrentNzbWatch       *bool   `json:"enable_torrent_nzb_watch"`
		EnableDeleteTorrentNzbWatch bool    `json:"enable_delete_torrent_nzb_watch"`
		TorrentNzbWatchFolder       *string `json:"torrent_nzb_watch_folder"`
	}
	if err := unmarshalObject(data, "Download Station location settings", &resp); err != nil {
		return downloadstation.LocationSettings{}, err
	}
	if resp.EnableTorrentNzbWatch == nil {
		return downloadstation.LocationSettings{}, errors.New("decode Download Station location settings: required field \"enable_torrent_nzb_watch\" is missing")
	}
	return downloadstation.LocationSettings{
		DefaultDestination:          normalizeNullSentinel(deref(resp.DefaultDestination)),
		EnableTorrentNzbWatch:       *resp.EnableTorrentNzbWatch,
		EnableDeleteTorrentNzbWatch: resp.EnableDeleteTorrentNzbWatch,
		TorrentNzbWatchFolder:       normalizeNullSentinel(deref(resp.TorrentNzbWatchFolder)),
	}, nil
}

// normalizeNullSentinel maps DSM's "(null)" placeholder for an unset path to an
// empty string. Download Station returns "(null)" for an unconfigured watch
// folder or destination; echoing that literal back on a set fails path
// validation (code 522), so the model must never carry the sentinel.
func normalizeNullSentinel(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "(null)" {
		return ""
	}
	return trimmed
}

func decodeRssSettings(data json.RawMessage) (downloadstation.RssSettings, error) {
	var resp struct {
		UpdateInterval *int `json:"update_interval"`
	}
	if err := unmarshalObject(data, "Download Station RSS settings", &resp); err != nil {
		return downloadstation.RssSettings{}, err
	}
	if resp.UpdateInterval == nil {
		return downloadstation.RssSettings{}, errors.New("decode Download Station RSS settings: required field \"update_interval\" is missing")
	}
	return downloadstation.RssSettings{UpdateIntervalMinutes: *resp.UpdateInterval}, nil
}

func decodeSchedulerSettings(data json.RawMessage) (downloadstation.SchedulerSettings, error) {
	var resp struct {
		EnableSchedule *bool   `json:"enable_schedule"`
		DownloadRate   int     `json:"download_rate"`
		UploadRate     int     `json:"upload_rate"`
		MaxTasks       int     `json:"max_tasks"`
		MaxTasksLimit  int     `json:"max_tasks_limit"`
		Order          *string `json:"order"`
		Schedule       *string `json:"schedule"`
	}
	if err := unmarshalObject(data, "Download Station scheduler settings", &resp); err != nil {
		return downloadstation.SchedulerSettings{}, err
	}
	if resp.EnableSchedule == nil {
		return downloadstation.SchedulerSettings{}, errors.New("decode Download Station scheduler settings: required field \"enable_schedule\" is missing")
	}
	return downloadstation.SchedulerSettings{
		EnableSchedule: *resp.EnableSchedule,
		DownloadRate:   resp.DownloadRate,
		UploadRate:     resp.UploadRate,
		MaxTasks:       resp.MaxTasks,
		MaxTasksLimit:  resp.MaxTasksLimit,
		Order:          strings.TrimSpace(deref(resp.Order)),
		ScheduleBitmap: strings.TrimSpace(deref(resp.Schedule)),
	}, nil
}
