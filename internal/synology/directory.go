package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/directory"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	directoryops "github.com/ychiu1211/dsmctl/internal/synology/operations/directory"
)

type DirectoryStatus = directory.Status
type DirectoryUsers = directory.DirectoryUsers
type DirectoryGroups = directory.DirectoryGroups
type DirectoryCapabilities = directory.Capabilities

// DirectoryStatusState reads the Control Panel Domain/LDAP directory-client
// status: whether the NAS is joined to an Active Directory domain or bound to
// an LDAP server, and each area's non-secret configuration. Domain and LDAP are
// independent failure boundaries — a NAS exposing only one area still reads it,
// and the mode is derived (ad/ldap/none). No bind/join password is ever read.
func (c *Client) DirectoryStatusState(ctx context.Context) (DirectoryStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, directoryops.APINames()...); err != nil {
		return DirectoryStatus{}, fmt.Errorf("prepare directory target: %w", err)
	}
	status, domainSelection, ldapSelection, err := directoryops.ReadStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return DirectoryStatus{}, fmt.Errorf("read directory status: %w", err)
	}
	if domainSelection.Supported {
		c.target.AddCapability(directoryops.DomainReadCapabilityName)
	}
	if ldapSelection.Supported {
		c.target.AddCapability(directoryops.LDAPReadCapabilityName)
	}
	return status, nil
}

// DirectoryUsersList reads the synced domain/LDAP users. It first reads the
// directory status to determine the active mode, then lists the users scoped to
// that mode; a NAS in neither mode returns an empty list. These principals are
// owned by the directory server and are read-only. No password hash or keytab
// material is surfaced.
func (c *Client) DirectoryUsersList(ctx context.Context) (DirectoryUsers, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, directoryops.APINames()...); err != nil {
		return DirectoryUsers{}, fmt.Errorf("prepare directory users target: %w", err)
	}
	mode, err := c.directoryModeLocked(ctx)
	if err != nil {
		return DirectoryUsers{}, err
	}
	page, selection, err := directoryops.ReadUsers(ctx, c.target, lockedExecutor{client: c}, mode)
	if err != nil {
		return DirectoryUsers{}, fmt.Errorf("read directory users: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(directoryops.UsersReadCapabilityName)
	}
	return page, nil
}

// DirectoryGroupsList reads the synced domain/LDAP groups, scoped to the active
// mode the same way DirectoryUsersList is.
func (c *Client) DirectoryGroupsList(ctx context.Context) (DirectoryGroups, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, directoryops.APINames()...); err != nil {
		return DirectoryGroups{}, fmt.Errorf("prepare directory groups target: %w", err)
	}
	mode, err := c.directoryModeLocked(ctx)
	if err != nil {
		return DirectoryGroups{}, err
	}
	page, selection, err := directoryops.ReadGroups(ctx, c.target, lockedExecutor{client: c}, mode)
	if err != nil {
		return DirectoryGroups{}, fmt.Errorf("read directory groups: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(directoryops.GroupsReadCapabilityName)
	}
	return page, nil
}

// directoryModeLocked reads just enough status to resolve the active directory
// mode for a principal read. The caller already holds c.mu.
func (c *Client) directoryModeLocked(ctx context.Context) (directory.Mode, error) {
	status, _, _, err := directoryops.ReadStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return directory.ModeNone, fmt.Errorf("resolve directory mode: %w", err)
	}
	return status.Mode, nil
}

// DirectoryCapabilitiesState reports which directory read areas this NAS
// exposes, each selected independently so one missing API family does not
// disable the others.
func (c *Client) DirectoryCapabilitiesState(ctx context.Context) (DirectoryCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, directoryops.APINames()...); err != nil {
		return DirectoryCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare directory capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{directoryops.SelectDomain, directoryops.DomainReadCapabilityName},
		{directoryops.SelectLDAP, directoryops.LDAPReadCapabilityName},
		{directoryops.SelectUsers, directoryops.UsersReadCapabilityName},
		{directoryops.SelectGroups, directoryops.GroupsReadCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return DirectoryCapabilities{}, CompatibilityReport{}, fmt.Errorf("select directory backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := DirectoryCapabilities{
		Domain: selections[0].Supported,
		LDAP:   selections[1].Supported,
		Users:  selections[2].Supported,
		Groups: selections[3].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
