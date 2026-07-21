package synology

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ychiu1211/dsmctl/internal/domain/snapshotreplication"
	snapshotops "github.com/ychiu1211/dsmctl/internal/synology/operations/snapshotreplication"
)

// PairingEndpoint is the destination NAS management endpoint plus a live DSM
// session, everything a source NAS needs to establish a Snapshot Replication
// pairing to this destination. Sid/SynoToken are session material minted from
// the destination profile's stored credential — never the account password.
type PairingEndpoint struct {
	Addr      string
	Port      int
	HTTPS     bool
	SID       string
	SynoToken string
}

// ReplicationPairingEndpoint logs this client in if needed and returns its
// management endpoint and live session. It is for the internal apply-time
// cross-NAS pairing flow: dsmctl calls it on the *destination* client (which
// Manager.Client authenticated with the destination profile's vault credential)
// and forwards the resulting session to the source NAS's DR pairing API. The
// account password never leaves the credential resolver; only the resulting
// session id is surfaced, and only to the in-process apply path.
func (c *Client) ReplicationPairingEndpoint(ctx context.Context) (PairingEndpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.loginLocked(ctx); err != nil {
		return PairingEndpoint{}, fmt.Errorf("authenticate destination for pairing: %w", err)
	}
	endpoint := PairingEndpoint{
		Addr:      c.baseURL.Hostname(),
		HTTPS:     c.baseURL.Scheme == "https",
		SID:       c.sid,
		SynoToken: c.synoToken,
	}
	if endpoint.HTTPS {
		endpoint.Port = 5001
	} else {
		endpoint.Port = 5000
	}
	if raw := c.baseURL.Port(); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			endpoint.Port = parsed
		}
	}
	if endpoint.SID == "" {
		return PairingEndpoint{}, fmt.Errorf("destination session is empty after login")
	}
	return endpoint, nil
}

// Re-exported types for the application layer.
type SnapshotReplicationRelationCreate = snapshotreplication.RelationCreate
type SnapshotReplicationPairEndpoint = snapshotops.PairEndpoint
type SnapshotReplicationCreateResult = snapshotops.CreateResult

// PairReplicationCredential establishes a temporary DR credential on this
// (source) client for the given destination endpoint+session, returning the
// cred_id the create call consumes.
func (c *Client) PairReplicationCredential(ctx context.Context, endpoint SnapshotReplicationPairEndpoint, sid string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return "", err
	}
	credID, _, err := snapshotops.ExecuteReplicationTempCredential(ctx, c.target, lockedExecutor{client: c}, endpoint, sid)
	if err != nil {
		return "", fmt.Errorf("pair replication credential: %w", err)
	}
	return credID, nil
}

// CheckReplicationRemoteConn verifies source→destination reachability for the
// given destination endpoint + credential before any relation is created.
func (c *Client) CheckReplicationRemoteConn(ctx context.Context, endpoint SnapshotReplicationPairEndpoint, credID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return err
	}
	if _, err := snapshotops.ExecuteReplicationCheckRemoteConn(ctx, c.target, lockedExecutor{client: c}, endpoint, credID); err != nil {
		return fmt.Errorf("check replication remote connection: %w", err)
	}
	return nil
}

// CreateReplicationPlan creates a share replication relation from this (source)
// client to the destination described by the credential + endpoint, returning
// the async task id.
func (c *Client) CreateReplicationPlan(ctx context.Context, input snapshotreplication.RelationCreate, endpoint SnapshotReplicationPairEndpoint, credID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return "", err
	}
	taskID, _, err := snapshotops.ExecuteReplicationCreate(ctx, c.target, lockedExecutor{client: c}, input, endpoint, credID)
	if err != nil {
		return "", fmt.Errorf("create replication plan: %w", err)
	}
	return taskID, nil
}

// PollReplicationTask reads one poll of an in-flight create task.
func (c *Client) PollReplicationTask(ctx context.Context, taskID string) (snapshotreplication.RelationTaskStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return snapshotreplication.RelationTaskStatus{}, err
	}
	status, _, err := snapshotops.ExecuteReplicationPollTask(ctx, c.target, lockedExecutor{client: c}, taskID)
	if err != nil {
		return snapshotreplication.RelationTaskStatus{}, fmt.Errorf("poll replication task: %w", err)
	}
	return status, nil
}

// DeleteReplicationPlan removes a replication relation by plan id (teardown).
func (c *Client) DeleteReplicationPlan(ctx context.Context, planID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return err
	}
	if _, err := snapshotops.ExecuteReplicationDelete(ctx, c.target, lockedExecutor{client: c}, planID); err != nil {
		return fmt.Errorf("delete replication plan: %w", err)
	}
	return nil
}

// DeleteReplicationCredential removes a temporary DR credential (cleanup after
// a failed create).
func (c *Client) DeleteReplicationCredential(ctx context.Context, credID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return err
	}
	if _, err := snapshotops.ExecuteReplicationDeleteCredential(ctx, c.target, lockedExecutor{client: c}, credID); err != nil {
		return fmt.Errorf("delete replication credential: %w", err)
	}
	return nil
}

// SyncReplicationPlan triggers a manual sync of an existing relation by plan id.
func (c *Client) SyncReplicationPlan(ctx context.Context, planID string, sendEncrypted bool, description string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return err
	}
	if _, err := snapshotops.ExecuteReplicationSync(ctx, c.target, lockedExecutor{client: c}, snapshotops.SyncInput{
		PlanID: planID, SnapshotLocked: false, SendEncrypted: sendEncrypted, Description: description,
	}); err != nil {
		return fmt.Errorf("sync replication plan: %w", err)
	}
	return nil
}

// PauseReplicationPlan stops (pauses) replication for an existing relation.
func (c *Client) PauseReplicationPlan(ctx context.Context, planID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.prepareSnapshotReplicationTargetLocked(ctx); err != nil {
		return err
	}
	if _, err := snapshotops.ExecuteReplicationPause(ctx, c.target, lockedExecutor{client: c}, planID); err != nil {
		return fmt.Errorf("pause replication plan: %w", err)
	}
	return nil
}

