package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/packagecenter"
)

func newPackageCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "package",
		Aliases: []string{"pkg", "package-center"},
		Short:   "Inspect and manage DSM Package Center packages and settings",
	}
	command.AddCommand(
		newPackageCapabilitiesCommand(opts),
		newPackageInventoryCommand(opts),
		newPackageAvailableCommand(opts),
		newPackageInstallCommand(opts),
		newPackageUpdateCommand(opts),
		newPackageSettingsCommand(opts),
		newPackagePlanCommand(opts),
		newPackageApplyCommand(opts),
	)
	return command
}

func newPackageInstallCommand(opts *options) *cobra.Command {
	var volume, approvalHash, spk string
	var start, quick, allowUnsigned bool
	command := &cobra.Command{
		Use:   "install [package-id]",
		Short: "Install a package — from the online server by id, or a local .spk with --spk (plan by default; --approve to run)",
		Long: "Install a package (plan by default; pass --approve <hash> to run).\n\n" +
			"Two modes:\n" +
			"  online: dsmctl package install <package-id> --volume /volume1\n" +
			"          resolves the package from Synology's online server and has DSM download it.\n" +
			"  local:  dsmctl package install --spk ./foo.spk --volume /volume1\n" +
			"          uploads a local .spk file and installs it (Package Center \"Manual Install\").",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)

			if spk != "" {
				if len(args) != 0 {
					return fmt.Errorf("provide either a package id (online install) or --spk (local install), not both")
				}
				plan, err := service.PlanPackageLocalInstall(cmd.Context(), opts.nas, spk, volume, start, allowUnsigned)
				if err != nil {
					return err
				}
				if approvalHash == "" {
					return encodeIndentedJSON(cmd.OutOrStdout(), plan)
				}
				result, err := service.ApplyPackageLocalInstallPlan(cmd.Context(), plan, approvalHash)
				if err != nil {
					return err
				}
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}

			if len(args) != 1 {
				return fmt.Errorf("install requires a package id, or use --spk <file> for a local package")
			}
			plan, err := service.PlanPackageInstall(cmd.Context(), opts.nas, args[0], volume, start, quick)
			if err != nil {
				return err
			}
			if approvalHash == "" {
				// Plan only: show what would happen and the approval hash.
				return encodeIndentedJSON(cmd.OutOrStdout(), plan)
			}
			result, err := service.ApplyPackageInstallPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVar(&spk, "spk", "", "path to a local .spk file to upload and install (Manual Install)")
	command.Flags().StringVar(&volume, "volume", "", "target install volume path (e.g. /volume1)")
	command.Flags().BoolVar(&start, "start", true, "start the package after install")
	command.Flags().BoolVar(&quick, "quick", true, "online install: quick install with defaults (no configuration wizard)")
	command.Flags().BoolVar(&allowUnsigned, "allow-unsigned", false, "local install: disable code-signature enforcement to install a package not signed by Synology")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the install plan to execute the install")
	_ = command.MarkFlagRequired("volume")
	return command
}

func newPackageUpdateCommand(opts *options) *cobra.Command {
	var approvalHash string
	command := &cobra.Command{
		Use:   "update <package-id>",
		Short: "Update an installed package to the offered version (plan by default; --approve to run)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanPackageUpdate(cmd.Context(), opts.nas, args[0])
			if err != nil {
				return err
			}
			if approvalHash == "" {
				// Plan only: show what would happen and the approval hash.
				return encodeIndentedJSON(cmd.OutOrStdout(), plan)
			}
			result, err := service.ApplyPackageInstallPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the update plan to execute the update")
	return command
}

func newPackageAvailableCommand(opts *options) *cobra.Command {
	var jsonOutput, updatesOnly bool
	command := &cobra.Command{
		Use:   "available",
		Short: "List packages offered by the online package server (Synology repository)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetPackageCatalog(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			shown := 0
			fmt.Fprintln(writer, "\nID\tVERSION\tINSTALLED\tUPDATE\tBETA\tSIZE")
			for _, pkg := range result.Catalog.Packages {
				if updatesOnly && !pkg.UpdateAvailable {
					continue
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%d\n", pkg.ID, valueOrDash(pkg.Version), yesNo(pkg.Installed), yesNo(pkg.UpdateAvailable), yesNo(pkg.Beta), pkg.Size)
				shown++
			}
			fmt.Fprintf(writer, "\nShown:\t%d of %d offered\n", shown, len(result.Catalog.Packages))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().BoolVar(&updatesOnly, "updates", false, "show only installed packages with an available update")
	return command
}

func newPackageCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Package Center operation support and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetPackageCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writePackageCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newPackageInventoryCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "inventory",
		Short: "List installed packages and their run status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetPackageState(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writePackageInventory(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newPackageSettingsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "settings",
		Short: "Show global Package Center settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetPackageSettings(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writePackageSettings(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newPackagePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a settings or lifecycle change and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request packagecenter.ChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read Package Center change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanPackageChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Package Center change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newPackageApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a Package Center plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.PackagePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read Package Center plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyPackagePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Package Center plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by package plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func writePackageCapabilities(cmd *cobra.Command, result application.PackageCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED")
	fmt.Fprintf(writer, "inventory read\t%s\n", yesNo(result.Capabilities.InventoryRead))
	fmt.Fprintf(writer, "settings read\t%s\n", yesNo(result.Capabilities.SettingsRead))
	fmt.Fprintf(writer, "settings set\t%s\n", yesNo(result.Capabilities.SettingsSet))
	fmt.Fprintf(writer, "start\t%s\n", yesNo(result.Capabilities.Start))
	fmt.Fprintf(writer, "stop\t%s\n", yesNo(result.Capabilities.Stop))
	fmt.Fprintf(writer, "uninstall\t%s\n", yesNo(result.Capabilities.Uninstall))
	fmt.Fprintf(writer, "install\t%s\n", yesNo(result.Capabilities.Install))
	fmt.Fprintf(writer, "update\t%s\n", yesNo(result.Capabilities.Update))
	fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writePackageInventory(cmd *cobra.Command, result application.PackageStateResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	if len(result.State.Packages) == 0 {
		fmt.Fprintln(writer, "No packages installed.")
		return writer.Flush()
	}
	fmt.Fprintln(writer, "\nID\tNAME\tVERSION\tSTATUS\tBETA\tSTART\tSTOP\tUNINSTALL")
	for _, pkg := range result.State.Packages {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pkg.ID, valueOrDash(pkg.Name), valueOrDash(pkg.Version), string(pkg.Status),
			yesNo(pkg.Beta), yesNo(pkg.CanStart), yesNo(pkg.CanStop), yesNo(pkg.CanUninstall))
	}
	return writer.Flush()
}

func writePackageSettings(cmd *cobra.Command, result application.PackageSettingsResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Trust level:\t%s\n", string(result.Settings.TrustLevel))
	fmt.Fprintf(writer, "Automatic updates:\t%s\n", yesNo(result.Settings.AutoUpdateEnabled))
	fmt.Fprintf(writer, "Important updates only:\t%s\n", yesNo(result.Settings.AutoUpdateImportantOnly))
	return writer.Flush()
}
