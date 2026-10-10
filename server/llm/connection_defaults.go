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
	if selected.Provider != ProviderGrok {
		return nil
	}
	if sources["model"].Kind == config.SourceDefault {
		settings.Model = "grok-4.7"
	}
	if sources["thinking_level"].Kind == config.SourceDefault {
		settings.ThinkingLevel = "high"
	}
	metadata, err := ModelContextForSettings(*settings, settings.Model)
	if err != nil {
		return err
	}
	if metadata != nil && sources["model_context_window"].Kind == config.SourceDefault {
		settings.ModelContextWindow = metadata.ContextWindowTokens
		if sources["context_compaction_threshold_tokens"].Kind == config.SourceDefault {
			settings.ContextCompactionThresholdTokens = settings.ModelContextWindow * 95 / 100
		}
	}
	config.InheritReviewerSettings(settings, sources)
	return nil
}
