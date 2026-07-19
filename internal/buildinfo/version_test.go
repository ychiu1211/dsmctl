package buildinfo

import "testing"

func TestCurrentVersionMatchesCompatibilityConstants(t *testing.T) {
	parsed, err := ParseReleaseVersion(CurrentVersion)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.String() != CurrentVersion {
		t.Fatalf("parsed version = %q", parsed.String())
	}
	if train := parsed.CompatibilityTrain(); train != CompatibilityTrain {
		t.Fatalf("compatibility train = %q, want %q", train, CompatibilityTrain)
	}
	if parsed.Build != ReleaseBuild {
		t.Fatalf("build = %d, want %d", parsed.Build, ReleaseBuild)
	}
}

func TestParseReleaseVersion(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "7.3.2-1", valid: true},
		{value: "7.3.2-104", valid: true},
		{value: "7.3.2", valid: false},
		{value: "7.3.2-0", valid: false},
		{value: "7.3.2-r1", valid: false},
		{value: "v7.3.2-1", valid: false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			_, err := ParseReleaseVersion(test.value)
			if (err == nil) != test.valid {
				t.Fatalf("ParseReleaseVersion(%q) error = %v, valid = %v", test.value, err, test.valid)
			}
		})
	}
}

func TestReleaseVersionCompare(t *testing.T) {
	parse := func(value string) ReleaseVersion {
		version, err := ParseReleaseVersion(value)
		if err != nil {
			t.Fatal(err)
		}
		return version
	}
	if parse("7.3.2-105").Compare(parse("7.3.2-104")) <= 0 {
		t.Fatal("newer build did not sort after older build")
	}
	if parse("7.3.3-1").Compare(parse("7.3.2-999")) <= 0 {
		t.Fatal("newer compatibility train did not sort after older train")
	}
	if parse("7.3.2-1").Compare(parse("7.3.2-1")) != 0 {
		t.Fatal("equal versions did not compare equal")
	}
}
