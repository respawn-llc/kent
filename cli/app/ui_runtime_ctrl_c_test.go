package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAcknowledgedInterruptIgnoresLateCompletion(t *testing.T) {
	for _, stopErr := range []error{nil, errors.New("late stop failure")} {
		t.Run(fmt.Sprintf("error=%v", stopErr), func(t *testing.T) {
			client := &runtimeControlFakeClient{interruptErr: stopErr}
			model := newProjectedClosedUIModel(client)
			model.setRuntimeActivityBusyForTest(true)
			_, stop := model.inputController().handleRuntimeCtrlC(nil)
			done := stop().(runtimeControlDoneMsg)

			_ = model.applyTranscriptHumanInputInterrupted(&transcriptpb.HumanInputInterrupted{})
			if model.hasPendingInterrupt() {
				t.Fatal("human-input acknowledgement left Interrupt pending")
			}
			if cmd := model.applyRuntimeControlDone(done); cmd != nil {
				t.Fatal("acknowledged Interrupt's late completion produced UI effects")
			}
			if model.runtimeMainViewBusy {
				t.Fatal("acknowledged Interrupt's late completion refreshed Runtime state")
			}
		})
	}
}

func TestInterruptCompletionsCannotAffectReplacementAttempt(t *testing.T) {
	for _, replacement := range []string{"successor-step", "same-step-after-human-input"} {
		for _, order := range []string{"old-first", "new-first"} {
			for _, oldFails := range []bool{false, true} {
				for _, newFails := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/old-error=%t/new-error=%t", replacement, order, oldFails, newFails), func(t *testing.T) {
						client := &runtimeControlFakeClient{}
						model := newProjectedClosedUIModel(client)
						model.setRuntimeActivityBusyForTest(true)
						if oldFails {
							client.interruptErr = errors.New("old stop failure")
						}
						_, first := model.inputController().handleRuntimeCtrlC(nil)
						oldDone := first().(runtimeControlDoneMsg)

						switch replacement {
						case "successor-step":
							activity := runtimeTupleTestRunningActivity()
							activity.ActiveStep.StepId = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
							if err := model.applyRuntimeActivityProjection(activity); err != nil {
								t.Fatal(err)
							}
						case "same-step-after-human-input":
							_ = model.applyTranscriptHumanInputInterrupted(&transcriptpb.HumanInputInterrupted{})
						}
						client.interruptErr = nil
						if newFails {
							client.interruptErr = errors.New("new stop failure")
						}
						_, second := model.inputController().handleRuntimeCtrlC(nil)
						message := second()
						newDone, ok := message.(runtimeControlDoneMsg)
						if !ok {
							t.Fatalf("replacement Ctrl+C produced %T instead of Interrupt", message)
						}
						if client.interruptCalls != 2 || !model.hasPendingInterrupt() {
							t.Fatal("replacement Interrupt was not sent and marked pending")
						}
						ignoreOld := func() {
							t.Helper()
							pending, refreshBusy := model.hasPendingInterrupt(), model.runtimeMainViewBusy
							if cmd := model.applyRuntimeControlDone(oldDone); cmd != nil {
								t.Fatal("replaced Interrupt completion produced UI effects")
							}
							if model.hasPendingInterrupt() != pending || model.runtimeMainViewBusy != refreshBusy {
								t.Fatal("replaced Interrupt completion changed pending state or refreshed")
							}
						}
						if order == "old-first" {
							ignoreOld()
						}
						if cmd := model.applyRuntimeControlDone(newDone); cmd == nil {
							t.Fatal("current Interrupt completion did not refresh authoritative activity")
						}
						if model.hasPendingInterrupt() == newFails {
							t.Fatal("current Interrupt did not retain accepted pending state or clear failed pending state")
						}
						if order == "new-first" {
							ignoreOld()
						}
					})
				}
			}
		}
	}
}

