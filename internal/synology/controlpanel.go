package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/controlpanel"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/controlpaneltime"
	"github.com/ychiu1211/dsmctl/internal/synology/operations/fileservices"
)

type ControlPanelTimeState = controlpanel.TimeState
type ControlPanelTimeCapabilities = controlpanel.TimeCapabilities
type SMBState = controlpanel.SMBState
type NFSState = controlpanel.NFSState
type FileServiceCapabilities = controlpanel.FileServiceCapabilities
type FileServiceChangeRequest = controlpanel.FileServiceChangeRequest
type FileServiceMutationResult = fileservices.MutationResult

// ControlPanelTimeState reads the focused time module without requiring or
// coupling to any other Control Panel module API.
func (c *Client) ControlPanelTimeState(ctx context.Context) (ControlPanelTimeState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, controlpaneltime.APINames()...); err != nil {
		return ControlPanelTimeState{}, fmt.Errorf("prepare Control Panel time target: %w", err)
	}
	state, _, err := controlpaneltime.Execute(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return ControlPanelTimeState{}, fmt.Errorf("get Control Panel time configuration: %w", err)
	}
	c.target.AddCapability(controlpaneltime.CapabilityName)
	return state, nil
}

// ControlPanelTimeCapabilities reports only this module's selection. The
// module can therefore be unsupported without changing another module's
// capability result.
func (c *Client) ControlPanelTimeCapabilities(ctx context.Context) (ControlPanelTimeCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, controlpaneltime.APINames()...); err != nil {
		return ControlPanelTimeCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare Control Panel time capabilities target: %w", err)
	}
	selection, err := controlpaneltime.Select(c.target)
	if err != nil && !compatibility.IsUnsupported(err) {
		return ControlPanelTimeCapabilities{}, CompatibilityReport{}, fmt.Errorf("select Control Panel time backend: %w", err)
	}
	if selection.Supported {
		c.target.AddCapability(controlpaneltime.CapabilityName)
	}
	capabilities := ControlPanelTimeCapabilities{
		Module: controlpanel.ModuleTime,
		Read:   selection.Supported,
		Set:    false,
	}
	return capabilities, c.target.Report(selection), nil
}

func (c *Client) SMBState(ctx context.Context) (SMBState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, fileservices.SMBAPIName); err != nil {
		return SMBState{}, fmt.Errorf("prepare SMB target: %w", err)
	}
	state, _, err := fileservices.ExecuteSMBRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return SMBState{}, fmt.Errorf("get SMB configuration: %w", err)
	}
	c.target.AddCapability(fileservices.SMBReadCapabilityName)
	return state, nil
}

func (c *Client) NFSState(ctx context.Context) (NFSState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, fileservices.NFSAPIName, fileservices.NFSAdvancedAPIName); err != nil {
		return NFSState{}, fmt.Errorf("prepare NFS target: %w", err)
	}
	state, _, err := fileservices.ExecuteNFSRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return NFSState{}, fmt.Errorf("get NFS configuration: %w", err)
	}
	c.target.AddCapability(fileservices.NFSReadCapabilityName)
	advancedSelection, selectionErr := fileservices.SelectNFSAdvancedRead(c.target)
	if selectionErr == nil && advancedSelection.Supported {
		advanced, _, readErr := fileservices.ExecuteNFSAdvancedRead(ctx, c.target, lockedExecutor{client: c})
		if readErr != nil {
			return NFSState{}, fmt.Errorf("get NFS advanced configuration: %w", readErr)
		}
		state.NFSv4Domain = advanced.Domain
		c.target.AddCapability(fileservices.NFSAdvancedReadCapabilityName)
	} else if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return NFSState{}, fmt.Errorf("select NFS advanced read backend: %w", selectionErr)
	}
	return state, nil
}

