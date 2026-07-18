package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const packageCenterAPIVersion = "dsmctl.io/v1alpha1"

type PackageStateResult struct {
	NAS   string                `json:"nas" jsonschema:"NAS profile used for the request"`
	State synology.PackageState `json:"state" jsonschema:"Normalized installed-package inventory"`
}

type PackageSettingsResult struct {
	NAS      string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.PackageSettings `json:"settings" jsonschema:"Normalized global Package Center settings"`
}

type PackageCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.PackageCapabilities `json:"capabilities" jsonschema:"Package Center operations currently exposed by dsmctl"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected Package Center backends"`
}

// PackageObservedState binds a plan to exactly the state its change touches: the
// complete settings for a settings change, or the single target package for a
// lifecycle change.
type PackageObservedState struct {
	Settings *packagecenter.Settings `json:"settings,omitempty" jsonschema:"Complete settings observed during planning for a settings change"`
	Package  *packagecenter.Package  `json:"package,omitempty" jsonschema:"Target package observed during planning for a lifecycle change"`
}

type PackagePlan struct {
	APIVersion          string                      `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                      `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64                      `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             packagecenter.ChangeRequest `json:"request" jsonschema:"Validated settings or lifecycle intent"`
	Observed            PackageObservedState        `json:"observed" jsonschema:"State observed during planning that must still match at apply"`
	ObservedFingerprint string                      `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the observed state"`
	Destructive         bool                        `json:"destructive" jsonschema:"Whether the plan removes a package or its data"`
	Risk                string                      `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                    `json:"warnings" jsonschema:"Service-disruption and security warnings"`
	Summary             []string                    `json:"summary" jsonschema:"Human-readable operations the plan will perform"`
	Hash                string                      `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed state"`
}

type PackageApplyResult struct {
	NAS       string                         `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                         `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                           `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.PackageMutationResult `json:"operation" jsonschema:"Selected DSM mutation backend"`
}

type packageClient interface {
	PackageState(context.Context) (synology.PackageState, error)
	PackageSettings(context.Context) (synology.PackageSettings, error)
	PackageCatalog(context.Context) (synology.PackageCatalog, error)
	PackageInstall(context.Context, synology.PackageInstallInput) (synology.PackageInstallResult, error)
	PackageCapabilities(context.Context) (synology.PackageCapabilities, synology.CompatibilityReport, error)
	ApplyPackageSettingsChange(context.Context, synology.PackageSettings) (synology.PackageMutationResult, error)
	ApplyPackageLifecycleChange(context.Context, synology.PackageLifecycleChange) (synology.PackageMutationResult, error)
}

// PackageCatalogResult is the online-catalog read result.
type PackageCatalogResult struct {
	NAS     string                 `json:"nas" jsonschema:"NAS profile used for the request"`
	Catalog synology.PackageCatalog `json:"catalog" jsonschema:"Packages offered by the online package server"`
}

func (s *Service) GetPackageCatalog(ctx context.Context, requestedNAS string) (PackageCatalogResult, error) {
	name, client, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackageCatalogResult{}, err
	}
	catalog, err := client.PackageCatalog(ctx)
	if err != nil {
		return PackageCatalogResult{}, authenticationError(name, err)
	}
	return PackageCatalogResult{NAS: name, Catalog: catalog}, nil
}

const packageInstallAPIVersion = "dsmctl.io/v1alpha1"

// PackageInstallPlan is a hash-bound install intent resolved against the online
// catalog and the current inventory.
type PackageInstallPlan struct {
	APIVersion      string `json:"api_version" jsonschema:"Plan schema version"`
	NAS             string `json:"nas" jsonschema:"NAS profile selected during planning"`
	PackageID       string `json:"package_id" jsonschema:"Package identifier to install"`
	Name            string `json:"name" jsonschema:"Human-readable package name"`
	Version         string `json:"version" jsonschema:"Offered version to install"`
	Size            int64  `json:"size" jsonschema:"Download size in bytes"`
	DownloadLink    string `json:"download_link" jsonschema:"Resolved download URL"`
	Checksum        string `json:"checksum" jsonschema:"Resolved package checksum (md5)"`
	Beta            bool   `json:"beta" jsonschema:"Whether the offered build is a beta"`
	QuickInstall    bool   `json:"quick_install" jsonschema:"Whether quick install (no wizard) is used"`
	VolumePath      string `json:"volume_path" jsonschema:"Target install volume path"`
	RunAfterInstall bool   `json:"run_after_install" jsonschema:"Whether the package starts after install"`
	Risk            string `json:"risk" jsonschema:"Plan risk level"`
	Warnings        []string `json:"warnings" jsonschema:"Install warnings"`
	Summary         []string `json:"summary" jsonschema:"Human-readable operations"`
	Hash            string `json:"hash" jsonschema:"SHA-256 approval hash covering the resolved install intent"`
}

// PackageInstallApplyResult reports the outcome of a completed install.
type PackageInstallApplyResult struct {
	NAS      string                          `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                          `json:"plan_hash" jsonschema:"Approved plan hash"`
	Result   synology.PackageInstallResult   `json:"result" jsonschema:"Install outcome confirmed by inventory"`
}

