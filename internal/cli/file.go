package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/application"
	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
)

// newFileCommand exposes the Synology FileStation module.
func newFileCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "file",
		Aliases: []string{"files", "fs"},
		Short:   "Browse, search, and manage Synology FileStation files",
	}
	command.AddCommand(
		newFileCapabilitiesCommand(opts),
		newFileInfoCommand(opts),
		newFileSharesCommand(opts),
		newFileListCommand(opts),
		newFileStatCommand(opts),
		newFileSearchCommand(opts),
		newFileDirSizeCommand(opts),
		newFileMD5Command(opts),
		newFileVirtualFoldersCommand(opts),
		newFileCheckPermissionCommand(opts),
		newFileGetCommand(opts),
		newFileMkdirCommand(opts),
		newFileRenameCommand(opts),
		newFileCopyCommand(opts),
		newFileMoveCommand(opts),
		newFileDeleteCommand(opts),
		newFileCompressCommand(opts),
		newFileExtractCommand(opts),
		newFilePutCommand(opts),
		newFileFavoriteCommand(opts),
		newFileShareLinkCommand(opts),
		newFileTasksCommand(opts),
		newFilePlanCommand(opts),
		newFileApplyCommand(opts),
	)
	return command
}

func newFileShareLinkCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "share-link",
		Aliases: []string{"sharelink"},
		Short:   "Manage public FileStation sharing links",
	}
	command.AddCommand(newFileShareLinkListCommand(opts), newFileShareLinkCreateCommand(opts), newFileShareLinkDeleteCommand(opts))
	return command
}

func newFileShareLinkListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List public sharing links",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationSharingLinks(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Total:\t%d\n", result.Links.Total)
			if len(result.Links.Links) > 0 {
				fmt.Fprintln(writer, "\nID\tSTATUS\tPWD\tPATH\tURL")
				for _, link := range result.Links.Links {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", valueOrDash(link.ID), valueOrDash(link.Status), yesNo(link.HasPassword), valueOrDash(link.Path), valueOrDash(link.URL))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileShareLinkCreateCommand(opts *options) *cobra.Command {
	var (
		yes         bool
		passwordRef string
		expire      string
	)
	command := &cobra.Command{
		Use:   "create <path>",
		Short: "Create a public sharing link (guarded plan/apply, high risk)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action:    filestation.ActionShareLinkCreate,
				ShareLink: &filestation.ShareLinkChange{Path: args[0], PasswordRef: passwordRef, ExpireDate: expire},
			}, yes)
		},
	}
	command.Flags().StringVar(&passwordRef, "password-ref", "", "env:NAME reference to a link password")
	command.Flags().StringVar(&expire, "expire", "", "expiry date YYYY-MM-DD")
	mutationYesFlag(command, &yes)
	return command
}

func newFileShareLinkDeleteCommand(opts *options) *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "delete <link-id>",
		Short: "Delete a public sharing link (guarded plan/apply)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action:    filestation.ActionShareLinkDelete,
				ShareLink: &filestation.ShareLinkChange{LinkID: args[0]},
			}, yes)
		},
	}
	mutationYesFlag(command, &yes)
	return command
}

func newFileTasksCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "tasks",
		Short: "List in-progress background file-operation tasks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationBackgroundTasks(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Total:\t%d\n", result.Tasks.Total)
			if len(result.Tasks.Tasks) > 0 {
				fmt.Fprintln(writer, "\nTASK ID\tAPI\tFINISHED\tPROCESSING")
				for _, task := range result.Tasks.Tasks {
					fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", valueOrDash(task.TaskID), valueOrDash(task.API), yesNo(task.Finished), valueOrDash(task.ProcessingPath))
				}
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

// runFileMutation plans a FileStation change, prints the plan, and — when
// autoApprove is set — applies it in the same process. Without --yes the plan and
// its approval hash are printed so a caller can review and apply separately.
func runFileMutation(cmd *cobra.Command, opts *options, request filestation.ChangeRequest, autoApprove bool) error {
	service, err := loadService(opts.configPath)
	if err != nil {
		return err
	}
	defer closeService(service)
	plan, err := service.PlanFileStationChange(cmd.Context(), opts.nas, request)
	if err != nil {
		return err
	}
	writeFilePlanSummary(cmd, plan)
	if !autoApprove {
		fmt.Fprintln(cmd.ErrOrStderr(), "\nRe-run with --yes to apply, or pass this plan to 'dsmctl file apply --approve <hash>'.")
		return nil
	}
	result, err := service.ApplyFileStationPlan(cmd.Context(), plan, plan.Hash)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "\nApplied: %s\n", strings.Join(plan.Summary, "; "))
	if len(result.Operation.Paths) > 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Paths: %s\n", strings.Join(result.Operation.Paths, ", "))
	}
	return nil
}

