package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"context"
	"core/shared/clientui"
	"core/shared/runtimeinput"
	"testing"
)

func (m *uiModel) setRuntimeActivityBusyForTest(busy bool) {
	if m == nil {
		return
	}
	if !busy {
		_ = m.applyRuntimeActivityProjection(&runtimepb.Activity{
			State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
			Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE})
		return
	}
	_ = m.applyRuntimeActivityProjection(&runtimepb.Activity{
		State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
		Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		ActiveStep: &runtimepb.ActiveStep{
			ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN, RunId: ongoingTestRunID().String(), StepId: ongoingTestStepID().String()}})
}

func submitRuntimeClientForTest(t *testing.T, client clientui.RuntimeClient, text string) (clientui.UserTurnSubmission, error) {
	t.Helper()
	return client.SubmitRuntimeInput(context.Background(), clientui.RuntimeSubmitRequest{
		Input: runtimeinput.Text(text),
	})
}
