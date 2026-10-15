package launch

import (
	"core/server/llm"
	"core/server/session"
	"core/shared/config"
)

// ResolveForkThinking retains the source contract for workflow continuation.
// Compact-and-Continue clones first compact with their outgoing configuration.
func ResolveForkThinking(app config.App, meta session.Meta, skipContinuationAgentRoleValidation bool) (session.ForkThinking, error) {
	current, err := ResolveReadOnlySessionContextSettings(app, meta, skipContinuationAgentRoleValidation)
	if err != nil {
		return session.ForkThinking{}, err
	}
	return resolveForkThinking(current.Settings)
}

type InteractiveForkTarget struct {
	ConnectionID config.ConnectionID
	Thinking     session.ForkThinking
}

// PrepareInteractiveFork allocates the child's fresh binding before projecting
// copied Thinking updates. The source contract does not select that binding.
func PrepareInteractiveFork(app config.App, meta session.Meta, rotation *ConnectionRotation) (InteractiveForkTarget, error) {
	var thinking *string
	if meta.ChatSettings != nil {
		thinking = meta.ChatSettings.Thinking
	}
	if thinking == nil {
		current, err := ResolveReadOnlySessionContextSettings(app, meta, true)
		if err != nil {
			return InteractiveForkTarget{}, err
		}
		thinking = &current.Settings.ThinkingLevel
	}
	settings, _, _, _, err := resolveSessionContextSettings(app, session.Meta{
		ChatSettings: &session.ChatSettingsOverrides{Thinking: thinking},
	}, false, rotation, true)
	if err != nil {
		return InteractiveForkTarget{}, err
	}
	targetThinking, err := resolveForkThinking(settings)
	if err != nil {
		return InteractiveForkTarget{}, err
	}
	connection, err := settings.Connection.ConcreteID()
	if err != nil {
		return InteractiveForkTarget{}, err
	}
	return InteractiveForkTarget{ConnectionID: *connection, Thinking: targetThinking}, nil
}

func resolveForkThinking(settings config.Settings) (session.ForkThinking, error) {
	caps, err := llm.ResolveRuntimeProviderCapabilities(settings)
	if err != nil {
		return session.ForkThinking{}, err
	}
	return session.ForkThinking{
		Desired:               settings.ThinkingLevel,
		PreserveNativeUpdates: llm.SupportsNativeThinkingUpdates(settings.Model, caps),
	}, nil
}
