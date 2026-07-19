package filestation

import (
	"context"
	"errors"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// Upload and Download are binary transports (streaming multipart POST and a
// streaming GET), so they cannot run through the JSON Executor. These operations
// exist only to select a backend and report a capability with a stable name,
// API, and version; the façade performs the actual transfer. Their Execute
// therefore never runs — Select is used, Run is not.
const (
	UploadAPIName   = "SYNO.FileStation.Upload"
	DownloadAPIName = "SYNO.FileStation.Download"

	UploadCapabilityName   = "file.upload"
	DownloadCapabilityName = "file.download"
)

var errTransferViaExecutor = errors.New("FileStation upload/download uses binary transport, not the WebAPI executor")

var uploadOperation = compatibility.Operation[struct{}, struct{}]{
	Name: UploadCapabilityName,
	Variants: []compatibility.Variant[struct{}, struct{}]{
		{
			Name: "filestation-upload-v2", API: UploadAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(UploadAPIName, 2),
			Execute: func(context.Context, compatibility.Executor, struct{}) (struct{}, error) {
				return struct{}{}, errTransferViaExecutor
			},
		},
	},
}

var downloadOperation = compatibility.Operation[struct{}, struct{}]{
	Name: DownloadCapabilityName,
	Variants: []compatibility.Variant[struct{}, struct{}]{
		{
			Name: "filestation-download-v2", API: DownloadAPIName, Version: 2, Priority: 10,
			Match: compatibility.APIVersion(DownloadAPIName, 2),
			Execute: func(context.Context, compatibility.Executor, struct{}) (struct{}, error) {
				return struct{}{}, errTransferViaExecutor
			},
		},
	},
}

func SelectUpload(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := uploadOperation.Select(target)
	return selection, err
}

func SelectDownload(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := downloadOperation.Select(target)
	return selection, err
}
