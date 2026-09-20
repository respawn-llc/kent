package sessionlaunch

import (
	"context"
	"core/server/sessionruntime"
	"errors"
	"fmt"
	"strings"

	"core/server/launch"
	"core/server/llm"
	"core/server/session"
	"core/server/subagentpolicy"
	servicecontract "core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

type Service struct {
	planner launch.Planner
}

type PlanResult struct {
	Plan     launch.SessionPlan
	Warnings []string
}

type PlanRequest struct {
	Mode            launch.Mode
	Intent          serverapi.SessionLaunchIntent
	CallerSessionID *string
	Overrides       serverapi.RunPromptOverrides
	InitialChat     *InitialChatCreation
}

func (r PlanRequest) Validate() error {
	switch r.Mode {
	case launch.ModeInteractive, launch.ModeHeadless:
	default:
		return fmt.Errorf("Session launch mode %q is invalid", r.Mode)
	}
	if err := r.Intent.Validate(); err != nil {
		return fmt.Errorf("Session launch intent: %w", err)
	}
	if err := serverapi.ValidateOptionalIdentifier("caller_session_id", r.CallerSessionID); err != nil {
		return err
	}
	if r.InitialChat != nil {
		if err := r.InitialChat.Validate(r.Mode, r.Intent); err != nil {
			return err
		}
	}
	return r.Overrides.ValidateAgentRoleOverride()
}

func NewService(planner launch.Planner) *Service {
	return &Service{planner: planner}
}

func (s *Service) PrepareSessionChatSettingsOperation(
	ctx context.Context,
	store *session.Store,
) (PreparedChatSettingsOperationInput, error) {
	input, _, err := s.prepareSessionChatSettings(ctx, store.Meta())
	return input, err
}

func (s *Service) prepareSessionChatSettings(
	ctx context.Context,
	meta session.Meta,
) (PreparedChatSettingsOperationInput, *serverapi.ChatSettingsTaskIdentity, error) {
	planner := s.planner
	if planner.ReloadConfig != nil {
		var err error
		planner.Config, err = planner.ReloadConfig()
		if err != nil {
			return PreparedChatSettingsOperationInput{}, nil, err
		}
	}
	catalog, err := launch.PrepareSessionChatAgentCatalog(planner.Config, meta)
	if err != nil {
		return PreparedChatSettingsOperationInput{}, nil, err
	}
	raw, err := session.ChatSettingsStateFromMeta(meta)
	if err != nil {
		return PreparedChatSettingsOperationInput{}, nil, err
	}
	entry, selectedAvailable := catalog.Lookup(raw.AgentSelector())
	if !selectedAvailable {
		var defaultAvailable bool
		entry, defaultAvailable = catalog.Lookup(config.DefaultSubagentRole)
		if !defaultAvailable {
			return PreparedChatSettingsOperationInput{}, nil, errors.New("default Chat Agent baseline is missing")
		}
	}
	effective, err := session.ResolveEffectiveChatSettings(raw.Settings, nil, entry.Settings.Baseline)
	if err != nil {
		return PreparedChatSettingsOperationInput{}, nil, err
	}
	if meta.Locked != nil {
		entry.Settings, err = lockedPreparedChatSettings(*meta.Locked, entry.Settings, effective)
		if err != nil {
			return PreparedChatSettingsOperationInput{}, nil, err
		}
	}
	effective = normalizeProjectedChatSettings(effective, entry.Settings)
	persistedQuestions := effective.Questions
	var persistedThinking *string
	if !selectedAvailable {
		persistedQuestions = entry.Settings.Baseline.Questions
		persistedThinking = textutil.Value(entry.Settings.Baseline.Thinking)
	} else if raw.Settings != nil {
		if raw.Settings.Questions != nil {
			persistedQuestions = *raw.Settings.Questions
		}
		if raw.Settings.Thinking != nil {
			thinking := strings.TrimSpace(*raw.Settings.Thinking)
			if thinking == "" {
				return PreparedChatSettingsOperationInput{}, nil, errors.New("persisted Session Chat settings Thinking is required when present")
			}
			persistedThinking = &thinking
		}
	}
	taskIdentity, err := s.chatSettingsTaskIdentity(ctx, meta.SessionID)
	if err != nil {
		return PreparedChatSettingsOperationInput{}, nil, err
	}
	return PreparedChatSettingsOperationInput{
		Raw:                raw,
		Effective:          effective,
		PersistedQuestions: persistedQuestions,
		PersistedThinking:  persistedThinking,
		Catalog:            catalog,
		Locked:             meta.Locked,
		WorkflowLocked:     taskIdentity != nil,
		CompactionMode:     planner.Config.Settings.CompactionMode,
	}, taskIdentity, nil
}

func (s *Service) NewChatSettings(ctx context.Context) (*chatsettingspb.ReadSuccess, error) {
	planner := s.planner
	if planner.ReloadConfig != nil {
		snapshot, err := planner.ReloadConfig()
		if err != nil {
			return nil, err
		}
		planner.Config = snapshot
	}
	catalog, err := launch.PrepareChatAgentCatalog(planner.Config, false)
	if err != nil {
		return nil, err
	}
	projected, err := projectNewChatCatalog(catalog, planner.Config.Settings.CompactionMode)
	if err != nil {
		return nil, err
	}
	return &chatsettingspb.ReadSuccess{Target: &chatsettingspb.ReadSuccess_NewChat{NewChat: projected}}, nil
}

func (s *Service) SessionChatSettings(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) (*chatsettingspb.ReadSuccess, error) {
	record, err := s.planner.PersistedSessions.ResolvePersistedSession(ctx, sessionID.String())
	if err != nil {
		return nil, err
	}
	input, taskIdentity, err := s.prepareSessionChatSettings(ctx, *record.Meta)
	if err != nil {
		return nil, err
	}
	settings, err := ProjectChatSettings(ChatSettingsProjectionInput{
		Catalog:        input.Catalog,
		Agent:          input.Raw.AgentSelector(),
		Settings:       input.Effective,
		WorkflowLocked: input.WorkflowLocked,
		CompactionMode: input.CompactionMode,
		Locked:         input.Locked,
	})
	if err != nil {
		return nil, err
	}
	facts := &chatsettingspb.SessionFacts{
		SessionId: sessionID.String(),
	}
	if record.Meta.PreviousSessionID != nil {
		facts.PreviousSessionId = textutil.Value(record.Meta.PreviousSessionID.String())
	}
	if taskIdentity != nil {
		facts.TaskId = &taskIdentity.TaskID
		facts.TaskShortId = &taskIdentity.TaskShortID
	}
	return &chatsettingspb.ReadSuccess{
		Target: &chatsettingspb.ReadSuccess_Session{Session: &chatsettingspb.SessionSettings{Settings: settings, Session: facts}},
	}, nil
}

func (s *Service) chatSettingsTaskIdentity(
	ctx context.Context,
	sessionID string,
) (*serverapi.ChatSettingsTaskIdentity, error) {
	reader, ok := s.planner.PersistedSessions.(interface {
		ChatSettingsTaskIdentityForSession(
			context.Context,
			string,
		) (*serverapi.ChatSettingsTaskIdentity, error)
	})
	if !ok {
		return nil, errors.New("Chat Settings Task identity reader is required")
	}
	identity, err := reader.ChatSettingsTaskIdentityForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if identity == nil {
		return nil, nil
	}
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	return identity, nil
}

func (s *Service) PlanSession(
	ctx context.Context,
	req *sessionlaunchpb.SessionPlanRequest,
) (*sessionlaunchpb.SessionPlanSuccess, error) {
	internal, err := sessionPlanRequestFromGenerated(req)
	if err != nil {
		return nil, err
	}
	result, err := s.PlanLaunchSession(ctx, internal)
	if err != nil {
		return nil, err
	}
	return sessionPlanSuccessFromResult(result)
}

func (s *Service) PlanLaunchSession(ctx context.Context, req PlanRequest) (PlanResult, error) {
	if err := req.Validate(); err != nil {
		return PlanResult{}, err
	}
	var selectedSessionID *runtimeids.SessionID
	var parentAgentSessionID *runtimeids.SessionID
	switch req.Intent.Kind() {
	case serverapi.SessionLaunchIntentOpenExisting:
		sessionID, _ := req.Intent.SessionID()
		selectedSessionID = &sessionID
	case serverapi.SessionLaunchIntentCreateNew:
		origin, _ := req.Intent.CreateOrigin()
		if origin.Kind() == serverapi.SessionCreateOriginParentAgent {
			sourceID, _ := origin.SessionID()
			parentAgentSessionID = &sourceID
		}
	}
	planner := s.planner
	if planner.ReloadConfig != nil {
		snapshot, snapshotErr := planner.ReloadConfig()
		if snapshotErr != nil {
			return PlanResult{}, snapshotErr
		}
		planner.Config = snapshot
		planner.ReloadConfig = nil
	}
	var initialChat *session.ChatDraftState
	if req.InitialChat != nil {
		var initialChatErr error
		initialChat, initialChatErr = s.prepareInitialChatCreation(ctx, planner.Config, *req.InitialChat)
		if initialChatErr != nil {
			return PlanResult{}, initialChatErr
		}
	}
	roleOverride, err := req.Overrides.AgentRoleOverride()
	if err != nil {
		return PlanResult{}, err
	}
	var caller *subagentpolicy.Caller
	if req.Mode == launch.ModeHeadless {
		if req.CallerSessionID != nil {
			resolved, callerErr := launch.ResolveSessionCaller(planner.Config.PersistenceRoot, *req.CallerSessionID)
			if callerErr != nil {
				return PlanResult{}, &serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialCallerMissing}
			}
			caller = &resolved
			if parentAgentSessionID != nil {
				callerSessionID, parseErr := runtimeids.ParseSessionID(*req.CallerSessionID)
				if parseErr != nil || *parentAgentSessionID != callerSessionID {
					return PlanResult{}, &serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialInvalidTarget}
				}
			}
		}
		if parentAgentSessionID != nil && req.CallerSessionID == nil {
			if _, parentErr := launch.ResolveSessionCaller(planner.Config.PersistenceRoot, parentAgentSessionID.String()); parentErr != nil {
				return PlanResult{}, &serverapi.SubagentLaunchDeniedError{Kind: serverapi.SubagentLaunchDenialParentMissing}
			}
		}
	}
	if selectedSessionID != nil {
		return s.planExistingSession(ctx, planner, req, *selectedSessionID, roleOverride, caller)
	}
	target := subagentpolicy.TargetFromOverride(roleOverride)
	if err := subagentpolicy.Authorize(planner.Config.Settings, caller, target); err != nil {
		return PlanResult{}, err
	}
	preparation := launch.RunPromptPreparationContext{Mode: req.Mode}
	preparedOverrides, err := launch.PrepareRunPromptOverridesWithContext(planner.Config, req.Overrides, preparation)
	if err != nil {
		return PlanResult{}, err
	}
	preparedPromptFacingTarget := preparePromptFacingTarget(req.Mode, roleOverride, &preparedOverrides)
	plan, warnings, err := planner.PlanNewSessionWithPreparedOverrides(ctx, launch.SessionRequest{
		Mode:                                req.Mode,
		Intent:                              req.Intent,
		SkipContinuationAgentRoleValidation: roleOverride.Default,
		PreparedPromptFacingTarget:          preparedPromptFacingTarget,
		InitialChat:                         initialChat,
	}, req.Overrides, preparedOverrides)
	return s.finalizeLaunchPlan(ctx, plan, warnings, err)
}

