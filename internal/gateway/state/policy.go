package state

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/ychiu1211/dsmctl/internal/remotepolicy"
)

const (
	maxAuditEvents = 10000
	auditRetention = 30 * 24 * time.Hour
	mcpTokenPrefix = "dsmctl_mcp_"
)

var validScopes = map[string]struct{}{
	remotepolicy.ScopeRead: {}, remotepolicy.ScopePlan: {},
	remotepolicy.ScopeApply: {}, remotepolicy.ScopeLANDiscover: {},
}

var errApprovalMatch = errors.New("approval match")

type MCPToken struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Scopes       []string   `json:"scopes"`
	NASAllowlist []string   `json:"nas_allowlist"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
}

type MCPTokenInput struct {
	Name         string     `json:"name"`
	Scopes       []string   `json:"scopes,omitempty"`
	NASAllowlist []string   `json:"nas_allowlist"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type IssuedMCPToken struct {
	Token       MCPToken `json:"token"`
	BearerToken string   `json:"bearer_token"`
}

type mcpTokenRecord struct {
	MCPToken
	Digest []byte `json:"digest"`
}

type Approval struct {
	ID                string     `json:"id"`
	PlanHash          string     `json:"plan_hash"`
	NAS               string     `json:"nas"`
	ProfileRevision   uint64     `json:"profile_revision"`
	RequestingTokenID string     `json:"requesting_token_id"`
	Administrator     string     `json:"administrator"`
	CreatedAt         time.Time  `json:"created_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	ConsumedAt        *time.Time `json:"consumed_at,omitempty"`
}

type ApprovalInput struct {
	PlanHash          string        `json:"plan_hash"`
	NAS               string        `json:"nas"`
	ProfileRevision   uint64        `json:"profile_revision"`
	RequestingTokenID string        `json:"requesting_token_id"`
	TTL               time.Duration `json:"-"`
}

// PendingApproval is advisory administrator UI state derived from the closed,
// non-secret fields of a successful remote high-risk plan result.
type PendingApproval struct {
	ID                string    `json:"id"`
	PlanHash          string    `json:"plan_hash"`
	NAS               string    `json:"nas"`
	ProfileRevision   uint64    `json:"profile_revision"`
	RequestingTokenID string    `json:"requesting_token_id"`
	RequestingToken   string    `json:"requesting_token,omitempty"`
	Tool              string    `json:"tool"`
	Risk              string    `json:"risk"`
	ResourceID        string    `json:"resource_id,omitempty"`
	Summary           string    `json:"summary"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type AuditEvent = remotepolicy.AuditEvent

type AuditQuery struct {
	After   time.Time
	Limit   int
	ActorID string
	Action  string
}

func (r *Repository) CreateMCPToken(ctx context.Context, input MCPTokenInput) (IssuedMCPToken, error) {
	if err := ctx.Err(); err != nil {
		return IssuedMCPToken{}, err
	}
	input, err := normalizeMCPTokenInput(input)
	if err != nil {
		return IssuedMCPToken{}, err
	}
	raw, err := randomToken(32)
	if err != nil {
		return IssuedMCPToken{}, err
	}
	raw = mcpTokenPrefix + raw
	digest := sha256.Sum256([]byte(raw))
	id, err := randomID(16)
	if err != nil {
		return IssuedMCPToken{}, err
	}
	now := time.Now().UTC()
	record := mcpTokenRecord{MCPToken: MCPToken{ID: id, Name: input.Name, Scopes: input.Scopes, NASAllowlist: input.NASAllowlist, CreatedAt: now, UpdatedAt: now, ExpiresAt: input.ExpiresAt}, Digest: digest[:]}
	err = r.db.Update(func(tx *bolt.Tx) error {
		if err := validateTokenNAS(tx, record.NASAllowlist); err != nil {
			return err
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketMCPTokens).Put([]byte(id), encoded); err != nil {
			return err
		}
		return tx.Bucket(bucketTokenDigests).Put(digest[:], []byte(id))
	})
	if err != nil {
		return IssuedMCPToken{}, err
	}
	return IssuedMCPToken{Token: record.MCPToken, BearerToken: raw}, nil
}

func (r *Repository) MCPTokens(ctx context.Context) ([]MCPToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var tokens []MCPToken
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketMCPTokens).ForEach(func(_, value []byte) error {
			record, err := decodeMCPToken(value)
			if err != nil {
				return err
			}
			tokens = append(tokens, record.MCPToken)
			return nil
		})
	})
	sort.Slice(tokens, func(i, j int) bool { return tokens[i].CreatedAt.Before(tokens[j].CreatedAt) })
	return tokens, err
}

