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

// ResolveInteractiveForkThinking copies only the current desired Thinking into
// a fresh interactive target. Source model locks, role and provider binding do
// not decide whether that target can retain native Thinking updates.
func ResolveInteractiveForkThinking(app config.App, meta session.Meta) (session.ForkThinking, error) {
	var thinking *string
	if meta.ChatSettings != nil {
		thinking = meta.ChatSettings.Thinking
	}
	if thinking == nil {
		current, err := ResolveReadOnlySessionContextSettings(app, meta, true)
		if err != nil {
			return session.ForkThinking{}, err
		}
		thinking = &current.Settings.ThinkingLevel
	}
	target, err := ResolveReadOnlySessionContextSettings(app, session.Meta{
		ChatSettings: &session.ChatSettingsOverrides{Thinking: thinking},
	}, false)
	if err != nil {
		return session.ForkThinking{}, err
	}
	return resolveForkThinking(target.Settings)
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