func (s *Service) PlanPackageInstall(ctx context.Context, requestedNAS, packageID, volumePath string, runAfterInstall, quickInstall bool) (PackageInstallPlan, error) {
	if strings.TrimSpace(packageID) == "" {
		return PackageInstallPlan{}, fmt.Errorf("install requires a package id")
	}
	if strings.TrimSpace(volumePath) == "" {
		return PackageInstallPlan{}, fmt.Errorf("install requires a target volume path")
	}
	name, client, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackageInstallPlan{}, err
	}
	return planPackageInstallWithClient(ctx, name, client, packageID, volumePath, runAfterInstall, quickInstall)
}

func planPackageInstallWithClient(ctx context.Context, nas string, client packageClient, packageID, volumePath string, runAfterInstall, quickInstall bool) (PackageInstallPlan, error) {
	state, err := client.PackageState(ctx)
	if err != nil {
		return PackageInstallPlan{}, authenticationError(nas, err)
	}
	for _, pkg := range state.Packages {
		if pkg.ID == packageID {
			return PackageInstallPlan{}, fmt.Errorf("package %q is already installed", packageID)
		}
	}
	catalog, err := client.PackageCatalog(ctx)
	if err != nil {
		return PackageInstallPlan{}, authenticationError(nas, err)
	}
	var offered *packagecenter.AvailablePackage
	for i := range catalog.Packages {
		if catalog.Packages[i].ID == packageID {
			offered = &catalog.Packages[i]
			break
		}
	}
	if offered == nil {
		return PackageInstallPlan{}, fmt.Errorf("package %q is not offered by the online package server", packageID)
	}
	if offered.DownloadLink == "" {
		return PackageInstallPlan{}, fmt.Errorf("package %q has no download link in the catalog", packageID)
	}
	plan := PackageInstallPlan{
		APIVersion: packageInstallAPIVersion, NAS: nas, PackageID: offered.ID, Name: offered.Name,
		Version: offered.Version, Size: offered.Size, DownloadLink: offered.DownloadLink, Checksum: offered.Checksum,
		Beta: offered.Beta, QuickInstall: offered.QuickInstall || quickInstall, VolumePath: volumePath, RunAfterInstall: runAfterInstall,
		Risk: "high",
		Warnings: []string{
			"installing downloads and runs third-party software on the NAS",
		},
		Summary: []string{fmt.Sprintf("install %s %s to %s", offered.ID, offered.Version, volumePath)},
	}
	if offered.Beta {
		plan.Warnings = append(plan.Warnings, "the offered build is a beta version")
	}
	plan.Hash, err = packageInstallPlanHash(plan)
	if err != nil {
		return PackageInstallPlan{}, err
	}
	return plan, nil
}

