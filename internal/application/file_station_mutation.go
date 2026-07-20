package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/filestation"
	"github.com/ychiu1211/dsmctl/internal/runtime"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const fileStationAPIVersion = "dsmctl.io/v1alpha1"

// FilePlan is a validated, hash-bound FileStation mutation plan. Its
// precondition holds slices, so apply compares the precondition Fingerprint plus
// the plan Hash rather than the struct itself.
type FilePlan struct {
	APIVersion      string                       `json:"api_version" jsonschema:"Plan schema version"`
	NAS             string                       `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision uint64                       `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request         filestation.ChangeRequest    `json:"request" jsonschema:"Validated FileStation mutation intent"`
	Precondition    filestation.FilePrecondition `json:"precondition" jsonschema:"Observed path state that must still match during apply"`
	Destructive     bool                         `json:"destructive" jsonschema:"Whether the plan removes or overwrites existing data"`
	Risk            string                       `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings        []string                     `json:"warnings" jsonschema:"Data-loss and exposure warnings"`
	Summary         []string                     `json:"summary" jsonschema:"Human-readable actions the plan will perform"`
	Hash            string                       `json:"hash" jsonschema:"SHA-256 approval hash covering intent and observed state"`
}

type FileApplyResult struct {
	NAS       string                             `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash  string                             `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied   bool                               `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Operation synology.FileStationMutationResult `json:"operation" jsonschema:"Normalized DSM mutation result (task id and affected paths)"`
}

func (s *Service) PlanFileStationChange(ctx context.Context, requestedNAS string, request filestation.ChangeRequest) (FilePlan, error) {
	if err := validateFileChange(request); err != nil {
		return FilePlan{}, err
	}
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FilePlan{}, err
	}
	plan, err := planFileChangeWithClient(ctx, name, client, request)
	if err != nil {
		return FilePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err != nil {
		return FilePlan{}, err
	}
	plan.Hash, err = fileStationPlanHash(plan)
	return plan, err
}

