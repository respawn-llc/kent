package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"strings"

	"core/shared/clientui"
	"core/shared/protoapi"
)

type uiRuntimeLifecycle struct {
	Run clientui.RunLifecycle
}

type uiInterruptLifecycle string

const (
	uiInterruptIdle    uiInterruptLifecycle = "idle"
	uiInterruptPending uiInterruptLifecycle = "pending"
)

func (m *uiModel) isBusy() bool {
	return m != nil && (m.runtimeLifecycle.Run.IsRunning() || m.hasLocalDispatchPending())
}

func (m *uiModel) runtimeActivityBusy() bool {
	return m != nil && m.runtimeLifecycle.Run.IsRunning()
}

func (m *uiModel) runtimeActivityBlocksInput() bool {
	if m == nil {
		return false
	}
	if m.runtimeActivityProjection.GetState() == runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING {
		return false
	}
	return protoapi.RuntimeActivityActiveForControl(m.runtimeActivityProjection)
}

func (m *uiModel) applyRuntimeActivityProjection(activity *runtimepb.Activity) error {
	if m == nil {
		return nil
	}
	if err := protoapi.Validate(activity); err != nil {
		return err
	}
	m.runtimeActivityProjection = activity
	m.reconcileMissingPromptRecoveryScope()
	if !protoapi.RuntimeActivityActiveForControl(activity) {
		m.runtimeLifecycle.Run = clientui.IdleRunLifecycle()
		m.activity = uiActivityIdle
		m.currentRunID = ""
		m.currentStepID = ""
		return nil
	}
	if activity.State == runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING || activity.State == runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT {
		m.runtimeLifecycle.Run = clientui.MustRunLifecycle(clientui.RunLifecycleRunning, runtimeRunModeFromActivityKind(activity.ActiveStep.ActiveKind))
	} else {
		m.runtimeLifecycle.Run = clientui.IdleRunLifecycle()
	}
	if activity.State == runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT {
		m.activity = uiActivityQuestion
	} else {
		m.activity = uiActivityRunning
	}
	if activity.ActiveStep != nil {
		m.currentRunID = activity.ActiveStep.RunId
		m.currentStepID = activity.ActiveStep.StepId
	} else {
		m.currentRunID = ""
		m.currentStepID = ""
	}
	return nil
}

func runtimeRunModeFromActivityKind(kind runtimepb.ActivityActiveKind) clientui.RunMode {
	if kind == runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP {
		return clientui.RunModeGoalLoop
	}
	return clientui.RunModeTurn
}

func (m *uiModel) isCompacting() bool {
	if m == nil ||
		m.runtimeActivityProjection.GetState() != runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING ||
		m.runtimeActivityProjection.ActiveStep == nil {
		return false
	}
	switch m.runtimeActivityProjection.ActiveStep.ActiveKind {
	case runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION,
		runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_PRE_SUBMIT_COMPACTION:
		return true
	default:
		return false
	}
}

func (m *uiModel) isReviewerActive() bool {
	if m == nil {
		return false
	}
	switch m.runtimeActivityProjection.GetReviewer() {
	case runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INVOKING, runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_ADDRESSING_FEEDBACK:
		return true
	default:
		return false
	}
}

func (m *uiModel) hasPendingInterrupt() bool {
	return m != nil && m.interruptLifecycle == uiInterruptPending
}

func (m *uiModel) hasLocalDispatchPending() bool {
	return m != nil && m.activeSubmit.token != 0
}

func (m *uiModel) blocksRuntimeInput() bool {
	return m != nil && (m.finalAnswerOperation != nil || m.isBusy() || m.runtimeActivityBlocksInput() || m.hasLocalDispatchPending())
}

func (m *uiModel) setPendingInterrupt(pending bool) {
	if m == nil {
		return
	}
	if pending {
		m.interruptLifecycle = uiInterruptPending
		m.interruptRunID = strings.TrimSpace(m.currentRunID)
		m.interruptStepID = strings.TrimSpace(m.currentStepID)
		m.completedRunID = ""
		m.completedStepID = ""
		return
	}
	if strings.TrimSpace(m.interruptRunID) != "" && strings.TrimSpace(m.interruptStepID) != "" {
		m.completedRunID = strings.TrimSpace(m.interruptRunID)
		m.completedStepID = strings.TrimSpace(m.interruptStepID)
	}
	m.interruptLifecycle = uiInterruptIdle
	m.interruptRunID = ""
	m.interruptStepID = ""
}
