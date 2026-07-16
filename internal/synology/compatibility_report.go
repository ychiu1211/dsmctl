package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/controlpaneltime"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/fileservices"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityappprivilege"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identitymembership"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identitymutation"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/identityquota"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/saninventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/sanmutation"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/shareinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/sharemutation"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/storageinventory"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/storagemodelconstraints"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/storagepoolmutation"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/systeminfo"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/volumemutation"
)

const (
	capabilityAuthSession       = "auth.session"
	capabilityAuthSynoToken     = "auth.syno_token"
	capabilityAuthTrustedDevice = "auth.trusted_device"
	quirkSessionCookieHeader    = "transport.session_cookie_and_token_header"
)

type CompatibilityReport = compatibility.Report

func (c *Client) Compatibility(ctx context.Context) (CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	apiNames := append([]string{authAPI}, storageinventory.APINames()...)
	apiNames = append(apiNames, storagemodelconstraints.APINames()...)
	apiNames = append(apiNames, storagepoolmutation.APINames()...)
	apiNames = append(apiNames, volumemutation.APINames()...)
	apiNames = append(apiNames, identityinventory.APINames()...)
	apiNames = append(apiNames, identitymutation.APINames()...)
	apiNames = append(apiNames, identitymembership.APINames()...)
	apiNames = append(apiNames, identityquota.APINames()...)
	apiNames = append(apiNames, identityappprivilege.APINames()...)
	apiNames = append(apiNames, shareinventory.APINames()...)
	apiNames = append(apiNames, sharemutation.APINames()...)
	apiNames = append(apiNames, controlpaneltime.APINames()...)
	apiNames = append(apiNames, fileservices.APINames()...)
	apiNames = append(apiNames, saninventory.APINames()...)
	apiNames = append(apiNames, sanmutation.APINames()...)
	if err := c.prepareCompatibilityTargetLocked(ctx, apiNames...); err != nil {
		return CompatibilityReport{}, fmt.Errorf("discover compatibility target: %w", err)
	}

	systemSelection, selectionErr := systeminfo.Select(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return CompatibilityReport{}, selectionErr
	}
	storageSelection, selectionErr := storageinventory.Select(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return CompatibilityReport{}, selectionErr
	}
	storageMutationSelections, selectionErr := storagepoolmutation.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	storageModelSelection, selectionErr := storagemodelconstraints.Select(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return CompatibilityReport{}, selectionErr
	}
	volumeMutationSelections, selectionErr := volumemutation.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	identitySelections, selectionErr := identityinventory.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	shareSelections, selectionErr := shareinventory.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	identityMutationSelections, selectionErr := identitymutation.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	membershipSelections, selectionErr := identitymembership.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	quotaSelections, selectionErr := identityquota.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	appPrivilegeSelections, selectionErr := identityappprivilege.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	shareMutationSelections, selectionErr := sharemutation.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	controlPanelTimeSelection, selectionErr := controlpaneltime.Select(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return CompatibilityReport{}, selectionErr
	}
	fileServiceSelectors := []func(compatibility.Target) (compatibility.Selection, error){
		fileservices.SelectSMBRead,
		fileservices.SelectSMBSet,
		fileservices.SelectNFSRead,
		fileservices.SelectNFSSet,
		fileservices.SelectNFSAdvancedRead,
		fileservices.SelectNFSAdvancedSet,
	}
	fileServiceSelections := make([]compatibility.Selection, 0, len(fileServiceSelectors))
	for _, selectOperation := range fileServiceSelectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return CompatibilityReport{}, err
		}
		fileServiceSelections = append(fileServiceSelections, selection)
	}
	sanSelections, selectionErr := saninventory.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	sanMutationSelections, selectionErr := sanmutation.Select(c.target)
	if selectionErr != nil {
		return CompatibilityReport{}, selectionErr
	}
	c.updateDerivedCapabilitiesLocked()
	selections := []compatibility.Selection{systemSelection, storageSelection, storageModelSelection}
	selections = append(selections, storageMutationSelections...)
	selections = append(selections, volumeMutationSelections...)
	selections = append(selections, identitySelections...)
	selections = append(selections, shareSelections...)
	selections = append(selections, identityMutationSelections...)
	selections = append(selections, membershipSelections...)
	selections = append(selections, quotaSelections...)
	selections = append(selections, appPrivilegeSelections...)
	selections = append(selections, shareMutationSelections...)
	selections = append(selections, controlPanelTimeSelection)
	selections = append(selections, fileServiceSelections...)
	selections = append(selections, sanSelections...)
	selections = append(selections, sanMutationSelections...)
	return c.target.Report(selections...), nil
}

// prepareCompatibilityTargetLocked discovers all APIs used by an operation
// and bootstraps the DSM release through SystemInfo. New operation façades call
// this before selecting variants so DSM-range overrides are eligible on the
// first execution, not only after another command has already run.
func (c *Client) prepareCompatibilityTargetLocked(ctx context.Context, apiNames ...string) error {
	names := append(systeminfo.APINames(), apiNames...)
	if err := c.discoverAPIsLocked(ctx, names...); err != nil {
		return err
	}
	if !c.target.DSM.Known() {
		if _, err := c.systemInfoLocked(ctx); err != nil {
			return fmt.Errorf("bootstrap DSM compatibility target: %w", err)
		}
	}
	return nil
}