func (s *Service) ApplyFileStationPlan(ctx context.Context, plan FilePlan, approvalHash string) (FileApplyResult, error) {
	if err := validateFilePlan(plan, approvalHash); err != nil {
		return FileApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return FileApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return FileApplyResult{}, err
	}
	name, client, err := s.manager.Client(ctx, plan.NAS)
	if err != nil {
		return FileApplyResult{}, err
	}
	if name != plan.NAS {
		return FileApplyResult{}, fmt.Errorf("FileStation plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	current, err := planFileChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return FileApplyResult{}, fmt.Errorf("FileStation plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = fileStationPlanHash(current)
	if err != nil {
		return FileApplyResult{}, err
	}
	if current.Precondition.Fingerprint != plan.Precondition.Fingerprint || current.Hash != plan.Hash {
		return FileApplyResult{}, fmt.Errorf("FileStation plan is stale; create a new plan")
	}
	password := ""
	if ref := passwordRefFor(plan.Request); ref != "" {
		password, err = s.secretReferences.ResolveSecret(ctx, ref)
		if err != nil {
			return FileApplyResult{}, fmt.Errorf("resolve archive password reference: %w", err)
		}
	}
	var operation synology.FileStationMutationResult
	if plan.Request.Action == filestation.ActionUpload {
		operation, err = applyUpload(ctx, client, plan.Request.Upload)
	} else {
		operation, err = client.ApplyFileStationChange(ctx, plan.Request, password)
	}
	if err != nil {
		return FileApplyResult{}, authenticationError(plan.NAS, err)
	}
	if err := verifyFilePostcondition(ctx, client, plan.Request); err != nil {
		return FileApplyResult{}, err
	}
	return FileApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Operation: operation}, nil
}

func planFileChangeWithClient(ctx context.Context, nas string, client runtime.Client, request filestation.ChangeRequest) (FilePlan, error) {
	capabilities, _, err := client.FileStationCapabilities(ctx)
	if err != nil {
		return FilePlan{}, authenticationError(nas, err)
	}
	if !actionSupported(capabilities, request.Action) {
		return FilePlan{}, fmt.Errorf("NAS %q does not expose a verified FileStation backend for %q", nas, request.Action)
	}
	precondition, destructive, warnings, summary, err := buildFilePrecondition(ctx, client, request)
	if err != nil {
		return FilePlan{}, err
	}
	plan := FilePlan{
		APIVersion:   fileStationAPIVersion,
		NAS:          nas,
		Request:      request,
		Precondition: precondition,
		Destructive:  destructive,
		Risk:         riskLevel(destructive),
		Warnings:     warnings,
		Summary:      summary,
	}
	plan.Hash, err = fileStationPlanHash(plan)
	if err != nil {
		return FilePlan{}, err
	}
	return plan, nil
}

// observePath records the observed state of one NAS path. A path that DSM does
// not return is recorded as absent; a session failure is propagated.
func observePath(ctx context.Context, client runtime.Client, target string) (filestation.PathObservation, error) {
	info, err := client.FileStationGetInfo(ctx, filestation.GetInfoQuery{Paths: []string{target}})
	if err != nil {
		if synology.IsSessionExpired(err) {
			return filestation.PathObservation{}, err
		}
		return filestation.PathObservation{Path: target, Exists: false}, nil
	}
	for _, entry := range info.Entries {
		// DSM getinfo returns a stub entry with an empty name (and no metadata)
		// for a path that does not exist; a real entry always has its base name.
		if entry.Path == target && entry.Name != "" {
			observation := filestation.PathObservation{Path: target, Exists: true, IsDir: entry.IsDir, Size: entry.Size}
			if entry.Time != nil {
				observation.Modified = entry.Time.Modified
			}
			return observation, nil
		}
	}
	return filestation.PathObservation{Path: target, Exists: false}, nil
}

// buildFilePrecondition observes the paths one action touches, validates the
// preconditions that DSM would otherwise fail late, and returns the fingerprinted
// precondition plus the plan's destructiveness, warnings, and summary.
func buildFilePrecondition(ctx context.Context, client runtime.Client, request filestation.ChangeRequest) (filestation.FilePrecondition, bool, []string, []string, error) {
	var (
		targets     []filestation.PathObservation
		destination *filestation.PathObservation
		warnings    []string
		summary     []string
		destructive bool
	)

	switch request.Action {
	case filestation.ActionCreateFolder:
		change := request.CreateFolder
		parent, err := observePath(ctx, client, change.Parent)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !change.CreateParents && (!parent.Exists || !parent.IsDir) {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("parent folder %q does not exist; set create_parents to create it", change.Parent)
		}
		targetPath := path.Join(change.Parent, change.Name)
		observed, err := observePath(ctx, client, targetPath)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if observed.Exists {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("path %q already exists", targetPath)
		}
		targets = []filestation.PathObservation{parent}
		destination = &observed
		summary = append(summary, fmt.Sprintf("create folder %q", targetPath))

	case filestation.ActionRename:
		change := request.Rename
		source, err := observePath(ctx, client, change.Path)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !source.Exists {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("path %q does not exist", change.Path)
		}
		newPath := path.Join(path.Dir(change.Path), change.NewName)
		observed, err := observePath(ctx, client, newPath)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if observed.Exists {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("path %q already exists", newPath)
		}
		targets = []filestation.PathObservation{source}
		destination = &observed
		summary = append(summary, fmt.Sprintf("rename %q to %q", change.Path, change.NewName))

	case filestation.ActionCopy, filestation.ActionMove:
		change := request.Transfer
		for _, source := range change.Sources {
			observed, err := observePath(ctx, client, source)
			if err != nil {
				return filestation.FilePrecondition{}, false, nil, nil, err
			}
			if !observed.Exists {
				return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("source %q does not exist", source)
			}
			targets = append(targets, observed)
			destChild := path.Join(change.DestFolder, path.Base(source))
			destObserved, err := observePath(ctx, client, destChild)
			if err != nil {
				return filestation.FilePrecondition{}, false, nil, nil, err
			}
			targets = append(targets, destObserved)
			if destObserved.Exists {
				if !change.Overwrite {
					return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("destination %q already exists; set overwrite to replace it", destChild)
				}
				destructive = true
				warnings = append(warnings, fmt.Sprintf("overwrites existing %q", destChild))
			}
		}
		destFolder, err := observePath(ctx, client, change.DestFolder)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !destFolder.Exists || !destFolder.IsDir {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("destination folder %q does not exist", change.DestFolder)
		}
		destination = &destFolder
		verb := "copy"
		if request.Action == filestation.ActionMove {
			verb = "move"
			destructive = true
			warnings = append(warnings, "moving removes the source entries")
		}
		summary = append(summary, fmt.Sprintf("%s %d item(s) into %q", verb, len(change.Sources), change.DestFolder))

	case filestation.ActionDelete:
		change := request.Delete
		for _, target := range change.Paths {
			observed, err := observePath(ctx, client, target)
			if err != nil {
				return filestation.FilePrecondition{}, false, nil, nil, err
			}
			if !observed.Exists {
				return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("path %q does not exist", target)
			}
			targets = append(targets, observed)
		}
		destructive = true
		warnings = append(warnings, "deletion is permanent and recursive; it does not use the recycle bin")
		summary = append(summary, fmt.Sprintf("delete %d path(s)", len(change.Paths)))

	case filestation.ActionCompress:
		change := request.Compress
		for _, source := range change.Sources {
			observed, err := observePath(ctx, client, source)
			if err != nil {
				return filestation.FilePrecondition{}, false, nil, nil, err
			}
			if !observed.Exists {
				return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("source %q does not exist", source)
			}
			targets = append(targets, observed)
		}
		archive, err := observePath(ctx, client, change.DestArchive)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		destination = &archive
		if archive.Exists {
			destructive = true
			warnings = append(warnings, fmt.Sprintf("overwrites existing archive %q", change.DestArchive))
		}
		summary = append(summary, fmt.Sprintf("compress %d item(s) into %q", len(change.Sources), change.DestArchive))

	case filestation.ActionExtract:
		change := request.Extract
		archive, err := observePath(ctx, client, change.Archive)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !archive.Exists {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("archive %q does not exist", change.Archive)
		}
		destFolder, err := observePath(ctx, client, change.DestFolder)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !destFolder.Exists || !destFolder.IsDir {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("destination folder %q does not exist", change.DestFolder)
		}
		targets = []filestation.PathObservation{archive}
		destination = &destFolder
		if change.Overwrite {
			destructive = true
			warnings = append(warnings, "extraction may overwrite existing files in the destination")
		}
		summary = append(summary, fmt.Sprintf("extract %q into %q", change.Archive, change.DestFolder))

	case filestation.ActionUpload:
		change := request.Upload
		size, hash, err := hashLocalFile(change.LocalPath)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("read local file %q: %w", change.LocalPath, err)
		}
		destFolder, err := observePath(ctx, client, change.DestFolder)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !change.CreateParents && (!destFolder.Exists || !destFolder.IsDir) {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("destination folder %q does not exist; set create_parents to create it", change.DestFolder)
		}
		destFile := path.Join(change.DestFolder, filepath.Base(change.LocalPath))
		destObserved, err := observePath(ctx, client, destFile)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if destObserved.Exists {
			if !change.Overwrite {
				return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("destination %q already exists; set overwrite to replace it", destFile)
			}
			destructive = true
			warnings = append(warnings, fmt.Sprintf("overwrites existing %q", destFile))
		}
		targets = []filestation.PathObservation{
			{Path: change.LocalPath, Exists: true, Size: size, ContentHash: hash},
			destFolder,
		}
		destination = &destObserved
		summary = append(summary, fmt.Sprintf("upload %q (%d bytes) to %q", change.LocalPath, size, destFile))

	case filestation.ActionShareLinkCreate:
		change := request.ShareLink
		target, err := observePath(ctx, client, change.Path)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !target.Exists {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("path %q does not exist", change.Path)
		}
		targets = []filestation.PathObservation{target}
		destructive = true // public exposure is treated as high risk
		warnings = append(warnings, fmt.Sprintf("creates an anonymous public URL that exposes %q to anyone with the link", change.Path))
		summary = append(summary, fmt.Sprintf("create a public sharing link for %q", change.Path))

	case filestation.ActionShareLinkDelete:
		change := request.ShareLink
		observed, err := observeSharingLink(ctx, client, change.LinkID)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !observed.Exists {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("sharing link %q does not exist", change.LinkID)
		}
		targets = []filestation.PathObservation{observed}
		summary = append(summary, fmt.Sprintf("delete sharing link %q", change.LinkID))

	case filestation.ActionShareLinkEdit:
		change := request.ShareLink
		observed, err := observeSharingLink(ctx, client, change.LinkID)
		if err != nil {
			return filestation.FilePrecondition{}, false, nil, nil, err
		}
		if !observed.Exists {
			return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("sharing link %q does not exist", change.LinkID)
		}
		targets = []filestation.PathObservation{observed}
		if change.ExpireDate != "" {
			summary = append(summary, fmt.Sprintf("set sharing link %q expiry to %s", change.LinkID, change.ExpireDate))
		}
		if change.PasswordRef != "" {
			summary = append(summary, fmt.Sprintf("change the password of sharing link %q (resolved from the credential reference at apply)", change.LinkID))
		}

	case filestation.ActionShareLinkClearInvalid:
		// A global cleanup of expired/broken links; it binds to no specific
		// path, so the precondition carries no targets.
		summary = append(summary, "remove every expired or invalid sharing link")

	default:
		return filestation.FilePrecondition{}, false, nil, nil, fmt.Errorf("unsupported FileStation action %q", request.Action)
	}

	precondition := filestation.FilePrecondition{Targets: targets, Destination: destination}
	precondition.ResourceID = fileResourceID(targets, destination)
	precondition.Fingerprint = fingerprint(struct {
		Targets     []filestation.PathObservation `json:"targets"`
		Destination *filestation.PathObservation  `json:"destination"`
	}{Targets: targets, Destination: destination})
	if warnings == nil {
		warnings = []string{}
	}
	return precondition, destructive, warnings, summary, nil
}