func (c *Client) FileServiceCapabilities(ctx context.Context) (FileServiceCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, fileservices.APINames()...); err != nil {
		return FileServiceCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare File Services capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		fileservices.SelectSMBRead,
		fileservices.SelectSMBSet,
		fileservices.SelectNFSRead,
		fileservices.SelectNFSSet,
		fileservices.SelectNFSAdvancedRead,
		fileservices.SelectNFSAdvancedSet,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return FileServiceCapabilities{}, CompatibilityReport{}, fmt.Errorf("select File Services backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilities := FileServiceCapabilities{
		SMB: controlpanel.FileServiceModuleCapabilities{
			Module: controlpanel.ModuleSMB,
			Read:   supported(0),
			Set:    supported(1),
		},
		NFS: controlpanel.FileServiceModuleCapabilities{
			Module:      controlpanel.ModuleNFS,
			Read:        supported(2),
			Set:         supported(3),
			SetAdvanced: supported(5),
		},
	}
	capabilityNames := []string{
		fileservices.SMBReadCapabilityName,
		fileservices.SMBSetCapabilityName,
		fileservices.NFSReadCapabilityName,
		fileservices.NFSSetCapabilityName,
		fileservices.NFSAdvancedReadCapabilityName,
		fileservices.NFSAdvancedSetCapabilityName,
	}
	for index, name := range capabilityNames {
		if supported(index) {
			c.target.AddCapability(name)
		}
	}
	return capabilities, c.target.Report(selections...), nil
}

func (c *Client) ApplyFileServiceChange(ctx context.Context, request FileServiceChangeRequest) (FileServiceMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, fileservices.APINames()...); err != nil {
		return FileServiceMutationResult{}, fmt.Errorf("prepare File Services mutation target: %w", err)
	}
	switch request.Protocol {
	case controlpanel.FileProtocolSMB:
		if request.SMB == nil || request.NFS != nil {
			return FileServiceMutationResult{}, fmt.Errorf("SMB mutation requires only the smb patch")
		}
		current, _, err := fileservices.ExecuteSMBRead(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return FileServiceMutationResult{}, fmt.Errorf("refresh SMB configuration before apply: %w", err)
		}
		effective := *request.SMB
		if request.SMB.Enabled != nil || request.SMB.Workgroup != nil {
			if effective.Enabled == nil {
				effective.Enabled = &current.Enabled
			}
			if effective.Workgroup == nil {
				effective.Workgroup = &current.Workgroup
			}
		}
		if request.SMB.MinimumProtocol != nil || request.SMB.MaximumProtocol != nil || request.SMB.TransportEncryption != nil || request.SMB.ServerSigning != nil {
			if effective.MinimumProtocol == nil {
				effective.MinimumProtocol = &current.MinimumProtocol
			}
			if effective.MaximumProtocol == nil {
				effective.MaximumProtocol = &current.MaximumProtocol
			}
			if effective.MaximumProtocol != nil && *effective.MaximumProtocol == controlpanel.SMBProtocol3 && effective.TransportEncryption == nil {
				effective.TransportEncryption = &current.TransportEncryption
			}
			if effective.ServerSigning == nil {
				effective.ServerSigning = &current.ServerSigning
			}
		}
		result, _, err := fileservices.ExecuteSMBSet(ctx, c.target, lockedExecutor{client: c}, effective)
		if err != nil {
			return FileServiceMutationResult{}, fmt.Errorf("apply SMB configuration: %w", err)
		}
		return result, nil
	case controlpanel.FileProtocolNFS:
		if request.NFS == nil || request.SMB != nil {
			return FileServiceMutationResult{}, fmt.Errorf("NFS mutation requires only the nfs patch")
		}
		if request.NFS.NFSv4Domain != nil {
			result, _, err := fileservices.ExecuteNFSAdvancedSet(ctx, c.target, lockedExecutor{client: c}, *request.NFS)
			if err != nil {
				return FileServiceMutationResult{}, fmt.Errorf("apply NFS advanced configuration: %w", err)
			}
			return result, nil
		}
		current, _, err := fileservices.ExecuteNFSRead(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return FileServiceMutationResult{}, fmt.Errorf("refresh NFS configuration before apply: %w", err)
		}
		effective := *request.NFS
		if effective.Enabled == nil {
			effective.Enabled = &current.Enabled
		}
		if effective.MaximumProtocol == nil {
			effective.MaximumProtocol = &current.MaximumProtocol
		}
		result, _, err := fileservices.ExecuteNFSSet(ctx, c.target, lockedExecutor{client: c}, effective)
		if err != nil {
			return FileServiceMutationResult{}, fmt.Errorf("apply NFS configuration: %w", err)
		}
		return result, nil
	default:
		return FileServiceMutationResult{}, fmt.Errorf("unsupported file protocol %q", request.Protocol)
	}
}