func (s *Service) planExistingSession(ctx context.Context, planner launch.Planner, req PlanRequest, sessionID runtimeids.SessionID, roleOverride serverapi.RunPromptAgentRoleOverride, caller *subagentpolicy.Caller) (PlanResult, error) {
	record, err := session.ResolvePersistedSessionRecord(ctx, planner.PersistedSessions, sessionID.String())
	if err != nil {
		return PlanResult{}, err
	}
	meta := *record.Meta
	if meta.Locked != nil && roleOverride.Present {
		req.Overrides.AgentRole = nil
		roleOverride = serverapi.RunPromptAgentRoleOverride{}
	}
	if roleOverride.Present {
		if err := subagentpolicy.Authorize(planner.Config.Settings, caller, subagentpolicy.TargetFromOverride(roleOverride)); err != nil {
			return PlanResult{}, err
		}
	} else if err := authorizePersistedHeadlessRole(planner, req, caller, meta); err != nil {
		return PlanResult{}, err
	}
	preparation := launch.RunPromptPreparationContext{Mode: req.Mode, ModelLock: meta.Locked, ToolLock: meta.Locked}
	if !roleOverride.Present {
		target, err := planner.SelectedSessionPromptFacingTargetFromMeta(meta)
		if err != nil {
			return PlanResult{}, err
		}
		preparation.OmittedTarget = &target
	}
	preparedOverrides, err := launch.PrepareRunPromptOverridesWithContext(planner.Config, req.Overrides, preparation)
	if err != nil {
		return PlanResult{}, err
	}
	meta, agentSelectionResolved, activationAgentSelection, err := applyPreparedAgentChatSettings(
		planner.Config,
		roleOverride,
		preparedOverrides,
		meta,
	)
	if err != nil {
		return PlanResult{}, err
	}
	preparedPromptFacingTarget := preparePromptFacingTarget(req.Mode, roleOverride, &preparedOverrides)
	plan, warnings, err := planner.PlanPersistedSessionWithPreparedOverrides(ctx, launch.SessionRequest{
		Mode:                                req.Mode,
		Intent:                              req.Intent,
		SkipContinuationAgentRoleValidation: roleOverride.Default,
		PreparedPromptFacingTarget:          preparedPromptFacingTarget,
	}, meta, req.Overrides, preparedOverrides, launch.RunPromptOverrideOptions{
		AgentSelectionPersisted: agentSelectionResolved,
	})
	plan.ActivationAgentSelection = activationAgentSelection
	return s.finalizeLaunchPlan(ctx, plan, warnings, err)
}

