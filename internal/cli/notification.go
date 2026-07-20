package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/notification"
	"github.com/ychiu1211/dsmctl/internal/domain/syslog"
)

func newNotificationCommand(opts *options) *cobra.Command {
	command := &cobra.Command{Use: "notification", Short: "Inspect DSM notification settings and history"}
	command.AddCommand(
		newNotificationCapabilitiesCommand(opts),
		newNotificationMailCommand(opts),
		newNotificationPushCommand(opts),
		newNotificationWebhookCommand(opts),
		newNotificationSMSCommand(opts),
		newNotificationRulesCommand(opts),
		newNotificationDesktopCommand(opts),
		newNotificationHistoryCommand(opts),
	)
	return command
}

func newNotificationCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show which notification areas can be read and the selected backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Mail read:\t%s\n", yesNo(result.Capabilities.Mail))
			fmt.Fprintf(writer, "Push read:\t%s\n", yesNo(result.Capabilities.Push))
			fmt.Fprintf(writer, "Webhook read:\t%s\n", yesNo(result.Capabilities.Webhook))
			fmt.Fprintf(writer, "SMS read:\t%s\n", yesNo(result.Capabilities.SMS))
			fmt.Fprintf(writer, "Rules read:\t%s\n", yesNo(result.Capabilities.Rules))
			fmt.Fprintf(writer, "Desktop read:\t%s\n", yesNo(result.Capabilities.Desktop))
			fmt.Fprintf(writer, "History read:\t%s\n", yesNo(result.Capabilities.History))
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

func newNotificationMailCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "mail",
		Short: "Show the email notification channel (SMTP and Synology relay)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationMail(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			mail := result.Mail
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "SMTP email enabled:\t%s\n", yesNo(mail.Enabled))
			fmt.Fprintf(writer, "SMTP server:\t%s\n", valueOrDash(mail.SMTP.Server))
			if mail.SMTP.Port != 0 {
				fmt.Fprintf(writer, "SMTP port:\t%d\n", mail.SMTP.Port)
			}
			fmt.Fprintf(writer, "SMTP SSL/TLS:\t%s\n", yesNo(mail.SMTP.SSL))
			fmt.Fprintf(writer, "Verify certificate:\t%s\n", yesNo(mail.SMTP.VerifyCert))
			fmt.Fprintf(writer, "SMTP auth:\t%s\n", yesNo(mail.SMTP.AuthEnabled))
			fmt.Fprintf(writer, "SMTP auth user:\t%s\n", valueOrDash(mail.SMTP.AuthUser))
			fmt.Fprintf(writer, "OAuth:\t%s\n", yesNo(mail.OAuthEnabled))
			fmt.Fprintf(writer, "Sender:\t%s\n", valueOrDash(strings.TrimSpace(mail.SenderName+" "+mail.SenderMail)))
			fmt.Fprintf(writer, "Subject prefix:\t%s\n", valueOrDash(mail.SubjectPrefix))
			fmt.Fprintf(writer, "Welcome mail:\t%s\n", yesNo(mail.WelcomeMailEnabled))
			fmt.Fprintf(writer, "Recipients:\t%s\n", valueOrDash(formatMailRecipients(mail.Recipients)))
			if mail.Relay != nil {
				fmt.Fprintf(writer, "Synology relay email:\t%s\n", yesNo(mail.Relay.Enabled))
				fmt.Fprintf(writer, "Relay recipients:\t%s\n", valueOrDash(formatMailRecipients(mail.Relay.Recipients)))
			} else {
				fmt.Fprintf(writer, "Synology relay email:\t(not supported)\n")
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func formatMailRecipients(recipients []notification.MailRecipient) string {
	parts := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		part := recipient.Address
		if part == "" {
			part = recipient.Name
		} else if recipient.Name != "" {
			part = fmt.Sprintf("%s <%s>", recipient.Name, recipient.Address)
		}
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

func newNotificationPushCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "push",
		Short: "Show the push notification channel and paired devices",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationPush(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Mobile push enabled:\t%s\n", yesNo(result.Push.MobileEnabled))
			fmt.Fprintf(writer, "Paired targets:\t%d\n", len(result.Push.Devices))
			if len(result.Push.Devices) > 0 {
				fmt.Fprintln(writer, "\nKIND\tNAME\tMODEL\tTARGET ID\tLAST SEEN")
				for _, device := range result.Push.Devices {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
						valueOrDash(device.Kind), valueOrDash(device.Name), valueOrDash(device.Model),
						valueOrDash(device.TargetID), valueOrDash(device.LastSeen))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNotificationWebhookCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "webhook",
		Short: "Show configured webhook notification providers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationWebhook(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Webhook providers:\t%d\n", len(result.Webhook.Providers))
			if len(result.Webhook.Providers) > 0 {
				fmt.Fprintln(writer, "\nID\tNAME\tKIND\tENABLED")
				for _, provider := range result.Webhook.Providers {
					enabled := "-"
					if provider.Enabled != nil {
						enabled = yesNo(*provider.Enabled)
					}
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
						valueOrDash(provider.ProfileID), valueOrDash(provider.Name), valueOrDash(provider.Kind), enabled)
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNotificationSMSCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "sms",
		Short: "Show the SMS notification channel",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationSMS(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "SMS enabled:\t%s\n", yesNo(result.SMS.Enabled))
			fmt.Fprintf(writer, "Provider:\t%s\n", valueOrDash(result.SMS.Provider))
			phones := make([]string, 0, len(result.SMS.Phones))
			for _, phone := range result.SMS.Phones {
				number := strings.TrimSpace(strings.Join([]string{phone.CountryCode, phone.Number}, " "))
				if number != "" {
					phones = append(phones, number)
				}
			}
			fmt.Fprintf(writer, "Phones:\t%s\n", valueOrDash(strings.Join(phones, ", ")))
			fmt.Fprintf(writer, "Minimum interval:\t%d minutes\n", result.SMS.IntervalMinutes)
			if result.SMS.Providers != nil {
				names := make([]string, 0, len(result.SMS.Providers))
				for _, provider := range result.SMS.Providers {
					names = append(names, provider.Name)
				}
				fmt.Fprintf(writer, "Available providers:\t%s\n", valueOrDash(strings.Join(names, ", ")))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNotificationRulesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var group string
	command := &cobra.Command{
		Use:   "rules",
		Short: "Show the notification event rule catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationRules(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			for _, profile := range result.Rules.Profiles {
				fmt.Fprintf(writer, "Profile:\t%s (%d events)\n", profile.Name, len(profile.Events))
				fmt.Fprintln(writer, "\nGROUP\tLEVEL\tEVENT\tTITLE")
				for _, event := range profile.Events {
					if group != "" && !strings.EqualFold(event.Group, group) {
						continue
					}
					name := event.Name
					if name == "" {
						name = event.Tag
					}
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
						valueOrDash(event.Group), valueOrDash(event.Level), valueOrDash(name), valueOrDash(event.Title))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().StringVar(&group, "group", "", "only show events in this group, such as Storage or System")
	return command
}

func newNotificationDesktopCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "desktop",
		Short: "Show per-category DSM desktop notification toggles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationDesktop(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintln(writer, "\nCATEGORY\tDESKTOP NOTIFICATIONS")
			for _, category := range result.Desktop.Categories {
				fmt.Fprintf(writer, "%s\t%s\n", valueOrDash(category.Category), yesNo(category.Enabled))
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newNotificationHistoryCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	var limit, offset int
	var level, from, to, lang string
	command := &cobra.Command{
		Use:   "history",
		Short: "List delivered DSM notifications (the desktop bell feed), newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fromTime, err := syslog.ParseTime(from)
			if err != nil {
				return err
			}
			toTime, err := syslog.ParseTime(to)
			if err != nil {
				return err
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetNotificationHistory(cmd.Context(), opts.nas, notification.HistoryQuery{
				Limit: limit, Offset: offset, Level: level,
				From: fromTime, To: toTime, Lang: lang,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeNotificationHistory(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().IntVar(&limit, "limit", 30, "maximum number of notifications to return")
	command.Flags().IntVar(&offset, "offset", 0, "number of newest notifications to skip for pagination")
	command.Flags().StringVar(&level, "level", "", "severity filter applied by DSM: info, warn, or error")
	command.Flags().StringVar(&from, "from", "", "only notifications at or after this local time (2006-01-02[ 15:04:05]) or Unix seconds")
	command.Flags().StringVar(&to, "to", "", "only notifications at or before this local time (2006-01-02[ 15:04:05]) or Unix seconds")
	command.Flags().StringVar(&lang, "lang", "", "DSM string-table language for rendered text, such as enu (default) or cht")
	return command
}

func writeNotificationHistory(cmd *cobra.Command, result application.NotificationHistoryResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	history := result.History
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total matching:\t%d\n", history.Total)
	fmt.Fprintf(writer, "Showing:\t%d\n", len(history.Entries))
	fmt.Fprintln(writer, "\nTIME\tLEVEL\tTITLE\tMESSAGE")
	for _, entry := range history.Entries {
		message := strings.ReplaceAll(entry.Message, "\n", " ")
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
			valueOrDash(entry.Time), valueOrDash(entry.Level), valueOrDash(entry.Title), valueOrDash(message))
	}
	return writer.Flush()
}
