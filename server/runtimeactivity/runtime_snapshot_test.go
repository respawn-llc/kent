package runtimeactivity

import (
	"testing"

	"core/server/runtime"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

func TestClientActiveKindFromRuntimeMapsEveryRuntimeKind(t *testing.T) {
	tests := map[runtime.ActiveKind]runtimepb.ActivityActiveKind{
		runtime.ActiveKindUserTurn:            runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN,
		runtime.ActiveKindWorkflowTurn:        runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_WORKFLOW_TURN,
		runtime.ActiveKindGoalLoop:            runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP,
		runtime.ActiveKindCompaction:          runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION,
		runtime.ActiveKindPreSubmitCompaction: runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_PRE_SUBMIT_COMPACTION,
		runtime.ActiveKindUserShell:           runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_USER_SHELL,
		runtime.ActiveKindBackground:          runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_BACKGROUND,
		runtime.ActiveKindRuntimeMaintenance:  runtimepb.ActivityActiveKind_RUNTIME_ACTIVITY_ACTIVE_KIND_RUNTIME_MAINTENANCE,
	}
	for kind, want := range tests {
		got, err := ClientActiveKindFromRuntime(kind)
		if err != nil {
			t.Fatalf("ClientActiveKindFromRuntime(%q): %v", kind, err)
		}
		if got != want {
			t.Fatalf("ClientActiveKindFromRuntime(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestClientActiveKindFromRuntimeRejectsUnknownKind(t *testing.T) {
	if _, err := ClientActiveKindFromRuntime(runtime.ActiveKind("new_kind")); err == nil {
		t.Fatal("expected unknown runtime active kind to fail")
	}
}
