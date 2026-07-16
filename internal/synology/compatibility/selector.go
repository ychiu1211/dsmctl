package compatibility

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type Request struct {
	API        string
	Version    int
	Method     string
	Parameters url.Values
}

type Executor interface {
	Execute(ctx context.Context, request Request) (json.RawMessage, error)
}

type Matcher func(target Target) (matched bool, reason string)

type Variant[I, O any] struct {
	Name     string
	API      string
	Version  int
	Priority int
	Match    Matcher
	Execute  func(context.Context, Executor, I) (O, error)
}

type Operation[I, O any] struct {
	Name     string
	Variants []Variant[I, O]
}

func (operation Operation[I, O]) APINames() []string {
	unique := make(map[string]struct{})
	for _, variant := range operation.Variants {
		if variant.API != "" {
			unique[variant.API] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for name := range unique {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

type Selection struct {
	Operation string `json:"operation"`
	Supported bool   `json:"supported"`
	Backend   string `json:"backend,omitempty"`
	API       string `json:"api,omitempty"`
	Version   int    `json:"version,omitempty"`
	Reason    string `json:"reason"`
}

type UnsupportedOperationError struct {
	Operation string
}

func (err *UnsupportedOperationError) Error() string {
	return fmt.Sprintf("operation %q is not supported by this DSM target", err.Operation)
}

func (operation Operation[I, O]) Select(target Target) (Variant[I, O], Selection, error) {
	var selected Variant[I, O]
	var reason string
	found := false
	for _, candidate := range operation.Variants {
		if candidate.Name == "" || candidate.Match == nil || candidate.Execute == nil {
			continue
		}
		matched, candidateReason := candidate.Match(target)
		if !matched {
			continue
		}
		if found && candidate.Priority == selected.Priority {
			return Variant[I, O]{}, Selection{}, fmt.Errorf(
				"operation %q has ambiguous variants %q and %q at priority %d",
				operation.Name, selected.Name, candidate.Name, candidate.Priority,
			)
		}
		if !found || candidate.Priority > selected.Priority {
			selected = candidate
			reason = candidateReason
			found = true
		}
	}
	if !found {
		selection := Selection{
			Operation: operation.Name,
			Supported: false,
			Reason:    "no compatible implementation matched the discovered target",
		}
		return Variant[I, O]{}, selection, &UnsupportedOperationError{Operation: operation.Name}
	}
	return selected, Selection{
		Operation: operation.Name,
		Supported: true,
		Backend:   selected.Name,
		API:       selected.API,
		Version:   selected.Version,
		Reason:    reason,
	}, nil
}

func (operation Operation[I, O]) Run(ctx context.Context, target Target, executor Executor, input I) (O, Selection, error) {
	variant, selection, err := operation.Select(target)
	if err != nil {
		var zero O
		return zero, selection, err
	}
	result, err := variant.Execute(ctx, executor, input)
	if err != nil {
		var zero O
		return zero, selection, fmt.Errorf("execute %s with backend %s: %w", operation.Name, variant.Name, err)
	}
	return result, selection, nil
}

func APIVersion(name string, version int) Matcher {
	return func(target Target) (bool, string) {
		info, ok := target.API(name)
		if !ok {
			return false, fmt.Sprintf("API %s was not discovered", name)
		}
		if !info.Supports(version) {
			return false, fmt.Sprintf("API %s supports versions %d-%d, not v%d", name, info.MinVersion, info.MaxVersion, version)
		}
		return true, fmt.Sprintf("API %s advertises support for v%d", name, version)
	}
}

func Capability(name string) Matcher {
	return func(target Target) (bool, string) {
		if !target.HasCapability(name) {
			return false, fmt.Sprintf("capability %s was not discovered", name)
		}
		return true, fmt.Sprintf("capability %s is available", name)
	}
}

// DSMVersionRange matches a known DSM release in [minimum, maximum). A zero
// minimum or maximum is unbounded. Prefer API/capability matching and use this
// matcher only for a release-specific behavioral difference.
func DSMVersionRange(minimum, maximum DSMVersion) Matcher {
	return func(target Target) (bool, string) {
		if !target.DSM.Known() {
			return false, "DSM release is not known"
		}
		if minimum.Known() && target.DSM.Compare(minimum) < 0 {
			return false, fmt.Sprintf("DSM %s is below the minimum %s", target.DSM.String(), minimum.String())
		}
		if maximum.Known() && target.DSM.Compare(maximum) >= 0 {
			return false, fmt.Sprintf("DSM %s is at or above the exclusive maximum %s", target.DSM.String(), maximum.String())
		}
		return true, fmt.Sprintf("DSM %s is in the required release range", target.DSM.String())
	}
}

func All(matchers ...Matcher) Matcher {
	return func(target Target) (bool, string) {
		reasons := make([]string, 0, len(matchers))
		for _, matcher := range matchers {
			if matcher == nil {
				continue
			}
			matched, reason := matcher(target)
			if !matched {
				return false, reason
			}
			if reason != "" {
				reasons = append(reasons, reason)
			}
		}
		return true, strings.Join(reasons, "; ")
	}
}

func IsUnsupported(err error) bool {
	var unsupported *UnsupportedOperationError
	return errors.As(err, &unsupported)
}
