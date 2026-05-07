package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"builder/cli/app/commands"
	"builder/shared/serverapi"
	"github.com/google/uuid"
)

func runSessionLifecycle(ctx context.Context, server embeddedServer, interactor authInteractor, initialSessionID string) error {
	originalServer := server
	boundServer, err := ensureInteractiveProjectBinding(ctx, server)
	if err != nil {
		return err
	}
	if shouldCloseReboundServer(originalServer, boundServer) {
		defer func() { _ = boundServer.Close() }()
	}
	server = boundServer
	planner := newSessionLaunchPlanner(server)
	currentSessionID := strings.TrimSpace(initialSessionID)
	nextSessionInitialPrompt := ""
	nextSessionInitialInput := ""
	nextSessionParentID := ""
	forceNewSession := false
	showStartupUpdateNotice := true
	for {
		plan, err := planner.PlanSession(ctx, sessionLaunchRequest{
			Mode:              launchModeInteractive,
			SelectedSessionID: currentSessionID,
			ForceNewSession:   forceNewSession,
			ParentSessionID:   nextSessionParentID,
		})
		if err != nil {
			return err
		}
		forceNewSession = false
		nextSessionParentID = ""
		workspaceChangeAction, err := maybeHandlePickedSessionWorkspaceChange(ctx, server, plan)
		if err != nil {
			return err
		}
		switch workspaceChangeAction {
		case sessionWorkspaceChangePickAgain:
			currentSessionID = ""
			continue
		case sessionWorkspaceChangeReplanSelected:
			currentSessionID = plan.SessionID
			continue
		}
		runtimePlan, err := planner.PrepareRuntime(ctx, plan, os.Stderr, "app.start session_id="+plan.SessionID+" workspace="+plan.WorkspaceRoot+" model="+plan.ActiveSettings.Model)
		if err != nil {
			return err
		}
		cfg := server.Config()
		commandRegistry, err := commands.NewDefaultRegistryWithFilePrompts(cfg.WorkspaceRoot, cfg.PersistenceRoot)
		if err != nil {
			runtimePlan.Close()
			return err
		}
		initialInput := sessionLaunchInitialInputFromServer(ctx, server, plan.SessionID, nextSessionInitialInput)

		finalModel, runErr := runUILoopWithInitialPrompt(
			runtimePlan.Wiring,
			plan.ActiveSettings,
			runtimePlan.Logger,
			commandRegistry,
			nextSessionInitialPrompt,
			initialInput,
			plan.SessionName,
			plan.ModelContractLocked,
			plan.ConfiguredModelName,
			plan.StatusConfig,
			showStartupUpdateNotice,
		)
		showStartupUpdateNotice = shouldRetryStartupUpdateNotice(finalModel, showStartupUpdateNotice)
		nextSessionInitialPrompt = ""
		nextSessionInitialInput = ""
		if runErr != nil {
			runtimePlan.Close()
			return runErr
		}
		if err := persistSessionDraftToServer(ctx, server, plan.SessionID, runtimePlan.CurrentControllerLeaseID(), finalModel); err != nil {
			runtimePlan.Close()
			return err
		}

		transition := extractUITransition(finalModel)
		resolved, err := resolveSessionAction(ctx, server, interactor, plan.SessionID, runtimePlan.CurrentControllerLeaseID(), transition)
		runtimePlan.Close()
		if err != nil {
			return err
		}
		if !resolved.ShouldContinue {
			return nil
		}
		currentSessionID = resolved.NextSessionID
		nextSessionInitialPrompt = resolved.InitialPrompt
		nextSessionInitialInput = resolved.InitialInput
		nextSessionParentID = resolved.ParentSessionID
		forceNewSession = resolved.ForceNewSession
	}
}

func shouldRetryStartupUpdateNotice(model any, enabled bool) bool {
	if !enabled {
		return false
	}
	ui, ok := model.(*uiModel)
	return !ok || ui == nil || !ui.startupUpdateShown
}