func (r *Repository) MCPToken(ctx context.Context, id string) (MCPToken, error) {
	if err := ctx.Err(); err != nil {
		return MCPToken{}, err
	}
	var token MCPToken
	err := r.db.View(func(tx *bolt.Tx) error { record, err := readMCPToken(tx, id); token = record.MCPToken; return err })
	return token, err
}

func (r *Repository) RotateMCPToken(ctx context.Context, id string) (IssuedMCPToken, error) {
	if err := ctx.Err(); err != nil {
		return IssuedMCPToken{}, err
	}
	raw, err := randomToken(32)
	if err != nil {
		return IssuedMCPToken{}, err
	}
	raw = mcpTokenPrefix + raw
	digest := sha256.Sum256([]byte(raw))
	var token MCPToken
	err = r.db.Update(func(tx *bolt.Tx) error {
		record, err := readMCPToken(tx, id)
		if err != nil {
			return err
		}
		if record.RevokedAt != nil {
			return errors.New("revoked MCP token cannot be rotated")
		}
		if err := tx.Bucket(bucketTokenDigests).Delete(record.Digest); err != nil {
			return err
		}
		record.Digest = append([]byte(nil), digest[:]...)
		record.UpdatedAt = time.Now().UTC()
		token = record.MCPToken
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketMCPTokens).Put([]byte(id), encoded); err != nil {
			return err
		}
		return tx.Bucket(bucketTokenDigests).Put(digest[:], []byte(id))
	})
	if err != nil {
		return IssuedMCPToken{}, err
	}
	return IssuedMCPToken{Token: token, BearerToken: raw}, nil
}

func (r *Repository) RevokeMCPToken(ctx context.Context, id string) (MCPToken, error) {
	return r.changeMCPToken(ctx, id, func(record *mcpTokenRecord, now time.Time) error {
		if record.RevokedAt == nil {
			record.RevokedAt = &now
		}
		return nil
	})
}

func (r *Repository) ExpireMCPToken(ctx context.Context, id string, expiresAt time.Time) (MCPToken, error) {
	if expiresAt.IsZero() {
		return MCPToken{}, errors.New("expires_at is required")
	}
	expiresAt = expiresAt.UTC()
	return r.changeMCPToken(ctx, id, func(record *mcpTokenRecord, _ time.Time) error { record.ExpiresAt = &expiresAt; return nil })
}

func (r *Repository) changeMCPToken(ctx context.Context, id string, change func(*mcpTokenRecord, time.Time) error) (MCPToken, error) {
	if err := ctx.Err(); err != nil {
		return MCPToken{}, err
	}
	var token MCPToken
	err := r.db.Update(func(tx *bolt.Tx) error {
		record, err := readMCPToken(tx, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := change(&record, now); err != nil {
			return err
		}
		record.UpdatedAt = now
		token = record.MCPToken
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketMCPTokens).Put([]byte(id), encoded); err != nil {
			return err
		}
		if record.RevokedAt != nil {
			return deletePendingApprovalsForToken(tx, record.ID)
		}
		return nil
	})
	return token, err
}

func (r *Repository) AuthenticateMCPToken(ctx context.Context, raw string) (remotepolicy.Principal, error) {
	if err := ctx.Err(); err != nil {
		return remotepolicy.Principal{}, err
	}
	if !strings.HasPrefix(raw, mcpTokenPrefix) || len(raw) < len(mcpTokenPrefix)+32 {
		return remotepolicy.Principal{}, ErrTokenUnauthorized
	}
	digest := sha256.Sum256([]byte(raw))
	raw = ""
	var principal remotepolicy.Principal
	err := r.db.Update(func(tx *bolt.Tx) error {
		id := tx.Bucket(bucketTokenDigests).Get(digest[:])
		if len(id) == 0 {
			return ErrTokenUnauthorized
		}
		record, err := readMCPToken(tx, string(id))
		if err != nil {
			return ErrTokenUnauthorized
		}
		now := time.Now().UTC()
		if err := validateActiveToken(record, now); err != nil {
			return err
		}
		record.LastUsedAt = &now
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := tx.Bucket(bucketMCPTokens).Put([]byte(record.ID), encoded); err != nil {
			return err
		}
		principal = principalFor(record.MCPToken)
		return nil
	})
	if err != nil {
		return remotepolicy.Principal{}, ErrTokenUnauthorized
	}
	return principal, nil
}

