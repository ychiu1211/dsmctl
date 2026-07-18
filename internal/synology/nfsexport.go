package synology

import (
	"context"
	"fmt"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/nfsexport"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	nfsexportop "github.com/ychiu1211/dsmctl/internal/synology/operations/nfsexport"
)

type NFSShareExport = nfsexport.ShareExport
type NFSExportCapabilities = nfsexport.Capabilities
type NFSExportChangeRequest = nfsexport.ChangeRequest
type NFSExportMutationResult = nfsexportop.MutationResult

// NFSExportState reads the complete NFS export rule set of one shared folder.
func (c *Client) NFSExportState(ctx context.Context, share string) (NFSShareExport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	share = strings.TrimSpace(share)
	if share == "" {
		return NFSShareExport{}, fmt.Errorf("shared-folder name is required")
	}
	if err := c.prepareCompatibilityTargetLocked(ctx, nfsexportop.APINames()...); err != nil {
		return NFSShareExport{}, fmt.Errorf("prepare NFS export target: %w", err)
	}
	rules, _, err := nfsexportop.ExecuteRead(ctx, c.target, lockedExecutor{client: c}, nfsexportop.ReadInput{Share: share})
	if err != nil {
		return NFSShareExport{}, fmt.Errorf("get NFS export rules for %q: %w", share, err)
	}
	c.target.AddCapability(nfsexportop.ReadCapabilityName)
	return NFSShareExport{Share: share, Rules: rules}, nil
}

// NFSExportCapabilities reports the independently selected read and set backends
// for per-shared-folder NFS export rules.
func (c *Client) NFSExportCapabilities(ctx context.Context) (NFSExportCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, nfsexportop.APINames()...); err != nil {
		return NFSExportCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare NFS export capabilities target: %w", err)
	}
	readSelection, err := nfsexportop.SelectRead(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return NFSExportCapabilities{}, CompatibilityReport{}, fmt.Errorf("select NFS export read backend: %w", err)
	}
	setSelection, err := nfsexportop.SelectSet(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return NFSExportCapabilities{}, CompatibilityReport{}, fmt.Errorf("select NFS export set backend: %w", err)
	}
	if readSelection.Supported {
		c.target.AddCapability(nfsexportop.ReadCapabilityName)
	}
	if setSelection.Supported {
		c.target.AddCapability(nfsexportop.SetCapabilityName)
	}
	capabilities := NFSExportCapabilities{Read: readSelection.Supported, Set: setSelection.Supported}
	return capabilities, c.target.Report(readSelection, setSelection), nil
}

// ApplyNFSExportChange replaces one shared folder's NFS export rule set. It
// reads the current rules first so existing clients are submitted as edits and
// new clients as creations, matching the DSM SharePrivilege save contract.
func (c *Client) ApplyNFSExportChange(ctx context.Context, request NFSExportChangeRequest) (NFSExportMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	share := strings.TrimSpace(request.Share)
	if share == "" {
		return NFSExportMutationResult{}, fmt.Errorf("shared-folder name is required")
	}
	if err := c.prepareCompatibilityTargetLocked(ctx, nfsexportop.APINames()...); err != nil {
		return NFSExportMutationResult{}, fmt.Errorf("prepare NFS export mutation target: %w", err)
	}
	current, _, err := nfsexportop.ExecuteRead(ctx, c.target, lockedExecutor{client: c}, nfsexportop.ReadInput{Share: share})
	if err != nil {
		return NFSExportMutationResult{}, fmt.Errorf("refresh NFS export rules before apply: %w", err)
	}
	existing := make(map[string]struct{}, len(current))
	for _, rule := range current {
		existing[strings.TrimSpace(rule.Client)] = struct{}{}
	}
	result, _, err := nfsexportop.ExecuteSet(ctx, c.target, lockedExecutor{client: c}, nfsexportop.SaveInput{
		Share:           share,
		Rules:           request.Rules,
		ExistingClients: existing,
	})
	if err != nil {
		return NFSExportMutationResult{}, fmt.Errorf("apply NFS export rules for %q: %w", share, err)
	}
	return result, nil
}
