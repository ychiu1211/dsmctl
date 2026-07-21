package cli

import (
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newDirectoryCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "directory",
		Short:   "Inspect Domain/LDAP directory-client status and synced users",
		Aliases: []string{"domain", "ldap"},
	}
	command.AddCommand(
		newDirectoryCapabilitiesCommand(opts),
		newDirectoryStatusCommand(opts),
		newDirectoryUsersCommand(opts),
		newDirectoryGroupsCommand(opts),
	)
	return command
}

func newDirectoryCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which directory areas can be read and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDirectoryCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "AD domain read:\t%s\n", yesNo(result.Capabilities.Domain))
			fmt.Fprintf(writer, "LDAP client read:\t%s\n", yesNo(result.Capabilities.LDAP))
			fmt.Fprintf(writer, "Synced users read:\t%s\n", yesNo(result.Capabilities.Users))
			fmt.Fprintf(writer, "Synced groups read:\t%s\n", yesNo(result.Capabilities.Groups))
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

func newDirectoryStatusCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show AD domain membership and/or LDAP bind status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDirectoryStatus(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Mode:\t%s\n", result.Status.Mode)

			if domain := result.Status.Domain; domain != nil {
				fmt.Fprintln(writer, "\nActive Directory:")
				fmt.Fprintf(writer, "  Joined:\t%s\n", yesNo(domain.Joined))
				if domain.Joined {
					fmt.Fprintf(writer, "  Domain:\t%s\n", valueOrDash(domain.DomainFQDN))
					fmt.Fprintf(writer, "  Workgroup:\t%s\n", valueOrDash(domain.Workgroup))
					fmt.Fprintf(writer, "  DNS server:\t%s\n", valueOrDash(domain.DNSServer))
					fmt.Fprintf(writer, "  Domain controller:\t%s\n", valueOrDash(domain.DomainController))
					fmt.Fprintf(writer, "  Organizational unit:\t%s\n", valueOrDash(domain.OrganizationalUnit))
					fmt.Fprintf(writer, "  Connection:\t%s\n", valueOrDash(domain.ConnectionStatus))
				}
				fmt.Fprintf(writer, "  Deny domain admins:\t%s\n", yesNo(domain.Options.DisableDomainAdmins))
				if schedule := domain.Schedule; schedule != nil {
					fmt.Fprintf(writer, "  Sync schedule:\t%s\n", yesNo(schedule.Enabled))
				}
			} else {
				fmt.Fprintln(writer, "\nActive Directory:\t(not supported)")
			}

			if ldap := result.Status.LDAP; ldap != nil {
				fmt.Fprintln(writer, "\nLDAP client:")
				fmt.Fprintf(writer, "  Bound:\t%s\n", yesNo(ldap.Bound))
				fmt.Fprintf(writer, "  Server:\t%s\n", valueOrDash(ldap.ServerAddress))
				fmt.Fprintf(writer, "  Base DN:\t%s\n", valueOrDash(ldap.BaseDN))
				fmt.Fprintf(writer, "  Bind DN:\t%s\n", valueOrDash(ldap.BindDN))
				fmt.Fprintf(writer, "  Encryption:\t%s\n", valueOrDash(ldap.Encryption))
				fmt.Fprintf(writer, "  Profile:\t%s\n", valueOrDash(ldap.Profile))
			} else {
				fmt.Fprintln(writer, "\nLDAP client:\t(not supported)")
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDirectoryUsersCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "users",
		Short: "List synced domain/LDAP users (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDirectoryUsers(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Mode:\t%s\n", result.Users.Mode)
			fmt.Fprintf(writer, "Users:\t%d\n", len(result.Users.Users))
			if len(result.Users.Users) > 0 {
				fmt.Fprintln(writer, "\nNAME\tUID\tSOURCE\tDESCRIPTION")
				for _, user := range result.Users.Users {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
						valueOrDash(user.Name), formatIntPtr(user.UID), valueOrDash(string(user.Source)), valueOrDash(user.Description))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newDirectoryGroupsCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "groups",
		Short: "List synced domain/LDAP groups (read-only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetDirectoryGroups(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Mode:\t%s\n", result.Groups.Mode)
			fmt.Fprintf(writer, "Groups:\t%d\n", len(result.Groups.Groups))
			if len(result.Groups.Groups) > 0 {
				fmt.Fprintln(writer, "\nNAME\tGID\tSOURCE\tDESCRIPTION")
				for _, group := range result.Groups.Groups {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
						valueOrDash(group.Name), formatIntPtr(group.GID), valueOrDash(string(group.Source)), valueOrDash(group.Description))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func formatIntPtr(value *int) string {
	if value == nil {
		return "-"
	}
	return strconv.Itoa(*value)
}
