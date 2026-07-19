package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestWarnIfUndercounted(t *testing.T) {
	cases := []struct {
		name       string
		found      int
		savedTotal int
		wantNote   bool
	}{
		{"zero found but many saved", 0, 170, true},
		{"badly undercounted", 21, 170, true},
		{"complete scan", 170, 170, false},
		{"nearly complete stays quiet", 160, 170, false},
		{"first run, nothing saved yet", 5, 5, false},
		{"first run finds nothing", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetErr(&stderr)
			warnIfUndercounted(cmd, tc.found, tc.savedTotal)
			gotNote := strings.Contains(stderr.String(), "Note:")
			if gotNote != tc.wantNote {
				t.Errorf("found=%d saved=%d: note=%v, want %v (output: %q)", tc.found, tc.savedTotal, gotNote, tc.wantNote, stderr.String())
			}
		})
	}
}
