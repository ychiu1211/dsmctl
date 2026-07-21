package snapshotreplication

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/snapshotreplication"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

// DR pairing/create APIs. All are package-gated on SnapshotReplication and
// issued on the SOURCE client; the destination is described in the parameters.
const (
	NodeCredentialAPIName = "SYNO.DR.Node.Credential"

	ReplicationPairCapabilityName   = "snapshot.replication.pair"
	ReplicationCreateCapabilityName = "snapshot.replication.create"
	ReplicationManageCapabilityName = "snapshot.replication.manage"
)

// Wire constants, live-verified against the DSM 7.4.7 UI bundle.
const (
	targetTypeShare  = 2
	replicaTypeBtrfs = 2
	replicaPortBtrfs = 5566
	solutionSynology = 1
	autoFill         = "_AUTO_FILL_"
)

// PairEndpoint is the destination NAS management endpoint used inside the DR
// connection descriptors.
type PairEndpoint struct {
	Addr  string
	Port  int
	HTTPS bool
}

func (e PairEndpoint) protocol() string {
	if e.HTTPS {
		return "https"
	}
	return "http"
}

func (e PairEndpoint) conn() map[string]any {
	return map[string]any{"addr": e.Addr, "port": e.Port, "protocol": e.protocol()}
}

// srcToDstConn builds the one connection descriptor for a single-share
// source→destination btrfs relation, referencing the paired credential.
func srcToDstConn(endpoint PairEndpoint, credID string) map[string]any {
	return map[string]any{
		"cred": map[string]any{
			"conn":    endpoint.conn(),
			"auth":    "cred_id",
			"cred_id": credID,
		},
		"replica_conn": map[string]any{
			"replica_addr": autoFill,
			"replica_port": replicaPortBtrfs,
			"replica_type": replicaTypeBtrfs,
		},
	}
}

// CreateResult is the async task reference returned by a relation create.
type CreateResult struct {
	TaskID string
}

// --- temp_create (pairing) ---

type pairInput struct {
	Endpoint PairEndpoint
	SID      string
}

var replicationPairOperation = compatibility.Operation[pairInput, string]{
	Name: ReplicationPairCapabilityName,
	Variants: []compatibility.Variant[pairInput, string]{
		{
			Name: "dr-node-credential-temp-create-v1", API: NodeCredentialAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(NodeCredentialAPIName, 1), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input pairInput) (string, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: NodeCredentialAPIName, Version: 1, Method: "temp_create",
					JSONParameters: map[string]any{
						"conn":    input.Endpoint.conn(),
						"auth":    "session",
						"session": input.SID,
					},
				})
				if err != nil {
					return "", fmt.Errorf("call %s.temp_create: %w", NodeCredentialAPIName, err)
				}
				return decodeCredID(data)
			},
		},
	},
}

func SelectReplicationPair(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := replicationPairOperation.Select(target)
	return selection, err
}

func ExecuteReplicationTempCredential(ctx context.Context, target compatibility.Target, executor compatibility.Executor, endpoint PairEndpoint, sid string) (string, compatibility.Selection, error) {
	return replicationPairOperation.Run(ctx, target, executor, pairInput{Endpoint: endpoint, SID: sid})
}

// --- check_remote_conn ---

type checkInput struct {
	Endpoint PairEndpoint
	CredID   string
}

var replicationCheckOperation = compatibility.Operation[checkInput, bool]{
	Name: ReplicationCreateCapabilityName,
	Variants: []compatibility.Variant[checkInput, bool]{
		{
			Name: "dr-plan-check-remote-conn-v1", API: PlanAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(PlanAPIName, 1), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input checkInput) (bool, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{
					API: PlanAPIName, Version: 1, Method: "check_remote_conn",
					JSONParameters: map[string]any{
						"src_to_dst_conns": []any{srcToDstConn(input.Endpoint, input.CredID)},
					},
				}); err != nil {
					return false, fmt.Errorf("call %s.check_remote_conn: %w", PlanAPIName, err)
				}
				return true, nil
			},
		},
	},
}

func ExecuteReplicationCheckRemoteConn(ctx context.Context, target compatibility.Target, executor compatibility.Executor, endpoint PairEndpoint, credID string) (bool, error) {
	ok, _, err := replicationCheckOperation.Run(ctx, target, executor, checkInput{Endpoint: endpoint, CredID: credID})
	return ok, err
}

