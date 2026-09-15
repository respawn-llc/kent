package runtimecontrol

import (
	"context"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"errors"
	"sync"
	"testing"
	"time"

	testharness "core/internal/testharness/testsetup"
	"core/server/attentionnotify"
	"core/server/llm"
	"core/server/registry"
	"core/server/runtime"
	"core/server/tools"
	servicecontract "core/shared/apicontract"
	"core/shared/clientui"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

func mustRuntimeControlStepID(t *testing.T) runtimeids.StepID {
	t.Helper()
	id, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	return id
}

type liveWatchPromptSourceStub struct {
	items []registry.PendingPromptSnapshot
}

func (s liveWatchPromptSourceStub) ListPendingPrompts(string) []registry.PendingPromptSnapshot {
	return append([]registry.PendingPromptSnapshot(nil), s.items...)
}

type failingLiveWatchAttention struct{ err error }

func (f failingLiveWatchAttention) SubscribeAttentionNotifications(context.Context, serverapi.AttentionNotificationSubscribeRequest) (serverapi.AttentionNotificationSubscription, error) {
	return nil, f.err
}

func (f failingLiveWatchAttention) SubscribeSessionAttentionNotifications(context.Context, *attentionpb.SubscribeRequest) (serverapi.SessionAttentionNotificationSubscription, error) {
	return nil, f.err
}

type liveWatchMutablePromptSource struct {
	mu    sync.RWMutex
	items []registry.PendingPromptSnapshot
}

func (s *liveWatchMutablePromptSource) ListPendingPrompts(string) []registry.PendingPromptSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]registry.PendingPromptSnapshot(nil), s.items...)
}

func (s *liveWatchMutablePromptSource) set(items ...registry.PendingPromptSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append([]registry.PendingPromptSnapshot(nil), items...)
}

type liveWatchObservedAttention struct {
	servicecontract.AttentionNotificationService
	subscribed chan struct{}
	once       sync.Once
}

func (s *liveWatchObservedAttention) SubscribeSessionAttentionNotifications(ctx context.Context, req *attentionpb.SubscribeRequest) (serverapi.SessionAttentionNotificationSubscription, error) {
	sub, err := s.AttentionNotificationService.SubscribeSessionAttentionNotifications(ctx, req)
	s.once.Do(func() { close(s.subscribed) })
	return sub, err
}

type liveWatchBlockingClient struct {
	started chan struct{}
	once    sync.Once
}

func newLiveWatchBlockingClient() *liveWatchBlockingClient {
	return &liveWatchBlockingClient{started: make(chan struct{})}
}

func (c *liveWatchBlockingClient) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	return llm.Response{}, context.Cause(ctx)
}

func (c *liveWatchBlockingClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true}, nil
}

type liveWatchReleasableFinalClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newLiveWatchReleasableFinalClient() *liveWatchReleasableFinalClient {
	return &liveWatchReleasableFinalClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *liveWatchReleasableFinalClient) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return llm.Response{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Content: textutil.Value("done"),
				Phase:   textutil.Value(llm.MessagePhaseFinal),
			},
			Usage: llm.Usage{WindowTokens: 200000},
		}, nil
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
}

func (c *liveWatchReleasableFinalClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true}, nil
}

func TestLiveWatchReturnsInitialPendingQuestionWhenNoRunIsActive(t *testing.T) {
	store, _, service := newRuntimeControlTestService(t, nil, nil, runtime.Config{})
	sessionID := store.Meta().SessionID
	attention := registry.NewRuntimeRegistry().WithAttentionNotifications(attentionnotify.NewBroker(), testharness.SessionNavigationBinding)
	service.WithLiveWatchPromptSources(
		liveWatchPromptSourceStub{items: []registry.PendingPromptSnapshot{{
			Request:   tools.AskQuestionRequest{ToolCallID: "ask-1", StepID: mustRuntimeControlStepID(t).String(), Question: "Continue?"},
			CreatedAt: time.Now().UTC(),
		}}},
		attention,
	)

	response, err := service.LiveWatch(context.Background(), &promptpb.LiveWatchRequest{SessionId: sessionID})
	if err != nil {
		t.Fatalf("LiveWatch: %v", err)
	}
	if response.Outcome.GetQuestion() == nil ||
		response.Outcome.GetQuestion().GetAsk() == nil ||
		response.Outcome.GetQuestion().GetAsk().ToolCallId != "ask-1" {
		t.Fatalf("LiveWatch response = %+v", response)
	}
}

