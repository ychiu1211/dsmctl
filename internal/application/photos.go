package application

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/photos"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const photosAPIVersion = "dsmctl.io/v1alpha1"

type PhotosSettingsResult struct {
	NAS      string                    `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.PhotosAdminSettings `json:"settings" jsonschema:"Normalized Synology Photos administration settings"`
}

type PhotosCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.PhotosCapabilities  `json:"capabilities" jsonschema:"Selected Photos administration operations and package evidence"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected Photos backend"`
}

type PhotosPlan struct {
	APIVersion          string                       `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string                       `json:"nas" jsonschema:"NAS profile selected during planning"`
	Request             photos.AdminChange           `json:"request" jsonschema:"Patch-only Photos administration intent"`
	Observed            synology.PhotosAdminSettings `json:"observed" jsonschema:"Complete settings observed during planning"`
	ObservedFingerprint string                       `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed settings"`
	Risk                string                       `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string                     `json:"warnings" jsonschema:"Data-loss and privacy warnings"`
	Summary             []string                     `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string                       `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed settings"`
}

type PhotosApplyResult struct {
	NAS      string                         `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                         `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                           `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.PhotosMutationResult  `json:"result" jsonschema:"Selected DSM mutation backend"`
}

type photosClient interface {
	PhotosAdminSettings(context.Context) (synology.PhotosAdminSettings, error)
	PhotosCapabilities(context.Context) (synology.PhotosCapabilities, synology.CompatibilityReport, error)
	ApplyPhotosAdminChange(context.Context, photos.AdminChange) (synology.PhotosMutationResult, error)
}

func (s *Service) GetPhotosSettings(ctx context.Context, requestedNAS string) (PhotosSettingsResult, error) {
	name, client, err := s.photosClient(ctx, requestedNAS)
	if err != nil {
		return PhotosSettingsResult{}, err
	}
	settings, err := client.PhotosAdminSettings(ctx)
	if err != nil {
		return PhotosSettingsResult{}, authenticationError(name, err)
	}
	return PhotosSettingsResult{NAS: name, Settings: settings}, nil
}

func (s *Service) GetPhotosCapabilities(ctx context.Context, requestedNAS string) (PhotosCapabilitiesResult, error) {
	name, client, err := s.photosClient(ctx, requestedNAS)
	if err != nil {
		return PhotosCapabilitiesResult{}, err
	}
	capabilities, report, err := client.PhotosCapabilities(ctx)
	if err != nil {
		return PhotosCapabilitiesResult{}, authenticationError(name, err)
	}
	return PhotosCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanPhotosChange(ctx context.Context, requestedNAS string, request photos.AdminChange) (PhotosPlan, error) {
	if err := validatePhotosChange(request); err != nil {
		return PhotosPlan{}, err
	}
	name, client, err := s.photosClient(ctx, requestedNAS)
	if err != nil {
		return PhotosPlan{}, err
	}
	return planPhotosChangeWithClient(ctx, name, client, request)
}

func (s *Service) ApplyPhotosPlan(ctx context.Context, plan PhotosPlan, approvalHash string) (PhotosApplyResult, error) {
	if err := validatePhotosPlan(plan, approvalHash); err != nil {
		return PhotosApplyResult{}, err
	}
	name, client, err := s.photosClient(ctx, plan.NAS)
	if err != nil {
		return PhotosApplyResult{}, err
	}
	if name != plan.NAS {
		return PhotosApplyResult{}, fmt.Errorf("Photos plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyPhotosPlanWithClient(ctx, client, plan)
}

func applyPhotosPlanWithClient(ctx context.Context, client photosClient, plan PhotosPlan) (PhotosApplyResult, error) {
	current, err := planPhotosChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return PhotosApplyResult{}, fmt.Errorf("Photos plan precondition no longer holds: %w", err)
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return PhotosApplyResult{}, fmt.Errorf("Photos plan is stale; create a new plan")
	}
	result, err := client.ApplyPhotosAdminChange(ctx, plan.Request)
	if err != nil {
		return PhotosApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := client.PhotosAdminSettings(ctx)
	if err != nil {
		return PhotosApplyResult{}, fmt.Errorf("verify Photos change: %w", err)
	}
	if !photosChangeMatches(after, plan.Request) {
		return PhotosApplyResult{}, fmt.Errorf("Photos settings do not match the approved patch")
	}
	return PhotosApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

func (s *Service) photosClient(ctx context.Context, requestedNAS string) (string, photosClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(photosClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Photos management")
	}
	return name, client, nil
}

func planPhotosChangeWithClient(ctx context.Context, nas string, client photosClient, request photos.AdminChange) (PhotosPlan, error) {
	capabilities, _, err := client.PhotosCapabilities(ctx)
	if err != nil {
		return PhotosPlan{}, authenticationError(nas, err)
	}
	if !capabilities.AdminRead {
		return PhotosPlan{}, fmt.Errorf("NAS %q does not expose a verified Photos admin read backend", nas)
	}
	if !capabilities.AdminSet {
		return PhotosPlan{}, fmt.Errorf("NAS %q does not expose a verified Photos admin set backend", nas)
	}
	observed, err := client.PhotosAdminSettings(ctx)
	if err != nil {
		return PhotosPlan{}, authenticationError(nas, err)
	}
	if photosChangeMatches(observed, request) {
		return PhotosPlan{}, fmt.Errorf("Photos patch would not change the current settings")
	}
	plan := PhotosPlan{APIVersion: photosAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return PhotosPlan{}, err
	}
	plan.Warnings, plan.Summary = photosPlanEffects(observed, request)
	if len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = photosPlanHash(plan)
	if err != nil {
		return PhotosPlan{}, err
	}
	return plan, nil
}

func validatePhotosChange(change photos.AdminChange) error {
	if reflect.DeepEqual(change, photos.AdminChange{}) {
		return fmt.Errorf("Photos patch has no fields")
	}
	if change.DefaultThumbnailSize != nil && strings.TrimSpace(*change.DefaultThumbnailSize) == "" {
		return fmt.Errorf("default_thumbnail_size cannot be empty")
	}
	return nil
}

func photosPlanEffects(observed synology.PhotosAdminSettings, change photos.AdminChange) ([]string, []string) {
	warnings := []string{}
	summary := []string{}
	boolChange := func(name string, cur bool, next *bool, disableWarning string) {
		if next == nil {
			return
		}
		summary = append(summary, fmt.Sprintf("set %s to %t", name, *next))
		if disableWarning != "" && cur && !*next {
			warnings = append(warnings, disableWarning)
		}
	}
	boolChange("face_recognition", observed.FaceRecognition, change.FaceRecognition, "disabling face recognition removes people grouping")
	boolChange("concept_grouping", observed.ConceptGrouping, change.ConceptGrouping, "")
	boolChange("similar_grouping", observed.SimilarGrouping, change.SimilarGrouping, "")
	boolChange("user_sharing", observed.UserSharing, change.UserSharing, "disabling user sharing revokes existing user-created shares")
	boolChange("show_info_to_guest", observed.ShowInfoToGuest, change.ShowInfoToGuest, "")
	boolChange("personal_recycle_bin", observed.PersonalRecycleBin, change.PersonalRecycleBin, "disabling the personal recycle bin makes deletions in personal space permanent")
	boolChange("shared_recycle_bin", observed.SharedRecycleBin, change.SharedRecycleBin, "disabling the shared recycle bin makes deletions in shared space permanent")
	boolChange("converted_original_jpeg", observed.ConvertedOriginalJPEG, change.ConvertedOriginalJPEG, "")
	boolChange("need_hevc", observed.NeedHEVC, change.NeedHEVC, "")
	if change.DefaultThumbnailSize != nil {
		summary = append(summary, fmt.Sprintf("set default_thumbnail_size to %q", *change.DefaultThumbnailSize))
	}
	if change.ExcludeExtensions != nil {
		summary = append(summary, "replace the excluded-extension list")
	}
	return warnings, summary
}

func photosChangeMatches(state synology.PhotosAdminSettings, change photos.AdminChange) bool {
	if change.FaceRecognition != nil && state.FaceRecognition != *change.FaceRecognition {
		return false
	}
	if change.ConceptGrouping != nil && state.ConceptGrouping != *change.ConceptGrouping {
		return false
	}
	if change.SimilarGrouping != nil && state.SimilarGrouping != *change.SimilarGrouping {
		return false
	}
	if change.UserSharing != nil && state.UserSharing != *change.UserSharing {
		return false
	}
	if change.ShowInfoToGuest != nil && state.ShowInfoToGuest != *change.ShowInfoToGuest {
		return false
	}
	if change.PersonalRecycleBin != nil && state.PersonalRecycleBin != *change.PersonalRecycleBin {
		return false
	}
	if change.SharedRecycleBin != nil && state.SharedRecycleBin != *change.SharedRecycleBin {
		return false
	}
	if change.ConvertedOriginalJPEG != nil && state.ConvertedOriginalJPEG != *change.ConvertedOriginalJPEG {
		return false
	}
	if change.NeedHEVC != nil && state.NeedHEVC != *change.NeedHEVC {
		return false
	}
	if change.DefaultThumbnailSize != nil && state.DefaultThumbnailSize != *change.DefaultThumbnailSize {
		return false
	}
	if change.ExcludeExtensions != nil && !reflect.DeepEqual(state.ExcludeExtensions, *change.ExcludeExtensions) {
		return false
	}
	return true
}

func validatePhotosPlan(plan PhotosPlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the Photos plan")
	}
	if plan.APIVersion != photosAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid Photos plan metadata")
	}
	if err := validatePhotosChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("Photos plan observed settings were modified")
	}
	expectedHash, err := photosPlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("Photos plan contents were modified after planning")
	}
	return nil
}

func photosPlanHash(plan PhotosPlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ photosClient = (*synology.Client)(nil)
