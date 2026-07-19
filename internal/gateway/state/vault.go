package state

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/credentials"
)

const (
	secretPassword      = "password"
	secretTrustedDevice = "trusted_device"
	secretSession       = "web_login_session"
	secretApply         = "apply_secret"
)

func (r *Repository) putSecret(tx *bolt.Tx, record *profileRecord, secretType string, plaintext []byte, existingID string) (string, error) {
	id := existingID
	if id == "" {
		var err error
		id, err = randomID(16)
		if err != nil {
			return "", err
		}
	}
	now := time.Now().UTC()
	metadata := SecretMetadata{ID: id, ProfileID: record.ID, Type: secretType, CreatedAt: now, UpdatedAt: now}
	if existing := tx.Bucket(bucketSecrets).Get([]byte(id)); existing != nil {
		var old sealedSecret
		if err := json.Unmarshal(existing, &old); err != nil {
			return "", err
		}
		metadata.CreatedAt = old.Metadata.CreatedAt
	}
	nonce := make([]byte, r.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	aad := []byte(secretType + "\x00" + record.ID + "\x00" + id)
	ciphertext := r.aead.Seal(nil, nonce, plaintext, aad)
	encoded, err := json.Marshal(sealedSecret{
		Metadata:   metadata,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return "", err
	}
	if err := tx.Bucket(bucketSecrets).Put([]byte(id), encoded); err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) secret(tx *bolt.Tx, id, expectedType, expectedProfileID string) ([]byte, SecretMetadata, error) {
	value := tx.Bucket(bucketSecrets).Get([]byte(id))
	if value == nil {
		return nil, SecretMetadata{}, ErrNotFound
	}
	var sealed sealedSecret
	if err := json.Unmarshal(value, &sealed); err != nil {
		return nil, SecretMetadata{}, fmt.Errorf("decode encrypted secret: %w", err)
	}
	if expectedType != "" && sealed.Metadata.Type != expectedType {
		return nil, SecretMetadata{}, errors.New("vault secret type does not match")
	}
	if expectedProfileID != "" && sealed.Metadata.ProfileID != expectedProfileID {
		return nil, SecretMetadata{}, errors.New("vault secret profile does not match")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(sealed.Nonce)
	if err != nil || len(nonce) != r.aead.NonceSize() {
		return nil, SecretMetadata{}, errors.New("encrypted secret nonce is malformed")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(sealed.Ciphertext)
	if err != nil {
		return nil, SecretMetadata{}, errors.New("encrypted secret payload is malformed")
	}
	aad := []byte(sealed.Metadata.Type + "\x00" + sealed.Metadata.ProfileID + "\x00" + sealed.Metadata.ID)
	plaintext, err := r.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, SecretMetadata{}, errors.New("decrypt vault secret: authentication failed")
	}
	return plaintext, sealed.Metadata, nil
}

func (r *Repository) Password(ctx context.Context, profileName string, profile config.Profile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var password string
	err := r.db.View(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		if record.PasswordSecretID == "" {
			return ErrNotFound
		}
		plaintext, _, err := r.secret(tx, record.PasswordSecretID, secretPassword, record.ID)
		if err == nil {
			password = string(plaintext)
		}
		return err
	})
	if err == nil {
		return password, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", fmt.Errorf("read password for NAS %q from gateway vault: %w", profileName, err)
	}
	return r.environment.Password(ctx, profileName, profile)
}

func (r *Repository) SavePassword(ctx context.Context, profileName, password string) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if password == "" {
		return 0, errors.New("password cannot be empty")
	}
	var revision uint64
	err := r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		id, err := r.putSecret(tx, &record, secretPassword, []byte(password), record.PasswordSecretID)
		if err != nil {
			return err
		}
		record.PasswordSecretID = id
		record.Revision, err = nextProfileRevision(tx)
		if err != nil {
			return err
		}
		record.UpdatedAt = time.Now().UTC()
		revision = record.Revision
		return putProfile(tx, record)
	})
	return revision, err
}

// EnrollPassword persists a verified password and optional trusted-device
// credential in one transaction and advances the profile revision once.
func (r *Repository) EnrollPassword(ctx context.Context, profileName, password string, device credentials.TrustedDevice) (uint64, error) {
	return r.enrollPassword(ctx, profileName, 0, "", password, device)
}

