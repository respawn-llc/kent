package app

import (
	"context"
	"errors"

	"core/shared/config"
	"core/shared/lifecyclecontract"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func prepareNewSessionUIRun(ctx context.Context, server interactiveSessionServer, origin *sessionlaunchpb.SessionCreateRequestOrigin, invocation serverapi.RunPromptOverrides) (uiLoopRequest, error) {
	if previous := origin.GetPreviousSessionId(); previous != nil {
		origin = &sessionlaunchpb.SessionCreateRequestOrigin{Origin: &sessionlaunchpb.SessionCreateRequestOrigin_NavigationOnlySessionId{NavigationOnlySessionId: previous}}
	}
	binding, present := server.ProjectBinding()
	if !present {
		return uiLoopRequest{}, errors.New("New Session requires a Project workspace binding")
	}
	options, err := protoapi.RunPromptOverridesToProto(invocation)
	if err != nil {
		return uiLoopRequest{}, err
	}
	source, ok := server.(sessionUISource)
	if !ok {
		return uiLoopRequest{}, errors.New("Session UI services are required")
	}
	clients := source.SessionUIClients()
	return uiLoopRequest{
		ctx:         ctx,
		newChatAuth: server.AuthBootstrapClient(),
		createSession: func(ctx context.Context, target *chatpb.NewChatTarget, draft string) (*chatpb.ExistingSessionTarget, error) {
			response, err := server.SessionLaunchClient().PlanSession(ctx, protoapi.NewChatSessionPlanRequest(target, &draft))
			if err != nil {
				return nil, err
			}
			return &chatpb.ExistingSessionTarget{SessionId: response.Plan.SessionId}, nil
		},
		newChatLogin:     server.Reauthenticate,
		sessionLifecycle: server.SessionLifecycleClient(),
		observeSession: func(ctx context.Context, id runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error) {
			planner := newSessionLaunchPlanner(server)
			intent, err := protoapi.SessionLaunchRequestIntentToProto(serverapi.OpenExistingSessionLaunchIntent(id))
			if err != nil {
				return sessionLaunchPlan{}, nil, err
			}
			plan, err := planner.PlanSession(ctx, sessionLaunchRequest{Mode: launchModeInteractive, Intent: intent})
			if err != nil {
				return sessionLaunchPlan{}, nil, err
			}
			plan.ClientLifecycleCommand = clientSettingsForInteractiveServer(server).Hooks.LifecycleCommand()
			plan.ClientLifecycleOpeningKind = lifecyclecontract.OpeningKindNew
			observation, err := prepareSessionObservation(ctx, server, planner, plan)
			return plan, observation, err
		},
		newChat: &chatpb.NewChatTarget{
			ProjectId:   binding.ProjectID,
			WorkspaceId: binding.WorkspaceID,
			Origin:      origin,
			Invocation:  options,
		},
		wiring: &runtimeWiring{
			chatSettings:    server.ChatSettingsClient(),
			chatMutations:   clients.ChatMutations,
			controls:        clients.RuntimeControls,
			liveControls:    clients.RuntimeLiveControls,
			pendingWork:     clients.PendingWork,
			processViews:    clients.ProcessViews,
			processControls: clients.ProcessControls,
			worktrees:       clients.Worktrees,
		},
		projectID: binding.ProjectID,
		active:    interactivePresentationSettings(server, config.Settings{}),
		loadCommandCatalog: func(ctx context.Context) uiCommandCatalog {
			return prepareUICommandCatalog(ctx, server, nil)
		},
		statusConfig: uiStatusConfig{
			WorkspaceRoot:   binding.WorkspaceRoot,
			PersistenceRoot: server.Connection().PersistenceRoot,
			SessionViews:    server.SessionViewClient(),
			AuthStatus:      server.AuthStatusClient(),
		},
	}, nil
}

func interactivePresentationSettings(server launchPlannerServer, settings config.Settings) config.Settings {
	local := server.LocalPreferences()
	settings.Theme = server.PresentationTheme()
	settings.Debug = local.Debug
	settings.NotificationMethod = local.NotificationMethod
	settings.TUINativeProgressBar = local.TUINativeProgressBar
	return settings
}
