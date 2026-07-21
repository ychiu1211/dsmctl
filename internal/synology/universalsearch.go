package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/universalsearch"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	universalsearchops "github.com/ychiu1211/dsmctl/internal/synology/operations/universalsearch"
)

type UniversalSearchIndexedFolders = universalsearch.IndexedFolders
type UniversalSearchIndexStatus = universalsearch.IndexStatus
type UniversalSearchCapabilities = universalsearch.Capabilities

func (c *Client) universalSearchEvidenceLocked() universalsearch.PackageEvidence {
	evidence := universalsearch.PackageEvidence{ID: universalsearchops.PackageID}
	if installed, ok := c.target.InstalledPackage(universalsearchops.PackageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

func universalSearchReadError(what string, evidence universalsearch.PackageEvidence, err error) error {
	if evidence.Installed && !evidence.Running {
		return fmt.Errorf("get Universal Search %s: the SynoFinder package is installed but not running; start it with a package lifecycle plan and retry: %w", what, err)
	}
	return fmt.Errorf("get Universal Search %s: %w", what, err)
}

// UniversalSearchIndexedFolders reads the Universal Search file-index folder
// list. It is gated on the installed SynoFinder package so a NAS without it
// fails closed.
func (c *Client) UniversalSearchIndexedFolders(ctx context.Context) (UniversalSearchIndexedFolders, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, universalsearchops.APINames()...); err != nil {
		return UniversalSearchIndexedFolders{}, fmt.Errorf("prepare Universal Search target: %w", err)
	}
	evidence := c.universalSearchEvidenceLocked()
	folders, _, err := universalsearchops.ExecuteFolders(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return UniversalSearchIndexedFolders{}, universalSearchReadError("indexed folders", evidence, err)
	}
	folders.Package = evidence
	c.target.AddCapability(universalsearchops.FolderReadCapabilityName)
	return folders, nil
}

// UniversalSearchIndexStatus reads the overall Universal Search index daemon
// status. It is gated on the installed SynoFinder package.
func (c *Client) UniversalSearchIndexStatus(ctx context.Context) (UniversalSearchIndexStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, universalsearchops.APINames()...); err != nil {
		return UniversalSearchIndexStatus{}, fmt.Errorf("prepare Universal Search target: %w", err)
	}
	evidence := c.universalSearchEvidenceLocked()
	status, _, err := universalsearchops.ExecuteStatus(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return UniversalSearchIndexStatus{}, universalSearchReadError("index status", evidence, err)
	}
	status.Package = evidence
	c.target.AddCapability(universalsearchops.StatusReadCapabilityName)
	return status, nil
}

// UniversalSearchCapabilities reports the Universal Search reads plus package
// evidence, each selected independently and gated on the installed package.
func (c *Client) UniversalSearchCapabilities(ctx context.Context) (UniversalSearchCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, universalsearchops.APINames()...); err != nil {
		return UniversalSearchCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Universal Search capabilities target: %w", err)
	}
	selectors := []struct {
		selectArea func(compatibility.Target) (compatibility.Selection, error)
		capability string
	}{
		{universalsearchops.SelectFolders, universalsearchops.FolderReadCapabilityName},
		{universalsearchops.SelectStatus, universalsearchops.StatusReadCapabilityName},
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selector := range selectors {
		selection, err := selector.selectArea(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return UniversalSearchCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Universal Search backend: %w", err)
		}
		if selection.Supported {
			c.target.AddCapability(selector.capability)
		}
		selections = append(selections, selection)
	}
	capabilities := UniversalSearchCapabilities{
		Module:     universalsearch.ModuleName,
		Package:    c.universalSearchEvidenceLocked(),
		FolderRead: selections[0].Supported,
		StatusRead: selections[1].Supported,
	}
	return capabilities, c.target.Report(selections...), nil
}