func writeFilePlanSummary(cmd *cobra.Command, plan application.FilePlan) {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", plan.NAS)
	fmt.Fprintf(writer, "Action:\t%s\n", plan.Request.Action)
	fmt.Fprintf(writer, "Risk:\t%s\n", plan.Risk)
	fmt.Fprintf(writer, "Summary:\t%s\n", strings.Join(plan.Summary, "; "))
	if len(plan.Warnings) > 0 {
		fmt.Fprintf(writer, "Warnings:\t%s\n", strings.Join(plan.Warnings, "; "))
	}
	fmt.Fprintf(writer, "Hash:\t%s\n", plan.Hash)
	_ = writer.Flush()
}

func mutationYesFlag(command *cobra.Command, yes *bool) {
	command.Flags().BoolVarP(yes, "yes", "y", false, "apply immediately after planning (the terminal user is the approver)")
}

func newFileMkdirCommand(opts *options) *cobra.Command {
	var (
		yes           bool
		createParents bool
	)
	command := &cobra.Command{
		Use:   "mkdir <parent> <name>",
		Short: "Create a folder (guarded plan/apply)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action:       filestation.ActionCreateFolder,
				CreateFolder: &filestation.CreateFolderChange{Parent: args[0], Name: args[1], CreateParents: createParents},
			}, yes)
		},
	}
	command.Flags().BoolVar(&createParents, "create-parents", false, "create missing intermediate parents")
	mutationYesFlag(command, &yes)
	return command
}

func newFileRenameCommand(opts *options) *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "rename <path> <new-name>",
		Short: "Rename an entry in place (guarded plan/apply)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action: filestation.ActionRename,
				Rename: &filestation.RenameChange{Path: args[0], NewName: args[1]},
			}, yes)
		},
	}
	mutationYesFlag(command, &yes)
	return command
}

func newFileCopyCommand(opts *options) *cobra.Command {
	var (
		yes       bool
		overwrite bool
	)
	command := &cobra.Command{
		Use:     "cp <source> [source...] <dest-folder>",
		Aliases: []string{"copy"},
		Short:   "Copy entries into a destination folder (guarded plan/apply)",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action:   filestation.ActionCopy,
				Transfer: &filestation.TransferChange{Sources: args[:len(args)-1], DestFolder: args[len(args)-1], Overwrite: overwrite},
			}, yes)
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite entries that already exist at the destination")
	mutationYesFlag(command, &yes)
	return command
}

func newFileMoveCommand(opts *options) *cobra.Command {
	var (
		yes       bool
		overwrite bool
	)
	command := &cobra.Command{
		Use:     "mv <source> [source...] <dest-folder>",
		Aliases: []string{"move"},
		Short:   "Move entries into a destination folder (guarded plan/apply, high risk)",
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action:   filestation.ActionMove,
				Transfer: &filestation.TransferChange{Sources: args[:len(args)-1], DestFolder: args[len(args)-1], Overwrite: overwrite},
			}, yes)
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite entries that already exist at the destination")
	mutationYesFlag(command, &yes)
	return command
}

func newFileDeleteCommand(opts *options) *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:     "rm <path> [path...]",
		Aliases: []string{"delete"},
		Short:   "Delete entries permanently (guarded plan/apply, high risk)",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action: filestation.ActionDelete,
				Delete: &filestation.DeleteChange{Paths: args},
			}, yes)
		},
	}
	mutationYesFlag(command, &yes)
	return command
}

func newFileCompressCommand(opts *options) *cobra.Command {
	var (
		yes         bool
		format      string
		level       string
		passwordRef string
	)
	command := &cobra.Command{
		Use:   "compress <dest-archive> <source> [source...]",
		Short: "Compress entries into an archive (guarded plan/apply)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action:   filestation.ActionCompress,
				Compress: &filestation.CompressChange{DestArchive: args[0], Sources: args[1:], Format: format, Level: level, PasswordRef: passwordRef},
			}, yes)
		},
	}
	command.Flags().StringVar(&format, "format", "", "archive format: zip (default) or 7z")
	command.Flags().StringVar(&level, "level", "", "compression level: moderate, fast, best, or store")
	command.Flags().StringVar(&passwordRef, "password-ref", "", "env:NAME reference to an archive password")
	mutationYesFlag(command, &yes)
	return command
}

