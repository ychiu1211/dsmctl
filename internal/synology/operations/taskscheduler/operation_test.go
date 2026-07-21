package taskscheduler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type recordingExecutor struct {
	responses map[string]json.RawMessage
	requests  []compatibility.Request
}

func (e *recordingExecutor) Execute(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
	e.requests = append(e.requests, request)
	key := request.API + "." + request.Method
	if resp, ok := e.responses[key]; ok {
		return resp, nil
	}
	return json.RawMessage(`{}`), nil
}

func (e *recordingExecutor) ExecuteScript(context.Context, compatibility.Request) ([]byte, error) {
	return nil, nil
}

// scheduledTarget advertises TaskScheduler v1-3 and EventScheduler v1, matching
// the live lab discovery.
func fullTarget() compatibility.Target {
	target := compatibility.NewTarget()
	target.SetAPI(ScheduledAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	target.SetAPI(TriggeredAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	return target
}

func TestSelectorsRequireTheirAPI(t *testing.T) {
	full := fullTarget()
	empty := compatibility.NewTarget()
	cases := []struct {
		name    string
		backend string
		selectF func(compatibility.Target) (compatibility.Selection, error)
	}{
		{"scheduled", "task-scheduler-list-v3", SelectScheduled},
		{"triggered", "event-scheduler-list-v1", SelectTriggered},
	}
	for _, tc := range cases {
		selection, err := tc.selectF(full)
		if err != nil || !selection.Supported || selection.Backend != tc.backend {
			t.Fatalf("%s: selection=%#v err=%v", tc.name, selection, err)
		}
		selection, err = tc.selectF(empty)
		if !compatibility.IsUnsupported(err) || selection.Supported {
			t.Fatalf("%s: expected unsupported, got selection=%#v err=%v", tc.name, selection, err)
		}
	}
}

// TestScheduledPrefersHighestVersion proves the v3 variant wins when advertised
// but the read still works on an older-only NAS.
func TestScheduledPrefersHighestVersion(t *testing.T) {
	target := compatibility.NewTarget()
	target.SetAPI(ScheduledAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	selection, err := SelectScheduled(target)
	if err != nil || selection.Backend != "task-scheduler-list-v1" {
		t.Fatalf("v1-only selection = %#v err=%v", selection, err)
	}
}

// TestIndependentBoundaries proves one family being absent never disables the
// other: a target with only the EventScheduler API reports triggered supported
// and scheduled unsupported, and vice versa.
func TestIndependentBoundaries(t *testing.T) {
	triggeredOnly := compatibility.NewTarget()
	triggeredOnly.SetAPI(TriggeredAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	if sel, _ := SelectTriggered(triggeredOnly); !sel.Supported {
		t.Fatalf("triggered should be supported with only EventScheduler")
	}
	if sel, err := SelectScheduled(triggeredOnly); sel.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("scheduled should be unsupported without TaskScheduler: %#v", sel)
	}

	scheduledOnly := compatibility.NewTarget()
	scheduledOnly.SetAPI(ScheduledAPIName, compatibility.APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	if sel, _ := SelectScheduled(scheduledOnly); !sel.Supported {
		t.Fatalf("scheduled should be supported with only TaskScheduler")
	}
	if sel, err := SelectTriggered(scheduledOnly); sel.Supported || !compatibility.IsUnsupported(err) {
		t.Fatalf("triggered should be unsupported without EventScheduler: %#v", sel)
	}
}

func TestExecuteScheduledDecodesLiveEmptyShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		ScheduledAPIName + ".list": json.RawMessage(`{"tasks":[],"total":0}`),
	}}
	tasks, selection, err := ExecuteScheduled(context.Background(), fullTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteScheduled() error = %v", err)
	}
	if selection.Backend != "task-scheduler-list-v3" || selection.Version != 3 {
		t.Fatalf("selection = %#v", selection)
	}
	if tasks.Total != 0 || len(tasks.Tasks) != 0 {
		t.Fatalf("tasks = %#v", tasks)
	}
	req := exec.requests[0]
	if req.API != ScheduledAPIName || req.Method != "list" || req.Version != 3 || !req.ReadOnly {
		t.Fatalf("request = %#v", req)
	}
}

func TestExecuteTriggeredDecodesLiveEmptyShape(t *testing.T) {
	exec := &recordingExecutor{responses: map[string]json.RawMessage{
		TriggeredAPIName + ".list": json.RawMessage(`[]`),
	}}
	tasks, selection, err := ExecuteTriggered(context.Background(), fullTarget(), exec)
	if err != nil {
		t.Fatalf("ExecuteTriggered() error = %v", err)
	}
	if selection.Backend != "event-scheduler-list-v1" {
		t.Fatalf("selection = %#v", selection)
	}
	if len(tasks.Tasks) != 0 {
		t.Fatalf("tasks = %#v", tasks)
	}
	req := exec.requests[0]
	if req.API != TriggeredAPIName || req.Method != "list" || req.Version != 1 || !req.ReadOnly {
		t.Fatalf("request = %#v", req)
	}
}