func TestLiveWatchSurfacesAttentionStreamFailureWhileRunIsBlocked(t *testing.T) {
	client := newLiveWatchBlockingClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	broker := attentionnotify.NewBroker()
	attention := registry.NewRuntimeRegistry().WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	observed := &liveWatchObservedAttention{
		AttentionNotificationService: attention,
		subscribed:                   make(chan struct{}),
	}
	service.WithLiveWatchPromptSources(liveWatchPromptSourceStub{}, observed)

	runDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserTurn(context.Background(), runtimeControlUserTurnRequest(store, "watch-stream-loss", "hello"))
		runDone <- err
	}()
	<-client.started

	watchDone := make(chan error, 1)
	go func() {
		_, err := service.LiveWatch(context.Background(), &promptpb.LiveWatchRequest{SessionId: store.Meta().SessionID})
		watchDone <- err
	}()
	<-observed.subscribed

	streamErr := errors.New("attention stream failed while blocked")
	broker.Close(streamErr)
	if err := <-watchDone; !errors.Is(err, streamErr) {
		t.Fatalf("LiveWatch error = %v, want stream failure", err)
	}
	if err := engine.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("live run did not stop after stream failure")
	}
}

func TestLiveWatchPromptWakeWinsWhileRunIsBlocked(t *testing.T) {
	client := newLiveWatchBlockingClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	runDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserTurn(context.Background(), runtimeControlUserTurnRequest(store, "watch-prompt", "hello"))
		runDone <- err
	}()
	<-client.started

	askView := &liveWatchMutablePromptSource{}
	broker := attentionnotify.NewBroker()
	attention := registry.NewRuntimeRegistry().WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	observed := &liveWatchObservedAttention{
		AttentionNotificationService: attention,
		subscribed:                   make(chan struct{}),
	}
	service.WithLiveWatchPromptSources(askView, observed)
	watchDone := make(chan *promptpb.LiveWatchSuccess, 1)
	watchErr := make(chan error, 1)
	go func() {
		response, err := service.LiveWatch(context.Background(), &promptpb.LiveWatchRequest{SessionId: store.Meta().SessionID})
		watchDone <- response
		watchErr <- err
	}()
	<-observed.subscribed

	now := time.Now().UTC()
	askView.set(registry.PendingPromptSnapshot{
		Request:   tools.AskQuestionRequest{ToolCallID: "ask-1", StepID: mustRuntimeControlStepID(t).String(), Question: "Continue?"},
		CreatedAt: now,
	})
	if err := broker.PublishPending(
		attentionnotify.RoutingScope{Kind: attentionnotify.RoutingSessionPrompt, SessionID: store.Meta().SessionID},
		clientui.AttentionNotification{
			ID:         clientui.AttentionNotificationID{Kind: clientui.AttentionNotificationKindQuestion, UUID: "ask-1"},
			Kind:       clientui.AttentionNotificationKindQuestion,
			OccurredAt: now,
			Revision:   1,
			Target: clientui.AttentionNotificationTarget{
				Kind:      clientui.AttentionNotificationTargetSessionPrompt,
				ProjectID: "project-1",
				SessionID: store.Meta().SessionID,
			},
			Question: &clientui.AttentionNotificationQuestionState{
				PreparedAskIDs:          []string{"ask-1"},
				MaterializedAskIDs:      []string{"ask-1"},
				CurrentUnresolvedAskIDs: []string{"ask-1"},
				Preview:                 "Continue?",
				DisplayCount:            1,
				MaterializedCount:       1,
			},
		},
	); err != nil {
		t.Fatalf("PublishPending: %v", err)
	}
	if err := <-watchErr; err != nil {
		t.Fatalf("LiveWatch: %v", err)
	}
	response := <-watchDone
	if response.Outcome.GetQuestion() == nil ||
		response.Outcome.GetQuestion() == nil ||
		response.Outcome.GetQuestion().GetAsk() == nil ||
		response.Outcome.GetQuestion().GetAsk().ToolCallId != "ask-1" {
		t.Fatalf("LiveWatch outcome = %+v", response.Outcome)
	}
	if err := engine.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("live run did not stop after prompt wake")
	}
}

