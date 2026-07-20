package synology

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	pkgops "github.com/ychiu1211/dsmctl/internal/synology/operations/packagecenter"
)

type PackageState = packagecenter.State
type PackageSettings = packagecenter.Settings
type PackageCapabilities = packagecenter.Capabilities
type PackageChangeRequest = packagecenter.ChangeRequest
type PackageSettingsChange = packagecenter.SettingsChange
type PackageLifecycleChange = packagecenter.LifecycleChange
type PackageMutationResult = pkgops.MutationResult
type PackageCatalog = packagecenter.Catalog

// PackageCatalog reads the online package server's Synology-published catalog
// and cross-references the installed inventory so each offered package is marked
// installed and, when the offered version differs from the installed one, as
// having an available update.
func (c *Client) PackageCatalog(ctx context.Context) (PackageCatalog, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.ServerAPIName, pkgops.InventoryAPIName); err != nil {
		return PackageCatalog{}, fmt.Errorf("prepare Package Center catalog target: %w", err)
	}
	catalog, _, err := pkgops.ExecuteCatalog(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return PackageCatalog{}, fmt.Errorf("get Package Center catalog: %w", err)
	}
	c.target.AddCapability(pkgops.CatalogCapabilityName)

	// Best-effort inventory cross-reference; a missing inventory just leaves the
	// installed/update flags at their defaults.
	if state, _, invErr := pkgops.ExecuteInventory(ctx, c.target, lockedExecutor{client: c}); invErr == nil {
		installed := make(map[string]string, len(state.Packages))
		for _, pkg := range state.Packages {
			installed[pkg.ID] = pkg.Version
		}
		for i := range catalog.Packages {
			version, ok := installed[catalog.Packages[i].ID]
			if !ok {
				continue
			}
			catalog.Packages[i].Installed = true
			// The online catalog offers the latest version, so a different
			// installed version means an update is available.
			catalog.Packages[i].UpdateAvailable = version != "" && catalog.Packages[i].Version != "" && version != catalog.Packages[i].Version
		}
	}
	return catalog, nil
}

// PackageState reads the installed-package inventory without requiring any other
// Package Center operation to be supported.
func (c *Client) PackageState(ctx context.Context) (PackageState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.InventoryAPIName); err != nil {
		return PackageState{}, fmt.Errorf("prepare Package Center inventory target: %w", err)
	}
	state, _, err := pkgops.ExecuteInventory(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return PackageState{}, fmt.Errorf("get Package Center inventory: %w", err)
	}
	c.target.AddCapability(pkgops.InventoryCapabilityName)
	return state, nil
}

// PackageSettings reads the global Package Center configuration.
func (c *Client) PackageSettings(ctx context.Context) (PackageSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.SettingAPIName); err != nil {
		return PackageSettings{}, fmt.Errorf("prepare Package Center settings target: %w", err)
	}
	settings, _, err := pkgops.ExecuteSettingsRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return PackageSettings{}, fmt.Errorf("get Package Center settings: %w", err)
	}
	c.target.AddCapability(pkgops.SettingsReadCapabilityName)
	return settings, nil
}

// PackageCapabilities reports each Package Center operation's selection. A
// missing API makes only the affected operation unsupported.
func (c *Client) PackageCapabilities(ctx context.Context) (PackageCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.APINames()...); err != nil {
		return PackageCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Package Center capabilities target: %w", err)
	}
	selections, err := pkgops.Select(c.target)
	if err != nil {
		return PackageCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Package Center backends: %w", err)
	}
	c.addPackageCapabilitiesLocked(selections)
	capabilities := packageCapabilitiesFromSelections(selections)
	return capabilities, c.target.Report(selections...), nil
}

// ApplyPackageSettingsChange submits the complete desired settings. The caller
// merges the patch into a freshly read full state so no unspecified field is
// reset.
func (c *Client) ApplyPackageSettingsChange(ctx context.Context, desired PackageSettings) (PackageMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.SettingAPIName); err != nil {
		return PackageMutationResult{}, fmt.Errorf("prepare Package Center settings mutation target: %w", err)
	}
	result, _, err := pkgops.ExecuteSettingsSet(ctx, c.target, lockedExecutor{client: c}, desired)
	if err != nil {
		return PackageMutationResult{}, fmt.Errorf("apply Package Center settings: %w", err)
	}
	return result, nil
}

