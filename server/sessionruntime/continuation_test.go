package sessionruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"core/server/llm"
	"core/server/runtime"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type continuationStartupBarrier struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func TestExecutionStopJoinsQueuedModelWork(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	client := &interruptReplacementLLMClient{entered: make(chan context.Context, 1)}
	plan := authorityTestRuntimePlan(t, fixture, client)
	openLifecycleRuntime(t, fixture.authority, id, &plan)
	handle, err := fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id), Resource: CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			err := bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
				_, err := engine.QueueUserInput(ctx, runtime.QueuedUserInput{
					ID: runtimeids.NewQueueItemID().String(), ExecutionText: "queued", CanonicalPresentation: "queued",
				})
				return err
			})
			if err != nil {
				return err
			}
			<-ctx.Done()
			return context.Cause(ctx)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var providerContext context.Context
	select {
	case providerContext = <-client.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("queued model work did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := handle.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-providerContext.Done():
	default:
		t.Fatal("execution retired while its queued model request was still running")
	}
	if _, live := fixture.authority.SessionExecution(id); live {
		t.Fatal("stopped execution remains live")
	}
}

func TestExecutionStopCancelsScheduledWorkBeforeStep(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	client := &sessionRuntimeTestLLMClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done"), Phase: textutil.Value(llm.MessagePhaseFinal)},
	}}}
	plan := authorityTestRuntimePlan(t, fixture, client)
	openLifecycleRuntime(t, fixture.authority, id, &plan)
	busy := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	maintenanceDone := make(chan error, 1)
	go func() {
		maintenanceDone <- fixture.authority.WithCurrentRuntime(t.Context(), id, func(ctx context.Context, engine *runtime.Engine) error {
			return engine.RunWhenIdle(ctx, runtime.ActiveKindRuntimeMaintenance, func() error {
				close(busy)
				<-release
				return nil
			})
		})
	}()
	<-busy
	scheduled := make(chan runtime.QueuedUserMessage, 1)
	handle, err := fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id), Resource: CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			err := bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
				item, err := engine.QueueUserInput(ctx, runtime.QueuedUserInput{
					ID: runtimeids.NewQueueItemID().String(), ExecutionText: "queued", CanonicalPresentation: "queued",
				})
				if err == nil && !engine.HasScheduledQueuedUserWork() {
					return errors.New("accepted input did not schedule a worker")
				}
				scheduled <- item
				return err
			})
			if err != nil {
				return err
			}
			<-ctx.Done()
			return context.Cause(ctx)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	item := <-scheduled
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := handle.Stop(ctx); err != nil {
		t.Fatalf("stop before Step entry: %v", err)
	}
	if len(client.requestSnapshot()) != 0 {
		t.Fatal("scheduled worker entered provider before startup was released")
	}
	if _, live := fixture.authority.SessionExecution(id); live {
		t.Fatal("stopped scheduled execution remains live")
	}
	// Stop leaves unconsumed Queue content intact. Discard that input explicitly
	// before releasing independent maintenance, which may schedule retained work.
	if err := fixture.authority.WithCurrentRuntime(t.Context(), id, func(ctx context.Context, engine *runtime.Engine) error {
		if engine.HasScheduledQueuedUserWork() {
			return errors.New("Stop returned before the worker exited")
		}
		itemID, err := runtimeids.ParseQueueItemID(item.ID)
		if err != nil {
			return err
		}
		_, err = engine.RemovePendingWork(ctx, itemID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	unblock()
	if err := <-maintenanceDone; err != nil {
		t.Fatal(err)
	}
	next, err := fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id), Resource: CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			return bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
				_, err := engine.SubmitUserMessage(ctx, "next")
				return err
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Wait(ctx); err != nil {
		t.Fatalf("subsequent execution: %v", err)
	}
	if got := len(client.requestSnapshot()); got != 1 {
		t.Fatalf("provider calls after subsequent execution = %d, want one", got)
	}
}

func (b *continuationStartupBarrier) StepBegan(context.Context, AgentResourceDescriptor, runtime.StepLifecycleSnapshot) error {
	b.once.Do(func() { close(b.entered); <-b.release })
	return nil
}

func (*continuationStartupBarrier) StepEnded(context.Context, AgentResourceDescriptor, runtime.StepLifecycleSnapshot) error {
	return nil
}

func TestContinuationWakesWhenStartingLiveGroupBecomesReady(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	barrier := &continuationStartupBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	fixture.authority.options.stepLifecycle = barrier
	unblockStartup := sync.OnceFunc(func() { close(barrier.release) })
	defer unblockStartup()
	client := &ownerlessRetirementLLMClient{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	unblockProvider := sync.OnceFunc(func() { close(client.releaseFirst) })
	defer unblockProvider()
	plan := authorityTestRuntimePlan(t, fixture, client)
	ref := openLifecycleRuntime(t, fixture.authority, id, &plan)
	original, err := fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id), Resource: CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			return bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
				_, err := engine.SubmitUserMessage(ctx, "start")
				return err
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-barrier.entered
	attempted := make(chan struct{})
	var attemptedOnce sync.Once
	var observer *runtime.LiveRunWaitHandle
	done := make(chan error, 1)
	go func() {
		joined, changed, err := fixture.authority.ContinueAgentExecution(t.Context(), ActiveContinuation{
			Descriptor: mustOpenSessionDescriptor(t, id),
			Resource:   ref, Join: func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) (bool, error) {
				_, handle, accepted, err := engine.QueueUserMessageForActiveRunWithObservation(ctx, runtimeids.NewQueueItemID().String(), "redirect", t.Context(), accept)
				if errors.Is(err, runtime.ErrNoActiveLiveRun) {
					attemptedOnce.Do(func() { close(attempted) })
				}
				observer = handle
				return accepted, err
			}})
		if err == nil && (changed || joined.Scope().ID() != original.Scope().ID()) {
			err = errors.New("startup continuation did not join the starting execution")
		}
		done <- err
	}()
	<-attempted
	unblockStartup()
	<-client.firstStarted
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("steering waited for provider completion after live-group creation")
	}
	unblockProvider()
	if _, err := observer.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	request := client.requests[len(client.requests)-1]
	client.mu.Unlock()
	deliveries := 0
	for _, message := range llm.MessagesFromItems(request.Items) {
		if message.Role == llm.RoleUser && message.Content != nil && *message.Content == "redirect" {
			deliveries++
		}
	}
	if deliveries != 1 {
		t.Fatalf("steering deliveries = %d", deliveries)
	}
}

