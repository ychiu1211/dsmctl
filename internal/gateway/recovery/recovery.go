package recovery

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	gatewaystate "github.com/derekvery666/dsmctl/internal/gateway/state"
)

const (
	requestFileName          = "recovery-restore-request.json"
	resultFileName           = "recovery-restore-result.json"
	defaultRecoveryRetention = 10
	maxMetadataSize          = int64(64 << 10)
	maxStateSize             = int64(4 << 30)
)

var (
	ErrConfirmation  = errors.New("restore confirmation does not match")
	ErrNotRestorable = errors.New("recovery set is not restorable")
	ErrPending       = errors.New("a recovery restore is already pending")

	preUpgradeNamePattern = regexp.MustCompile(`^pre-upgrade-([A-Za-z0-9][A-Za-z0-9._+-]*)-([0-9]{14})$`)
	preRestoreNamePattern = regexp.MustCompile(`^pre-restore-([0-9]{8}T[0-9]{6}\.[0-9]{9}Z)$`)
	requiredFiles         = []string{"gateway.db", "master.key", "dsm-sso.key"}
)

type Options struct {
	Root            string
	StatePath       string
	MasterKeyPath   string
	PlatformKeyPath string
	Now             func() time.Time
}

type Manager struct {
	root            string
	statePath       string
	masterKeyPath   string
	platformKeyPath string
	now             func() time.Time
	mu              sync.Mutex
}

type Backup struct {
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	SizeBytes  int64     `json:"size_bytes"`
	Complete   bool      `json:"complete"`
	Restorable bool      `json:"restorable"`
	Reason     string    `json:"reason,omitempty"`
}

type Result struct {
	Status     string    `json:"status"`
	Backup     string    `json:"backup,omitempty"`
	FinishedAt time.Time `json:"finished_at"`
	Message    string    `json:"message"`
}

type Status struct {
	Backups       []Backup `json:"backups"`
	PendingBackup string   `json:"pending_backup,omitempty"`
	LastResult    *Result  `json:"last_result,omitempty"`
}

type restoreRequest struct {
	Version  int               `json:"version"`
	Backup   string            `json:"backup"`
	QueuedAt time.Time         `json:"queued_at"`
	Hashes   map[string]string `json:"hashes"`
}

type inspectedBackup struct {
	public Backup
	hashes map[string][sha256.Size]byte
}

