package app

import worktreepb "core/shared/protoapi/gen/kent/api/worktree"

import (
	"context"
	"errors"
	"os"
	"strings"

	"core/cli/app/commands"
	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/lifecyclecontract"
	"core/shared/protoapi"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type sessionLifecycleClientProvider interface {
	SessionLifecycleClient() apicontract.SessionLifecycleService
}

type sessionTransitionServer interface {
	sessionLifecycleClientProvider
	Reauthenticate(ctx context.Context) error
}

type sessionReattachServer interface {
	sessionLifecycleClientProvider
	ReattachSession(context.Context, string) error
}

type promptCommandCatalogServer interface {
	PromptCommandCatalogClient(context.Context, string, *worktreepb.SessionExecutionTarget) (apicontract.PromptCommandCatalogService, error)
}

type interactiveSessionServer interface {
	Close() error
	launchPlannerServer
	sessionTransitionServer
	EnsureAuthReady(ctx context.Context, settings config.Settings, interactor authInteractor) error
	EnsureConnectionSetup(ctx context.Context) error
	BindProjectWorkspace(ctx context.Context, projectID string, workspaceID string) (interactiveSessionServer, error)
}

type sessionLifecycleOptions struct {
	Intent              *sessionlaunchpb.SessionLaunchRequestIntent
	Overrides           serverapi.RunPromptOverrides
	InvocationOverrides serverapi.RunPromptOverrides
}

func runSessionLifecycleWithOptions(ctx context.Context, server interactiveSessionServer, interactor authInteractor, opts sessionLifecycleOptions) error {
	clientSettings := clientSettingsForInteractiveServer(server)
	originalServer := server
	boundServer := server
	var err error
	if _, attached := server.ProjectBinding(); !attached {
		boundServer, err = ensureInteractiveProjectBinding(ctx, server)
		if err != nil {
			return err
		}
	}
	if shouldCloseReboundServer(originalServer, boundServer) {
		defer func() { _ = boundServer.Close() }()
	}
	server = boundServer
	planner := newSessionLaunchPlanner(server)
	next := &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_SelectSession{
		SelectSession: &sessionlaunchpb.SessionSelectDirective{Auth: sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH},
	}}
	nextSessionOverrides := opts.Overrides
	var pickerNotice *startupPickerNotice
	for {
		intent := opts.Intent
		preparation := defaultSessionLaunchPreparation()
		opts.Intent = nil
		if intent == nil {
			switch next.Directive.(type) {
			case *sessionlaunchpb.SessionDirective_Stop:
				return nil
			case *sessionlaunchpb.SessionDirective_SelectSession:
				picked, err := planner.selectSession(ctx, pickerNotice)
				if err != nil {
					return err
				}
				pickerNotice = nil
				switch picked := picked.(type) {
				case sessionPickerCancelResult:
					next = &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_Stop{Stop: &emptypb.Empty{}}}
				case sessionPickerCreateResult:
					next, err = defaultSessionLaunchDirective(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
					if err != nil {
						return err
					}
				case sessionPickerOpenResult:
					sessionID := picked.sessionID
					executionTarget, err := loadSelectedSessionExecutionTarget(ctx, server.SessionViewClient(), sessionID.String())
					if err != nil {
						pickerNotice = &startupPickerNotice{
							Text:       "Could not inspect the selected session. Try again.",
							Kind:       startupPickerNoticeError,
							Diagnostic: err,
						}
						continue
					}
					workspaceChangeAction, err := maybeHandlePickedSessionWorkspaceChange(ctx, server, sessionID.String(), executionTarget)
					if err != nil {
						return err
					}
					if workspaceChangeAction == sessionWorkspaceChangePickAgain {
						continue
					}
					next, err = defaultSessionLaunchDirective(serverapi.OpenExistingSessionLaunchIntent(sessionID))
					if err != nil {
						return err
					}
				default:
					return errors.New("session picker returned an invalid result")
				}
				continue
			case *sessionlaunchpb.SessionDirective_Launch:
			default:
				return errors.New("session lifecycle returned no result")
			}

			canonicalIntent := protoapi.SessionLaunchIntentFromProto(next.GetLaunch().Intent)
			intent, err = protoapi.SessionLaunchRequestIntentToProto(canonicalIntent)
			if err != nil {
				return err
			}
			preparation = next.GetLaunch().Preparation
		}
		reboundServer, rebound, err := bindNavigationSessionContext(ctx, server, preparation)
		if err != nil {
			return err
		}
		if rebound {
			if shouldCloseReboundServer(server, reboundServer) {
				defer func() { _ = reboundServer.Close() }()
			}
			server = reboundServer
			planner = newSessionLaunchPlanner(server)
		}
		launchRequest := sessionLaunchRequest{
			Mode:      launchModeInteractive,
			Intent:    intent,
			Overrides: mergeSessionPlanOverrides(opts.InvocationOverrides, nextSessionOverrides),
		}
		if err := server.EnsureConnectionSetup(ctx); err != nil {
			if errors.Is(err, ErrAuthCanceledByUser) {
				return nil
			}
			return err
		}
		plan, err := planner.PlanSession(ctx, launchRequest)
		if err != nil {
			return err
		}
		if err := server.EnsureAuthReady(ctx, plan.ActiveSettings, interactor); err != nil {
			return err
		}
		plan.ClientLifecycleCommand = clientSettings.Hooks.LifecycleCommand()
		switch intent.Intent.(type) {
		case *sessionlaunchpb.SessionLaunchRequestIntent_CreateNew:
			plan.ClientLifecycleOpeningKind = lifecyclecontract.OpeningKindNew
		case *sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId:
			plan.ClientLifecycleOpeningKind = lifecyclecontract.OpeningKindResumed
		}
		nextSessionOverrides = serverapi.RunPromptOverrides{}
		initialPrompt, initialPromptHistoryRecorded, transitionInput, overrideStoredDraft, err := sessionLaunchPreparationValues(preparation)
		if err != nil {
			return err
		}
		runtimePlan, request, err := prepareSessionUIRun(
			ctx,
			server,
			planner,
			plan,
			initialPrompt,
			initialPromptHistoryRecorded,
			transitionInput,
			overrideStoredDraft,
		)
		if err != nil {
			return err
		}
		finalModel, runErr := runUILoop(request)
		if runErr != nil {
			runtimePlan.Close()
			return runErr
		}
		transition := extractUITransition(finalModel)
		if transition.SessionRetargeted {
			reattacher, ok := server.(sessionReattachServer)
			if !ok {
				runtimePlan.Close()
				return errors.New("Session rebind requires a Remote server")
			}
			next, err = reopenRetargetedSession(ctx, reattacher, runtimePlan, plan.SessionID)
			if err != nil {
				return err
			}
			continue
		}
		if transition.Exit {
			runtimePlan.Close()
			return nil
		}
		resolved, err := resolveAndReleaseSessionAction(ctx, server, interactor, plan.SessionID, transition, runtimePlan, finalModel)
		if err != nil {
			return err
		}
		next = resolved
	}
}