func newFileExtractCommand(opts *options) *cobra.Command {
	var (
		yes         bool
		overwrite   bool
		passwordRef string
	)
	command := &cobra.Command{
		Use:     "extract <archive> <dest-folder>",
		Aliases: []string{"unzip"},
		Short:   "Extract an archive into a folder (guarded plan/apply)",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action:  filestation.ActionExtract,
				Extract: &filestation.ExtractChange{Archive: args[0], DestFolder: args[1], Overwrite: overwrite, PasswordRef: passwordRef},
			}, yes)
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite existing files in the destination")
	command.Flags().StringVar(&passwordRef, "password-ref", "", "env:NAME reference to the archive password")
	mutationYesFlag(command, &yes)
	return command
}

func newFilePutCommand(opts *options) *cobra.Command {
	var (
		yes           bool
		overwrite     bool
		createParents bool
	)
	command := &cobra.Command{
		Use:     "put <local-file> <dest-folder>",
		Aliases: []string{"upload"},
		Short:   "Upload a local file to the NAS (guarded plan/apply)",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFileMutation(cmd, opts, filestation.ChangeRequest{
				Action: filestation.ActionUpload,
				Upload: &filestation.UploadChange{LocalPath: args[0], DestFolder: args[1], Overwrite: overwrite, CreateParents: createParents},
			}, yes)
		},
	}
	command.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite an existing destination file")
	command.Flags().BoolVar(&createParents, "create-parents", false, "create missing parent folders")
	mutationYesFlag(command, &yes)
	return command
}

func newFilePlanCommand(opts *options) *cobra.Command {
	var inputPath, outputPath string
	command := &cobra.Command{
		Use:   "plan",
		Short: "Validate a FileStation change and emit an approval plan as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var request filestation.ChangeRequest
			if err := decodeJSONInput(cmd, inputPath, &request); err != nil {
				return fmt.Errorf("read FileStation change: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			plan, err := service.PlanFileStationChange(cmd.Context(), opts.nas, request)
			if err != nil {
				return err
			}
			return encodeJSONOutput(cmd, outputPath, plan)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "FileStation change JSON file, or - for stdin")
	command.Flags().StringVarP(&outputPath, "output", "o", "-", "plan JSON file, or - for stdout")
	return command
}

func newFileApplyCommand(opts *options) *cobra.Command {
	var inputPath, approvalHash string
	command := &cobra.Command{
		Use:   "apply",
		Short: "Apply a FileStation plan after hash and stale-state validation",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var plan application.FilePlan
			if err := decodeJSONInput(cmd, inputPath, &plan); err != nil {
				return fmt.Errorf("read FileStation plan: %w", err)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ApplyFileStationPlan(cmd.Context(), plan, approvalHash)
			if err != nil {
				return err
			}
			return encodeIndentedJSON(cmd.OutOrStdout(), result)
		},
	}
	command.Flags().StringVarP(&inputPath, "file", "f", "-", "FileStation plan JSON file, or - for stdin")
	command.Flags().StringVar(&approvalHash, "approve", "", "exact SHA-256 hash printed by FileStation plan")
	_ = command.MarkFlagRequired("approve")
	return command
}

func newFileFavoriteCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "favorite",
		Aliases: []string{"fav"},
		Short:   "Manage personal FileStation favorites",
	}
	command.AddCommand(newFileFavoriteListCommand(opts), newFileFavoriteAddCommand(opts), newFileFavoriteRemoveCommand(opts))
	return command
}

func newFileFavoriteListCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List personal favorites",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationFavorites(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeFileFavorites(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileFavoriteAddCommand(opts *options) *cobra.Command {
	var name string
	command := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a personal favorite (reversible, local to your account)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.AddFileStationFavorite(cmd.Context(), opts.nas, args[0], name)
			if err != nil {
				return err
			}
			return writeFileFavorites(cmd, result)
		},
	}
	command.Flags().StringVar(&name, "name", "", "display name for the favorite (defaults to the folder name)")
	return command
}

func newFileFavoriteRemoveCommand(opts *options) *cobra.Command {
	command := &cobra.Command{
		Use:     "remove <path>",
		Aliases: []string{"rm"},
		Short:   "Remove a personal favorite",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.RemoveFileStationFavorite(cmd.Context(), opts.nas, args[0])
			if err != nil {
				return err
			}
			return writeFileFavorites(cmd, result)
		},
	}
	return command
}

