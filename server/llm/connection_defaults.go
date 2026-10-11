package llm

import "core/shared/config"

// ApplyConnectionModelDefaults preserves authored selections and Session
// selections. Only unresolved defaults depend on the actual connection.
func ApplyConnectionModelDefaults(settings *config.Settings, sources map[string]config.Origin) error {
	connection, err := settings.SelectedConnection()
	if err != nil {
		return err
	}
	selected, err := ResolveConnectionVariant(connection)
	if err != nil {
		return err
	}
	if selected.Variant.DefaultModel == nil {
		return nil
	}
	defaults, _ := LookupModelCapabilityContract(*selected.Variant.DefaultModel)
	if sources["model"].Kind == config.SourceDefault {
		settings.Model = defaults.Model
	}
	if sources["thinking_level"].Kind == config.SourceDefault {
		settings.ThinkingLevel = *defaults.DefaultReasoningEffort
	}
	if sources["model_context_window"].Kind == config.SourceDefault {
		derived := *settings
		if err := ApplyDerivedModelContextBudget(&derived, settings.Model, settings.ModelContextWindow, settings.ContextCompactionThresholdTokens); err != nil {
			return err
		}
		settings.ModelContextWindow = derived.ModelContextWindow
		if sources["context_compaction_threshold_tokens"].Kind == config.SourceDefault {
			settings.ContextCompactionThresholdTokens = derived.ContextCompactionThresholdTokens
		}
	}
	config.InheritReviewerSettings(settings, sources)
	return nil
}
