package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"core/server/llm"
	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/server/tools"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestRuntimeOperationFIFOCompletesTypedOperationsInAcceptanceOrder(t *testing.T) {
	synctest.Test(t, testRuntimeOperationFIFOCompletesTypedOperationsInAcceptanceOrder)
}

func testRuntimeOperationFIFOCompletesTypedOperationsInAcceptanceOrder(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	first := submitRuntimeOperation(fifo, func(context.Context) (int, error) {
		close(firstStarted)
		<-releaseFirst
		return 41, nil
	})
	<-firstStarted

	secondStarted := make(chan struct{})
	second := submitRuntimeOperation(fifo, func(context.Context) (string, error) {
		close(secondStarted)
		return "second", nil
	})
	synctest.Wait()
	select {
	case <-secondStarted:
		t.Fatal("second Runtime operation started before the first completed")
	default:
	}

	close(releaseFirst)
	firstResult, err := first.Await(t.Context())
	if err != nil || firstResult != 41 {
		t.Fatalf("first Runtime operation = %d, %v; want 41, nil", firstResult, err)
	}
	secondResult, err := second.Await(t.Context())
	if err != nil || secondResult != "second" {
		t.Fatalf("second Runtime operation = %q, %v; want second, nil", secondResult, err)
	}
}

func TestRuntimeOperationFIFODefersAcceptedOperationsUntilTheProtectedStepBoundary(t *testing.T) {
	synctest.Test(t, testRuntimeOperationFIFODefersAcceptedOperationsUntilTheProtectedStepBoundary)
}

func testRuntimeOperationFIFODefersAcceptedOperationsUntilTheProtectedStepBoundary(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)
	if err := fifo.Pause(t.Context()); err != nil {
		t.Fatalf("pause Runtime operations: %v", err)
	}

	started := make(chan struct{})
	deferred := submitRuntimeOperation(fifo, func(context.Context) (string, error) {
		close(started)
		return "applied", nil
	})
	synctest.Wait()
	select {
	case <-started:
		t.Fatal("Runtime operation applied during a protected Agent Step")
	default:
	}

	if err := fifo.Drain(t.Context()); err != nil {
		t.Fatalf("drain Runtime operations at Step Boundary: %v", err)
	}
	result, err := deferred.Await(t.Context())
	if err != nil || result != "applied" {
		t.Fatalf("deferred Runtime operation = %q, %v; want applied, nil", result, err)
	}
}

func TestAgentStepBoundaryDrainsEveryAcceptedSteerBeforeNextRequest(t *testing.T) {
	first := commentaryResponse("working", llm.ToolCall{
		ID:    "boundary-tool",
		Name:  string(toolspec.ToolExecCommand),
		Input: json.RawMessage(`{"command":"true"}`),
	})
	client, requestStarted, releaseRequest := newGatedHookClient(first, finalTextResponse("done"))
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID:      toolspec.ToolExecCommand,
			Handler: fakeTool{name: toolspec.ToolExecCommand},
		}),
		Config{Model: "gpt-6-sol"},
	)

	runDone := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		runDone <- err
	}()
	<-requestStarted

	texts := []string{"first steer", "second steer", "third steer"}
	expectedMessages := make([]string, 0, len(texts))
	steerDone := make(chan error, len(texts))
	for _, text := range texts {
		steer := NewAgentSteer(runtimeids.SessionIdentity{ID: runtimeids.NewSessionID()}, text)
		expectedMessages = append(expectedMessages, messageContent(steer.Message()))
		go func() {
			_, accepted, err := engine.QueueAgentSteerForActiveRun(t.Context(), runtimeids.NewQueueItemID().String(), steer, nil)
			if err == nil && !accepted {
				err = errors.New("Agent Steer was not accepted")
			}
			steerDone <- err
		}()
	}
	for range texts {
		if err := <-steerDone; err != nil {
			t.Fatalf("accept Agent Steer: %v", err)
		}
	}
	if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != len(texts) {
		t.Fatalf("Pending Work before boundary = %+v, want every accepted Steer", pending.Items)
	}
	releaseRequest()
	if err := <-runDone; err != nil {
		t.Fatalf("Agent Turn: %v", err)
	}

	client.mu.Lock()
	requests := append([]llm.Request(nil), client.calls...)
	client.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(requests))
	}
	found := make(map[string]bool, len(expectedMessages))
	for _, message := range requestMessages(requests[1]) {
		if message.Role == llm.RoleDeveloper &&
			message.MessageType != nil &&
			*message.MessageType == llm.MessageTypeAgentSteer {
			found[messageContent(message)] = true
		}
	}
	for _, message := range expectedMessages {
		if !found[message] {
			t.Fatalf("next provider request omitted an accepted Agent Steer: %+v", requestMessages(requests[1]))
		}
	}
	if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != 0 {
		t.Fatalf("Pending Work after next request = %+v, want empty", pending.Items)
	}
}

