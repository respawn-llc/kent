package app

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeinput"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

type recordingTaskCompletionSink struct {
	results []*transcriptpb.LiveRunFinished
}

func TestCompletionHooksRunPerTurnWhileSuccessorIsPending(t *testing.T) {
	completions := &recordingTaskCompletionSink{}
	ringer := &countRinger{}
	model := newTurnQueueHookTestModel(newTurnQueueHooks(newUnfocusedBellHooks(ringer), completions))
	model.setRuntimeActivityBusyForTest(true)
	model.enqueueInput("later turn", runtimeinput.Text("later turn"), runtimeinput.PendingWorkLaneQueue)
	model.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: ongoingHydrationMessage(1)})
	for index, work := range []bool{false, true} {
		message := testAssistantFinalLiveRunMessage(uint64(index+2), "answer", work)
		model.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: message})
		if len(completions.results) != index+1 {
			t.Fatal("completion hook waited for successor work")
		}
	}
	if ringer.total() != 0 {
		t.Fatal("transcript completion decided notification eligibility")
	}
}

func (s *recordingTaskCompletionSink) enqueueTaskCompletion(result *transcriptpb.LiveRunFinished) {
	s.results = append(s.results, result)
}

func TestTurnEndAttentionNotifiesWhileSuccessorIsPending(t *testing.T) {
	ringer := &countRinger{}
	hooks := newTurnQueueHooks(newUnfocusedBellHooks(ringer), nil)
	model := newTurnQueueHookTestModel(hooks)
	model.setRuntimeActivityBusyForTest(true)
	model.enqueueInput("later", runtimeinput.Text("later"), runtimeinput.PendingWorkLaneQueue)
	hooks.OnSessionTurnEnd("answer")
	hooks.OnSessionTurnEnd("another answer")
	if ringer.notifications != 2 {
		t.Fatalf("turn-end notifications = %d, want two immediate deliveries", ringer.notifications)
	}
}

func newTurnQueueHookTestModel(hooks turnQueueHook) *uiModel {
	model := newProjectedStaticUIModel(WithUITurnQueueHook(hooks))
	model.ongoingTranscript = newOngoingTranscriptController(
		&ongoingSurfaceSpy{},
		ongoingTestFrameProvider,
		noopOngoingTranscriptRuntimeAdmission,
		model.applyAdmittedTranscriptMessageState,
	)
	return model
}

func TestTurnQueueHooksDoNotCompleteNonCompletedAssistantFinalResult(t *testing.T) {
	for _, status := range []transcriptpb.LiveRunStatus{transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_FAILED, transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_INTERRUPTED} {
		t.Run(status.String(), func(t *testing.T) {
			completions := &recordingTaskCompletionSink{}
			hooks := newTurnQueueHooks(newUnfocusedBellHooks(&countRinger{}), completions)
			message := testAssistantFinalLiveRunMessage(2, "answer before non-completed outcome", true)
			result := message.GetEvent().GetLiveRunFinished()
			result.Status = status
			if status == transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_FAILED {
				failure := "terminal failure"
				result.Failure = &failure
			}
			message = transcriptTestMessage(2, result)

			hooks.OnTranscriptMessage(message)

			if got := len(completions.results); got != 0 {
				t.Fatalf("%s assistant-final completion hooks = %d, want 0", status, got)
			}
		})
	}
}

func testAssistantFinalLiveRunMessage(
	sequence uint64,
	answer string,
	workPerformed bool) *transcriptpb.Message {
	startedAt := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Duration(sequence) * time.Second)
	return transcriptTestMessage(sequence, &transcriptpb.LiveRunFinished{
		Status:        transcriptpb.LiveRunStatus_LIVE_RUN_STATUS_COMPLETED,
		ResultKind:    transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_ASSISTANT_FINAL_ANSWER,
		WorkPerformed: workPerformed,
		FinalAnswer:   &answer,
		StartedAt:     timestamppb.New(startedAt),
		FinishedAt:    timestamppb.New(finishedAt)})
}
