package filestation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	SharingAPIName        = "SYNO.FileStation.Sharing"
	BackgroundTaskAPIName = "SYNO.FileStation.BackgroundTask"

	SharingCapabilityName        = "file.sharing"
	BackgroundTaskCapabilityName = "file.backgroundtask"
)

type SharingCreateInput struct {
	Path       string
	Password   string
	ExpireDate string
}

type SharingDeleteInput struct {
	LinkID string
}

var sharingListOperation = compatibility.Operation[struct{}, filestation.SharingLinks]{
	Name: SharingCapabilityName,
	Variants: []compatibility.Variant[struct{}, filestation.SharingLinks]{
		{
			Name: "filestation-sharing-list-v3", API: SharingAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(SharingAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ struct{}) (filestation.SharingLinks, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SharingAPIName, Version: 3, Method: "list"})
				if err != nil {
					return filestation.SharingLinks{}, fmt.Errorf("call %s.list: %w", SharingAPIName, err)
				}
				return decodeSharingLinks(data)
			},
		},
	},
}

var sharingCreateOperation = compatibility.Operation[SharingCreateInput, MutationResult]{
	Name: SharingCapabilityName,
	Variants: []compatibility.Variant[SharingCreateInput, MutationResult]{
		{
			Name: "filestation-sharing-create-v3", API: SharingAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(SharingAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, input SharingCreateInput) (MutationResult, error) {
				params := url.Values{"path": {input.Path}}
				if input.Password != "" {
					params.Set("password", input.Password)
				}
				if input.ExpireDate != "" {
					params.Set("date_expired", input.ExpireDate)
				}
				data, err := executor.Execute(ctx, compatibility.Request{API: SharingAPIName, Version: 3, Method: "create", Parameters: params})
				if err != nil {
					return MutationResult{}, fmt.Errorf("call %s.create: %w", SharingAPIName, err)
				}
				return decodeSharingCreate(data)
			},
		},
	},
}

var sharingDeleteOperation = compatibility.Operation[SharingDeleteInput, MutationResult]{
	Name: SharingCapabilityName,
	Variants: []compatibility.Variant[SharingDeleteInput, MutationResult]{
		{
			Name: "filestation-sharing-delete-v3", API: SharingAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(SharingAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, input SharingDeleteInput) (MutationResult, error) {
				_, err := executor.Execute(ctx, compatibility.Request{API: SharingAPIName, Version: 3, Method: "delete", Parameters: url.Values{"id": {input.LinkID}}})
				if err != nil {
					return MutationResult{}, fmt.Errorf("call %s.delete: %w", SharingAPIName, err)
				}
				return MutationResult{Paths: []string{input.LinkID}}, nil
			},
		},
	},
}

var backgroundTaskListOperation = compatibility.Operation[struct{}, filestation.BackgroundTasks]{
	Name: BackgroundTaskCapabilityName,
	Variants: []compatibility.Variant[struct{}, filestation.BackgroundTasks]{
		{
			Name: "filestation-backgroundtask-list-v3", API: BackgroundTaskAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(BackgroundTaskAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ struct{}) (filestation.BackgroundTasks, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: BackgroundTaskAPIName, Version: 3, Method: "list"})
				if err != nil {
					return filestation.BackgroundTasks{}, fmt.Errorf("call %s.list: %w", BackgroundTaskAPIName, err)
				}
				return decodeBackgroundTasks(data)
			},
		},
	},
}

func decodeSharingLinks(data json.RawMessage) (filestation.SharingLinks, error) {
	var resp struct {
		Total *int `json:"total"`
		Links []struct {
			ID            *string `json:"id"`
			Name          *string `json:"name"`
			Path          *string `json:"path"`
			URL           *string `json:"url"`
			IsFolder      *bool   `json:"isFolder"`
			HasPassword   *bool   `json:"has_password"`
			Status        *string `json:"status"`
			DateExpired   *string `json:"date_expired"`
			DateAvailable *string `json:"date_available"`
		} `json:"links"`
	}
	if err := unmarshalObject(data, "sharing links", &resp); err != nil {
		return filestation.SharingLinks{}, err
	}
	links := make([]filestation.SharingLink, 0, len(resp.Links))
	for _, link := range resp.Links {
		links = append(links, filestation.SharingLink{
			ID:            deref(link.ID),
			Name:          deref(link.Name),
			Path:          deref(link.Path),
			URL:           deref(link.URL),
			IsFolder:      link.IsFolder != nil && *link.IsFolder,
			HasPassword:   link.HasPassword != nil && *link.HasPassword,
			Status:        deref(link.Status),
			DateExpired:   deref(link.DateExpired),
			DateAvailable: deref(link.DateAvailable),
		})
	}
	total := len(links)
	if resp.Total != nil {
		total = *resp.Total
	}
	return filestation.SharingLinks{Total: total, Links: links}, nil
}

func decodeSharingCreate(data json.RawMessage) (MutationResult, error) {
	var resp struct {
		Links []struct {
			ID  *string `json:"id"`
			URL *string `json:"url"`
		} `json:"links"`
	}
	if err := unmarshalObject(data, "created sharing link", &resp); err != nil {
		return MutationResult{}, err
	}
	if len(resp.Links) == 0 {
		return MutationResult{}, fmt.Errorf("decode created sharing link: DSM returned no link")
	}
	link := resp.Links[0]
	result := MutationResult{URL: deref(link.URL)}
	if id := deref(link.ID); id != "" {
		result.Paths = []string{id}
	}
	return result, nil
}

func decodeBackgroundTasks(data json.RawMessage) (filestation.BackgroundTasks, error) {
	var resp struct {
		Total *int `json:"total"`
		Tasks []struct {
			TaskID         *string  `json:"taskid"`
			API            *string  `json:"api"`
			Finished       *bool    `json:"finished"`
			ProcessingPath *string  `json:"processing_path"`
			Progress       *float64 `json:"progress"`
		} `json:"tasks"`
	}
	if err := unmarshalObject(data, "background tasks", &resp); err != nil {
		return filestation.BackgroundTasks{}, err
	}
	tasks := make([]filestation.BackgroundTask, 0, len(resp.Tasks))
	for _, task := range resp.Tasks {
		entry := filestation.BackgroundTask{
			TaskID:         deref(task.TaskID),
			API:            deref(task.API),
			Finished:       task.Finished != nil && *task.Finished,
			ProcessingPath: deref(task.ProcessingPath),
		}
		if task.Progress != nil {
			entry.Progress = *task.Progress
		}
		tasks = append(tasks, entry)
	}
	total := len(tasks)
	if resp.Total != nil {
		total = *resp.Total
	}
	return filestation.BackgroundTasks{Total: total, Tasks: tasks}, nil
}

func SelectSharing(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := sharingListOperation.Select(target)
	return selection, err
}

func SelectBackgroundTask(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := backgroundTaskListOperation.Select(target)
	return selection, err
}

func ExecuteSharingList(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (filestation.SharingLinks, compatibility.Selection, error) {
	return sharingListOperation.Run(ctx, target, executor, struct{}{})
}

func ExecuteSharingCreate(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input SharingCreateInput) (MutationResult, compatibility.Selection, error) {
	return sharingCreateOperation.Run(ctx, target, executor, input)
}

func ExecuteSharingDelete(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input SharingDeleteInput) (MutationResult, compatibility.Selection, error) {
	return sharingDeleteOperation.Run(ctx, target, executor, input)
}

func ExecuteBackgroundTaskList(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (filestation.BackgroundTasks, compatibility.Selection, error) {
	return backgroundTaskListOperation.Run(ctx, target, executor, struct{}{})
}
