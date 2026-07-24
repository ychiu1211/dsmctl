package synology

import (
	"testing"

	"github.com/derekvery666/dsmctl/internal/domain/packagecenter"
)

func TestPackageUpdateAvailableRequiresNewerOffer(t *testing.T) {
	tests := []struct {
		name      string
		installed string
		offered   string
		want      bool
	}{
		{name: "newer build", installed: "1.4.3-1610", offered: "1.4.3-2210", want: true},
		{name: "newer feature", installed: "3.5.1-26102", offered: "4.0.3-27892", want: true},
		{name: "same", installed: "1.4.3-2210", offered: "1.4.3-2210", want: false},
		{name: "repository downgrade", installed: "1.4.3-2210", offered: "1.4.3-1610", want: false},
		{name: "missing installed version", installed: "", offered: "1.0.0-1", want: false},
		{name: "missing offered version", installed: "1.0.0-1", offered: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := packageUpdateAvailable(test.installed, test.offered); got != test.want {
				t.Fatalf("packageUpdateAvailable(%q, %q) = %v, want %v", test.installed, test.offered, got, test.want)
			}
		})
	}
}

func TestPreferredPackageOffersMatchesUpdatePlannerSelection(t *testing.T) {
	packages := []packagecenter.AvailablePackage{
		{ID: "FileStation", Version: "1.4.3-1610"},
		{ID: "FileStation", Version: "1.5.0-9999", Beta: true},
		{ID: "BetaOnly", Version: "1.0.0-1", Beta: true},
		{ID: "BetaOnly", Version: "1.1.0-2", Beta: true},
	}
	preferred := preferredPackageOffers(packages)
	if got := preferred["FileStation"]; got != 0 {
		t.Fatalf("stable FileStation offer index = %d, want 0", got)
	}
	if got := preferred["BetaOnly"]; got != 3 {
		t.Fatalf("highest beta-only offer index = %d, want 3", got)
	}
}