// --- create v3 ---

type createInput struct {
	Request  snapshotreplication.RelationCreate
	Endpoint PairEndpoint
	CredID   string
}

var replicationCreateOperation = compatibility.Operation[createInput, CreateResult]{
	Name: ReplicationCreateCapabilityName,
	Variants: []compatibility.Variant[createInput, CreateResult]{
		{
			Name: "dr-plan-create-v3", API: PlanAPIName, Version: 3, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(PlanAPIName, 3), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input createInput) (CreateResult, error) {
				sendEncrypted := input.Endpoint.HTTPS
				if input.Request.SendEncrypted != nil {
					sendEncrypted = *input.Request.SendEncrypted
				}
				params := map[string]any{
					"nowait":        true,
					"auto_remove":   false,
					"is_to_local":   false,
					"solution_type": solutionSynology,
					"dst_volume":    input.Request.DestVolume,
					"target": map[string]any{
						"target_id":   input.Request.SourceShare,
						"target_type": targetTypeShare,
					},
					"src_to_dst_conns": []any{srcToDstConn(input.Endpoint, input.CredID)},
					"sync_policy": map[string]any{
						"enabled": false,
						"mode":    2,
						"schedule": map[string]any{
							"date_type":      0,
							"week_name":      "1,1,1,1,1,1,1",
							"hour":           0,
							"min":            0,
							"last_work_hour": 23,
							"repeat_hour":    0,
							"repeat_min":     0,
						},
						"notify_time_in_min":      0,
						"worm_lock_enable":        false,
						"worm_lock_day":           7,
						"sync_window":             map[string]any{"enabled": false},
						"is_send_encrypted":       sendEncrypted,
						"is_sync_local_snapshots": false,
					},
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API: PlanAPIName, Version: 3, Method: "create", JSONParameters: params,
				})
				if err != nil {
					return CreateResult{}, fmt.Errorf("call %s.create v3: %w", PlanAPIName, err)
				}
				taskID, err := decodeTaskID(data)
				if err != nil {
					return CreateResult{}, err
				}
				return CreateResult{TaskID: taskID}, nil
			},
		},
	},
}

func SelectReplicationCreate(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := replicationCreateOperation.Select(target)
	return selection, err
}

func ExecuteReplicationCreate(ctx context.Context, target compatibility.Target, executor compatibility.Executor, request snapshotreplication.RelationCreate, endpoint PairEndpoint, credID string) (string, compatibility.Selection, error) {
	result, selection, err := replicationCreateOperation.Run(ctx, target, executor, createInput{Request: request, Endpoint: endpoint, CredID: credID})
	return result.TaskID, selection, err
}

// --- get_poll_task ---

var replicationPollOperation = compatibility.Operation[string, snapshotreplication.RelationTaskStatus]{
	Name: ReplicationCreateCapabilityName,
	Variants: []compatibility.Variant[string, snapshotreplication.RelationTaskStatus]{
		{
			Name: "dr-plan-get-poll-task-v1", API: PlanAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(PlanAPIName, 1), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, taskID string) (snapshotreplication.RelationTaskStatus, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: PlanAPIName, Version: 1, Method: "get_poll_task",
					JSONParameters: map[string]any{"task_id": taskID}, ReadOnly: true,
				})
				if err != nil {
					return snapshotreplication.RelationTaskStatus{}, fmt.Errorf("call %s.get_poll_task: %w", PlanAPIName, err)
				}
				return decodePollTask(data)
			},
		},
	},
}

func ExecuteReplicationPollTask(ctx context.Context, target compatibility.Target, executor compatibility.Executor, taskID string) (snapshotreplication.RelationTaskStatus, compatibility.Selection, error) {
	return replicationPollOperation.Run(ctx, target, executor, taskID)
}

// --- delete plan (teardown) ---

var replicationDeleteOperation = compatibility.Operation[string, bool]{
	Name: ReplicationCreateCapabilityName,
	Variants: []compatibility.Variant[string, bool]{
		{
			Name: "dr-plan-delete-v1", API: PlanAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(PlanAPIName, 1), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, planID string) (bool, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{
					API: PlanAPIName, Version: 1, Method: "delete",
					JSONParameters: map[string]any{
						"plan_id":                planID,
						"is_data_deleted":        false,
						"is_remote_site_deleted": true,
						"deleted_by":             "user",
						"nowait":                 true,
						"auto_remove":            true,
					},
				}); err != nil {
					return false, fmt.Errorf("call %s.delete: %w", PlanAPIName, err)
				}
				return true, nil
			},
		},
	},
}