func TestInterruptOwnerFollowsSessionAdoption(t *testing.T) {
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprintf("error=%t", fails), func(t *testing.T) {
			client := &runtimeControlFakeClient{}
			if fails {
				client.interruptErr = errors.New("departed session stop failure")
			}
			model := newProjectedClosedUIModel(client)
			model.setRuntimeActivityBusyForTest(true)
			originalSession := model.currentRuntimeSessionID()
			_, first := model.inputController().handleRuntimeCtrlC(nil)
			done := first().(runtimeControlDoneMsg)

			_ = model.applyRuntimeSessionMetadata(&runtimepb.SessionView{SessionId: originalSession})
			if !model.hasPendingInterrupt() {
				t.Fatal("same-session metadata discarded the pending Interrupt")
			}
			adoptedSession := runtimeids.NewSessionID().String()
			_ = model.applyRuntimeSessionMetadata(&runtimepb.SessionView{SessionId: adoptedSession})
			if model.hasPendingInterrupt() {
				t.Fatal("adopting another Session retained the departed Interrupt")
			}
			if cmd := model.applyRuntimeControlDone(done); cmd != nil || model.runtimeMainViewBusy {
				t.Fatal("departed Session's completion produced UI effects or refreshed")
			}
			_ = model.applyRuntimeSessionMetadata(&runtimepb.SessionView{SessionId: originalSession})
			if cmd := model.applyRuntimeControlDone(done); cmd != nil || model.hasPendingInterrupt() {
				t.Fatal("returning to the old Session resurrected its departed Interrupt")
			}
			_ = model.applyRuntimeSessionMetadata(&runtimepb.SessionView{SessionId: adoptedSession})
			client.interruptErr = nil
			_, second := model.inputController().handleRuntimeCtrlC(nil)
			for _, message := range collectCmdMessages(t, second) {
				if _, quits := message.(tea.QuitMsg); quits {
					t.Fatal("adopted Session inherited the old Session's second-Ctrl+C exit")
				}
			}
			if client.interruptCalls != 2 || !model.hasPendingInterrupt() {
				t.Fatal("adopted Session did not receive its own Interrupt")
			}
		})
	}
}

func TestAcceptedStopReadFailureRemainsVisibleWithoutReplayingStop(t *testing.T) {
	for _, readErr := range []error{errors.New("read failed"), context.DeadlineExceeded} {
		t.Run(readErr.Error(), func(t *testing.T) {
			controls := &reconnectRetryRuntimeControlClient{stopStatus: runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_IDLE}
			reads := &flakySessionViewClient{errs: []error{readErr}}
			client := newTestSessionRuntimeClient(reads)
			client.storeMainView(runtimeTupleTestView(10, runtimeTupleTestRunningActivity()))
			model := newProjectedTestUIModel(client)
			model.sessionLiveControls = controls
			model.setRuntimeActivityBusyForTest(true)
			stop := model.runtimeControlCommand(runtimeControlInterrupt, nil)
			done := stop().(runtimeControlDoneMsg)
			if done.err != nil {
				t.Fatalf("Stop was not accepted: %v", done.err)
			}
			refresh := model.applyRuntimeControlDone(done)
			for _, msg := range collectCmdMessages(t, refresh) {
				next, _ := model.Update(msg)
				model = next.(*uiModel)
			}
			if model.transientStatus == "" || model.transientStatusKind != uiStatusNoticeError {
				t.Fatal("failed authoritative read after accepted Stop did not surface an error")
			}
			if !model.runtimeActivityBusy() || !model.hasPendingInterrupt() {
				t.Fatal("failed read fabricated an idle projection")
			}
			if reads.count != 1 || model.runtimeMainViewBusy || model.runtimeMainViewPendingSet {
				t.Fatal("failed read scheduled automatic recovery")
			}
			_, exit := model.inputController().handleRuntimeCtrlC(nil)
			messages := collectCmdMessages(t, exit)
			if len(messages) != 1 {
				t.Fatalf("second Ctrl+C returned %d messages, want Quit", len(messages))
			}
			if _, quits := messages[0].(tea.QuitMsg); !quits || controls.stopCalls != 1 {
				t.Fatal("second Ctrl+C did not exit without replaying Stop")
			}
		})
	}
}

func TestAcceptedStopRefreshesAuthoritativeActivity(t *testing.T) {
	for _, test := range []struct {
		name            string
		status          runtimepb.LiveStopStatus
		readBusy        bool
		wantBusy        bool
		currentSequence uint64
	}{
		{"stopped-idle", runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED, false, false, 10},
		{"stopped-still-busy", runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED, true, true, 10},
		{"idle-stale-busy", runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_IDLE, false, false, 10},
		{"idle-successor-busy", runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_IDLE, true, true, 10},
		{"stale-idle-read", runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED, false, true, 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			controls := &reconnectRetryRuntimeControlClient{stopStatus: test.status}
			reads := &countingSessionViewClient{}
			client := newTestSessionRuntimeClient(reads)
			client.storeMainView(runtimeTupleTestView(test.currentSequence, runtimeTupleTestRunningActivity()))
			model := newProjectedTestUIModel(client)
			model.sessionLiveControls = controls
			model.setRuntimeActivityBusyForTest(true)
			activity := runtimeTupleTestIdleActivity()
			if test.readBusy {
				activity = runtimeTupleTestRunningActivity()
			}
			reads.view = runtimeTupleTestView(20, activity)
			stop := model.runtimeControlCommand(runtimeControlInterrupt, nil)
			done := stop().(runtimeControlDoneMsg)
			refresh := model.applyRuntimeControlDone(done)
			if refresh == nil {
				t.Fatal("accepted Stop did not schedule an independent Runtime read")
			}
			if !model.hasPendingInterrupt() || !model.runtimeActivityBusy() {
				t.Fatal("Stop status cleared busy state before an authoritative read")
			}
			for _, msg := range collectCmdMessages(t, refresh) {
				next, _ := model.Update(msg)
				model = next.(*uiModel)
			}
			if model.runtimeActivityBusy() != test.wantBusy || model.hasPendingInterrupt() != test.wantBusy {
				t.Fatalf("refreshed busy/pending = %t/%t, want %t", model.runtimeActivityBusy(), model.hasPendingInterrupt(), test.wantBusy)
			}
		})
	}
}