func reopenRetargetedSession(
	ctx context.Context,
	server sessionReattachServer,
	runtimePlan *runtimeLaunchPlan,
	rawSessionID string,
) (*sessionlaunchpb.SessionDirective, error) {
	defer runtimePlan.Close()
	sessionID, err := runtimeids.ParseSessionID(rawSessionID)
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	runtimePlan.Close()
	if err := server.ReattachSession(ctx, rawSessionID); err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	return defaultSessionLaunchDirective(serverapi.OpenExistingSessionLaunchIntent(sessionID))
}

func defaultSessionLaunchDirective(intent serverapi.SessionLaunchIntent) (*sessionlaunchpb.SessionDirective, error) {
	return protoapi.SessionLaunchDirectiveToProto(intent, defaultSessionLaunchPreparation())
}

func defaultSessionLaunchPreparation() *sessionlaunchpb.SessionLaunchPreparation {
	return &sessionlaunchpb.SessionLaunchPreparation{
		Auth:        sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_KEEP_CURRENT_AUTH,
		InputPolicy: &sessionlaunchpb.SessionDraftDisposition{Disposition: &sessionlaunchpb.SessionDraftDisposition_RestoreStoredDraft{RestoreStoredDraft: &emptypb.Empty{}}},
	}
}

func bindNavigationSessionContext(ctx context.Context, server interactiveSessionServer, preparation *sessionlaunchpb.SessionLaunchPreparation) (interactiveSessionServer, bool, error) {
	binding := preparation.NavigationBinding
	if binding == nil {
		return server, false, nil
	}
	rebound, err := server.BindProjectWorkspace(ctx, binding.ProjectId, binding.WorkspaceId)
	if err != nil {
		return nil, false, err
	}
	return rebound, true, nil
}

