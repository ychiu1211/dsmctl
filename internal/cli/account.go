package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/identity"
)

func newAccountCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "account", Short: "Inspect and manage DSM users, groups, quotas, and application access"}
	command.AddCommand(
		newAccountCapabilitiesCommand(opts),
		newAccountInventoryCommand(opts),
		newAccountPlanCommand(opts),
		newAccountApplyCommand(opts),
	)
	return command
}

func newAccountPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate an account change and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request identity.ChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read account change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanIdentityChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "account change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newAccountApplyCommand(opts *options) *cobra.Command {
	var inputPath, approveHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply an account plan after validating its approval hash and precondition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.IdentityPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read account plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyIdentityPlan(cmd.Context(), plan, approveHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "account plan JSON file, or - for stdin")
	command.Flags().StringVar(&approveHash, "approve", "", "exact SHA-256 hash printed by account plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func decodeJSONInput(cmd *cobra.Command, path string, destination any) error {
	var reader io.Reader = cmd.InOrStdin()
	var file *os.File
	if path != "" && path != "-" {
		opened, err := os.Open(path)
		if err != nil {
			return err
		}
		file = opened
		defer file.Close()
		reader = file
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("input contains more than one JSON value")
		}
		return fmt.Errorf("read trailing JSON: %w", err)
	}
	return nil
}

func encodeIndentedJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func encodeJSONOutput(cmd *cobra.Command, path string, value any) error {
	if path == "" || path == "-" {
		return encodeIndentedJSON(cmd.OutOrStdout(), value)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := encodeIndentedJSON(file, value); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func newAccountCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show supported account operations and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetIdentityCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeAccountCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newAccountInventoryCommand(opts *options) *cobra.Command {
	var jsonOutput, includeMemberships, includeQuotas, includeApplicationPrivileges bool
	var principalType, principal string
	command := &cobra.Command{
		Use:   "inventory",
		Short: "Show local DSM identity state with optional membership, quota, and application expansion",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetIdentityStateWithQuery(cmd.Context(), opts.nas, identity.StateQuery{
				IncludeMemberships: includeMemberships, IncludeQuotas: includeQuotas,
				IncludeApplicationPrivileges: includeApplicationPrivileges,
				PrincipalType:                principalType, Principal: principal,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeAccountInventory(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().BoolVar(&includeMemberships, "memberships", false, "include user-to-group memberships")
	command.Flags().BoolVar(&includeQuotas, "quotas", false, "include quota assignments")
	command.Flags().BoolVar(&includeApplicationPrivileges, "application-privileges", false, "include applications and explicit privilege rules")
	command.Flags().StringVar(&principalType, "principal-type", "", "optional principal filter type: user or group")
	command.Flags().StringVar(&principal, "principal", "", "optional principal name filter")
	return command
}

func writeAccountCapabilities(cmd *cobra.Command, result application.IdentityCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Inventory read:\t%s\n", yesNo(result.Capabilities.InventoryRead))
	fmt.Fprintf(writer, "User create/update/delete:\t%s/%s/%s\n", yesNo(result.Capabilities.UserCreate), yesNo(result.Capabilities.UserUpdate), yesNo(result.Capabilities.UserDelete))
	fmt.Fprintf(writer, "Group create/update/delete:\t%s/%s/%s\n", yesNo(result.Capabilities.GroupCreate), yesNo(result.Capabilities.GroupUpdate), yesNo(result.Capabilities.GroupDelete))
	fmt.Fprintf(writer, "Membership read/set:\t%s/%s\n", yesNo(result.Capabilities.MembershipRead), yesNo(result.Capabilities.MembershipSet))
	fmt.Fprintf(writer, "Quota read/set:\t%s/%s\n", yesNo(result.Capabilities.QuotaRead), yesNo(result.Capabilities.QuotaSet))
	fmt.Fprintf(writer, "Application privilege read/set:\t%s/%s\n", yesNo(result.Capabilities.ApplicationPrivilegeRead), yesNo(result.Capabilities.ApplicationPrivilegeSet))
	fmt.Fprintf(writer, "Application privilege preview:\t%s\n", yesNo(result.Capabilities.ApplicationPrivilegePreview))
	fmt.Fprintf(writer, "Mutations:\t%s\n", yesNo(result.Capabilities.Mutations))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeAccountInventory(cmd *cobra.Command, result application.IdentityStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintln(writer, "\nUSERS")
	fmt.Fprintln(writer, "NAME\tID\tEMAIL\tDESCRIPTION\tEXPIRED\tPASSWORD NEVER EXPIRES\t2FA")
	for _, user := range result.Identity.Users {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			valueOrDash(user.Name), valueOrDash(user.ID), valueOrDash(user.Email), valueOrDash(user.Description),
			yesNo(user.Expired), yesNo(user.PasswordNeverExpires), valueOrDash(user.TwoFactorStatus),
		)
	}
	fmt.Fprintln(writer, "\nGROUPS")
	fmt.Fprintln(writer, "NAME\tID\tDESCRIPTION")
	for _, group := range result.Identity.Groups {
		fmt.Fprintf(writer, "%s\t%s\t%s\n", valueOrDash(group.Name), valueOrDash(group.ID), valueOrDash(group.Description))
	}
	if result.Identity.Memberships != nil {
		fmt.Fprintln(writer, "\nMEMBERSHIPS")
		fmt.Fprintln(writer, "USER\tGROUPS")
		for _, membership := range result.Identity.Memberships {
			fmt.Fprintf(writer, "%s\t%s\n", membership.User, strings.Join(membership.Groups, ","))
		}
	}
	if result.Identity.Quotas != nil {
		fmt.Fprintln(writer, "\nQUOTAS")
		fmt.Fprintln(writer, "TYPE\tPRINCIPAL\tTARGET TYPE\tTARGET\tQUOTA MIB\tSTATUS")
		for _, quota := range result.Identity.Quotas {
			for _, limit := range quota.Limits {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%s\n", quota.PrincipalType, quota.Principal, limit.TargetType, limit.Target, limit.QuotaMiB, valueOrDash(limit.Status))
			}
		}
	}
	if result.Identity.Applications != nil {
		fmt.Fprintln(writer, "\nAPPLICATIONS")
		fmt.Fprintln(writer, "ID\tNAME")
		for _, application := range result.Identity.Applications {
			fmt.Fprintf(writer, "%s\t%s\n", application.ID, valueOrDash(application.Name))
		}
		fmt.Fprintln(writer, "\nAPPLICATION PRIVILEGES")
		fmt.Fprintln(writer, "TYPE\tPRINCIPAL\tAPPLICATION\tACCESS")
		for _, assignment := range result.Identity.ApplicationPrivileges {
			for _, permission := range assignment.Permissions {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", assignment.PrincipalType, assignment.Principal, permission.ApplicationID, permission.Access)
			}
		}
	}
	return writer.Flush()
}