func TestRuntimeCtrlCInterruptsRestartedAgentLoopInsteadOfQuitting(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedClosedUIModel(client)
	model.setRuntimeActivityBusyForTest(true)

	_, firstInterrupt := model.inputController().handleRuntimeCtrlC(nil)
	_ = collectCmdMessages(t, firstInterrupt)
	if client.interruptCalls != 1 || !model.hasPendingInterrupt() {
		t.Fatalf(
			"first Ctrl+C = interrupt calls %d, pending %t; want one pending interrupt",
			client.interruptCalls,
			model.hasPendingInterrupt(),
		)
	}

	restartedStepID, err := runtimeids.ParseStepID("eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee")
	if err != nil {
		t.Fatalf("parse restarted Step id: %v", err)
	}
	if err := model.applyRuntimeActivityProjection(&runtimepb.Activity{
		State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
		Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		ActiveStep: &runtimepb.ActiveStep{
			ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN, StepId: restartedStepID.String()}}); err != nil {
		t.Fatalf("apply restarted runtime activity: %v", err)
	}

	_, secondInterrupt := model.inputController().handleRuntimeCtrlC(nil)
	for _, message := range collectCmdMessages(t, secondInterrupt) {
		if _, quits := message.(tea.QuitMsg); quits {
			t.Fatal("second-run Ctrl+C exited the TUI while the Agent loop was running")
		}
	}
	if client.interruptCalls != 2 {
		t.Fatalf("interrupt calls after second-run Ctrl+C = %d, want 2", client.interruptCalls)
	}
}

func TestRuntimeCtrlCExitsWhenInterruptIsPendingForCurrentRun(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedClosedUIModel(client)
	model.setRuntimeActivityBusyForTest(true)

	_, firstInterrupt := model.inputController().handleRuntimeCtrlC(nil)
	_ = collectCmdMessages(t, firstInterrupt)
	if client.interruptCalls != 1 || !model.hasPendingInterrupt() {
		t.Fatalf(
			"first Ctrl+C = interrupt calls %d, pending %t; want one pending interrupt",
			client.interruptCalls,
			model.hasPendingInterrupt(),
		)
	}

	_, secondInterrupt := model.inputController().handleRuntimeCtrlC(nil)
	messages := collectCmdMessages(t, secondInterrupt)
	if len(messages) != 1 {
		t.Fatalf("second Ctrl+C messages = %d, want one Quit message", len(messages))
	}
	if _, quits := messages[0].(tea.QuitMsg); !quits {
		t.Fatalf("second Ctrl+C message = %T, want tea.QuitMsg", messages[0])
	}
	if !model.forcedLocalExit {
		t.Fatal("second Ctrl+C did not mark the exit as forced")
	}
	if client.interruptCalls != 1 {
		t.Fatalf("second Ctrl+C interrupt calls = %d, want no duplicate", client.interruptCalls)
	}
}

func TestRuntimeCtrlCUsesServerRunningLifecycleWithoutActiveKindPolicy(t *testing.T) {
	for _, kind := range []runtimepb.ActivityActiveKind{runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_WORKFLOW_TURN, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION} {
		t.Run(string(kind), func(t *testing.T) {
			client := &runtimeControlFakeClient{}
			model := newProjectedClosedUIModel(client)
			if err := model.applyRuntimeActivityProjection(&runtimepb.Activity{
				State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
				Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
				ActiveStep: &runtimepb.ActiveStep{
					ActiveKind: kind, StepId: ongoingTestStepID().String()}}); err != nil {
				t.Fatalf("apply running %s activity: %v", kind, err)
			}
			if !model.runtimeLifecycle.Run.IsRunning() {
				t.Fatalf("server running lifecycle for %s was not running", kind)
			}

			_, command := model.inputController().handleRuntimeCtrlC(nil)
			for _, message := range collectCmdMessages(t, command) {
				if _, quits := message.(tea.QuitMsg); quits {
					t.Fatalf("Ctrl+C exited during server running lifecycle %s", kind)
				}
			}
			if client.interruptCalls != 1 {
				t.Fatalf("%s interrupt calls = %d, want one", kind, client.interruptCalls)
			}
		})
	}
}

