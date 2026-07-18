// Package nfsexport implements the per-shared-folder NFS export rule
// operations over SYNO.Core.FileServ.NFS.SharePrivilege. DSM API names,
// versions, and field encodings stay behind this package so the shared domain,
// application, CLI, and MCP contracts remain stable.
package nfsexport

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
)

const (
	APIName = "SYNO.Core.FileServ.NFS.SharePrivilege"

	ReadCapabilityName = "controlpanel.fileservices.nfs.export.read"
	SetCapabilityName  = "controlpanel.fileservices.nfs.export.set"

	ReadOperationName = "controlpanel.fileservices.nfs.export.read"
	SetOperationName  = "controlpanel.fileservices.nfs.export.set"
)

// ReadInput selects the shared folder whose export rules are loaded.
type ReadInput struct {
	Share string
}

// SaveInput carries the complete desired rule set plus the clients that already
// have a rule. Existing clients are submitted as edits (DSM id = old client)
// and new clients as creations (DSM id = ""), matching the SharePrivilege save
// contract.
type SaveInput struct {
	Share           string
	Rules           []nfsexport.Rule
	ExistingClients map[string]struct{}
}

// MutationResult records the selected backend for a save.
type MutationResult struct {
	Share   string `json:"share" jsonschema:"Shared folder whose NFS export rules changed"`
	Backend string `json:"backend" jsonschema:"Selected DSM compatibility backend"`
	API     string `json:"api" jsonschema:"DSM WebAPI used for the change"`
	Version int    `json:"version" jsonschema:"DSM WebAPI version used for the change"`
	Method  string `json:"method" jsonschema:"DSM WebAPI method used for the change"`
}

var readOperation = compatibility.Operation[ReadInput, []nfsexport.Rule]{
	Name: ReadOperationName,
	Variants: []compatibility.Variant[ReadInput, []nfsexport.Rule]{
		{
			Name:     "core-fileserv-nfs-shareprivilege-v1",
			API:      APIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input ReadInput) ([]nfsexport.Rule, error) {
				if input.Share == "" {
					return nil, fmt.Errorf("shared-folder name is required to load NFS export rules")
				}
				data, err := executor.Execute(ctx, compatibility.Request{
					API:        APIName,
					Version:    1,
					Method:     "load",
					Parameters: url.Values{"share_name": {input.Share}},
				})
				if err != nil {
					return nil, fmt.Errorf("call %s.load v1: %w", APIName, err)
				}
				return decodeRules(data)
			},
		},
	},
}

var setOperation = compatibility.Operation[SaveInput, MutationResult]{
	Name: SetOperationName,
	Variants: []compatibility.Variant[SaveInput, MutationResult]{
		{
			Name:     "core-fileserv-nfs-shareprivilege-v1",
			API:      APIName,
			Version:  1,
			Priority: 10,
			Match:    compatibility.APIVersion(APIName, 1),
			Execute: func(ctx context.Context, executor compatibility.Executor, input SaveInput) (MutationResult, error) {
				if input.Share == "" {
					return MutationResult{}, fmt.Errorf("shared-folder name is required to save NFS export rules")
				}
				encoded, err := encodeRules(input)
				if err != nil {
					return MutationResult{}, err
				}
				parameters := url.Values{
					"share_name": {input.Share},
					"rule":       {string(encoded)},
				}
				if _, err := executor.Execute(ctx, compatibility.Request{API: APIName, Version: 1, Method: "save", Parameters: parameters}); err != nil {
					return MutationResult{}, fmt.Errorf("call %s.save v1: %w", APIName, err)
				}
				return MutationResult{Share: input.Share}, nil
			},
		},
	},
}

func APINames() []string {
	return []string{APIName}
}

func SelectRead(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := readOperation.Select(target)
	return selection, err
}

func SelectSet(target compatibility.Target) (compatibility.Selection, error) {
	_, selection, err := setOperation.Select(target)
	return selection, err
}

func ExecuteRead(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input ReadInput) ([]nfsexport.Rule, compatibility.Selection, error) {
	return readOperation.Run(ctx, target, executor, input)
}

func ExecuteSet(ctx context.Context, target compatibility.Target, executor compatibility.Executor, input SaveInput) (MutationResult, compatibility.Selection, error) {
	result, selection, err := setOperation.Run(ctx, target, executor, input)
	if err == nil {
		result.Backend, result.API, result.Version, result.Method = selection.Backend, selection.API, selection.Version, "save"
	}
	return result, selection, err
}
