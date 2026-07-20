package application

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/domain/office"
	"github.com/ychiu1211/dsmctl/internal/synology"
)

const officeAPIVersion = "dsmctl.io/v1alpha1"

type OfficeInfoResult struct {
	NAS  string              `json:"nas" jsonschema:"NAS profile used for the request"`
	Info synology.OfficeInfo `json:"info" jsonschema:"Normalized Synology Office deployment info"`
}

type OfficeSettingsResult struct {
	NAS      string                        `json:"nas" jsonschema:"NAS profile used for the request"`
	Settings synology.OfficeSystemSettings `json:"settings" jsonschema:"Normalized system-wide Synology Office settings"`
}

type OfficePreferencesResult struct {
	NAS         string                     `json:"nas" jsonschema:"NAS profile used for the request"`
	Preferences synology.OfficePreferences `json:"preferences" jsonschema:"Calling user's normalized Synology Office editor preferences"`
}

type OfficeFontsResult struct {
	NAS   string                `json:"nas" jsonschema:"NAS profile used for the request"`
	Fonts []synology.OfficeFont `json:"fonts" jsonschema:"Name-sorted Synology Office font inventory"`
}

type OfficeCapabilitiesResult struct {
	NAS          string                       `json:"nas" jsonschema:"NAS profile used for the request"`
	Capabilities synology.OfficeCapabilities  `json:"capabilities" jsonschema:"Selected Office settings operations and package evidence"`
	Report       synology.CompatibilityReport `json:"report" jsonschema:"Discovered APIs and selected Office backend"`
}

// OfficeObservedState is the pre-change state a plan binds to. Exactly the
// branch matching the change scope is populated, so an unrelated change on the
// other surfaces cannot spuriously invalidate the plan.
type OfficeObservedState struct {
	System      *synology.OfficeSystemSettings `json:"system,omitempty" jsonschema:"System settings observed during planning (system scope only)"`
	Preferences *synology.OfficePreferences    `json:"preferences,omitempty" jsonschema:"Editor preferences observed during planning (preferences scope only)"`
	Fonts       *[]synology.OfficeFont         `json:"fonts,omitempty" jsonschema:"Font inventory observed during planning (fonts scope only)"`
}

type OfficePlan struct {
	APIVersion          string              `json:"api_version" jsonschema:"Plan schema version"`
	NAS                 string              `json:"nas" jsonschema:"NAS profile selected during planning"`
	ProfileRevision     uint64              `json:"profile_revision,omitempty" jsonschema:"Persistent gateway profile revision selected during planning"`
	Request             office.Change       `json:"request" jsonschema:"Patch-only Office settings intent (exactly one scope)"`
	Observed            OfficeObservedState `json:"observed" jsonschema:"Complete scope state observed during planning"`
	ObservedFingerprint string              `json:"observed_fingerprint" jsonschema:"SHA-256 hash of the complete observed scope state"`
	Risk                string              `json:"risk" jsonschema:"Plan risk level: medium or high"`
	Warnings            []string            `json:"warnings" jsonschema:"Data-removal warnings"`
	Summary             []string            `json:"summary" jsonschema:"Human-readable patch operations"`
	Hash                string              `json:"hash" jsonschema:"SHA-256 approval hash covering intent and full observed state"`
}

type OfficeApplyResult struct {
	NAS      string                        `json:"nas" jsonschema:"NAS profile used for apply"`
	PlanHash string                        `json:"plan_hash" jsonschema:"Approved plan hash"`
	Applied  bool                          `json:"applied" jsonschema:"Whether DSM accepted the change and postcondition verification passed"`
	Result   synology.OfficeMutationResult `json:"result" jsonschema:"Selected DSM mutation backend"`
}

type officeClient interface {
	OfficeInfo(context.Context) (synology.OfficeInfo, error)
	OfficeSystemSettings(context.Context) (synology.OfficeSystemSettings, error)
	OfficePreferences(context.Context) (synology.OfficePreferences, error)
	OfficeFonts(context.Context) ([]synology.OfficeFont, error)
	OfficeCapabilities(context.Context) (synology.OfficeCapabilities, synology.CompatibilityReport, error)
	ApplyOfficeSystemChange(context.Context, office.SystemChange) (synology.OfficeMutationResult, error)
	ApplyOfficePreferencesChange(context.Context, office.PreferencesChange) (synology.OfficeMutationResult, error)
	ApplyOfficeFontChange(context.Context, office.FontChange) (synology.OfficeMutationResult, error)
}

