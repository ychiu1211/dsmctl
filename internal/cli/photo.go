package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/photos"
)

func newPhotoCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "photo",
		Short: "Inspect and manage the Synology Photos package",
	}
	command.AddCommand(
		newPhotoCapabilitiesCommand(opts),
		newPhotoSettingsCommand(opts),
		newPhotoPlanCommand(opts),
		newPhotoApplyCommand(opts),
	)
	return command
}

func newPhotoCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Photos administration support and the installed package",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetPhotosCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			pkg := result.Capabilities.Package
			fmt.Fprintf(writer, "Package:\t%s %s (%s)\n", pkg.ID, valueOrDash(pkg.Version), packageRunState(pkg.Installed, pkg.Running))
			fmt.Fprintf(writer, "Admin read:\t%s\n", yesNo(result.Capabilities.AdminRead))
			fmt.Fprintf(writer, "Admin set:\t%s\n", yesNo(result.Capabilities.AdminSet))
			fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
			for _, operation := range result.Report.Operations {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func packageRunState(installed, running bool) string {
	if !installed {
		return "not installed"
	}
	if running {
		return "running"
	}
	return "stopped"
}

func newPhotoSettingsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "settings",
		Short: "Show Synology Photos administration settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetPhotosSettings(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			s := result.Settings
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Version:\t%s\n", valueOrDash(s.PackageVersion))
			fmt.Fprintf(writer, "Face recognition:\t%s\n", yesNo(s.FaceRecognition))
			fmt.Fprintf(writer, "Concept grouping:\t%s\n", yesNo(s.ConceptGrouping))
			fmt.Fprintf(writer, "Similar grouping:\t%s\n", yesNo(s.SimilarGrouping))
			fmt.Fprintf(writer, "User sharing:\t%s\n", yesNo(s.UserSharing))
			fmt.Fprintf(writer, "Show info to guest:\t%s\n", yesNo(s.ShowInfoToGuest))
			fmt.Fprintf(writer, "Personal recycle bin:\t%s\n", yesNo(s.PersonalRecycleBin))
			fmt.Fprintf(writer, "Shared recycle bin:\t%s\n", yesNo(s.SharedRecycleBin))
			fmt.Fprintf(writer, "Default thumbnail size:\t%s\n", valueOrDash(s.DefaultThumbnailSize))
			fmt.Fprintf(writer, "Excluded extensions:\t%s\n", valueOrDash(strings.Join(s.ExcludeExtensions, ", ")))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newPhotoPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a Photos administration patch and emit an approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request photos.AdminChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read Photos change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanPhotosChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Photos change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newPhotoApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a Photos administration plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.PhotosPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read Photos plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyPhotosPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Photos plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the Photos plan")
	_ = command.MarkFlagRequired("approve")
	return command
}