func TestAgentStepBoundaryDrainsSteersAcceptedWhileFollowingRequestIsPreparing(t *testing.T) {
	gate := sessiontest.NewPersistenceGate(runtimeTestSessionPersistence)
	store := mustCreateTestSessionAt(t, t.TempDir(), session.WithPersistenceObserver(gate))
	first := commentaryResponse("working", llm.ToolCall{
		ID:    "live-tail-boundary-tool",
		Name:  string(toolspec.ToolExecCommand),
		Input: json.RawMessage(`{"command":"true"}`),
	})
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	followingRequestStarted := make(chan struct{})
	releaseFollowingRequest := make(chan struct{})
	client := &hookClient{response: first}
	client.beforeReturn = func() error {
		close(requestStarted)
		<-releaseRequest
		client.mu.Lock()
		client.response = finalTextResponse("done")
		client.beforeReturn = func() error {
			client.mu.Lock()
			client.beforeReturn = nil
			client.mu.Unlock()
			close(followingRequestStarted)
			<-releaseFollowingRequest
			return nil
		}
		client.mu.Unlock()
		return nil
	}
	var releaseRequestOnce sync.Once
	releaseInitialRequest := func() {
		releaseRequestOnce.Do(func() { close(releaseRequest) })
	}
	var releaseFollowingRequestOnce sync.Once
	releaseNextRequest := func() {
		releaseFollowingRequestOnce.Do(func() { close(releaseFollowingRequest) })
	}
	t.Cleanup(releaseInitialRequest)
	t.Cleanup(releaseNextRequest)
	engine := mustNewTestEngine(
		t,
		store,
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID:      toolspec.ToolExecCommand,
			Handler: fakeTool{name: toolspec.ToolExecCommand},
		}),
		Config{Model: "gpt-6-sol"},
	)

	runDone := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		runDone <- err
	}()
	<-requestStarted

	queueSteer := func(text string, beforeQueue func() error) (<-chan error, string) {
		t.Helper()
		done := make(chan error, 1)
		steer := NewAgentSteer(runtimeids.SessionIdentity{ID: runtimeids.NewSessionID()}, text)
		go func() {
			_, accepted, queueErr := engine.QueueAgentSteerForActiveRun(t.Context(), runtimeids.NewQueueItemID().String(), steer, beforeQueue)
			if queueErr == nil && !accepted {
				queueErr = errors.New("Agent Steer was not accepted")
			}
			done <- queueErr
		}()
		return done, messageContent(steer.Message())
	}

	type persistenceBlock struct {
		entered <-chan struct{}
		release func()
	}
	persistenceBlockReady := make(chan persistenceBlock, 1)
	firstSteerDone, firstSteerMessage := queueSteer("first steer", func() error {
		persistenceObservations := 0
		entered, release := gate.BlockWhen(func(session.PersistedStoreSnapshot) bool {
			persistenceObservations++
			// The first observation commits the tool result and first Steer.
			// The second prepares durable state for the following request.
			return persistenceObservations == 2
		})
		persistenceBlockReady <- persistenceBlock{entered: entered, release: release}
		return nil
	})
	block := <-persistenceBlockReady
	t.Cleanup(block.release)
	if err := <-firstSteerDone; err != nil {
		t.Fatalf("accept first Agent Steer: %v", err)
	}
	releaseInitialRequest()
	<-block.entered

	secondSteerDone, secondSteerMessage := queueSteer("second steer", nil)
	secondSteerErr := <-secondSteerDone
	thirdSteerDone, thirdSteerMessage := queueSteer("third steer", nil)
	thirdSteerErr := <-thirdSteerDone
	block.release()
	<-followingRequestStarted

	client.mu.Lock()
	requests := append([]llm.Request(nil), client.calls...)
	client.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider requests at following boundary = %d, want 2", len(requests))
	}
	var actualSteers []string
	for _, message := range requestMessages(requests[1]) {
		if message.Role == llm.RoleDeveloper &&
			message.MessageType != nil &&
			*message.MessageType == llm.MessageTypeAgentSteer {
			actualSteers = append(actualSteers, messageContent(message))
		}
	}
	expectedSteers := []string{firstSteerMessage, secondSteerMessage, thirdSteerMessage}
	if !slices.Equal(actualSteers, expectedSteers) {
		t.Fatalf("following provider request Steers = %+v, want %+v", actualSteers, expectedSteers)
	}

	releaseNextRequest()
	if secondSteerErr != nil {
		t.Fatalf("accept second Agent Steer: %v", secondSteerErr)
	}
	if thirdSteerErr != nil {
		t.Fatalf("accept third Agent Steer: %v", thirdSteerErr)
	}
	if err := <-runDone; err != nil {
		t.Fatalf("Agent Turn: %v", err)
	}
	waitEngineLifecycleTasks(t, engine)

	if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != 0 {
		t.Fatalf("Pending Work after following request = %+v, want empty", pending.Items)
	}
}

