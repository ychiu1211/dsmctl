package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/share"
)

func newShareCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "share", Short: "Inspect and manage DSM shared folders and permissions"}
	command.AddCommand(
		newShareCapabilitiesCommand(opts),
		newShareInventoryCommand(opts),
		newSharePlanCommand(opts),
		newShareApplyCommand(opts),
	)
	return command
}

func newSharePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a shared-folder change and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request share.ChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read shared-folder change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanShareChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "shared-folder change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newShareApplyCommand(opts *options) *cobra.Command {
	var inputPath, approveHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a shared-folder plan after validating its approval hash and precondition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.SharePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read shared-folder plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplySharePlan(cmd.Context(), plan, approveHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "shared-folder plan JSON file, or - for stdin")
	command.Flags().StringVar(&approveHash, "approve", "", "exact SHA-256 hash printed by share plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newShareCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show supported shared-folder operations and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetShareCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeShareCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newShareInventoryCommand(opts *options) *cobra.Command {
	var jsonOutput, includePermissions bool
	command := &cobra.Command{
		Use:   "inventory",
		Short: "Show shared folders and optionally expand user/group permissions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetShareState(cmd.Context(), opts.nas, includePermissions)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeShareInventory(cmd, result)
		},
	}
	command.Flags().BoolVar(&includePermissions, "include-permissions", false, "include the user/group permission matrix (additional DSM calls)")
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func writeShareCapabilities(cmd *cobra.Command, result application.ShareCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Inventory read:\t%s\n", yesNo(result.Capabilities.InventoryRead))
	fmt.Fprintf(writer, "Permission read:\t%s\n", yesNo(result.Capabilities.PermissionRead))
	fmt.Fprintf(writer, "Share create/update/delete:\t%s/%s/%s\n", yesNo(result.Capabilities.ShareCreate), yesNo(result.Capabilities.ShareUpdate), yesNo(result.Capabilities.ShareDelete))
	fmt.Fprintf(writer, "Permission write:\t%s\n", yesNo(result.Capabilities.PermissionWrite))
	fmt.Fprintf(writer, "Mutations:\t%s\n", yesNo(result.Capabilities.Mutations))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeShareInventory(cmd *cobra.Command, result application.ShareStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintln(writer, "\nSHARED FOLDERS")
	fmt.Fprintln(writer, "NAME\tVOLUME\tDESCRIPTION\tHIDDEN\tENCRYPTED\tACL\tSNAPSHOT\tQUOTA\tUSED")
	for _, folder := range result.Shares.Shares {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(folder.Name), valueOrDash(folder.VolumePath), valueOrDash(folder.Description), yesNo(folder.Hidden),
			yesNo(folder.Encrypted), yesNo(folder.ACLMode), yesNo(folder.SnapshotSupported), formatBytes(folder.QuotaBytes), formatBytes(folder.QuotaUsedBytes),
		)
	}
	if result.Shares.PermissionsIncluded {
		fmt.Fprintln(writer, "\nPERMISSIONS")
		fmt.Fprintln(writer, "SHARE\tTYPE\tPRINCIPAL\tDIRECT ACCESS\tINHERITED ACCESS\tCUSTOM\tMASKED\tACL")
		for _, folder := range result.Shares.Shares {
			for _, permission := range folder.Permissions {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					folder.Name, permission.PrincipalType, permission.Principal, permission.Access,
					valueOrDash(permission.InheritedAccess), yesNo(permission.Custom), yesNo(permission.Masked), yesNo(permission.ACLMode),
				)
			}
		}
	}
	return writer.Flush()
}
