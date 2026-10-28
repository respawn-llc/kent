package app

import (
	"context"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"core/cli/app/internal/lifecyclehook"
	"core/shared/apicontract"
	"core/shared/lifecyclecontract"
	"core/shared/protoapi"
	"core/shared/serverapi"
)

type sessionUISource interface {
	SessionUIClients() sessionUIClients
}

type sessionUIClients struct {
	AttentionNotifications apicontract.AttentionNotificationService
	ProcessControls        apicontract.ProcessControlService
	ProcessViews           apicontract.ProcessViewService
	PromptControl          apicontract.PromptControlService
	RuntimeControls        apicontract.RuntimeControlService
	RuntimeLiveControls    apicontract.RuntimeLiveControlService
	PendingWork            apicontract.RuntimePendingWorkService
	ChatMutations          apicontract.ChatMutationService
	ChatSettings           apicontract.ChatSettingsService
	SessionTranscript      apicontract.SessionTranscriptService
	SessionViews           apicontract.SessionViewService
	Worktrees              apicontract.WorktreeService
}

func prepareSharedRuntime(ctx context.Context, source sessionUISource, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error) {
	if source == nil {
		return nil, errors.New("server is required")
	}
	clients := source.SessionUIClients()
	if clients.SessionTranscript == nil {
		return nil, errors.New("session transcript service is required")
	}
	_ = diagnosticWriter
	_ = startLogLine
	wiring, stopTranscriptEvents, err := prepareSharedRuntimeWiring(
		ctx,
		clients,
		plan,
	)
	if err != nil {
		return nil, err
	}
	var stopStreamsOnce sync.Once
	stopStreams := func() {
		stopStreamsOnce.Do(func() {
			stopTranscriptEvents()
		})
	}
	return &runtimeLaunchPlan{
		Wiring:           wiring,
		stopEventStreams: stopStreams,
	}, nil
}

func prepareSharedRuntimeWiring(
	ctx context.Context,
	clients sessionUIClients,
	plan sessionLaunchPlan,
) (*runtimeWiring, func(), error) {
	toolIDs := make([]string, len(plan.EnabledTools))
	for index, id := range plan.EnabledTools {
		toolIDs[index] = string(id)
	}
	preparation, err := protoapi.SessionRuntimePreparationToProto(serverapi.SessionRuntimePreparationOptions{
		ConnectionScope: plan.ConnectionScope,
		ActiveSettings:  plan.ActiveSettings, EnabledToolIDs: toolIDs,
		QuestionsEnabled: &plan.QuestionsEnabled, AutoCompactionEnabled: &plan.AutoCompactionEnabled,
		ThinkingOverrideExplicit: plan.ThinkingOverrideExplicit,
		AgentSelection:           plan.ActivationAgentSelection, ExplicitToolSelection: plan.ExplicitToolSelection,
		Source: plan.Source,
	})
	if err != nil {
		return nil, nil, err
	}
	runtimeClient := newUIRuntimeClientWithReads(plan.SessionID, clients.SessionViews).(*sessionRuntimeClient)
	terminalFocus := newTerminalFocusState()
	initialLifecycleContext := lifecyclecontract.Context{}
	if len(plan.ClientLifecycleCommand) > 0 {
		var err error
		initialLifecycleContext, err = lifecyclehook.InitialContext(plan.SessionID, plan.SessionTitle)
		if err != nil {
			return nil, nil, err
		}
	}
	lifecycleProxy := newClientLifecycleProxy(
		ctx,
		plan.ClientLifecycleCommand,
		initialLifecycleContext,
		terminalFocus.FocusedForAttention,
	)
	subscribeTranscript := func(ctx context.Context, req *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
		return clients.SessionTranscript.SubscribeSessionTranscript(ctx, req)
	}
	var transcriptStream ongoingTranscriptEventStream
	if lifecycleProxy != nil {
		transcriptStream = startSessionTranscriptEvents(ctx, plan.SessionID, subscribeTranscript, lifecycleProxy.AcceptTranscript)
	} else {
		transcriptStream = startSessionTranscriptEvents(ctx, plan.SessionID, subscribeTranscript)
	}
	transcriptEvents := transcriptStream.Events
	eventDispatcher := newUIEventDispatcher(transcriptEvents)
	requestTranscriptOpen := transcriptStream.RequestRehydration
	notificationHooks := newBellHooks(newTerminalNotifier(plan.ActiveSettings.NotificationMethod, os.Stdout, os.LookupEnv), func() string {
		if runtimeClient != nil {
			if sessionName := strings.TrimSpace(runtimeClient.MainView().Session.GetSessionName()); sessionName != "" {
				return sessionName
			}
		}
		if plan.SessionTitle == nil {
			return ""
		}
		return strings.TrimSpace(*plan.SessionTitle)
	}, terminalFocus.FocusedForAttention)
	var queueHook turnQueueHook = notificationHooks
	if lifecycleProxy != nil {
		queueHook = newTurnQueueHooks(notificationHooks, lifecycleProxy)
	}
	wiring := &runtimeWiring{
		chatMutations:          clients.ChatMutations,
		chatSettings:           clients.ChatSettings,
		controls:               clients.RuntimeControls,
		liveControls:           clients.RuntimeLiveControls,
		pendingWork:            clients.PendingWork,
		preparation:            preparation,
		attentionNotifications: clients.AttentionNotifications,
		eventDispatcher:        eventDispatcher,
		requestTranscriptOpen:  requestTranscriptOpen,
		promptAnswers:          newTranscriptPromptAnswerer(ctx, clients.PromptControl),
		promptAttention:        notificationHooks,
		turnQueueHook:          queueHook,
		terminalFocus:          terminalFocus,
		runtimeClient:          runtimeClient,
		worktrees:              clients.Worktrees,
		processControls:        clients.ProcessControls,
		processViews:           clients.ProcessViews,
	}
	if lifecycleProxy != nil {
		wiring.lifecycleHookIssues = lifecycleProxy.Issues()
		wiring.lifecycleHookDone = lifecycleProxy.Done()
		lifecycleProxy.AcceptSessionStart(plan.ClientLifecycleOpeningKind)
	}
	return wiring, func() {
		transcriptStream.Stop()
		if lifecycleProxy != nil {
			lifecycleProxy.Close()
		}
	}, nil
}
