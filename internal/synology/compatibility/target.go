package compatibility

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var dsmVersionPattern = regexp.MustCompile(`(?i)(?:DSM\s*)?(\d+)\.(\d+)(?:\.(\d+))?(?:-(\d+))?`)

type APIInfo struct {
	Path          string `json:"path"`
	MinVersion    int    `json:"minVersion"`
	MaxVersion    int    `json:"maxVersion"`
	RequestFormat string `json:"requestFormat,omitempty"`
}

func (info APIInfo) Supports(version int) bool {
	return version >= info.MinVersion && version <= info.MaxVersion
}

type DSMVersion struct {
	Raw   string `json:"raw,omitempty"`
	Major int    `json:"major,omitempty"`
	Minor int    `json:"minor,omitempty"`
	Patch int    `json:"patch,omitempty"`
	Build int    `json:"build,omitempty"`
}

func ParseDSMVersion(value string) DSMVersion {
	version := DSMVersion{Raw: strings.TrimSpace(value)}
	parts := dsmVersionPattern.FindStringSubmatch(value)
	if len(parts) == 0 {
		return version
	}
	version.Major, _ = strconv.Atoi(parts[1])
	version.Minor, _ = strconv.Atoi(parts[2])
	version.Patch, _ = strconv.Atoi(parts[3])
	version.Build, _ = strconv.Atoi(parts[4])
	return version
}

func (version DSMVersion) Known() bool {
	return version.Major > 0
}

func (version DSMVersion) Compare(other DSMVersion) int {
	left := [...]int{version.Major, version.Minor, version.Patch, version.Build}
	right := [...]int{other.Major, other.Minor, other.Patch, other.Build}
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

type Target struct {
	DSM          DSMVersion
	APIs         map[string]APIInfo
	capabilities map[string]struct{}
	quirks       map[string]struct{}
}

func NewTarget() Target {
	return Target{
		APIs:         make(map[string]APIInfo),
		capabilities: make(map[string]struct{}),
		quirks:       make(map[string]struct{}),
	}
}

func (target *Target) Normalize() {
	if target.APIs == nil {
		target.APIs = make(map[string]APIInfo)
	}
	if target.capabilities == nil {
		target.capabilities = make(map[string]struct{})
	}
	if target.quirks == nil {
		target.quirks = make(map[string]struct{})
	}
}

func (target Target) API(name string) (APIInfo, bool) {
	info, ok := target.APIs[name]
	return info, ok
}

func (target Target) SupportsAPI(name string, version int) bool {
	info, ok := target.API(name)
	return ok && info.Supports(version)
}

func (target *Target) SetAPI(name string, info APIInfo) {
	target.Normalize()
	target.APIs[name] = info
}

func (target *Target) AddCapability(name string) {
	target.Normalize()
	target.capabilities[name] = struct{}{}
}

func (target Target) HasCapability(name string) bool {
	_, ok := target.capabilities[name]
	return ok
}

func (target *Target) AddQuirk(name string) {
	target.Normalize()
	target.quirks[name] = struct{}{}
}

func (target Target) HasQuirk(name string) bool {
	_, ok := target.quirks[name]
	return ok
}

type APIReport struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	MinVersion    int    `json:"min_version"`
	MaxVersion    int    `json:"max_version"`
	RequestFormat string `json:"request_format,omitempty"`
}

type Report struct {
	DSM          DSMVersion  `json:"dsm"`
	APIs         []APIReport `json:"apis"`
	Capabilities []string    `json:"capabilities"`
	Quirks       []string    `json:"quirks,omitempty"`
	Operations   []Selection `json:"operations"`
}

func (target Target) Report(selections ...Selection) Report {
	apiNames := make([]string, 0, len(target.APIs))
	for name := range target.APIs {
		apiNames = append(apiNames, name)
	}
	sort.Strings(apiNames)
	apis := make([]APIReport, 0, len(apiNames))
	for _, name := range apiNames {
		info := target.APIs[name]
		apis = append(apis, APIReport{
			Name:          name,
			Path:          info.Path,
			MinVersion:    info.MinVersion,
			MaxVersion:    info.MaxVersion,
			RequestFormat: info.RequestFormat,
		})
	}
	capabilities := sortedSet(target.capabilities)
	quirks := sortedSet(target.quirks)
	operations := append([]Selection(nil), selections...)
	sort.Slice(operations, func(i, j int) bool {
		return operations[i].Operation < operations[j].Operation
	})
	return Report{
		DSM:          target.DSM,
		APIs:         apis,
		Capabilities: capabilities,
		Quirks:       quirks,
		Operations:   operations,
	}
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (version DSMVersion) String() string {
	if version.Raw != "" {
		return version.Raw
	}
	if !version.Known() {
		return "unknown"
	}
	value := fmt.Sprintf("DSM %d.%d.%d", version.Major, version.Minor, version.Patch)
	if version.Build > 0 {
		value += fmt.Sprintf("-%d", version.Build)
	}
	return value
}
