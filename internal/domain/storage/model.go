package storage

// State is a stable, DSM-version-independent snapshot of the storage resources
// currently visible to the authenticated account.
type State struct {
	Disks   []Disk   `json:"disks" jsonschema:"Physical disks reported by DSM"`
	Pools   []Pool   `json:"pools" jsonschema:"Storage pools and RAID groups reported by DSM"`
	Volumes []Volume `json:"volumes" jsonschema:"Volumes reported by DSM"`
}

type Disk struct {
	ID           string   `json:"id" jsonschema:"Stable DSM disk identifier"`
	Name         string   `json:"name,omitempty" jsonschema:"Human-readable disk name"`
	Device       string   `json:"device,omitempty" jsonschema:"DSM or operating-system device name"`
	Slot         string   `json:"slot,omitempty" jsonschema:"Physical slot or bay identifier"`
	Vendor       string   `json:"vendor,omitempty" jsonschema:"Disk vendor"`
	Model        string   `json:"model,omitempty" jsonschema:"Disk model"`
	Serial       string   `json:"serial,omitempty" jsonschema:"Disk serial number"`
	Firmware     string   `json:"firmware,omitempty" jsonschema:"Disk firmware version"`
	Type         string   `json:"type,omitempty" jsonschema:"Disk media type such as HDD or SSD"`
	Interface    string   `json:"interface,omitempty" jsonschema:"Disk interface such as SATA or NVMe"`
	Status       string   `json:"status,omitempty" jsonschema:"Normalized DSM disk status code"`
	Health       string   `json:"health,omitempty" jsonschema:"DSM health or overview status"`
	SMARTStatus  string   `json:"smart_status,omitempty" jsonschema:"Latest SMART status reported by DSM"`
	SizeBytes    uint64   `json:"size_bytes,omitempty" jsonschema:"Disk capacity in bytes"`
	TemperatureC *float64 `json:"temperature_c,omitempty" jsonschema:"Disk temperature in Celsius when available"`
}

type Pool struct {
	ID             string   `json:"id" jsonschema:"Stable DSM storage-pool identifier"`
	Name           string   `json:"name,omitempty" jsonschema:"Human-readable storage-pool name"`
	RAIDType       string   `json:"raid_type,omitempty" jsonschema:"RAID or Synology Hybrid RAID type"`
	Status         string   `json:"status,omitempty" jsonschema:"Normalized DSM storage-pool status code"`
	Health         string   `json:"health,omitempty" jsonschema:"DSM health or overview status"`
	SizeBytes      uint64   `json:"size_bytes,omitempty" jsonschema:"Total storage-pool capacity in bytes"`
	UsedBytes      uint64   `json:"used_bytes,omitempty" jsonschema:"Used storage-pool capacity in bytes"`
	AvailableBytes uint64   `json:"available_bytes,omitempty" jsonschema:"Available storage-pool capacity in bytes"`
	DiskIDs        []string `json:"disk_ids" jsonschema:"DSM disk identifiers belonging to the pool"`
}

type Volume struct {
	ID             string `json:"id" jsonschema:"Stable DSM volume identifier"`
	Name           string `json:"name,omitempty" jsonschema:"Human-readable volume name"`
	PoolID         string `json:"pool_id,omitempty" jsonschema:"Storage pool containing the volume"`
	FileSystem     string `json:"file_system,omitempty" jsonschema:"Volume file-system type"`
	Status         string `json:"status,omitempty" jsonschema:"Normalized DSM volume status code"`
	Health         string `json:"health,omitempty" jsonschema:"DSM health or overview status"`
	SizeBytes      uint64 `json:"size_bytes,omitempty" jsonschema:"Total volume capacity in bytes"`
	UsedBytes      uint64 `json:"used_bytes,omitempty" jsonschema:"Used volume capacity in bytes"`
	AvailableBytes uint64 `json:"available_bytes,omitempty" jsonschema:"Available volume capacity in bytes"`
	ReadOnly       bool   `json:"read_only" jsonschema:"Whether DSM reports the volume as read-only"`
}

// Capabilities deliberately distinguishes the read-only first milestone from
// future mutation support so an agent cannot infer that discovery implies write
// access.
type Capabilities struct {
	InventoryRead bool `json:"inventory_read" jsonschema:"Disk, pool, and volume inventory can be read"`
	DiskStatus    bool `json:"disk_status" jsonschema:"Disk status and health can be read"`
	PoolStatus    bool `json:"pool_status" jsonschema:"Storage-pool status and health can be read"`
	VolumeStatus  bool `json:"volume_status" jsonschema:"Volume status and health can be read"`
	PoolCreate    bool `json:"pool_create" jsonschema:"Storage pools can be created; false in the first milestone"`
	VolumeCreate  bool `json:"volume_create" jsonschema:"Volumes can be created; false in the first milestone"`
	Mutations     bool `json:"mutations" jsonschema:"Any storage mutation is currently exposed"`
}
