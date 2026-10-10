package sessionlaunch

import (
	"context"
	"errors"
	"strings"

	"core/server/launch"
	"core/server/llm"
	"core/server/session"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/serverapi"
	"core/shared/textutil"
)

type runSelection struct {
	request  PlanRequest
	input    PreparedChatSettingsOperationInput
	result   PreparedChatSettingsOperationResult
	prepared launch.PreparedRunPromptOverrides
}

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

	var selection runSelection
	result, err := s.applyChatSettings(ctx, sessionID, operation, func(ctx context.Context, store *session.Store) (PreparedChatSettingsOperationInput, PreparedChatSettingsOperationResult, error) {
		var err error
		selection, err = s.prepareRunSelection(ctx, store, req, operation)
		return selection.input, selection.result, err
	})
	if err != nil {
		return PlanRequest{}, err
	}
	if rejected := result.GetRejected(); rejected != nil {
		return PlanRequest{}, &serverapi.RunSelectionRejectedError{Reason: rejected.Reason}
	}

	req = selection.request
	req.preparedSelection = &selection.prepared
	req.Overrides.AgentRole = nil
	return req, nil
}

func (s *Service) prepareRunSelection(
	ctx context.Context,
	store *session.Store,
	req PlanRequest,
	operation *chatsettingspb.MutationOperation,
) (runSelection, error) {
	planner := s.planner
	if planner.ReloadConfig != nil {
		snapshot, err := planner.ReloadConfig()
		if err != nil {
			return runSelection{}, err
		}
		planner.Config, planner.ReloadConfig = snapshot, nil
	}

	meta := store.Meta()
	input, _, err := (&Service{planner: planner}).prepareSessionChatSettings(ctx, meta)
	if err != nil {
		return runSelection{}, err
	}

	req = ignoreLockedRunSelection(req, meta)
	role, err := req.Overrides.AgentRoleOverride()
	if err != nil {
		return runSelection{}, err
	}
	if req.CallerSessionID != nil {
		_, err := launch.ResolveSessionCaller(planner.Config.PersistenceRoot, *req.CallerSessionID)
		if err != nil {
			if errors.Is(err, session.ErrSessionNotFound) {
				return runSelection{}, &serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing}
			}
			return runSelection{}, err
		}
	}
	baselineMeta := meta
	baselineMeta.ChatSettings = nil
	prepared, role, err := prepareExistingSelection(planner, req, baselineMeta, role)
	if err != nil {
		return runSelection{}, err
	}
	target := prepared.PromptFacingTarget()
	if target == nil || prepared.ProviderCapabilities == nil {
		return runSelection{}, errors.New("prepared Run selection is required")
	}
	settings, err := launch.PrepareChatSettingsForPreparedTarget(
		*target,
		llm.SupportsFastModeProvider(*prepared.ProviderCapabilities),
	)
	if err != nil {
		return runSelection{}, err
	}
	selectedMeta, _, _, err := applyPreparedAgentChatSettings(planner.Config, role, prepared, meta)
	if err != nil {
		return runSelection{}, err
	}
	selection, err := resolveRunSettings(req, input, selectedMeta, settings, operation)
	if err != nil {
		return runSelection{}, err
	}
	return runSelection{
		request: req, input: input, prepared: prepared,
		result: selection,
	}, nil
}

func resolveRunSettings(
	req PlanRequest,
	input PreparedChatSettingsOperationInput,
	selectedMeta session.Meta,
	settings launch.PreparedChatSettings,
	operation *chatsettingspb.MutationOperation,
) (PreparedChatSettingsOperationResult, error) {
	state, err := session.ChatSettingsStateFromMeta(selectedMeta)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	effective, err := session.ResolveEffectiveChatSettings(state.Settings, nil, settings.Baseline)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	if !textutil.EqualOptional(state.AgentRole, input.Raw.AgentRole) ||
		(input.Locked == nil && strings.TrimSpace(req.Overrides.Model) != "") {
		// New selections use their own capability baseline, not the previous model's saved value.
		effective.Thinking = settings.Baseline.Thinking
	}
	targetState, err := session.ChatSettingsStateFromRole(state.AgentRole, effective)
	if err != nil {
		return PreparedChatSettingsOperationResult{}, err
	}
	targetState.ConnectionID = state.ConnectionID

	return ProjectResolvedChatSettingsOperation(ResolvedChatSettingsOperation{
		ChatSettingsMutationContext: input.ChatSettingsMutationContext,
		Selection:                   ResolvedChatSettingsSelection{State: targetState, Settings: settings},
		Edit:                        operation,
	})
}

func ignoreLockedRunSelection(req PlanRequest, meta session.Meta) PlanRequest {
	if req.Mode != launch.ModeHeadless || meta.Locked == nil {
		return req
	}
	if req.Overrides.AgentRole != nil {
		req.selectionWarnings = append(req.selectionWarnings,
			runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_AGENT_IGNORED_TO_PRESERVE_CACHE)
		req.Overrides.AgentRole = nil
	}
	if strings.TrimSpace(req.Overrides.Model) != "" {
		req.selectionWarnings = append(req.selectionWarnings,
			runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_MODEL_IGNORED_TO_PRESERVE_CACHE)
		req.Overrides.Model = ""
	}
	return req
}