// ApplyPackageLifecycleChange starts, stops, or uninstalls one package.
func (c *Client) ApplyPackageLifecycleChange(ctx context.Context, change PackageLifecycleChange) (PackageMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.APINames()...); err != nil {
		return PackageMutationResult{}, fmt.Errorf("prepare Package Center lifecycle target: %w", err)
	}
	switch change.Action {
	case packagecenter.ActionStart, packagecenter.ActionStop:
		result, _, err := pkgops.ExecuteControl(ctx, c.target, lockedExecutor{client: c}, pkgops.ControlInput{Action: change.Action, PackageID: change.PackageID})
		if err != nil {
			return PackageMutationResult{}, fmt.Errorf("apply Package Center %s: %w", change.Action, err)
		}
		return result, nil
	case packagecenter.ActionUninstall:
		result, _, err := pkgops.ExecuteUninstall(ctx, c.target, lockedExecutor{client: c}, pkgops.UninstallInput{PackageID: change.PackageID})
		if err != nil {
			return PackageMutationResult{}, fmt.Errorf("apply Package Center uninstall: %w", err)
		}
		return result, nil
	default:
		return PackageMutationResult{}, fmt.Errorf("unsupported package lifecycle action %q", change.Action)
	}
}

// PackageInstallInput carries catalog-resolved download metadata plus install
// options for a guarded online install.
type PackageInstallInput struct {
	Name            string
	URL             string
	Checksum        string
	Filesize        int64
	Beta            bool
	QuickInstall    bool
	VolumePath      string
	RunAfterInstall bool
	// ExpectVersion, when set, makes the inventory confirmation require this
	// exact installed version. An update targets a package that is already in
	// the inventory, so plain presence would confirm instantly against the old
	// version.
	ExpectVersion string
}

// PackageInstallResult reports the outcome of a completed install.
type PackageInstallResult struct {
	PackageID string `json:"package_id" jsonschema:"Installed package identifier"`
	TaskID    string `json:"task_id,omitempty" jsonschema:"DSM install task identifier"`
	Installed bool   `json:"installed" jsonschema:"Whether the package is installed after the operation"`
	Version   string `json:"version,omitempty" jsonschema:"Installed version confirmed by inventory"`
}

// PackageInstall starts the online download+install task for one package and
// polls until DSM reports the task finished, then confirms via inventory. It is
// the callers' responsibility to have resolved the download metadata from the
// catalog and to have confirmed the package is not already installed.
func (c *Client) PackageInstall(ctx context.Context, input PackageInstallInput) (PackageInstallResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.InstallationAPIName, pkgops.InventoryAPIName); err != nil {
		return PackageInstallResult{}, fmt.Errorf("prepare Package Center install target: %w", err)
	}
	task, _, err := pkgops.ExecuteInstallDownload(ctx, c.target, lockedExecutor{client: c}, pkgops.InstallInput{
		Name: input.Name, URL: input.URL, Checksum: input.Checksum, Filesize: input.Filesize,
		Beta: input.Beta, QuickInstall: input.QuickInstall, VolumePath: input.VolumePath, RunAfterInstall: input.RunAfterInstall,
	})
	if err != nil {
		return PackageInstallResult{}, fmt.Errorf("start install of %s: %w", input.Name, err)
	}
	result := PackageInstallResult{PackageID: input.Name, TaskID: task.TaskID}
	return c.awaitPackageInstalledLocked(ctx, input.Name, input.ExpectVersion, result)
}

// PackageLocalInstallInput carries a local .spk's bytes plus manual-install
// options. The caller reads and validates the file; this method uploads it.
type PackageLocalInstallInput struct {
	FileName        string
	Data            []byte
	VolumePath      string
	RunAfterInstall bool
	// AllowUnsigned disables DSM's code-signature enforcement, permitting a
	// package that is not signed by Synology (or a trusted publisher) to install.
	AllowUnsigned bool
}

