package buildinfo

import (
	"fmt"
	"regexp"
	"strconv"
)

var releaseVersionPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)-([1-9][0-9]*)$`)

// ReleaseVersion separates the certified DSM compatibility train from the
// monotonically increasing dsmctl build within that train.
type ReleaseVersion struct {
	DSMMajor int
	DSMMinor int
	DSMPatch int
	Build    int
}

// ParseReleaseVersion accepts DSM_MAJOR.DSM_MINOR.DSM_PATCH-DSMCTL_BUILD.
func ParseReleaseVersion(value string) (ReleaseVersion, error) {
	parts := releaseVersionPattern.FindStringSubmatch(value)
	if parts == nil {
		return ReleaseVersion{}, fmt.Errorf("invalid dsmctl release version %q; want X.Y.Z-N", value)
	}
	values := make([]int, 4)
	for index := range values {
		parsed, err := strconv.Atoi(parts[index+1])
		if err != nil {
			return ReleaseVersion{}, fmt.Errorf("parse dsmctl release version %q: %w", value, err)
		}
		values[index] = parsed
	}
	return ReleaseVersion{DSMMajor: values[0], DSMMinor: values[1], DSMPatch: values[2], Build: values[3]}, nil
}

func (version ReleaseVersion) String() string {
	return fmt.Sprintf("%d.%d.%d-%d", version.DSMMajor, version.DSMMinor, version.DSMPatch, version.Build)
}

// CompatibilityTrain renders only the certified DSM feature-release axis.
func (version ReleaseVersion) CompatibilityTrain() string {
	return fmt.Sprintf("%d.%d.%d", version.DSMMajor, version.DSMMinor, version.DSMPatch)
}

// Compare orders first by DSM compatibility train, then by dsmctl build.
func (version ReleaseVersion) Compare(other ReleaseVersion) int {
	left := [...]int{version.DSMMajor, version.DSMMinor, version.DSMPatch, version.Build}
	right := [...]int{other.DSMMajor, other.DSMMinor, other.DSMPatch, other.Build}
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