func TestRuntimeCtrlCExitsWheneverServerRuntimeIsNotRunning(t *testing.T) {
	for _, state := range []runtimepb.ActivityState{runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE, runtimepb.ActivityState_RUNTIME_ACTIVITY_STARTING, runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING, runtimepb.ActivityState_RUNTIME_ACTIVITY_CLOSING} {
		t.Run(string(state), func(t *testing.T) {
			model := newProjectedClosedUIModel(&runtimeControlFakeClient{})
			if err := model.applyRuntimeActivityProjection(&runtimepb.Activity{
				State:    state,
				Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE}); err != nil {
				t.Fatalf("apply %s activity: %v", state, err)
			}

			_, command := model.inputController().handleRuntimeCtrlC(nil)
			messages := collectCmdMessages(t, command)
			if len(messages) != 1 {
				t.Fatalf("%s Ctrl+C messages = %d, want one Quit message", state, len(messages))
			}
			if _, quits := messages[0].(tea.QuitMsg); !quits {
				t.Fatalf("%s Ctrl+C message = %T, want tea.QuitMsg", state, messages[0])
			}
		})
	}
}

func TestRuntimeCtrlCInterruptsAwaitingQuestionInsteadOfQuitting(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedClosedUIModel(client)
	stepID, err := runtimeids.ParseStepID("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err != nil {
		t.Fatalf("parse Step id: %v", err)
	}
	if err := model.applyRuntimeActivityProjection(&runtimepb.Activity{
		State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT,
		Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		ActiveStep: &runtimepb.ActiveStep{
			ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN, StepId: stepID.String()}}); err != nil {
		t.Fatalf("apply awaiting-Question activity: %v", err)
	}

	_, command := model.inputController().handleRuntimeCtrlC(nil)
	for _, message := range collectCmdMessages(t, command) {
		if _, quits := message.(tea.QuitMsg); quits {
			t.Fatal("Ctrl+C exited the TUI while an Agent Turn awaited a Question")
		}
	}
	if client.interruptCalls != 1 {
		t.Fatalf("interrupt calls = %d, want 1", client.interruptCalls)
	}
}

func TestRuntimeCtrlCInterruptsPendingQuestionWithoutObservedStep(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedClosedUIModel(client)
	model, _ = updateQuestionProjection(model, askEventMsg{event: testQuestionAskEvent("ask-1", "Question?")})
	if model.currentStepID != "" || !model.ask.hasCurrent() {
		t.Fatal("expected a pending Question without an observed active Step")
	}
	_, first := model.inputController().handleRuntimeCtrlC(nil)
	for _, message := range collectCmdMessages(t, first) {
		if _, quits := message.(tea.QuitMsg); quits {
			t.Fatal("first Ctrl+C detached instead of interrupting the pending Question")
		}
	}
	if client.interruptCalls != 1 || !model.hasPendingInterrupt() {
		t.Fatal("Question without an observed Step did not send Interrupt")
	}
	_, second := model.inputController().handleRuntimeCtrlC(nil)
	messages := collectCmdMessages(t, second)
	if len(messages) != 1 {
		t.Fatalf("second Ctrl+C produced %d messages, want local detach", len(messages))
	}
	if _, quits := messages[0].(tea.QuitMsg); !quits || !model.forcedLocalExit || client.interruptCalls != 1 {
		t.Fatal("second Ctrl+C did not detach locally without replaying Stop")
	}
}

func TestRuntimeCtrlCExitsWhenNoAgentLoopIsRunning(t *testing.T) {
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{})

	_, command := model.inputController().handleRuntimeCtrlC(nil)
	messages := collectCmdMessages(t, command)
	if len(messages) != 1 {
		t.Fatalf("idle Ctrl+C messages = %d, want one Quit message", len(messages))
	}
	if _, ok := messages[0].(tea.QuitMsg); !ok {
		t.Fatalf("idle Ctrl+C message = %T, want tea.QuitMsg", messages[0])
	}
}