func (r *Repository) CreateApproval(ctx context.Context, input ApprovalInput, administrator string) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	input.PlanHash = strings.ToLower(strings.TrimSpace(input.PlanHash))
	input.NAS = strings.TrimSpace(input.NAS)
	input.RequestingTokenID = strings.TrimSpace(input.RequestingTokenID)
	administrator = strings.TrimSpace(administrator)
	if err := validateApprovalInput(input, administrator); err != nil {
		return Approval{}, err
	}
	var approval Approval
	err := r.db.Update(func(tx *bolt.Tx) error {
		var err error
		approval, _, err = r.createApprovalTx(tx, input, administrator, r.now().UTC())
		return err
	})
	return approval, err
}

func validateApprovalInput(input ApprovalInput, administrator string) error {
	if len(input.PlanHash) != 64 {
		return errors.New("plan_hash must be a SHA-256 hash")
	}
	for _, char := range input.PlanHash {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return errors.New("plan_hash must be a SHA-256 hash")
		}
	}
	if input.NAS == "" || input.RequestingTokenID == "" {
		return errors.New("NAS and requesting_token_id are required")
	}
	if administrator == "" {
		return errors.New("administrator identity is required")
	}
	if input.TTL < 0 || input.TTL > DefaultApprovalTTL {
		return fmt.Errorf("approval TTL must be at most %s", DefaultApprovalTTL)
	}
	return nil
}

func (r *Repository) createApprovalTx(tx *bolt.Tx, input ApprovalInput, administrator string, now time.Time) (Approval, bool, error) {
	profile, err := readProfile(tx, input.NAS)
	if err != nil {
		return Approval{}, false, err
	}
	if input.ProfileRevision == 0 {
		input.ProfileRevision = profile.Revision
	}
	if profile.Revision != input.ProfileRevision {
		return Approval{}, false, errors.New("NAS profile revision does not match")
	}
	token, err := readMCPToken(tx, input.RequestingTokenID)
	if err != nil {
		return Approval{}, false, err
	}
	if err := validateActiveToken(token, now); err != nil {
		return Approval{}, false, err
	}
	if !sliceContains(token.Scopes, remotepolicy.ScopeApply) || !sliceContains(token.NASAllowlist, input.NAS) {
		return Approval{}, false, remotepolicy.ErrDenied
	}
	if existing, ok, err := existingApproval(tx, input, now); err != nil {
		return Approval{}, false, err
	} else if ok {
		if err := deleteMatchingApprovalRequests(tx, input.PlanHash, input.RequestingTokenID); err != nil {
			return Approval{}, false, err
		}
		return existing, false, nil
	}
	id, err := randomID(16)
	if err != nil {
		return Approval{}, false, err
	}
	ttl := input.TTL
	if ttl == 0 {
		ttl = DefaultApprovalTTL
	}
	approval := Approval{ID: id, PlanHash: input.PlanHash, NAS: input.NAS, ProfileRevision: input.ProfileRevision, RequestingTokenID: input.RequestingTokenID, Administrator: administrator, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	encoded, err := json.Marshal(approval)
	if err != nil {
		return Approval{}, false, err
	}
	if err := tx.Bucket(bucketApprovals).Put([]byte(id), encoded); err != nil {
		return Approval{}, false, err
	}
	if err := deleteMatchingApprovalRequests(tx, input.PlanHash, input.RequestingTokenID); err != nil {
		return Approval{}, false, err
	}
	return approval, true, nil
}

func (r *Repository) Approvals(ctx context.Context, includeConsumed bool) ([]Approval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []Approval
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketApprovals).ForEach(func(_, value []byte) error {
			var item Approval
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			if includeConsumed || item.ConsumedAt == nil {
				result = append(result, item)
			}
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, err
}

// RecordPendingApproval stores only the closed plan summary supplied by the
// remote policy boundary. Requests are deduplicated per plan hash and token,
// refreshed on re-plan, and bounded independently of standard approvals.
func (r *Repository) RecordPendingApproval(ctx context.Context, input remotepolicy.PendingApprovalRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input.PlanHash = strings.ToLower(strings.TrimSpace(input.PlanHash))
	input.NAS = strings.TrimSpace(input.NAS)
	input.RequestingTokenID = strings.TrimSpace(input.RequestingTokenID)
	input.Tool = strings.TrimSpace(input.Tool)
	input.Risk = strings.ToLower(strings.TrimSpace(input.Risk))
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.Summary = strings.TrimSpace(input.Summary)
	if err := validatePendingApproval(input); err != nil {
		return err
	}
	now := r.now().UTC()
	return r.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketApprovalRequests)
		if err := prunePendingApprovals(bucket, now); err != nil {
			return err
		}
		var current PendingApproval
		if err := bucket.ForEach(func(_, value []byte) error {
			var candidate PendingApproval
			if err := json.Unmarshal(value, &candidate); err != nil {
				return err
			}
			if candidate.PlanHash == input.PlanHash && candidate.RequestingTokenID == input.RequestingTokenID {
				current = candidate
				return errApprovalMatch
			}
			return nil
		}); err != nil && !errors.Is(err, errApprovalMatch) {
			return err
		}
		if current.ID == "" {
			id, err := randomID(16)
			if err != nil {
				return err
			}
			current.ID = id
		}
		current.PlanHash = input.PlanHash
		current.NAS = input.NAS
		current.ProfileRevision = input.ProfileRevision
		current.RequestingTokenID = input.RequestingTokenID
		current.Tool = input.Tool
		current.Risk = input.Risk
		current.ResourceID = input.ResourceID
		current.Summary = input.Summary
		current.CreatedAt = now
		current.ExpiresAt = now.Add(PendingApprovalTTL)
		encoded, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if err := bucket.Put([]byte(current.ID), encoded); err != nil {
			return err
		}
		return boundPendingApprovals(bucket)
	})
}