func New(options Options) (*Manager, error) {
	root := filepath.Clean(strings.TrimSpace(options.Root))
	statePath := filepath.Clean(strings.TrimSpace(options.StatePath))
	masterKeyPath := filepath.Clean(strings.TrimSpace(options.MasterKeyPath))
	platformKeyPath := filepath.Clean(strings.TrimSpace(options.PlatformKeyPath))
	if strings.TrimSpace(options.Root) == "" {
		return nil, errors.New("recovery root is required")
	}
	if strings.TrimSpace(options.StatePath) == "" {
		return nil, errors.New("gateway state path is required")
	}
	if strings.TrimSpace(options.MasterKeyPath) == "" {
		return nil, errors.New("master key path is required")
	}
	if strings.TrimSpace(options.PlatformKeyPath) == "" {
		return nil, errors.New("platform assertion key path is required")
	}
	if root == string(filepath.Separator) {
		return nil, errors.New("recovery root must not be the filesystem root")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Manager{
		root:            root,
		statePath:       statePath,
		masterKeyPath:   masterKeyPath,
		platformKeyPath: platformKeyPath,
		now:             now,
	}, nil
}

func (m *Manager) Status(ctx context.Context) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	status := Status{Backups: make([]Backup, 0)}
	entries, err := os.ReadDir(m.root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Status{}, fmt.Errorf("read recovery root: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "pre-upgrade-") &&
			!strings.HasPrefix(entry.Name(), "pre-restore-") {
			continue
		}
		inspected := m.inspect(entry.Name(), false)
		status.Backups = append(status.Backups, inspected.public)
	}
	sort.Slice(status.Backups, func(i, j int) bool {
		if status.Backups[i].CreatedAt.Equal(status.Backups[j].CreatedAt) {
			return status.Backups[i].Name > status.Backups[j].Name
		}
		return status.Backups[i].CreatedAt.After(status.Backups[j].CreatedAt)
	})
	if request, readErr := m.readRequest(); readErr == nil {
		status.PendingBackup = request.Backup
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Status{}, readErr
	}
	if result, readErr := m.readResult(); readErr == nil {
		status.LastResult = result
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Status{}, readErr
	}
	return status, nil
}

func (m *Manager) Queue(ctx context.Context, name, confirmation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if confirmation != "RESTORE "+name {
		return ErrConfirmation
	}
	if _, err := m.readRequest(); err == nil {
		return ErrPending
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	backup := m.inspect(name, true)
	if !backup.public.Restorable {
		if backup.public.Reason == "" {
			return ErrNotRestorable
		}
		return fmt.Errorf("%w: %s", ErrNotRestorable, backup.public.Reason)
	}
	hashes := make(map[string]string, len(requiredFiles))
	for _, fileName := range requiredFiles {
		hash := backup.hashes[fileName]
		hashes[fileName] = hex.EncodeToString(hash[:])
	}
	request := restoreRequest{
		Version:  1,
		Backup:   name,
		QueuedAt: m.now().UTC(),
		Hashes:   hashes,
	}
	return writeJSONAtomic(m.requestPath(), request)
}

// ApplyPending runs before the live state database is opened. Any validation
// failure is recorded and consumes the request while preserving the live state.
func (m *Manager) ApplyPending(ctx context.Context) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	request, err := m.readRequest()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return m.finishFailure("", fmt.Errorf("read pending restore: %w", err))
	}
	if request.Version != 1 {
		return m.finishFailure(request.Backup, errors.New("unsupported restore request version"))
	}
	backup := m.inspect(request.Backup, true)
	if !backup.public.Restorable {
		reason := backup.public.Reason
		if reason == "" {
			reason = "recovery set is not restorable"
		}
		return m.finishFailure(request.Backup, errors.New(reason))
	}
	expectedHashes := make(map[string][sha256.Size]byte, len(requiredFiles))
	for _, fileName := range requiredFiles {
		expected, err := decodeHash(request.Hashes[fileName])
		if err != nil {
			return m.finishFailure(request.Backup, fmt.Errorf("invalid recorded hash for %s", fileName))
		}
		expectedHashes[fileName] = expected
		actual := backup.hashes[fileName]
		if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			return m.finishFailure(request.Backup, fmt.Errorf("recovery file changed after confirmation: %s", fileName))
		}
	}

	masterKey, err := gatewaystate.ReadMasterKey(m.masterKeyPath)
	if err != nil {
		return m.finishFailure(request.Backup, fmt.Errorf("read active master key: %w", err))
	}
	defer zero(masterKey)

	stagePath := m.statePath + ".restore-stage"
	if err := removeRegularIfPresent(stagePath); err != nil {
		return m.finishFailure(request.Backup, fmt.Errorf("prepare staged state: %w", err))
	}
	defer func() {
		_ = removeRegularIfPresent(stagePath)
		_ = removeStagedMigrationBackups(stagePath)
	}()
	if err := copyRegularFile(filepath.Join(m.root, request.Backup, "gateway.db"), stagePath, 0o600, maxStateSize); err != nil {
		return m.finishFailure(request.Backup, fmt.Errorf("stage recovery state: %w", err))
	}
	stagedHash, _, err := hashRegularFile(stagePath, maxStateSize)
	expectedStateHash := expectedHashes["gateway.db"]
	if err != nil || subtle.ConstantTimeCompare(expectedStateHash[:], stagedHash[:]) != 1 {
		return m.finishFailure(request.Backup, errors.New("staged recovery state does not match the confirmed content"))
	}
	staged, err := gatewaystate.Open(stagePath, masterKey)
	if err != nil {
		return m.finishFailure(request.Backup, fmt.Errorf("validate staged recovery state: %w", err))
	}
	if err := staged.Close(); err != nil {
		return m.finishFailure(request.Backup, fmt.Errorf("close staged recovery state: %w", err))
	}

	if err := m.createSafetyCopy(); err != nil {
		return m.finishFailure(request.Backup, fmt.Errorf("create pre-restore safety copy: %w", err))
	}
	if err := replaceState(stagePath, m.statePath); err != nil {
		return m.finishFailure(request.Backup, fmt.Errorf("replace gateway state: %w", err))
	}
	result := &Result{
		Status:     "success",
		Backup:     request.Backup,
		FinishedAt: m.now().UTC(),
		Message:    "Recovery restore completed; administrator sessions reflect the restored state.",
	}
	if err := m.consumeRequestAndWriteResult(result); err != nil {
		return result, err
	}
	return result, nil
}

