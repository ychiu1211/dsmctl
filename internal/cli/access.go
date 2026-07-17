package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/access"
)

func newAccessCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "access",
		Short: "Explain effective shared-folder and application access",
	}
	command.AddCommand(newAccessExplainCommand(opts))
	return command
}

func newAccessExplainCommand(opts *options) *cobra.Command {
	var query access.Query
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "explain",
		Short: "Explain why one user or group can or cannot access one resource",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ExplainEffectiveAccess(cmd.Context(), opts.nas, query)
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return writeEffectiveAccess(cmd, result)
		},
	}
	command.Flags().StringVar(&query.PrincipalType, "principal-type", "", "principal type: user or group")
	command.Flags().StringVar(&query.Principal, "principal", "", "local DSM user or group name")
	command.Flags().StringVar(&query.ResourceType, "resource-type", "", "resource type: share or application")
	command.Flags().StringVar(&query.Resource, "resource", "", "shared-folder name or application ID")
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	_ = command.MarkFlagRequired("principal-type")
	_ = command.MarkFlagRequired("principal")
	_ = command.MarkFlagRequired("resource-type")
	_ = command.MarkFlagRequired("resource")
	return command
}

func writeEffectiveAccess(cmd *cobra.Command, result application.EffectiveAccessResult) error {
	explanation := result.Explanation
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Principal:\t%s %s\n", explanation.PrincipalType, explanation.Principal)
	fmt.Fprintf(writer, "Resource:\t%s %s\n", explanation.ResourceType, explanation.Resource)
	fmt.Fprintf(writer, "Effective access:\t%s\n", explanation.EffectiveAccess)
	fmt.Fprintf(writer, "Determinate:\t%s\n", yesNo(explanation.Determinate))
	fmt.Fprintf(writer, "Summary:\t%s\n", explanation.Summary)
	if len(explanation.Limitations) > 0 {
		fmt.Fprintf(writer, "Limitations:\t%s\n", strings.Join(explanation.Limitations, "; "))
	}
	fmt.Fprintln(writer, "\nEVIDENCE")
	fmt.Fprintln(writer, "SOURCE\tTYPE\tPRINCIPAL\tACCESS\tINHERITED ACCESS\tCUSTOM\tMASKED\tALLOW IP\tDENY IP\tREASON")
	for _, evidence := range explanation.Evidence {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			evidence.Source, evidence.PrincipalType, evidence.Principal, evidence.Access,
			valueOrDash(evidence.InheritedAccess), yesNo(evidence.Custom), yesNo(evidence.Masked),
			strings.Join(evidence.AllowIP, ","), strings.Join(evidence.DenyIP, ","), evidence.Reason,
		)
	}
	return writer.Flush()
}
