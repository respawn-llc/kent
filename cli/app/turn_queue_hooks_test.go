package app

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"
)

type recordingTaskCompletionSink struct {
	results []*transcriptpb.LiveRunFinished
}

func (s *recordingTaskCompletionSink) enqueueTaskCompletion(result *transcriptpb.LiveRunFinished) {
	s.results = append(s.results, result)
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
			hooks.OnTurnQueueDrained()

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
