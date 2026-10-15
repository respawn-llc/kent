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
	attachment := openLifecycleRuntime(t, fixture.authority, id, "owner", &plan)
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
	ref := attachment.Resource()
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
	attachment := openLifecycleRuntime(t, fixture.authority, id, "owner", &plan)
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
	ref := attachment.Resource()
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
	attachment := openLifecycleRuntime(t, fixture.authority, id, "owner", &plan)
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
	ref := attachment.Resource()
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
	attachment := openLifecycleRuntime(t, fixture.authority, id, "owner", &plan)
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
	ref := attachment.Resource()
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