func verifyFilePostcondition(ctx context.Context, client runtime.Client, request filestation.ChangeRequest) error {
	exists := func(target string) (bool, error) {
		observed, err := observePath(ctx, client, target)
		if err != nil {
			return false, fmt.Errorf("verify %q: %w", target, err)
		}
		return observed.Exists, nil
	}
	switch request.Action {
	case filestation.ActionCreateFolder:
		created := path.Join(request.CreateFolder.Parent, request.CreateFolder.Name)
		if ok, err := exists(created); err != nil || !ok {
			return orMismatch(err, "created folder %q was not found after apply", created)
		}
	case filestation.ActionRename:
		newPath := path.Join(path.Dir(request.Rename.Path), request.Rename.NewName)
		if ok, err := exists(newPath); err != nil || !ok {
			return orMismatch(err, "renamed entry %q was not found after apply", newPath)
		}
	case filestation.ActionCopy, filestation.ActionMove:
		for _, source := range request.Transfer.Sources {
			destChild := path.Join(request.Transfer.DestFolder, path.Base(source))
			if ok, err := exists(destChild); err != nil || !ok {
				return orMismatch(err, "destination %q was not found after apply", destChild)
			}
		}
		if request.Action == filestation.ActionMove {
			for _, source := range request.Transfer.Sources {
				if ok, err := exists(source); err != nil || ok {
					return orMismatch(err, "source %q still exists after move", source)
				}
			}
		}
	case filestation.ActionDelete:
		for _, target := range request.Delete.Paths {
			if ok, err := exists(target); err != nil || ok {
				return orMismatch(err, "path %q still exists after delete", target)
			}
		}
	case filestation.ActionCompress:
		if ok, err := exists(request.Compress.DestArchive); err != nil || !ok {
			return orMismatch(err, "archive %q was not found after apply", request.Compress.DestArchive)
		}
	case filestation.ActionExtract:
		if ok, err := exists(request.Extract.DestFolder); err != nil || !ok {
			return orMismatch(err, "destination folder %q was not found after extract", request.Extract.DestFolder)
		}
	case filestation.ActionUpload:
		destFile := path.Join(request.Upload.DestFolder, filepath.Base(request.Upload.LocalPath))
		if ok, err := exists(destFile); err != nil || !ok {
			return orMismatch(err, "uploaded file %q was not found after apply", destFile)
		}
	case filestation.ActionShareLinkCreate:
		links, err := client.FileStationSharingList(ctx)
		if err != nil {
			return fmt.Errorf("verify sharing link: %w", err)
		}
		for _, link := range links.Links {
			if link.Path == request.ShareLink.Path {
				return nil
			}
		}
		return fmt.Errorf("no sharing link for %q was found after apply", request.ShareLink.Path)
	case filestation.ActionShareLinkDelete:
		observed, err := observeSharingLink(ctx, client, request.ShareLink.LinkID)
		if err != nil {
			return err
		}
		if observed.Exists {
			return fmt.Errorf("sharing link %q still exists after delete", request.ShareLink.LinkID)
		}
	case filestation.ActionShareLinkEdit:
		links, err := client.FileStationSharingList(ctx)
		if err != nil {
			return fmt.Errorf("verify sharing link edit: %w", err)
		}
		for _, link := range links.Links {
			if link.ID != request.ShareLink.LinkID {
				continue
			}
			if want := request.ShareLink.ExpireDate; want != "" && !strings.HasPrefix(link.DateExpired, want) {
				return fmt.Errorf("sharing link %q expiry is %q, want %s", link.ID, link.DateExpired, want)
			}
			return nil
		}
		return fmt.Errorf("sharing link %q was not found after edit", request.ShareLink.LinkID)
	case filestation.ActionShareLinkClearInvalid:
		// The cleanup has no per-link postcondition beyond a successful re-read.
		if _, err := client.FileStationSharingList(ctx); err != nil {
			return fmt.Errorf("verify sharing cleanup: %w", err)
		}
	}
	return nil
}

