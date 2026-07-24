package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/derekvery666/dsmctl/internal/domain/packagecenter"
	"github.com/derekvery666/dsmctl/internal/synology"
	"github.com/derekvery666/dsmctl/internal/synology/compatibility"
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
	PackageInstallLocal(context.Context, synology.PackageLocalInstallInput) (synology.PackageInstallResult, error)
	PackageCapabilities(context.Context) (synology.PackageCapabilities, synology.CompatibilityReport, error)
	ApplyPackageSettingsChange(context.Context, synology.PackageSettings) (synology.PackageMutationResult, error)
	ApplyPackageLifecycleChange(context.Context, synology.PackageLifecycleChange) (synology.PackageMutationResult, error)
}

// PackageCatalogResult is the online-catalog read result.
type PackageCatalogResult struct {
	NAS     string                  `json:"nas" jsonschema:"NAS profile used for the request"`
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
	APIVersion      string               `json:"api_version" jsonschema:"Plan schema version"`
	NAS             string               `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision uint64               `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	PackageID       string               `json:"package_id" jsonschema:"Target package identifier to install"`
	Name            string               `json:"name" jsonschema:"Human-readable target package name"`
	Version         string               `json:"version" jsonschema:"Offered target version to install"`
	Update          bool                 `json:"update,omitempty" jsonschema:"Whether this plan updates an installed package instead of installing a new one"`
	FromVersion     string               `json:"from_version,omitempty" jsonschema:"Installed version the update plan binds to (update plans only)"`
	VolumePath      string               `json:"volume_path" jsonschema:"Target install volume path"`
	RunAfterInstall bool                 `json:"run_after_install" jsonschema:"Whether the packages start after install"`
	Dependencies    []string             `json:"dependencies,omitempty" jsonschema:"Missing dependency packages that will be installed first, in order"`
	Steps           []PackageInstallStep `json:"steps" jsonschema:"Ordered install steps: missing dependencies first, target last"`
	Risk            string               `json:"risk" jsonschema:"Plan risk level"`
	Warnings        []string             `json:"warnings" jsonschema:"Install warnings"`
	Summary         []string             `json:"summary" jsonschema:"Human-readable operations"`
	Hash            string               `json:"hash" jsonschema:"SHA-256 approval hash covering the resolved install intent"`
}

// PackageInstallStep is one package to install (a dependency or the target),
// with the catalog-resolved download metadata.
type PackageInstallStep struct {
	PackageID    string `json:"package_id" jsonschema:"Package identifier"`
	Name         string `json:"name" jsonschema:"Human-readable package name"`
	Version      string `json:"version" jsonschema:"Offered version"`
	Size         int64  `json:"size" jsonschema:"Download size in bytes"`
	DownloadLink string `json:"download_link" jsonschema:"Resolved download URL"`
	Checksum     string `json:"checksum" jsonschema:"Resolved package checksum (md5)"`
	Beta         bool   `json:"beta" jsonschema:"Whether the offered build is a beta"`
	QuickInstall bool   `json:"quick_install" jsonschema:"Whether quick install (no wizard) is used"`
	Dependency   bool   `json:"dependency" jsonschema:"Whether this step is a dependency of the target"`
}