func TestRuntimeOperationFIFOLongOwnerReentersAtTheCurrentTail(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)

	var mu sync.Mutex
	applied := make([]string, 0, 3)
	record := func(value string) {
		mu.Lock()
		applied = append(applied, value)
		mu.Unlock()
	}

	longOwnerStarted := make(chan struct{})
	releaseLongOwner := make(chan struct{})
	longOwnerDone := make(chan error, 1)
	scheduled := submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
		record("scheduled")
		go func() {
			close(longOwnerStarted)
			<-releaseLongOwner
			_, err := submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
				record("terminal")
				return struct{}{}, nil
			}).Await(t.Context())
			longOwnerDone <- err
		}()
		return struct{}{}, nil
	})
	if _, err := scheduled.Await(t.Context()); err != nil {
		t.Fatalf("schedule long owner: %v", err)
	}
	<-longOwnerStarted

	later := submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
		record("later")
		return struct{}{}, nil
	})
	close(releaseLongOwner)
	if _, err := later.Await(t.Context()); err != nil {
		t.Fatalf("apply later Runtime mutation: %v", err)
	}
	if err := <-longOwnerDone; err != nil {
		t.Fatalf("apply long-owner terminal mutation: %v", err)
	}

	mu.Lock()
	got := slices.Clone(applied)
	mu.Unlock()
	if want := []string{"scheduled", "later", "terminal"}; !slices.Equal(got, want) {
		t.Fatalf("Runtime operation order = %v, want %v", got, want)
	}
}

func TestWorktreeTransitionRunsBeforeQueuedHumanProviderTurn(t *testing.T) {
	toolCall := llm.ToolCall{
		ID:    "hold-preceding-step",
		Name:  string(toolspec.ToolExecCommand),
		Input: json.RawMessage(`{"command":"pwd"}`),
	}
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("working", toolCall),
		finalTextResponse("done"),
	}}
	toolStarted := make(chan struct{})
	releaseTool := make(chan struct{})
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolExecCommand,
			Handler: blockingTool{
				name:    toolspec.ToolExecCommand,
				started: toolStarted,
				release: releaseTool,
			},
		}),
		Config{Model: "gpt-6-sol"},
	)

	initialDone := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		initialDone <- err
	}()
	<-toolStarted

	humanApplying := make(chan struct{})
	releaseHumanApplication := make(chan struct{})
	humanDone := make(chan error, 1)
	go func() {
		_, accepted, err := engine.QueueUserMessageForActiveRun(
			t.Context(), runtimeids.NewQueueItemID().String(), "queued human turn",
			func() error {
				close(humanApplying)
				<-releaseHumanApplication
				return nil
			},
		)
		if err == nil && !accepted {
			err = errors.New("queued Human input was not accepted")
		}
		humanDone <- err
	}()
	<-humanApplying

	transitionStarted := make(chan struct{})
	transitionScheduled := make(chan struct{})
	releaseTransition := make(chan struct{})
	transitionDone := make(chan error, 1)
	go func() {
		transitionDone <- engine.RunExecutionTargetTransition(t.Context(), func() { close(transitionScheduled) }, func() error {
			close(transitionStarted)
			<-releaseTransition
			return nil
		})
	}()
	close(releaseHumanApplication)
	if err := <-humanDone; err != nil {
		t.Fatalf("accept queued Human input: %v", err)
	}
	<-transitionScheduled
	close(releaseTool)

	<-transitionStarted
	if got := fakeClientCallCount(client); got != 1 {
		t.Fatal("queued Human provider turn started while the Worktree transition held eligibility")
	}

	close(releaseTransition)
	if err := <-transitionDone; err != nil {
		t.Fatalf("Worktree transition: %v", err)
	}
	if err := <-initialDone; err != nil {
		t.Fatalf("preceding Agent turn: %v", err)
	}
	waitEngineLifecycleTasks(t, engine)
	if got := fakeClientCallCount(client); got != 2 {
		t.Fatal("queued Human provider turn did not start after the Worktree transition")
	}
}

func TestWorktreeTransitionWaitsForActiveAgentStepBoundary(t *testing.T) {
	synctest.Test(t, testWorktreeTransitionWaitsForActiveAgentStepBoundary)
}

func testWorktreeTransitionWaitsForActiveAgentStepBoundary(t *testing.T) {
	toolCall := llm.ToolCall{
		ID:    "hold-active-step",
		Name:  string(toolspec.ToolExecCommand),
		Input: json.RawMessage(`{"command":"pwd"}`),
	}
	client := &fakeClient{responses: []llm.Response{
		commentaryResponse("working", toolCall),
		finalTextResponse("done"),
	}}
	toolStarted := make(chan struct{})
	releaseTool := make(chan struct{})
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolExecCommand,
			Handler: blockingTool{
				name:    toolspec.ToolExecCommand,
				started: toolStarted,
				release: releaseTool,
			},
		}),
		Config{Model: "gpt-6-sol"},
	)

	initialDone := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserMessage(t.Context(), "start")
		initialDone <- err
	}()
	<-toolStarted

	transitionStarted := make(chan struct{})
	releaseTransition := make(chan struct{})
	transitionDone := make(chan error, 1)
	go func() {
		transitionDone <- engine.RunExecutionTargetTransition(t.Context(), nil, func() error {
			close(transitionStarted)
			<-releaseTransition
			return nil
		})
	}()
	synctest.Wait()
	select {
	case <-transitionStarted:
		t.Fatal("Worktree transition preempted the active Agent Step")
	default:
	}

	close(releaseTool)
	<-transitionStarted
	if got := fakeClientCallCount(client); got != 1 {
		t.Fatalf("ordinary provider continuation started before Worktree: calls=%d", got)
	}

	close(releaseTransition)
	if err := <-transitionDone; err != nil {
		t.Fatalf("Worktree transition: %v", err)
	}
	if err := <-initialDone; err != nil {
		t.Fatalf("active Agent turn: %v", err)
	}
	if got := fakeClientCallCount(client); got != 2 {
		t.Fatalf("ordinary provider continuation calls=%d, want 2 after Worktree", got)
	}
}