func (s *Service) GetOfficeInfo(ctx context.Context, requestedNAS string) (OfficeInfoResult, error) {
	name, client, err := s.officeClient(ctx, requestedNAS)
	if err != nil {
		return OfficeInfoResult{}, err
	}
	info, err := client.OfficeInfo(ctx)
	if err != nil {
		return OfficeInfoResult{}, authenticationError(name, err)
	}
	return OfficeInfoResult{NAS: name, Info: info}, nil
}

func (s *Service) GetOfficeSettings(ctx context.Context, requestedNAS string) (OfficeSettingsResult, error) {
	name, client, err := s.officeClient(ctx, requestedNAS)
	if err != nil {
		return OfficeSettingsResult{}, err
	}
	settings, err := client.OfficeSystemSettings(ctx)
	if err != nil {
		return OfficeSettingsResult{}, authenticationError(name, err)
	}
	return OfficeSettingsResult{NAS: name, Settings: settings}, nil
}

func (s *Service) GetOfficePreferences(ctx context.Context, requestedNAS string) (OfficePreferencesResult, error) {
	name, client, err := s.officeClient(ctx, requestedNAS)
	if err != nil {
		return OfficePreferencesResult{}, err
	}
	preferences, err := client.OfficePreferences(ctx)
	if err != nil {
		return OfficePreferencesResult{}, authenticationError(name, err)
	}
	return OfficePreferencesResult{NAS: name, Preferences: preferences}, nil
}

func (s *Service) GetOfficeFonts(ctx context.Context, requestedNAS string) (OfficeFontsResult, error) {
	name, client, err := s.officeClient(ctx, requestedNAS)
	if err != nil {
		return OfficeFontsResult{}, err
	}
	fonts, err := client.OfficeFonts(ctx)
	if err != nil {
		return OfficeFontsResult{}, authenticationError(name, err)
	}
	return OfficeFontsResult{NAS: name, Fonts: fonts}, nil
}

func (s *Service) GetOfficeCapabilities(ctx context.Context, requestedNAS string) (OfficeCapabilitiesResult, error) {
	name, client, err := s.officeClient(ctx, requestedNAS)
	if err != nil {
		return OfficeCapabilitiesResult{}, err
	}
	capabilities, report, err := client.OfficeCapabilities(ctx)
	if err != nil {
		return OfficeCapabilitiesResult{}, authenticationError(name, err)
	}
	return OfficeCapabilitiesResult{NAS: name, Capabilities: capabilities, Report: report}, nil
}

func (s *Service) PlanOfficeChange(ctx context.Context, requestedNAS string, request office.Change) (OfficePlan, error) {
	if err := validateOfficeChange(request); err != nil {
		return OfficePlan{}, err
	}
	name, client, err := s.officeClient(ctx, requestedNAS)
	if err != nil {
		return OfficePlan{}, err
	}
	plan, err := planOfficeChangeWithClient(ctx, name, client, request)
	if err != nil {
		return OfficePlan{}, err
	}
	plan.ProfileRevision, err = s.profileRevision(ctx, name)
	if err == nil {
		plan.Hash, err = officePlanHash(plan)
	}
	return plan, err
}

func (s *Service) ApplyOfficePlan(ctx context.Context, plan OfficePlan, approvalHash string) (OfficeApplyResult, error) {
	if err := validateOfficePlan(plan, approvalHash); err != nil {
		return OfficeApplyResult{}, err
	}
	if err := s.authorizeRemoteApply(ctx, plan.NAS, plan.ProfileRevision, plan.Hash, plan.Risk); err != nil {
		return OfficeApplyResult{}, err
	}
	if err := s.verifyProfileRevision(ctx, plan.NAS, plan.ProfileRevision); err != nil {
		return OfficeApplyResult{}, err
	}
	name, client, err := s.officeClient(ctx, plan.NAS)
	if err != nil {
		return OfficeApplyResult{}, err
	}
	if name != plan.NAS {
		return OfficeApplyResult{}, fmt.Errorf("Office plan NAS %q resolved to different profile %q", plan.NAS, name)
	}
	return applyOfficePlanWithClient(ctx, client, plan)
}