func (r *Repository) PendingApprovals(ctx context.Context) ([]PendingApproval, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []PendingApproval
	now := r.now().UTC()
	err := r.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketApprovalRequests)
		if err := prunePendingApprovals(bucket, now); err != nil {
			return err
		}
		return bucket.ForEach(func(_, value []byte) error {
			var item PendingApproval
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			if token, err := readMCPToken(tx, item.RequestingTokenID); err == nil {
				item.RequestingToken = token.Name
			}
			result = append(result, item)
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, err
}

func (r *Repository) ApprovePendingApproval(ctx context.Context, id, administrator string) (Approval, error) {
	if err := ctx.Err(); err != nil {
		return Approval{}, err
	}
	id = strings.TrimSpace(id)
	administrator = strings.TrimSpace(administrator)
	if id == "" || administrator == "" {
		return Approval{}, errors.New("approval request and administrator are required")
	}
	var approval Approval
	now := r.now().UTC()
	err := r.db.Update(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketApprovalRequests).Get([]byte(id))
		if value == nil {
			return fmt.Errorf("%w: pending approval", ErrNotFound)
		}
		var request PendingApproval
		if err := json.Unmarshal(value, &request); err != nil {
			return err
		}
		if !now.Before(request.ExpiresAt) {
			if err := tx.Bucket(bucketApprovalRequests).Delete([]byte(id)); err != nil {
				return err
			}
			return fmt.Errorf("%w: pending approval expired", ErrNotFound)
		}
		var err error
		approval, _, err = r.createApprovalTx(tx, ApprovalInput{PlanHash: request.PlanHash, NAS: request.NAS, ProfileRevision: request.ProfileRevision, RequestingTokenID: request.RequestingTokenID}, administrator, now)
		if err != nil {
			return err
		}
		return r.appendAuditTx(tx, AuditEvent{Time: now, ActorType: "gateway_admin", ActorID: administrator, Action: "approval.request.approve", Tool: request.Tool, NAS: request.NAS, Outcome: "success"})
	})
	return approval, err
}

func (r *Repository) DismissPendingApproval(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("approval request is required")
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketApprovalRequests)
		if bucket.Get([]byte(id)) == nil {
			return fmt.Errorf("%w: pending approval", ErrNotFound)
		}
		return bucket.Delete([]byte(id))
	})
}