// PackageInstallApplyResult reports the outcome of a completed install.
type PackageInstallApplyResult struct {
	NAS      string                          `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                          `json:"plan_hash" jsonschema:"Approved plan hash"`
	Results  []synology.PackageInstallResult `json:"results" jsonschema:"Per-package install outcomes confirmed by inventory, in install order"`
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
	plan, err := planPackageInstallWithClient(ctx, name, client, packageID, volumePath, runAfterInstall, quickInstall)
	if err != nil {
		return PackageInstallPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = packageInstallPlanHash(plan)
	}
	return plan, err
}

func planPackageInstallWithClient(ctx context.Context, nas string, client packageClient, packageID, volumePath string, runAfterInstall, quickInstall bool) (PackageInstallPlan, error) {
	state, err := client.PackageState(ctx)
	if err != nil {
		return PackageInstallPlan{}, authenticationError(nas, err)
	}
	installed := make(map[string]bool, len(state.Packages))
	for _, pkg := range state.Packages {
		installed[pkg.ID] = true
	}
	if installed[packageID] {
		return PackageInstallPlan{}, fmt.Errorf("package %q is already installed", packageID)
	}
	catalog, err := client.PackageCatalog(ctx)
	if err != nil {
		return PackageInstallPlan{}, authenticationError(nas, err)
	}
	offeredByID := catalogByID(catalog)

	// Resolve the dependency closure (deps-first) from the catalog deppkgs, so
	// missing dependencies are installed before the target — this is the precheck
	// DSM's UI would show as "install <dependency> first".
	ordered, err := resolveInstallOrder(packageID, offeredByID, installed)
	if err != nil {
		return PackageInstallPlan{}, err
	}

	plan := PackageInstallPlan{
		APIVersion: packageInstallAPIVersion, NAS: nas, PackageID: packageID, VolumePath: volumePath, RunAfterInstall: runAfterInstall,
		Risk:     "high",
		Warnings: []string{"installing downloads and runs third-party software on the NAS"},
	}
	target := offeredByID[packageID]
	plan.Name, plan.Version = target.Name, target.Version
	for _, pkg := range ordered {
		isDep := pkg.ID != packageID
		plan.Steps = append(plan.Steps, PackageInstallStep{
			PackageID: pkg.ID, Name: pkg.Name, Version: pkg.Version, Size: pkg.Size,
			DownloadLink: pkg.DownloadLink, Checksum: pkg.Checksum,
			Beta: pkg.Beta, QuickInstall: pkg.QuickInstall || quickInstall, Dependency: isDep,
		})
		if isDep {
			plan.Dependencies = append(plan.Dependencies, pkg.ID)
			plan.Summary = append(plan.Summary, fmt.Sprintf("install dependency %s %s", pkg.ID, pkg.Version))
		}
		if pkg.Beta {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is a beta version", pkg.ID))
		}
	}
	plan.Summary = append(plan.Summary, fmt.Sprintf("install %s %s to %s", target.ID, target.Version, volumePath))
	if len(plan.Dependencies) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s requires %d dependency package(s) that will be installed first: %s", packageID, len(plan.Dependencies), strings.Join(plan.Dependencies, ", ")))
	}
	plan.Hash, err = packageInstallPlanHash(plan)
	if err != nil {
		return PackageInstallPlan{}, err
	}
	return plan, nil
}

// PlanPackageUpdate resolves an update of an installed package to the version
// offered by the online catalog. The plan binds to the installed version, so a
// package that changes between plan and apply is rejected as stale.
func (s *Service) PlanPackageUpdate(ctx context.Context, requestedNAS, packageID string) (PackageInstallPlan, error) {
	if strings.TrimSpace(packageID) == "" {
		return PackageInstallPlan{}, fmt.Errorf("update requires a package id")
	}
	name, client, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackageInstallPlan{}, err
	}
	plan, err := planPackageUpdateWithClient(ctx, name, client, packageID)
	if err != nil {
		return PackageInstallPlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = packageInstallPlanHash(plan)
	}
	return plan, err
}

func planPackageUpdateWithClient(ctx context.Context, nas string, client packageClient, packageID string) (PackageInstallPlan, error) {
	state, err := client.PackageState(ctx)
	if err != nil {
		return PackageInstallPlan{}, authenticationError(nas, err)
	}
	installed := make(map[string]bool, len(state.Packages))
	var current *packagecenter.Package
	for i := range state.Packages {
		installed[state.Packages[i].ID] = true
		if state.Packages[i].ID == packageID {
			current = &state.Packages[i]
		}
	}
	if current == nil {
		return PackageInstallPlan{}, fmt.Errorf("package %q is not installed; use install instead", packageID)
	}
	catalog, err := client.PackageCatalog(ctx)
	if err != nil {
		return PackageInstallPlan{}, authenticationError(nas, err)
	}
	offeredByID := catalogByID(catalog)
	target, offered := offeredByID[packageID]
	if !offered {
		return PackageInstallPlan{}, fmt.Errorf("package %q is not offered by the online package server", packageID)
	}
	if target.Version == current.Version {
		return PackageInstallPlan{}, fmt.Errorf("package %q is already at the offered version %s", packageID, current.Version)
	}
	// Re-compare versions even though the catalog carries an update flag. Plans
	// must independently refuse a downgrade if the inventory or catalog changed.
	installedVersion := compatibility.ParsePackageVersion(current.Version)
	offeredVersion := compatibility.ParsePackageVersion(target.Version)
	if offeredVersion.Compare(installedVersion) <= 0 {
		return PackageInstallPlan{}, fmt.Errorf("the offered %s %s is not newer than the installed %s; refusing to downgrade", packageID, target.Version, current.Version)
	}

	// Resolve missing NEW dependencies of the offered version (deps-first). The
	// target itself is installed, so lift it from the installed set to include
	// it as the final step.
	dependencyBase := make(map[string]bool, len(installed))
	for id := range installed {
		dependencyBase[id] = true
	}
	delete(dependencyBase, packageID)
	ordered, err := resolveInstallOrder(packageID, offeredByID, dependencyBase)
	if err != nil {
		return PackageInstallPlan{}, err
	}

	plan := PackageInstallPlan{
		APIVersion: packageInstallAPIVersion, NAS: nas, PackageID: packageID,
		Update: true, FromVersion: current.Version,
		VolumePath: current.Volume, RunAfterInstall: current.Running,
		Risk: "high",
		Warnings: []string{
			"updating downloads and runs third-party software on the NAS",
			"a package update cannot be downgraded afterwards",
		},
	}
	plan.Name, plan.Version = target.Name, target.Version
	for _, pkg := range ordered {
		isDep := pkg.ID != packageID
		plan.Steps = append(plan.Steps, PackageInstallStep{
			PackageID: pkg.ID, Name: pkg.Name, Version: pkg.Version, Size: pkg.Size,
			DownloadLink: pkg.DownloadLink, Checksum: pkg.Checksum,
			Beta: pkg.Beta, QuickInstall: true, Dependency: isDep,
		})
		if isDep {
			plan.Dependencies = append(plan.Dependencies, pkg.ID)
			plan.Summary = append(plan.Summary, fmt.Sprintf("install new dependency %s %s", pkg.ID, pkg.Version))
		}
		if pkg.Beta {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is a beta version", pkg.ID))
		}
	}
	plan.Summary = append(plan.Summary, fmt.Sprintf("update %s from %s to %s", target.ID, current.Version, target.Version))
	if len(plan.Dependencies) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s requires %d new dependency package(s) that will be installed first: %s", packageID, len(plan.Dependencies), strings.Join(plan.Dependencies, ", ")))
	}
	plan.Hash, err = packageInstallPlanHash(plan)
	if err != nil {
		return PackageInstallPlan{}, err
	}
	return plan, nil
}

// catalogByID indexes the online catalog by package id. The catalog can offer
// the same package twice (a stable and a beta build); prefer the stable entry,
// and among builds of the same channel prefer the higher version, so a beta
// row can never shadow the stable offer.
func catalogByID(catalog synology.PackageCatalog) map[string]*packagecenter.AvailablePackage {
	offered := make(map[string]*packagecenter.AvailablePackage, len(catalog.Packages))
	for i := range catalog.Packages {
		candidate := &catalog.Packages[i]
		existing, ok := offered[candidate.ID]
		if !ok {
			offered[candidate.ID] = candidate
			continue
		}
		if existing.Beta != candidate.Beta {
			if existing.Beta {
				offered[candidate.ID] = candidate
			}
			continue
		}
		if compatibility.ParsePackageVersion(candidate.Version).Compare(compatibility.ParsePackageVersion(existing.Version)) > 0 {
			offered[candidate.ID] = candidate
		}
	}
	return offered
}

// resolveInstallOrder returns the target and its missing dependencies in
// install order (dependencies first, target last), using the catalog's deppkgs.
// A required dependency that is neither installed nor offered is a hard error —
// the precheck failure DSM would raise.
func resolveInstallOrder(targetID string, offered map[string]*packagecenter.AvailablePackage, installed map[string]bool) ([]*packagecenter.AvailablePackage, error) {
	var order []*packagecenter.AvailablePackage
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(id string, requiredBy string) error
	visit = func(id string, requiredBy string) error {
		if visited[id] || installed[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("dependency cycle detected at package %q", id)
		}
		pkg, ok := offered[id]
		if !ok {
			if requiredBy == "" {
				return fmt.Errorf("package %q is not offered by the online package server", id)
			}
			return fmt.Errorf("package %q requires %q, which is not installed and not offered by the online package server", requiredBy, id)
		}
		if pkg.DownloadLink == "" {
			return fmt.Errorf("package %q has no download link in the catalog", id)
		}
		visiting[id] = true
		for _, dep := range pkg.Dependencies {
			if err := visit(dep, id); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		order = append(order, pkg)
		return nil
	}
	if err := visit(targetID, ""); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *Service) ApplyPackageInstallPlan(ctx context.Context, plan PackageInstallPlan, approvalHash string) (PackageInstallApplyResult, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return PackageInstallApplyResult{}, fmt.Errorf("approval hash does not match the install plan")
	}
	if plan.APIVersion != packageInstallAPIVersion || strings.TrimSpace(plan.NAS) == "" || strings.TrimSpace(plan.PackageID) == "" {
		return PackageInstallApplyResult{}, fmt.Errorf("invalid install plan metadata")
	}
	if len(plan.Steps) == 0 {
		return PackageInstallApplyResult{}, fmt.Errorf("install plan has no steps")
	}
	expectedHash, err := packageInstallPlanHash(plan)
	if err != nil {
		return PackageInstallApplyResult{}, err
	}
	if expectedHash != plan.Hash {
		return PackageInstallApplyResult{}, fmt.Errorf("install plan contents were modified after planning")
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return PackageInstallApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return PackageInstallApplyResult{}, err
	}
	name, client, err := s.packageClient(ctx, plan.NAS)
	if err != nil {
		return PackageInstallApplyResult{}, err
	}
	if name != plan.NAS {
		return PackageInstallApplyResult{}, fmt.Errorf("install plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	if plan.Update {
		if err := verifyPackageUpdatePrecondition(ctx, plan, client); err != nil {
			return PackageInstallApplyResult{}, err
		}
	}
	// Install each step in order (dependencies first, target last). A failed
	// dependency aborts before the target is attempted.
	results := make([]synology.PackageInstallResult, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		expectVersion := ""
		if plan.Update && step.PackageID == plan.PackageID {
			// The update target is already present; completion means the
			// inventory reports the offered version.
			expectVersion = step.Version
		}
		result, err := client.PackageInstall(ctx, synology.PackageInstallInput{
			Name: step.PackageID, URL: step.DownloadLink, Checksum: step.Checksum, Filesize: step.Size,
			Beta: step.Beta, QuickInstall: step.QuickInstall, VolumePath: plan.VolumePath, RunAfterInstall: plan.RunAfterInstall,
			ExpectVersion: expectVersion,
		})
		if err != nil {
			return PackageInstallApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Results: results}, authenticationError(plan.NAS, fmt.Errorf("install %s: %w", step.PackageID, err))
		}
		results = append(results, result)
	}
	return PackageInstallApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Results: results}, nil
}

func packageInstallPlanHash(plan PackageInstallPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// verifyPackageUpdatePrecondition rejects an update plan whose target changed
// after planning: the plan binds to the installed version it observed, so a
// package that was already updated or removed in between is stale.
func verifyPackageUpdatePrecondition(ctx context.Context, plan PackageInstallPlan, client packageClient) error {
	if strings.TrimSpace(plan.FromVersion) == "" {
		return fmt.Errorf("update plan is missing the installed version it binds to")
	}
	state, err := client.PackageState(ctx)
	if err != nil {
		return authenticationError(plan.NAS, err)
	}
	currentVersion := ""
	for _, pkg := range state.Packages {
		if pkg.ID == plan.PackageID {
			currentVersion = pkg.Version
			break
		}
	}
	if currentVersion == "" {
		return fmt.Errorf("package %q is no longer installed; the update plan is stale", plan.PackageID)
	}
	if currentVersion != plan.FromVersion {
		return fmt.Errorf("package %q is now %s, not the planned %s; create a new plan", plan.PackageID, currentVersion, plan.FromVersion)
	}
	return nil
}

const packageLocalInstallAPIVersion = "dsmctl.io/v1alpha1"

// maxLocalPackageSize caps the .spk read into memory for upload. Real packages
// are well under this; the bound turns a wrong path (e.g. a disk image) into a
// clear error instead of exhausting memory.
const maxLocalPackageSize = 1 << 30 // 1 GiB

// PackageLocalInstallPlan is a hash-bound intent to install a package from a
// local .spk file. Unlike the online install plan it is not resolved against the
// online catalog; it is bound to the exact file content (size + SHA-256) so apply
// installs precisely what was planned.
type PackageLocalInstallPlan struct {
	APIVersion      string   `json:"api_version" jsonschema:"Plan schema version"`
	NAS             string   `json:"nas" jsonschema:"NAS profile selected during planning"`
	SPKPath         string   `json:"spk_path" jsonschema:"Local .spk file path to upload and install"`
	FileName        string   `json:"file_name" jsonschema:"Base name of the .spk file sent to DSM"`
	FileSize        int64    `json:"file_size" jsonschema:"Size of the .spk file in bytes"`
	FileSHA256      string   `json:"file_sha256" jsonschema:"SHA-256 of the .spk file content the plan is bound to"`
	VolumePath      string   `json:"volume_path" jsonschema:"Target install volume path"`
	RunAfterInstall bool     `json:"run_after_install" jsonschema:"Whether the package starts after install"`
	AllowUnsigned   bool     `json:"allow_unsigned" jsonschema:"Whether DSM code-signature enforcement is disabled for this install"`
	Risk            string   `json:"risk" jsonschema:"Plan risk level"`
	Warnings        []string `json:"warnings" jsonschema:"Install warnings"`
	Summary         []string `json:"summary" jsonschema:"Human-readable operations"`
	Hash            string   `json:"hash" jsonschema:"SHA-256 approval hash covering the resolved install intent"`
}

// PackageLocalInstallApplyResult reports the outcome of a completed local install.
type PackageLocalInstallApplyResult struct {
	NAS      string                        `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                        `json:"plan_hash" jsonschema:"Approved plan hash"`
	Result   synology.PackageInstallResult `json:"result" jsonschema:"Install outcome confirmed by inventory"`
}