func mustRuntimeControlSessionID(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	return id
}

func TestLiveWatchCancellationWhileRunIsBlocked(t *testing.T) {
	client := newLiveWatchBlockingClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	runDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserTurn(context.Background(), runtimeControlUserTurnRequest(store, "watch-cancel", "hello"))
		runDone <- err
	}()
	<-client.started

	broker := attentionnotify.NewBroker()
	attention := registry.NewRuntimeRegistry().WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	observed := &liveWatchObservedAttention{
		AttentionNotificationService: attention,
		subscribed:                   make(chan struct{}),
	}
	service.WithLiveWatchPromptSources(liveWatchPromptSourceStub{}, observed)
	ctx, cancel := context.WithCancel(context.Background())
	watchDone := make(chan error, 1)
	go func() {
		_, err := service.LiveWatch(ctx, &promptpb.LiveWatchRequest{SessionId: store.Meta().SessionID})
		watchDone <- err
	}()
	<-observed.subscribed
	cancel()
	if err := <-watchDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("LiveWatch error = %v, want context cancellation", err)
	}
	if err := engine.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("live run did not stop after cancellation")
	}
}

func TestLiveWatchTerminalCompletionWinsWhileRunIsBlocked(t *testing.T) {
	client := newLiveWatchReleasableFinalClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	runDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserTurn(context.Background(), runtimeControlUserTurnRequest(store, "watch-terminal", "hello"))
		runDone <- err
	}()
	<-client.started

	broker := attentionnotify.NewBroker()
	attention := registry.NewRuntimeRegistry().WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	observed := &liveWatchObservedAttention{
		AttentionNotificationService: attention,
		subscribed:                   make(chan struct{}),
	}
	service.WithLiveWatchPromptSources(liveWatchPromptSourceStub{}, observed)
	watchDone := make(chan *promptpb.LiveWatchSuccess, 1)
	watchErr := make(chan error, 1)
	go func() {
		response, err := service.LiveWatch(context.Background(), &promptpb.LiveWatchRequest{SessionId: store.Meta().SessionID})
		watchDone <- response
		watchErr <- err
	}()
	<-observed.subscribed
	close(client.release)
	if err := <-watchErr; err != nil {
		t.Fatalf("LiveWatch: %v", err)
	}
	if response := <-watchDone; response.Outcome.GetFinalAnswer() == nil {
		t.Fatalf("LiveWatch outcome = %+v, want final answer", response.Outcome)
	}
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("live run did not finish after terminal completion")
	}
	_ = engine
}

func TestLiveWatchReturnsInterruptedOutcomeWhenRunStops(t *testing.T) {
	client := newLiveWatchBlockingClient()
	store, _, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	runDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserTurn(context.Background(), runtimeControlUserTurnRequest(store, "watch-interrupt", "hello"))
		runDone <- err
	}()
	<-client.started

	broker := attentionnotify.NewBroker()
	attention := registry.NewRuntimeRegistry().WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	observed := &liveWatchObservedAttention{
		AttentionNotificationService: attention,
		subscribed:                   make(chan struct{}),
	}
	service.WithLiveWatchPromptSources(liveWatchPromptSourceStub{}, observed)
	watchDone := make(chan *promptpb.LiveWatchSuccess, 1)
	watchErr := make(chan error, 1)
	go func() {
		response, err := service.LiveWatch(context.Background(), &promptpb.LiveWatchRequest{SessionId: store.Meta().SessionID})
		watchDone <- response
		watchErr <- err
	}()
	<-observed.subscribed

	stopResponse, err := service.LiveStop(context.Background(), &runtimepb.LiveStopRequest{
		SessionId: store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("LiveStop: %v", err)
	}
	if stopResponse.Status != runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED {
		t.Fatalf("LiveStop status = %q, want %q", stopResponse.Status, runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED)
	}
	if err := <-watchErr; err != nil {
		t.Fatalf("LiveWatch: %v", err)
	}
	response := <-watchDone
	if response.Outcome.GetInterrupted() == nil ||
		response.Outcome.GetInterrupted().Reason != string(runtime.RunStatusInterrupted) ||
		response.Outcome.GetInterrupted().Diagnostic == nil {
		t.Fatalf("LiveWatch outcome = %+v, want interrupted failure", response.Outcome)
	}
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("live run did not finish after interruption")
	}
}