func (m *Manager) inspect(name string, hashState bool) inspectedBackup {
	result := inspectedBackup{
		public: Backup{Name: name},
		hashes: make(map[string][sha256.Size]byte, len(requiredFiles)),
	}
	version, validName := recoveryVersion(name)
	if filepath.Base(name) != name || !validName {
		result.public.Reason = "invalid recovery directory name"
		return result
	}
	result.public.Version = version
	path := filepath.Join(m.root, name)
	info, err := os.Lstat(path)
	if err != nil {
		result.public.Reason = "recovery directory is unavailable"
		return result
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		result.public.Reason = "recovery entry is not a regular directory"
		return result
	}
	// Directory mtime is an absolute filesystem timestamp. The suffix is
	// validated above but was formatted in the NAS timezone, which may differ
	// from the container timezone and must not be reinterpreted here.
	result.public.CreatedAt = info.ModTime().UTC()
	for _, fileName := range requiredFiles {
		filePath := filepath.Join(path, fileName)
		if fileName == "gateway.db" && !hashState {
			file, size, err := openRegular(filePath, fileLimit(fileName))
			if err != nil {
				result.public.Reason = "missing or unsafe recovery file: " + fileName
				return result
			}
			_ = file.Close()
			result.public.SizeBytes += size
			continue
		}
		hash, size, err := hashRegularFile(filePath, fileLimit(fileName))
		if err != nil {
			result.public.Reason = "missing or unsafe recovery file: " + fileName
			return result
		}
		if fileName != "gateway.db" && size != 32 {
			result.public.Reason = "invalid recovery key length: " + fileName
			return result
		}
		result.hashes[fileName] = hash
		result.public.SizeBytes += size
	}
	result.public.Complete = true

	activeMaster, _, err := hashRegularFile(m.masterKeyPath, 32)
	if err != nil {
		result.public.Reason = "active master key is unavailable"
		return result
	}
	activePlatform, _, err := hashRegularFile(m.platformKeyPath, 32)
	if err != nil {
		result.public.Reason = "active platform key is unavailable"
		return result
	}
	backupMaster := result.hashes["master.key"]
	if subtle.ConstantTimeCompare(activeMaster[:], backupMaster[:]) != 1 {
		result.public.Reason = "master key does not match this installation"
		return result
	}
	backupPlatform := result.hashes["dsm-sso.key"]
	if subtle.ConstantTimeCompare(activePlatform[:], backupPlatform[:]) != 1 {
		result.public.Reason = "platform key does not match this installation"
		return result
	}
	result.public.Restorable = true
	return result
}

func (m *Manager) createSafetyCopy() error {
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return err
	}
	createdAt := m.now().UTC()
	base := "pre-restore-" + createdAt.Format("20060102T150405.000000000Z")
	partial := filepath.Join(m.root, "."+base+".partial")
	final := filepath.Join(m.root, base)
	if err := os.Mkdir(partial, 0o700); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(partial)
		}
	}()
	copies := []struct {
		source string
		name   string
		limit  int64
	}{
		{m.statePath, "gateway.db", maxStateSize},
		{m.masterKeyPath, "master.key", 32},
		{m.platformKeyPath, "dsm-sso.key", 32},
	}
	for _, item := range copies {
		if err := copyRegularFile(item.source, filepath.Join(partial, item.name), 0o600, item.limit); err != nil {
			return err
		}
	}
	if err := os.Rename(partial, final); err != nil {
		return err
	}
	complete = true
	if err := os.Chtimes(final, createdAt, createdAt); err != nil {
		return err
	}
	return m.pruneRecoverySets(defaultRecoveryRetention)
}

func recoveryVersion(name string) (string, bool) {
	if matches := preUpgradeNamePattern.FindStringSubmatch(name); len(matches) == 3 {
		return matches[1], true
	}
	if preRestoreNamePattern.MatchString(name) {
		return "pre-restore", true
	}
	return "", false
}

