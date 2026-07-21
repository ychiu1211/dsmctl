// Package directory implements independently selectable, read-only DSM
// operations for the Control Panel Domain/LDAP (directory-client) surface.
//
// Two independent DSM API families are read, each gated separately so a NAS
// that advertises one but not the other fails closed only for the missing area:
//
//   - Active Directory membership: SYNO.Core.Directory.Domain v1 `get`
//     (enable_domain is the join flag), enriched with the non-secret join
//     options from SYNO.Core.Directory.Domain.Conf `get` and the periodic sync
//     schedule from SYNO.Core.Directory.Domain.Schedule v1 `get`.
//   - LDAP client bind: SYNO.Core.Directory.LDAP `get` (v2 preferred, adds
//     server_address and expand_nested_groups; v1 falls back to host), enriched
//     with the offered profile list from SYNO.Core.Directory.LDAP.Profile v1
//     `list`.
//
// The synced principal lists reuse the core identity APIs SYNO.Core.User /
// SYNO.Core.Group v1 `list` with a domain/ldap `type` filter (the dedicated
// SYNO.Core.Directory.Domain.User/.Group APIs do not exist on DSM 7.3), scoped
// to the NAS's active mode.
//
// Every shape below was live-verified against the lab (DSM 7.3), which is
// neither AD-joined nor LDAP-bound: Domain.get returns {"enable_domain":false},
// LDAP.get returns enable_client:false with the full config shape, and the
// synced lists are empty. The joined-identity fields decode tolerantly because
// an unjoined NAS omits them.
//
// SECRET HYGIENE: this module is read-only and never reads the AD join password
// or the LDAP bind password into any model; DSM does not return them on get.
package directory

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ychiu1211/dsmctl/internal/domain/directory"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	DomainAPI         = "SYNO.Core.Directory.Domain"
	DomainConfAPI     = "SYNO.Core.Directory.Domain.Conf"
	DomainScheduleAPI = "SYNO.Core.Directory.Domain.Schedule"
	LDAPAPI           = "SYNO.Core.Directory.LDAP"
	LDAPProfileAPI    = "SYNO.Core.Directory.LDAP.Profile"
	UserAPI           = "SYNO.Core.User"
	GroupAPI          = "SYNO.Core.Group"

	DomainReadCapabilityName = "directory.domain.read"
	LDAPReadCapabilityName   = "directory.ldap.read"
	UsersReadCapabilityName  = "directory.users.read"
	GroupsReadCapabilityName = "directory.groups.read"

	// additionalUserFields / additionalGroupFields request the non-secret
	// identity detail DSM omits from the bare list. No secret field is requested.
	additionalUserFields  = `["description","uid"]`
	additionalGroupFields = `["description","gid"]`
)

// Input is the empty request the status reads take.
type Input struct{}

// principalInput carries the DSM user/group list `type` for the active mode.
type principalInput struct {
	Type string
}

var domainStatusOp = compatibility.Operation[Input, directory.DomainState]{
	Name: DomainReadCapabilityName,
	Variants: []compatibility.Variant[Input, directory.DomainState]{
		{
			Name: "core-directory-domain-get-v1", API: DomainAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(DomainAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (directory.DomainState, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: DomainAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return directory.DomainState{}, fmt.Errorf("call %s.get v1: %w", DomainAPI, err)
				}
				return decodeDomain(data)
			},
		},
	},
}