func applyOfficePlanWithClient(ctx context.Context, client officeClient, plan OfficePlan) (OfficeApplyResult, error) {
	current, err := planOfficeChangeWithClient(ctx, plan.NAS, client, plan.Request)
	if err != nil {
		return OfficeApplyResult{}, fmt.Errorf("Office plan precondition no longer holds: %w", err)
	}
	current.ProfileRevision = plan.ProfileRevision
	current.Hash, err = officePlanHash(current)
	if err != nil {
		return OfficeApplyResult{}, err
	}
	if current.ObservedFingerprint != plan.ObservedFingerprint || current.Hash != plan.Hash {
		return OfficeApplyResult{}, fmt.Errorf("Office plan is stale; create a new plan")
	}
	var result synology.OfficeMutationResult
	switch {
	case plan.Request.System != nil:
		result, err = client.ApplyOfficeSystemChange(ctx, *plan.Request.System)
	case plan.Request.Preferences != nil:
		result, err = client.ApplyOfficePreferencesChange(ctx, *plan.Request.Preferences)
	case plan.Request.Fonts != nil:
		result, err = client.ApplyOfficeFontChange(ctx, *plan.Request.Fonts)
	default:
		return OfficeApplyResult{}, fmt.Errorf("Office plan has no scope")
	}
	if err != nil {
		return OfficeApplyResult{}, authenticationError(plan.NAS, err)
	}
	after, err := observeOfficeState(ctx, client, plan.Request)
	if err != nil {
		return OfficeApplyResult{}, fmt.Errorf("verify Office change: %w", err)
	}
	if !officeChangeMatches(after, plan.Request) {
		return OfficeApplyResult{}, fmt.Errorf("Office settings do not match the approved patch")
	}
	return OfficeApplyResult{NAS: plan.NAS, PlanHash: plan.Hash, Applied: true, Result: result}, nil
}

func (s *Service) officeClient(ctx context.Context, requestedNAS string) (string, officeClient, error) {
	name, generic, err := s.manager.Client(ctx, requestedNAS)
	if err != nil {
		return "", nil, err
	}
	client, ok := generic.(officeClient)
	if !ok {
		return "", nil, fmt.Errorf("NAS client does not implement Office management")
	}
	return name, client, nil
}

func observeOfficeState(ctx context.Context, client officeClient, request office.Change) (OfficeObservedState, error) {
	observed := OfficeObservedState{}
	if request.System != nil {
		settings, err := client.OfficeSystemSettings(ctx)
		if err != nil {
			return OfficeObservedState{}, err
		}
		observed.System = &settings
	}
	if request.Preferences != nil {
		preferences, err := client.OfficePreferences(ctx)
		if err != nil {
			return OfficeObservedState{}, err
		}
		observed.Preferences = &preferences
	}
	if request.Fonts != nil {
		fonts, err := client.OfficeFonts(ctx)
		if err != nil {
			return OfficeObservedState{}, err
		}
		observed.Fonts = &fonts
	}
	return observed, nil
}

