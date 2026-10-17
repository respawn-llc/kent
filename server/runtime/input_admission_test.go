package runtime

import (
	"testing"
	"time"

	"core/server/tools"
	"core/shared/runtimeinput"
)

func TestIdleInputAdmissionStartsTurnBeforeModelCompletes(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	client := &hookClient{response: finalTextResponse("done"), beforeReturn: func() error {
		started <- struct{}{}
		<-release
		return nil
	}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	item, err := engine.SteerInput(t.Context(), plainQueuedUserInput("start"), nil)
	pendingWorkTestNoError(t, err)
	if item.Disposition != runtimeinput.InputAdmissionStartedTurn {
		t.Fatalf("idle admission = %v, want started turn", item.Disposition)
	}
}

func TestBusyInputAdmissionCapturesPendingLane(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	client := &hookClient{response: finalTextResponse("done"), beforeReturn: func() error {
		started <- struct{}{}
		<-release
		return nil
	}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	_, err := engine.SteerInput(t.Context(), plainQueuedUserInput("start"), nil)
	pendingWorkTestNoError(t, err)
	select {
	case <-started:
	case <-time.After(runtimeTestSynchronizationTimeout):
		t.Fatal("model request did not start")
	}
	queue, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("queue"))
	pendingWorkTestNoError(t, err)
	steer, err := engine.SteerInput(t.Context(), plainQueuedUserInput("steer"), func(commit func() (bool, error)) (bool, error) {
		committed, err := commit()
		if err == nil && committed {
			err = engine.Interrupt()
		}
		return committed, err
	})
	pendingWorkTestNoError(t, err)
	if steer.Disposition != runtimeinput.InputAdmissionPendingSteer || queue.Disposition != runtimeinput.InputAdmissionPendingQueue {
		t.Fatalf("busy dispositions = %v, %v", steer.Disposition, queue.Disposition)
	}
}

func TestIdleExplicitQueueAdmissionStartsTurn(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	client := &hookClient{response: finalTextResponse("done"), beforeReturn: func() error {
		<-release
		return nil
	}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	item, err := engine.QueueUserInput(t.Context(), plainQueuedUserInput("start"))
	pendingWorkTestNoError(t, err)
	if item.Disposition != runtimeinput.InputAdmissionStartedTurn {
		t.Fatalf("idle queue = %v, want started turn", item.Disposition)
	}
}

func TestRapidInputAdmissionBehindScheduledTurnIsPending(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	client := &hookClient{response: finalTextResponse("done"), beforeReturn: func() error {
		<-release
		return nil
	}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	engine.pauseQueuedUserAutoDrain()
	defer engine.resumeQueuedUserAutoDrain()
	first, err := engine.SteerInput(t.Context(), plainQueuedUserInput("first"), nil)
	pendingWorkTestNoError(t, err)
	second, err := engine.SteerInput(t.Context(), plainQueuedUserInput("second"), nil)
	pendingWorkTestNoError(t, err)
	if first.Disposition != runtimeinput.InputAdmissionStartedTurn || second.Disposition != runtimeinput.InputAdmissionPendingSteer {
		t.Fatalf("rapid admissions = %v, %v", first.Disposition, second.Disposition)
	}
}