func TestContinuationReservationPrecedesExecutionFinalization(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	client := &ownerlessRetirementLLMClient{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	plan := authorityTestRuntimePlan(t, fixture, client)
	ref := openLifecycleRuntime(t, fixture.authority, id, &plan)
	finishAttempt := make(chan struct{})
	original, err := fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id), Resource: CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			err := bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
				_, err := engine.SubmitUserMessage(ctx, "start")
				return err
			})
			close(finishAttempt)
			return err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	<-client.firstStarted
	var observer *runtime.LiveRunWaitHandle
	joined, changed, err := fixture.authority.ContinueAgentExecution(t.Context(), ActiveContinuation{
		Descriptor: mustOpenSessionDescriptor(t, id),
		Resource:   ref, Join: func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) (bool, error) {
			_, handle, accepted, err := engine.QueueUserMessageForActiveRunWithObservation(ctx, runtimeids.NewQueueItemID().String(), "redirect", t.Context(),
				func(commit func() (bool, error)) (bool, error) {
					// Reservation is held while the original worker attempts cleanup.
					close(client.releaseFirst)
					<-finishAttempt
					current, live := fixture.authority.SessionExecution(id)
					if !live || current.Scope().ID() != original.Scope().ID() {
						t.Error("accepting execution binding was removed before queue publication")
					}
					return accept(commit)
				})
			observer = handle
			return accepted, err
		}})
	if err != nil || changed || joined.Scope().ID() != original.Scope().ID() {
		t.Fatalf("joined=%v changed=%t err=%v", joined, changed, err)
	}
	outcome, err := observer.Wait()
	if err != nil || outcome.Status != runtime.RunStatusCompleted {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if _, err := original.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if client.callCount() != 2 {
		t.Fatalf("provider calls = %d", client.callCount())
	}
}