// EnrollPasswordForAccount commits the verified DSM identity and credentials
// together. The expected revision closes the network-authentication race: a
// concurrent profile edit invalidates the enrollment instead of attaching a
// credential to changed connection settings.
func (r *Repository) EnrollPasswordForAccount(ctx context.Context, profileName string, expectedRevision uint64, account, password string, device credentials.TrustedDevice) (uint64, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return 0, errors.New("DSM account is required")
	}
	if expectedRevision == 0 {
		return 0, errors.New("expected profile revision is required")
	}
	return r.enrollPassword(ctx, profileName, expectedRevision, account, password, device)
}

func (r *Repository) enrollPassword(ctx context.Context, profileName string, expectedRevision uint64, account, password string, device credentials.TrustedDevice) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if password == "" {
		return 0, errors.New("password cannot be empty")
	}
	if (device.Name == "") != (device.ID == "") {
		return 0, errors.New("trusted device name and ID must be supplied together")
	}
	var devicePayload []byte
	var err error
	if device.ID != "" {
		devicePayload, err = json.Marshal(device)
		if err != nil {
			return 0, err
		}
	}
	var revision uint64
	err = r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		if expectedRevision != 0 && record.Revision != expectedRevision {
			return fmt.Errorf("%w: expected %d, current %d", ErrRevisionConflict, expectedRevision, record.Revision)
		}
		passwordID, err := r.putSecret(tx, &record, secretPassword, []byte(password), record.PasswordSecretID)
		if err != nil {
			return err
		}
		record.PasswordSecretID = passwordID
		if account != "" {
			record.Username = account
		}
		if len(devicePayload) > 0 {
			deviceID, err := r.putSecret(tx, &record, secretTrustedDevice, devicePayload, record.TrustedDeviceSecretID)
			if err != nil {
				return err
			}
			record.TrustedDeviceSecretID = deviceID
		}
		record.Revision, err = nextProfileRevision(tx)
		if err != nil {
			return err
		}
		record.UpdatedAt = time.Now().UTC()
		revision = record.Revision
		return putProfile(tx, record)
	})
	return revision, err
}

func (r *Repository) HasPassword(ctx context.Context, profileName string) (bool, error) {
	return r.hasProfileSecret(ctx, profileName, func(record profileRecord) string { return record.PasswordSecretID })
}

func (r *Repository) DeletePassword(ctx context.Context, profileName string) (bool, uint64, error) {
	return r.deleteProfileSecret(ctx, profileName, func(record *profileRecord) *string { return &record.PasswordSecretID }, true)
}

func (r *Repository) TrustedDevice(ctx context.Context, profileName string) (credentials.TrustedDevice, error) {
	if err := ctx.Err(); err != nil {
		return credentials.TrustedDevice{}, err
	}
	var device credentials.TrustedDevice
	err := r.db.View(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		if record.TrustedDeviceSecretID == "" {
			return nil
		}
		plaintext, _, err := r.secret(tx, record.TrustedDeviceSecretID, secretTrustedDevice, record.ID)
		if err != nil {
			return err
		}
		return json.Unmarshal(plaintext, &device)
	})
	return device, err
}

func (r *Repository) SaveTrustedDevice(ctx context.Context, profileName string, device credentials.TrustedDevice) error {
	_, err := r.saveTrustedDevice(ctx, profileName, device, false)
	return err
}

func (r *Repository) SaveTrustedDeviceRevision(ctx context.Context, profileName string, device credentials.TrustedDevice) (uint64, error) {
	return r.saveTrustedDevice(ctx, profileName, device, true)
}

func (r *Repository) saveTrustedDevice(ctx context.Context, profileName string, device credentials.TrustedDevice, advanceRevision bool) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if device.Name == "" || device.ID == "" {
		return 0, errors.New("trusted device name and ID are required")
	}
	plaintext, err := json.Marshal(device)
	if err != nil {
		return 0, err
	}
	var revision uint64
	err = r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		id, err := r.putSecret(tx, &record, secretTrustedDevice, plaintext, record.TrustedDeviceSecretID)
		if err != nil {
			return err
		}
		record.TrustedDeviceSecretID = id
		if advanceRevision {
			record.Revision, err = nextProfileRevision(tx)
			if err != nil {
				return err
			}
			record.UpdatedAt = time.Now().UTC()
		}
		revision = record.Revision
		return putProfile(tx, record)
	})
	return revision, err
}

func (r *Repository) HasTrustedDevice(ctx context.Context, profileName string) (bool, error) {
	return r.hasProfileSecret(ctx, profileName, func(record profileRecord) string { return record.TrustedDeviceSecretID })
}

