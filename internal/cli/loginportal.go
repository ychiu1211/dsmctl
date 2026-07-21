package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/loginportal"
)

func newControlPanelLoginPortalCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "login-portal",
		Aliases: []string{"loginportal"},
		Short:   "Inspect and manage the Login Portal (DSM access, application portals, reverse proxy)",
		Long: "Read and change the Control Panel > Login Portal surface: the DSM web-service access settings (ports, " +
			"HTTPS, HTTP->HTTPS redirect, HSTS, HTTP/2, customized domain), the per-application portals, and the " +
			"reverse-proxy rules. Reads are safe; every change goes through the guarded plan/apply contract. A " +
			"DSM-access change is high risk and refuses, without an explicit override, any change that would sever " +
			"the transport dsmctl is connected on.",
	}
	command.AddCommand(
		newLoginPortalCapabilitiesCommand(opts),
		newLoginPortalDSMCommand(opts),
		newLoginPortalApplicationsCommand(opts),
		newLoginPortalReverseProxyCommand(opts),
		newLoginPortalDSMPlanCommand(opts),
		newLoginPortalDSMApplyCommand(opts),
		newLoginPortalApplicationPlanCommand(opts),
		newLoginPortalApplicationApplyCommand(opts),
		newLoginPortalReverseProxyCreatePlanCommand(opts),
		newLoginPortalReverseProxyDeletePlanCommand(opts),
		newLoginPortalReverseProxyApplyCommand(opts),
	)
	return command
}

func newLoginPortalDSMPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "dsm-plan",
		Short: "Validate a DSM web-service change and emit an approval plan as JSON",
		Long: "Validate a patch-only DSM web-service change (http_port, https_port, https_enabled, " +
			"http_redirect_enabled, hsts_enabled, http2_enabled, custom_domain_enabled, custom_domain, " +
			"external_hostname) and return an approval plan bound to the complete observed settings and the current " +
			"dsmctl transport. Every DSM web-service change is high risk. A change that would sever the transport " +
			"dsmctl is connected on (moving/disabling the current HTTPS port or scheme, forcing a redirect that " +
			"bounces the current session, or enabling HSTS) is refused without allow_connectivity_break. This command " +
			"never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request loginportal.DSMWebServiceChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read DSM web-service change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanDSMWebServiceChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "DSM web-service change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newLoginPortalDSMApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "dsm-apply",
		Short: "Apply a DSM web-service plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.DSMWebServicePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read DSM web-service plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyDSMWebServicePlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "DSM web-service plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by login-portal dsm-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newLoginPortalApplicationPlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "application-plan",
		Short: "Validate an application-portal change and emit an approval plan as JSON",
		Long: "Validate a patch-only application-portal change (redirect_https, alias, http_port, https_port) keyed by " +
			"app_id and return an approval plan bound to the observed portal. Classified medium risk. This command " +
			"never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request loginportal.ApplicationPortalChange
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read application portal change: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanApplicationPortalChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "application portal change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newLoginPortalApplicationApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "application-apply",
		Short: "Apply an application-portal plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.ApplicationPortalPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read application portal plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyApplicationPortalPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "application portal plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by login-portal application-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newLoginPortalReverseProxyCreatePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "reverse-proxy-create-plan",
		Short: "Validate a reverse-proxy rule creation and emit an approval plan as JSON",
		Long: "Validate a reverse-proxy rule to create (description, frontend, backend, and optional custom headers) " +
			"and return an approval plan bound to the COMPLETE observed rule set. A secret header value must use " +
			"credential_ref (env:NAME); it is resolved only at apply time and never stored in the plan. Classified " +
			"medium risk. This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request loginportal.ReverseProxyRuleCreate
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read reverse proxy create: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanReverseProxyCreate(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "reverse proxy create JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newLoginPortalReverseProxyDeletePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "reverse-proxy-delete-plan",
		Short: "Validate a reverse-proxy rule deletion and emit an approval plan as JSON",
		Long: "Validate a reverse-proxy rule to delete (keyed by uuid) and return an approval plan bound to the " +
			"COMPLETE observed rule set, so a concurrent edit invalidates a stale plan. This command never mutates DSM.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request loginportal.ReverseProxyRuleDelete
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read reverse proxy delete: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanReverseProxyDelete(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "reverse proxy delete JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newLoginPortalReverseProxyApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "reverse-proxy-apply",
		Short: "Apply a reverse-proxy create/delete plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.ReverseProxyPlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read reverse proxy plan: %w", err)
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyReverseProxyPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "reverse proxy plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by login-portal reverse-proxy-*-plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newLoginPortalCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show Login Portal read support and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetLoginPortalCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Module:\t%s\n", result.Capabilities.Module)
			fmt.Fprintf(writer, "dsm web service read\t%s\n", yesNo(result.Capabilities.DSMWebServiceRead))
			fmt.Fprintf(writer, "external domain read\t%s\n", yesNo(result.Capabilities.ExternalDomainRead))
			fmt.Fprintf(writer, "application portal read\t%s\n", yesNo(result.Capabilities.ApplicationPortalRead))
			fmt.Fprintf(writer, "reverse proxy read\t%s\n", yesNo(result.Capabilities.ReverseProxyRead))
			fmt.Fprintf(writer, "dsm web service write\t%s\n", yesNo(result.Capabilities.DSMWebServiceWrite))
			fmt.Fprintf(writer, "external domain write\t%s\n", yesNo(result.Capabilities.ExternalDomainWrite))
			fmt.Fprintf(writer, "application portal write\t%s\n", yesNo(result.Capabilities.ApplicationPortalWrite))
			fmt.Fprintf(writer, "reverse proxy write\t%s\n", yesNo(result.Capabilities.ReverseProxyWrite))
			fmt.Fprintln(writer, "\nOPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
			for _, op := range result.Report.Operations {
				fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", op.Operation, yesNo(op.Supported), valueOrDash(op.Backend), valueOrDash(op.API), op.Version)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newLoginPortalDSMCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "dsm",
		Short: "Show DSM web-service access settings (ports, HTTPS, redirect, HSTS, HTTP/2, domain)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDSMWebService(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			s := result.Settings
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "HTTP port:\t%d\n", s.HTTPPort)
			fmt.Fprintf(writer, "HTTPS port:\t%d\n", s.HTTPSPort)
			fmt.Fprintf(writer, "HTTPS enabled:\t%s\n", yesNo(s.HTTPSEnabled))
			fmt.Fprintf(writer, "HTTP->HTTPS redirect:\t%s\n", yesNo(s.HTTPRedirectEnabled))
			fmt.Fprintf(writer, "HSTS enabled:\t%s\n", yesNo(s.HSTSEnabled))
			fmt.Fprintf(writer, "HTTP/2 enabled:\t%s\n", yesNo(s.HTTP2Enabled))
			fmt.Fprintf(writer, "Customized domain enabled:\t%s\n", yesNo(s.CustomDomainEnabled))
			fmt.Fprintf(writer, "Customized domain:\t%s\n", valueOrDash(s.CustomDomain))
			if s.ExternalDomainSupported {
				fmt.Fprintf(writer, "External hostname:\t%s\n", valueOrDash(s.ExternalHostname))
			} else {
				fmt.Fprintf(writer, "External hostname:\t%s\n", "(not supported)")
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newLoginPortalApplicationsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "applications",
		Aliases: []string{"apps"},
		Short:   "Show the per-application portal list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetApplicationPortals(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Applications:\t%d\n", result.Portals.Total)
			if len(result.Portals.Portals) > 0 {
				fmt.Fprintln(writer, "\nAPPLICATION\tID\tHTTPS REDIRECT\tALIAS\tHTTP PORT\tHTTPS PORT")
				for _, portal := range result.Portals.Portals {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
						valueOrDash(portal.DisplayName), portal.AppID, yesNo(portal.RedirectHTTPS),
						valueOrDash(portal.Alias), portOrDash(portal.HTTPPort), portOrDash(portal.HTTPSPort))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newLoginPortalReverseProxyCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "reverse-proxy",
		Aliases: []string{"reverseproxy", "rp"},
		Short:   "Show the reverse-proxy rule list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetReverseProxyRules(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Reverse-proxy rules:\t%d\n", result.Rules.Total)
			if len(result.Rules.Rules) > 0 {
				fmt.Fprintln(writer, "\nDESCRIPTION\tFRONTEND\tBACKEND\tHSTS\tHTTP/2\tCERT\tHEADERS")
				for _, rule := range result.Rules.Rules {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
						valueOrDash(rule.Description), formatEndpoint(rule.Frontend), formatEndpoint(rule.Backend),
						yesNo(rule.HSTSEnabled), yesNo(rule.HTTP2Enabled), yesNo(rule.CertificatePresent), rule.CustomHeaderCount)
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func formatEndpoint(endpoint loginportal.ReverseProxyEndpoint) string {
	host := endpoint.Hostname
	if host == "" {
		host = "*"
	}
	scheme := endpoint.Protocol
	if scheme != "" {
		scheme += "://"
	}
	if endpoint.Port > 0 {
		return fmt.Sprintf("%s%s:%d", scheme, host, endpoint.Port)
	}
	return fmt.Sprintf("%s%s", scheme, host)
}

func portOrDash(port int) string {
	if port <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", port)
}