func resolveAndReleaseSessionAction(
	ctx context.Context,
	server sessionTransitionServer,
	interactor authInteractor,
	sessionID string,
	transition UITransition,
	runtimePlan *runtimeLaunchPlan,
	finalModel any,
) (*sessionlaunchpb.SessionDirective, error) {
	resolved, err := resolveSessionAction(ctx, server, interactor, sessionID, transition)
	runtimePlan.Close()
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	return resolved, nil
}

func prepareSessionUIRun(
	ctx context.Context,
	server interactiveSessionServer,
	planner *launchPlanner,
	plan sessionLaunchPlan,
	initialPrompt string,
	initialPromptHistoryRecorded bool,
	transitionInput string,
	overrideStoredDraft bool,
) (*runtimeLaunchPlan, uiLoopRequest, error) {
	id, err := runtimeids.ParseSessionID(plan.SessionID)
	if err != nil {
		return nil, uiLoopRequest{}, err
	}
	if _, err := server.SessionLifecycleClient().Promote(ctx, &sessionlaunchpb.SessionPromoteRequest{SessionId: protoapi.SessionSelectorToProto(id)}); err != nil {
		return nil, uiLoopRequest{}, err
	}
	runtimePlan, err := planner.PrepareRuntime(ctx, plan, os.Stderr, "app.start session_id="+plan.SessionID+" workspace="+plan.ExecutionTarget.EffectiveWorkdir+" model="+plan.ActiveSettings.Model)
	if err != nil {
		return nil, uiLoopRequest{}, err
	}
	commandRegistry := commands.NewDefaultRegistry()
	var catalogStatus *string
	var promptCatalog apicontract.PromptCommandCatalogService
	var catalogEntries []commands.PromptCommandCatalogEntry
	if catalogServer, ok := server.(promptCommandCatalogServer); ok {
		catalogClient, catalogErr := catalogServer.PromptCommandCatalogClient(ctx, plan.SessionID, plan.ExecutionTarget)
		if catalogErr != nil {
			notice := "Custom prompt commands are unavailable for this session."
			catalogStatus = &notice
		} else if catalogClient != nil {
			promptCatalog = catalogClient
			response, catalogErr := catalogClient.GetPromptCommandCatalog(ctx, &promptcommandpb.GetCatalogRequest{})
			if catalogErr == nil {
				entries := promptCatalogSnapshot(response)
				catalogEntries = entries
				commandRegistry = commands.NewDefaultRegistryWithPromptCatalog(entries)
			}
			if catalogErr != nil {
				notice := "Custom prompt commands are unavailable for this session."
				catalogStatus = &notice
			}
		}
	}
	initialState, err := sessionLaunchInitialStateFromServer(
		ctx,
		server,
		plan.SessionID,
		transitionInput,
		overrideStoredDraft,
	)
	if err != nil {
		runtimePlan.Close()
		return nil, uiLoopRequest{}, err
	}
	return runtimePlan, uiLoopRequest{
		sessionLifecycle:             server.SessionLifecycleClient(),
		ctx:                          ctx,
		wiring:                       runtimePlan.Wiring,
		projectID:                    strings.TrimSpace(server.ProjectID()),
		active:                       plan.ActiveSettings,
		commandRegistry:              commandRegistry,
		initialPrompt:                initialPrompt,
		initialPromptHistoryRecorded: initialPromptHistoryRecorded,
		initialInput:                 initialState.Input,
		sessionTitle:                 textutil.Pointer(plan.SessionTitle),
		modelContractLocked:          plan.ModelContractLocked,
		configuredModelName:          plan.ConfiguredModelName,
		statusConfig:                 plan.StatusConfig,
		initialTransientStatus:       catalogStatus,
		promptCatalog:                promptCatalog,
		promptCatalogEntries:         catalogEntries,
	}, nil
}

func shouldCloseReboundServer(original, rebound interactiveSessionServer) bool {
	return original != nil && rebound != nil && original != rebound
}

type sessionLaunchInitialState struct {
	Input string
}

func sessionLaunchInitialStateFromServer(
	ctx context.Context,
	server sessionLifecycleClientProvider,
	sessionID string,
	transitionInput string,
	overrideStoredDraft bool,
) (sessionLaunchInitialState, error) {
	if server == nil || server.SessionLifecycleClient() == nil {
		return sessionLaunchInitialState{}, errors.New("session lifecycle client is required")
	}
	var selectedSessionID *string
	if normalized := strings.TrimSpace(sessionID); normalized != "" {
		selectedSessionID = &normalized
	}
	resp, err := server.SessionLifecycleClient().GetInitialInput(ctx, &sessionlaunchpb.SessionInitialInputRequest{
		SessionId:           selectedSessionID,
		TransitionInput:     transitionInput,
		OverrideStoredDraft: overrideStoredDraft,
	})
	if err != nil {
		return sessionLaunchInitialState{}, err
	}
	return sessionLaunchInitialState{Input: resp.Input}, nil
}