func TestContinuationAfterFinalizationChoosesIdleLaunch(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	client := &ownerlessRetirementLLMClient{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	plan := authorityTestRuntimePlan(t, fixture, client)
	ref := openLifecycleRuntime(t, fixture.authority, id, &plan)
	request := AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id), Resource: CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			return bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
				_, err := engine.SubmitUserMessage(ctx, "start")
				return err
			})
		},
	}
	original, err := fixture.authority.StartAgentExecution(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	<-client.firstStarted
	close(client.releaseFirst)
	if _, err := original.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, changed, err := fixture.authority.ContinueAgentExecution(t.Context(), ActiveContinuation{Descriptor: request.Descriptor, Resource: ref,
		Join: func(context.Context, *runtime.Engine, runtime.CommandAcceptance) (bool, error) {
			t.Fatal("finished execution accepted input")
			return false, nil
		}})
	if err != nil || !changed {
		t.Fatalf("preparation refresh = %t, err=%v", changed, err)
	}
	replacementClient := &sessionRuntimeTestLLMClient{responses: []llm.Response{{
		Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("continued"), Phase: textutil.Value(llm.MessagePhaseFinal)},
		Usage:     llm.Usage{WindowTokens: 200000},
	}}}
	replacementPlan := authorityTestRuntimePlan(t, fixture, replacementClient)
	request.Runtime, request.Resource = &replacementPlan, ReplaceAgentResource{}
	continued, changed, err := fixture.authority.ContinueAgentExecution(t.Context(), IdleContinuation{Request: request})
	if err != nil || changed {
		t.Fatalf("idle admission = %v, changed=%t, err=%v", continued, changed, err)
	}
	if continued.Scope().ID() == original.Scope().ID() {
		t.Fatal("idle continuation observed the finished execution")
	}
	if _, err := continued.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(replacementClient.requestSnapshot()) != 1 {
		t.Fatal("idle continuation did not deliver exactly once")
	}
}

func TestContinuationCompetingStartRetainsAcceptingResource(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	client := &ownerlessRetirementLLMClient{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	plan := authorityTestRuntimePlan(t, fixture, client)
	ref := openLifecycleRuntime(t, fixture.authority, id, &plan)
	request := AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id), Resource: CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			return bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
				_, err := engine.SubmitUserMessage(ctx, "start")
				return err
			})
		},
	}
	competing, err := fixture.authority.StartAgentExecution(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	<-client.firstStarted
	// An idle-prepared launch arrives after another caller has started work.
	otherClient := &sessionRuntimeTestLLMClient{}
	otherPlan := authorityTestRuntimePlan(t, fixture, otherClient)
	request.Runtime, request.Resource = &otherPlan, ReplaceAgentResource{}
	_, changed, err := fixture.authority.ContinueAgentExecution(t.Context(), IdleContinuation{Request: request})
	if err != nil || !changed {
		t.Fatalf("refresh = %t, err=%v", changed, err)
	}
	var observer *runtime.LiveRunWaitHandle
	joined, changed, err := fixture.authority.ContinueAgentExecution(t.Context(), ActiveContinuation{Descriptor: request.Descriptor, Resource: ref,
		Join: func(ctx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) (bool, error) {
			_, handle, accepted, err := engine.QueueUserMessageForActiveRunWithObservation(ctx, runtimeids.NewQueueItemID().String(), "redirect", t.Context(), accept)
			observer = handle
			return accepted, err
		}})
	close(client.releaseFirst)
	if err != nil || changed || joined.Scope().ID() != competing.Scope().ID() {
		t.Fatalf("joined=%v changed=%t err=%v", joined, changed, err)
	}
	if _, err := observer.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := competing.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if client.callCount() != 2 || len(otherClient.requestSnapshot()) != 0 {
		t.Fatal("continuation replaced active work or submitted to both resources")
	}
}