func (s *Service) ApplyPackageInstallPlan(ctx context.Context, plan PackageInstallPlan, approvalHash string) (PackageInstallApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return PackageInstallApplyResult{}, fmt.Errorf("approval hash does not match the install plan")
	}
	if plan.APIVersion != packageInstallAPIVersion || strings.TrimSpace(plan.NAS) == "" || strings.TrimSpace(plan.PackageID) == "" {
		return PackageInstallApplyResult{}, fmt.Errorf("invalid install plan metadata")
	}
	expectedHash, err := packageInstallPlanHash(plan)
	if err != nil {
		return PackageInstallApplyResult{}, err
	}
	if expectedHash != plan.Hash {
		return PackageInstallApplyResult{}, fmt.Errorf("install plan contents were modified after planning")
	}
	name, client, err := s.packageClient(ctx, plan.NAS)
	if err != nil {
		return PackageInstallApplyResult{}, err
	}
	if name != plan.NAS {
		return PackageInstallApplyResult{}, fmt.Errorf("install plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	result, err := client.PackageInstall(ctx, synology.PackageInstallInput{
		Name: plan.PackageID, URL: plan.DownloadLink, Checksum: plan.Checksum, Filesize: plan.Size,
		Beta: plan.Beta, QuickInstall: plan.QuickInstall, VolumePath: plan.VolumePath, RunAfterInstall: plan.RunAfterInstall,
	})
	if err != nil {
		return PackageInstallApplyResult{}, authenticationError(plan.NAS, err)
	}
	return PackageInstallApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Result: result}, nil
}

func packageInstallPlanHash(plan PackageInstallPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func (s *Service) GetPackageState(ctx context.Context, requestedNAS string) (PackageStateResult, error) {
	name, client, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackageStateResult{}, err
	}
	state, err := client.PackageState(ctx)
	if err != nil {
		return PackageStateResult{}, authenticationError(name, err)
	}
	return PackageStateResult{NAS: name, State: state}, nil
}

func (s *Service) GetPackageSettings(ctx context.Context, requestedNAS string) (PackageSettingsResult, error) {
	name, client, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackageSettingsResult{}, err
	}
	settings, err := client.PackageSettings(ctx)
	if err != nil {
		return PackageSettingsResult{}, authenticationError(name, err)
	}
	return PackageSettingsResult{NAS: name, Settings: settings}, nil
}