// observeSharingLink records whether a sharing link id currently exists.
func observeSharingLink(ctx context.Context, client runtime.Client, linkID string) (filestation.PathObservation, error) {
	links, err := client.FileStationSharingList(ctx)
	if err != nil {
		if synology.IsSessionExpired(err) {
			return filestation.PathObservation{}, err
		}
		return filestation.PathObservation{Path: linkID, Exists: false}, nil
	}
	for _, link := range links.Links {
		if link.ID == linkID {
			return filestation.PathObservation{Path: linkID, Exists: true, IsDir: link.IsFolder}, nil
		}
	}
	return filestation.PathObservation{Path: linkID, Exists: false}, nil
}

func orMismatch(err error, format string, args ...any) error {
	if err != nil {
		return err
	}
	return fmt.Errorf(format, args...)
}

func applyUpload(ctx context.Context, client runtime.Client, change *filestation.UploadChange) (synology.FileStationMutationResult, error) {
	file, err := os.Open(change.LocalPath)
	if err != nil {
		return synology.FileStationMutationResult{}, fmt.Errorf("open local file %q: %w", change.LocalPath, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return synology.FileStationMutationResult{}, fmt.Errorf("stat local file %q: %w", change.LocalPath, err)
	}
	name := filepath.Base(change.LocalPath)
	if _, err := client.UploadFile(ctx, change.DestFolder, name, file, info.Size(), synology.UploadOptions{
		Overwrite:     change.Overwrite,
		CreateParents: change.CreateParents,
	}); err != nil {
		return synology.FileStationMutationResult{}, err
	}
	return synology.FileStationMutationResult{Paths: []string{path.Join(change.DestFolder, name)}}, nil
}

func hashLocalFile(localPath string) (int64, string, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(hasher.Sum(nil)), nil
}

func fileResourceID(targets []filestation.PathObservation, destination *filestation.PathObservation) string {
	paths := make([]string, 0, len(targets)+1)
	for _, target := range targets {
		paths = append(paths, target.Path)
	}
	if destination != nil {
		paths = append(paths, destination.Path)
	}
	sort.Strings(paths)
	return strings.Join(paths, "\n")
}

func actionSupported(capabilities synology.FileStationCapabilities, action string) bool {
	switch action {
	case filestation.ActionCreateFolder:
		return capabilities.CreateFolder
	case filestation.ActionRename:
		return capabilities.Rename
	case filestation.ActionCopy:
		return capabilities.Copy
	case filestation.ActionMove:
		return capabilities.Move
	case filestation.ActionDelete:
		return capabilities.Delete
	case filestation.ActionCompress:
		return capabilities.Compress
	case filestation.ActionExtract:
		return capabilities.Extract
	case filestation.ActionUpload:
		return capabilities.Upload
	case filestation.ActionShareLinkCreate, filestation.ActionShareLinkDelete,
		filestation.ActionShareLinkEdit, filestation.ActionShareLinkClearInvalid:
		return capabilities.Sharing
	default:
		return false
	}
}

func passwordRefFor(request filestation.ChangeRequest) string {
	switch request.Action {
	case filestation.ActionCompress:
		if request.Compress != nil {
			return request.Compress.PasswordRef
		}
	case filestation.ActionExtract:
		if request.Extract != nil {
			return request.Extract.PasswordRef
		}
	case filestation.ActionShareLinkCreate, filestation.ActionShareLinkEdit:
		if request.ShareLink != nil {
			return request.ShareLink.PasswordRef
		}
	}
	return ""
}

func fileStationPlanHash(plan FilePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

func validateFilePlan(plan FilePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the FileStation plan")
	}
	if plan.APIVersion != fileStationAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid FileStation plan metadata")
	}
	if err := validateFileChange(plan.Request); err != nil {
		return err
	}
	expected, err := fileStationPlanHash(plan)
	if err != nil {
		return err
	}
	if expected != plan.Hash {
		return fmt.Errorf("FileStation plan contents were modified after planning")
	}
	return nil
}

