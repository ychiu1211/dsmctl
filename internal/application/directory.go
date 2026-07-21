package application

import (
	"context"

	"github.com/ychiu1211/dsmctl/internal/synology"
)

type DirectoryCapabilitiesResult struct {
	NAS          string                         `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.DirectoryCapabilities `json:"capabilities" jsonschema:"Directory (Domain/LDAP) read areas currently exposed by dsmctl"`
	Report       synology.CompatibilityReport   `json:"report" jsonschema:"Discovered APIs and selected directory compatibility backends"`
}

type DirectoryStatusResult struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Status synology.DirectoryStatus `json:"status" jsonschema:"Directory-client status: AD domain membership and/or LDAP bind, with non-secret configuration"`
}

type DirectoryUsersResult struct {
	NAS   string                  `json:"nas" jsonschema:"NAS profile used for the request"`
	Users synology.DirectoryUsers `json:"users" jsonschema:"Synced domain/LDAP users, scoped to the active mode"`
}

type DirectoryGroupsResult struct {
	NAS    string                   `json:"nas" jsonschema:"NAS profile used for the request"`
	Groups synology.DirectoryGroups `json:"groups" jsonschema:"Synced domain/LDAP groups, scoped to the active mode"`
}

func (s *Service) GetDirectoryCapabilities(ctx context.Context, requestedNAS string) (DirectoryCapabilitiesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DirectoryCapabilitiesResult{}, err
	}
	capabilities, report, err := client.DirectoryCapabilitiesState(ctx)
	if err != nil {
		return DirectoryCapabilitiesResult{}, authenticationError(name, err)
	}
	return DirectoryCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) GetDirectoryStatus(ctx context.Context, requestedNAS string) (DirectoryStatusResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DirectoryStatusResult{}, err
	}
	status, err := client.DirectoryStatusState(ctx)
	if err != nil {
		return DirectoryStatusResult{}, authenticationError(name, err)
	}
	return DirectoryStatusResult{NAS: name, Status: status}, nil
}

func (s *Service) GetDirectoryUsers(ctx context.Context, requestedNAS string) (DirectoryUsersResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DirectoryUsersResult{}, err
	}
	users, err := client.DirectoryUsersList(ctx)
	if err != nil {
		return DirectoryUsersResult{}, authenticationError(name, err)
	}
	return DirectoryUsersResult{NAS: name, Users: users}, nil
}

func (s *Service) GetDirectoryGroups(ctx context.Context, requestedNAS string) (DirectoryGroupsResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return DirectoryGroupsResult{}, err
	}
	groups, err := client.DirectoryGroupsList(ctx)
	if err != nil {
		return DirectoryGroupsResult{}, authenticationError(name, err)
	}
	return DirectoryGroupsResult{NAS: name, Groups: groups}, nil
}