func sessionLaunchPreparationValues(preparation *sessionlaunchpb.SessionLaunchPreparation) (string, bool, string, bool, error) {
	initialPrompt := ""
	initialPromptHistoryRecorded := false
	if prompt := preparation.InitialPrompt; prompt != nil {
		initialPrompt = prompt.Text
		initialPromptHistoryRecorded = prompt.HistoryRecorded
	}
	switch disposition := preparation.GetInputPolicy().GetDisposition().(type) {
	case *sessionlaunchpb.SessionDraftDisposition_RestoreStoredDraft:
		return initialPrompt, initialPromptHistoryRecorded, "", false, nil
	case *sessionlaunchpb.SessionDraftDisposition_OverrideStoredDraft:
		return initialPrompt, initialPromptHistoryRecorded, disposition.OverrideStoredDraft, true, nil
	default:
		return "", false, "", false, errors.New("session draft disposition kind is invalid")
	}
}

func lifecycleResultAuthPreparation(result *sessionlaunchpb.SessionDirective) (*sessionlaunchpb.SessionAuthPreparation, error) {
	switch directive := result.GetDirective().(type) {
	case *sessionlaunchpb.SessionDirective_Stop:
		return nil, nil
	case *sessionlaunchpb.SessionDirective_SelectSession:
		return &directive.SelectSession.Auth, nil
	case *sessionlaunchpb.SessionDirective_Launch:
		return &directive.Launch.Preparation.Auth, nil
	default:
		return nil, errors.New("session directive kind is invalid")
	}
}

func resolveSessionAction(ctx context.Context, server sessionTransitionServer, interactor authInteractor, sessionID string, transition UITransition) (*sessionlaunchpb.SessionDirective, error) {
	if transition.Exit {
		return &sessionlaunchpb.SessionDirective{Directive: &sessionlaunchpb.SessionDirective_Stop{Stop: &emptypb.Empty{}}}, nil
	}
	if server == nil || server.SessionLifecycleClient() == nil {
		return &sessionlaunchpb.SessionDirective{}, errors.New("session lifecycle client is required")
	}
	action, err := transition.Action.transitionAction()
	if err != nil {
		return nil, err
	}
	var currentSessionID, previousSessionID, targetSessionID, forkTargetID *string
	if normalized := strings.TrimSpace(sessionID); normalized != "" {
		currentSessionID = &normalized
	}
	if transition.PreviousSessionID != nil {
		previousSessionID = proto.String(transition.PreviousSessionID.String())
	}
	if transition.TargetSessionID != "" {
		targetSessionID = &transition.TargetSessionID
	}
	if transition.ForkRollbackTargetID != "" {
		forkTargetID = &transition.ForkRollbackTargetID
	}
	resolved, err := server.SessionLifecycleClient().ResolveTransition(ctx, &sessionlaunchpb.SessionResolveTransitionRequest{
		SessionId: currentSessionID,
		Transition: &sessionlaunchpb.SessionTransition{
			Action:                       action,
			InitialPrompt:                transition.InitialPrompt,
			InitialPromptHistoryRecorded: transition.InitialPromptHistoryRecorded,
			InitialInput:                 sessionTransitionInitialInput(transition),
			TargetSessionId:              targetSessionID,
			ForkRollbackTargetId:         forkTargetID,
			PreviousSessionId:            previousSessionID,
		},
	})
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	authPreparation, err := lifecycleResultAuthPreparation(resolved)
	if err != nil {
		return &sessionlaunchpb.SessionDirective{}, err
	}
	if authPreparation != nil && *authPreparation == sessionlaunchpb.SessionAuthPreparation_SESSION_AUTH_PREPARATION_REAUTHENTICATE {
		if err := server.Reauthenticate(ctx); err != nil {
			return &sessionlaunchpb.SessionDirective{}, err
		}
	}
	return resolved, nil
}

func sessionTransitionInitialInput(transition UITransition) *string {
	switch transition.Action {
	case UIActionForkRollback, UIActionOpenSession:
		return textutil.Pointer(transition.InitialInput)
	default:
		return nil
	}
}