func SelectReplicationDelete(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := replicationDeleteOperation.Select(target)
	return selection, err
}

func ExecuteReplicationDelete(ctx context.Context, target compatibility.Target, executor compatibility.Executor, planID string) (bool, error) {
	ok, _, err := replicationDeleteOperation.Run(ctx, target, executor, planID)
	return ok, err
}

// --- delete temp credential (cleanup after a failed create) ---

var replicationDeleteCredOperation = compatibility.Operation[string, bool]{
	Name: ReplicationPairCapabilityName,
	Variants: []compatibility.Variant[string, bool]{
		{
			Name: "dr-node-credential-delete-v1", API: NodeCredentialAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(NodeCredentialAPIName, 1), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, credID string) (bool, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{
					API: NodeCredentialAPIName, Version: 1, Method: "delete",
					JSONParameters: map[string]any{"cred_id": credID},
				}); err != nil {
					return false, fmt.Errorf("call %s.delete: %w", NodeCredentialAPIName, err)
				}
				return true, nil
			},
		},
	},
}

func ExecuteReplicationDeleteCredential(ctx context.Context, target compatibility.Target, executor compatibility.Executor, credID string) (bool, error) {
	ok, _, err := replicationDeleteCredOperation.Run(ctx, target, executor, credID)
	return ok, err
}

// --- sync now (management of an existing relation, by plan id) ---

// SyncInput triggers a manual replication sync of an existing relation.
type SyncInput struct {
	PlanID         string
	SnapshotLocked bool
	SendEncrypted  bool
	Description    string
}

var replicationSyncOperation = compatibility.Operation[SyncInput, bool]{
	Name: ReplicationManageCapabilityName,
	Variants: []compatibility.Variant[SyncInput, bool]{
		{
			Name: "dr-plan-sync-v1", API: PlanAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(PlanAPIName, 1), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, input SyncInput) (bool, error) {
				params := map[string]any{
					"plan_id":            input.PlanID,
					"nowait":             true,
					"auto_remove":        true,
					"is_snapshot_locked": input.SnapshotLocked,
					"is_send_encrypted":  input.SendEncrypted,
				}
				if input.Description != "" {
					params["sync_description"] = input.Description
				}
				if _, err := executor.Execute(ctx, compatibility.Request{
					API: PlanAPIName, Version: 1, Method: "sync", JSONParameters: params,
				}); err != nil {
					return false, fmt.Errorf("call %s.sync: %w", PlanAPIName, err)
				}
				return true, nil
			},
		},
	},
}

func SelectReplicationManage(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := replicationSyncOperation.Select(target)
	return selection, err
}

func ExecuteReplicationSync(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input SyncInput) (bool, error) {
	ok, _, err := replicationSyncOperation.Run(ctx, target, executor, input)
	return ok, err
}

// --- stop / pause replication of an existing relation ---

var replicationPauseOperation = compatibility.Operation[string, bool]{
	Name: ReplicationManageCapabilityName,
	Variants: []compatibility.Variant[string, bool]{
		{
			Name: "dr-plan-pause-v1", API: PlanAPIName, Version: 1, Priority: 10,
			Match: compatibility.All(compatibility.APIVersion(PlanAPIName, 1), replicationPackage),
			Execute: func(ctx context.Context, executor compatibility.Executor, planID string) (bool, error) {
				if _, err := executor.Execute(ctx, compatibility.Request{
					API: PlanAPIName, Version: 1, Method: "pause",
					JSONParameters: map[string]any{"plan_id": planID, "nowait": true},
				}); err != nil {
					return false, fmt.Errorf("call %s.pause: %w", PlanAPIName, err)
				}
				return true, nil
			},
		},
	},
}

func ExecuteReplicationPause(ctx context.Context, target compatibility.Target, executor compatibility.Executor, planID string) (bool, error) {
	ok, _, err := replicationPauseOperation.Run(ctx, target, executor, planID)
	return ok, err
}