func newHeldReviewerWorktreeEngine(t *testing.T, mainClient llm.Client) (*Engine, <-chan struct{}) {
	t.Helper()
	reviewerClient, reviewerStarted, releaseReviewer := newGatedHookClient(
		llm.Response{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Content: textutil.Value(`{"suggestions":[]}`),
			},
			Usage: llm.Usage{WindowTokens: 200000},
		},
		llm.Response{},
	)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		mainClient,
		tools.NewRegistry(),
		Config{
			Model: "gpt-6-sol",
			Reviewer: ReviewerConfig{
				Frequency:     "all",
				Model:         "gpt-6-sol",
				ThinkingLevel: "low",
				Client:        reviewerClient,
			},
		},
	)
	t.Cleanup(func() {
		releaseReviewer()
		waitEngineLifecycleTasks(t, engine)
	})
	return engine, reviewerStarted
}

func TestWorktreeTransitionRunsWhileReviewerIsActive(t *testing.T) {
	engine, reviewerStarted := newHeldReviewerWorktreeEngine(
		t,
		&fakeClient{responses: []llm.Response{finalTextResponse("done")}},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "start Reviewer"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	<-reviewerStarted

	transitionRan := false
	err := engine.RunExecutionTargetTransition(t.Context(), nil, func() error {
		transitionRan = true
		return nil
	})
	if err != nil {
		t.Fatalf("RunWorktreeTransition: %v", err)
	}
	if !transitionRan {
		t.Fatal("Worktree transition did not run while Reviewer was active")
	}
}

type nonOverlappingStepLifecycleSink struct {
	mu     sync.Mutex
	active bool
}

func (s *nonOverlappingStepLifecycleSink) StepBegan(context.Context, StepLifecycleSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return errors.New("overlapping external Engine Step")
	}
	s.active = true
	return nil
}

func (s *nonOverlappingStepLifecycleSink) StepEnded(context.Context, StepLifecycleSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return errors.New("external Engine Step ended while idle")
	}
	s.active = false
	return nil
}

func TestWorktreeTransitionUsesReviewerFollowUpStepAtToolBoundary(t *testing.T) {
	synctest.Test(t, testWorktreeTransitionUsesReviewerFollowUpStepAtToolBoundary)
}

func testWorktreeTransitionUsesReviewerFollowUpStepAtToolBoundary(t *testing.T) {
	toolCall := llm.ToolCall{
		ID:    "reviewer-follow-up-tool",
		Name:  string(toolspec.ToolExecCommand),
		Input: json.RawMessage(`{"command":"pwd"}`),
	}
	mainClient := &fakeClient{responses: []llm.Response{
		finalTextResponse("initial"),
		commentaryResponse("applying review", toolCall),
		finalTextResponse("review applied"),
	}}
	reviewerClient := &fakeClient{responses: []llm.Response{{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Content: textutil.Value(`{"suggestions":["apply correction"]}`),
		},
		Usage: llm.Usage{WindowTokens: 200000},
	}}}
	toolStarted := make(chan struct{})
	releaseTool := make(chan struct{})
	stepLifecycle := &nonOverlappingStepLifecycleSink{}
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		mainClient,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolExecCommand,
			Handler: blockingTool{
				name:    toolspec.ToolExecCommand,
				started: toolStarted,
				release: releaseTool,
			},
		}),
		Config{
			Model:         "gpt-6-sol",
			StepLifecycle: stepLifecycle,
			Reviewer: ReviewerConfig{
				Frequency:     "all",
				Model:         "gpt-6-sol",
				ThinkingLevel: "low",
				Client:        reviewerClient,
			},
		},
	)
	var releaseToolOnce sync.Once
	releaseHeldTool := func() { releaseToolOnce.Do(func() { close(releaseTool) }) }
	t.Cleanup(func() {
		releaseHeldTool()
		waitEngineLifecycleTasks(t, engine)
	})

	if _, err := engine.SubmitUserMessage(t.Context(), "start Reviewer follow-up"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	<-toolStarted
	if got := engine.ReviewerActivity(); got != runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_ADDRESSING_FEEDBACK {
		t.Fatalf("Reviewer activity completed before its follow-up tool boundary: %q", got)
	}

	transitionRan := false
	transitionDone := make(chan error, 1)
	go func() {
		transitionDone <- engine.RunExecutionTargetTransition(t.Context(), nil, func() error {
			transitionRan = true
			return nil
		})
	}()
	synctest.Wait()
	select {
	case err := <-transitionDone:
		t.Fatalf("Worktree transition completed before the Reviewer follow-up tool boundary: %v", err)
	default:
	}

	releaseHeldTool()
	if err := <-transitionDone; err != nil {
		t.Fatalf("Worktree transition failed at Reviewer follow-up boundary: %v", err)
	}
	if !transitionRan {
		t.Fatal("Worktree transition callback did not run")
	}
	waitEngineLifecycleTasks(t, engine)
	if got := engine.ReviewerActivity(); got != runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE {
		t.Fatal("Reviewer activity remained active after its follow-up completed")
	}
}