func shouldCloseReboundServer(original embeddedServer, rebound embeddedServer) bool {
	if original == nil || rebound == nil || original == rebound {
		return false
	}
	originalEmbedded, originalOK := original.(*embeddedAppServer)
	reboundEmbedded, reboundOK := rebound.(*embeddedAppServer)
	if originalOK && reboundOK {
		return originalEmbedded.inner != reboundEmbedded.inner
	}
	return true
}

func sessionLaunchInitialInputFromServer(ctx context.Context, server embeddedServer, sessionID string, transitionInput string) string {
	if server == nil || server.SessionLifecycleClient() == nil {
		return transitionInput
	}
	resp, err := server.SessionLifecycleClient().GetInitialInput(ctx, serverapi.SessionInitialInputRequest{
		SessionID:       strings.TrimSpace(sessionID),
		TransitionInput: transitionInput,
	})
	if err != nil {
		return transitionInput
	}
	return resp.Input
}

func persistSessionDraftToServer(ctx context.Context, server embeddedServer, sessionID string, controllerLeaseID string, model any) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	ui, ok := model.(*uiModel)
	if !ok || ui == nil {
		return nil
	}
	if server == nil || server.SessionLifecycleClient() == nil {
		return nil
	}
	_, err := server.SessionLifecycleClient().PersistInputDraft(ctx, serverapi.SessionPersistInputDraftRequest{ClientRequestID: uuid.NewString(), SessionID: strings.TrimSpace(sessionID), ControllerLeaseID: strings.TrimSpace(controllerLeaseID), Input: ui.input})
	return err
}

type resolvedSessionAction struct {
	NextSessionID   string
	InitialPrompt   string
	InitialInput    string
	ParentSessionID string
	ForceNewSession bool
	ShouldContinue  bool
}

func resolveSessionAction(ctx context.Context, server embeddedServer, interactor authInteractor, sessionID string, controllerLeaseID string, transition UITransition) (resolvedSessionAction, error) {
	if server == nil || server.SessionLifecycleClient() == nil {
		return resolvedSessionAction{}, errors.New("session lifecycle client is required")
	}
	var forkTranscriptEntryIndex *int
	if transition.Action == UIActionForkRollback && transition.ForkUserMessageIndex == 0 && transition.ForkTranscriptEntryIndex >= 0 {
		value := transition.ForkTranscriptEntryIndex
		forkTranscriptEntryIndex = &value
	}
	resolved, err := server.SessionLifecycleClient().ResolveTransition(ctx, serverapi.SessionResolveTransitionRequest{
		ClientRequestID:   uuid.NewString(),
		SessionID:         strings.TrimSpace(sessionID),
		ControllerLeaseID: strings.TrimSpace(controllerLeaseID),
		Transition: serverapi.SessionTransition{
			Action:                   string(transition.Action),
			InitialPrompt:            transition.InitialPrompt,
			InitialInput:             transition.InitialInput,
			TargetSessionID:          transition.TargetSessionID,
			ForkUserMessageIndex:     transition.ForkUserMessageIndex,
			ForkTranscriptEntryIndex: forkTranscriptEntryIndex,
			ParentSessionID:          transition.ParentSessionID,
		},
	})
	if err != nil {
		return resolvedSessionAction{}, err
	}
	if resolved.RequiresReauth {
		if err := server.Reauthenticate(ctx, interactor); err != nil {
			return resolvedSessionAction{}, err
		}
	}
	return resolvedSessionAction{
		NextSessionID:   resolved.NextSessionID,
		InitialPrompt:   resolved.InitialPrompt,
		InitialInput:    resolved.InitialInput,
		ParentSessionID: resolved.ParentSessionID,
		ForceNewSession: resolved.ForceNewSession,
		ShouldContinue:  resolved.ShouldContinue,
	}, nil
}
