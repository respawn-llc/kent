package sessionlaunch

import (
	"context"
	"errors"
	"strings"

	"core/server/launch"
	"core/server/llm"
	"core/server/session"
	"core/server/subagentpolicy"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"
	"core/shared/textutil"
)

// SaveRunSelection prepares and saves the existing Session's selection without
// admitting continuation or retaining a Store or Runtime in the returned plan.
func (s *Service) SaveRunSelection(ctx context.Context, req PlanRequest) (PlanRequest, error) {
	if err := req.Validate(); err != nil {
		return PlanRequest{}, err
	}
	sessionID, existing := req.Intent.SessionID()
	if !existing {
		return PlanRequest{}, errors.New("Run selection requires an existing Session")
	}
	operation := &chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: req.Overrides.ThinkingLevel},
	}
	req.Overrides.ThinkingLevel = ""
	result, err := s.applyChatSettings(ctx, sessionID, operation, func(ctx context.Context, store *session.Store) (PreparedChatSettingsOperationInput, PreparedChatSettingsOperationResult, error) {
		planner := s.planner
		if planner.ReloadConfig != nil {
			snapshot, err := planner.ReloadConfig()
			if err != nil {
				return PreparedChatSettingsOperationInput{}, PreparedChatSettingsOperationResult{}, err
			}
			planner.Config, planner.ReloadConfig = snapshot, nil
		}
		input, _, err := (&Service{planner: planner}).prepareSessionChatSettings(ctx, store.Meta())
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		role, err := req.Overrides.AgentRoleOverride()
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		var caller *subagentpolicy.Caller
		if req.CallerSessionID != nil {
			resolved, err := launch.ResolveSessionCaller(planner.Config.PersistenceRoot, *req.CallerSessionID)
			if err != nil {
				return input, PreparedChatSettingsOperationResult{}, &serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing}
			}
			caller = &resolved
		}
		// The requested value must never become its own capability baseline.
		baselineMeta := store.Meta()
		baselineMeta.ChatSettings = nil
		prepared, role, err := prepareExistingSelection(planner, req, baselineMeta, role, caller)
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		target := prepared.PromptFacingTarget()
		if target == nil || prepared.ProviderCapabilities == nil {
			return input, PreparedChatSettingsOperationResult{}, errors.New("prepared Run selection is required")
		}
		settings, err := launch.PrepareChatSettingsForPreparedTarget(*target, llm.SupportsFastModeProvider(*prepared.ProviderCapabilities))
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		selectedMeta, _, _, err := applyPreparedAgentChatSettings(planner.Config, role, prepared, store.Meta())
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		state, err := session.ChatSettingsStateFromMeta(selectedMeta)
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		effective, err := session.ResolveEffectiveChatSettings(state.Settings, nil, settings.Baseline)
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		if !textutil.EqualOptional(state.AgentRole, input.Raw.AgentRole) ||
			(input.Locked == nil && strings.TrimSpace(req.Overrides.Model) != "") {
			// New selections validate against their own configured capability,
			// independently of the old model's saved custom effort.
			effective.Thinking = settings.Baseline.Thinking
		}
		targetState, err := session.ChatSettingsStateFromRole(state.AgentRole, effective)
		if err != nil {
			return input, PreparedChatSettingsOperationResult{}, err
		}
		targetState.ConnectionID = state.ConnectionID
		projected, err := ProjectResolvedChatSettingsOperation(ResolvedChatSettingsOperation{
			ChatSettingsMutationContext: input.ChatSettingsMutationContext,
			Selection:                   ResolvedChatSettingsSelection{State: targetState, Settings: settings},
			Edit:                        operation,
		})
		if err != nil || projected.Rejection != nil {
			return input, projected, err
		}
		req.preparedSelection = &prepared
		return input, projected, nil
	})
	if err != nil {
		return PlanRequest{}, err
	}
	if rejected := result.GetRejected(); rejected != nil {
		return PlanRequest{}, &serverapi.RunSelectionRejectedError{Reason: rejected.Reason}
	}
	req.Overrides.AgentRole = nil
	return req, nil
}