func TestRuntimeOperationFIFOPropagatesTypedFailure(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)
	wantErr := errors.New("typed failure")

	result, err := submitRuntimeOperation(fifo, func(context.Context) (int, error) {
		return 73, wantErr
	}).Await(t.Context())
	if result != 73 || !errors.Is(err, wantErr) {
		t.Fatalf("typed Runtime result = %d, %v; want 73, %v", result, err, wantErr)
	}
}

func TestRuntimeOperationFIFOIdleDrainWaitsForAcceptedWork(t *testing.T) {
	synctest.Test(t, testRuntimeOperationFIFOIdleDrainWaitsForAcceptedWork)
}

func testRuntimeOperationFIFOIdleDrainWaitsForAcceptedWork(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)
	started := make(chan struct{})
	release := make(chan struct{})
	submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
		close(started)
		<-release
		return struct{}{}, nil
	})
	<-started

	drained := make(chan error, 1)
	go func() {
		drained <- fifo.Drain(t.Context())
	}()
	synctest.Wait()
	select {
	case err := <-drained:
		t.Fatalf("Idle drain completed before accepted work: %v", err)
	default:
	}
	close(release)
	if err := <-drained; err != nil {
		t.Fatalf("Idle drain: %v", err)
	}
}

func TestRuntimeOperationFIFOPauseCutoffDetectsAcceptanceAfterIdleDrain(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)

	drainedThrough, err := fifo.drainThrough(t.Context())
	if err != nil {
		t.Fatalf("idle drain: %v", err)
	}
	applied := submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	})
	if _, err := applied.Await(t.Context()); err != nil {
		t.Fatalf("accepted Runtime operation: %v", err)
	}
	pausedThrough, err := fifo.pauseThrough(t.Context())
	if err != nil {
		t.Fatalf("pause after acceptance: %v", err)
	}
	if pausedThrough <= drainedThrough {
		t.Fatalf(
			"pause cutoff = %d, want newer than preceding drain cutoff %d",
			pausedThrough,
			drainedThrough,
		)
	}
}

func TestRuntimeOperationFIFOCanceledPauseDoesNotSuspendLaterAcceptedWork(t *testing.T) {
	synctest.Test(t, testRuntimeOperationFIFOCanceledPauseDoesNotSuspendLaterAcceptedWork)
}

func testRuntimeOperationFIFOCanceledPauseDoesNotSuspendLaterAcceptedWork(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
		close(firstStarted)
		<-releaseFirst
		return struct{}{}, nil
	})
	<-firstStarted

	secondStarted := make(chan struct{})
	submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
		close(secondStarted)
		return struct{}{}, nil
	})

	pauseCtx, cancelPause := context.WithCancel(t.Context())
	pauseDone := make(chan error, 1)
	go func() { pauseDone <- fifo.Pause(pauseCtx) }()
	synctest.Wait()
	fifo.mu.Lock()
	pauseRequested := fifo.pauseDone != nil
	fifo.mu.Unlock()
	if !pauseRequested {
		t.Fatal("Runtime FIFO did not accept the pause request")
	}
	cancelPause()
	if err := <-pauseDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled pause error = %v, want context canceled", err)
	}

	drained := make(chan error, 1)
	go func() { drained <- fifo.Drain(t.Context()) }()
	close(releaseFirst)
	<-secondStarted
	if err := <-drained; err != nil {
		t.Fatalf("drain after pause cancellation: %v", err)
	}
}

func TestRuntimeOperationFIFOCloseCancelsCurrentAndRejectsQueuedAndNewWork(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	currentStarted := make(chan struct{})
	current := submitRuntimeOperation(fifo, func(ctx context.Context) (string, error) {
		close(currentStarted)
		<-ctx.Done()
		return "current", context.Cause(ctx)
	})
	<-currentStarted
	queuedStarted := make(chan struct{})
	queued := submitRuntimeOperation(fifo, func(context.Context) (string, error) {
		close(queuedStarted)
		return "queued", nil
	})

	closed := make(chan struct{})
	go func() {
		fifo.Close()
		close(closed)
	}()
	<-closed
	currentResult, currentErr := current.Await(t.Context())
	if currentResult != "current" || !errors.Is(currentErr, context.Canceled) {
		t.Fatalf("current Runtime result = %q, %v; want current, canceled", currentResult, currentErr)
	}
	queuedResult, queuedErr := queued.Await(t.Context())
	if queuedResult != "" || !errors.Is(queuedErr, ErrEngineClosed) {
		t.Fatalf("queued Runtime result = %q, %v; want zero, Runtime closed", queuedResult, queuedErr)
	}
	select {
	case <-queuedStarted:
		t.Fatal("queued Runtime operation started during close")
	default:
	}
	newResult, newErr := submitRuntimeOperation(fifo, func(context.Context) (string, error) {
		return "new", nil
	}).Await(t.Context())
	if newResult != "" || !errors.Is(newErr, ErrEngineClosed) {
		t.Fatalf("new Runtime result = %q, %v; want zero, Runtime closed", newResult, newErr)
	}
}

