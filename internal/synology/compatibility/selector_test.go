package compatibility

import (
	"context"
	"encoding/json"
	"testing"
)

type executorFunc func(context.Context, Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request Request) (json.RawMessage, error) {
	return function(ctx, request)
}

func TestOperationSelectsHighestPriorityMatchingVariant(t *testing.T) {
	target := NewTarget()
	target.SetAPI("SYNO.Test", APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	operation := Operation[struct{}, string]{
		Name: "test.operation",
		Variants: []Variant[struct{}, string]{
			{Name: "v1", API: "SYNO.Test", Version: 1, Priority: 10, Match: APIVersion("SYNO.Test", 1), Execute: returnName("v1")},
			{Name: "v3", API: "SYNO.Test", Version: 3, Priority: 30, Match: APIVersion("SYNO.Test", 3), Execute: returnName("v3")},
			{Name: "v4", API: "SYNO.Test", Version: 4, Priority: 40, Match: APIVersion("SYNO.Test", 4), Execute: returnName("v4")},
		},
	}
	result, selection, err := operation.Run(context.Background(), target, executorFunc(func(context.Context, Request) (json.RawMessage, error) {
		return nil, nil
	}), struct{}{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != "v3" || selection.Backend != "v3" || selection.Version != 3 {
		t.Fatalf("result=%q selection=%#v", result, selection)
	}
}

func TestDSMReleaseOverrideBeatsCommonAPIVariant(t *testing.T) {
	target := NewTarget()
	target.DSM = ParseDSMVersion("DSM 8.0.0-10000")
	target.SetAPI("SYNO.Test", APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 3})
	operation := Operation[struct{}, string]{
		Name: "test.operation",
		Variants: []Variant[struct{}, string]{
			{
				Name: "common-v3", API: "SYNO.Test", Version: 3, Priority: 30,
				Match: APIVersion("SYNO.Test", 3), Execute: returnName("common"),
			},
			{
				Name: "dsm8-override", API: "SYNO.Test", Version: 3, Priority: 100,
				Match: All(
					APIVersion("SYNO.Test", 3),
					DSMVersionRange(DSMVersion{Major: 8}, DSMVersion{Major: 9}),
				),
				Execute: returnName("override"),
			},
		},
	}
	result, selection, err := operation.Run(context.Background(), target, executorFunc(func(context.Context, Request) (json.RawMessage, error) {
		return nil, nil
	}), struct{}{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != "override" || selection.Backend != "dsm8-override" {
		t.Fatalf("result=%q selection=%#v", result, selection)
	}
}

func TestOperationReportsUnsupportedTarget(t *testing.T) {
	operation := Operation[struct{}, string]{
		Name: "test.operation",
		Variants: []Variant[struct{}, string]{
			{Name: "v3", API: "SYNO.Test", Version: 3, Priority: 30, Match: APIVersion("SYNO.Test", 3), Execute: returnName("v3")},
		},
	}
	_, selection, err := operation.Select(NewTarget())
	if !IsUnsupported(err) || selection.Supported || selection.Operation != "test.operation" {
		t.Fatalf("selection=%#v error=%v", selection, err)
	}
}

func TestOperationRejectsAmbiguousPriority(t *testing.T) {
	target := NewTarget()
	target.SetAPI("SYNO.Test", APIInfo{Path: "entry.cgi", MinVersion: 1, MaxVersion: 1})
	operation := Operation[struct{}, string]{
		Name: "test.operation",
		Variants: []Variant[struct{}, string]{
			{Name: "one", Priority: 10, Match: APIVersion("SYNO.Test", 1), Execute: returnName("one")},
			{Name: "two", Priority: 10, Match: APIVersion("SYNO.Test", 1), Execute: returnName("two")},
		},
	}
	_, _, err := operation.Select(target)
	if err == nil {
		t.Fatal("Select() error = nil")
	}
}

func TestParseDSMVersion(t *testing.T) {
	version := ParseDSMVersion("DSM 7.3.2-86009 Update 1")
	if version.Major != 7 || version.Minor != 3 || version.Patch != 2 || version.Build != 86009 {
		t.Fatalf("ParseDSMVersion() = %#v", version)
	}
	if version.Compare(DSMVersion{Major: 7, Minor: 3, Patch: 1, Build: 99999}) <= 0 {
		t.Fatal("Compare() did not prioritize patch before build")
	}
}

func returnName(name string) func(context.Context, Executor, struct{}) (string, error) {
	return func(context.Context, Executor, struct{}) (string, error) {
		return name, nil
	}
}
