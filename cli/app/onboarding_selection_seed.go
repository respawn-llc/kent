package app

import (
	"fmt"
	"slices"
	"strings"

	"core/shared/config"
	capabilitypb "core/shared/protoapi/gen/kent/api/capability"
	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
	"core/shared/theme"
)

type onboardingSelectionConversionError struct {
	Field  string
	Value  any
	Reason string
}

func (e *onboardingSelectionConversionError) Error() string {
	return fmt.Sprintf("convert onboarding selection %s (%v): %s", e.Field, e.Value, e.Reason)
}

func newOnboardingFlowState(local config.LocalPreferences, facts *capabilitypb.Facts) (onboardingFlowState, error) {
	selections, err := onboardingSelectionsFromFacts(local, facts)
	if err != nil {
		return onboardingFlowState{}, err
	}
	state := onboardingFlowState{
		selections:    selections,
		facts:         facts,
		pendingAction: onboardingPendingActionNone,
		imports:       onboardingImportDiscoveryFromFacts(facts.GetImports()),
		debug:         local.Debug,
	}
	if id := state.imports.commandRecommendationID; id != nil {
		for _, choice := range state.imports.commandChoices {
			if choice.OptionID == *id {
				state.selections.commandImport = onboardingImportSelection{Mode: choice.Mode, ChoiceRef: choice.Ref}
				break
			}
		}
	}
	state.recomputeSkillEnablement(&state.selections)
	if err := state.validateInvariant("construction", "theme"); err != nil {
		return onboardingFlowState{}, err
	}
	return state, nil
}

func onboardingSelectionsFromFacts(local config.LocalPreferences, facts *capabilitypb.Facts) (onboardingSelections, error) {
	defaults := facts.GetDefaults()
	model := onboardingModelSelectionForModel(strings.TrimSpace(defaults.PrimaryModelId), facts)
	themeSelection, err := seedThemeSelection(local.Theme)
	if err != nil {
		return onboardingSelections{}, err
	}
	contextWindow := seedContextSelection(int(defaults.GetContextWindowTokens()), model.value, facts)
	thinking := seedThinkingFact(defaults.Thinking, modelFactForFacts(facts, model.value).SupportedThinkingLevels)
	verbosity := seedVerbositySelection(defaults.Verbosity)
	supervisor := seedSupervisorSelection(defaults.Supervisor, facts)
	compaction := seedCompactionSelection(config.CompactionMode(defaults.CompactionMode))
	var baselineModelContextWindow *int
	if defaults.ContextWindowTokens != nil {
		value := int(*defaults.ContextWindowTokens)
		baselineModelContextWindow = &value
	}
	return onboardingSelections{
		theme:                      themeSelection,
		model:                      model,
		contextWindow:              contextWindow,
		thinking:                   thinking,
		verbosity:                  verbosity,
		askQuestion:                defaults.AskQuestion,
		supervisor:                 supervisor,
		compaction:                 compaction,
		skillImport:                onboardingImportSelection{Mode: onboardingImportModeNone},
		commandImport:              onboardingImportSelection{Mode: onboardingImportModeNone},
		skillEnablement:            map[string]bool{},
		pendingPrimaryThinking:     onboardingThinkingEdit{kind: onboardingThinkingEditNone},
		pendingReviewerThinking:    onboardingThinkingEdit{kind: onboardingThinkingEditNone},
		baselineModelContextWindow: baselineModelContextWindow,
	}, nil
}

func seedThemeSelection(value string) (onboardingThemeSelection, error) {
	switch theme.Normalize(value) {
	case theme.Auto:
		return onboardingThemeSelection{kind: onboardingThemeAuto}, nil
	case theme.Light:
		return onboardingThemeSelection{kind: onboardingThemeLight}, nil
	case theme.Dark:
		return onboardingThemeSelection{kind: onboardingThemeDark}, nil
	default:
		return onboardingThemeSelection{}, conversionError("theme", value, "unsupported theme")
	}
}

func onboardingModelSelectionFromValue(value string, facts *capabilitypb.Facts) (onboardingModelSelection, error) {
	model := strings.TrimSpace(value)
	if model == "" {
		return onboardingModelSelection{}, conversionError("model", value, "must not be blank")
	}
	return onboardingModelSelectionForModel(model, facts), nil
}

func onboardingModelSelectionForModel(model string, facts *capabilitypb.Facts) onboardingModelSelection {
	kind := onboardingModelCustom
	if modelFactForFacts(facts, model).Known {
		kind = onboardingModelKnown
	}
	return onboardingModelSelection{kind: kind, value: model}
}

