package externaldevice

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

func targetWith(t *testing.T, apis ...string) compatibility.Target {
	t.Helper()
	target := compatibility.NewTarget()
	for _, name := range apis {
		target.SetAPI(name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	}
	return target
}

// TestReadStorageIndependentGating proves the two buses are gated independently:
// a NAS exposing only USB storage still returns USB and omits the absent eSATA
// bus instead of failing (many models have no eSATA port).
func TestReadStorageIndependentGating(t *testing.T) {
	target := targetWith(t, USBStorageAPI) // no eSATA
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == USBStorageAPI && request.Method == "list" {
			return json.RawMessage(`{"devices":[]}`), nil
		}
		t.Fatalf("no request expected for absent eSATA bus, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, selection, err := ReadStorage(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Supported {
		t.Fatalf("USB selection should be supported: %#v", selection)
	}
	if state.USB == nil || state.ESATA != nil {
		t.Fatalf("only USB should be present: %#v", state)
	}
}

// TestReadStorageBothPresent proves both buses are read when both APIs exist,
// each returning its (live-verified empty) device list.
func TestReadStorageBothPresent(t *testing.T) {
	target := targetWith(t, USBStorageAPI, ESATAStorageAPI)
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.API {
		case USBStorageAPI:
			return json.RawMessage(`{"devices":[]}`), nil
		case ESATAStorageAPI:
			return json.RawMessage(`{"devices":[]}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, _, err := ReadStorage(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if state.USB == nil || state.ESATA == nil {
		t.Fatalf("both buses expected: %#v", state)
	}
}

// TestReadStorageNoAPIs proves the storage read succeeds and returns an empty
// state when the model exposes neither storage bus.
func TestReadStorageNoAPIs(t *testing.T) {
	target := targetWith(t)
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		t.Fatalf("no request expected, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, selection, err := ReadStorage(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Supported {
		t.Fatalf("USB selection should be unsupported: %#v", selection)
	}
	if state.USB != nil || state.ESATA != nil {
		t.Fatalf("empty state expected: %#v", state)
	}
}

// TestPrinterFailsClosed proves the printer area fails closed when its API is
// absent, independently of the storage areas.
func TestPrinterFailsClosed(t *testing.T) {
	target := targetWith(t, USBStorageAPI) // no printer
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		t.Fatalf("no request expected when Printer is absent, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	_, selection, err := ReadPrinters(context.Background(), target, executor)
	if !compatibility.IsUnsupported(err) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
	if selection.Supported {
		t.Fatalf("selection should be unsupported: %#v", selection)
	}
}

// TestReadPrintersWithSharing proves the printer list and the Bonjour sharing
// toggle are read together when both APIs exist.
func TestReadPrintersWithSharing(t *testing.T) {
	target := targetWith(t, PrinterAPI, PrinterBonjourAPI)
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.API {
		case PrinterAPI:
			return json.RawMessage(`{"printers":[]}`), nil
		case PrinterBonjourAPI:
			return json.RawMessage(`{"enable_bonjour_support":true}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, selection, err := ReadPrinters(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Supported {
		t.Fatalf("printer selection should be supported: %#v", selection)
	}
	if state.Sharing == nil || !state.Sharing.BonjourEnabled {
		t.Fatalf("sharing toggle should be present and on: %#v", state.Sharing)
	}
}

// TestReadPrintersSharingIndependentlyAbsent proves the printer list still reads
// when the Bonjour-sharing API is absent (sharing omitted, not an error).
func TestReadPrintersSharingIndependentlyAbsent(t *testing.T) {
	target := targetWith(t, PrinterAPI) // no BonjourSharing
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == PrinterAPI && request.Method == "list" {
			return json.RawMessage(`{"printers":[]}`), nil
		}
		t.Fatalf("no request expected for absent sharing API, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, _, err := ReadPrinters(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if state.Sharing != nil {
		t.Fatalf("sharing should be omitted when its API is absent: %#v", state.Sharing)
	}
}

// TestSelectIndependentGating proves each area selects its own backend so a
// missing API family fails closed only for its own area.
func TestSelectIndependentGating(t *testing.T) {
	target := targetWith(t, USBStorageAPI, PrinterAPI) // USB storage + printer only
	cases := []struct {
		name    string
		selectF func(compatibility.Target) (compatibility.Selection, error)
		want    bool
	}{
		{"usb_storage", SelectUSBStorage, true},
		{"esata_storage", SelectESATAStorage, false},
		{"printer", SelectPrinter, true},
		{"printer_sharing", SelectPrinterSharing, false},
	}
	for _, tc := range cases {
		selection, _ := tc.selectF(target)
		if selection.Supported != tc.want {
			t.Fatalf("%s supported = %v, want %v (%#v)", tc.name, selection.Supported, tc.want, selection)
		}
	}
}