var domainOptionsOp = compatibility.Operation[Input, directory.DomainOptions]{
	Name: "directory.domain.options.read",
	Variants: []compatibility.Variant[Input, directory.DomainOptions]{
		{
			Name: "core-directory-domain-conf-get-v2", API: DomainConfAPI, Version: 2, Priority: 20,
			Match: compatibility.APIVersion(DomainConfAPI, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (directory.DomainOptions, error) {
				return runDomainOptions(ctx, executor, 2)
			},
		},
		{
			Name: "core-directory-domain-conf-get-v1", API: DomainConfAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(DomainConfAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (directory.DomainOptions, error) {
				return runDomainOptions(ctx, executor, 1)
			},
		},
	},
}

func runDomainOptions(ctx context.Context, executor compatibility.Executor, version int) (directory.DomainOptions, error) {
	data, err := executor.Execute(ctx, compatibility.Request{
		API: DomainConfAPI, Version: version, Method: "get", ReadOnly: true,
	})
	if err != nil {
		return directory.DomainOptions{}, fmt.Errorf("call %s.get v%d: %w", DomainConfAPI, version, err)
	}
	return decodeDomainOptions(data)
}

var domainScheduleOp = compatibility.Operation[Input, directory.DomainSchedule]{
	Name: "directory.domain.schedule.read",
	Variants: []compatibility.Variant[Input, directory.DomainSchedule]{
		{
			Name: "core-directory-domain-schedule-get-v1", API: DomainScheduleAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(DomainScheduleAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (directory.DomainSchedule, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: DomainScheduleAPI, Version: 1, Method: "get", ReadOnly: true,
				})
				if err != nil {
					return directory.DomainSchedule{}, fmt.Errorf("call %s.get v1: %w", DomainScheduleAPI, err)
				}
				return decodeDomainSchedule(data)
			},
		},
	},
}

var ldapStatusOp = compatibility.Operation[Input, directory.LDAPState]{
	Name: LDAPReadCapabilityName,
	Variants: []compatibility.Variant[Input, directory.LDAPState]{
		{
			Name: "core-directory-ldap-get-v2", API: LDAPAPI, Version: 2, Priority: 20,
			Match: compatibility.APIVersion(LDAPAPI, 2),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (directory.LDAPState, error) {
				return runLDAP(ctx, executor, 2)
			},
		},
		{
			Name: "core-directory-ldap-get-v1", API: LDAPAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(LDAPAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) (directory.LDAPState, error) {
				return runLDAP(ctx, executor, 1)
			},
		},
	},
}

func runLDAP(ctx context.Context, executor compatibility.Executor, version int) (directory.LDAPState, error) {
	data, err := executor.Execute(ctx, compatibility.Request{
		API: LDAPAPI, Version: version, Method: "get", ReadOnly: true,
	})
	if err != nil {
		return directory.LDAPState{}, fmt.Errorf("call %s.get v%d: %w", LDAPAPI, version, err)
	}
	return decodeLDAP(data)
}

var ldapProfilesOp = compatibility.Operation[Input, []string]{
	Name: "directory.ldap.profiles.read",
	Variants: []compatibility.Variant[Input, []string]{
		{
			Name: "core-directory-ldap-profile-list-v1", API: LDAPProfileAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(LDAPProfileAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, _ Input) ([]string, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: LDAPProfileAPI, Version: 1, Method: "list", ReadOnly: true,
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.list v1: %w", LDAPProfileAPI, err)
				}
				return decodeLDAPProfiles(data)
			},
		},
	},
}

var usersOp = compatibility.Operation[principalInput, directory.DirectoryUsers]{
	Name: UsersReadCapabilityName,
	Variants: []compatibility.Variant[principalInput, directory.DirectoryUsers]{
		{
			Name: "core-user-list-v1", API: UserAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(UserAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input principalInput) (directory.DirectoryUsers, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: UserAPI, Version: 1, Method: "list",
					Parameters: url.Values{
						"offset":     {"0"},
						"limit":      {"-1"},
						"type":       {input.Type},
						"additional": {additionalUserFields},
					},
					ReadOnly: true,
				})
				if err != nil {
					return directory.DirectoryUsers{}, fmt.Errorf("call %s.list v1: %w", UserAPI, err)
				}
				return decodeUsers(data, modeForType(input.Type))
			},
		},
	},
}

var groupsOp = compatibility.Operation[principalInput, directory.DirectoryGroups]{
	Name: GroupsReadCapabilityName,
	Variants: []compatibility.Variant[principalInput, directory.DirectoryGroups]{
		{
			Name: "core-group-list-v1", API: GroupAPI, Version: 1, Priority: 10,
			Match: compatibility.APIVersion(GroupAPI, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input principalInput) (directory.DirectoryGroups, error) {
				data, err := executor.Execute(ctx, compatibility.Request{
					API: GroupAPI, Version: 1, Method: "list",
					Parameters: url.Values{
						"offset":     {"0"},
						"limit":      {"-1"},
						"type":       {input.Type},
						"additional": {additionalGroupFields},
					},
					ReadOnly: true,
				})
				if err != nil {
					return directory.DirectoryGroups{}, fmt.Errorf("call %s.list v1: %w", GroupAPI, err)
				}
				return decodeGroups(data, modeForType(input.Type))
			},
		},
	},
}

