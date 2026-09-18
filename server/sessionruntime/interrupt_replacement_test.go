package sessionruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"core/server/llm"
	"core/server/runtime"
)

type interruptReplacementLLMClient struct {
	entered chan context.Context
}

func (c *interruptReplacementLLMClient) Generate(
	ctx context.Context,
	_ llm.Request,
	_ llm.StreamCallbacks,
) (llm.Response, error) {
	c.entered <- ctx
	<-ctx.Done()
	return llm.Response{}, context.Cause(ctx)
}

func (*interruptReplacementLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return sessionRuntimeTestProviderCapabilities(), nil
}

func TestInterruptSessionDoesNotCancelSuccessorStep(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	sessionID := lifecycleSessionID(t, fixture)
	client := &interruptReplacementLLMClient{entered: make(chan context.Context, 1)}
	plan := authorityTestRuntimePlan(t, fixture, client)
	openLifecycleRuntime(t, fixture.authority, sessionID, "successor-test", &plan)
	descriptor := mustOpenSessionDescriptor(t, sessionID)
	original, err := fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: descriptor,
		Resource:   CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, _ AgentRuntimeBridge) error {
			<-ctx.Done()
			return context.Cause(ctx)
		},
	})
	if err != nil {
		t.Fatalf("start original execution: %v", err)
	}

	// Pause the existing cancellation boundary so A can finish while Stop
	// still has its captured execution and has not yet interrupted the engine.
	execution := original.(executionHandle).execution
	cancel := execution.cancel
	canceled := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	execution.cancel = func() {
		cancel()
		close(canceled)
		<-release
	}
	interruptDone := make(chan error, 1)
	go func() {
		interrupted, err := fixture.authority.InterruptSession(t.Context(), sessionID)
		if err == nil && !interrupted {
			err = errors.New("captured execution was not interrupted")
		}
		interruptDone <- err
	}()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("captured cancellation did not start")
	}

	type startResult struct {
		handle ExecutionHandle
		err    error
	}
	successorDone := make(chan startResult, 1)
	admissionCtx, stopAdmission := context.WithTimeout(t.Context(), 3*time.Second)
	defer stopAdmission()
	startSuccessor := func() (ExecutionHandle, error) {
		return fixture.authority.StartAgentExecution(admissionCtx, AgentExecutionRequest{
			Descriptor: descriptor,
			Resource:   CurrentAgentResource{},
			Runner: func(ctx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
				return bridge.WithEngine(ctx, func(ctx context.Context, engine *runtime.Engine) error {
					_, err := engine.SubmitUserMessage(ctx, "successor")
					return err
				})
			},
		})
	}
	go func() {
		for {
			handle, err := startSuccessor()
			if errors.Is(err, ErrSessionRunActive) {
				continue
			}
			successorDone <- startResult{handle: handle, err: err}
			return
		}
	}()
	var providerCtx context.Context
	select {
	case providerCtx = <-client.entered:
		// Admission before Stop returns is permitted provided Stop does not
		// cancel this successor's provider request.
	case <-time.After(250 * time.Millisecond):
	}
	unblock()
	if err := <-interruptDone; err != nil {
		t.Fatalf("interrupt original execution: %v", err)
	}
	if _, err := original.Wait(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("original execution outcome = %v, want cancellation", err)
	}
	successor := <-successorDone
	if successor.err != nil {
		t.Fatalf("start successor execution: %v", successor.err)
	}
	t.Cleanup(func() {
		if err := successor.handle.Stop(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("stop successor: %v", err)
		}
	})
	if providerCtx == nil {
		select {
		case providerCtx = <-client.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("successor provider request did not start")
		}
	}
	if err := context.Cause(providerCtx); err != nil {
		t.Fatalf("captured Stop canceled successor provider request: %v", err)
	}
}