func TestRuntimeOperationFIFOCloseWhileIdle(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	fifo.Close()
}

func TestRuntimeDeferredCallerCancellationDoesNotCancelAcceptedWork(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)
	started := make(chan struct{})
	release := make(chan struct{})
	deferred := submitRuntimeOperation(fifo, func(context.Context) (string, error) {
		close(started)
		<-release
		return "applied", nil
	})
	<-started

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := deferred.Await(canceled); result != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait = %q, %v; want zero, canceled", result, err)
	}
	close(release)
	if result, err := deferred.Await(t.Context()); result != "applied" || err != nil {
		t.Fatalf("accepted Runtime operation = %q, %v; want applied, nil", result, err)
	}
}

func TestRuntimeOperationFIFORejectsTheTenThousandthPendingOperation(t *testing.T) {
	fifo := newRuntimeOperationFIFO()
	t.Cleanup(fifo.Close)
	if err := fifo.Pause(t.Context()); err != nil {
		t.Fatalf("pause Runtime FIFO: %v", err)
	}
	for range maxPendingRuntimeOperations {
		submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
			return struct{}{}, nil
		})
	}
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("10,000th pending Runtime operation did not panic")
		}
	}()
	submitRuntimeOperation(fifo, func(context.Context) (struct{}, error) {
		return struct{}{}, nil
	})
}

func TestEngineDefersOrdinaryRuntimeMutationUntilProtectedStepFinishes(t *testing.T) {
	synctest.Test(t, testEngineDefersOrdinaryRuntimeMutationUntilProtectedStepFinishes)
}

func testEngineDefersOrdinaryRuntimeMutationUntilProtectedStepFinishes(t *testing.T) {
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		&fakeClient{},
		tools.NewRegistry(),
		Config{Model: "gpt-6-sol"},
	)
	stepStarted := make(chan struct{})
	releaseStep := make(chan struct{})
	stepDone := make(chan error, 1)
	go func() {
		stepDone <- engine.RunWhenIdle(t.Context(), ActiveKindUserTurn, func() error {
			close(stepStarted)
			<-releaseStep
			return nil
		})
	}()
	<-stepStarted

	mutationDone := make(chan error, 1)
	go func() {
		mutationDone <- engine.AppendCommittedEntry(t.Context(), "system", "after boundary")
	}()
	synctest.Wait()
	select {
	case err := <-mutationDone:
		t.Fatalf("ordinary Runtime mutation completed during protected Step: %v", err)
	default:
	}

	close(releaseStep)
	if err := <-stepDone; err != nil {
		t.Fatalf("finish protected Agent Step: %v", err)
	}
	if err := <-mutationDone; err != nil {
		t.Fatalf("apply Runtime mutation at boundary: %v", err)
	}
	entries := engine.ChatSnapshot().Entries
	if len(entries) != 1 || entries[0].Text != "after boundary" {
		t.Fatalf("post-boundary transcript = %+v", entries)
	}
}

func TestActiveSessionRuntimeFIFOsAreIndependent(t *testing.T) {
	first := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		&fakeClient{},
		tools.NewRegistry(),
		Config{Model: "gpt-6-sol"},
	)
	second := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		&fakeClient{},
		tools.NewRegistry(),
		Config{Model: "gpt-6-sol", SupportedThinkingValues: []string{"low"}},
	)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	blocked := submitEngineRuntimeOperation(first, func(context.Context) (struct{}, error) {
		close(firstStarted)
		<-releaseFirst
		return struct{}{}, nil
	})
	<-firstStarted

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.SetThinkingLevel(t.Context(), "low")
	}()
	if err := <-secondDone; err != nil {
		t.Fatalf("second Session Runtime mutation: %v", err)
	}
	close(releaseFirst)
	if _, err := blocked.Await(t.Context()); err != nil {
		t.Fatalf("release first Session Runtime operation: %v", err)
	}
}

func TestEngineAppliesStreamingStateAndItsEventAsOneOrderedMutation(t *testing.T) {
	synctest.Test(t, testEngineAppliesStreamingStateAndItsEventAsOneOrderedMutation)
}

