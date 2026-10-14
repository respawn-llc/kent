package llm

import (
	"fmt"

	"core/shared/config"
	"core/shared/textutil"
)

type NanoTarget struct {
	Settings             config.Settings
	Capabilities         ProviderCapabilities
	Estimator            TokenEstimator
	SupportsVisionInputs bool
}

// ResolveNanoTargets validates every member without allocating rotation or auth.
func ResolveNanoTargets(settings config.Settings) ([]NanoTarget, error) {
	n := settings.Nano
	if n == nil {
		return nil, nil
	}
	if err := config.ValidateSettingsWithSources(settings, nil); err != nil {
		return nil, err
	}
	selected := settings
	selected.Connection = n.Connection
	members, err := selected.ConnectionMembers()
	if err != nil {
		return nil, err
	}
	targets := make([]NanoTarget, 0, len(members))
	for _, id := range members {
		connection := settings.Connections[id]
		variant, err := ResolveConnectionVariant(connection)
		if err != nil {
			return nil, err
		}
		caps, err := ResolveConnectionCapabilities(connection)
		if err != nil {
			return nil, err
		}
		estimator, err := ResolveConnectionTokenEstimator(connection)
		if err != nil {
			return nil, err
		}
		window := n.ModelContextWindow
		if window == nil {
			if contract, known := LookupModelCapabilityContract(*n.Model); known {
				window = textutil.Value(contract.ContextMetadata(variant.Variant.ProviderID).ContextWindowTokens)
			}
		}
		if window == nil || *window < 2500 {
			return nil, fmt.Errorf("nano model %q on connection %q requires a known context window or an override of at least 2500 tokens", *n.Model, id)
		}
		modelCaps := LockedModelCapabilitiesForModel(*n.Model, caps)
		if n.SupportsReasoningEffort != nil {
			modelCaps.SupportsReasoningEffort = *n.SupportsReasoningEffort
		}
		if n.SupportsVisionInputs != nil {
			modelCaps.SupportsVisionInputs = *n.SupportsVisionInputs
		}
		active := config.Settings{
			Connection: config.SingleConnection(id), Connections: settings.Connections,
			Model: *n.Model, ModelContextWindow: *window,
			ThinkingLevel:      "medium",
			Timeouts:           config.Timeouts{ModelRequestSeconds: n.TimeoutSeconds},
			ProviderIdentifier: config.DefaultAppName,
			ModelCapabilities: config.ModelCapabilitiesOverride{
				SupportsReasoningEffort: modelCaps.SupportsReasoningEffort,
				SupportsVisionInputs:    modelCaps.SupportsVisionInputs,
			},
		}
		if n.ThinkingLevel != nil {
			if err := ValidateModelReasoningEffort(*n.Model, *n.ThinkingLevel); err != nil {
				return nil, err
			}
			active.ThinkingLevel = *n.ThinkingLevel
		} else if contract, known := LookupModelCapabilityContract(*n.Model); known && contract.DefaultReasoningEffort != nil {
			active.ThinkingLevel = *contract.DefaultReasoningEffort
		}
		if n.ModelVerbosity != nil {
			active.ModelVerbosity = *n.ModelVerbosity
		}
		targets = append(targets, NanoTarget{Settings: active, Capabilities: caps, Estimator: estimator, SupportsVisionInputs: modelCaps.SupportsVisionInputs})
	}
	return targets, nil
}
