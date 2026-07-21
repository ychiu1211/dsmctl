package taskscheduler

import "testing"

func TestNormalizeTypeGroup(t *testing.T) {
	cases := map[string]string{
		"script":         TypeGroupScript,
		"user_defined":   TypeGroupScript,
		"service":        TypeGroupService,
		"package":        TypeGroupService,
		"recycle_bin":    TypeGroupRetention,
		"smart_test":     TypeGroupSystem,
		"data_scrubbing": TypeGroupSystem,
		"dsm_update":     TypeGroupSystem,
		"hyper_backup":   TypeGroupSystem,
		"":               TypeGroupOther,
		"something_else": TypeGroupOther,
	}
	for raw, want := range cases {
		if got := NormalizeTypeGroup(raw); got != want {
			t.Errorf("NormalizeTypeGroup(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestIsPrivilegedOwner(t *testing.T) {
	privileged := []string{"", "root", "ROOT", " admin ", "administrator", "system"}
	for _, owner := range privileged {
		if !IsPrivilegedOwner(owner) {
			t.Errorf("IsPrivilegedOwner(%q) = false, want true", owner)
		}
	}
	unprivileged := []string{"testuser", "backupbot", "guest"}
	for _, owner := range unprivileged {
		if IsPrivilegedOwner(owner) {
			t.Errorf("IsPrivilegedOwner(%q) = true, want false", owner)
		}
	}
}