func (c *Client) updateDerivedCapabilitiesLocked() {
	if auth, ok := c.target.API(authAPI); ok {
		c.target.AddCapability(capabilityAuthSession)
		if auth.Supports(6) {
			c.target.AddCapability(capabilityAuthSynoToken)
			c.target.AddCapability(capabilityAuthTrustedDevice)
		}
	}
	if _, err := systeminfo.Select(c.target); err == nil {
		c.target.AddCapability(systeminfo.CapabilityName)
	}
	if _, err := storageinventory.Select(c.target); err == nil {
		c.target.AddCapability(storageinventory.CapabilityName)
	}
	if selection, err := storagemodelconstraints.Select(c.target); err == nil && selection.Supported {
		c.target.AddCapability(storagemodelconstraints.CapabilityName)
	}
	if selections, err := storagepoolmutation.Select(c.target); err == nil {
		for index, capability := range []string{
			storagepoolmutation.CreateCapabilityName,
			storagepoolmutation.ExpandCapabilityName,
			storagepoolmutation.DeleteCapabilityName,
		} {
			if storagepoolmutation.Supported(selections, index) {
				c.target.AddCapability(capability)
			}
		}
	}
	if selections, err := volumemutation.Select(c.target); err == nil {
		for index, capability := range []string{
			volumemutation.CreateCapabilityName,
			volumemutation.ExpandCapabilityName,
			volumemutation.DeleteCapabilityName,
		} {
			if volumemutation.Supported(selections, index) {
				c.target.AddCapability(capability)
			}
		}
	}
	identitySupported := false
	if selections, err := identityinventory.Select(c.target); err == nil && identityinventory.Supported(selections) {
		identitySupported = true
		c.target.AddCapability(identityinventory.CapabilityName)
	}
	if selections, err := shareinventory.Select(c.target); err == nil {
		if shareinventory.InventorySupported(selections) {
			c.target.AddCapability(shareinventory.InventoryCapabilityName)
		}
		if shareinventory.PermissionsSupported(selections) && identitySupported {
			c.target.AddCapability(shareinventory.PermissionCapabilityName)
		}
	}
	if selections, err := identitymutation.Select(c.target); err == nil {
		if len(selections) > 0 && selections[0].Supported {
			c.target.AddCapability(identitymutation.UserCapabilityName)
		}
		if len(selections) > 1 && selections[1].Supported {
			c.target.AddCapability(identitymutation.GroupCapabilityName)
		}
	}
	if selections, err := identitymembership.Select(c.target); err == nil {
		if selectionSupported(selections, 0) {
			c.target.AddCapability(identitymembership.ReadCapabilityName)
		}
		if selectionSupported(selections, 1) {
			c.target.AddCapability(identitymembership.SetCapabilityName)
		}
	}
	if selections, err := identityquota.Select(c.target); err == nil {
		if selectionSupported(selections, 0) {
			c.target.AddCapability(identityquota.ReadCapabilityName)
		}
		if selectionSupported(selections, 1) {
			c.target.AddCapability(identityquota.SetCapabilityName)
		}
	}
	if selections, err := identityappprivilege.Select(c.target); err == nil {
		if selectionSupported(selections, 0) && selectionSupported(selections, 1) {
			c.target.AddCapability(identityappprivilege.ReadCapabilityName)
		}
		if selectionSupported(selections, 2) {
			c.target.AddCapability(identityappprivilege.SetCapabilityName)
		}
		if selectionSupported(selections, 3) {
			c.target.AddCapability(identityappprivilege.PreviewCapabilityName)
		}
	}
	if selections, err := sharemutation.Select(c.target); err == nil {
		if len(selections) > 0 && selections[0].Supported {
			c.target.AddCapability(sharemutation.ShareCapabilityName)
		}
		if len(selections) > 1 && selections[1].Supported {
			c.target.AddCapability(sharemutation.PermissionCapabilityName)
		}
	}
	if _, err := controlpaneltime.Select(c.target); err == nil {
		c.target.AddCapability(controlpaneltime.CapabilityName)
	}
	for _, operation := range []struct {
		selectOperation func(compatibility.Target) (compatibility.Selection, error)
		capability      string
	}{
		{fileservices.SelectSMBRead, fileservices.SMBReadCapabilityName},
		{fileservices.SelectSMBSet, fileservices.SMBSetCapabilityName},
		{fileservices.SelectNFSRead, fileservices.NFSReadCapabilityName},
		{fileservices.SelectNFSSet, fileservices.NFSSetCapabilityName},
		{fileservices.SelectNFSAdvancedRead, fileservices.NFSAdvancedReadCapabilityName},
		{fileservices.SelectNFSAdvancedSet, fileservices.NFSAdvancedSetCapabilityName},
	} {
		if selection, err := operation.selectOperation(c.target); err == nil && selection.Supported {
			c.target.AddCapability(operation.capability)
		}
	}
	if selections, err := saninventory.Select(c.target); err == nil {
		c.addSANCapabilitiesLocked(selections)
	}
	if selections, err := sanmutation.Select(c.target); err == nil {
		c.addSANMutationCapabilitiesLocked(selections)
	}
	// Sending session credentials in both documented parameters and the web UI
	// cookie/header locations is safe across tested DSM versions and fixes Core
	// APIs that reject body-only session parameters.
	c.target.AddQuirk(quirkSessionCookieHeader)
}
