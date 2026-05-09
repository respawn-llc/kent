package app

import (
	"context"
	"errors"
	"io"
	"strings"

	"builder/cli/app/internal/runtimeattach"
	"builder/shared/client"
	"builder/shared/serverapi"
	"builder/shared/transcriptdiag"
)

const runtimeReleaseTimeout = runtimeattach.ReleaseTimeout

type runtimeAttachmentServer interface {
	runtimeActivityServer
	runtimeEventServer
	runtimeWiringServer
}

type runtimeActivityServer interface {
	PromptActivityClient() client.PromptActivityClient
	SessionActivityClient() client.SessionActivityClient
	SessionRuntimeClient() client.SessionRuntimeClient
}

type runtimeEventServer interface {
	PromptActivityClient() client.PromptActivityClient
	PromptControlClient() client.PromptControlClient
	SessionActivityClient() client.SessionActivityClient
	SessionViewClient() client.SessionViewClient
	RuntimeControlClient() client.RuntimeControlClient
}

type runtimeWiringServer interface {
	ApprovalViewClient() client.ApprovalViewClient
	AskViewClient() client.AskViewClient
	ProcessControlClient() client.ProcessControlClient
	ProcessOutputClient() client.ProcessOutputClient
	ProcessViewClient() client.ProcessViewClient
	PromptControlClient() client.PromptControlClient
	RuntimeControlClient() client.RuntimeControlClient
	SessionActivityClient() client.SessionActivityClient
	SessionViewClient() client.SessionViewClient
	WorktreeClient() client.WorktreeClient
}

type runtimeEventWiringServer interface {
	runtimeEventServer
	runtimeWiringServer
}

func prepareSharedRuntime(ctx context.Context, server runtimeAttachmentServer, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error) {
	if server == nil {
		return nil, errors.New("server is required")
	}
	lease, leaseManager, err := activateSharedRuntime(ctx, server, plan)
	if err != nil {
		return nil, err
	}
	activities, err := subscribeSharedRuntimeActivities(ctx, server, plan.SessionID, lease.ID)
	if err != nil {
		return nil, err
	}
	logger := &runLogger{}
	_ = diagnosticWriter
	logger.Logf("%s", startLogLine)
	wiring, stopRuntimeEvents, stopAskEvents := prepareSharedRuntimeWiring(ctx, server, plan, activities, leaseManager, logger)
	return &runtimeLaunchPlan{
		Logger:            logger,
		Wiring:            wiring,
		ControllerLeaseID: lease.ID,
		controllerLease:   leaseManager,
		close: func() {
			stopAskEvents()
			stopRuntimeEvents()
			runtimeattach.Release(server.SessionRuntimeClient(), plan.SessionID, leaseManager.Value())
		},
	}, nil
}

func activateSharedRuntime(ctx context.Context, server runtimeActivityServer, plan sessionLaunchPlan) (runtimeattach.Lease, *controllerLeaseManager, error) {
	lease, err := runtimeattach.Activate(ctx, server.SessionRuntimeClient(), runtimeattach.Request{
		SessionID:      plan.SessionID,
		ActiveSettings: plan.ActiveSettings,
		EnabledTools:   plan.EnabledTools,
		Source:         plan.Source,
	})
	if err != nil {
		return runtimeattach.Lease{}, nil, err
	}
	leaseManager := newControllerLeaseManager(lease.ID)
	leaseManager.SetRecoverFunc(lease.Recover)
	return lease, leaseManager, nil
}

func subscribeSharedRuntimeActivities(ctx context.Context, server runtimeActivityServer, sessionID string, leaseID string) (runtimeattach.Activities, error) {
	return runtimeattach.SubscribeActivities(ctx, runtimeattach.ActivityRequest{
		SessionID:       sessionID,
		Runtime:         server.SessionRuntimeClient(),
		LeaseID:         leaseID,
		SessionActivity: server.SessionActivityClient(),
		PromptActivity:  server.PromptActivityClient(),
	})
}

func prepareSharedRuntimeWiring(ctx context.Context, server runtimeEventWiringServer, plan sessionLaunchPlan, activities runtimeattach.Activities, leaseManager *controllerLeaseManager, logger *runLogger) (*runtimeWiring, func(), func()) {
	runtimeClient := newUIRuntimeClientWithReads(plan.SessionID, server.SessionViewClient(), server.RuntimeControlClient()).(*sessionRuntimeClient)
	runtimeClient.SetControllerLeaseManager(leaseManager)
	runtimeClient.SetTranscriptDiagnosticsEnabled(transcriptdiag.EnabledForProcess(plan.ActiveSettings.Debug))
	runtimeEvents, stopRuntimeEvents := startSessionActivityEvents(ctx, activities.Session, func(ctx context.Context, afterSequence uint64) (serverapi.SessionActivitySubscription, error) {
		return server.SessionActivityClient().SubscribeSessionActivity(ctx, serverapi.SessionActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
	}, runtimeClient.transcriptDiagnosticsEnabled, func(line string) {
		logger.Logf("%s", line)
	})
	terminalFocus := newTerminalFocusState()
	turnQueueHook := newBellHooks(defaultTerminalNotifier(plan.ActiveSettings.NotificationMethod), func() string {
		if runtimeClient != nil {
			if sessionName := strings.TrimSpace(runtimeClient.MainView().Session.SessionName); sessionName != "" {
				return sessionName
			}
		}
		return strings.TrimSpace(plan.SessionName)
	}, terminalFocus.FocusedForAttention)
	askEvents, stopAskEvents := startPendingPromptEvents(ctx, activities.Prompt, func(ctx context.Context, afterSequence uint64) (serverapi.PromptActivitySubscription, error) {
		return server.PromptActivityClient().SubscribePromptActivity(ctx, serverapi.PromptActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
	}, server.PromptControlClient(), leaseManager, turnQueueHook.OnAsk)
	wiring := &runtimeWiring{
		runtimeEvents:         runtimeEvents,
		askEvents:             askEvents,
		turnQueueHook:         turnQueueHook,
		terminalFocus:         terminalFocus,
		runtimeClient:         runtimeClient,
		promptControl:         server.PromptControlClient(),
		runtimeControls:       server.RuntimeControlClient(),
		worktrees:             server.WorktreeClient(),
		processControls:       server.ProcessControlClient(),
		processOutput:         server.ProcessOutputClient(),
		processViews:          server.ProcessViewClient(),
		approvalViews:         server.ApprovalViewClient(),
		askViews:              server.AskViewClient(),
		sessionActivity:       server.SessionActivityClient(),
		sessionViews:          server.SessionViewClient(),
		hasOtherSessions:      plan.HasOtherSessions,
		hasOtherSessionsKnown: plan.HasOtherSessionsKnown,
	}
	return wiring, stopRuntimeEvents, stopAskEvents
}
