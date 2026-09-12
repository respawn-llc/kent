package app

import (
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"testing"
)

func TestRuntimeActivityOwnsCompactionStatus(t *testing.T) {
	for _, kind := range []runtimepb.ActivityActiveKind{runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION, runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_PRE_SUBMIT_COMPACTION} {
		t.Run(kind.String(), func(t *testing.T) {
			model := newProjectedStaticUIModel()

			if err := model.applyRuntimeActivityProjection(&runtimepb.Activity{
				State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
				Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
				ActiveStep: &runtimepb.ActiveStep{RunId: ongoingTestRunID().String(), StepId: ongoingTestStepID().String(),
					ActiveKind: kind}}); err != nil {
				t.Fatalf("apply running compaction activity: %v", err)
			}

			if !model.isCompacting() ||
				model.statusLineLabel() == "" ||
				!model.statusLineSpinning() {
				t.Fatalf(
					"running compaction status = compacting %t label %q spinning %t",
					model.isCompacting(),
					model.statusLineLabel(),
					model.statusLineSpinning(),
				)
			}

			if err := model.applyRuntimeActivityProjection(&runtimepb.Activity{
				State:          runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
				Reviewer:       runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
				QueueAccepting: true}); err != nil {
				t.Fatalf("apply idle activity: %v", err)
			}
			if model.isCompacting() || model.statusLineLabel() != "" || model.statusLineSpinning() {
				t.Fatalf(
					"idle status = compacting %t label %q spinning %t",
					model.isCompacting(),
					model.statusLineLabel(),
					model.statusLineSpinning(),
				)
			}
		})
	}
}

func TestTranscriptCompactionEventDoesNotMakeIdleRuntimeActive(t *testing.T) {
	model := newProjectedStaticUIModel()

	model.applyAdmittedTranscriptMessageState(transcriptTestMessage(1, &transcriptpb.CompactionStatus{StepId: ongoingTestStepID().String(),
		State: transcriptpb.CompactionState_COMPACTION_STATE_STARTED,
		Mode:  transcriptpb.CompactionMode_COMPACTION_MODE_AUTO,
		Count: 1}), runtimeTupleMergeResult{})

	if model.isCompacting() || model.statusLineSpinning() {
		t.Fatalf(
			"idle runtime became active from transcript event: compacting %t spinning %t",
			model.isCompacting(),
			model.statusLineSpinning(),
		)
	}
}
