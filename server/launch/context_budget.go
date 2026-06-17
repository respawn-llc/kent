package launch

import (
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/shared/compaction"
	"core/shared/config"
)

type contextBudgetFallback struct {
	window    int
	threshold int
	lead      int
}

type contextBudgetReconciliation struct {
	sources       map[string]string
	fallback      contextBudgetFallback
	modelSelected bool
	locked        *session.LockedContract
}

func budgetFallbackFromSettings(settings config.Settings) contextBudgetFallback {
	return contextBudgetFallback{
		window:    settings.ModelContextWindow,
		threshold: settings.ContextCompactionThresholdTokens,
		lead:      settings.PreSubmitCompactionLeadTokens,
	}
}

func reconcileContextBudget(settings *config.Settings, options contextBudgetReconciliation) error {
	if settings == nil {
		return nil
	}
	if options.locked != nil {
		if !reconcileLockedContextBudget(settings, options) {
			return nil
		}
		return validateContextBudgetTriple(*settings)
	}
	explicitWindow := explicitBudgetSource(options.sources, "model_context_window")
	explicitThreshold := explicitBudgetSource(options.sources, "context_compaction_threshold_tokens")
	explicitLead := explicitBudgetSource(options.sources, "pre_submit_compaction_lead_tokens")
	budgetSelected := explicitWindow || explicitThreshold || explicitLead
	if !options.modelSelected && !budgetSelected {
		return nil
	}
	if options.modelSelected && !explicitWindow {
		if meta, ok := llm.LookupModelMetadata(settings.Model); ok && meta.ContextWindowTokens > 0 {
			settings.ModelContextWindow = meta.ContextWindowTokens
		} else {
			settings.ModelContextWindow = options.fallback.window
			if !explicitThreshold {
				settings.ContextCompactionThresholdTokens = options.fallback.threshold
			}
			if !explicitLead {
				settings.PreSubmitCompactionLeadTokens = options.fallback.lead
			}
			if options.fallback.window <= 0 && !explicitThreshold && !explicitLead {
				return nil
			}
			return validateContextBudgetTriple(*settings)
		}
	}
	if !explicitThreshold {
		settings.ContextCompactionThresholdTokens = settings.ModelContextWindow * 95 / 100
	}
	if !explicitLead && (options.modelSelected || explicitWindow) {
		settings.PreSubmitCompactionLeadTokens = compaction.DefaultPreSubmitRunwayTokens
	}
	return validateContextBudgetTriple(*settings)
}

func reconcileLockedContextBudget(settings *config.Settings, options contextBudgetReconciliation) bool {
	window := options.locked.ContextWindow
	if window <= 0 {
		window = options.fallback.window
	}
	if window <= 0 {
		return false
	}
	settings.ModelContextWindow = window
	if validContextThreshold(window, options.fallback.threshold) {
		settings.ContextCompactionThresholdTokens = options.fallback.threshold
	} else {
		percent := options.locked.ContextPercent
		if percent <= 0 || percent > 100 {
			percent = 95
		}
		settings.ContextCompactionThresholdTokens = window * percent / 100
	}
	explicitLead := explicitBudgetSource(options.sources, "pre_submit_compaction_lead_tokens")
	lead := options.fallback.lead
	if explicitLead {
		lead = settings.PreSubmitCompactionLeadTokens
	}
	if validContextLead(window, settings.ContextCompactionThresholdTokens, lead) {
		settings.PreSubmitCompactionLeadTokens = lead
		return true
	}
	if explicitLead {
		settings.PreSubmitCompactionLeadTokens = lead
		return true
	}
	settings.PreSubmitCompactionLeadTokens = compaction.DefaultPreSubmitRunwayTokens
	return true
}

func explicitBudgetSource(sources map[string]string, key string) bool {
	source := strings.TrimSpace(sources[key])
	return source != "" && source != "default"
}

func validateContextBudgetTriple(settings config.Settings) error {
	window := settings.ModelContextWindow
	threshold := settings.ContextCompactionThresholdTokens
	lead := settings.PreSubmitCompactionLeadTokens
	if window <= 0 {
		return fmt.Errorf("model_context_window must be > 0")
	}
	if threshold <= 0 {
		return fmt.Errorf("context_compaction_threshold_tokens must be > 0")
	}
	if threshold >= window {
		return fmt.Errorf("context_compaction_threshold_tokens must be < model_context_window")
	}
	minimumThreshold := compaction.MinimumThresholdTokens(window)
	if threshold < minimumThreshold {
		return fmt.Errorf("context_compaction_threshold_tokens must be >= %d (%d%% of model_context_window=%d)", minimumThreshold, compaction.MinimumWindowPercent, window)
	}
	if lead <= 0 {
		return fmt.Errorf("pre_submit_compaction_lead_tokens must be > 0")
	}
	effectivePreSubmitThreshold := compaction.EffectivePreSubmitThresholdTokens(threshold, lead)
	if effectivePreSubmitThreshold < minimumThreshold {
		return fmt.Errorf("pre_submit_compaction_lead_tokens makes the effective pre-submit threshold %d, below %d (%d%% of model_context_window=%d)", effectivePreSubmitThreshold, minimumThreshold, compaction.MinimumWindowPercent, window)
	}
	return nil
}

func validContextThreshold(window int, threshold int) bool {
	if window <= 0 || threshold <= 0 || threshold >= window {
		return false
	}
	return threshold >= compaction.MinimumThresholdTokens(window)
}

func validContextLead(window int, threshold int, lead int) bool {
	if lead <= 0 || !validContextThreshold(window, threshold) {
		return false
	}
	return compaction.EffectivePreSubmitThresholdTokens(threshold, lead) >= compaction.MinimumThresholdTokens(window)
}