func (m *Manager) pruneRecoverySets(keep int) error {
	if keep < 1 {
		return errors.New("recovery retention must be positive")
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	complete := make([]Backup, 0, len(entries))
	incomplete := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if _, valid := recoveryVersion(name); !valid {
			continue
		}
		inspected := m.inspect(name, false).public
		if inspected.Complete {
			complete = append(complete, inspected)
			continue
		}
		info, statErr := os.Lstat(filepath.Join(m.root, name))
		if statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			incomplete = append(incomplete, name)
		}
	}
	sort.Slice(complete, func(i, j int) bool {
		if complete[i].CreatedAt.Equal(complete[j].CreatedAt) {
			return complete[i].Name > complete[j].Name
		}
		return complete[i].CreatedAt.After(complete[j].CreatedAt)
	})
	if len(complete) > keep {
		for _, backup := range complete[keep:] {
			if err := m.removeRecoveryDirectory(backup.Name); err != nil {
				return err
			}
		}
	}
	if len(complete) >= keep {
		for _, name := range incomplete {
			if err := m.removeRecoveryDirectory(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) removeRecoveryDirectory(name string) error {
	if filepath.Base(name) != name {
		return errors.New("refusing to remove a non-child recovery path")
	}
	if _, valid := recoveryVersion(name); !valid {
		return errors.New("refusing to remove an invalid recovery directory")
	}
	path := filepath.Join(m.root, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("refusing to remove a non-directory recovery entry")
	}
	return os.RemoveAll(path)
}

func (m *Manager) finishFailure(backup string, cause error) (*Result, error) {
	result := &Result{
		Status:     "failure",
		Backup:     backup,
		FinishedAt: m.now().UTC(),
		Message:    safeFailureMessage(cause),
	}
	if err := m.consumeRequestAndWriteResult(result); err != nil {
		return result, errors.Join(cause, err)
	}
	return result, nil
}

func (m *Manager) consumeRequestAndWriteResult(result *Result) error {
	if err := os.Remove(m.requestPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove restore request: %w", err)
	}
	if err := writeJSONAtomic(m.resultPath(), result); err != nil {
		return fmt.Errorf("write restore result: %w", err)
	}
	return nil
}

func (m *Manager) readRequest() (*restoreRequest, error) {
	var request restoreRequest
	if err := readJSONFile(m.requestPath(), &request); err != nil {
		return nil, err
	}
	return &request, nil
}

func (m *Manager) readResult() (*Result, error) {
	var result Result
	if err := readJSONFile(m.resultPath(), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (m *Manager) requestPath() string {
	return filepath.Join(filepath.Dir(m.statePath), requestFileName)
}

func (m *Manager) resultPath() string {
	return filepath.Join(filepath.Dir(m.statePath), resultFileName)
}

func hashRegularFile(path string, limit int64) ([sha256.Size]byte, int64, error) {
	var empty [sha256.Size]byte
	file, size, err := openRegular(path, limit)
	if err != nil {
		return empty, 0, err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, file)
	if err != nil {
		return empty, 0, err
	}
	if written != size {
		return empty, 0, errors.New("recovery file changed while reading")
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, size, nil
}

func copyRegularFile(source, destination string, mode os.FileMode, limit int64) error {
	input, size, err := openRegular(source, limit)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	copied, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || copied != size {
		_ = os.Remove(destination)
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		return errors.New("recovery file changed while copying")
	}
	return nil
}

func openRegular(path string, limit int64) (*os.File, int64, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, 0, errors.New("path is not a regular file")
	}
	if before.Size() < 1 || before.Size() > limit {
		return nil, 0, errors.New("file size is outside the recovery limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		_ = file.Close()
		return nil, 0, errors.New("recovery file changed while opening")
	}
	return file, after.Size(), nil
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(path)
		if retryErr := os.Rename(tempPath, path); retryErr != nil {
			return errors.Join(err, retryErr)
		}
	}
	committed = true
	return nil
}

func readJSONFile(path string, target any) error {
	file, size, err := openRegular(path, maxMetadataSize)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, size))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("metadata contains trailing JSON")
	}
	return nil
}

func removeRegularIfPresent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("refusing to remove a non-regular staging path")
	}
	return os.Remove(path)
}

func removeStagedMigrationBackups(stagePath string) error {
	matches, err := filepath.Glob(stagePath + ".pre-v*.bak")
	if err != nil {
		return err
	}
	var result error
	for _, path := range matches {
		if !strings.HasPrefix(filepath.Base(path), filepath.Base(stagePath)+".pre-v") {
			result = errors.Join(result, errors.New("unexpected staged migration backup path"))
			continue
		}
		result = errors.Join(result, removeRegularIfPresent(path))
	}
	return result
}

func replaceState(staged, destination string) error {
	if err := os.Rename(staged, destination); err == nil {
		return nil
	}
	displaced := destination + ".restore-displaced"
	if _, err := os.Lstat(displaced); err == nil {
		return errors.New("stale displaced state file requires manual inspection")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(destination, displaced); err != nil {
		return err
	}
	if err := os.Rename(staged, destination); err != nil {
		rollbackErr := os.Rename(displaced, destination)
		return errors.Join(err, rollbackErr)
	}
	if err := os.Remove(displaced); err != nil {
		return fmt.Errorf("remove displaced state after successful replacement: %w", err)
	}
	return nil
}

func decodeHash(value string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return result, errors.New("invalid SHA-256 digest")
	}
	copy(result[:], decoded)
	return result, nil
}

func fileLimit(name string) int64 {
	if name == "gateway.db" {
		return maxStateSize
	}
	return 32
}

func safeFailureMessage(err error) string {
	message := strings.TrimSpace(err.Error())
	for _, forbidden := range []string{"master.key", "dsm-sso.key"} {
		message = strings.ReplaceAll(message, forbidden, "recovery key")
	}
	if len(message) > 240 {
		message = message[:240]
	}
	return message
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
