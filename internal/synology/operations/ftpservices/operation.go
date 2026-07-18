// Package ftpservices implements the DSM File Services FTP operations. Plain FTP
// and FTPS share SYNO.Core.FileServ.FTP get/set; SFTP uses
// SYNO.Core.FileServ.FTP.SFTP get/set. Each API is an independent compatibility
// boundary behind this package.
package ftpservices

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	FTPAPIName  = "SYNO.Core.FileServ.FTP"
	SFTPAPIName = "SYNO.Core.FileServ.FTP.SFTP"

	FTPReadCapabilityName  = "controlpanel.fileservices.ftp.read"
	FTPSetCapabilityName   = "controlpanel.fileservices.ftp.set"
	SFTPReadCapabilityName = "controlpanel.fileservices.sftp.read"
	SFTPSetCapabilityName  = "controlpanel.fileservices.sftp.set"
)

type Input struct{}

// FTPSettings is the plain-FTP/FTPS switch pair carried by the FTP API.
type FTPSettings struct {
	Plain bool
	FTPS  bool
}

// SFTPSettings is the SFTP switch and listening port.
type SFTPSettings struct {
	Enabled bool
	Port    int
}

// MutationResult records the selected backend for one set.
type MutationResult struct {
	Area    string `json:"area" jsonschema:"Changed FTP area: ftp or sftp"`
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

const (
	areaFTP  = "ftp"
	areaSFTP = "sftp"
)

var ftpReadOperation = compatibility.Operation[Input, FTPSettings]{
	Name: "controlpanel.fileservices.ftp.read",
	Variants: []compatibility.Variant[Input, FTPSettings]{
		{
			Name: "core-fileserv-ftp-v3", API: FTPAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(FTPAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (FTPSettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: FTPAPIName, Version: 3, Method: "get"})
				if err != nil {
					return FTPSettings{}, fmt.Errorf("call %s.get v3: %w", FTPAPIName, err)
				}
				return decodeFTP(data)
			},
		},
	},
}

var ftpSetOperation = compatibility.Operation[FTPSettings, MutationResult]{
	Name: "controlpanel.fileservices.ftp.set",
	Variants: []compatibility.Variant[FTPSettings, MutationResult]{
		{
			Name: "core-fileserv-ftp-v3", API: FTPAPIName, Version: 3, Priority: 10,
			Match: compatibility.APIVersion(FTPAPIName, 3),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired FTPSettings) (MutationResult, error) {
				// DSM requires both switches on every FTP set, so the facade
				// always supplies a fully merged pair.
				parameters := map[string]any{
					"enable_ftp":  desired.Plain,
					"enable_ftps": desired.FTPS,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: FTPAPIName, Version: 3, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v3: %w", FTPAPIName, err)
				}
				return MutationResult{Area: areaFTP}, nil
			},
		},
	},
}

var sftpReadOperation = compatibility.Operation[Input, SFTPSettings]{
	Name: "controlpanel.fileservices.sftp.read",
	Variants: []compatibility.Variant[Input, SFTPSettings]{
		{
			Name: "core-fileserv-sftp-v1", API: SFTPAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(SFTPAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (SFTPSettings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: SFTPAPIName, Version: 1, Method: "get"})
				if err != nil {
					return SFTPSettings{}, fmt.Errorf("call %s.get v1: %w", SFTPAPIName, err)
				}
				return decodeSFTP(data)
			},
		},
	},
}

var sftpSetOperation = compatibility.Operation[SFTPSettings, MutationResult]{
	Name: "controlpanel.fileservices.sftp.set",
	Variants: []compatibility.Variant[SFTPSettings, MutationResult]{
		{
			Name: "core-fileserv-sftp-v1", API: SFTPAPIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(SFTPAPIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired SFTPSettings) (MutationResult, error) {
				// DSM requires the enable switch on every SFTP set; the port is
				// optional but always sent to preserve it across the write.
				parameters := map[string]any{
					"enable":  desired.Enabled,
					"portnum": desired.Port,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: SFTPAPIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", SFTPAPIName, err)
				}
				return MutationResult{Area: areaSFTP}, nil
			},
		},
	},
}

func APINames() []string {
	return []string{FTPAPIName, SFTPAPIName}
}

func SelectFTPRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ftpReadOperation.Select(target)
	return selection, err
}

func SelectFTPSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ftpSetOperation.Select(target)
	return selection, err
}

func SelectSFTPRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := sftpReadOperation.Select(target)
	return selection, err
}

func SelectSFTPSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := sftpSetOperation.Select(target)
	return selection, err
}

func ExecuteFTPRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (FTPSettings, compatibility.Selection, error) {
	return ftpReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteFTPSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired FTPSettings) (MutationResult, compatibility.Selection, error) {
	result, selection, err := ftpSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteSFTPRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (SFTPSettings, compatibility.Selection, error) {
	return sftpReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSFTPSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired SFTPSettings) (MutationResult, compatibility.Selection, error) {
	result, selection, err := sftpSetOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}