func authorizePersistedHeadlessRole(
	planner launch.Planner,
	req PlanRequest,
	caller *subagentpolicy.Caller,
	meta session.Meta,
) error {
	if req.Mode != launch.ModeHeadless {
		return nil
	}
	if caller == nil {
		return nil
	}
	if meta.Continuation == nil || meta.Continuation.AgentRole == nil {
		return subagentpolicy.Authorize(planner.Config.Settings, caller, subagentpolicy.Target{Kind: subagentpolicy.TargetOmittedBase})
	}
	persistedRole := strings.TrimSpace(*meta.Continuation.AgentRole)
	lookup := config.LookupSubagentRole(planner.Config.Settings, persistedRole)
	if lookup.Status != config.SubagentRoleLookupPresent {
		if meta.Locked == nil {
			return subagentpolicy.Authorize(planner.Config.Settings, caller, subagentpolicy.Target{Kind: subagentpolicy.TargetOmittedBase})
		}
		return nil
	}
	persistedOverride, err := (serverapi.RunPromptOverrides{AgentRole: &persistedRole}).AgentRoleOverride()
	if err != nil {
		return err
	}
	return subagentpolicy.Authorize(planner.Config.Settings, caller, subagentpolicy.TargetFromOverride(persistedOverride))
}

func applyPreparedAgentChatSettings(
	app config.App,
	roleOverride serverapi.RunPromptAgentRoleOverride,
	preparedOverrides launch.PreparedRunPromptOverrides,
	meta session.Meta,
) (session.Meta, bool, *session.ChatSettingsState, error) {
	state, err := session.ChatSettingsStateFromMeta(meta)
	if err != nil {
		return session.Meta{}, false, nil, err
	}
	targetAgent := state.AgentSelector()
	selectAgent := roleOverride.Present
	if roleOverride.Present {
		targetAgent = config.DefaultSubagentRole
		if !roleOverride.Default {
			targetAgent = roleOverride.Role
		}
	} else if meta.Locked == nil && state.AgentSelector() != config.DefaultSubagentRole {
		lookup := config.LookupSubagentRole(app.Settings, state.AgentSelector())
		selectAgent = lookup.Status != config.SubagentRoleLookupPresent
		if selectAgent {
			targetAgent = config.DefaultSubagentRole
		}
	}
	if !selectAgent {
		return meta, false, nil, nil
	}
	if meta.Locked != nil {
		return meta, false, nil, nil
	}
	var prepared launch.PreparedChatSettings
	if roleOverride.Present {
		target := preparedOverrides.PromptFacingTarget()
		if target == nil {
			return session.Meta{}, false, nil, fmt.Errorf("prepared Chat Agent %q target is required", targetAgent)
		}
		if preparedOverrides.ProviderCapabilities == nil {
			return session.Meta{}, false, nil, fmt.Errorf("prepared Chat Agent %q provider capabilities are required", targetAgent)
		}
		prepared, err = launch.PrepareChatSettingsForPreparedTarget(
			*target,
			llm.SupportsFastModeProvider(*preparedOverrides.ProviderCapabilities),
		)
	} else {
		prepared, err = launch.PrepareChatSettingsForAgent(app, targetAgent)
	}
	if err != nil {
		return session.Meta{}, false, nil, err
	}
	var targetRole *string
	if preparedOverrides.NamedTarget != nil {
		targetRole = textutil.Value(preparedOverrides.NamedTarget.Selector)
	}
	target, err := session.ChatSettingsStateFromRole(targetRole, prepared.Baseline)
	if err != nil {
		return session.Meta{}, false, nil, err
	}
	target.ConnectionID = meta.ConnectionID
	if textutil.EqualOptional(target.AgentRole, state.AgentRole) {
		return meta, true, nil, nil
	}
	projected, changed, err := session.ProjectChatSettingsState(meta, target)
	if errors.Is(err, session.ErrChatAgentLocked) {
		return meta, false, nil, nil
	}
	if err != nil {
		return session.Meta{}, false, nil, err
	}
	if !changed {
		return projected, true, nil, nil
	}
	return projected, true, &target, nil
}

