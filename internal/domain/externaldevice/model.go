// Package externaldevice contains stable, read-only models for the Control
// Panel External Devices surface: attached external storage (USB and eSATA
// disks with their partitions) and connected printers (with the global
// Bonjour/AirPrint sharing toggle).
//
// Three independent DSM API families back these areas, each read and gated in
// isolation so a NAS missing one keeps the others usable (WI-076):
//
//   - USB storage:   SYNO.Core.ExternalDevice.Storage.USB   v1 `list` -> {devices:[...]}
//   - eSATA storage: SYNO.Core.ExternalDevice.Storage.eSATA v1 `list` -> {devices:[...]}
//   - Printers:      SYNO.Core.ExternalDevice.Printer       v1 `list` -> {printers:[...]}
//     plus the global Bonjour/AirPrint sharing toggle read from
//     SYNO.Core.ExternalDevice.Printer.BonjourSharing v1 `get`.
//
// The API families, method names (`list` for storage/printers, `get` for
// Bonjour sharing), versions, and the top-level envelope keys (`devices`,
// `printers`, `enable_bonjour_support`) were live-verified against the lab
// (DS3018xs, DSM 7.3): USB storage, eSATA storage, and all four printer
// sub-families are advertised, but no external disk or printer was attached, so
// every list returned empty. The graceful no-device path (an area that is
// supported but has nothing attached) is therefore the live-verified path.
//
// The PER-DEVICE, PER-PARTITION, and PER-PRINTER item fields could NOT be
// observed live (nothing was attached) and are decoded tolerantly through their
// known DSM key names and alternates rather than fabricated — the same approach
// the hardware power-schedule tasks use when the lab reports an empty list.
// Those item fields are wire-unverified; the decoders never fail on an unknown
// item shape and simply omit fields they cannot read.
//
// UPS (SYNO.Core.ExternalDevice.UPS) is deliberately NOT part of this module: it
// belongs to the Hardware & Power surface and ships in the hardware module
// (WI-075). This is a read model only; guarded eject / printer set / spooler
// clear are WI-076 Slice B.
package externaldevice

// ExternalStoragePartition is one partition on an attached external disk. All
// fields are wire-unverified (no disk was attached on the lab) and read through
// their known DSM key alternates.
type ExternalStoragePartition struct {
	Name        string `json:"name,omitempty" jsonschema:"Partition device name or title (DSM part_title / name_id / dev_path)"`
	Filesystem  string `json:"filesystem,omitempty" jsonschema:"Partition filesystem such as ext4, ext3, btrfs, ntfs, fat32, or exfat"`
	TotalSizeMB int    `json:"total_size_mb,omitempty" jsonschema:"Partition total size in megabytes"`
	UsedSizeMB  int    `json:"used_size_mb,omitempty" jsonschema:"Partition used size in megabytes"`
	MountPoint  string `json:"mount_point,omitempty" jsonschema:"Where DSM has mounted the partition, when mounted"`
	ShareName   string `json:"share_name,omitempty" jsonschema:"Auto-created share name exposing this partition, when shared"`
	Status      string `json:"status,omitempty" jsonschema:"DSM raw partition status (e.g. normal); model dependent"`
}

// ExternalStorageDevice is one attached external disk (a USB or eSATA device)
// and its partitions. The device identity (DevID/DevPath) is the eject key a
// future guarded eject (Slice B) would bind. All item fields are wire-unverified
// and read tolerantly.
type ExternalStorageDevice struct {
	DevID       string                     `json:"dev_id,omitempty" jsonschema:"DSM device id such as usb1 or sata1"`
	DevPath     string                     `json:"dev_path,omitempty" jsonschema:"DSM device path such as /dev/usb1"`
	Type        string                     `json:"type,omitempty" jsonschema:"DSM raw device type such as usbDisk or sata"`
	Title       string                     `json:"title,omitempty" jsonschema:"Human-readable device title DSM shows in Control Panel"`
	Product     string                     `json:"product,omitempty" jsonschema:"Device product name, when reported"`
	Vendor      string                     `json:"vendor,omitempty" jsonschema:"Device manufacturer/vendor, when reported"`
	Serial      string                     `json:"serial,omitempty" jsonschema:"Device serial number when reported; model-identifying inventory data, not a secret"`
	Status      string                     `json:"status,omitempty" jsonschema:"DSM raw device status; model dependent"`
	TotalSizeMB int                        `json:"total_size_mb,omitempty" jsonschema:"Whole-device total size in megabytes"`
	Partitions  []ExternalStoragePartition `json:"partitions" jsonschema:"Partitions on the device"`
}