func (r *Repository) DeleteTrustedDevice(ctx context.Context, profileName string) (bool, uint64, error) {
	return r.deleteProfileSecret(ctx, profileName, func(record *profileRecord) *string { return &record.TrustedDeviceSecretID }, true)
}

func (r *Repository) Session(ctx context.Context, profileName string) (credentials.SessionCredential, error) {
	if err := ctx.Err(); err != nil {
		return credentials.SessionCredential{}, err
	}
	var session credentials.SessionCredential
	err := r.db.View(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		if record.SessionSecretID == "" {
			return nil
		}
		plaintext, _, err := r.secret(tx, record.SessionSecretID, secretSession, record.ID)
		if err != nil {
			return err
		}
		return json.Unmarshal(plaintext, &session)
	})
	return session, err
}

// SaveSession is used by headless Noise_KK renewal. It rewrites the encrypted
// payload with a fresh nonce without advancing the profile revision.
func (r *Repository) SaveSession(ctx context.Context, profileName string, session credentials.SessionCredential) error {
	return r.saveSession(ctx, profileName, session, false)
}

// EnrollSession stores a newly established browser session and advances the
// profile revision so an existing password-backed client is evicted.
func (r *Repository) EnrollSession(ctx context.Context, profileName string, session credentials.SessionCredential) (uint64, error) {
	if err := r.saveSession(ctx, profileName, session, true); err != nil {
		return 0, err
	}
	profile, err := r.Profile(ctx, profileName)
	return profile.Revision, err
}

func (r *Repository) saveSession(ctx context.Context, profileName string, session credentials.SessionCredential, advanceRevision bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session.SID == "" && !session.CanResume() {
		return errors.New("session must carry a session ID or resume key material")
	}
	plaintext, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		id, err := r.putSecret(tx, &record, secretSession, plaintext, record.SessionSecretID)
		if err != nil {
			return err
		}
		record.SessionSecretID = id
		if advanceRevision {
			if strings.TrimSpace(session.Account) != "" {
				record.Username = strings.TrimSpace(session.Account)
			}
			record.Revision, err = nextProfileRevision(tx)
			if err != nil {
				return err
			}
			record.UpdatedAt = time.Now().UTC()
		}
		return putProfile(tx, record)
	})
}

func (r *Repository) SessionMeta(ctx context.Context, profileName string) (credentials.SessionMeta, error) {
	session, err := r.Session(ctx, profileName)
	if err != nil {
		return credentials.SessionMeta{}, err
	}
	if session.SID == "" && !session.CanResume() {
		return credentials.SessionMeta{}, nil
	}
	return credentials.SessionMeta{Present: true, Account: session.Account, IssuedAt: session.IssuedAt, ExpiresAt: session.ExpiresAt, LastVerified: session.LastVerified, CanResume: session.CanResume()}, nil
}

func (r *Repository) DeleteSession(ctx context.Context, profileName string) (bool, error) {
	removed, _, err := r.deleteProfileSecret(ctx, profileName, func(record *profileRecord) *string { return &record.SessionSecretID }, true)
	return removed, err
}

func (r *Repository) PasswordEnvironment(profileName string, profile config.Profile) (string, bool) {
	return r.environment.Status(profileName, profile)
}

// ResolveSecret accepts only opaque vault references. It is intended to be
// called by apply-time execution after plan hashing; callers must never place
// the returned value in an application result.
func (r *Repository) ResolveSecret(ctx context.Context, reference string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	reference = strings.TrimSpace(reference)
	if !strings.HasPrefix(reference, "vault:") || strings.TrimPrefix(reference, "vault:") == "" {
		return "", errors.New("credential reference must use vault:<id>")
	}
	id := strings.TrimPrefix(reference, "vault:")
	var result string
	err := r.db.View(func(tx *bolt.Tx) error {
		plaintext, _, err := r.secret(tx, id, secretApply, "")
		if err == nil {
			result = string(plaintext)
		}
		return err
	})
	return result, err
}

// StoreApplySecret creates an opaque apply-time secret reference. The value is
// not a NAS login binding, so it does not advance the profile revision.
func (r *Repository) StoreApplySecret(ctx context.Context, profileName, value string) (SecretMetadata, error) {
	if err := ctx.Err(); err != nil {
		return SecretMetadata{}, err
	}
	if value == "" {
		return SecretMetadata{}, errors.New("apply secret cannot be empty")
	}
	var metadata SecretMetadata
	err := r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		id, err := r.putSecret(tx, &record, secretApply, []byte(value), "")
		if err != nil {
			return err
		}
		var sealed sealedSecret
		if err := json.Unmarshal(tx.Bucket(bucketSecrets).Get([]byte(id)), &sealed); err != nil {
			return err
		}
		metadata = sealed.Metadata
		return nil
	})
	return metadata, err
}