func preparePromptFacingTarget(
	mode launch.Mode,
	roleOverride serverapi.RunPromptAgentRoleOverride,
	preparedOverrides *launch.PreparedRunPromptOverrides,
) *launch.PreparedBaseTarget {
	if mode != launch.ModeHeadless {
		if !roleOverride.Present {
			preparedOverrides.BaseTarget = nil
		}
		return nil
	}
	return preparedOverrides.PromptFacingTarget()
}

func (s *Service) finalizeLaunchPlan(ctx context.Context, plan launch.SessionPlan, warnings []string, err error) (PlanResult, error) {
	if err != nil {
		return PlanResult{}, err
	}
	capabilities, err := llm.ResolveEffectiveProviderCapabilities(
		plan.Locked,
		plan.ActiveSettings,
	)
	if err != nil {
		return PlanResult{}, err
	}
	plan = launch.ApplyContextPolicy(plan, capabilities)
	return PlanResult{Plan: plan, Warnings: warnings}, nil
}

func sessionPlanRequestFromGenerated(request *sessionlaunchpb.SessionPlanRequest) (PlanRequest, error) {
	if request == nil {
		return PlanRequest{}, errors.New("Session plan request is required")
	}
	var mode launch.Mode
	switch request.Mode {
	case sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE:
		mode = launch.ModeInteractive
	case sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_HEADLESS:
		mode = launch.ModeHeadless
	default:
		return PlanRequest{}, fmt.Errorf("generated Session launch mode %v is invalid", request.Mode)
	}
	intent, err := protoapi.SessionLaunchIntentFromProto(request.Intent)
	if err != nil {
		return PlanRequest{}, err
	}
	overrides := serverapi.RunPromptOverrides{}
	if request.Overrides != nil {
		overrides, err = protoapi.RunPromptOverridesFromProto(request.Overrides)
		if err != nil {
			return PlanRequest{}, err
		}
	}
	initialChat, err := initialChatCreationFromGenerated(
		request.InitialChatSettings,
		request.InitialInputDraft,
	)
	if err != nil {
		return PlanRequest{}, err
	}
	internal := PlanRequest{
		Mode:            mode,
		Intent:          intent,
		CallerSessionID: textutil.Pointer(request.CallerSessionId),
		Overrides:       overrides,
		InitialChat:     initialChat,
	}
	return internal, internal.Validate()
}