// AdmitRemoteApply re-evaluates token validity, scope, target access, profile
// revision and (for high risk) an exact approval in one writable transaction.
// The approval and mandatory audit admission are committed before application
// precondition reads, so retries and postcondition failures cannot restore it.
func (r *Repository) AdmitRemoteApply(ctx context.Context, tokenID, nas string, revision uint64, planHash, risk string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.auditFailure != nil {
		if err := r.auditFailure(); err != nil {
			return fmt.Errorf("persist mandatory apply audit: %w", err)
		}
	}
	now := time.Now().UTC()
	return r.db.Update(func(tx *bolt.Tx) error {
		token, err := readMCPToken(tx, tokenID)
		if err != nil {
			return remotepolicy.ErrDenied
		}
		if err := validateActiveToken(token, now); err != nil {
			return remotepolicy.ErrDenied
		}
		if !sliceContains(token.Scopes, remotepolicy.ScopeApply) || !sliceContains(token.NASAllowlist, nas) {
			return remotepolicy.ErrDenied
		}
		profile, err := readProfile(tx, nas)
		if err != nil || profile.Revision != revision {
			return errors.New("NAS profile revision changed; create a new plan and approval")
		}
		if strings.EqualFold(risk, "high") {
			approval, err := findApproval(tx, tokenID, nas, revision, planHash, now)
			if err != nil {
				return err
			}
			approval.ConsumedAt = &now
			encoded, err := json.Marshal(approval)
			if err != nil {
				return err
			}
			if err := tx.Bucket(bucketApprovals).Put([]byte(approval.ID), encoded); err != nil {
				return err
			}
		}
		return r.appendAuditTx(tx, AuditEvent{Time: now, CorrelationID: remotepolicy.CorrelationID(ctx), ActorType: "mcp_token", ActorID: tokenID, Action: "apply.admit", NAS: nas, Outcome: "admitted"})
	})
}

func (r *Repository) AppendAudit(ctx context.Context, event AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.auditFailure != nil {
		if err := r.auditFailure(); err != nil {
			return err
		}
	}
	return r.db.Update(func(tx *bolt.Tx) error { return r.appendAuditTx(tx, event) })
}

func (r *Repository) appendAuditTx(tx *bolt.Tx, event AuditEvent) error {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	} else {
		event.Time = event.Time.UTC()
	}
	if event.ID == "" {
		id, err := randomID(8)
		if err != nil {
			return err
		}
		event.ID = id
	}
	event.Reason = safeAuditReason(event.Reason)
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	key := make([]byte, 8+len(event.ID))
	binary.BigEndian.PutUint64(key[:8], uint64(event.Time.UnixNano()))
	copy(key[8:], event.ID)
	bucket := tx.Bucket(bucketAudit)
	if err := bucket.Put(key, encoded); err != nil {
		return err
	}
	cutoff := time.Now().UTC().Add(-auditRetention)
	for cursor := bucket.Cursor(); ; {
		key, value := cursor.First()
		if key == nil {
			break
		}
		var oldest AuditEvent
		if json.Unmarshal(value, &oldest) != nil || (!oldest.Time.Before(cutoff) && bucket.Stats().KeyN <= maxAuditEvents) {
			break
		}
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) AuditEvents(ctx context.Context, query AuditQuery) ([]AuditEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var result []AuditEvent
	err := r.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketAudit).Cursor()
		for key, value := cursor.Last(); key != nil && len(result) < limit; key, value = cursor.Prev() {
			var event AuditEvent
			if err := json.Unmarshal(value, &event); err != nil {
				return err
			}
			if !query.After.IsZero() && !event.Time.After(query.After) {
				continue
			}
			if query.ActorID != "" && event.ActorID != query.ActorID {
				continue
			}
			if query.Action != "" && event.Action != query.Action {
				continue
			}
			result = append(result, event)
		}
		return nil
	})
	return result, err
}

// AuditExport returns every retained event in chronological order. The store
// is already bounded to maxAuditEvents, so export never inherits the 1,000-row
// interactive-view limit.
func (r *Repository) AuditExport(ctx context.Context) ([]AuditEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]AuditEvent, 0)
	err := r.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(bucketAudit).Cursor()
		for _, value := cursor.First(); value != nil; _, value = cursor.Next() {
			var event AuditEvent
			if err := json.Unmarshal(value, &event); err != nil {
				return err
			}
			result = append(result, event)
		}
		return nil
	})
	return result, err
}