func validateFileChange(request filestation.ChangeRequest) error {
	assertAbsolute := func(label, value string) error {
		if !strings.HasPrefix(value, "/") {
			return fmt.Errorf("%s must be an absolute NAS path, got %q", label, value)
		}
		return nil
	}
	assertName := func(value string) error {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "/\\") {
			return fmt.Errorf("name %q must be a non-empty base name without path separators", value)
		}
		return nil
	}
	switch request.Action {
	case filestation.ActionCreateFolder:
		change := request.CreateFolder
		if change == nil {
			return fmt.Errorf("create_folder payload is required")
		}
		if err := assertAbsolute("parent", change.Parent); err != nil {
			return err
		}
		return assertName(change.Name)
	case filestation.ActionRename:
		change := request.Rename
		if change == nil {
			return fmt.Errorf("rename payload is required")
		}
		if err := assertAbsolute("path", change.Path); err != nil {
			return err
		}
		return assertName(change.NewName)
	case filestation.ActionCopy, filestation.ActionMove:
		change := request.Transfer
		if change == nil || len(change.Sources) == 0 {
			return fmt.Errorf("transfer payload with at least one source is required")
		}
		for _, source := range change.Sources {
			if err := assertAbsolute("source", source); err != nil {
				return err
			}
		}
		return assertAbsolute("dest_folder", change.DestFolder)
	case filestation.ActionDelete:
		change := request.Delete
		if change == nil || len(change.Paths) == 0 {
			return fmt.Errorf("delete payload with at least one path is required")
		}
		for _, target := range change.Paths {
			if err := assertAbsolute("path", target); err != nil {
				return err
			}
			if target == "/" || strings.Count(strings.Trim(target, "/"), "/") == 0 {
				return fmt.Errorf("refusing to delete a shared-folder root or the volume root %q", target)
			}
		}
		return nil
	case filestation.ActionCompress:
		change := request.Compress
		if change == nil || len(change.Sources) == 0 {
			return fmt.Errorf("compress payload with at least one source is required")
		}
		for _, source := range change.Sources {
			if err := assertAbsolute("source", source); err != nil {
				return err
			}
		}
		if err := assertAbsolute("dest_archive", change.DestArchive); err != nil {
			return err
		}
		return assertCredentialRef(change.PasswordRef)
	case filestation.ActionExtract:
		change := request.Extract
		if change == nil {
			return fmt.Errorf("extract payload is required")
		}
		if err := assertAbsolute("archive", change.Archive); err != nil {
			return err
		}
		if err := assertAbsolute("dest_folder", change.DestFolder); err != nil {
			return err
		}
		return assertCredentialRef(change.PasswordRef)
	case filestation.ActionUpload:
		change := request.Upload
		if change == nil {
			return fmt.Errorf("upload payload is required")
		}
		if strings.TrimSpace(change.LocalPath) == "" {
			return fmt.Errorf("upload local_path is required")
		}
		return assertAbsolute("dest_folder", change.DestFolder)
	case filestation.ActionShareLinkCreate:
		change := request.ShareLink
		if change == nil {
			return fmt.Errorf("share_link payload is required")
		}
		if err := assertAbsolute("path", change.Path); err != nil {
			return err
		}
		return assertCredentialRef(change.PasswordRef)
	case filestation.ActionShareLinkDelete:
		change := request.ShareLink
		if change == nil || strings.TrimSpace(change.LinkID) == "" {
			return fmt.Errorf("share_link link_id is required for deletion")
		}
		return nil
	case filestation.ActionShareLinkEdit:
		change := request.ShareLink
		if change == nil || strings.TrimSpace(change.LinkID) == "" {
			return fmt.Errorf("share_link link_id is required for editing")
		}
		if strings.TrimSpace(change.ExpireDate) == "" && strings.TrimSpace(change.PasswordRef) == "" {
			return fmt.Errorf("a sharing-link edit requires expire_date and/or password_ref")
		}
		return assertCredentialRef(change.PasswordRef)
	case filestation.ActionShareLinkClearInvalid:
		if request.ShareLink != nil {
			return fmt.Errorf("sharelink_clear_invalid takes no payload")
		}
		return nil
	default:
		return fmt.Errorf("unsupported FileStation action %q", request.Action)
	}
}

