package disksmart

import "testing"

// The fixtures below are sanitized captures from DSM 7.3 (SYNO.Core.Storage.Disk
// and SYNO.Storage.CGI.Smart). Real disk serial numbers are stable hardware
// identifiers and are replaced with fake values (TESTDISK*) per the WI-002
// evidence policy.

func TestDecodeDiskHealthList(t *testing.T) {
	disks, err := decodeDiskHealthList([]byte(`{
		"disks": [
			{
				"id": "sda", "device": "/dev/sda", "name": "Drive 1", "longName": "Drive 1",
				"model": "TESTSSD240G", "firm": "TF10", "vendor": "TESTVEN ", "serial": "TESTDISK0001",
				"diskType": "SATA", "isSsd": true, "slot_id": 1, "disk_location": "Main",
				"size_total": "240057409536", "temp": 29, "status": "not_use",
				"overview_status": "normal", "drive_status_key": "normal", "smart_status": "normal",
				"smart_test_support": true, "smart_testing": false, "testing_type": "idle",
				"remain_life": {"trustable": true, "value": 99}, "remain_life_danger": false,
				"below_remain_life_thr": false, "sb_days_left": 0, "sb_days_left_critical": false,
				"sb_days_left_warning": false, "unc": -1,
				"container": {"str": "TestNAS", "type": "internal"}
			},
			{
				"id": "sdc", "device": "/dev/sdc", "name": "Drive 3",
				"model": "TESTHDD4T", "serial": "TESTDISK0002", "isSsd": false,
				"overview_status": "normal", "smart_status": "normal", "smart_test_support": true,
				"remain_life": {"trustable": true, "value": -1}, "unc": 0
			}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(disks) != 2 {
		t.Fatalf("disk count = %d, want 2", len(disks))
	}
	sda := disks[0]
	if sda.ID != "sda" || sda.Device != "/dev/sda" || sda.Name != "Drive 1" {
		t.Fatalf("sda identity = %#v", sda)
	}
	if sda.Type != "SSD" || sda.Interface != "SATA" || sda.Unit != "TestNAS" {
		t.Fatalf("sda media/unit = %#v", sda)
	}
	if sda.Health != "normal" || sda.SMARTStatus != "normal" || !sda.SMARTSupported {
		t.Fatalf("sda health = %#v", sda)
	}
	if sda.RemainingLifePercent == nil || *sda.RemainingLifePercent != 99 || !sda.RemainingLifeTrustable {
		t.Fatalf("sda remaining life = %#v", sda)
	}
	if sda.TemperatureC == nil || *sda.TemperatureC != 29 {
		t.Fatalf("sda temperature = %#v", sda.TemperatureC)
	}
	if sda.UncorrectableCount != -1 {
		t.Fatalf("sda unc = %d, want -1 sentinel", sda.UncorrectableCount)
	}
	hdd := disks[1]
	if hdd.Type != "HDD" || !hdd.SMARTSupported {
		t.Fatalf("sdc media/support = %#v", hdd)
	}
	if hdd.UncorrectableCount != 0 {
		t.Fatalf("sdc unc = %d, want real 0", hdd.UncorrectableCount)
	}
	// An HDD reports remaining-life value -1 (not applicable) -> nil, not -1%.
	if hdd.RemainingLifePercent != nil {
		t.Fatalf("sdc remaining life = %v, want nil for the -1 sentinel", *hdd.RemainingLifePercent)
	}
}

func TestDecodeThresholds(t *testing.T) {
	thresholds, err := decodeThresholds([]byte(`{
		"BadSctrThrEn": true, "RemainLifeThrEn": false, "RemainLifeThrVal": 5,
		"SBMonthLeftThrEn": true, "SBMonthLeftThrVal": 1, "WddaEn": false,
		"healthReportEn": true, "chkMailSetting": false, "db_last_update_time": 1779940698
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !thresholds.BadSectorThresholdEnabled || thresholds.RemainingLifeThresholdEnabled {
		t.Fatalf("threshold enables = %#v", thresholds)
	}
	if thresholds.RemainingLifeThresholdPercent != 5 || thresholds.SpareBlockMonthsThreshold != 1 {
		t.Fatalf("threshold values = %#v", thresholds)
	}
	if !thresholds.SpareBlockMonthsThresholdEnabled || !thresholds.HealthReportEnabled || thresholds.WriteDurabilityAssuranceEnabled {
		t.Fatalf("threshold flags = %#v", thresholds)
	}
}

func TestDecodeDiskSMART(t *testing.T) {
	smart, err := decodeDiskSMART([]byte(`{
		"healthInfo": {
			"count": 3,
			"overview": {
				"smart": "normal", "smart_info": "normal", "smart_test": "normal",
				"remain_life_attr": "Media_Wearout_Indicator", "isNVMeDisk": false, "isSsd": true,
				"remain_life": {"trustable": true, "value": 99}
			},
			"smartInfo": [
				{"id": "5", "name": "Reallocated_Sector_Ct", "current": "100", "worst": "100", "threshold": "000", "raw": "0", "status": "OK"},
				{"id": "9", "name": "Power_On_Hours", "current": "100", "worst": "100", "threshold": "000", "raw": "46327", "status": "OK"},
				{"id": "194", "name": "Temperature_Celsius", "current": "070", "worst": "060", "threshold": "000", "raw": "30", "status": "OK"}
			]
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if smart.OverallStatus != "normal" || smart.SMARTTestStatus != "normal" || smart.RemainingLifeAttribute != "Media_Wearout_Indicator" {
		t.Fatalf("smart summary = %#v", smart)
	}
	if smart.IsNVMe || !smart.IsSSD {
		t.Fatalf("smart media = %#v", smart)
	}
	if smart.RemainingLifePercent == nil || *smart.RemainingLifePercent != 99 {
		t.Fatalf("smart remaining life = %#v", smart)
	}
	if smart.AttributeCount != 3 || len(smart.Attributes) != 3 {
		t.Fatalf("attribute count = %d / %d", smart.AttributeCount, len(smart.Attributes))
	}
	first := smart.Attributes[0]
	if first.ID != "5" || first.Name != "Reallocated_Sector_Ct" || first.Current != "100" || first.Worst != "100" || first.Threshold != "000" || first.Raw != "0" || first.Status != "OK" {
		t.Fatalf("first attribute = %#v", first)
	}
}

func TestDecodeDiskSMARTFallsBackToAttributeLength(t *testing.T) {
	// When count is absent the decoder falls back to the row count.
	smart, err := decodeDiskSMART([]byte(`{"healthInfo":{"smartInfo":[{"id":"5"},{"id":"9"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if smart.AttributeCount != 2 {
		t.Fatalf("attribute count fallback = %d, want 2", smart.AttributeCount)
	}
}

func TestDecodeTestStatus(t *testing.T) {
	status, err := decodeTestStatus([]byte(`{
		"latest_test_time": "2026/07/20 03:00",
		"testInfo": [
			{"device": "/dev/sdb", "latest_test_result": "aborted", "latest_test_type": 2, "testing": true, "remain": "40%", "quickTime": "   1", "extendTime": "   2"},
			{"device": "/dev/sda", "latest_test_result": "completed", "latest_test_type": 1, "testing": false, "remain": "", "quickTime": "   1", "extendTime": "   2"}
		]
	}`), "/dev/sda")
	if err != nil {
		t.Fatal(err)
	}
	// The decoder selects the entry matching the requested device, not the first.
	if status.LatestResult != "completed" || status.LatestType != 1 || status.Testing {
		t.Fatalf("selected wrong entry: %#v", status)
	}
	if status.QuickEstimate != "1" || status.ExtendedEstimate != "2" {
		t.Fatalf("estimates not trimmed: %#v", status)
	}
	if status.LatestTime != "2026/07/20 03:00" {
		t.Fatalf("latest time = %q", status.LatestTime)
	}
}

func TestDecodeDiskHealthListRejectsMalformed(t *testing.T) {
	if _, err := decodeDiskHealthList([]byte(`not json`)); err == nil {
		t.Fatal("expected error for malformed disk list")
	}
	if _, err := decodeThresholds([]byte(`[]`)); err == nil {
		t.Fatal("expected error decoding a non-object thresholds payload")
	}
	if _, err := decodeDiskSMART([]byte(`"nope"`)); err == nil {
		t.Fatal("expected error decoding a non-object SMART payload")
	}
}