func writeFileFavorites(cmd *cobra.Command, result application.FileStationFavoritesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Total:\t%d\n", result.Favorites.Total)
	if len(result.Favorites.Favorites) > 0 {
		fmt.Fprintln(writer, "\nNAME\tSTATUS\tPATH")
		for _, favorite := range result.Favorites.Favorites {
			fmt.Fprintf(writer, "%s\t%s\t%s\n", valueOrDash(favorite.Name), valueOrDash(favorite.Status), valueOrDash(favorite.Path))
		}
	}
	return writer.Flush()
}

func newFileGetCommand(opts *options) *cobra.Command {
	var output string
	command := &cobra.Command{
		Use:     "get <path>",
		Aliases: []string{"download"},
		Short:   "Download a file from the NAS to local disk",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			remote := args[0]
			local := output
			if local == "" {
				local = filepath.Base(remote)
			}
			if local == "" || local == "." || local == string(filepath.Separator) {
				return fmt.Errorf("cannot determine a local output name for %q; pass --output", remote)
			}
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			// Stream to a sibling .part file, then rename, so an interrupted
			// download never leaves a partial file masquerading as complete.
			part := local + ".part"
			file, err := os.Create(part)
			if err != nil {
				return fmt.Errorf("create %q: %w", part, err)
			}
			result, downloadErr := service.DownloadFileStationFile(cmd.Context(), opts.nas, remote, file)
			closeErr := file.Close()
			if downloadErr != nil {
				_ = os.Remove(part)
				return downloadErr
			}
			if closeErr != nil {
				_ = os.Remove(part)
				return fmt.Errorf("finalize %q: %w", part, closeErr)
			}
			if err := os.Rename(part, local); err != nil {
				_ = os.Remove(part)
				return fmt.Errorf("rename %q to %q: %w", part, local, err)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Downloaded %s (%d bytes) to %s\n", remote, result.Size, local)
			return nil
		},
	}
	command.Flags().StringVarP(&output, "output", "o", "", "local output path (default: base name of the remote path)")
	return command
}

func newFileCapabilitiesCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "capabilities",
		Short: "Show FileStation read support and selected DSM backends",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationCapabilities(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeFileCapabilities(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileInfoCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "info",
		Short: "Show FileStation service information for the current session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationInfo(cmd.Context(), opts.nas)
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Hostname:\t%s\n", valueOrDash(result.Service.Hostname))
			fmt.Fprintf(writer, "Manager:\t%s\n", yesNo(result.Service.IsManager))
			fmt.Fprintf(writer, "Sharing supported:\t%s\n", yesNo(result.Service.SupportSharing))
			protocols := "-"
			if len(result.Service.SupportVirtualProtocols) > 0 {
				protocols = joinStrings(result.Service.SupportVirtualProtocols)
			}
			fmt.Fprintf(writer, "Virtual protocols:\t%s\n", protocols)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileSharesCommand(opts *options) *cobra.Command {
	var (
		jsonOutput bool
		writable   bool
		limit      int
		offset     int
	)
	command := &cobra.Command{
		Use:     "shares",
		Aliases: []string{"list-shares"},
		Short:   "List shared folders visible to the current session",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ListFileStationShares(cmd.Context(), opts.nas, filestation.ListShareQuery{
				OnlyWritable: writable,
				Limit:        limit,
				Offset:       offset,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeFileListing(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().BoolVar(&writable, "writable", false, "list only writable shared folders")
	command.Flags().IntVar(&limit, "limit", 0, "maximum number of shared folders to return")
	command.Flags().IntVar(&offset, "offset", 0, "offset of the first shared folder")
	return command
}

func newFileListCommand(opts *options) *cobra.Command {
	var (
		jsonOutput bool
		limit      int
		offset     int
		sortBy     string
		sortDir    string
		pattern    string
		fileType   string
	)
	command := &cobra.Command{
		Use:     "ls <path>",
		Aliases: []string{"list"},
		Short:   "List the entries of a folder",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ListFileStationDirectory(cmd.Context(), opts.nas, filestation.ListQuery{
				Path:          args[0],
				Limit:         limit,
				Offset:        offset,
				SortBy:        sortBy,
				SortDirection: sortDir,
				Pattern:       pattern,
				FileType:      fileType,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeFileListing(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().IntVar(&limit, "limit", 0, "maximum number of entries to return")
	command.Flags().IntVar(&offset, "offset", 0, "offset of the first entry")
	command.Flags().StringVar(&sortBy, "sort-by", "", "sort key: name, size, mtime, ...")
	command.Flags().StringVar(&sortDir, "sort-direction", "", "sort direction: asc or desc")
	command.Flags().StringVar(&pattern, "pattern", "", "glob pattern entry names must match")
	command.Flags().StringVar(&fileType, "type", "", "restrict to file, dir, or all")
	return command
}

func newFileStatCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "stat <path> [path...]",
		Aliases: []string{"getinfo"},
		Short:   "Show detailed information for one or more entries",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationEntryInfo(cmd.Context(), opts.nas, filestation.GetInfoQuery{Paths: args})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintln(writer, "\nNAME\tTYPE\tSIZE\tMODIFIED\tOWNER\tPATH")
			for _, entry := range result.Info.Entries {
				writeFileEntryRow(writer, entry)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileSearchCommand(opts *options) *cobra.Command {
	var (
		jsonOutput   bool
		pattern      string
		extension    string
		fileType     string
		nonRecursive bool
	)
	command := &cobra.Command{
		Use:   "search <path>",
		Short: "Search a folder subtree for matching entries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.SearchFileStation(cmd.Context(), opts.nas, filestation.SearchQuery{
				Path:      args[0],
				Pattern:   pattern,
				Extension: extension,
				FileType:  fileType,
				Recursive: !nonRecursive,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Total:\t%d\n", result.Result.Total)
			fmt.Fprintf(writer, "Finished:\t%s\n", yesNo(result.Result.Finished))
			fmt.Fprintln(writer, "\nNAME\tTYPE\tSIZE\tMODIFIED\tOWNER\tPATH")
			for _, entry := range result.Result.Entries {
				writeFileEntryRow(writer, entry)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().StringVar(&pattern, "pattern", "", "glob pattern entry names must match")
	command.Flags().StringVar(&extension, "ext", "", "file extension filter without a leading dot")
	command.Flags().StringVar(&fileType, "type", "", "restrict to file, dir, or all")
	command.Flags().BoolVar(&nonRecursive, "no-recursive", false, "do not search subdirectories")
	return command
}

func newFileDirSizeCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "du <path> [path...]",
		Aliases: []string{"dir-size"},
		Short:   "Compute the aggregate size of one or more folders",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationDirSize(cmd.Context(), opts.nas, filestation.DirSizeQuery{Paths: args})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Finished:\t%s\n", yesNo(result.DirSize.Finished))
			fmt.Fprintf(writer, "Directories:\t%d\n", result.DirSize.NumDir)
			fmt.Fprintf(writer, "Files:\t%d\n", result.DirSize.NumFile)
			fmt.Fprintf(writer, "Total size (bytes):\t%d\n", result.DirSize.TotalSize)
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileMD5Command(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:   "md5 <path>",
		Short: "Compute the MD5 digest of a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.GetFileStationMD5(cmd.Context(), opts.nas, filestation.MD5Query{Path: args[0]})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Finished:\t%s\n", yesNo(result.MD5.Finished))
			fmt.Fprintf(writer, "MD5:\t%s\n", valueOrDash(result.MD5.MD5))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileVirtualFoldersCommand(opts *options) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use:     "virtual-folders",
		Aliases: []string{"vfolders"},
		Short:   "List mounted virtual folders",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.ListFileStationVirtualFolders(cmd.Context(), opts.nas, filestation.VirtualFolderQuery{})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			return writeFileListing(cmd, result)
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	return command
}

func newFileCheckPermissionCommand(opts *options) *cobra.Command {
	var (
		jsonOutput    bool
		filename      string
		overwrite     bool
		createParents bool
	)
	command := &cobra.Command{
		Use:     "check-permission <path>",
		Aliases: []string{"check-perm"},
		Short:   "Check whether the current session may write at a path",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := loadService(opts.configPath)
			if err != nil {
				return err
			}
			defer closeService(service)
			result, err := service.CheckFileStationPermission(cmd.Context(), opts.nas, filestation.CheckPermissionQuery{
				Path:          args[0],
				Filename:      filename,
				Overwrite:     overwrite,
				CreateParents: createParents,
			})
			if err != nil {
				return err
			}
			if jsonOutput {
				return encodeIndentedJSON(cmd.OutOrStdout(), result)
			}
			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
			fmt.Fprintf(writer, "Path:\t%s\n", valueOrDash(result.Permission.Path))
			fmt.Fprintf(writer, "Writable:\t%s\n", yesNo(result.Permission.Writable))
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "output structured JSON")
	command.Flags().StringVar(&filename, "filename", "", "probe writing this file name inside the folder")
	command.Flags().BoolVar(&overwrite, "overwrite", false, "probe assuming an existing file is overwritten")
	command.Flags().BoolVar(&createParents, "create-parents", false, "probe assuming missing parents are created")
	return command
}

func writeFileCapabilities(cmd *cobra.Command, result application.FileStationCapabilitiesResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	c := result.Capabilities
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	fmt.Fprintf(writer, "Info read:\t%s\n", yesNo(c.InfoRead))
	fmt.Fprintf(writer, "List read:\t%s\n", yesNo(c.ListRead))
	fmt.Fprintf(writer, "Search read:\t%s\n", yesNo(c.SearchRead))
	fmt.Fprintf(writer, "Directory size read:\t%s\n", yesNo(c.DirSizeRead))
	fmt.Fprintf(writer, "MD5 read:\t%s\n", yesNo(c.MD5Read))
	fmt.Fprintf(writer, "Virtual folder read:\t%s\n", yesNo(c.VirtualFolderRead))
	fmt.Fprintf(writer, "Permission check:\t%s\n", yesNo(c.PermissionCheck))
	fmt.Fprintf(writer, "Download / Upload:\t%s / %s\n", yesNo(c.Download), yesNo(c.Upload))
	fmt.Fprintf(writer, "Create folder / Rename:\t%s / %s\n", yesNo(c.CreateFolder), yesNo(c.Rename))
	fmt.Fprintf(writer, "Copy / Move / Delete:\t%s / %s / %s\n", yesNo(c.Copy), yesNo(c.Move), yesNo(c.Delete))
	fmt.Fprintf(writer, "Compress / Extract:\t%s / %s\n", yesNo(c.Compress), yesNo(c.Extract))
	fmt.Fprintf(writer, "Favorite / Sharing:\t%s / %s\n", yesNo(c.Favorite), yesNo(c.Sharing))
	fmt.Fprintf(writer, "Background task list:\t%s\n", yesNo(c.BackgroundTask))
	fmt.Fprintln(writer, "\nOPERATIONS")
	fmt.Fprintln(writer, "OPERATION\tSUPPORTED\tBACKEND\tAPI\tVERSION")
	for _, operation := range result.Report.Operations {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\tv%d\n", operation.Operation, yesNo(operation.Supported), valueOrDash(operation.Backend), valueOrDash(operation.API), operation.Version)
	}
	return writer.Flush()
}

func writeFileListing(cmd *cobra.Command, result application.FileStationListingResult) error {
	writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(writer, "NAS:\t%s\n", result.NAS)
	if result.Listing.Path != "" {
		fmt.Fprintf(writer, "Path:\t%s\n", result.Listing.Path)
	}
	fmt.Fprintf(writer, "Total:\t%d\n", result.Listing.Total)
	fmt.Fprintln(writer, "\nNAME\tTYPE\tSIZE\tMODIFIED\tOWNER\tPATH")
	if len(result.Listing.Entries) == 0 {
		fmt.Fprintln(writer, "(no entries)")
		return writer.Flush()
	}
	for _, entry := range result.Listing.Entries {
		writeFileEntryRow(writer, entry)
	}
	return writer.Flush()
}

func writeFileEntryRow(writer *tabwriter.Writer, entry filestation.Entry) {
	entryType := "file"
	if entry.IsDir {
		entryType = "dir"
	}
	size := "-"
	if !entry.IsDir {
		size = fmt.Sprintf("%d", entry.Size)
	}
	modified := "-"
	if entry.Time != nil && entry.Time.Modified > 0 {
		modified = time.Unix(entry.Time.Modified, 0).Format("2006-01-02 15:04")
	}
	owner := "-"
	if entry.Owner != nil && entry.Owner.User != "" {
		owner = entry.Owner.User
	}
	fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", valueOrDash(entry.Name), entryType, size, modified, owner, valueOrDash(entry.Path))
}

func joinStrings(values []string) string {
	out := ""
	for index, value := range values {
		if index > 0 {
			out += ", "
		}
		out += value
	}
	return out
}
