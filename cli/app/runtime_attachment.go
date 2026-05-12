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

type runtimeAttachmentSource interface {
	RuntimeAttachmentClients() runtimeAttachmentClients
}

type runtimeAttachmentClients struct {
	ApprovalViews   client.ApprovalViewClient
	AskViews        client.AskViewClient
	ProcessControls client.ProcessControlClient
	ProcessOutput   client.ProcessOutputClient
	ProcessViews    client.ProcessViewClient
	PromptActivity  client.PromptActivityClient
	PromptControl   client.PromptControlClient
	RuntimeControls client.RuntimeControlClient
	SessionActivity client.SessionActivityClient
	SessionRuntime  client.SessionRuntimeClient
	SessionViews    client.SessionViewClient
	Worktrees       client.WorktreeClient
}

func prepareSharedRuntime(ctx context.Context, source runtimeAttachmentSource, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error) {
	if source == nil {
		return nil, errors.New("server is required")
	}
	clients := source.RuntimeAttachmentClients()
	lease, leaseManager, err := activateSharedRuntime(ctx, clients, plan)
	if err != nil {
		return nil, err
	}
	activities, err := subscribeSharedRuntimeActivities(ctx, clients, plan.SessionID, lease.ID)
	if err != nil {
		return nil, err
	}
	logger := &runLogger{}
	_ = diagnosticWriter
	logger.Logf("%s", startLogLine)
	wiring, stopRuntimeEvents, stopAskEvents := prepareSharedRuntimeWiring(ctx, clients, plan, activities, leaseManager, logger)
	return &runtimeLaunchPlan{
		Logger:            logger,
		Wiring:            wiring,
		ControllerLeaseID: lease.ID,
		controllerLease:   leaseManager,
		close: func() {
			stopAskEvents()
			stopRuntimeEvents()
			runtimeattach.Release(clients.SessionRuntime, plan.SessionID, leaseManager.Value())
		},
	}, nil
}

func activateSharedRuntime(ctx context.Context, clients runtimeAttachmentClients, plan sessionLaunchPlan) (runtimeattach.Lease, *controllerLeaseManager, error) {
	lease, err := runtimeattach.Activate(ctx, clients.SessionRuntime, runtimeattach.Request{
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

func subscribeSharedRuntimeActivities(ctx context.Context, clients runtimeAttachmentClients, sessionID string, leaseID string) (runtimeattach.Activities, error) {
	return runtimeattach.SubscribeActivities(ctx, runtimeattach.ActivityRequest{
		SessionID:       sessionID,
		Runtime:         clients.SessionRuntime,
		LeaseID:         leaseID,
		SessionActivity: clients.SessionActivity,
		PromptActivity:  clients.PromptActivity,
	})
}

func prepareSharedRuntimeWiring(ctx context.Context, clients runtimeAttachmentClients, plan sessionLaunchPlan, activities runtimeattach.Activities, leaseManager *controllerLeaseManager, logger *runLogger) (*runtimeWiring, func(), func()) {
	runtimeClient := newUIRuntimeClientWithReads(plan.SessionID, clients.SessionViews, clients.RuntimeControls).(*sessionRuntimeClient)
	runtimeClient.SetControllerLeaseManager(leaseManager)
	runtimeClient.SetTranscriptDiagnosticsEnabled(transcriptdiag.EnabledForProcess(plan.ActiveSettings.Debug))
	runtimeEvents, stopRuntimeEvents := startSessionActivityEvents(ctx, activities.Session, func(ctx context.Context, afterSequence uint64) (serverapi.SessionActivitySubscription, error) {
		return clients.SessionActivity.SubscribeSessionActivity(ctx, serverapi.SessionActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
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
		return clients.PromptActivity.SubscribePromptActivity(ctx, serverapi.PromptActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
	}, clients.PromptControl, leaseManager)
	wiring := &runtimeWiring{
		runtimeEvents:         runtimeEvents,
		askEvents:             askEvents,
		turnQueueHook:         turnQueueHook,
		askNotificationHook:   turnQueueHook,
		terminalFocus:         terminalFocus,
		runtimeClient:         runtimeClient,
		promptControl:         clients.PromptControl,
		runtimeControls:       clients.RuntimeControls,
		worktrees:             clients.Worktrees,
		processControls:       clients.ProcessControls,
		processOutput:         clients.ProcessOutput,
		processViews:          clients.ProcessViews,
		approvalViews:         clients.ApprovalViews,
		askViews:              clients.AskViews,
		sessionActivity:       clients.SessionActivity,
		sessionViews:          clients.SessionViews,
		hasOtherSessions:      plan.HasOtherSessions,
		hasOtherSessionsKnown: plan.HasOtherSessionsKnown,
	}
	return wiring, stopRuntimeEvents, stopAskEvents
}
