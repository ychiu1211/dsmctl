package application

import (
	"strings"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/driveadmin"
)

func TestValidateDriveAdminLogQueryDefaultsAndBounds(t *testing.T) {
	query := driveadmin.LogQuery{}
	if err := validateDriveAdminLogQuery(&query); err != nil {
		t.Fatalf("validateDriveAdminLogQuery(zero) error = %v", err)
	}
	if query.Limit != driveAdminDefaultLogLimit {
		t.Fatalf("default limit = %d", query.Limit)
	}

	valid := driveadmin.LogQuery{Limit: 25, From: 1700000000, To: 1700003600}
	if err := validateDriveAdminLogQuery(&valid); err != nil || valid.Limit != 25 {
		t.Fatalf("valid query error=%v limit=%d", err, valid.Limit)
	}

	cases := []struct {
		name  string
		query driveadmin.LogQuery
		want  string
	}{
		{"negative limit", driveadmin.LogQuery{Limit: -1}, "cannot be negative"},
		{"excessive limit", driveadmin.LogQuery{Limit: driveAdminMaxLogLimit + 1}, "exceeds the maximum"},
		{"negative bound", driveadmin.LogQuery{From: -5}, "Unix seconds"},
		{"inverted range", driveadmin.LogQuery{From: 200, To: 100}, "before the lower bound"},
	}
	for _, test := range cases {
		query := test.query
		if err := validateDriveAdminLogQuery(&query); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: error=%v, want %q", test.name, err, test.want)
		}
	}
}
