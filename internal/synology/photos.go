package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/photos"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	photosops "github.com/ychiu1211/dsmctl/internal/synology/operations/photos"
)

type PhotosAdminSettings = photos.AdminSettings
type PhotosCapabilities = photos.Capabilities
type PhotosAdminChange = photos.AdminChange

func (c *Client) photosEvidenceLocked() photos.PackageEvidence {
	evidence := photos.PackageEvidence{ID: photosops.PackageID}
	if installed, ok := c.target.InstalledPackage(photosops.PackageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

func photosReadError(evidence photos.PackageEvidence, err error) error {
	if evidence.Installed && !evidence.Running {
		return fmt.Errorf("get Photos admin settings: the SynologyPhotos package is installed but not running; start it with a package lifecycle plan and retry: %w", err)
	}
	return fmt.Errorf("get Photos admin settings: %w", err)
}

// PhotosAdminSettings reads the Synology Photos administration configuration.
func (c *Client) PhotosAdminSettings(ctx context.Context) (PhotosAdminSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, photosops.APINames()...); err != nil {
		return PhotosAdminSettings{}, fmt.Errorf("prepare Photos target: %w", err)
	}
	settings, _, err := photosops.ExecuteAdminRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return PhotosAdminSettings{}, photosReadError(c.photosEvidenceLocked(), err)
	}
	c.target.AddCapability(photosops.AdminReadCapabilityName)
	return settings, nil
}

// PhotosCapabilities reports the Photos administration operations plus the
// installed-package evidence the selection used.
func (c *Client) PhotosCapabilities(ctx context.Context) (PhotosCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, photosops.APINames()...); err != nil {
		return PhotosCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Photos capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		photosops.SelectAdminRead,
		photosops.SelectAdminSet,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return PhotosCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Photos backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	if supported(0) {
		c.target.AddCapability(photosops.AdminReadCapabilityName)
	}
	if supported(1) {
		c.target.AddCapability(photosops.AdminSetCapabilityName)
	}
	capabilities := PhotosCapabilities{
		Module:    photos.ModuleName,
		AdminRead: supported(0),
		AdminSet:  supported(1),
		Package:   c.photosEvidenceLocked(),
	}
	return capabilities, c.target.Report(selections...), nil
}

// ApplyPhotosAdminChange applies a partial patch: only the fields present in the
// change are sent, so unspecified settings are preserved.
func (c *Client) ApplyPhotosAdminChange(ctx context.Context, change PhotosAdminChange) (PhotosMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, photosops.APINames()...); err != nil {
		return PhotosMutationResult{}, fmt.Errorf("prepare Photos mutation target: %w", err)
	}
	result, _, err := photosops.ExecuteAdminSet(ctx, c.target, lockedExecutor{client: c}, change)
	if err != nil {
		return PhotosMutationResult{}, fmt.Errorf("apply Photos admin settings: %w", err)
	}
	return result, nil
}

type PhotosMutationResult = photosops.MutationResult