// APINames returns every DSM API this module may read, so the facade can
// discover them all in a single query before selecting any area.
func APINames() []string {
	return []string{
		DomainAPI, DomainConfAPI, DomainScheduleAPI,
		LDAPAPI, LDAPProfileAPI,
		UserAPI, GroupAPI,
	}
}

// SelectDomain reports the AD domain-status backend selection.
func SelectDomain(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := domainStatusOp.Select(target)
	return selection, err
}

// SelectLDAP reports the LDAP-status backend selection.
func SelectLDAP(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := ldapStatusOp.Select(target)
	return selection, err
}

// SelectUsers reports the synced-user backend selection.
func SelectUsers(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := usersOp.Select(target)
	return selection, err
}

// SelectGroups reports the synced-group backend selection.
func SelectGroups(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := groupsOp.Select(target)
	return selection, err
}

// ReadStatus reads the combined directory-client status. Domain and LDAP are
// read independently; an area whose API family is absent is reported nil (not
// supported) without failing the other. The mode is derived: ad when joined,
// ldap when bound, else none.
func ReadStatus(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (directory.Status, compatibility.Selection, compatibility.Selection, error) {
	status := directory.Status{Mode: directory.ModeNone}

	domain, domainSelection, err := readDomain(ctx, target, executor)
	if err != nil {
		return directory.Status{}, domainSelection, compatibility.Selection{}, err
	}
	if domainSelection.Supported {
		status.Domain = domain
		if domain.Joined {
			status.Mode = directory.ModeAD
		}
	}

	ldap, ldapSelection, err := readLDAP(ctx, target, executor)
	if err != nil {
		return directory.Status{}, domainSelection, ldapSelection, err
	}
	if ldapSelection.Supported {
		status.LDAP = ldap
		if ldap.Bound && status.Mode == directory.ModeNone {
			status.Mode = directory.ModeLDAP
		}
	}

	return status, domainSelection, ldapSelection, nil
}

// readDomain runs the primary domain-status read and enriches it with the
// optional join-options and sync-schedule areas. An unsupported enrichment area
// is a normal skip, never a failure.
func readDomain(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (*directory.DomainState, compatibility.Selection, error) {
	state, selection, err := domainStatusOp.Run(ctx, target, executor, Input{})
	if err != nil {
		if compatibility.IsUnsupported(err) {
			return nil, selection, nil
		}
		return nil, selection, err
	}
	if options, ok, err := readOptionalOptions(ctx, target, executor); err != nil {
		return nil, selection, err
	} else if ok {
		state.Options = options
	}
	if schedule, ok, err := readOptionalSchedule(ctx, target, executor); err != nil {
		return nil, selection, err
	} else if ok {
		state.Schedule = &schedule
	}
	return &state, selection, nil
}

// readLDAP runs the primary LDAP-status read and enriches it with the optional
// profile list.
func readLDAP(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (*directory.LDAPState, compatibility.Selection, error) {
	state, selection, err := ldapStatusOp.Run(ctx, target, executor, Input{})
	if err != nil {
		if compatibility.IsUnsupported(err) {
			return nil, selection, nil
		}
		return nil, selection, err
	}
	if profiles, ok, err := readOptionalProfiles(ctx, target, executor); err != nil {
		return nil, selection, err
	} else if ok {
		state.AvailableProfiles = profiles
	}
	return &state, selection, nil
}

// ReadUsers reads the synced users for the given mode. In ModeNone there are no
// synced users, so an empty page is returned without a DSM call. A DSM
// application error (for example "not joined") is treated as an empty list, not
// a failure, since the mode may transition out of band.
func ReadUsers(ctx context.Context, target compatibility.Target, executor compatibility.Executor, mode directory.Mode) (directory.DirectoryUsers, compatibility.Selection, error) {
	selection, err := SelectUsers(target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return directory.DirectoryUsers{}, selection, err
	}
	if !selection.Supported || mode == directory.ModeNone {
		return directory.DirectoryUsers{Mode: mode, Users: []directory.DomainUser{}}, selection, nil
	}
	page, _, err := usersOp.Run(ctx, target, executor, principalInput{Type: typeForMode(mode)})
	if err != nil {
		if _, isAPIErr := compatibility.APIErrorCode(err); isAPIErr {
			return directory.DirectoryUsers{Mode: mode, Users: []directory.DomainUser{}}, selection, nil
		}
		return directory.DirectoryUsers{}, selection, err
	}
	return page, selection, nil
}

// ReadGroups reads the synced groups for the given mode, with the same
// mode-gating and graceful DSM-error handling as ReadUsers.
func ReadGroups(ctx context.Context, target compatibility.Target, executor compatibility.Executor, mode directory.Mode) (directory.DirectoryGroups, compatibility.Selection, error) {
	selection, err := SelectGroups(target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return directory.DirectoryGroups{}, selection, err
	}
	if !selection.Supported || mode == directory.ModeNone {
		return directory.DirectoryGroups{Mode: mode, Groups: []directory.DomainGroup{}}, selection, nil
	}
	page, _, err := groupsOp.Run(ctx, target, executor, principalInput{Type: typeForMode(mode)})
	if err != nil {
		if _, isAPIErr := compatibility.APIErrorCode(err); isAPIErr {
			return directory.DirectoryGroups{Mode: mode, Groups: []directory.DomainGroup{}}, selection, nil
		}
		return directory.DirectoryGroups{}, selection, err
	}
	return page, selection, nil
}

func readOptionalOptions(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (directory.DomainOptions, bool, error) {
	if _, selection, err := domainOptionsOp.Select(target); err != nil {
		if compatibility.IsUnsupported(err) {
			return directory.DomainOptions{}, false, nil
		}
		return directory.DomainOptions{}, false, err
	} else if !selection.Supported {
		return directory.DomainOptions{}, false, nil
	}
	result, _, err := domainOptionsOp.Run(ctx, target, executor, Input{})
	if err != nil {
		// A DSM application error on the optional options read (for example on a
		// not-joined NAS) leaves the primary status intact.
		if _, isAPIErr := compatibility.APIErrorCode(err); isAPIErr {
			return directory.DomainOptions{}, false, nil
		}
		return directory.DomainOptions{}, false, err
	}
	return result, true, nil
}

func readOptionalSchedule(ctx context.Context, target compatibility.Target, executor compatibility.Executor) (directory.DomainSchedule, bool, error) {
	if _, selection, err := domainScheduleOp.Select(target); err != nil {
		if compatibility.IsUnsupported(err) {
			return directory.DomainSchedule{}, false, nil
		}
		return directory.DomainSchedule{}, false, err
	} else if !selection.Supported {
		return directory.DomainSchedule{}, false, nil
	}
	result, _, err := domainScheduleOp.Run(ctx, target, executor, Input{})
	if err != nil {
		if _, isAPIErr := compatibility.APIErrorCode(err); isAPIErr {
			return directory.DomainSchedule{}, false, nil
		}
		return directory.DomainSchedule{}, false, err
	}
	return result, true, nil
}

func readOptionalProfiles(ctx context.Context, target compatibility.Target, executor compatibility.Executor) ([]string, bool, error) {
	if _, selection, err := ldapProfilesOp.Select(target); err != nil {
		if compatibility.IsUnsupported(err) {
			return nil, false, nil
		}
		return nil, false, err
	} else if !selection.Supported {
		return nil, false, nil
	}
	result, _, err := ldapProfilesOp.Run(ctx, target, executor, Input{})
	if err != nil {
		if _, isAPIErr := compatibility.APIErrorCode(err); isAPIErr {
			return nil, false, nil
		}
		return nil, false, err
	}
	return result, true, nil
}

// typeForMode maps a directory mode to the DSM SYNO.Core.User/.Group list
// `type` filter. Live-verified: AD principals use type=domain, LDAP principals
// use type=ldap.
func typeForMode(mode directory.Mode) string {
	switch mode {
	case directory.ModeAD:
		return "domain"
	case directory.ModeLDAP:
		return "ldap"
	default:
		return "local"
	}
}

func modeForType(listType string) directory.Mode {
	switch listType {
	case "domain":
		return directory.ModeAD
	case "ldap":
		return directory.ModeLDAP
	default:
		return directory.ModeNone
	}
}
