// Package rsyncservice implements the DSM rsync network-backup service
// operations behind SYNO.Backup.Service.NetworkBackup v1. Both get and set name
// the service switch "enable" (confirmed live on DSM 7.3.2; the older source doc
// comment "enable_rsync" is stale), the account switch "enable_rsync_account",
// and the rsync-over-SSH port "rsync_sshd_port" (a string, shared with SSH).
package rsyncservice

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName = "SYNO.Backup.Service.NetworkBackup"

	ReadCapabilityName = "controlpanel.fileservices.rsync.read"
	SetCapabilityName  = "controlpanel.fileservices.rsync.set"
)

type Input struct{}

// Settings is the observed/desired rsync service configuration. SSHPort is
// read-only (shared with the SSH daemon) and never sent on set.
type Settings struct {
	Enabled      bool
	RsyncAccount bool
	SSHPort      int
}

// MutationResult records the selected backend for one set.
type MutationResult struct {
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var readOperation = compatibility.Operation[Input, Settings]{
	Name: "controlpanel.fileservices.rsync.read",
	Variants: []compatibility.Variant[Input, Settings]{
		{
			Name: "backup-service-networkbackup-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (Settings, error) {
				data, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "get"})
				if err != nil {
					return Settings{}, fmt.Errorf("call %s.get v1: %w", APIName, err)
				}
				return decodeSettings(data)
			},
		},
	},
}

var setOperation = compatibility.Operation[Settings, MutationResult]{
	Name: "controlpanel.fileservices.rsync.set",
	Variants: []compatibility.Variant[Settings, MutationResult]{
		{
			Name: "backup-service-networkbackup-v1", API: APIName, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, desired Settings) (MutationResult, error) {
				// Both get and set use "enable" for the service switch; the account
				// switch is a no-op when unchanged.
				parameters := map[string]any{
					"enable":               desired.Enabled,
					"enable_rsync_account": desired.RsyncAccount,
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "set", JSONParameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.set v1: %w", APIName, err)
				}
				return MutationResult{}, nil
			},
		},
	},
}

func APINames() []string { return []string{APIName} }

func SelectRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := readOperation.Select(target)
	return selection, err
}

func SelectSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := setOperation.Select(target)
	return selection, err
}

func ExecuteRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (Settings, compatibility.Selection, error) {
	return readOperation.Run(ctx, target, executor, Input{})
}

func ExecuteSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, desired Settings) (MutationResult, compatibility.Selection, error) {
	result, selection, err := setOperation.Run(ctx, target, executor, desired)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "set"
	}
	return result, selection, err
}
