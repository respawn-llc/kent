package runtimeactivity

import (
	"fmt"

	"core/server/runtime"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

func ActiveStepFromRuntimeSnapshot(snapshot *runtime.RunSnapshot) *ActiveStepSnapshot {
	if snapshot == nil {
		return nil
	}
	return &ActiveStepSnapshot{
		RunID:      snapshot.RunID,
		StepID:     snapshot.StepID,
		ActiveKind: MustClientActiveKindFromRuntime(snapshot.ActiveKind),
	}
}

type ActiveStepSnapshotProvider interface {
	ActiveStepSnapshot() *runtime.RunSnapshot
}

type ActiveSessionSnapshot struct {
	SessionID string
	Activity  *runtimepb.Activity
}

func ActiveStepFromProvider(provider ActiveStepSnapshotProvider) *ActiveStepSnapshot {
	if provider == nil {
		return nil
	}
	return ActiveStepFromRuntimeSnapshot(provider.ActiveStepSnapshot())
}

func ClientActiveKindFromRuntime(kind runtime.ActiveKind) (runtimepb.ActivityActiveKind, error) {
	switch kind {
	case runtime.ActiveKindUserTurn:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN, nil
	case runtime.ActiveKindGoalLoop:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP, nil
	case runtime.ActiveKindWorkflowTurn:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_WORKFLOW_TURN, nil
	case runtime.ActiveKindCompaction:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION, nil
	case runtime.ActiveKindPreSubmitCompaction:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_PRE_SUBMIT_COMPACTION, nil
	case runtime.ActiveKindUserShell:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_SHELL, nil
	case runtime.ActiveKindBackground:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_BACKGROUND, nil
	case runtime.ActiveKindRuntimeMaintenance:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_RUNTIME_MAINTENANCE, nil
	default:
		return runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_UNSPECIFIED, fmt.Errorf("unmapped runtime active kind %q", kind)
	}
}

func MustClientActiveKindFromRuntime(kind runtime.ActiveKind) runtimepb.ActivityActiveKind {
	mapped, err := ClientActiveKindFromRuntime(kind)
	if err != nil {
		panic(err)
	}
	return mapped
}
