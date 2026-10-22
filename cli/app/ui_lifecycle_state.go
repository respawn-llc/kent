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
	return m != nil && m.runtimeLifecycle.Run.IsRunning()
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
	m.runtimeActivityProjection = activity
	m.reconcileMissingPromptRecoveryScope()
	if !protoapi.RuntimeActivityActiveForControl(activity) {
		m.runtimeLifecycle.Run = clientui.IdleRunLifecycle()
		m.activity = uiActivityIdle
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
		m.currentStepID = activity.ActiveStep.StepId
	} else {
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
	case runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION:
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

func (m *uiModel) blocksRuntimeInput() bool {
	return m != nil && (m.finalAnswerOperation != nil || m.isBusy() || m.runtimeActivityBlocksInput())
}

func (m *uiModel) setPendingInterrupt(pending bool) {
	if m == nil {
		return
	}
	if pending {
		m.interruptLifecycle = uiInterruptPending
		m.interruptStepID = strings.TrimSpace(m.currentStepID)
		return
	}
	m.interruptLifecycle = uiInterruptIdle
	m.interruptStepID = ""
}