// PlanPackageLocalInstall reads and hashes a local .spk and emits a hash-bound
// install plan. The file is bound by content, so apply refuses a changed file.
func (s *Service) PlanPackageLocalInstall(ctx context.Context, requestedNAS, spkPath, volumePath string, runAfterInstall, allowUnsigned bool) (PackageLocalInstallPlan, error) {
	if strings.TrimSpace(spkPath) == "" {
		return PackageLocalInstallPlan{}, fmt.Errorf("local install requires a .spk file path")
	}
	if strings.TrimSpace(volumePath) == "" {
		return PackageLocalInstallPlan{}, fmt.Errorf("local install requires a target volume path")
	}
	data, err := readLocalPackage(spkPath)
	if err != nil {
		return PackageLocalInstallPlan{}, err
	}
	// Resolve the NAS profile (and validate connectivity) so the plan names the
	// exact profile apply must match.
	name, _, err := s.packageClient(ctx, requestedNAS)
	if err != nil {
		return PackageLocalInstallPlan{}, err
	}
	return newLocalInstallPlan(name, spkPath, data, volumePath, runAfterInstall, allowUnsigned)
}

// newLocalInstallPlan builds a hash-bound plan from the resolved NAS name and the
// .spk bytes. It is the pure core of PlanPackageLocalInstall.
func newLocalInstallPlan(nas, spkPath string, data []byte, volumePath string, runAfterInstall, allowUnsigned bool) (PackageLocalInstallPlan, error) {
	sum := sha256.Sum256(data)
	plan := PackageLocalInstallPlan{
		APIVersion:      packageLocalInstallAPIVersion,
		NAS:             nas,
		SPKPath:         spkPath,
		FileName:        filepath.Base(spkPath),
		FileSize:        int64(len(data)),
		FileSHA256:      hex.EncodeToString(sum[:]),
		VolumePath:      volumePath,
		RunAfterInstall: runAfterInstall,
		AllowUnsigned:   allowUnsigned,
		Risk:            "high",
		Warnings:        []string{"installing uploads and runs third-party software on the NAS from a local file"},
	}
	if allowUnsigned {
		plan.Warnings = append(plan.Warnings, "code-signature verification is disabled (--allow-unsigned): the package publisher is not verified")
	}
	plan.Summary = []string{fmt.Sprintf("upload %s (%d bytes) and install to %s", plan.FileName, plan.FileSize, volumePath)}
	var err error
	plan.Hash, err = packageLocalInstallPlanHash(plan)
	if err != nil {
		return PackageLocalInstallPlan{}, err
	}
	return plan, nil
}

