package hardware

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

// TestReadGeneralIndependentSubGating proves the three comfort sub-areas are
// gated independently: a NAS exposing only beep control still returns beep and
// omits the absent fan and LED areas instead of failing.
func TestReadGeneralIndependentSubGating(t *testing.T) {
	target := targetWith(t, BeepAPI) // no fan, no LED
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == BeepAPI && request.Method == "get" {
			return json.RawMessage(`{"fan_fail":true,"support_fan_fail":true}`), nil
		}
		t.Fatalf("no request expected for absent areas, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, selection, err := ReadGeneral(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Supported {
		t.Fatalf("beep selection should be supported: %#v", selection)
	}
	if state.Beep == nil || state.Fan != nil || state.LED != nil {
		t.Fatalf("only beep should be present: %#v", state)
	}
}

// TestReadGeneralAllPresent proves all three comfort areas are read when their
// APIs are present.
func TestReadGeneralAllPresent(t *testing.T) {
	target := targetWith(t, BeepAPI, FanAPI, LEDAPI)
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.API {
		case BeepAPI:
			return json.RawMessage(`{"fan_fail":true,"support_fan_fail":true}`), nil
		case FanAPI:
			return json.RawMessage(`{"dual_fan_speed":"coolfan"}`), nil
		case LEDAPI:
			return json.RawMessage(`{"led_brightness":2}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, _, err := ReadGeneral(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if state.Beep == nil || state.Fan == nil || state.LED == nil {
		t.Fatalf("all three areas expected: %#v", state)
	}
	if state.Fan.Mode != "coolfan" || state.LED.Brightness != 2 {
		t.Fatalf("general state = %#v", state)
	}
}

// TestReadGeneralNoComfortAPIs proves general read succeeds and returns an empty
// state when the model exposes none of the three comfort APIs.
func TestReadGeneralNoComfortAPIs(t *testing.T) {
	target := targetWith(t) // nothing
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		t.Fatalf("no request expected, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	state, selection, err := ReadGeneral(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Supported {
		t.Fatalf("beep selection should be unsupported: %#v", selection)
	}
	if state.Beep != nil || state.Fan != nil || state.LED != nil {
		t.Fatalf("empty state expected: %#v", state)
	}
}

// TestPowerScheduleFailsClosed proves the power-schedule area fails closed when
// its API is absent, independently of the other areas.
func TestPowerScheduleFailsClosed(t *testing.T) {
	target := targetWith(t, BeepAPI) // no PowerSchedule
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		t.Fatalf("no request expected when PowerSchedule is absent, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	_, selection, err := ReadPowerSchedule(context.Background(), target, executor)
	if !compatibility.IsUnsupported(err) {
		t.Fatalf("expected unsupported error, got %v", err)
	}
	if selection.Supported {
		t.Fatalf("selection should be unsupported: %#v", selection)
	}
}

// TestUPSPresentWithoutDevice proves the UPS area is supported (API present)
// even when no UPS device is attached, and reports the no-device state.
func TestUPSPresentWithoutDevice(t *testing.T) {
	target := targetWith(t, UPSAPI)
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		if request.API == UPSAPI && request.Method == "get" {
			return json.RawMessage(`{"enable":false,"usb_ups_connect":false,"status":"usb_ups_status_unknown","mode":"SLAVE"}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	ups, selection, err := ReadUPS(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.Supported {
		t.Fatalf("UPS API present should select supported: %#v", selection)
	}
	if ups.Enabled || ups.USBConnected {
		t.Fatalf("no-device UPS state = %#v", ups)
	}
}

// TestSelectIndependentGating proves each area selects its own backend so a
// missing API family fails closed only for its own area.
func TestSelectIndependentGating(t *testing.T) {
	target := targetWith(t, BeepAPI, UPSAPI) // beep + UPS only
	cases := []struct {
		name    string
		selectF func(compatibility.Target) (compatibility.Selection, error)
		want    bool
	}{
		{"beep", SelectBeep, true},
		{"fan", SelectFan, false},
		{"led", SelectLED, false},
		{"power_schedule", SelectPowerSchedule, false},
		{"power_recovery", SelectPowerRecovery, false},
		{"ups", SelectUPS, true},
	}
	for _, tc := range cases {
		selection, _ := tc.selectF(target)
		if selection.Supported != tc.want {
			t.Fatalf("%s supported = %v, want %v (%#v)", tc.name, selection.Supported, tc.want, selection)
		}
	}
}
