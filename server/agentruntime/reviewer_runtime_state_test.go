package runtime

import (
	"testing"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

func TestReviewerRuntimeStateReservesBeforeInvocation(t *testing.T) {
	state := newReviewerRuntimeState(nil, nil)
	stepID := runtimeTestStepID("reviewer-phase")

	if !state.Reserve(stepID) {
		t.Fatal("Reserve returned false")
	}
	if !state.Active() {
		t.Fatal("reserved Reviewer activity is not active")
	}
	if got := state.Activity(); got != runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE {
		t.Fatalf("reserved Reviewer activity = %q, want inactive", got)
	}
	if state.Reserve(runtimeTestStepID("reviewer-second")) {
		t.Fatal("Reserve accepted a second Reviewer request")
	}
	if !state.Start(stepID) {
		t.Fatal("Start returned false")
	}
	if got := state.Activity(); got != runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INVOKING {
		t.Fatalf("started Reviewer activity = %q, want invoking", got)
	}
	if !state.Clear(stepID) {
		t.Fatal("Clear returned false")
	}
	if got := state.Activity(); got != runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE {
		t.Fatalf("completed Reviewer activity = %q, want inactive", got)
	}
}