func testEngineAppliesStreamingStateAndItsEventAsOneOrderedMutation(t *testing.T) {
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		&fakeClient{},
		tools.NewRegistry(),
		Config{Model: "gpt-6-sol"},
	)
	stepStarted := make(chan struct{})
	releaseStep := make(chan struct{})
	stepDone := make(chan error, 1)
	go func() {
		stepDone <- engine.RunWhenIdle(t.Context(), ActiveKindUserTurn, func() error {
			close(stepStarted)
			<-releaseStep
			return nil
		})
	}()
	<-stepStarted

	mutationDone := make(chan struct{})
	go func() {
		engine.SetStreamingError("ordered error")
		close(mutationDone)
	}()
	synctest.Wait()
	select {
	case <-mutationDone:
		t.Fatal("streaming mutation completed during protected Step")
	default:
	}
	if got := engine.ChatSnapshot().StreamingError; got != "" {
		t.Fatalf("streaming error applied before its ordered event: %q", got)
	}

	close(releaseStep)
	if err := <-stepDone; err != nil {
		t.Fatalf("finish protected Agent Step: %v", err)
	}
	<-mutationDone
	if got := engine.ChatSnapshot().StreamingError; got != "ordered error" {
		t.Fatalf("streaming error = %q, want ordered error", got)
	}
}

func TestForegroundShellReleasesRuntimeFIFOAfterScheduling(t *testing.T) {
	synctest.Test(t, testForegroundShellReleasesRuntimeFIFOAfterScheduling)
}

func testForegroundShellReleasesRuntimeFIFOAfterScheduling(t *testing.T) {
	handler := &heldRuntimeShell{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		&fakeClient{},
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID:      toolspec.ToolExecCommand,
			Handler: handler,
		}),
		Config{Model: "gpt-6-sol", SupportedThinkingValues: []string{"low"}},
	)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	first := submitEngineRuntimeOperation(engine, func(context.Context) (struct{}, error) {
		close(firstStarted)
		<-releaseFirst
		return struct{}{}, nil
	})
	<-firstStarted

	shellDone := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserShellCommand(t.Context(), "pwd")
		shellDone <- err
	}()
	synctest.Wait()
	select {
	case <-handler.started:
		t.Fatal("foreground shell started before the preceding Runtime mutation")
	default:
	}
	close(releaseFirst)
	if _, err := first.Await(t.Context()); err != nil {
		t.Fatalf("complete preceding Runtime mutation: %v", err)
	}
	<-handler.started

	settingDone := make(chan error, 1)
	go func() {
		settingDone <- engine.SetThinkingLevel(t.Context(), "low")
	}()
	if err := <-settingDone; err != nil {
		t.Fatalf("apply setting while shell is held: %v", err)
	}

	close(handler.release)
	if err := <-shellDone; err != nil {
		t.Fatalf("foreground shell: %v", err)
	}
}

func TestForegroundShellTerminalEffectReentersAtTheCurrentRuntimeTail(t *testing.T) {
	synctest.Test(t, testForegroundShellTerminalEffectReentersAtTheCurrentRuntimeTail)
}

func testForegroundShellTerminalEffectReentersAtTheCurrentRuntimeTail(t *testing.T) {
	handler := &heldRuntimeShell{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		returned: make(chan struct{}),
	}
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		&fakeClient{},
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID:      toolspec.ToolExecCommand,
			Handler: handler,
		}),
		Config{Model: "gpt-6-sol"},
	)
	shellDone := make(chan error, 1)
	go func() {
		_, err := engine.SubmitUserShellCommand(t.Context(), "pwd")
		shellDone <- err
	}()
	<-handler.started

	laterStarted := make(chan struct{})
	releaseLater := make(chan struct{})
	later := submitEngineRuntimeOperation(engine, func(context.Context) (struct{}, error) {
		close(laterStarted)
		<-releaseLater
		return struct{}{}, nil
	})
	<-laterStarted
	close(handler.release)
	<-handler.returned
	synctest.Wait()
	select {
	case err := <-shellDone:
		t.Fatalf("foreground shell completed before its terminal FIFO effect: %v", err)
	default:
	}

	close(releaseLater)
	if _, err := later.Await(t.Context()); err != nil {
		t.Fatalf("later Runtime mutation: %v", err)
	}
	if err := <-shellDone; err != nil {
		t.Fatalf("foreground shell: %v", err)
	}
}

func TestWorktreeTerminalEffectReentersAtTheCurrentRuntimeTail(t *testing.T) {
	synctest.Test(t, testWorktreeTerminalEffectReentersAtTheCurrentRuntimeTail)
}

func testWorktreeTerminalEffectReentersAtTheCurrentRuntimeTail(t *testing.T) {
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		&fakeClient{},
		tools.NewRegistry(),
		Config{Model: "gpt-6-sol"},
	)
	transitionStarted := make(chan struct{})
	releaseTransition := make(chan struct{})
	terminalApplied := make(chan struct{})
	transitionDone := make(chan error, 1)
	go func() {
		transitionDone <- engine.RunExecutionTargetTransition(t.Context(), nil, func() error {
			close(transitionStarted)
			<-releaseTransition
			err := engine.ApplyWorktreeTransitionTerminal(t.Context(), func(context.Context) error {
				close(terminalApplied)
				return nil
			})
			return err
		})
	}()
	<-transitionStarted

	laterStarted := make(chan struct{})
	releaseLater := make(chan struct{})
	later := submitEngineRuntimeOperation(engine, func(context.Context) (struct{}, error) {
		close(laterStarted)
		<-releaseLater
		return struct{}{}, nil
	})
	<-laterStarted
	close(releaseTransition)
	synctest.Wait()
	select {
	case <-terminalApplied:
		t.Fatal("Worktree terminal effect bypassed the current Runtime tail")
	default:
	}

	close(releaseLater)
	if _, err := later.Await(t.Context()); err != nil {
		t.Fatalf("later Runtime mutation: %v", err)
	}
	if err := <-transitionDone; err != nil {
		t.Fatalf("Worktree transition: %v", err)
	}
}

