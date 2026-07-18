package state

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
)

var (
	bucketMeta         = []byte("meta")
	bucketProfiles     = []byte("profiles")
	bucketSecrets      = []byte("secrets")
	bucketMigrations   = []byte("migrations")
	bucketMCPTokens    = []byte("mcp_tokens")
	bucketTokenDigests = []byte("mcp_token_digests")
	bucketApprovals    = []byte("approvals")
	bucketAudit        = []byte("audit")

	keySchemaVersion   = []byte("schema_version")
	keyDefaultProfile  = []byte("default_profile")
	keyKeyCheck        = []byte("master_key_check")
	keyBootstrapDigest = []byte("bootstrap_digest")
	keyBootstrapUsed   = []byte("bootstrap_used")
	keyAdminDigest     = []byte("admin_digest")
	keyAdminMode       = []byte("admin_mode")
	keyRevisionCounter = []byte("profile_revision_counter")
)

const keyCheckPlaintext = "dsmctl-gateway-master-key-v1"

type profileRecord struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	URL                    string    `json:"url"`
	Username               string    `json:"username,omitempty"`
	TLSMode                string    `json:"tls_mode"`
	CertificateFingerprint string    `json:"certificate_fingerprint,omitempty"`
	TimeoutSeconds         int       `json:"timeout_seconds,omitempty"`
	Revision               uint64    `json:"revision"`
	PasswordSecretID       string    `json:"password_secret_id,omitempty"`
	TrustedDeviceSecretID  string    `json:"trusted_device_secret_id,omitempty"`
	SessionSecretID        string    `json:"session_secret_id,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type sealedSecret struct {
	Metadata   SecretMetadata `json:"metadata"`
	Nonce      string         `json:"nonce"`
	Ciphertext string         `json:"ciphertext"`
}

type Repository struct {
	db           *bolt.DB
	aead         cipher.AEAD
	path         string
	environment  *credentials.Environment
	closeOnce    sync.Once
	closeErr     error
	auditFailure func() error
}

type OpenOptions struct {
	// BeforeMigrationCommit is a test/diagnostic seam invoked inside the
	// migration transaction. Returning an error proves rollback and backup
	// behavior without shipping a deliberately broken schema migration.
	BeforeMigrationCommit func(from, to uint64) error
	// AuditFailure is a test seam. Production callers leave it nil. It lets
	// authorization tests prove that mutating requests fail before admission
	// when their mandatory audit record cannot be persisted.
	AuditFailure func() error
}

// ReadMasterKey reads an exact 32-byte AES-256 key. It deliberately does not
// trim data because doing so would silently alter valid binary key material.
func ReadMasterKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("master key file is required")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read master key file: %w", err)
	}
	if len(value) != 32 {
		return nil, errors.New("master key file must contain exactly 32 bytes")
	}
	return value, nil
}

func Open(path string, masterKey []byte) (*Repository, error) {
	return OpenWithOptions(path, masterKey, OpenOptions{})
}

func OpenWithOptions(path string, masterKey []byte, options OpenOptions) (*Repository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("gateway state path is required")
	}
	if len(masterKey) != 32 {
		return nil, errors.New("gateway master key must be exactly 32 bytes")
	}
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("initialize gateway vault cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize gateway vault AEAD: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create gateway data directory: %w", err)
	}
	_, statErr := os.Stat(path)
	existed := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect gateway state: %w", statErr)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second, NoGrowSync: false})
	if err != nil {
		return nil, fmt.Errorf("open gateway state: %w", err)
	}
	repository := &Repository{db: db, aead: aead, path: path, environment: credentials.NewEnvironment(), auditFailure: options.AuditFailure}
	if err := repository.initialize(existed, options); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repository, nil
}

func (r *Repository) initialize(existed bool, options OpenOptions) error {
	var version uint64
	var keyCheck []byte
	err := r.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if meta == nil {
			return nil
		}
		version = decodeUint64(meta.Get(keySchemaVersion))
		keyCheck = append([]byte(nil), meta.Get(keyKeyCheck)...)
		return nil
	})
	if err != nil {
		return fmt.Errorf("read gateway schema: %w", err)
	}
	if version > SchemaVersion {
		return fmt.Errorf("gateway state schema %d is newer than supported schema %d", version, SchemaVersion)
	}
	if len(keyCheck) > 0 {
		plain, err := r.openEnvelope("key-check", "key-check", "", keyCheck)
		if err != nil || subtle.ConstantTimeCompare(plain, []byte(keyCheckPlaintext)) != 1 {
			return errors.New("gateway master key does not match the existing encrypted state")
		}
	} else if existed && version > 0 {
		return errors.New("gateway state is missing its encrypted master-key check")
	}
	if version < SchemaVersion && existed {
		backup := fmt.Sprintf("%s.pre-v%d-%d.bak", r.path, version, time.Now().UnixNano())
		if err := r.db.View(func(tx *bolt.Tx) error { return tx.CopyFile(backup, 0o600) }); err != nil {
			return fmt.Errorf("back up gateway state before migration: %w", err)
		}
	}
	if err := r.db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(bucketMeta)
		if err != nil {
			return err
		}
		for _, name := range [][]byte{bucketProfiles, bucketSecrets, bucketMigrations, bucketMCPTokens, bucketTokenDigests, bucketApprovals, bucketAudit} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		if len(meta.Get(keyKeyCheck)) == 0 {
			sealed, err := r.sealPrefixed("key-check", "key-check", "", []byte(keyCheckPlaintext))
			if err != nil {
				return err
			}
			if err := meta.Put(keyKeyCheck, sealed); err != nil {
				return err
			}
		}
		if version < SchemaVersion && options.BeforeMigrationCommit != nil {
			if err := options.BeforeMigrationCommit(version, SchemaVersion); err != nil {
				return err
			}
		}
		if err := meta.Put(keySchemaVersion, encodeUint64(SchemaVersion)); err != nil {
			return err
		}
		if len(meta.Get(keyRevisionCounter)) == 0 {
			var highest uint64
			if err := tx.Bucket(bucketProfiles).ForEach(func(_, value []byte) error {
				record, err := decodeProfile(value)
				if err == nil && record.Revision > highest {
					highest = record.Revision
				}
				return err
			}); err != nil {
				return err
			}
			if err := meta.Put(keyRevisionCounter, encodeUint64(highest)); err != nil {
				return err
			}
		}
		if len(meta.Get(keyAdminMode)) == 0 && len(meta.Get(keyAdminDigest)) > 0 {
			if err := meta.Put(keyAdminMode, []byte(AdminModeLocal)); err != nil {
				return err
			}
		}
		return tx.Bucket(bucketMigrations).Put(encodeUint64(SchemaVersion), []byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}); err != nil {
		return fmt.Errorf("migrate gateway state: %w", err)
	}
	return os.Chmod(r.path, 0o600)
}

func (r *Repository) Close() error {
	r.closeOnce.Do(func() { r.closeErr = r.db.Close() })
	return r.closeErr
}

func (r *Repository) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if meta == nil || decodeUint64(meta.Get(keySchemaVersion)) != SchemaVersion {
			return errors.New("gateway schema is not ready")
		}
		if !administratorInitialized(meta) {
			return ErrBootstrapRequired
		}
		return nil
	})
}

func (r *Repository) Health(ctx context.Context) (Health, error) {
	if err := ctx.Err(); err != nil {
		return Health{}, err
	}
	health := Health{}
	err := r.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		profiles := tx.Bucket(bucketProfiles)
		if meta == nil || profiles == nil {
			return errors.New("gateway state buckets are missing")
		}
		health.SchemaVersion = int(decodeUint64(meta.Get(keySchemaVersion)))
		health.ProfileCount = profiles.Stats().KeyN
		health.AdminMode = string(meta.Get(keyAdminMode))
		health.Initialized = administratorInitialized(meta)
		health.Ready = health.SchemaVersion == SchemaVersion && health.Initialized
		return nil
	})
	return health, err
}

func (r *Repository) Profiles(ctx context.Context) ([]Profile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]Profile, 0)
	err := r.db.View(func(tx *bolt.Tx) error {
		defaults := string(tx.Bucket(bucketMeta).Get(keyDefaultProfile))
		return tx.Bucket(bucketProfiles).ForEach(func(_, value []byte) error {
			record, err := decodeProfile(value)
			if err != nil {
				return err
			}
			result = append(result, publicProfile(record, record.Name == defaults))
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list gateway profiles: %w", err)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func (r *Repository) Profile(ctx context.Context, name string) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	var profile Profile
	err := r.db.View(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, name)
		if err != nil {
			return err
		}
		profile = publicProfile(record, string(tx.Bucket(bucketMeta).Get(keyDefaultProfile)) == name)
		return nil
	})
	return profile, err
}

func (r *Repository) CreateProfile(ctx context.Context, input ProfileInput) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	input, err := normalizeProfileInput(input)
	if err != nil {
		return Profile{}, err
	}
	now := time.Now().UTC()
	id, err := randomID(16)
	if err != nil {
		return Profile{}, err
	}
	record := profileRecord{ID: id, Name: input.Name, URL: input.URL, Username: input.Username, TLSMode: input.TLSMode, CertificateFingerprint: input.CertificateFingerprint, TimeoutSeconds: input.TimeoutSeconds, CreatedAt: now, UpdatedAt: now}
	err = r.db.Update(func(tx *bolt.Tx) error {
		profiles := tx.Bucket(bucketProfiles)
		if profiles.Get([]byte(input.Name)) != nil {
			return fmt.Errorf("NAS profile %q already exists", input.Name)
		}
		if profiles.Stats().KeyN >= MaxProfiles {
			return fmt.Errorf("gateway supports at most %d NAS profiles", MaxProfiles)
		}
		record.Revision, err = nextProfileRevision(tx)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := profiles.Put([]byte(input.Name), encoded); err != nil {
			return err
		}
		meta := tx.Bucket(bucketMeta)
		if len(meta.Get(keyDefaultProfile)) == 0 {
			return meta.Put(keyDefaultProfile, []byte(input.Name))
		}
		return nil
	})
	if err != nil {
		return Profile{}, err
	}
	return r.Profile(ctx, input.Name)
}

func (r *Repository) UpdateProfile(ctx context.Context, name string, expectedRevision uint64, input ProfileInput) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	input.Name = name
	normalized, err := normalizeProfileInput(input)
	if err != nil {
		return Profile{}, err
	}
	err = r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, name)
		if err != nil {
			return err
		}
		if expectedRevision == 0 || record.Revision != expectedRevision {
			return fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, record.Revision)
		}
		changed := record.URL != normalized.URL || record.Username != normalized.Username || record.TLSMode != normalized.TLSMode || record.CertificateFingerprint != normalized.CertificateFingerprint || record.TimeoutSeconds != normalized.TimeoutSeconds
		if !changed {
			return nil
		}
		record.URL = normalized.URL
		record.Username = normalized.Username
		record.TLSMode = normalized.TLSMode
		record.CertificateFingerprint = normalized.CertificateFingerprint
		record.TimeoutSeconds = normalized.TimeoutSeconds
		record.Revision, err = nextProfileRevision(tx)
		if err != nil {
			return err
		}
		record.UpdatedAt = time.Now().UTC()
		return putProfile(tx, record)
	})
	if err != nil {
		return Profile{}, err
	}
	return r.Profile(ctx, name)
}

func (r *Repository) SetDefault(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		if _, err := readProfile(tx, name); err != nil {
			return err
		}
		return tx.Bucket(bucketMeta).Put(keyDefaultProfile, []byte(name))
	})
}

func (r *Repository) DeleteProfile(ctx context.Context, name string, expectedRevision uint64, retainSecrets bool) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	var removed Profile
	err := r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, name)
		if err != nil {
			return err
		}
		if expectedRevision == 0 || record.Revision != expectedRevision {
			return fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, record.Revision)
		}
		removed = publicProfile(record, string(tx.Bucket(bucketMeta).Get(keyDefaultProfile)) == name)
		if !retainSecrets {
			var deleteIDs [][]byte
			if err := tx.Bucket(bucketSecrets).ForEach(func(key, value []byte) error {
				var sealed sealedSecret
				if err := json.Unmarshal(value, &sealed); err != nil {
					return err
				}
				if sealed.Metadata.ProfileID == record.ID {
					deleteIDs = append(deleteIDs, append([]byte(nil), key...))
				}
				return nil
			}); err != nil {
				return err
			}
			for _, id := range deleteIDs {
				if err := tx.Bucket(bucketSecrets).Delete(id); err != nil {
					return err
				}
			}
		}
		if err := tx.Bucket(bucketProfiles).Delete([]byte(name)); err != nil {
			return err
		}
		meta := tx.Bucket(bucketMeta)
		if string(meta.Get(keyDefaultProfile)) == name {
			if err := meta.Delete(keyDefaultProfile); err != nil {
				return err
			}
		}
		return nil
	})
	return removed, err
}

// Snapshot implements the dynamic config source used by the gateway runtime.
// Every profile carries its persistent revision so cached DSM clients can be
// rejected after a committed profile or credential change.
func (r *Repository) Snapshot(ctx context.Context) (*config.Config, error) {
	profiles, err := r.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	cfg := config.New()
	for _, profile := range profiles {
		cfg.NAS[profile.Name] = config.Profile{
			URL:                    profile.URL,
			Username:               profile.Username,
			TLSMode:                profile.TLSMode,
			CertificateFingerprint: profile.CertificateFingerprint,
			TimeoutSeconds:         profile.TimeoutSeconds,
			Revision:               profile.Revision,
		}
		if profile.Default {
			cfg.DefaultNAS = profile.Name
		}
	}
	return cfg, nil
}

func (r *Repository) ConfigureBootstrap(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(token) < 32 || strings.IndexFunc(token, func(r rune) bool { return r == ' ' || r == '\n' || r == '\r' || r == '\t' }) >= 0 {
		return errors.New("bootstrap token must be at least 32 non-whitespace bytes")
	}
	digest := sha256.Sum256([]byte(token))
	return r.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if string(meta.Get(keyAdminMode)) == AdminModePlatform {
			return errors.New("generic bootstrap is disabled for platform administration")
		}
		if len(meta.Get(keyAdminDigest)) > 0 || len(meta.Get(keyBootstrapUsed)) > 0 {
			return nil
		}
		if err := meta.Put(keyAdminMode, []byte(AdminModeLocal)); err != nil {
			return err
		}
		current := meta.Get(keyBootstrapDigest)
		if len(current) > 0 && subtle.ConstantTimeCompare(current, digest[:]) != 1 {
			return errors.New("bootstrap token does not match the token that initialized this state")
		}
		if len(current) == 0 {
			return meta.Put(keyBootstrapDigest, digest[:])
		}
		return nil
	})
}

// EstablishAdministrator consumes the generic bootstrap token exactly once
// and returns a newly generated administrator bearer token. Only its SHA-256
// digest is persisted.
func (r *Repository) EstablishAdministrator(ctx context.Context, bootstrapToken string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	adminToken, err := randomToken(32)
	if err != nil {
		return "", err
	}
	bootstrapDigest := sha256.Sum256([]byte(bootstrapToken))
	adminDigest := sha256.Sum256([]byte(adminToken))
	err = r.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if string(meta.Get(keyAdminMode)) == AdminModePlatform {
			return ErrUnauthorized
		}
		if len(meta.Get(keyAdminDigest)) > 0 || len(meta.Get(keyBootstrapUsed)) > 0 {
			return ErrBootstrapConsumed
		}
		expected := meta.Get(keyBootstrapDigest)
		if len(expected) == 0 {
			return ErrBootstrapRequired
		}
		if subtle.ConstantTimeCompare(expected, bootstrapDigest[:]) != 1 {
			return ErrUnauthorized
		}
		if err := meta.Put(keyAdminDigest, adminDigest[:]); err != nil {
			return err
		}
		if err := meta.Put(keyBootstrapUsed, []byte{1}); err != nil {
			return err
		}
		return meta.Delete(keyBootstrapDigest)
	})
	if err != nil {
		return "", err
	}
	return adminToken, nil
}

func (r *Repository) AuthenticateAdministrator(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(token))
	return r.db.View(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if string(meta.Get(keyAdminMode)) == AdminModePlatform {
			return ErrUnauthorized
		}
		expected := meta.Get(keyAdminDigest)
		if len(expected) != sha256.Size || subtle.ConstantTimeCompare(expected, digest[:]) != 1 {
			return ErrUnauthorized
		}
		return nil
	})
}

func (r *Repository) RotateAdministrator(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(token))
	err = r.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		if string(meta.Get(keyAdminMode)) == AdminModePlatform {
			return ErrUnauthorized
		}
		return meta.Put(keyAdminDigest, digest[:])
	})
	return token, err
}

// EnablePlatformAdministration selects an external, signed administrator
// identity for a fresh deployment. It is deliberately irreversible through
// the runtime API so a local bootstrap secret cannot be used to downgrade a
// Synology installation after the package has initialized it.
func (r *Repository) EnablePlatformAdministration(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(bucketMeta)
		mode := string(meta.Get(keyAdminMode))
		if mode == AdminModePlatform {
			return nil
		}
		if mode != "" || len(meta.Get(keyAdminDigest)) > 0 || len(meta.Get(keyBootstrapDigest)) > 0 || len(meta.Get(keyBootstrapUsed)) > 0 {
			return errors.New("gateway state already uses local administration")
		}
		return meta.Put(keyAdminMode, []byte(AdminModePlatform))
	})
}

func administratorInitialized(meta *bolt.Bucket) bool {
	if string(meta.Get(keyAdminMode)) == AdminModePlatform {
		return true
	}
	return len(meta.Get(keyAdminDigest)) == sha256.Size
}

func readProfile(tx *bolt.Tx, name string) (profileRecord, error) {
	value := tx.Bucket(bucketProfiles).Get([]byte(name))
	if value == nil {
		return profileRecord{}, fmt.Errorf("%w: NAS profile %q", ErrNotFound, name)
	}
	return decodeProfile(value)
}

func decodeProfile(value []byte) (profileRecord, error) {
	var record profileRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return profileRecord{}, fmt.Errorf("decode gateway profile: %w", err)
	}
	return record, nil
}

func putProfile(tx *bolt.Tx, record profileRecord) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return tx.Bucket(bucketProfiles).Put([]byte(record.Name), encoded)
}

func publicProfile(record profileRecord, isDefault bool) Profile {
	return Profile{
		ID: record.ID, Name: record.Name, URL: record.URL, Username: record.Username,
		TLSMode: record.TLSMode, CertificateFingerprint: record.CertificateFingerprint,
		TimeoutSeconds: record.TimeoutSeconds, Revision: record.Revision, Default: isDefault,
		PasswordStored: record.PasswordSecretID != "", TrustedDeviceStored: record.TrustedDeviceSecretID != "", SessionStored: record.SessionSecretID != "",
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func encodeUint64(value uint64) []byte {
	encoded := make([]byte, 8)
	binary.BigEndian.PutUint64(encoded, value)
	return encoded
}

func decodeUint64(value []byte) uint64 {
	if len(value) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(value)
}

func nextProfileRevision(tx *bolt.Tx) (uint64, error) {
	meta := tx.Bucket(bucketMeta)
	next := decodeUint64(meta.Get(keyRevisionCounter)) + 1
	if err := meta.Put(keyRevisionCounter, encodeUint64(next)); err != nil {
		return 0, err
	}
	return next, nil
}

func randomID(size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (r *Repository) openEnvelope(id, secretType, profileID string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < r.aead.NonceSize() {
		return nil, errors.New("encrypted gateway value is malformed")
	}
	nonce, ciphertext := ciphertext[:r.aead.NonceSize()], ciphertext[r.aead.NonceSize():]
	aad := []byte(secretType + "\x00" + profileID + "\x00" + id)
	return r.aead.Open(nil, nonce, ciphertext, aad)
}

func (r *Repository) sealPrefixed(id, secretType, profileID string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, r.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	aad := []byte(secretType + "\x00" + profileID + "\x00" + id)
	sealed := r.aead.Seal(nil, nonce, plaintext, aad)
	return append(nonce, sealed...), nil
}