// ExternalStorageArea is the device list for one storage bus (USB or eSATA). It
// is present in StorageState only when that bus's DSM API is available; an empty
// Devices slice with the area present is the live-verified no-disk path.
type ExternalStorageArea struct {
	Devices []ExternalStorageDevice `json:"devices" jsonschema:"Attached external disks on this bus; empty when none is connected"`
}

// StorageState is the combined external-storage read. Each bus is gated
// independently: a nil sub-area means that bus's DSM API is absent (for example
// a model with no eSATA port), reported (not supported) without disabling the
// other bus.
type StorageState struct {
	USB   *ExternalStorageArea `json:"usb,omitempty" jsonschema:"USB external-storage devices; absent when the USB storage API is unavailable"`
	ESATA *ExternalStorageArea `json:"esata,omitempty" jsonschema:"eSATA external-storage devices; absent when the eSATA storage API is unavailable"`
}

// Printer is one connected printer DSM enumerates. All item fields are
// wire-unverified (no printer was attached on the lab) and read tolerantly.
type Printer struct {
	ID           string `json:"id,omitempty" jsonschema:"DSM printer id"`
	Name         string `json:"name,omitempty" jsonschema:"Printer display name"`
	Type         string `json:"type,omitempty" jsonschema:"Printer connection type such as usb or network"`
	Status       string `json:"status,omitempty" jsonschema:"DSM raw printer/spooler status; model dependent"`
	Manager      string `json:"manager,omitempty" jsonschema:"DSM user managing the printer, when set"`
	Default      bool   `json:"default" jsonschema:"Whether this is the default printer"`
	SpoolerCount int    `json:"spooler_count,omitempty" jsonschema:"Number of jobs queued in the printer spooler, when reported"`
}

// PrinterSharing is the global print-sharing configuration read from
// SYNO.Core.ExternalDevice.Printer.BonjourSharing.get. The Bonjour/AirPrint
// enable flag was live-verified on the lab.
type PrinterSharing struct {
	BonjourEnabled bool `json:"bonjour_enabled" jsonschema:"Whether Bonjour/AirPrint printer sharing is enabled (enable_bonjour_support)"`
}

// PrinterState is the combined printer read: the enumerated printers plus the
// global sharing toggle. Sharing is nil when the Bonjour-sharing API is absent,
// gated independently of the printer list.
type PrinterState struct {
	Printers []Printer       `json:"printers" jsonschema:"Connected printers; empty when none is connected"`
	Sharing  *PrinterSharing `json:"sharing,omitempty" jsonschema:"Global Bonjour/AirPrint printer-sharing toggle; absent when its DSM API is unavailable"`
}

// Capabilities reports which External Devices read areas the NAS exposes. Each
// area selects its backend independently; a missing API family fails closed for
// its own area only and never disables the others. UPS is intentionally absent
// here — it belongs to the Hardware & Power module (WI-075).
type Capabilities struct {
	USBStorage     bool `json:"usb_storage" jsonschema:"Whether USB external storage can be read (SYNO.Core.ExternalDevice.Storage.USB)"`
	ESATAStorage   bool `json:"esata_storage" jsonschema:"Whether eSATA external storage can be read (SYNO.Core.ExternalDevice.Storage.eSATA)"`
	Printer        bool `json:"printer" jsonschema:"Whether connected printers can be read (SYNO.Core.ExternalDevice.Printer)"`
	PrinterSharing bool `json:"printer_sharing" jsonschema:"Whether the Bonjour/AirPrint printer-sharing toggle can be read (SYNO.Core.ExternalDevice.Printer.BonjourSharing)"`
}
