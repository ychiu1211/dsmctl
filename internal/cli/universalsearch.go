package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/domain/universalsearch"
)

func newUniversalSearchCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "universal-search",
		Short:   "Inspect the Universal Search file index (indexed folders and status)",
		Aliases: []string{"usearch"},
	}
	command.AddCommand(
		newUniversalSearchCapabilitiesCommand(opts),
		newUniversalSearchFoldersCommand(opts),
		newUniversalSearchStatusCommand(opts),
	)
	return command
}

func newUniversalSearchCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which Universal Search reads are available and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetUniversalSearchCapabilities(cmd.Context(), opts.nas)
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
			fmt.Fprintf(writer, "Indexed-folder read:\t%s\n", yesNo(result.Capabilities.FolderRead))
			fmt.Fprintf(writer, "Index-status read:\t%s\n", yesNo(result.Capabilities.StatusRead))
			fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tDSM API")
			for _, operation := range result.Report.Operations {
				api := "-"
				if operation.API != "" {
					api = fmt.Sprintf("%s v%d", operation.API, operation.Version)
				}
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), api)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newUniversalSearchFoldersCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "folders",
		Short: "List the folders in the Universal Search file index",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetUniversalSearchFolders(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Indexed folders:\t%d\n", result.Folders.Total)
			fmt.Fprintln(writer, "\nPATH\tNAME\tOWNER\tPAUSED\tCONTENT")
			for _, folder := range result.Folders.Folders {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
					valueOrDash(folder.Path), valueOrDash(folder.Name), valueOrDash(folder.Owner),
					yesNo(folder.Paused), formatContentTypes(folder))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newUniversalSearchStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show the overall Universal Search index status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetUniversalSearchStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			state := "idle"
			if result.Status.Indexing {
				state = "indexing"
			}
			fmt.Fprintf(writer, "Index state:\t%s\n", state)
			fmt.Fprintf(writer, "Content index:\t%s\n", valueOrDash(result.Status.Index))
			fmt.Fprintf(writer, "Term index:\t%s\n", valueOrDash(result.Status.Term))
			if result.Status.Progress != nil {
				fmt.Fprintf(writer, "Progress:\t%d%%\n", *result.Status.Progress)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func formatContentTypes(folder universalsearch.IndexedFolder) string {
	types := ""
	appendType := func(label string, on bool) {
		if on {
			if types != "" {
				types += ","
			}
			types += label
		}
	}
	appendType("audio", folder.ContentTypes.Audio)
	appendType("video", folder.ContentTypes.Video)
	appendType("photo", folder.ContentTypes.Photo)
	appendType("document", folder.ContentTypes.Document)
	if types == "" {
		return "-"
	}
	return types
}
