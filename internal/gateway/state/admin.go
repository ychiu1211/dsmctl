package state

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
	"golang.org/x/crypto/argon2"
)

// PasswordHashParameters is persisted in each Argon2id PHC verifier. The
// defaults stay below the packaged 256 MiB limit and the parser enforces hard
// upper bounds before allocating memory for a verifier read from disk.
type PasswordHashParameters struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultPasswordHashParameters = PasswordHashParameters{
	MemoryKiB: 32 * 1024, Iterations: 3, Parallelism: 1, SaltLength: 16, KeyLength: 32,
}

type AdministratorStatus struct {
	Initialized   bool      `json:"initialized"`
	Username      string    `json:"username,omitempty"`
	InitializedAt time.Time `json:"initialized_at,omitempty"`
}

type administratorSessionRecord struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (parameters PasswordHashParameters) validate() error {
	if parameters.MemoryKiB < 8 || parameters.MemoryKiB > 128*1024 {
		return errors.New("Argon2id memory must be between 8 KiB and 128 MiB")
	}
	if parameters.Iterations < 1 || parameters.Iterations > 10 {
		return errors.New("Argon2id iterations must be between 1 and 10")
	}
	if parameters.Parallelism < 1 || parameters.Parallelism > 4 {
		return errors.New("Argon2id parallelism must be between 1 and 4")
	}
	if parameters.SaltLength < 8 || parameters.SaltLength > 64 || parameters.KeyLength < 16 || parameters.KeyLength > 64 {
		return errors.New("Argon2id salt or key length is outside the safe bound")
	}
	return nil
}

func normalizeAdministratorUsername(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 3 || len(value) > 64 {
		return "", errors.New("administrator username must be between 3 and 64 characters")
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			continue
		}
		return "", errors.New("administrator username may contain only ASCII letters, digits, dot, underscore, and hyphen")
	}
	return value, nil
}

func validateNewAdministratorPassword(value string) error {
	if utf8.RuneCountInString(value) < 12 || len(value) > 4096 {
		return errors.New("administrator password must be at least 12 characters")
	}
	return nil
}

func validateLoginPassword(value string) error {
	if value == "" || len(value) > 1024 {
		return ErrUnauthorized
	}
	return nil
}

func (r *Repository) AdministratorStatus(ctx context.Context) (AdministratorStatus, error) {
	if err := ctx.Err(); err != nil {
		return AdministratorStatus{}, err
	}
	var status AdministratorStatus
	err := r.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if !administratorConfigured(meta) {
			return nil
		}
		if err := validateAdministratorRecord(meta); err != nil {
			return err
		}
		status.Initialized = true
		status.Username = string(meta.Get(keyAdminUsername))
		if value := string(meta.Get(keyAdminInitializedAt)); value != "" {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return fmt.Errorf("decode administrator initialization time: %w", err)
			}
			status.InitializedAt = parsed
		}
		return nil
	})
	return status, err
}

// CreateAdministrator initializes the single local account and its first
// browser session in one transaction. The returned token is for Set-Cookie
// only and must never be encoded in an API response.
func (r *Repository) CreateAdministrator(ctx context.Context, username, password string) (string, AdministratorSession, error) {
	if err := ctx.Err(); err != nil {
		return "", AdministratorSession{}, err
	}
	username, err := normalizeAdministratorUsername(username)
	if err != nil {
		return "", AdministratorSession{}, err
	}
	if err := validateNewAdministratorPassword(password); err != nil {
		return "", AdministratorSession{}, err
	}
	verifier, err := r.hashPassword(ctx, password)
	if err != nil {
		return "", AdministratorSession{}, err
	}
	password = ""
	token, digest, session, err := r.newAdministratorSession(username)
	if err != nil {
		return "", AdministratorSession{}, err
	}
	err = r.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if administratorConfigured(meta) {
			return ErrAlreadyInitialized
		}
		if err := meta.Put(keyAdminUsername, []byte(username)); err != nil {
			return err
		}
		if err := meta.Put(keyAdminPassword, []byte(verifier)); err != nil {
			return err
		}
		if err := meta.Put(keyAdminInitializedAt, []byte(session.CreatedAt.Format(time.RFC3339Nano))); err != nil {
			return err
		}
		return putAdministratorSession(tx.Bucket(bucketAdminSessions), digest, session)
	})
	if err != nil {
		return "", AdministratorSession{}, err
	}
	return token, publicAdministratorSession(session), nil
}