// ApplyPackageLocalInstallPlan verifies the approval hash and that the .spk on
// disk still matches the plan, then uploads and installs it.
func (s *Service) ApplyPackageLocalInstallPlan(ctx context.Context, plan PackageLocalInstallPlan, approvalHash string) (PackageLocalInstallApplyResult, error) {
	data, err := validateLocalInstallApply(plan, approvalHash)
	if err != nil {
		return PackageLocalInstallApplyResult{}, err
	}
	name, client, err := s.packageClient(ctx, plan.NAS)
	if err != nil {
		return PackageLocalInstallApplyResult{}, err
	}
	if name != plan.NAS {
		return PackageLocalInstallApplyResult{}, fmt.Errorf("local install plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyLocalInstallWithClient(ctx, client, plan, data)
}

// validateLocalInstallApply checks the approval hash, plan metadata, and that the
// .spk on disk still matches the planned content, returning the verified bytes.
func validateLocalInstallApply(plan PackageLocalInstallPlan, approvalHash string) ([]byte, error) {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return nil, fmt.Errorf("approval hash does not match the local install plan")
	}
	if plan.APIVersion != packageLocalInstallAPIVersion || strings.TrimSpace(plan.NAS) == "" || strings.TrimSpace(plan.SPKPath) == "" {
		return nil, fmt.Errorf("invalid local install plan metadata")
	}
	expectedHash, err := packageLocalInstallPlanHash(plan)
	if err != nil {
		return nil, err
	}
	if expectedHash != plan.Hash {
		return nil, fmt.Errorf("local install plan contents were modified after planning")
	}
	data, err := readLocalPackage(plan.SPKPath)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != plan.FileSize || hex.EncodeToString(sum[:]) != plan.FileSHA256 {
		return nil, fmt.Errorf("the .spk file changed since planning; create a new plan")
	}
	return data, nil
}

// applyLocalInstallWithClient uploads and installs the verified .spk bytes. It is
// the client-facing core of ApplyPackageLocalInstallPlan.
func applyLocalInstallWithClient(ctx context.Context, client packageClient, plan PackageLocalInstallPlan, data []byte) (PackageLocalInstallApplyResult, error) {
	result, err := client.PackageInstallLocal(ctx, synology.PackageLocalInstallInput{
		FileName:        plan.FileName,
		Data:            data,
		VolumePath:      plan.VolumePath,
		RunAfterInstall: plan.RunAfterInstall,
		AllowUnsigned:   plan.AllowUnsigned,
	})
	if err != nil {
		return PackageLocalInstallApplyResult{NAS: plan.NAS, PlanHash: plan.Hash}, authenticationError(plan.NAS, err)
	}
	return PackageLocalInstallApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Result: result}, nil
}

func packageLocalInstallPlanHash(plan PackageLocalInstallPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

// readLocalPackage reads a .spk into memory, rejecting an empty or oversized file
// so a wrong path fails clearly rather than being uploaded or exhausting memory.
func readLocalPackage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read .spk file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory, not a .spk file", path)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	if info.Size() > maxLocalPackageSize {
		return nil, fmt.Errorf("%s is %d bytes, larger than the %d-byte local install limit", path, info.Size(), maxLocalPackageSize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read .spk file: %w", err)
	}
	return data, nil
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