// PackageInstallLocal uploads a local package file and installs (or upgrades, if
// the uploaded package is already installed) it, then polls until DSM reports the
// task finished and confirms via the inventory. The upload's parsed INFO supplies
// the package id used to confirm the install.
func (c *Client) PackageInstallLocal(ctx context.Context, input PackageLocalInstallInput) (PackageInstallResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(input.Data) == 0 {
		return PackageInstallResult{}, fmt.Errorf("local install requires package file bytes")
	}
	if err := c.prepareCompatibilityTargetLocked(ctx, pkgops.InstallationAPIName, pkgops.InventoryAPIName); err != nil {
		return PackageInstallResult{}, fmt.Errorf("prepare Package Center local install target: %w", err)
	}
	executor := lockedExecutor{client: c}

	// 1) Upload the .spk. DSM extracts it to a temp location, parses its INFO,
	// and returns a temp reference (task_id/filename) plus the package identity.
	// The upload reuses the streaming multipart transport that backs the
	// FileStation upload rather than a bespoke one.
	uploadData, err := c.uploadPackageLocked(ctx, pkgops.UploadMethod, input.FileName, input.Data)
	if err != nil {
		return PackageInstallResult{}, fmt.Errorf("upload local package %s: %w", input.FileName, err)
	}
	upload, err := pkgops.DecodeUploadResult(uploadData)
	if err != nil {
		return PackageInstallResult{}, err
	}
	if upload.PackageID == "" {
		_ = pkgops.ExecuteUploadCleanup(ctx, c.target, executor, upload.TaskID, upload.FileName)
		return PackageInstallResult{}, fmt.Errorf("upload of %s did not report a package id; the file may not be a valid .spk", input.FileName)
	}

	// Determine whether this is an upgrade so DSM's install vs upgrade method is
	// chosen correctly. When it is an upgrade the confirmation must require the
	// uploaded package's version, since the old version is already present in the
	// inventory and would otherwise confirm instantly.
	upgrade := false
	expectVersion := ""
	if state, _, invErr := pkgops.ExecuteInventory(ctx, c.target, executor); invErr == nil {
		for _, pkg := range state.Packages {
			if pkg.ID == upload.PackageID {
				upgrade = true
				expectVersion = upload.Version
				break
			}
		}
	}

	// 2) Install from the uploaded file.
	task, _, err := pkgops.ExecuteLocalInstall(ctx, c.target, executor, pkgops.LocalInstallInput{
		VolumePath: input.VolumePath, TaskID: upload.TaskID, Path: upload.FileName,
		CheckCodesign: !input.AllowUnsigned, RunAfterInstall: input.RunAfterInstall, Upgrade: upgrade,
	})
	if err != nil {
		// Best-effort cleanup of the uploaded temp package so a failed install
		// does not leak the upload.
		_ = pkgops.ExecuteUploadCleanup(ctx, c.target, executor, upload.TaskID, upload.FileName)
		return PackageInstallResult{}, fmt.Errorf("install local package %s: %w", upload.PackageID, err)
	}

	// 3) Poll status + confirm via inventory (same machinery as the online
	// install), keying on the id parsed from the uploaded package's INFO.
	result := PackageInstallResult{PackageID: upload.PackageID, TaskID: task.TaskID}
	return c.awaitPackageInstalledLocked(ctx, upload.PackageID, expectVersion, result)
}

// uploadPackageLocked POSTs a local .spk to SYNO.Core.Package.Installation's
// upload method and returns the raw envelope data for the caller to decode. It
// runs under the client mutex (like the other package methods) and reuses the
// shared streaming multipart transport that backs the FileStation upload rather
// than a bespoke helper. The package bytes are held in memory (the caller has
// already read and hashed the file), so a single retry after a session refresh
// replays them from a fresh reader.
func (c *Client) uploadPackageLocked(ctx context.Context, method, fileName string, data []byte) (json.RawMessage, error) {
	if err := c.ensureAPIsLocked(ctx, pkgops.InstallationAPIName); err != nil {
		return nil, err
	}
	if err := c.loginLocked(ctx); err != nil {
		return nil, err
	}
	info, ok := c.target.API(pkgops.InstallationAPIName)
	if !ok {
		return nil, fmt.Errorf("Synology API %s is not available on this NAS", pkgops.InstallationAPIName)
	}
	const version = 1
	if !info.Supports(version) {
		return nil, fmt.Errorf("Synology API %s does not support requested version %d (available %d-%d)", pkgops.InstallationAPIName, version, info.MinVersion, info.MaxVersion)
	}
	// A local upload can be large; streamingClient drops the fixed per-request
	// timeout so the transfer is bounded only by the caller's context.
	prep := transferPrep{endpoint: *c.baseURL, apiPath: info.Path, version: version, client: c.streamingClient()}

	fields := func(sid, synoToken string) []multipartField {
		f := []multipartField{
			{"api", pkgops.InstallationAPIName},
			{"version", strconv.Itoa(version)},
			{"method", method},
			// additional makes the upload parse and return the .spk INFO keys, so
			// the response carries the package identity (id/name/version).
			{pkgops.UploadAdditionalField, pkgops.UploadAdditionalValue},
			{"_sid", sid},
		}
		if synoToken != "" {
			f = append(f, multipartField{"SynoToken", synoToken})
		}
		return f
	}

	raw, err := doMultipartUpload(ctx, prep, c.sid, c.synoToken, pkgops.InstallationAPIName, method, fields(c.sid, c.synoToken), pkgops.UploadFileField, fileName, bytes.NewReader(data), int64(len(data)))
	if isSessionError(err) {
		c.sid = ""
		c.synoToken = ""
		if reErr := c.reestablishLocked(ctx); reErr != nil {
			if IsSessionExpired(reErr) {
				return nil, reErr
			}
			return nil, &SessionExpiredError{Cause: reErr}
		}
		return doMultipartUpload(ctx, prep, c.sid, c.synoToken, pkgops.InstallationAPIName, method, fields(c.sid, c.synoToken), pkgops.UploadFileField, fileName, bytes.NewReader(data), int64(len(data)))
	}
	return raw, err
}

