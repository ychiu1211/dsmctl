package externaldevice

import (
	"encoding/json"
	"testing"
)

// The empty-envelope fixtures below are the live DSM 7.3 (DS3018xs) responses
// captured with a read-only probe: the lab advertises every External Devices
// family but had no disk or printer attached, so each list returned empty. The
// per-item fixtures are SYNTHETIC (sanitized fake identity, RFC-5737-safe) —
// the item shape could not be observed live and is decoded tolerantly.

func TestDecodeStorageAreaEmpty(t *testing.T) {
	area, err := decodeStorageArea(json.RawMessage(`{"devices":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if area.Devices == nil || len(area.Devices) != 0 {
		t.Fatalf("empty device list expected, got %#v", area.Devices)
	}
}

func TestDecodeStorageAreaMalformed(t *testing.T) {
	if _, err := decodeStorageArea(json.RawMessage(`["not","an","object"]`)); err == nil {
		t.Fatal("a non-object top-level response must be an error, not a silent empty success")
	}
}

// TestDecodeStorageDevice proves the tolerant per-device / per-partition decode:
// the item shape was not observable live, so the decoder reads DSM's documented
// keys into the model. Fixture identity is fake.
func TestDecodeStorageDevice(t *testing.T) {
	raw := `{"devices":[{
		"dev_id":"usb1","dev_path":"/dev/usb1","dev_type":"usbDisk",
		"dev_title":"USB Disk 1","product":"TestDisk","vendor":"TestVendor",
		"serial":"TESTSERIAL123","status":"normal","total_size_mb":15000,
		"partitions":[{
			"part_title":"USB Disk 1 Partition 1","filesystem":"ntfs",
			"total_size_mb":15000,"used_size_mb":4200,"mount_point":"/volumeUSB1/usbshare",
			"share_name":"usbshare1","status":"normal"
		}]
	}]}`
	area, err := decodeStorageArea(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(area.Devices) != 1 {
		t.Fatalf("device count = %d", len(area.Devices))
	}
	device := area.Devices[0]
	if device.DevID != "usb1" || device.DevPath != "/dev/usb1" || device.Type != "usbDisk" {
		t.Fatalf("device identity = %#v", device)
	}
	if device.Title != "USB Disk 1" || device.Product != "TestDisk" || device.Vendor != "TestVendor" {
		t.Fatalf("device descriptive fields = %#v", device)
	}
	if device.Serial != "TESTSERIAL123" || device.TotalSizeMB != 15000 || device.Status != "normal" {
		t.Fatalf("device = %#v", device)
	}
	if len(device.Partitions) != 1 {
		t.Fatalf("partition count = %d", len(device.Partitions))
	}
	part := device.Partitions[0]
	if part.Filesystem != "ntfs" || part.TotalSizeMB != 15000 || part.UsedSizeMB != 4200 {
		t.Fatalf("partition = %#v", part)
	}
	if part.MountPoint != "/volumeUSB1/usbshare" || part.ShareName != "usbshare1" || part.Status != "normal" {
		t.Fatalf("partition mount/share = %#v", part)
	}
}

// TestDecodeStorageDeviceAlternateKeys proves DSM's alternate item key names
// (a build that renamed a key) still decode into the same model.
func TestDecodeStorageDeviceAlternateKeys(t *testing.T) {
	raw := `{"devices":[{
		"id":"sata1","path":"/dev/sata1","type":"sata","name":"eSATA Disk",
		"producer":"AltVendor","model":"AltModel","dev_size_mb":8000,
		"partitions":[{"name_id":"sata1p1","dev_fstype":"ext4","total_size":8000,"used_size":100,"mountpoint":"/mnt/x"}]
	}]}`
	area, err := decodeStorageArea(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	device := area.Devices[0]
	if device.DevID != "sata1" || device.DevPath != "/dev/sata1" || device.Type != "sata" {
		t.Fatalf("alternate identity keys = %#v", device)
	}
	if device.Vendor != "AltVendor" || device.Product != "AltModel" || device.TotalSizeMB != 8000 {
		t.Fatalf("alternate descriptive keys = %#v", device)
	}
	part := device.Partitions[0]
	if part.Name != "sata1p1" || part.Filesystem != "ext4" || part.TotalSizeMB != 8000 || part.MountPoint != "/mnt/x" {
		t.Fatalf("alternate partition keys = %#v", part)
	}
}

func TestDecodePrintersEmpty(t *testing.T) {
	printers, err := decodePrinters(json.RawMessage(`{"printers":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if printers == nil || len(printers) != 0 {
		t.Fatalf("empty printer list expected, got %#v", printers)
	}
}

func TestDecodePrinters(t *testing.T) {
	raw := `{"printers":[
		{"id":"printer1","name":"Test Printer","type":"usb","status":"idle","manager":"testuser","default":true,"spooler_count":2}
	]}`
	printers, err := decodePrinters(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(printers) != 1 {
		t.Fatalf("printer count = %d", len(printers))
	}
	printer := printers[0]
	if printer.ID != "printer1" || printer.Name != "Test Printer" || printer.Type != "usb" {
		t.Fatalf("printer identity = %#v", printer)
	}
	if printer.Status != "idle" || printer.Manager != "testuser" || !printer.Default || printer.SpoolerCount != 2 {
		t.Fatalf("printer = %#v", printer)
	}
}

func TestDecodePrintersMalformed(t *testing.T) {
	if _, err := decodePrinters(json.RawMessage(`42`)); err == nil {
		t.Fatal("a non-object printer response must be an error")
	}
}

// TestDecodePrinterSharing covers the live-verified Bonjour toggle in both
// states.
func TestDecodePrinterSharing(t *testing.T) {
	off, err := decodePrinterSharing(json.RawMessage(`{"enable_bonjour_support":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if off.BonjourEnabled {
		t.Fatalf("bonjour should be off: %#v", off)
	}
	on, err := decodePrinterSharing(json.RawMessage(`{"enable_bonjour_support":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !on.BonjourEnabled {
		t.Fatalf("bonjour should be on: %#v", on)
	}
}
