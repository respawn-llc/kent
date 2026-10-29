package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"core/shared/clientui"
	"core/shared/runtimeids"
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
			ActiveKind: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN, StepId: ongoingTestStepID().String()}})
}

func submitRuntimeClientForTest(t *testing.T, wiring *runtimeWiring, text string) (clientui.UserTurnSubmission, error) {
	t.Helper()
	model := newProjectedStaticUIModel()
	model.setKnownSessionTarget(wiring.runtimeClient.SessionView().SessionId)
	selected := model.presentationTarget.(uiKnownSessionTarget)
	selected.preparation = wiring.preparation
	wiring.preparation = nil
	model.presentationTarget = selected
	model.chatMutations = wiring.chatMutations
	activation, err := chatActivationFromInput(runtimeinput.Text(text))
	if err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	return model.captureChatAction(chatInputAction{input: activation, queueItemID: runtimeids.NewQueueItemID()}, nil)().submission(text)
}