func (r *Repository) LoginAdministrator(ctx context.Context, username, password string) (string, AdministratorSession, error) {
	if err := ctx.Err(); err != nil {
		return "", AdministratorSession{}, err
	}
	normalized, usernameErr := normalizeAdministratorUsername(username)
	passwordErr := validateLoginPassword(password)
	var storedUsername, verifier string
	if err := r.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		storedUsername = string(meta.Get(keyAdminUsername))
		verifier = string(meta.Get(keyAdminPassword))
		return nil
	}); err != nil {
		return "", AdministratorSession{}, err
	}
	verificationTarget := verifier
	if usernameErr != nil || passwordErr != nil || normalized != storedUsername || verificationTarget == "" {
		verificationTarget = r.dummyPasswordVerifier()
	}
	valid := r.verifyPassword(ctx, password, verificationTarget)
	password = ""
	if usernameErr != nil || passwordErr != nil || normalized != storedUsername || verifier == "" || !valid {
		return "", AdministratorSession{}, ErrUnauthorized
	}
	token, digest, session, err := r.newAdministratorSession(storedUsername)
	if err != nil {
		return "", AdministratorSession{}, err
	}
	err = r.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if string(meta.Get(keyAdminUsername)) != storedUsername || subtle.ConstantTimeCompare(meta.Get(keyAdminPassword), []byte(verifier)) != 1 {
			return ErrUnauthorized
		}
		return r.putBoundedAdministratorSession(tx.Bucket(bucketAdminSessions), digest, session)
	})
	if err != nil {
		return "", AdministratorSession{}, err
	}
	return token, publicAdministratorSession(session), nil
}

func (r *Repository) AuthenticateAdministratorSession(ctx context.Context, token string) (AdministratorSession, error) {
	if err := ctx.Err(); err != nil {
		return AdministratorSession{}, err
	}
	digest := sha256.Sum256([]byte(token))
	var record administratorSessionRecord
	err := r.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketAdminSessions).Get(digest[:])
		if value == nil || json.Unmarshal(value, &record) != nil {
			return ErrUnauthorized
		}
		meta := tx.Bucket(bucketMeta)
		if validateAdministratorRecord(meta) != nil || record.Username != string(meta.Get(keyAdminUsername)) || !r.now().UTC().Before(record.ExpiresAt) {
			return ErrUnauthorized
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			_ = r.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(bucketAdminSessions).Delete(digest[:]) })
		}
		return AdministratorSession{}, err
	}
	return publicAdministratorSession(record), nil
}

func (r *Repository) LogoutAdministrator(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(token))
	return r.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(bucketAdminSessions).Delete(digest[:]) })
}

func (r *Repository) ChangeAdministratorPassword(ctx context.Context, sessionToken, currentPassword, newPassword string) error {
	if err := validateNewAdministratorPassword(newPassword); err != nil {
		return err
	}
	session, err := r.AuthenticateAdministratorSession(ctx, sessionToken)
	if err != nil {
		return err
	}
	var oldVerifier string
	if err := r.db.View(func(tx *bolt.Tx) error {
		oldVerifier = string(tx.Bucket(bucketMeta).Get(keyAdminPassword))
		return nil
	}); err != nil {
		return err
	}
	if validateLoginPassword(currentPassword) != nil || !r.verifyPassword(ctx, currentPassword, oldVerifier) {
		return ErrUnauthorized
	}
	currentPassword = ""
	newVerifier, err := r.hashPassword(ctx, newPassword)
	if err != nil {
		return err
	}
	newPassword = ""
	currentDigest := sha256.Sum256([]byte(sessionToken))
	return r.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if subtle.ConstantTimeCompare(meta.Get(keyAdminPassword), []byte(oldVerifier)) != 1 {
			return ErrUnauthorized
		}
		bucket := tx.Bucket(bucketAdminSessions)
		value := bucket.Get(currentDigest[:])
		var current administratorSessionRecord
		if value == nil || json.Unmarshal(value, &current) != nil || current.ID != session.ID || !r.now().UTC().Before(current.ExpiresAt) {
			return ErrUnauthorized
		}
		if err := meta.Put(keyAdminPassword, []byte(newVerifier)); err != nil {
			return err
		}
		return deleteOtherAdministratorSessions(bucket, currentDigest[:])
	})
}

func (r *Repository) RevokeOtherAdministratorSessions(ctx context.Context, sessionToken string) error {
	if _, err := r.AuthenticateAdministratorSession(ctx, sessionToken); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(sessionToken))
	return r.db.Update(func(tx *bolt.Tx) error {
		return deleteOtherAdministratorSessions(tx.Bucket(bucketAdminSessions), digest[:])
	})
}

func (r *Repository) newAdministratorSession(username string) (string, []byte, administratorSessionRecord, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", nil, administratorSessionRecord{}, err
	}
	id, err := randomID(16)
	if err != nil {
		return "", nil, administratorSessionRecord{}, err
	}
	now := r.now().UTC()
	digest := sha256.Sum256([]byte(token))
	return token, digest[:], administratorSessionRecord{ID: id, Username: username, CreatedAt: now, ExpiresAt: now.Add(AdminSessionTTL)}, nil
}

func putAdministratorSession(bucket *bolt.Bucket, digest []byte, session administratorSessionRecord) error {
	encoded, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return bucket.Put(digest, encoded)
}

