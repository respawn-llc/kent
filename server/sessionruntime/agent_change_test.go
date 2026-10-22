package sessionruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/runtime"
	"core/server/session"
)

func TestAgentChangeWaitsForStopAndSurvivesCallerCancellation(t *testing.T) {
	for _, abort := range []bool{false, true} {
		name := "normal-stop"
		if abort {
			name = "runtime-abort"
		}
		t.Run(name, func(t *testing.T) { testAgentChangeShutdown(t, abort) })
	}
}

func testAgentChangeShutdown(t *testing.T, abort bool) {
	fixture := newSessionRuntimeFixture(t)
	id := lifecycleSessionID(t, fixture)
	client := &interruptReplacementLLMClient{entered: make(chan context.Context, 1)}
	plan := authorityTestRuntimePlan(t, fixture, client)
	openLifecycleRuntime(t, fixture.authority, id, &plan)
	canceled, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	handle, err := fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id),
		Resource:   CurrentAgentResource{},
		Runner: func(ctx context.Context, _ ExecutionScope, _ AgentRuntimeBridge) error {
			<-ctx.Done()
			close(canceled)
			<-release
			if abort {
				return &lifecycleRuntimeAbort{cause: context.Cause(ctx)}
			}
			return context.Cause(ctx)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- fixture.authority.WithSessionAgentChange(ctx, id,
			func(context.Context, *session.Store, *runtime.Engine) (bool, error) { return true, nil },
			func(context.Context, *session.Store, *runtime.Engine) (bool, error) { return true, nil })
	}()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("switch did not stop execution")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("switch finished before execution shutdown: %v", err)
	default:
	}
	_, err = fixture.authority.StartAgentExecution(t.Context(), AgentExecutionRequest{
		Descriptor: mustOpenSessionDescriptor(t, id),
		Resource:   CurrentAgentResource{},
		Runner:     func(context.Context, ExecutionScope, AgentRuntimeBridge) error { return nil },
	})
	if !errors.Is(err, ErrSessionStartsBlocked) {
		t.Fatalf("new execution during switch: %v", err)
	}
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("switch did not finish after execution shutdown")
	}
	if _, err := handle.Wait(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped execution: %v", err)
	}
}