func TestManualCompactionReleasesRuntimeFIFOAfterScheduling(t *testing.T) {
	client := &heldRuntimeCompactionClient{
		fakeCompactionClient: &fakeCompactionClient{
			compactionResponses: []llm.CompactionResponse{{
				OutputItems: []llm.ResponseItem{llm.ResponseItem{
					Type:             llm.ResponseItemTypeCompaction,
					EncryptedContent: textutil.Value("checkpoint"),
				}},
				Usage: llm.Usage{WindowTokens: 200000},
			}},
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		tools.NewRegistry(),
		Config{Model: "gpt-6-sol", SupportedThinkingValues: []string{"low"}},
	)
	engine.compactionRuntimeState().SetManualCompactionEligible(true)

	compactionDone := make(chan error, 1)
	go func() {
		compactionDone <- engine.CompactContext(t.Context(), "")
	}()
	if err := <-compactionDone; err != nil {
		t.Fatalf("schedule manual compaction: %v", err)
	}
	<-client.started
	if err := engine.SetThinkingLevel(t.Context(), "low"); err != nil {
		t.Fatalf("apply setting while manual compaction is held: %v", err)
	}
	close(client.release)
}

func TestSteersAcceptedDuringCompactionFullyDrainIntoTheFollowingAgentStep(t *testing.T) {
	client := &heldRuntimeCompactionClient{
		fakeCompactionClient: &fakeCompactionClient{
			compactionResponses: []llm.CompactionResponse{{
				OutputItems: []llm.ResponseItem{llm.ResponseItem{
					Type:             llm.ResponseItemTypeCompaction,
					EncryptedContent: textutil.Value("checkpoint"),
				}},
				Usage: llm.Usage{WindowTokens: 200000},
			}},
			responses: []llm.Response{finalTextResponse("done")},
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		tools.NewRegistry(),
		Config{Model: "gpt-6-sol", SupportedThinkingValues: []string{"low"}},
	)
	engine.compactionRuntimeState().SetManualCompactionEligible(true)

	if err := engine.CompactContext(t.Context(), ""); err != nil {
		t.Fatalf("schedule manual compaction: %v", err)
	}
	<-client.started

	texts := []string{"first steer", "second steer", "third steer"}
	for _, text := range texts {
		if _, err := engine.Steer(t.Context(), text, nil); err != nil {
			t.Fatalf("accept %q during compaction: %v", text, err)
		}
	}
	if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != len(texts) {
		t.Fatalf("Pending Work during compaction = %+v, want every accepted Steer", pending.Items)
	}

	close(client.release)
	waitEngineLifecycleTasks(t, engine)

	client.mu.Lock()
	requests := append([]llm.Request(nil), client.calls...)
	client.mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("post-compaction provider requests = %d, want 1", len(requests))
	}
	var actual []string
	for _, message := range requestMessages(requests[0]) {
		if message.Role == llm.RoleUser {
			actual = append(actual, messageContent(message))
		}
	}
	if !slices.Equal(actual, texts) {
		t.Fatalf("post-compaction human messages = %q, want distinct ordered messages %q", actual, texts)
	}
	if pending := pendingWorkTestSnapshot(t, engine); len(pending.Items) != 0 {
		t.Fatalf("Pending Work after post-compaction request = %+v, want empty", pending.Items)
	}
}

type heldRuntimeShell struct {
	started  chan struct{}
	release  chan struct{}
	returned chan struct{}
}

func (h *heldRuntimeShell) Call(ctx context.Context, call tools.Call) (tools.Result, error) {
	close(h.started)
	select {
	case <-h.release:
		if h.returned != nil {
			close(h.returned)
		}
		return tools.Result{
			CallID: call.ID,
			Name:   toolspec.ToolExecCommand,
			Output: []byte(`"done"`),
		}, nil
	case <-ctx.Done():
		return tools.Result{}, context.Cause(ctx)
	}
}

type heldRuntimeCompactionClient struct {
	*fakeCompactionClient
	started chan struct{}
	release chan struct{}
}

func (c *heldRuntimeCompactionClient) Compact(
	ctx context.Context,
	request llm.CompactionRequest,
) (llm.CompactionResponse, error) {
	close(c.started)
	select {
	case <-c.release:
		return c.fakeCompactionClient.Compact(ctx, request)
	case <-ctx.Done():
		return llm.CompactionResponse{}, context.Cause(ctx)
	}
}