func normalizeMCPTokenInput(input MCPTokenInput) (MCPTokenInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > MaxMCPTokenNameBytes {
		return MCPTokenInput{}, fmt.Errorf("token name must be 1 to %d bytes", MaxMCPTokenNameBytes)
	}
	if strings.IndexFunc(input.Name, func(character rune) bool { return character < 0x20 || character == 0x7f }) >= 0 {
		return MCPTokenInput{}, errors.New("token name must not contain control characters")
	}
	if len(input.Scopes) == 0 {
		input.Scopes = []string{remotepolicy.ScopeRead}
	}
	input.Scopes = uniqueSorted(input.Scopes)
	input.NASAllowlist = uniqueSorted(input.NASAllowlist)
	for _, scope := range input.Scopes {
		if _, ok := validScopes[scope]; !ok {
			return MCPTokenInput{}, fmt.Errorf("unsupported scope %q", scope)
		}
	}
	if input.ExpiresAt != nil {
		value := input.ExpiresAt.UTC()
		if !value.After(time.Now()) {
			return MCPTokenInput{}, errors.New("expires_at must be in the future")
		}
		input.ExpiresAt = &value
	}
	return input, nil
}

func migrateLANDiscoveryScope(tx *bolt.Tx) error {
	bucket := tx.Bucket(bucketMCPTokens)
	return bucket.ForEach(func(key, value []byte) error {
		record, err := decodeMCPToken(value)
		if err != nil {
			return err
		}
		changed := false
		for index, scope := range record.Scopes {
			if scope == "nas.admin" {
				record.Scopes[index] = remotepolicy.ScopeLANDiscover
				changed = true
			}
		}
		if !changed {
			return nil
		}
		record.Scopes = uniqueSorted(record.Scopes)
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return bucket.Put(key, encoded)
	})
}

func validatePendingApproval(input remotepolicy.PendingApprovalRequest) error {
	if len(input.PlanHash) != 64 {
		return errors.New("plan hash must be a SHA-256 hash")
	}
	for _, char := range input.PlanHash {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return errors.New("plan hash must be a SHA-256 hash")
		}
	}
	if input.NAS == "" || input.ProfileRevision == 0 || input.RequestingTokenID == "" || input.Tool == "" || input.Risk != "high" || input.Summary == "" {
		return errors.New("pending approval requires NAS, revision, token, tool, high risk, and summary")
	}
	if len(input.NAS) > 64 || len(input.RequestingTokenID) > 64 || len(input.Tool) > 128 || len(input.ResourceID) > 256 || len(input.Summary) > 4096 {
		return errors.New("pending approval scalar exceeds its size limit")
	}
	return nil
}

func existingApproval(tx *bolt.Tx, input ApprovalInput, now time.Time) (Approval, bool, error) {
	var match Approval
	err := tx.Bucket(bucketApprovals).ForEach(func(_, value []byte) error {
		var candidate Approval
		if err := json.Unmarshal(value, &candidate); err != nil {
			return err
		}
		if candidate.ConsumedAt == nil && now.Before(candidate.ExpiresAt) && candidate.PlanHash == input.PlanHash && candidate.NAS == input.NAS && candidate.ProfileRevision == input.ProfileRevision && candidate.RequestingTokenID == input.RequestingTokenID {
			match = candidate
			return errApprovalMatch
		}
		return nil
	})
	if err != nil && !errors.Is(err, errApprovalMatch) {
		return Approval{}, false, err
	}
	return match, match.ID != "", nil
}

func deleteMatchingApprovalRequests(tx *bolt.Tx, hash, tokenID string) error {
	bucket := tx.Bucket(bucketApprovalRequests)
	var ids [][]byte
	if err := bucket.ForEach(func(key, value []byte) error {
		var request PendingApproval
		if err := json.Unmarshal(value, &request); err != nil {
			return err
		}
		if request.PlanHash == hash && request.RequestingTokenID == tokenID {
			ids = append(ids, append([]byte(nil), key...))
		}
		return nil
	}); err != nil {
		return err
	}
	for _, id := range ids {
		if err := bucket.Delete(id); err != nil {
			return err
		}
	}
	return nil
}