func TestLiveWatchSurfacesCanceledAttentionStreamWhileRunIsBlocked(t *testing.T) {
	client := newLiveWatchBlockingClient()
	store, engine, service := newRuntimeControlTestService(t, client, nil, runtime.Config{})
	runDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitUserTurn(context.Background(), runtimeControlUserTurnRequest(store, "watch-stream-canceled", "hello"))
		runDone <- err
	}()
	<-client.started

	broker := attentionnotify.NewBroker()
	attention := registry.NewRuntimeRegistry().WithAttentionNotifications(broker, testharness.SessionNavigationBinding)
	observed := &liveWatchObservedAttention{
		AttentionNotificationService: attention,
		subscribed:                   make(chan struct{}),
	}
	service.WithLiveWatchPromptSources(liveWatchPromptSourceStub{}, observed)
	watchErr := make(chan error, 1)
	go func() {
		_, err := service.LiveWatch(context.Background(), &promptpb.LiveWatchRequest{SessionId: store.Meta().SessionID})
		watchErr <- err
	}()
	<-observed.subscribed

	broker.Close(context.Canceled)
	if err := <-watchErr; !errors.Is(err, serverapi.ErrStreamFailed) || errors.Is(err, context.Canceled) {
		t.Fatalf("LiveWatch error = %v, want canceled attention stream", err)
	}
	if err := engine.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("live run did not stop after attention stream cancellation")
	}
}

func TestLiveWatchSurfacesAttentionStreamFailureBeforeArbitration(t *testing.T) {
	store, _, service := newRuntimeControlTestService(t, nil, nil, runtime.Config{})
	streamErr := errors.New("attention stream failed")
	service.WithLiveWatchPromptSources(liveWatchPromptSourceStub{}, failingLiveWatchAttention{err: streamErr})

	_, err := service.LiveWatch(context.Background(), &promptpb.LiveWatchRequest{SessionId: store.Meta().SessionID})
	if !errors.Is(err, streamErr) {
		t.Fatalf("LiveWatch error = %v, want attention stream failure", err)
	}
}

func TestLiveWatchResultClassifiesTypedTerminalStates(t *testing.T) {
	id := runtimeids.NewSessionID()
	cases := []struct {
		name       string
		result     runtime.LiveRunResult
		err        error
		failure    func(*promptpb.LiveWatchOutcome) *promptpb.LiveWatchFailure
		reason     string
		diagnostic string
	}{
		{"no final", runtime.LiveRunResult{NoFinalReason: runtime.LiveRunNoFinalAnswerReasonGoalLoop}, runtime.ErrLiveRunNoFinalAnswer, (*promptpb.LiveWatchOutcome).GetNoFinalResult, "", ""},
		{"interrupted", runtime.LiveRunResult{Status: runtime.RunStatusInterrupted, Error: errors.New("stop detail")}, errors.New("terminal"), (*promptpb.LiveWatchOutcome).GetInterrupted, "interrupted", "stop detail"},
		{"error", runtime.LiveRunResult{Status: runtime.RunStatusFailed, Error: errors.New("failure detail")}, errors.New("terminal"), (*promptpb.LiveWatchOutcome).GetExecutionError, "terminal", "failure detail"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response, err := liveWatchResult(id, "session", tc.result, tc.err)
			if err != nil {
				t.Fatalf("result = %+v, err = %v", response, err)
			}
			failure := tc.failure(response.Outcome)
			if failure == nil {
				t.Fatalf("unexpected terminal outcome: %+v", response.Outcome)
			}
			if tc.reason == "" {
				return
			}
			if failure.Reason != tc.reason ||
				failure.Diagnostic == nil || *failure.Diagnostic != tc.diagnostic {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}