func assertCredentialRef(ref string) error {
	if ref == "" {
		return nil
	}
	if !strings.HasPrefix(ref, "env:") {
		return fmt.Errorf("password_ref must be an env:NAME credential reference, not a literal password")
	}
	return nil
}

// FileStationFavoritesResult is the personal favorite list.
type FileStationFavoritesResult struct {
	NAS       string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Favorites synology.FileStationFavorites `json:"favorites" jsonschema:"Personal FileStation favorites"`
}

func (s *Service) GetFileStationFavorites(ctx context.Context, requestedNAS string) (FileStationFavoritesResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationFavoritesResult{}, err
	}
	favorites, err := client.FileStationFavoriteList(ctx)
	if err != nil {
		return FileStationFavoritesResult{}, authenticationError(name, err)
	}
	return FileStationFavoritesResult{NAS: name, Favorites: favorites}, nil
}

func (s *Service) AddFileStationFavorite(ctx context.Context, requestedNAS, targetPath, name string) (FileStationFavoritesResult, error) {
	profile, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationFavoritesResult{}, err
	}
	if !strings.HasPrefix(targetPath, "/") {
		return FileStationFavoritesResult{}, fmt.Errorf("favorite path must be an absolute NAS path, got %q", targetPath)
	}
	if err := client.FileStationFavoriteAdd(ctx, targetPath, name); err != nil {
		return FileStationFavoritesResult{}, authenticationError(profile, err)
	}
	favorites, err := client.FileStationFavoriteList(ctx)
	if err != nil {
		return FileStationFavoritesResult{}, authenticationError(profile, err)
	}
	return FileStationFavoritesResult{NAS: profile, Favorites: favorites}, nil
}

