package application

import "testing"

func TestValidateServerName(t *testing.T) {
	valid := []string{"nas51", "DiskStation", "n", "a-b-c", "nas-01", "A0", repeatByte('x', 63)}
	for _, name := range valid {
		if err := validateServerName(name); err != nil {
			t.Errorf("validateServerName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "-nas", "nas-", "na s", "na_s", "na.s", "hej!", repeatByte('x', 64)}
	for _, name := range invalid {
		if err := validateServerName(name); err == nil {
			t.Errorf("validateServerName(%q) = nil, want error", name)
		}
	}
}

func repeatByte(ch byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ch
	}
	return string(out)
}
