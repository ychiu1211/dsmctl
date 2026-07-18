// Package surveillance contains stable, package-version-independent models for
// the Synology Surveillance Station package: system info and camera inventory.
// DSM WebAPI names, versions, and field names stay behind the operation package,
// and the installed SurveillanceStation package version is carried as evidence
// because Surveillance Station's WebAPI follows the package release.
package surveillance

// ModuleName is the stable product-facing identifier for the module.
const ModuleName = "surveillance"

// PackageEvidence reports the installed SurveillanceStation package.
type PackageEvidence struct {
	ID        string `json:"id" jsonschema:"DSM package identifier: SurveillanceStation"`
	Installed bool   `json:"installed" jsonschema:"Whether the Surveillance Station package is installed"`
	Version   string `json:"version,omitempty" jsonschema:"Installed package version"`
	Running   bool   `json:"running" jsonschema:"Whether the Surveillance Station service is running"`
}

// Info is the normalized Surveillance Station system information.
type Info struct {
	Version          string          `json:"version,omitempty" jsonschema:"Surveillance Station version reported by the service"`
	Hostname         string          `json:"hostname,omitempty" jsonschema:"NAS hostname reported by Surveillance Station"`
	CameraNumber     int             `json:"camera_number" jsonschema:"Number of configured cameras"`
	MaxCameraSupport int             `json:"max_camera_support" jsonschema:"Maximum cameras this NAS model supports"`
	LicenseNumber    int             `json:"license_number" jsonschema:"Number of installed camera licenses"`
	Timezone         string          `json:"timezone,omitempty" jsonschema:"Configured timezone"`
	Package          PackageEvidence `json:"package" jsonschema:"Installed Surveillance Station package evidence"`
}

// Camera is one configured camera as seen by the admin camera list.
type Camera struct {
	ID       int    `json:"id" jsonschema:"Camera identifier"`
	Name     string `json:"name,omitempty" jsonschema:"Camera display name"`
	IP       string `json:"ip,omitempty" jsonschema:"Camera IP address or host"`
	Port     int    `json:"port,omitempty" jsonschema:"Camera port when reported"`
	Vendor   string `json:"vendor,omitempty" jsonschema:"Camera vendor/brand"`
	Model    string `json:"model,omitempty" jsonschema:"Camera model"`
	Status   int    `json:"status" jsonschema:"Camera connection status as reported by Surveillance Station"`
	Enabled  bool   `json:"enabled" jsonschema:"Whether the camera is enabled"`
	RecTime  int64  `json:"recording_status,omitempty" jsonschema:"Recording status code as reported by Surveillance Station"`
}

// Cameras is the admin view of configured cameras.
type Cameras struct {
	Total   int      `json:"total" jsonschema:"Total cameras reported; falls back to the item count when absent"`
	Cameras []Camera `json:"cameras" jsonschema:"Configured cameras reported by Surveillance Station"`
}

// Capabilities reports which Surveillance Station operations dsmctl exposes for
// the installed package.
type Capabilities struct {
	Module     string          `json:"module" jsonschema:"Stable module name: surveillance"`
	Package    PackageEvidence `json:"package" jsonschema:"Installed Surveillance Station package evidence"`
	InfoRead   bool            `json:"info_read" jsonschema:"Whether system info can be read"`
	CameraRead bool            `json:"camera_read" jsonschema:"Whether the camera list can be read"`
}