func (s *Service) RemoveFileStationFavorite(ctx context.Context, requestedNAS, targetPath string) (FileStationFavoritesResult, error) {
	profile, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationFavoritesResult{}, err
	}
	if err := client.FileStationFavoriteDelete(ctx, targetPath); err != nil {
		return FileStationFavoritesResult{}, authenticationError(profile, err)
	}
	favorites, err := client.FileStationFavoriteList(ctx)
	if err != nil {
		return FileStationFavoritesResult{}, authenticationError(profile, err)
	}
	return FileStationFavoritesResult{NAS: profile, Favorites: favorites}, nil
}

// FileStationSharingResult is the public sharing-link inventory.
type FileStationSharingResult struct {
	NAS   string                           `json:"nas" jsonschema:"NAS profile used for the request"`
	Links synology.FileStationSharingLinks `json:"links" jsonschema:"Public sharing links"`
}

func (s *Service) GetFileStationSharingLinks(ctx context.Context, requestedNAS string) (FileStationSharingResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationSharingResult{}, err
	}
	links, err := client.FileStationSharingList(ctx)
	if err != nil {
		return FileStationSharingResult{}, authenticationError(name, err)
	}
	return FileStationSharingResult{NAS: name, Links: links}, nil
}

// FileStationBackgroundTasksResult is the background file-operation task list.
type FileStationBackgroundTasksResult struct {
	NAS   string                              `json:"nas" jsonschema:"NAS profile used for the request"`
	Tasks synology.FileStationBackgroundTasks `json:"tasks" jsonschema:"Background file-operation tasks"`
}

func (s *Service) GetFileStationBackgroundTasks(ctx context.Context, requestedNAS string) (FileStationBackgroundTasksResult, error) {
	name, client, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return FileStationBackgroundTasksResult{}, err
	}
	tasks, err := client.FileStationBackgroundTasks(ctx)
	if err != nil {
		return FileStationBackgroundTasksResult{}, authenticationError(name, err)
	}
	return FileStationBackgroundTasksResult{NAS: name, Tasks: tasks}, nil
}
