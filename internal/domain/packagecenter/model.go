// Package packagecenter contains stable, DSM-version-independent models for
// Synology Package Center: installed-package inventory, global settings, and
// guarded package lifecycle intent. DSM WebAPI names, versions, and field names
// stay behind the operation package so these contracts remain stable.
package packagecenter

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "package-center"

// Status is the normalized run state of an installed package. It is independent
// of the raw DSM status vocabulary, which varies across releases.
type Status string

const (
	StatusRunning    Status = "running"
	StatusStopped    Status = "stopped"
	StatusStarting   Status = "starting"
	StatusStopping   Status = "stopping"
	StatusInstalling Status = "installing"
	StatusError      Status = "error"
	StatusUnknown    Status = "unknown"
)

// TrustLevel is the normalized publisher trust policy that Package Center
// enforces when installing or updating packages.
type TrustLevel string

const (
	// TrustSynology accepts only packages published by Synology.
	TrustSynology TrustLevel = "synology"
	// TrustSynologyAndTrusted accepts Synology and trusted third-party publishers.
	TrustSynologyAndTrusted TrustLevel = "synology_and_trusted"
	// TrustAny accepts packages from any publisher.
	TrustAny TrustLevel = "any"
)

// Change kinds carried by a single ChangeRequest.
const (
	KindSettings  = "settings"
	KindLifecycle = "lifecycle"
)

// Lifecycle actions. Install and Update are modeled so capability reporting and
// request validation can name them, but they are not implemented in this slice
// and fail closed.
const (
	ActionStart     = "start"
	ActionStop      = "stop"
	ActionUninstall = "uninstall"
	ActionInstall   = "install"
	ActionUpdate    = "update"
)

// Package is one installed DSM package with normalized, semantic fields.
type Package struct {
	ID           string `json:"id" jsonschema:"Stable DSM package identifier"`
	Name         string `json:"name" jsonschema:"Human-readable package name"`
	Version      string `json:"version,omitempty" jsonschema:"Installed package version"`
	Status       Status `json:"status" jsonschema:"Normalized run status"`
	Running      bool   `json:"running" jsonschema:"Whether the package service is currently running"`
	Beta         bool   `json:"beta,omitempty" jsonschema:"Whether the installed package is a beta build"`
	Volume       string `json:"volume,omitempty" jsonschema:"Install volume path when reported by DSM"`
	CanStart     bool   `json:"can_start" jsonschema:"Whether DSM reports the package can be started"`
	CanStop      bool   `json:"can_stop" jsonschema:"Whether DSM reports the package can be stopped"`
	CanUninstall bool   `json:"can_uninstall" jsonschema:"Whether DSM reports the package can be uninstalled"`
}

// State is a point-in-time inventory of installed packages.
type State struct {
	Packages []Package `json:"packages" jsonschema:"Installed packages reported by Package Center"`
}

// Settings is the normalized global Package Center configuration. Only the
// fields exposed by SYNO.Core.Package.Setting are modeled: the publisher trust
// level and the automatic-update policy. Beta packages and default install
// volume live in other DSM APIs (the online package server and per-install
// selection) and are deferred with the install/update work. Per-package,
// application-specific settings are out of scope.
type Settings struct {
	TrustLevel              TrustLevel `json:"trust_level" jsonschema:"Publisher trust policy for installs and updates: synology, synology_and_trusted, or any"`
	AutoUpdateEnabled       bool       `json:"auto_update_enabled" jsonschema:"Whether DSM installs package updates automatically"`
	AutoUpdateImportantOnly bool       `json:"auto_update_important_only" jsonschema:"When auto-update is enabled, whether only important (security) updates install automatically"`
}

// Capabilities reports which Package Center operations dsmctl currently exposes
// for the selected DSM backend. Update is false until its upgrade backend ships.
type Capabilities struct {
	Module        string `json:"module" jsonschema:"Stable module name: package-center"`
	InventoryRead bool   `json:"inventory_read" jsonschema:"Whether installed-package inventory can be read"`
	SettingsRead  bool   `json:"settings_read" jsonschema:"Whether global settings can be read"`
	SettingsSet   bool   `json:"settings_set" jsonschema:"Whether guarded global settings changes are available"`
	Start         bool   `json:"start" jsonschema:"Whether guarded package start is available"`
	Stop          bool   `json:"stop" jsonschema:"Whether guarded package stop is available"`
	Uninstall     bool   `json:"uninstall" jsonschema:"Whether guarded package uninstall is available"`
	Install       bool   `json:"install" jsonschema:"Whether guarded online package install is available"`
	Update        bool   `json:"update" jsonschema:"Whether guarded package update is available; deferred, currently always false"`
}

// AvailablePackage is one package offered by the configured online package
// server (Synology's repository), with the metadata needed to plan an install or
// update. Download fields are what DSM's guarded download+install task consumes.
type AvailablePackage struct {
	ID              string `json:"id" jsonschema:"Stable DSM package identifier"`
	Name            string `json:"name,omitempty" jsonschema:"Human-readable package name"`
	Version         string `json:"version,omitempty" jsonschema:"Offered package version"`
	Beta            bool   `json:"beta,omitempty" jsonschema:"Whether the offered build is a beta"`
	Size            int64  `json:"size,omitempty" jsonschema:"Download size in bytes when reported"`
	DownloadLink    string `json:"download_link,omitempty" jsonschema:"Package download URL used by the guarded install"`
	Checksum        string   `json:"checksum,omitempty" jsonschema:"Package checksum (md5) when reported"`
	QuickInstall    bool     `json:"quick_install" jsonschema:"Whether the package supports quick install (no configuration wizard)"`
	Dependencies    []string `json:"dependencies,omitempty" jsonschema:"Package identifiers this package requires (from the catalog deppkgs)"`
	Installed       bool     `json:"installed" jsonschema:"Whether this package is already installed"`
	UpdateAvailable bool     `json:"update_available" jsonschema:"Whether an installed package has a newer offered version"`
}

// Catalog is a point-in-time view of packages offered by the online package
// server.
type Catalog struct {
	Packages []AvailablePackage `json:"packages" jsonschema:"Packages offered by the online package server"`
}

// ChangeRequest is the stable Package Center intent shared by CLI and MCP. It
// carries exactly one of a patch-only settings change or a package lifecycle
// action, selected by Kind.
type ChangeRequest struct {
	Kind      string           `json:"kind" jsonschema:"Change kind: settings or lifecycle"`
	Settings  *SettingsChange  `json:"settings,omitempty" jsonschema:"Patch-only global settings intent when kind is settings"`
	Lifecycle *LifecycleChange `json:"lifecycle,omitempty" jsonschema:"Package lifecycle intent when kind is lifecycle"`
}

// SettingsChange is a patch-only settings intent. A nil field keeps the
// currently configured DSM value. Only the automatic-update policy is writable;
// publisher trust level is read-only (no DSM endpoint writes it).
type SettingsChange struct {
	AutoUpdateEnabled       *bool `json:"auto_update_enabled,omitempty" jsonschema:"Enable or disable automatic package updates"`
	AutoUpdateImportantOnly *bool `json:"auto_update_important_only,omitempty" jsonschema:"Restrict automatic updates to important updates only"`
}

// LifecycleChange identifies a package by its stable DSM identifier and the
// action to take on it.
type LifecycleChange struct {
	Action    string `json:"action" jsonschema:"Lifecycle action: start, stop, or uninstall"`
	PackageID string `json:"package_id" jsonschema:"Stable DSM package identifier to act on"`
}
