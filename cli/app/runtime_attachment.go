package app

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"core/cli/app/internal/runtimeattach"
	"core/shared/client"
	"core/shared/serverapi"
	"core/shared/transcriptdiag"
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
	activities, err := runtimeattach.SubscribeActivities(ctx, runtimeattach.ActivityRequest{
		SessionID:       plan.SessionID,
		Runtime:         clients.SessionRuntime,
		LeaseID:         lease.ID,
		ReadOnly:        strings.TrimSpace(lease.ID) == "",
		SessionActivity: clients.SessionActivity,
		PromptActivity:  clients.PromptActivity,
	})
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
		ReadOnly:          lease.ReadOnly,
		controllerLease:   leaseManager,
		close: func() {
			stopAskEvents()
			stopRuntimeEvents()
			if !lease.ReadOnly {
				runtimeattach.Release(clients.SessionRuntime, plan.SessionID, leaseManager.Value())
			}
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
	if lease.ReadOnly {
		return lease, nil, nil
	}
	leaseManager := newControllerLeaseManager(lease.ID)
	leaseManager.SetRecoverFunc(lease.Recover)
	return lease, leaseManager, nil
}

func prepareSharedRuntimeWiring(ctx context.Context, clients runtimeAttachmentClients, plan sessionLaunchPlan, activities runtimeattach.Activities, leaseManager *controllerLeaseManager, logger *runLogger) (*runtimeWiring, func(), func()) {
	runtimeClient := newUIRuntimeClientWithReads(plan.SessionID, clients.SessionViews, clients.RuntimeControls).(*sessionRuntimeClient)
	if leaseManager != nil {
		runtimeClient.SetControllerLeaseManager(leaseManager)
	} else {
		runtimeClient.SetReadOnly(true)
	}
	runtimeClient.SetTranscriptDiagnosticsEnabled(transcriptdiag.EnabledForProcess(plan.ActiveSettings.Debug))
	runtimeEvents, stopRuntimeEvents := startSessionActivityEvents(ctx, activities.Session, func(ctx context.Context, afterSequence uint64) (serverapi.SessionActivitySubscription, error) {
		return clients.SessionActivity.SubscribeSessionActivity(ctx, serverapi.SessionActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
	}, runtimeClient.transcriptDiagnosticsEnabled, func(line string) {
		logger.Logf("%s", line)
	})
	terminalFocus := newTerminalFocusState()
	turnQueueHook := newBellHooks(newTerminalNotifier(plan.ActiveSettings.NotificationMethod, os.Stdout, os.LookupEnv), func() string {
		if runtimeClient != nil {
			if sessionName := strings.TrimSpace(runtimeClient.MainView().Session.SessionName); sessionName != "" {
				return sessionName
			}
		}
		return strings.TrimSpace(plan.SessionName)
	}, terminalFocus.FocusedForAttention)
	askEvents, stopAskEvents := newClosedAskEventStream()
	if leaseManager != nil && activities.Prompt != nil {
		askEvents, stopAskEvents = startPendingPromptEvents(ctx, activities.Prompt, func(ctx context.Context, afterSequence uint64) (serverapi.PromptActivitySubscription, error) {
			return clients.PromptActivity.SubscribePromptActivity(ctx, serverapi.PromptActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
		}, clients.PromptControl, leaseManager)
	}
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
		promptHistory:         append([]string(nil), plan.PromptHistory...),
		hasOtherSessions:      plan.HasOtherSessions,
		hasOtherSessionsKnown: plan.HasOtherSessionsKnown,
	}
	return wiring, stopRuntimeEvents, stopAskEvents
}

func newClosedAskEventStream() (<-chan askEvent, func()) {
	ch := make(chan askEvent)
	close(ch)
	return ch, func() {}
}
