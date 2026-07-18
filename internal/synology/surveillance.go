package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/surveillance"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	surveillanceops "github.com/ychiu1211/dsmctl/internal/synology/operations/surveillance"
)

type SurveillanceInfo = surveillance.Info
type SurveillanceCameras = surveillance.Cameras
type SurveillanceCapabilities = surveillance.Capabilities

func (c *Client) surveillanceEvidenceLocked() surveillance.PackageEvidence {
	evidence := surveillance.PackageEvidence{ID: surveillanceops.PackageID}
	if installed, ok := c.target.InstalledPackage(surveillanceops.PackageID); ok {
		evidence.Installed = true
		evidence.Version = installed.Version.Raw
		evidence.Running = installed.Running
	}
	return evidence
}

func surveillanceReadError(what string, evidence surveillance.PackageEvidence, err error) error {
	if evidence.Installed && !evidence.Running {
		return fmt.Errorf("get Surveillance %s: the SurveillanceStation package is installed but not running; start it with a package lifecycle plan and retry: %w", what, err)
	}
	return fmt.Errorf("get Surveillance %s: %w", what, err)
}

// SurveillanceInfo reads Surveillance Station system information.
func (c *Client) SurveillanceInfo(ctx context.Context) (SurveillanceInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, surveillanceops.APINames()...); err != nil {
		return SurveillanceInfo{}, fmt.Errorf("prepare Surveillance target: %w", err)
	}
	evidence := c.surveillanceEvidenceLocked()
	info, _, err := surveillanceops.ExecuteInfo(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SurveillanceInfo{}, surveillanceReadError("info", evidence, err)
	}
	info.Package = evidence
	c.target.AddCapability(surveillanceops.InfoReadCapabilityName)
	return info, nil
}

// SurveillanceCameras reads the configured camera inventory.
func (c *Client) SurveillanceCameras(ctx context.Context) (SurveillanceCameras, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, surveillanceops.APINames()...); err != nil {
		return SurveillanceCameras{}, fmt.Errorf("prepare Surveillance target: %w", err)
	}
	cameras, _, err := surveillanceops.ExecuteCamera(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SurveillanceCameras{}, surveillanceReadError("cameras", c.surveillanceEvidenceLocked(), err)
	}
	c.target.AddCapability(surveillanceops.CameraReadCapabilityName)
	return cameras, nil
}

// SurveillanceCapabilities reports the Surveillance operations plus package evidence.
func (c *Client) SurveillanceCapabilities(ctx context.Context) (SurveillanceCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.preparePackageScopedTargetLocked(ctx, surveillanceops.APINames()...); err != nil {
		return SurveillanceCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Surveillance capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		surveillanceops.SelectInfo,
		surveillanceops.SelectCamera,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return SurveillanceCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Surveillance backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	if supported(0) {
		c.target.AddCapability(surveillanceops.InfoReadCapabilityName)
	}
	if supported(1) {
		c.target.AddCapability(surveillanceops.CameraReadCapabilityName)
	}
	capabilities := SurveillanceCapabilities{
		Module:     surveillance.ModuleName,
		InfoRead:   supported(0),
		CameraRead: supported(1),
		Package:    c.surveillanceEvidenceLocked(),
	}
	return capabilities, c.target.Report(selections...), nil
}