func deletePendingApprovalsForToken(tx *bolt.Tx, tokenID string) error {
	bucket := tx.Bucket(bucketApprovalRequests)
	var ids [][]byte
	if err := bucket.ForEach(func(key, value []byte) error {
		var request PendingApproval
		if err := json.Unmarshal(value, &request); err != nil {
			return err
		}
		if request.RequestingTokenID == tokenID {
			ids = append(ids, append([]byte(nil), key...))
		}
		return nil
	}); err != nil {
		return err
	}
	for _, id := range ids {
		if err := bucket.Delete(id); err != nil {
			return err
		}
	}
	return nil
}

func prunePendingApprovals(bucket *bolt.Bucket, now time.Time) error {
	var ids [][]byte
	if err := bucket.ForEach(func(key, value []byte) error {
		var request PendingApproval
		if err := json.Unmarshal(value, &request); err != nil {
			return err
		}
		if !now.Before(request.ExpiresAt) {
			ids = append(ids, append([]byte(nil), key...))
		}
		return nil
	}); err != nil {
		return err
	}
	for _, id := range ids {
		if err := bucket.Delete(id); err != nil {
			return err
		}
	}
	return nil
}

func boundPendingApprovals(bucket *bolt.Bucket) error {
	for {
		count := 0
		var oldest PendingApproval
		var oldestKey []byte
		if err := bucket.ForEach(func(key, value []byte) error {
			count++
			var request PendingApproval
			if err := json.Unmarshal(value, &request); err != nil {
				return err
			}
			if oldestKey == nil || request.CreatedAt.Before(oldest.CreatedAt) {
				oldest = request
				oldestKey = append([]byte(nil), key...)
			}
			return nil
		}); err != nil {
			return err
		}
		if count <= MaxPendingApprovals || oldestKey == nil {
			return nil
		}
		if err := bucket.Delete(oldestKey); err != nil {
			return err
		}
	}
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func validateTokenNAS(tx *bolt.Tx, names []string) error {
	for _, name := range names {
		if _, err := readProfile(tx, name); err != nil {
			return fmt.Errorf("NAS allowlist entry is not configured")
		}
	}
	return nil
}
func decodeMCPToken(value []byte) (mcpTokenRecord, error) {
	var record mcpTokenRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return mcpTokenRecord{}, err
	}
	return record, nil
}
func readMCPToken(tx *bolt.Tx, id string) (mcpTokenRecord, error) {
	value := tx.Bucket(bucketMCPTokens).Get([]byte(id))
	if value == nil {
		return mcpTokenRecord{}, fmt.Errorf("%w: MCP token", ErrNotFound)
	}
	return decodeMCPToken(value)
}
func validateActiveToken(record mcpTokenRecord, now time.Time) error {
	if record.RevokedAt != nil || (record.ExpiresAt != nil && !now.Before(*record.ExpiresAt)) {
		return ErrTokenUnauthorized
	}
	return nil
}
func principalFor(token MCPToken) remotepolicy.Principal {
	principal := remotepolicy.Principal{TokenID: token.ID, Name: token.Name, Scopes: map[string]struct{}{}, NAS: map[string]struct{}{}}
	for _, scope := range token.Scopes {
		principal.Scopes[scope] = struct{}{}
	}
	for _, nas := range token.NASAllowlist {
		principal.NAS[nas] = struct{}{}
	}
	return principal
}
func sliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func findApproval(tx *bolt.Tx, tokenID, nas string, revision uint64, hash string, now time.Time) (Approval, error) {
	var match Approval
	err := tx.Bucket(bucketApprovals).ForEach(func(_, value []byte) error {
		var candidate Approval
		if err := json.Unmarshal(value, &candidate); err != nil {
			return err
		}
		if candidate.ConsumedAt == nil && now.Before(candidate.ExpiresAt) && candidate.RequestingTokenID == tokenID && candidate.NAS == nas && candidate.ProfileRevision == revision && candidate.PlanHash == hash && candidate.Administrator != "" {
			match = candidate
			return errApprovalMatch
		}
		return nil
	})
	if err != nil && !errors.Is(err, errApprovalMatch) {
		return Approval{}, err
	}
	if match.ID == "" {
		return Approval{}, ErrApprovalRequired
	}
	return match, nil
}
func safeAuditReason(reason string) string {
	switch reason {
	case "", "authorized", "denied", "success", "failure", "invalid_token", "expired", "revoked", "rate_limited", "approval_required", "storage_failure":
		return reason
	default:
		return "failure"
	}
}
