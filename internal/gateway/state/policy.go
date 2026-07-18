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
	remotepolicy.ScopeApply: {}, remotepolicy.ScopeAdmin: {},
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
		return tx.Bucket(bucketMCPTokens).Put([]byte(id), encoded)
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
	if len(input.PlanHash) != 64 {
		return Approval{}, errors.New("plan_hash must be a SHA-256 hash")
	}
	for _, char := range input.PlanHash {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return Approval{}, errors.New("plan_hash must be a SHA-256 hash")
		}
	}
	if input.NAS == "" || input.ProfileRevision == 0 || input.RequestingTokenID == "" {
		return Approval{}, errors.New("NAS, profile_revision, and requesting_token_id are required")
	}
	administrator = strings.TrimSpace(administrator)
	if administrator == "" {
		return Approval{}, errors.New("administrator identity is required")
	}
	ttl := input.TTL
	if ttl == 0 {
		ttl = DefaultApprovalTTL
	}
	if ttl <= 0 || ttl > DefaultApprovalTTL {
		return Approval{}, fmt.Errorf("approval TTL must be at most %s", DefaultApprovalTTL)
	}
	id, err := randomID(16)
	if err != nil {
		return Approval{}, err
	}
	now := time.Now().UTC()
	approval := Approval{ID: id, PlanHash: input.PlanHash, NAS: input.NAS, ProfileRevision: input.ProfileRevision, RequestingTokenID: input.RequestingTokenID, Administrator: administrator, CreatedAt: now, ExpiresAt: now.Add(ttl)}
	err = r.db.Update(func(tx *bolt.Tx) error {
		profile, err := readProfile(tx, input.NAS)
		if err != nil || profile.Revision != input.ProfileRevision {
			return errors.New("NAS profile revision does not match")
		}
		token, err := readMCPToken(tx, input.RequestingTokenID)
		if err != nil {
			return err
		}
		if err := validateActiveToken(token, now); err != nil {
			return err
		}
		if !sliceContains(token.Scopes, remotepolicy.ScopeApply) || !sliceContains(token.NASAllowlist, input.NAS) {
			return remotepolicy.ErrDenied
		}
		encoded, err := json.Marshal(approval)
		if err != nil {
			return err
		}
		return tx.Bucket(bucketApprovals).Put([]byte(id), encoded)
	})
	return approval, err
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
