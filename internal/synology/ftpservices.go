package synology

import (
	"context"
	"fmt"

	"github.com/ychiu1211/dsmctl/internal/domain/ftpservices"
	"github.com/ychiu1211/dsmctl/internal/synology/compatibility"
	ftpservicesop "github.com/ychiu1211/dsmctl/internal/synology/operations/ftpservices"
)

type FTPServicesState = ftpservices.State
type FTPServicesCapabilities = ftpservices.Capabilities
type FTPServicesChange = ftpservices.Change
type FTPServicesMutationResult = ftpservicesop.MutationResult

// FTPServicesState reads the plain-FTP/FTPS switches and, when its backend is
// available, the SFTP switch and port.
func (c *Client) FTPServicesState(ctx context.Context) (FTPServicesState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, ftpservicesop.APINames()...); err != nil {
		return FTPServicesState{}, fmt.Errorf("prepare FTP services target: %w", err)
	}
	ftp, _, err := ftpservicesop.ExecuteFTPRead(ctx, c.target, lockedExecutor{client: c})
	if err != nil {
		return FTPServicesState{}, fmt.Errorf("get FTP settings: %w", err)
	}
	c.target.AddCapability(ftpservicesop.FTPReadCapabilityName)
	state := FTPServicesState{FTP: ftpservices.FTPState{Plain: ftp.Plain, FTPS: ftp.FTPS}}

	sftpSelection, selectionErr := ftpservicesop.SelectSFTPRead(c.target)
	if selectionErr != nil && !compatibility.IsUnsupported(selectionErr) {
		return FTPServicesState{}, fmt.Errorf("select SFTP read backend: %w", selectionErr)
	}
	if sftpSelection.Supported {
		sftp, _, sftpErr := ftpservicesop.ExecuteSFTPRead(ctx, c.target, lockedExecutor{client: c})
		if sftpErr != nil {
			return FTPServicesState{}, fmt.Errorf("get SFTP settings: %w", sftpErr)
		}
		state.SFTP = &ftpservices.SFTPState{Enabled: sftp.Enabled, Port: sftp.Port}
		c.target.AddCapability(ftpservicesop.SFTPReadCapabilityName)
	}
	return state, nil
}

// FTPServicesCapabilities reports the independently selected FTP and SFTP
// backends.
func (c *Client) FTPServicesCapabilities(ctx context.Context) (FTPServicesCapabilities, CompatibilityReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, ftpservicesop.APINames()...); err != nil {
		return FTPServicesCapabilities{}, CompatibilityReport{}, fmt.Errorf("prepare FTP services capabilities target: %w", err)
	}
	selectors := []func(compatibility.Target) (compatibility.Selection, error){
		ftpservicesop.SelectFTPRead,
		ftpservicesop.SelectFTPSet,
		ftpservicesop.SelectSFTPRead,
		ftpservicesop.SelectSFTPSet,
	}
	selections := make([]compatibility.Selection, 0, len(selectors))
	for _, selectOperation := range selectors {
		selection, err := selectOperation(c.target)
		if err != nil && !compatibility.IsUnsupported(err) {
			return FTPServicesCapabilities{}, CompatibilityReport{}, fmt.Errorf("select FTP services backend: %w", err)
		}
		selections = append(selections, selection)
	}
	supported := func(index int) bool { return index < len(selections) && selections[index].Supported }
	capabilityNames := []string{
		ftpservicesop.FTPReadCapabilityName,
		ftpservicesop.FTPSetCapabilityName,
		ftpservicesop.SFTPReadCapabilityName,
		ftpservicesop.SFTPSetCapabilityName,
	}
	for index, name := range capabilityNames {
		if supported(index) {
			c.target.AddCapability(name)
		}
	}
	capabilities := FTPServicesCapabilities{
		FTPRead:  supported(0),
		FTPSet:   supported(1),
		SFTPRead: supported(2),
		SFTPSet:  supported(3),
	}
	return capabilities, c.target.Report(selections...), nil
}

// ApplyFTPServicesChange applies a patch. DSM's FTP set requires both switches,
// so an FTP change is merged into a freshly read pair before submitting; the
// SFTP set requires the enable switch and always resends the port to preserve
// it. Each touched protocol yields one mutation result.
func (c *Client) ApplyFTPServicesChange(ctx context.Context, change FTPServicesChange) ([]FTPServicesMutationResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.prepareCompatibilityTargetLocked(ctx, ftpservicesop.APINames()...); err != nil {
		return nil, fmt.Errorf("prepare FTP services mutation target: %w", err)
	}
	results := make([]FTPServicesMutationResult, 0, 2)
	if change.FTP != nil {
		current, _, err := ftpservicesop.ExecuteFTPRead(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return nil, fmt.Errorf("refresh FTP settings before apply: %w", err)
		}
		desired := current
		if change.FTP.Plain != nil {
			desired.Plain = *change.FTP.Plain
		}
		if change.FTP.FTPS != nil {
			desired.FTPS = *change.FTP.FTPS
		}
		result, _, err := ftpservicesop.ExecuteFTPSet(ctx, c.target, lockedExecutor{client: c}, desired)
		if err != nil {
			return nil, fmt.Errorf("apply FTP settings: %w", err)
		}
		results = append(results, result)
	}
	if change.SFTP != nil {
		current, _, err := ftpservicesop.ExecuteSFTPRead(ctx, c.target, lockedExecutor{client: c})
		if err != nil {
			return nil, fmt.Errorf("refresh SFTP settings before apply: %w", err)
		}
		desired := current
		if change.SFTP.Enabled != nil {
			desired.Enabled = *change.SFTP.Enabled
		}
		if change.SFTP.Port != nil {
			desired.Port = *change.SFTP.Port
		}
		result, _, err := ftpservicesop.ExecuteSFTPSet(ctx, c.target, lockedExecutor{client: c}, desired)
		if err != nil {
			return nil, fmt.Errorf("apply SFTP settings: %w", err)
		}
		results = append(results, result)
	}
	return results, nil
}
