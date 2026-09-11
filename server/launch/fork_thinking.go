package launch

import (
	"context"

	"core/server/auth"
	"core/server/llm"
	"core/server/session"
	"core/shared/config"
)

// ResolveForkThinking uses the configuration of the child's first request.
// Compact-and-Continue clones first compact with their outgoing configuration.
func ResolveForkThinking(ctx context.Context, app config.App, meta session.Meta, authManager *auth.Manager, skipContinuationAgentRoleValidation bool) (session.ForkThinking, error) {
	current, err := ResolveReadOnlySessionContextSettings(app, meta, skipContinuationAgentRoleValidation)
	if err != nil {
		return session.ForkThinking{}, err
	}
	state := auth.EmptyState()
	if authManager != nil {
		state, err = authManager.Load(ctx)
		if err != nil {
			return session.ForkThinking{}, err
		}
	}
	caps, err := llm.ResolveRuntimeProviderCapabilities(state, current.Settings)
	if err != nil {
		return session.ForkThinking{}, err
	}
	return session.ForkThinking{
		Desired:               current.Settings.ThinkingLevel,
		PreserveNativeUpdates: llm.SupportsNativeThinkingUpdates(current.Settings.Model, caps),
	}, nil
}