func planOfficeChangeWithClient(ctx context.Context, nas string, client officeClient, request office.Change) (OfficePlan, error) {
	capabilities, _, err := client.OfficeCapabilities(ctx)
	if err != nil {
		return OfficePlan{}, authenticationError(nas, err)
	}
	if request.System != nil {
		if !capabilities.SystemRead {
			return OfficePlan{}, fmt.Errorf("NAS %q does not expose a verified Office system read backend", nas)
		}
		if !capabilities.SystemSet {
			return OfficePlan{}, fmt.Errorf("NAS %q does not expose a verified Office system set backend", nas)
		}
	}
	if request.Preferences != nil {
		if !capabilities.PreferencesRead {
			return OfficePlan{}, fmt.Errorf("NAS %q does not expose a verified Office preferences read backend", nas)
		}
		if !capabilities.PreferencesSet {
			return OfficePlan{}, fmt.Errorf("NAS %q does not expose a verified Office preferences set backend", nas)
		}
	}
	if request.Fonts != nil {
		if !capabilities.FontsRead {
			return OfficePlan{}, fmt.Errorf("NAS %q does not expose a verified Office fonts read backend", nas)
		}
		if !capabilities.FontsSet {
			return OfficePlan{}, fmt.Errorf("NAS %q does not expose a verified Office fonts set backend", nas)
		}
	}
	observed, err := observeOfficeState(ctx, client, request)
	if err != nil {
		return OfficePlan{}, authenticationError(nas, err)
	}
	if request.Fonts != nil {
		if err := validateOfficeFontTargets(observed, *request.Fonts); err != nil {
			return OfficePlan{}, err
		}
	}
	if officeChangeMatches(observed, request) {
		return OfficePlan{}, fmt.Errorf("Office patch would not change the current settings")
	}
	plan := OfficePlan{APIVersion: officeAPIVersion, NAS: nas, Request: request, Observed: observed}
	plan.ObservedFingerprint, err = hashJSON(plan.Observed)
	if err != nil {
		return OfficePlan{}, err
	}
	plan.Warnings, plan.Summary = officePlanEffects(observed, request)
	if len(plan.Warnings) > 0 {
		plan.Risk = "high"
	} else {
		plan.Risk = "medium"
	}
	plan.Hash, err = officePlanHash(plan)
	if err != nil {
		return OfficePlan{}, err
	}
	return plan, nil
}

func validateOfficeChange(change office.Change) error {
	scopes := 0
	for _, set := range []bool{change.System != nil, change.Preferences != nil, change.Fonts != nil} {
		if set {
			scopes++
		}
	}
	if scopes != 1 {
		return fmt.Errorf("Office change must target exactly one scope: system, preferences, or fonts")
	}
	switch {
	case change.System != nil:
		if reflect.DeepEqual(*change.System, office.SystemChange{}) {
			return fmt.Errorf("Office system patch has no fields")
		}
	case change.Preferences != nil:
		if reflect.DeepEqual(*change.Preferences, office.PreferencesChange{}) {
			return fmt.Errorf("Office preferences patch has no fields")
		}
	case change.Fonts != nil:
		return validateOfficeFontChange(*change.Fonts)
	}
	return nil
}

func validateOfficeFontChange(change office.FontChange) error {
	switch change.Action {
	case office.FontActionAdd, office.FontActionEnable, office.FontActionDisable, office.FontActionDelete:
	default:
		return fmt.Errorf("Office font action must be add, enable, disable, or delete")
	}
	if len(change.Names) == 0 {
		return fmt.Errorf("Office font change has no font names")
	}
	seen := map[string]bool{}
	for _, name := range change.Names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("Office font change contains an empty font name")
		}
		if seen[name] {
			return fmt.Errorf("Office font change lists %q twice", name)
		}
		seen[name] = true
	}
	return nil
}

// validateOfficeFontTargets checks the requested names against the observed
// inventory. DSM silently skips system fonts and unknown names (verified
// live), so acting on them must fail during planning instead of surfacing as
// a late postcondition error.
func validateOfficeFontTargets(observed OfficeObservedState, change office.FontChange) error {
	if observed.Fonts == nil {
		return fmt.Errorf("Office font plan is missing the observed inventory")
	}
	inventory := map[string]synology.OfficeFont{}
	for _, font := range *observed.Fonts {
		inventory[font.Name] = font
	}
	for _, name := range change.Names {
		font, exists := inventory[name]
		if change.Action == office.FontActionAdd {
			if exists && !font.Custom {
				return fmt.Errorf("font %q is a system font and cannot be re-added", name)
			}
			continue
		}
		if !exists {
			return fmt.Errorf("font %q does not exist", name)
		}
		if !font.Custom {
			return fmt.Errorf("font %q is a system font and cannot be changed", name)
		}
	}
	return nil
}

