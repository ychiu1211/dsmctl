package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/office"
)

func newOfficeCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "office",
		Short: "Inspect and manage the Synology Office package settings",
	}
	command.AddCommand(
		newOfficeCapabilitiesCommand(opts),
		newOfficeInfoCommand(opts),
		newOfficeSettingsCommand(opts),
		newOfficePreferencesCommand(opts),
		newOfficeFontsCommand(opts),
		newOfficePlanCommand(opts),
		newOfficeApplyCommand(opts),
	)
	return command
}

func newOfficeCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Office settings support and the installed package",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetOfficeCapabilities(cmd.Context(), opts.nas)
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
			fmt.Fprintf(writer, "Info read:\t%s\n", yesNo(result.Capabilities.InfoRead))
			fmt.Fprintf(writer, "System read:\t%s\n", yesNo(result.Capabilities.SystemRead))
			fmt.Fprintf(writer, "System set:\t%s\n", yesNo(result.Capabilities.SystemSet))
			fmt.Fprintf(writer, "Preferences read:\t%s\n", yesNo(result.Capabilities.PreferencesRead))
			fmt.Fprintf(writer, "Preferences set:\t%s\n", yesNo(result.Capabilities.PreferencesSet))
			fmt.Fprintf(writer, "Fonts read:\t%s\n", yesNo(result.Capabilities.FontsRead))
			fmt.Fprintf(writer, "Fonts set:\t%s\n", yesNo(result.Capabilities.FontsSet))
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

func newOfficeInfoCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "info",
		Short: "Show Synology Office deployment info",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetOfficeInfo(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			info := result.Info
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Version:\t%s\n", info.Version)
			fmt.Fprintf(writer, "Session user is manager:\t%s\n", yesNo(info.IsManager))
			fmt.Fprintf(writer, "Document schema:\t%d\n", info.SchemaDocument)
			fmt.Fprintf(writer, "Spreadsheet schema:\t%d\n", info.SchemaSpreadsheet)
			fmt.Fprintf(writer, "Slides schema:\t%d\n", info.SchemaSlides)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newOfficeSettingsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "settings",
		Short: "Show system-wide Synology Office settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetOfficeSettings(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Version-history auto-cleanup:\t%s\n", yesNo(result.Settings.HistoryPrune))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newOfficePreferencesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "preferences",
		Short: "Show the calling user's Synology Office editor preferences",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetOfficePreferences(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			preferences := result.Preferences
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Ruler:\t%s\n", yesNo(preferences.Ruler))
			fmt.Fprintf(writer, "Formula preview:\t%s\n", yesNo(preferences.FormulaPreview))
			fmt.Fprintf(writer, "Formula panel opened:\t%s\n", yesNo(preferences.FormulaPanelOpened))
			fmt.Fprintf(writer, "Formula panel expanded:\t%s\n", yesNo(preferences.FormulaPanelExpanded))
			fmt.Fprintf(writer, "Default locale:\t%s\n", valueOrDash(preferences.DefaultLocale))
			fmt.Fprintf(writer, "AI translator language:\t%s\n", valueOrDash(preferences.AITranslatorLanguage))
			fmt.Fprintf(writer, "AI helper languages:\t%s\n", valueOrDash(strings.Join(preferences.AIHelperLanguages, ", ")))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newOfficeFontsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "fonts",
		Short: "List the Synology Office font inventory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetOfficeFonts(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Fonts:\t%d\n", len(result.Fonts))
			fmt.Fprintln(writer, "\nNAME\tDISPLAY NAME\tCUSTOM\tENABLED")
			for _, font := range result.Fonts {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", font.Name, valueOrDash(font.DisplayName), yesNo(font.Custom), yesNo(font.Enabled))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newOfficePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate an Office settings patch and emit an approval plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request office.Change
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read Office change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanOfficeChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Office change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newOfficeApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply an Office settings plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.OfficePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read Office plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyOfficePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "Office plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by the Office plan")
	_ = command.MarkFlagRequired("approve")
	return command
}