func (r *Repository) putBoundedAdministratorSession(bucket *bolt.Bucket, digest []byte, session administratorSessionRecord) error {
	var expired [][]byte
	type existingSession struct {
		key       []byte
		createdAt time.Time
	}
	var active []existingSession
	if err := bucket.ForEach(func(key, value []byte) error {
		var record administratorSessionRecord
		if err := json.Unmarshal(value, &record); err != nil || !r.now().UTC().Before(record.ExpiresAt) {
			expired = append(expired, append([]byte(nil), key...))
			return nil
		}
		active = append(active, existingSession{key: append([]byte(nil), key...), createdAt: record.CreatedAt})
		return nil
	}); err != nil {
		return err
	}
	for _, key := range expired {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	for len(active) >= MaxAdminSessions {
		oldest := 0
		for index := 1; index < len(active); index++ {
			if active[index].createdAt.Before(active[oldest].createdAt) {
				oldest = index
			}
		}
		if err := bucket.Delete(active[oldest].key); err != nil {
			return err
		}
		active = append(active[:oldest], active[oldest+1:]...)
	}
	return putAdministratorSession(bucket, digest, session)
}

func deleteOtherAdministratorSessions(bucket *bolt.Bucket, keep []byte) error {
	var remove [][]byte
	if err := bucket.ForEach(func(key, _ []byte) error {
		if subtle.ConstantTimeCompare(key, keep) != 1 {
			remove = append(remove, append([]byte(nil), key...))
		}
		return nil
	}); err != nil {
		return err
	}
	for _, key := range remove {
		if err := bucket.Delete(key); err != nil {
			return err
		}
	}
	return nil
}

func publicAdministratorSession(record administratorSessionRecord) AdministratorSession {
	return AdministratorSession{ID: record.ID, Username: record.Username, CreatedAt: record.CreatedAt, ExpiresAt: record.ExpiresAt}
}

func (r *Repository) hashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, r.passwordHash.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("create administrator password salt: %w", err)
	}
	return r.hashPasswordWithSalt(ctx, password, salt, r.passwordHash)
}

func (r *Repository) hashPasswordWithSalt(ctx context.Context, password string, salt []byte, parameters PasswordHashParameters) (string, error) {
	select {
	case r.hashSlots <- struct{}{}:
		defer func() { <-r.hashSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	key := argon2.IDKey([]byte(password), salt, parameters.Iterations, parameters.MemoryKiB, parameters.Parallelism, parameters.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, parameters.MemoryKiB, parameters.Iterations, parameters.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (r *Repository) verifyPassword(ctx context.Context, password, verifier string) bool {
	parameters, salt, expected, err := parsePasswordVerifier(verifier)
	if err != nil {
		return false
	}
	select {
	case r.hashSlots <- struct{}{}:
		defer func() { <-r.hashSlots }()
	case <-ctx.Done():
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, parameters.Iterations, parameters.MemoryKiB, parameters.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func parsePasswordVerifier(verifier string) (PasswordHashParameters, []byte, []byte, error) {
	parts := strings.Split(verifier, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return PasswordHashParameters{}, nil, nil, errors.New("invalid Argon2id verifier")
	}
	parameters := PasswordHashParameters{}
	var parallelism uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &parameters.MemoryKiB, &parameters.Iterations, &parallelism); err != nil || parallelism > 255 {
		return PasswordHashParameters{}, nil, nil, errors.New("invalid Argon2id parameters")
	}
	parameters.Parallelism = uint8(parallelism)
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", parameters.MemoryKiB, parameters.Iterations, parameters.Parallelism) {
		return PasswordHashParameters{}, nil, nil, errors.New("invalid Argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return PasswordHashParameters{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return PasswordHashParameters{}, nil, nil, errors.New("invalid Argon2id key")
	}
	parameters.SaltLength = uint32(len(salt))
	parameters.KeyLength = uint32(len(expected))
	if err := parameters.validate(); err != nil {
		return PasswordHashParameters{}, nil, nil, err
	}
	return parameters, salt, expected, nil
}

func (r *Repository) dummyPasswordVerifier() string {
	salt := []byte("dsmctl-dummy-salt")
	key := make([]byte, r.passwordHash.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, r.passwordHash.MemoryKiB, r.passwordHash.Iterations, r.passwordHash.Parallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

func (r *Repository) legacyAdministrationState() (bool, bool, error) {
	var legacyAdmin, managedData bool
	err := r.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if meta == nil {
			return nil
		}
		legacyAdmin = len(meta.Get(keyAdminDigest)) > 0 || len(meta.Get(keyBootstrapDigest)) > 0 || len(meta.Get(keyBootstrapUsed)) > 0 || len(meta.Get(keyAdminMode)) > 0
		for _, name := range [][]byte{bucketProfiles, bucketSecrets, bucketMCPTokens, bucketTokenDigests, bucketApprovals, bucketApprovalRequests} {
			if bucket := tx.Bucket(name); bucket != nil && bucket.Stats().KeyN > 0 {
				managedData = true
			}
		}
		return nil
	})
	if err != nil {
		return false, false, fmt.Errorf("inspect legacy administrator state: %w", err)
	}
	return legacyAdmin, managedData, nil
}