func officePlanEffects(observed OfficeObservedState, change office.Change) ([]string, []string) {
	warnings := []string{}
	summary := []string{}
	if change.System != nil && change.System.HistoryPrune != nil {
		next := *change.System.HistoryPrune
		summary = append(summary, fmt.Sprintf("set system history_prune to %t", next))
		if next && observed.System != nil && !observed.System.HistoryPrune {
			warnings = append(warnings, "enabling automatic version-history cleanup permanently deletes older document versions")
		}
	}
	if change.Preferences != nil {
		preferences := change.Preferences
		boolChange := func(name string, next *bool) {
			if next != nil {
				summary = append(summary, fmt.Sprintf("set preference %s to %t", name, *next))
			}
		}
		boolChange("ruler", preferences.Ruler)
		boolChange("formula_preview", preferences.FormulaPreview)
		boolChange("formula_panel_opened", preferences.FormulaPanelOpened)
		boolChange("formula_panel_expanded", preferences.FormulaPanelExpanded)
		if preferences.DefaultLocale != nil {
			summary = append(summary, fmt.Sprintf("set preference default_locale to %q", *preferences.DefaultLocale))
		}
		if preferences.AITranslatorLanguage != nil {
			summary = append(summary, fmt.Sprintf("set preference ai_translator_language to %q", *preferences.AITranslatorLanguage))
		}
		if preferences.AIHelperLanguages != nil {
			summary = append(summary, "replace the preference ai_helper_languages list")
		}
	}
	if change.Fonts != nil {
		summary = append(summary, fmt.Sprintf("%s %d custom font(s): %s",
			change.Fonts.Action, len(change.Fonts.Names), strings.Join(change.Fonts.Names, ", ")))
	}
	return warnings, summary
}

func officeChangeMatches(state OfficeObservedState, change office.Change) bool {
	if change.System != nil {
		if state.System == nil {
			return false
		}
		if change.System.HistoryPrune != nil && state.System.HistoryPrune != *change.System.HistoryPrune {
			return false
		}
	}
	if change.Preferences != nil {
		if state.Preferences == nil {
			return false
		}
		preferences, current := change.Preferences, state.Preferences
		if preferences.Ruler != nil && current.Ruler != *preferences.Ruler {
			return false
		}
		if preferences.FormulaPreview != nil && current.FormulaPreview != *preferences.FormulaPreview {
			return false
		}
		if preferences.FormulaPanelOpened != nil && current.FormulaPanelOpened != *preferences.FormulaPanelOpened {
			return false
		}
		if preferences.FormulaPanelExpanded != nil && current.FormulaPanelExpanded != *preferences.FormulaPanelExpanded {
			return false
		}
		if preferences.DefaultLocale != nil && current.DefaultLocale != *preferences.DefaultLocale {
			return false
		}
		if preferences.AITranslatorLanguage != nil && current.AITranslatorLanguage != *preferences.AITranslatorLanguage {
			return false
		}
		if preferences.AIHelperLanguages != nil && !reflect.DeepEqual(current.AIHelperLanguages, *preferences.AIHelperLanguages) {
			return false
		}
	}
	if change.Fonts != nil {
		if state.Fonts == nil {
			return false
		}
		inventory := map[string]synology.OfficeFont{}
		for _, font := range *state.Fonts {
			inventory[font.Name] = font
		}
		for _, name := range change.Fonts.Names {
			font, exists := inventory[name]
			switch change.Fonts.Action {
			case office.FontActionAdd:
				if !exists || !font.Custom {
					return false
				}
			case office.FontActionEnable:
				if !exists || !font.Custom || !font.Enabled {
					return false
				}
			case office.FontActionDisable:
				if !exists || !font.Custom || font.Enabled {
					return false
				}
			case office.FontActionDelete:
				if exists {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

func validateOfficePlan(plan OfficePlan, approvalHash string) error {
	if strings.TrimSpace(approvalHash) == "" || approvalHash != plan.Hash {
		return fmt.Errorf("approval hash does not match the Office plan")
	}
	if plan.APIVersion != officeAPIVersion || strings.TrimSpace(plan.NAS) == "" {
		return fmt.Errorf("invalid Office plan metadata")
	}
	if err := validateOfficeChange(plan.Request); err != nil {
		return err
	}
	expectedFingerprint, err := hashJSON(plan.Observed)
	if err != nil || expectedFingerprint != plan.ObservedFingerprint {
		return fmt.Errorf("Office plan observed settings were modified")
	}
	expectedHash, err := officePlanHash(plan)
	if err != nil {
		return err
	}
	if expectedHash != plan.Hash {
		return fmt.Errorf("Office plan contents were modified after planning")
	}
	return nil
}

func officePlanHash(plan OfficePlan) (string, error) {
	plan.Hash = ""
	return hashJSON(plan)
}

var _ officeClient = (*synology.Client)(nil)
