// Package fileservices implements independently selectable SMB and NFS
// Control Panel operations. DSM API names and versions stay behind this
// package so the shared domain, CLI, and MCP contracts remain stable.
package fileservices

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	SMBAPIName         = "SYNO.Core.FileServ.SMB"
	NFSAPIName         = "SYNO.Core.FileServ.NFS"
	NFSAdvancedAPIName = "SYNO.Core.FileServ.NFS.AdvancedSetting"

	SMBReadCapabilityName         = "controlpanel.fileservices.smb.read"
	SMBSetCapabilityName          = "controlpanel.fileservices.smb.set"
	NFSReadCapabilityName         = "controlpanel.fileservices.nfs.read"
	NFSSetCapabilityName          = "controlpanel.fileservices.nfs.set"
	NFSAdvancedReadCapabilityName = "controlpanel.fileservices.nfs.advanced.read"
	NFSAdvancedSetCapabilityName  = "controlpanel.fileservices.nfs.advanced.set"
)

type Input struct{}

type NFSAdvancedState struct {
	Domain string
}

type MutationResult struct {
	Protocol controlpanel.FileProtocol `json:"protocol" jsonschema:"Changed file service"`
	Backend  string                    `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API      string                    `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version  int                       `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method   string                    `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var smbReadOperation = compatibility.Operation[Input, controlpanel.SMBState]{
	Name: "controlpanel.fileservices.smb.read",
	Variants: []compatibility.Variant[Input, controlpanel.SMBState]{
		smbReadVariant("core-fileserv-smb-v3", 3, 30, true),
		smbReadVariant("core-fileserv-smb-v2-legacy", 2, 20, false),
		smbReadVariant("core-fileserv-smb-v1-legacy", 1, 10, false),
	},
}

var smbSetOperation = compatibility.Operation[controlpanel.SMBChange, MutationResult]{
	Name: "controlpanel.fileservices.smb.set",
	Variants: []compatibility.Variant[controlpanel.SMBChange, MutationResult]{
		smbSetVariant("core-fileserv-smb-v3", 3, 30),
	},
}

var nfsReadOperation = compatibility.Operation[Input, controlpanel.NFSState]{
	Name: "controlpanel.fileservices.nfs.read",
	Variants: []compatibility.Variant[Input, controlpanel.NFSState]{
		nfsReadVariant("core-fileserv-nfs-v3", 3, 30, true),
		nfsReadVariant("core-fileserv-nfs-v2", 2, 20, true),
		nfsReadVariant("core-fileserv-nfs-v1-legacy", 1, 10, false),
	},
}

var nfsSetOperation = compatibility.Operation[controlpanel.NFSChange, MutationResult]{
	Name: "controlpanel.fileservices.nfs.set",
	Variants: []compatibility.Variant[controlpanel.NFSChange, MutationResult]{
		nfsSetVariant("core-fileserv-nfs-v3", 3, 30),
		nfsSetVariant("core-fileserv-nfs-v2", 2, 20),
	},
}

var nfsAdvancedReadOperation = compatibility.Operation[Input, NFSAdvancedState]{
	Name: "controlpanel.fileservices.nfs.advanced.read",
	Variants: []compatibility.Variant[Input, NFSAdvancedState]{
		nfsAdvancedReadVariant(),
	},
}

var nfsAdvancedSetOperation = compatibility.Operation[controlpanel.NFSChange, MutationResult]{
	Name: "controlpanel.fileservices.nfs.advanced.set",
}

func APINames() []string {
	return []string{NFSAdvancedAPIName, NFSAPIName, SMBAPIName}
}

func SelectSMBRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := smbReadOperation.Select(target)
	return selection, err
}

func SelectSMBSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := smbSetOperation.Select(target)
	return selection, err
}

func SelectNFSRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := nfsReadOperation.Select(target)
	return selection, err
}

func SelectNFSSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := nfsSetOperation.Select(target)
	return selection, err
}

func SelectNFSAdvancedRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := nfsAdvancedReadOperation.Select(target)
	return selection, err
}

func SelectNFSAdvancedSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := nfsAdvancedSetOperation.Select(target)
	return selection, err
}

func ExecuteSMBRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (controlpanel.SMBState, compatibility.Selection, error) {
	return smbReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSMBSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change controlpanel.SMBChange) (MutationResult, compatibility.Selection, error) {
	result, selection, err := smbSetOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteNFSRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (controlpanel.NFSState, compatibility.Selection, error) {
	return nfsReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteNFSSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change controlpanel.NFSChange) (MutationResult, compatibility.Selection, error) {
	result, selection, err := nfsSetOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func ExecuteNFSAdvancedRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (NFSAdvancedState, compatibility.Selection, error) {
	return nfsAdvancedReadOperation.Run(ctx, target, executor, Input{})
}

func ExecuteNFSAdvancedSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, change controlpanel.NFSChange) (MutationResult, compatibility.Selection, error) {
	result, selection, err := nfsAdvancedSetOperation.Run(ctx, target, executor, change)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}

func smbReadVariant(name string, version, priority int, modern bool) compatibility.Variant[Input, controlpanel.SMBState] {
	return compatibility.Variant[Input, controlpanel.SMBState]{
		Name: name, API: SMBAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(SMBAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (controlpanel.SMBState, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: SMBAPIName, Version: version, Method: "get"})
			if err != nil {
				return controlpanel.SMBState{}, fmt.Errorf("call %s.get v%d: %w", SMBAPIName, version, err)
			}
			return decodeSMB(data, modern)
		},
	}
}

func smbSetVariant(name string, version, priority int) compatibility.Variant[controlpanel.SMBChange, MutationResult] {
	return compatibility.Variant[controlpanel.SMBChange, MutationResult]{
		Name: name, API: SMBAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(SMBAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, change controlpanel.SMBChange) (MutationResult, error) {
			parameters, err := encodeSMBChange(change)
			if err != nil {
				return MutationResult{}, err
			}
			if _, err := executor.Execute(ctx, compatibility.Request{API: SMBAPIName, Version: version, Method: "set", JSONParameters: parameters}); err != nil {
				return MutationResult{}, fmt.Errorf("call %s.set v%d: %w", SMBAPIName, version, err)
			}
			return MutationResult{Protocol: controlpanel.FileProtocolSMB}, nil
		},
	}
}

func nfsReadVariant(name string, version, priority int, modern bool) compatibility.Variant[Input, controlpanel.NFSState] {
	return compatibility.Variant[Input, controlpanel.NFSState]{
		Name: name, API: NFSAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(NFSAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (controlpanel.NFSState, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: NFSAPIName, Version: version, Method: "get"})
			if err != nil {
				return controlpanel.NFSState{}, fmt.Errorf("call %s.get v%d: %w", NFSAPIName, version, err)
			}
			return decodeNFS(data, modern)
		},
	}
}

func nfsSetVariant(name string, version, priority int) compatibility.Variant[controlpanel.NFSChange, MutationResult] {
	return compatibility.Variant[controlpanel.NFSChange, MutationResult]{
		Name: name, API: NFSAPIName, Version: version, Priority: priority,
		Match: compatibility.APIVersion(NFSAPIName, version),
		Execute: func(ctx context.Context, executor compatibility.Executor, change controlpanel.NFSChange) (MutationResult, error) {
			parameters, err := encodeNFSBaseChange(change)
			if err != nil {
				return MutationResult{}, err
			}
			if _, err := executor.Execute(ctx, compatibility.Request{API: NFSAPIName, Version: version, Method: "set", JSONParameters: parameters}); err != nil {
				return MutationResult{}, fmt.Errorf("call %s.set v%d: %w", NFSAPIName, version, err)
			}
			return MutationResult{Protocol: controlpanel.FileProtocolNFS}, nil
		},
	}
}

func nfsAdvancedReadVariant() compatibility.Variant[Input, NFSAdvancedState] {
	return compatibility.Variant[Input, NFSAdvancedState]{
		Name: "core-fileserv-nfs-advanced-v1", API: NFSAdvancedAPIName, Version: 1, Priority: 10,
		Match: compatibility.APIVersion(NFSAdvancedAPIName, 1),
		Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (NFSAdvancedState, error) {
			data, err := executor.Execute(ctx, compatibility.Request{API: NFSAdvancedAPIName, Version: 1, Method: "get"})
			if err != nil {
				return NFSAdvancedState{}, fmt.Errorf("call %s.get v1: %w", NFSAdvancedAPIName, err)
			}
			domain, err := decodeNFSAdvanced(data)
			return NFSAdvancedState{Domain: domain}, err
		},
	}
}
