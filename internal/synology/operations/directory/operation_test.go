package directory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ychiu1211/dsmctl/internal/domain/directory"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

type executorFunc func(context.Context, compatibility.Request) (json.RawMessage, error)

func (function executorFunc) Execute(ctx context.Context, request compatibility.Request) (json.RawMessage, error) {
	return function(ctx, request)
}

// codedError mimics the transport APIError: it carries a DSM application code so
// compatibility.APIErrorCode can classify it.
type codedError struct{ code int }

func (e codedError) Error() string     { return fmt.Sprintf("dsm code %d", e.code) }
func (e codedError) DSMErrorCode() int { return e.code }

// apiSpec is an API name with a supported version range for the test target.
type apiSpec struct {
	name       string
	minV, maxV int
}

func targetWith(t *testing.T, specs ...apiSpec) compatibility.Target {
	t.Helper()
	target := compatibility.NewTarget()
	for _, spec := range specs {
		target.SetAPI(spec.name, compatibility.APIInfo{Path: "entry.cgi", MinVersion: spec.minV, MaxVersion: spec.maxV})
	}
	return target
}

func v1(name string) apiSpec { return apiSpec{name, 1, 1} }

// TestReadStatusDomainOnly proves an AD-only NAS reads the Domain area, derives
// the mode from enable_domain, and reports LDAP as absent without failing.
func TestReadStatusDomainOnly(t *testing.T) {
	target := targetWith(t, v1(DomainAPI), v1(DomainConfAPI), v1(DomainScheduleAPI))
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.API {
		case DomainAPI:
			return json.RawMessage(`{"enable_domain":true,"domain_fqdn":"ad.example.com","workgroup":"EXAMPLE"}`), nil
		case DomainConfAPI:
			return json.RawMessage(`{"disable_domain_admins":true,"domain_nested_group":1}`), nil
		case DomainScheduleAPI:
			return json.RawMessage(`{"date_type":2}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	status, domainSel, ldapSel, err := ReadStatus(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if !domainSel.Supported || ldapSel.Supported {
		t.Fatalf("selections: domain=%#v ldap=%#v", domainSel, ldapSel)
	}
	if status.Mode != directory.ModeAD {
		t.Fatalf("mode = %q, want ad", status.Mode)
	}
	if status.Domain == nil || status.Domain.DomainFQDN != "ad.example.com" || !status.Domain.Options.DisableDomainAdmins {
		t.Fatalf("domain = %#v", status.Domain)
	}
	if status.Domain.Schedule == nil || status.Domain.Schedule.DateType != 2 {
		t.Fatalf("schedule = %#v", status.Domain.Schedule)
	}
	if status.LDAP != nil {
		t.Fatalf("LDAP should be nil when its API family is absent: %#v", status.LDAP)
	}
}

// TestReadStatusLDAPOnlyModeAndVersion proves an LDAP-only NAS reads the LDAP
// area (preferring v2), derives mode ldap when bound, and reports Domain absent.
func TestReadStatusLDAPOnlyModeAndVersion(t *testing.T) {
	target := targetWith(t, apiSpec{LDAPAPI, 1, 2}, v1(LDAPProfileAPI))
	var usedVersion int
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.API {
		case LDAPAPI:
			usedVersion = request.Version
			return json.RawMessage(`{"enable_client":true,"server_address":"ldap.example.com","base_dn":"dc=example,dc=com"}`), nil
		case LDAPProfileAPI:
			return json.RawMessage(`{"profiles":["standard","mac"]}`), nil
		}
		t.Fatalf("unexpected request %s.%s", request.API, request.Method)
		return nil, nil
	})
	status, domainSel, ldapSel, err := ReadStatus(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if domainSel.Supported || !ldapSel.Supported {
		t.Fatalf("selections: domain=%#v ldap=%#v", domainSel, ldapSel)
	}
	if usedVersion != 2 {
		t.Fatalf("LDAP get used v%d, want v2 preference", usedVersion)
	}
	if status.Mode != directory.ModeLDAP {
		t.Fatalf("mode = %q, want ldap", status.Mode)
	}
	if status.LDAP == nil || !status.LDAP.Bound || status.LDAP.ServerAddress != "ldap.example.com" ||
		len(status.LDAP.AvailableProfiles) != 2 {
		t.Fatalf("ldap = %#v", status.LDAP)
	}
	if status.Domain != nil {
		t.Fatalf("Domain should be nil when its API family is absent: %#v", status.Domain)
	}
}

// TestReadStatusUnjoinedUnboundIsNone proves the graceful not-supported path: a
// NAS neither joined nor bound reports mode none with both areas readable.
func TestReadStatusUnjoinedUnbound(t *testing.T) {
	target := targetWith(t, v1(DomainAPI), apiSpec{LDAPAPI, 1, 2})
	executor := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		switch request.API {
		case DomainAPI:
			return json.RawMessage(`{"enable_domain":false}`), nil
		case LDAPAPI:
			return json.RawMessage(`{"enable_client":false,"error":2703}`), nil
		}
		return nil, codedError{code: 103} // optional enrichment areas absent
	})
	status, _, _, err := ReadStatus(context.Background(), target, executor)
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != directory.ModeNone {
		t.Fatalf("mode = %q, want none", status.Mode)
	}
	if status.Domain == nil || status.Domain.Joined {
		t.Fatalf("domain = %#v", status.Domain)
	}
	if status.LDAP == nil || status.LDAP.Bound {
		t.Fatalf("ldap = %#v", status.LDAP)
	}
}

// TestReadUsersModeGating proves ModeNone returns an empty list without any DSM
// call, and an active mode reads SYNO.Core.User with the matching type filter.
func TestReadUsersModeGating(t *testing.T) {
	target := targetWith(t, v1(UserAPI))

	// ModeNone: no call at all.
	noCall := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		t.Fatalf("no DSM call expected in ModeNone, got %s.%s", request.API, request.Method)
		return nil, nil
	})
	page, _, err := ReadUsers(context.Background(), target, noCall, directory.ModeNone)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Users) != 0 || page.Users == nil {
		t.Fatalf("ModeNone users = %#v", page)
	}

	// ModeAD: type=domain.
	var seenType string
	adExec := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		seenType = request.Parameters.Get("type")
		return json.RawMessage(`{"total":1,"users":[{"name":"testuser","uid":100001}]}`), nil
	})
	page, selection, err := ReadUsers(context.Background(), target, adExec, directory.ModeAD)
	if err != nil {
		t.Fatal(err)
	}
	if seenType != "domain" {
		t.Fatalf("user list type = %q, want domain", seenType)
	}
	if !selection.Supported || len(page.Users) != 1 || page.Users[0].Source != directory.ModeAD {
		t.Fatalf("ad users = %#v", page)
	}
}

// TestReadUsersGracefulAPIError proves a DSM application error (for example a
// not-joined race after mode was resolved) yields an empty list, not a failure,
// while a transport error propagates.
func TestReadUsersGracefulAPIError(t *testing.T) {
	target := targetWith(t, v1(UserAPI))

	apiErr := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return nil, codedError{code: 3101}
	})
	page, _, err := ReadUsers(context.Background(), target, apiErr, directory.ModeLDAP)
	if err != nil {
		t.Fatalf("DSM application error should be swallowed as empty, got %v", err)
	}
	if len(page.Users) != 0 {
		t.Fatalf("expected empty list on DSM error, got %#v", page)
	}

	boom := errors.New("connection reset")
	transportErr := executorFunc(func(_ context.Context, _ compatibility.Request) (json.RawMessage, error) {
		return nil, boom
	})
	if _, _, err := ReadUsers(context.Background(), target, transportErr, directory.ModeAD); !errors.Is(err, boom) {
		t.Fatalf("transport error should propagate, got %v", err)
	}
}

// TestReadGroupsModeGating mirrors the user gating for groups.
func TestReadGroupsModeGating(t *testing.T) {
	target := targetWith(t, v1(GroupAPI))
	var seenType string
	exec := executorFunc(func(_ context.Context, request compatibility.Request) (json.RawMessage, error) {
		seenType = request.Parameters.Get("type")
		return json.RawMessage(`{"total":1,"groups":[{"name":"grp","gid":512}]}`), nil
	})
	page, selection, err := ReadGroups(context.Background(), target, exec, directory.ModeLDAP)
	if err != nil {
		t.Fatal(err)
	}
	if seenType != "ldap" {
		t.Fatalf("group list type = %q, want ldap", seenType)
	}
	if !selection.Supported || len(page.Groups) != 1 || page.Groups[0].Source != directory.ModeLDAP {
		t.Fatalf("ldap groups = %#v", page)
	}
}

// TestSelectIndependentGating proves each area selects its own backend so a
// missing API family fails closed only for its own area.
func TestSelectIndependentGating(t *testing.T) {
	target := targetWith(t, v1(DomainAPI)) // domain only
	if selection, _ := SelectDomain(target); !selection.Supported {
		t.Fatalf("domain should be supported: %#v", selection)
	}
	if selection, _ := SelectLDAP(target); selection.Supported {
		t.Fatalf("ldap should be unsupported without the LDAP API: %#v", selection)
	}
	if selection, _ := SelectUsers(target); selection.Supported {
		t.Fatalf("users should be unsupported without SYNO.Core.User: %#v", selection)
	}
}
