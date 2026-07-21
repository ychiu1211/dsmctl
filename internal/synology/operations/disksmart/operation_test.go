package disksmart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

// codedError mimics the transport APIError: it carries a DSM application code so
// compatibility.APIErrorCode can classify it.
type codedError struct{ code int }

func (e codedError) Error() string     { return fmt.Sprintf("dsm code %d", e.code) }
func (e codedError) DSMErrorCode() int { return e.code }

func targetWith(t *testing.T, apis ...string) compatibility.Target {
	t.Helper()
	target := compatibility.NewTarget()
	for _, name := range apis {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	}
	return target
}

const twoDiskList = `{"disks":[
	{"id":"sda","device":"/dev/sda","name":"Drive 1","isSsd":true,"overview_status":"normal","smart_status":"normal","smart_test_support":true},
	{"id":"sdb","device":"/dev/sdb","name":"Drive 2","isSsd":true,"overview_status":"normal","smart_status":"normal","smart_test_support":true}
]}`

const oneAttrSet = `{"healthInfo":{"count":1,"overview":{"smart":"normal","isSsd":true},"smartInfo":[
	{"id":"5","name":"Reallocated_Sector_Ct","current":"100","worst":"100","threshold":"000","raw":"0","status":"OK"}
]}}`

const testLog = `{"latest_test_time":"","testInfo":[{"device":"/dev/sdb","latest_test_result":"completed","latest_test_type":1,"testing":false}]}`

// TestReadSMARTGracefulAbsence proves a per-disk DSM error (code 117) marks that
// disk "no SMART data" without failing the whole read, while the other disk's
// attribute table and self-test status are returned.
func TestReadSMARTGracefulAbsence(t *testing.T) {
	target := targetWith(t, CoreDiskAPI, SmartAPI)
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch {
		case request.API == CoreDiskAPI && request.Method == "list":
			return json.RawMessage(twoDiskList), nil
		case request.API == SmartAPI && request.Method == "get_health_info":
			if request.JSONParameters["device"] == "/dev/sda" {
				return nil, codedError{code: NoSMARTDataCode}
			}
			return json.RawMessage(oneAttrSet), nil
		case request.API == CoreDiskAPI && request.Method == "get_smart_test_log":
			return json.RawMessage(testLog), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})

	state, selection, err := ReadSMART(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Supported || selection.API != SmartAPI {
		t.Fatalf("selection = %#v", selection)
	}
	if len(state.Disks) != 2 {
		t.Fatalf("disk count = %d, want 2", len(state.Disks))
	}
	sda := state.Disks[0]
	if !sda.NoSMARTData || sda.AbsenceCode != NoSMARTDataCode || len(sda.Attributes) != 0 {
		t.Fatalf("sda no-smart-data path = %#v", sda)
	}
	if sda.ID != "sda" || sda.Device != "/dev/sda" || sda.Name != "Drive 1" {
		t.Fatalf("sda identity not overlaid: %#v", sda)
	}
	sdb := state.Disks[1]
	if sdb.NoSMARTData || len(sdb.Attributes) != 1 || sdb.Attributes[0].ID != "5" {
		t.Fatalf("sdb attributes = %#v", sdb)
	}
	if sdb.TestStatus == nil || sdb.TestStatus.LatestResult != "completed" {
		t.Fatalf("sdb test status = %#v", sdb.TestStatus)
	}
}

// TestReadSMARTPropagatesTransportError proves a non-DSM error (transport or
// session failure) on a per-disk read is NOT swallowed as "no SMART data".
func TestReadSMARTPropagatesTransportError(t *testing.T) {
	target := targetWith(t, CoreDiskAPI, SmartAPI)
	boom := errors.New("connection reset")
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == CoreDiskAPI && request.Method == "list" {
			return json.RawMessage(twoDiskList), nil
		}
		return nil, boom
	})
	if _, _, err := ReadSMART(context.Background(), target, executor); !errors.Is(err, boom) {
		t.Fatalf("expected the transport error to propagate, got %v", err)
	}
}

// TestReadSMARTUnsupportedFailsClosed proves the attribute area fails closed
// when SYNO.Storage.CGI.Smart is absent, without consulting the disk list.
func TestReadSMARTUnsupportedFailsClosed(t *testing.T) {
	target := targetWith(t, CoreDiskAPI) // no SmartAPI
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		t.Fatalf("no request expected when the attribute API is absent, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	_, selection, err := ReadSMART(context.Background(), target, executor)
	if !compatibility.IsUnsupported(err) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
	if selection.Supported {
		t.Fatalf("selection should be unsupported: %#v", selection)
	}
}

// TestReadHealthWithoutThresholds proves the health read succeeds and reports no
// thresholds when SYNO.Storage.CGI.HddMan is absent (independent gating).
func TestReadHealthWithoutThresholds(t *testing.T) {
	target := targetWith(t, CoreDiskAPI) // no HddMan
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == CoreDiskAPI && request.Method == "list" {
			return json.RawMessage(twoDiskList), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, selection, err := ReadHealth(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Supported || len(state.Disks) != 2 || state.Thresholds != nil {
		t.Fatalf("health state = %#v (selection %#v)", state, selection)
	}
}

// TestReadHealthWithThresholds proves the threshold enrichment attaches when
// SYNO.Storage.CGI.HddMan is present.
func TestReadHealthWithThresholds(t *testing.T) {
	target := targetWith(t, CoreDiskAPI, HddManAPI)
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch {
		case request.API == CoreDiskAPI && request.Method == "list":
			return json.RawMessage(twoDiskList), nil
		case request.API == HddManAPI && request.Method == "get":
			return json.RawMessage(`{"BadSctrThrEn":true,"RemainLifeThrEn":true,"RemainLifeThrVal":5,"healthReportEn":true}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, _, err := ReadHealth(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if state.Thresholds == nil || !state.Thresholds.BadSectorThresholdEnabled || state.Thresholds.RemainingLifeThresholdPercent != 5 {
		t.Fatalf("thresholds = %#v", state.Thresholds)
	}
}

// TestSelectIndependentGating proves each area selects its own backend so a
// missing API family fails closed only for its own area.
func TestSelectIndependentGating(t *testing.T) {
	target := targetWith(t, CoreDiskAPI) // health only
	if selection, _ := SelectHealth(target); !selection.Supported {
		t.Fatalf("health should be supported: %#v", selection)
	}
	if selection, _ := SelectAttributes(target); selection.Supported {
		t.Fatalf("attributes should be unsupported without SmartAPI: %#v", selection)
	}
	if selection, _ := SelectThresholds(target); selection.Supported {
		t.Fatalf("thresholds should be unsupported without HddManAPI: %#v", selection)
	}
}