func seedContextSelection(tokens int, model string, facts *capabilitypb.Facts) onboardingContextSelection {
	fact := modelFactForFacts(facts, model)
	switch {
	case tokens == 0:
		return onboardingContextSelection{kind: onboardingContextDefault}
	case fact.ContextWindowTokens != nil && tokens == int(*fact.ContextWindowTokens):
		return onboardingContextSelection{kind: onboardingContextDefault}
	case fact.LargeWindow != nil && tokens == int(fact.LargeWindow.Tokens):
		return onboardingContextSelection{kind: onboardingContextLarge}
	default:
		return onboardingContextSelection{kind: onboardingContextCustom, tokens: tokens}
	}
}

func seedThinkingSelection(value string, supportedLevels []string) onboardingThinkingSelection {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return onboardingThinkingSelection{kind: onboardingThinkingDisabled}
	}
	if slices.Contains(supportedLevels, trimmed) {
		return onboardingThinkingSelection{kind: onboardingThinkingLevel, value: trimmed}
	}
	return onboardingThinkingSelection{kind: onboardingThinkingCustom, value: trimmed}
}

func seedVerbositySelection(value *capabilitypb.VerbosityDefaultFact) onboardingVerbositySelection {
	if value == nil {
		return onboardingVerbositySelection{kind: onboardingVerbosityOmitted}
	}
	return onboardingVerbositySelection{kind: onboardingVerbosityLevel, value: value.Level}
}

func seedSupervisorSelection(choice *onboardingpb.SupervisorChoice, facts *capabilitypb.Facts) onboardingSupervisorSelection {
	frequency := map[onboardingpb.SupervisorFrequency]onboardingSupervisorFrequency{
		onboardingpb.SupervisorFrequency_SUPERVISOR_FREQUENCY_OFF:   onboardingSupervisorOff,
		onboardingpb.SupervisorFrequency_SUPERVISOR_FREQUENCY_EDITS: onboardingSupervisorEdits,
		onboardingpb.SupervisorFrequency_SUPERVISOR_FREQUENCY_ALL:   onboardingSupervisorAll,
	}[choice.Frequency]
	reviewerModel := onboardingReviewerModelSelection{kind: onboardingReviewerModelInherited}
	model := facts.Defaults.PrimaryModelId
	if choice.Model != nil {
		switch choice.Model.Kind {
		case onboardingpb.ModelKind_MODEL_KIND_KNOWN:
			model = choice.Model.GetModelId()
		case onboardingpb.ModelKind_MODEL_KIND_CUSTOM:
			model = choice.Model.GetAlias()
		}
		override := onboardingModelSelectionForModel(strings.TrimSpace(model), facts)
		reviewerModel = onboardingReviewerModelSelection{kind: onboardingReviewerModelOverridden, override: override}
	}
	reviewerThinking := onboardingReviewerThinkingSelection{kind: onboardingReviewerThinkingInherited}
	if choice.Thinking != nil {
		var thinkingFact capabilitypb.ThinkingDefaultFact
		switch choice.Thinking.Kind {
		case onboardingpb.ThinkingKind_THINKING_KIND_DEFAULT:
			thinkingFact.Mode = "default"
		case onboardingpb.ThinkingKind_THINKING_KIND_DISABLED:
			thinkingFact.Mode = "disabled"
		case onboardingpb.ThinkingKind_THINKING_KIND_LEVEL:
			thinkingFact.Mode, thinkingFact.Level = "level", choice.Thinking.Level
		case onboardingpb.ThinkingKind_THINKING_KIND_CUSTOM:
			thinkingFact.Mode, thinkingFact.Value = "custom", choice.Thinking.Value
		}
		override := seedThinkingFact(&thinkingFact, modelFactForFacts(facts, model).SupportedThinkingLevels)
		reviewerThinking = onboardingReviewerThinkingSelection{kind: onboardingReviewerThinkingOverridden, override: override}
	}
	return onboardingSupervisorSelection{
		frequency: frequency,
		model:     reviewerModel,
		thinking:  reviewerThinking,
	}
}

func seedCompactionSelection(value config.CompactionMode) onboardingCompactionSelection {
	return map[config.CompactionMode]onboardingCompactionSelection{
		config.CompactionModeLocal:  onboardingCompactionLocal,
		config.CompactionModeNative: onboardingCompactionNative,
		config.CompactionModeNone:   onboardingCompactionManualOnly,
	}[value]
}

func seedThinkingFact(fact *capabilitypb.ThinkingDefaultFact, levels []string) onboardingThinkingSelection {
	if fact.Mode == "level" {
		return seedThinkingSelection(fact.GetLevel(), levels)
	}
	return map[string]onboardingThinkingSelection{
		"default":  {kind: onboardingThinkingDefault},
		"disabled": {kind: onboardingThinkingDisabled},
		"custom":   {kind: onboardingThinkingCustom, value: fact.GetValue()},
	}[fact.Mode]
}

func conversionError(field string, value any, reason string) error {
	return &onboardingSelectionConversionError{Field: field, Value: value, Reason: reason}
}