func (s *Service) GetPackageCapabilities(ctx context.Context, requestedNAS string) (PackageCapabilitiesResult, error) {
	name, client, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackageCapabilitiesResult{}, err
	}
	capabilities, report, err := client.PackageCapabilities(ctx)
	if err != nil {
		return PackageCapabilitiesResult{}, authenticationError(name, err)
	}
	return PackageCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanPackageChange(ctx context.Context, requestedNAS string, request packagecenter.ChangeRequest) (PackagePlan, error) {
	if err := validatePackageRequestShape(request); err != nil {
		return PackagePlan{}, err
	}
	name, client, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackagePlan{}, err
	}
	plan, err := planPackageChangeWithClient(ctx, name, client, request)
	if err != nil {
		return PackagePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = packagePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyPackagePlan(ctx context.Context, plan PackagePlan, approvalHash string) (PackageApplyResult, error) {
	if err := validatePackagePlan(plan, approvalHash); err != nil {
		return PackageApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return PackageApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return PackageApplyResult{}, err
	}
	name, client, err := s.packageClient(ctx, plan.NAS)
	if err != nil {
		return PackageApplyResult{}, err
	}
	if name != plan.NAS {
		return PackageApplyResult{}, fmt.Errorf("Package Center plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyPackagePlanWithClient(ctx, client, plan)
}

func (s *Service) packageClient(ctx context.Context, requestedNAS string) (string, packageClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(packageClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Package Center management")
	}
	return name, client, nil
}

func planPackageChangeWithClient(ctx context.Context, nas string, client packageClient, request packagecenter.ChangeRequest) (PackagePlan, error) {
	capabilities, _, err := client.PackageCapabilities(ctx)
	if err != nil {
		return PackagePlan{}, authenticationError(nas, err)
	}
	plan := PackagePlan{APIVersion: packageCenterAPIVersion, NAS: nas, Request: request}
	switch request.Kind {
	case packagecenter.KindSettings:
		if !capabilities.SettingsRead || !capabilities.SettingsSet {
			return PackagePlan{}, fmt.Errorf("NAS %q does not expose a verified Package Center settings read/set backend", nas)
		}
		settings, err := client.PackageSettings(ctx)
		if err != nil {
			return PackagePlan{}, authenticationError(nas, err)
		}
		if err := validateSettingsChange(settings, *request.Settings); err != nil {
			return PackagePlan{}, err
		}
		plan.Observed.Settings = &settings
	case packagecenter.KindLifecycle:
		if !capabilities.InventoryRead {
			return PackagePlan{}, fmt.Errorf("NAS %q does not expose a verified Package Center inventory backend", nas)
		}
		if err := checkLifecycleCapability(capabilities, request.Lifecycle.Action); err != nil {
			return PackagePlan{}, fmt.Errorf("NAS %q %w", nas, err)
		}
		state, err := client.PackageState(ctx)
		if err != nil {
			return PackagePlan{}, authenticationError(nas, err)
		}
		pkg, ok := findPackage(state, request.Lifecycle.PackageID)
		if !ok {
			return PackagePlan{}, fmt.Errorf("package %q is not installed on NAS %q", request.Lifecycle.PackageID, nas)
		}
		if err := validateLifecycleAgainstPackage(request.Lifecycle.Action, pkg); err != nil {
			return PackagePlan{}, err
		}
		plan.Observed.Package = &pkg
	}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return PackagePlan{}, err
	}
	destructive, highRisk, warnings, summary := packagePlanEffects(plan)
	plan.Destructive, plan.Warnings, plan.Summary = destructive, warnings, summary
	if destructive || highRisk {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = packagePlanHash(plan)
	if err != nil {
		return PackagePlan{}, err
	}
	return plan, nil
}

func applyPackagePlanWithClient(ctx context.Context, client packageClient, plan PackagePlan) (PackageApplyResult, error) {
	current, err := planPackageChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return PackageApplyResult{}, fmt.Errorf("Package Center plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = packagePlanHash(current)
	if err != nil {
		return PackageApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return PackageApplyResult{}, fmt.Errorf("Package Center plan is stale; create a new plan")
	}
	var operation synology.PackageMutationResult
	switch plan.Request.Kind {
	case packagecenter.KindSettings:
		desired := mergeSettingsChange(*plan.Observed.Settings, *plan.Request.Settings)
		operation, err = client.ApplyPackageSettingsChange(ctx, desired)
	case packagecenter.KindLifecycle:
		operation, err = client.ApplyPackageLifecycleChange(ctx, *plan.Request.Lifecycle)
	default:
		return PackageApplyResult{}, fmt.Errorf("unsupported change kind %q", plan.Request.Kind)
	}
	if err != nil {
		return PackageApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyPackagePostcondition(ctx, client, plan); err != nil {
		return PackageApplyResult{}, fmt.Errorf("verify Package Center change: %w", err)
	}
	return PackageApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func verifyPackagePostcondition(ctx context.Context, client packageClient, plan PackagePlan) error {
	switch plan.Request.Kind {
	case packagecenter.KindSettings:
		settings, err := client.PackageSettings(ctx)
		if err != nil {
			return err
		}
		if !settingsChangeMatches(settings, *plan.Request.Settings) {
			return fmt.Errorf("settings do not match the approved patch")
		}
		return nil
	case packagecenter.KindLifecycle:
		state, err := client.PackageState(ctx)
		if err != nil {
			return err
		}
		action := plan.Request.Lifecycle.Action
		id := plan.Request.Lifecycle.PackageID
		pkg, ok := findPackage(state, id)
		switch action {
		case packagecenter.ActionUninstall:
			if ok {
				return fmt.Errorf("package %q is still installed", id)
			}
			return nil
		case packagecenter.ActionStart:
			if !ok {
				return fmt.Errorf("package %q disappeared during start", id)
			}
			if pkg.Status == packagecenter.StatusRunning {
				return nil
			}
			return transitionalOrError(pkg, "running")
		case packagecenter.ActionStop:
			if !ok {
				return fmt.Errorf("package %q disappeared during stop", id)
			}
			if pkg.Status == packagecenter.StatusStopped {
				return nil
			}
			return transitionalOrError(pkg, "stopped")
		}
	}
	return nil
}

// transitionalOrError reports an actionable not-yet-confirmed error when DSM is
// still mid-transition, rather than claiming a false success.
func transitionalOrError(pkg packagecenter.Package, want string) error {
	switch pkg.Status {
	case packagecenter.StatusStarting, packagecenter.StatusStopping, packagecenter.StatusInstalling:
		return fmt.Errorf("package %q is still %s; DSM has not reached %s yet, re-check with package inventory", pkg.ID, pkg.Status, want)
	default:
		return fmt.Errorf("package %q is %s, not %s", pkg.ID, pkg.Status, want)
	}
}

func validatePackageRequestShape(request packagecenter.ChangeRequest) error {
	switch request.Kind {
	case packagecenter.KindSettings:
		if request.Settings == nil || request.Lifecycle != nil {
			return fmt.Errorf("settings change requires only the settings patch")
		}
		if emptySettingsChange(*request.Settings) {
			return fmt.Errorf("settings patch has no fields")
		}
	case packagecenter.KindLifecycle:
		if request.Lifecycle == nil || request.Settings != nil {
			return fmt.Errorf("lifecycle change requires only the lifecycle intent")
		}
		if strings.TrimSpace(request.Lifecycle.PackageID) == "" {
			return fmt.Errorf("lifecycle change requires a package id")
		}
		switch request.Lifecycle.Action {
		case packagecenter.ActionStart, packagecenter.ActionStop, packagecenter.ActionUninstall:
		case packagecenter.ActionInstall, packagecenter.ActionUpdate:
			return fmt.Errorf("package %s is not supported in this release; install and update are deferred", request.Lifecycle.Action)
		default:
			return fmt.Errorf("unsupported lifecycle action %q", request.Lifecycle.Action)
		}
	default:
		return fmt.Errorf("unsupported change kind %q", request.Kind)
	}
	return nil
}

func checkLifecycleCapability(capabilities synology.PackageCapabilities, action string) error {
	switch action {
	case packagecenter.ActionStart:
		if !capabilities.Start {
			return fmt.Errorf("does not expose a verified package start backend")
		}
	case packagecenter.ActionStop:
		if !capabilities.Stop {
			return fmt.Errorf("does not expose a verified package stop backend")
		}
	case packagecenter.ActionUninstall:
		if !capabilities.Uninstall {
			return fmt.Errorf("does not expose a verified package uninstall backend")
		}
	default:
		return fmt.Errorf("unsupported lifecycle action %q", action)
	}
	return nil
}

func validateLifecycleAgainstPackage(action string, pkg packagecenter.Package) error {
	switch action {
	case packagecenter.ActionStart:
		if pkg.Status == packagecenter.StatusRunning {
			return fmt.Errorf("package %q is already running", pkg.ID)
		}
		if !pkg.CanStart {
			return fmt.Errorf("package %q reports it cannot be started", pkg.ID)
		}
	case packagecenter.ActionStop:
		if pkg.Status == packagecenter.StatusStopped {
			return fmt.Errorf("package %q is already stopped", pkg.ID)
		}
		if !pkg.CanStop {
			return fmt.Errorf("package %q reports it cannot be stopped", pkg.ID)
		}
	case packagecenter.ActionUninstall:
		if !pkg.CanUninstall {
			return fmt.Errorf("package %q reports it cannot be uninstalled", pkg.ID)
		}
	}
	return nil
}

func validateSettingsChange(state synology.PackageSettings, change packagecenter.SettingsChange) error {
	if settingsChangeMatches(state, change) {
		return fmt.Errorf("settings patch would not change the current state")
	}
	return nil
}

func packagePlanEffects(plan PackagePlan) (destructive, highRisk bool, warnings, summary []string) {
	warnings = []string{}
	summary = []string{}
	switch plan.Request.Kind {
	case packagecenter.KindSettings:
		change := plan.Request.Settings
		if change.AutoUpdateEnabled != nil {
			summary = append(summary, fmt.Sprintf("set automatic updates to %t", *change.AutoUpdateEnabled))
			if !*change.AutoUpdateEnabled {
				warnings = append(warnings, "disabling automatic updates stops security and important package updates from installing on their own")
			}
		}
		if change.AutoUpdateImportantOnly != nil {
			summary = append(summary, fmt.Sprintf("set automatic important-only updates to %t", *change.AutoUpdateImportantOnly))
		}
	case packagecenter.KindLifecycle:
		change := plan.Request.Lifecycle
		switch change.Action {
		case packagecenter.ActionStart:
			summary = append(summary, fmt.Sprintf("start package %q", change.PackageID))
		case packagecenter.ActionStop:
			summary = append(summary, fmt.Sprintf("stop package %q", change.PackageID))
			highRisk = true
			warnings = append(warnings, "stopping the package interrupts its service and dependent packages")
		case packagecenter.ActionUninstall:
			summary = append(summary, fmt.Sprintf("uninstall package %q", change.PackageID))
			destructive = true
			warnings = append(warnings, "uninstalling removes the package and may delete its configuration and data")
		}
	}
	return destructive, highRisk, warnings, summary
}

func mergeSettingsChange(current synology.PackageSettings, change packagecenter.SettingsChange) synology.PackageSettings {
	desired := current
	if change.AutoUpdateEnabled != nil {
		desired.AutoUpdateEnabled = *change.AutoUpdateEnabled
	}
	if change.AutoUpdateImportantOnly != nil {
		desired.AutoUpdateImportantOnly = *change.AutoUpdateImportantOnly
	}
	return desired
}

func settingsChangeMatches(state synology.PackageSettings, change packagecenter.SettingsChange) bool {
	return (change.AutoUpdateEnabled == nil || state.AutoUpdateEnabled == *change.AutoUpdateEnabled) &&
		(change.AutoUpdateImportantOnly == nil || state.AutoUpdateImportantOnly == *change.AutoUpdateImportantOnly)
}

func validatePackagePlan(plan PackagePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the Package Center plan")
	}
	if plan.APIVersion != packageCenterAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid Package Center plan metadata")
	}
	if err := validatePackageRequestShape(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("Package Center plan observed state was modified")
	}
	expectedHash, err := packagePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("Package Center plan contents were modified after planning")
	}
	return nil
}

func packagePlanHash(plan PackagePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func emptySettingsChange(change packagecenter.SettingsChange) bool {
	return change.AutoUpdateEnabled == nil && change.AutoUpdateImportantOnly == nil
}

func findPackage(state synology.PackageState, id string) (packagecenter.Package, bool) {
	id = strings.TrimSpace(id)
	for _, pkg := range state.Packages {
		if pkg.ID == id {
			return pkg, true
		}
	}
	return packagecenter.Package{}, false
}

var _ packageClient = (*synology.Client)(nil)