func (r *Repository) DeleteApplySecret(ctx context.Context, profileID, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	removed := false
	err := r.db.Update(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketSecrets).Get([]byte(id))
		if value == nil {
			return nil
		}
		var sealed sealedSecret
		if err := json.Unmarshal(value, &sealed); err != nil {
			return err
		}
		if sealed.Metadata.ProfileID != profileID || sealed.Metadata.Type != secretApply {
			return errors.New("vault reference is not an apply secret for this NAS profile")
		}
		removed = true
		return tx.Bucket(bucketSecrets).Delete([]byte(id))
	})
	return removed, err
}

func (r *Repository) SecretMetadataForProfile(ctx context.Context, profileID string) ([]SecretMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]SecretMetadata, 0)
	err := r.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSecrets).ForEach(func(_, value []byte) error {
			var sealed sealedSecret
			if err := json.Unmarshal(value, &sealed); err != nil {
				return err
			}
			if sealed.Metadata.ProfileID == profileID {
				result = append(result, sealed.Metadata)
			}
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, err
}

func (r *Repository) OrphanedSecrets(ctx context.Context) ([]SecretMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]SecretMetadata, 0)
	err := r.db.View(func(tx *bolt.Tx) error {
		profileIDs := make(map[string]struct{})
		if err := tx.Bucket(bucketProfiles).ForEach(func(_, value []byte) error {
			record, err := decodeProfile(value)
			if err == nil {
				profileIDs[record.ID] = struct{}{}
			}
			return err
		}); err != nil {
			return err
		}
		return tx.Bucket(bucketSecrets).ForEach(func(_, value []byte) error {
			var sealed sealedSecret
			if err := json.Unmarshal(value, &sealed); err != nil {
				return err
			}
			if _, exists := profileIDs[sealed.Metadata.ProfileID]; !exists {
				result = append(result, sealed.Metadata)
			}
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, err
}

func (r *Repository) DeleteOrphanedSecret(ctx context.Context, id string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	removed := false
	err := r.db.Update(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketSecrets).Get([]byte(id))
		if value == nil {
			return nil
		}
		var sealed sealedSecret
		if err := json.Unmarshal(value, &sealed); err != nil {
			return err
		}
		bound := false
		if err := tx.Bucket(bucketProfiles).ForEach(func(_, value []byte) error {
			record, err := decodeProfile(value)
			if err == nil && record.ID == sealed.Metadata.ProfileID {
				bound = true
			}
			return err
		}); err != nil {
			return err
		}
		if bound {
			return errors.New("secret is still bound to a configured NAS profile")
		}
		removed = true
		return tx.Bucket(bucketSecrets).Delete([]byte(id))
	})
	return removed, err
}

func (r *Repository) hasProfileSecret(ctx context.Context, profileName string, selectID func(profileRecord) string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var present bool
	err := r.db.View(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		id := selectID(record)
		present = id != "" && tx.Bucket(bucketSecrets).Get([]byte(id)) != nil
		return nil
	})
	return present, err
}

func (r *Repository) deleteProfileSecret(ctx context.Context, profileName string, selectID func(*profileRecord) *string, advanceRevision bool) (bool, uint64, error) {
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	var removed bool
	var revision uint64
	err := r.db.Update(func(tx *bolt.Tx) error {
		record, err := readProfile(tx, profileName)
		if err != nil {
			return err
		}
		id := selectID(&record)
		if *id == "" {
			revision = record.Revision
			return nil
		}
		if err := tx.Bucket(bucketSecrets).Delete([]byte(*id)); err != nil {
			return err
		}
		*id = ""
		removed = true
		if advanceRevision {
			record.Revision, err = nextProfileRevision(tx)
			if err != nil {
				return err
			}
			record.UpdatedAt = time.Now().UTC()
		}
		revision = record.Revision
		return putProfile(tx, record)
	})
	return removed, revision, err
}

var _ credentials.Resolver = (*Repository)(nil)
var _ credentials.DeviceStore = (*Repository)(nil)
var _ credentials.SessionStore = (*Repository)(nil)
var _ credentials.ReferenceResolver = (*Repository)(nil)