// awaitPackageInstalledLocked polls an in-flight install task and confirms
// completion via the inventory. The download/install phases run asynchronously
// and the task id can change between them, so success is confirmed by the
// inventory rather than a single task's finished flag. The status poll is
// best-effort: it surfaces an explicit task error fast, but a status call that
// errors (for example the task was cleared) is not itself fatal. Large packages
// can take a few minutes; the wait is capped and a timeout is reported.
//
// expectVersion, when set, makes the inventory confirmation require that exact
// installed version. An update or a local upgrade targets a package that is
// already in the inventory, so plain presence would confirm instantly against
// the old version; a fresh install passes "" and confirms on presence.
func (c *Client) awaitPackageInstalledLocked(ctx context.Context, packageID, expectVersion string, result PackageInstallResult) (PackageInstallResult, error) {
	executor := lockedExecutor{client: c}
	deadline := time.Now().Add(30 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		if progress, statusErr := pkgops.ExecuteInstallStatus(ctx, c.target, executor, result.TaskID); statusErr == nil {
			if _, taskErr := installTaskDone(progress, packageID); taskErr != "" {
				return result, fmt.Errorf("install of %s failed: %s", packageID, taskErr)
			}
		}
		state, _, invErr := pkgops.ExecuteInventory(ctx, c.target, executor)
		if invErr != nil {
			return result, fmt.Errorf("verify install of %s: %w", packageID, invErr)
		}
		for _, pkg := range state.Packages {
			if pkg.ID == packageID && (expectVersion == "" || pkg.Version == expectVersion) {
				result.Installed = true
				result.Version = pkg.Version
				break
			}
		}
		if result.Installed {
			return result, nil
		}
		if time.Now().After(deadline) {
			if expectVersion != "" {
				return result, fmt.Errorf("install of %s did not reach version %s in the inventory within the timeout", packageID, expectVersion)
			}
			return result, fmt.Errorf("install of %s did not appear in the inventory within the timeout", packageID)
		}
		if err := sleepContext(ctx, 5*time.Second); err != nil {
			return result, err
		}
	}
}

// installTaskDone reports whether the task for packageID finished, and returns a
// non-empty error string if it failed. When no task is present anymore, the
// operation is treated as finished (DSM clears completed tasks).
func installTaskDone(progress []pkgops.TaskProgress, packageID string) (bool, string) {
	found := false
	for _, task := range progress {
		if task.PackageID != "" && task.PackageID != packageID {
			continue
		}
		found = true
		if task.Error != "" {
			return false, task.Error
		}
		if task.Finished {
			return true, ""
		}
	}
	// No matching in-flight task: DSM cleared it, so treat as finished and let the
	// inventory check confirm.
	if !found {
		return true, ""
	}
	return false, ""
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func packageCapabilitiesFromSelections(selections []compatibility.Selection) PackageCapabilities {
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	return PackageCapabilities{
		Module:        packagecenter.ModuleName,
		InventoryRead: supported(0),
		SettingsRead:  supported(1),
		SettingsSet:   supported(2),
		Start:         supported(3),
		Stop:          supported(3),
		Uninstall:     supported(4),
		Install:       supported(5),
		Update:        supported(6),
	}
}

func (c *Client) addPackageCapabilitiesLocked(selections []compatibility.Selection) {
	names := []string{
		pkgops.InventoryCapabilityName,
		pkgops.SettingsReadCapabilityName,
		pkgops.SettingsSetCapabilityName,
		pkgops.ControlCapabilityName,
		pkgops.UninstallCapabilityName,
		pkgops.InstallCapabilityName,
		pkgops.UpdateCapabilityName,
	}
	for index, name := range names {
		if index < len(selections) && selections[index].Supported {
			c.target.AddCapability(name)
		}
	}
}