func sessionPlanSuccessFromResult(result PlanResult) (*sessionlaunchpb.SessionPlanSuccess, error) {
	settings, err := protoapi.SessionSettingsToProto(result.Plan.ActiveSettings)
	if err != nil {
		return nil, err
	}
	source, err := protoapi.SessionSourceReportToProto(result.Plan.Source)
	if err != nil {
		return nil, err
	}
	enabledToolIDs := make([]sessionlaunchpb.ToolID, 0, len(result.Plan.EnabledTools))
	for _, id := range result.Plan.EnabledTools {
		generated, err := protoapi.SessionToolIDToProto(id)
		if err != nil {
			return nil, err
		}
		enabledToolIDs = append(enabledToolIDs, generated)
	}
	plan := &sessionlaunchpb.SessionPlan{
		SessionId:                result.Plan.Descriptor.SessionID().String(),
		ActiveSettings:           settings,
		EnabledToolIds:           enabledToolIDs,
		SessionName:              textutil.Pointer(result.Plan.SessionName),
		ModelContractLocked:      result.Plan.ModelContractLocked,
		QuestionsEnabled:         result.Plan.QuestionsEnabled,
		AutoCompactionEnabled:    result.Plan.AutoCompactionEnabled,
		ThinkingOverrideExplicit: result.Plan.ThinkingOverrideExplicit,
		Source:                   source,
	}
	selection, err := sessionruntime.AgentSelectionFromState(result.Plan.ActivationAgentSelection)
	if err != nil {
		return nil, err
	}
	plan.ActivationAgentSelection = protoapi.SessionRuntimeAgentSelectionToProto(selection)
	plan.ExplicitToolSelection, err = protoapi.ToolSelectionToProto(result.Plan.ExplicitToolSelection)
	if err != nil {
		return nil, err
	}
	if result.Plan.ConfiguredModelName != "" {
		plan.ConfiguredModelName = &result.Plan.ConfiguredModelName
	}
	success := &sessionlaunchpb.SessionPlanSuccess{
		Plan:     plan,
		Warnings: append([]string(nil), result.Warnings...),
	}
	if err := protoapi.Validate(success); err != nil {
		return nil, err
	}
	return success, nil
}

var _ servicecontract.SessionLaunchService = (*Service)(nil)
