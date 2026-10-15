package app

import (
	"context"
	"errors"

	"core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
)

func prepareNewSessionUIRun(ctx context.Context, server interactiveSessionServer, origin *sessionlaunchpb.SessionCreateRequestOrigin, invocation serverapi.RunPromptOverrides) (uiLoopRequest, error) {
	binding, present := server.ProjectBinding()
	if !present {
		return uiLoopRequest{}, errors.New("New Session requires a Project workspace binding")
	}
	options, err := protoapi.RunPromptOverridesToProto(invocation)
	if err != nil {
		return uiLoopRequest{}, err
	}
	catalog := prepareUICommandCatalog(ctx, server, nil)
	source, ok := server.(sessionUISource)
	if !ok {
		return uiLoopRequest{}, errors.New("Session UI services are required")
	}
	return uiLoopRequest{
		ctx:              ctx,
		newChatSettings:  server.ChatSettingsClient(),
		newChatAuth:      server.AuthBootstrapClient(),
		newChatMutations: source.SessionUIClients().ChatMutations,
		newChatLogin:     server.Reauthenticate,
		newChat: &chatpb.NewChatTarget{
			ProjectId:   binding.ProjectID,
			WorkspaceId: binding.WorkspaceID,
			Origin:      origin,
			Invocation:  options,
		},
		wiring:                 &runtimeWiring{},
		projectID:              binding.ProjectID,
		active:                 interactivePresentationSettings(server, config.Settings{}),
		commandRegistry:        catalog.registry,
		promptCatalog:          catalog.client,
		promptCatalogEntries:   catalog.entries,
		initialTransientStatus: catalog.status,
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
