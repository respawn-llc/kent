package session

import (
	"testing"

	"core/shared/sessioncontract"
)

func TestWorkflowCompletionModeSurvivesCompaction(t *testing.T) {
	for _, compaction := range []CompactionMode{CompactionModeManual, CompactionModeAuto} {
		t.Run(string(compaction), func(t *testing.T) {
			store := newSessionTestStore(t)
			mode := sessioncontract.WorkflowCompletionModeShellCommand
			if err := store.SetWorkflowSnapshot(WorkflowSnapshot{CompletionMode: mode}); err != nil {
				t.Fatal(err)
			}
			projected := ProjectSnapshotBoundary(store.Meta(), SnapshotBoundaryCompaction)
			if projected.WorkflowSnapshot == nil || projected.WorkflowSnapshot.CompletionMode != mode {
				t.Fatal("compaction projection lost completion mode")
			}
			projected.WorkflowSnapshot.CompletionMode = sessioncontract.WorkflowCompletionModeTool
			if store.Meta().WorkflowSnapshot.CompletionMode != mode {
				t.Fatal("projection aliases retained metadata")
			}
			log := mustMaterializeSessionTestEventLog(t, store)
			if _, receipt, err := log.AppendHistoryReplacement(nil, HistoryReplacementRecord{
				Engine: "local", Mode: compaction,
			}); err != nil || !receipt.Committed {
				t.Fatalf("commit compaction: %+v, %v", receipt, err)
			}
			reopened := mustOpenSessionTestStore(t, store)
			if actual := reopened.Meta().WorkflowSnapshot; actual == nil || actual.CompletionMode != mode {
				t.Fatalf("completion mode after compaction and reopen = %v", actual)
			}
		})
	}
}

func TestWorkflowContextResetExpiresSnapshotsWithoutMutatingSource(t *testing.T) {
	mode := sessioncontract.WorkflowCompletionModeTool
	effort := "medium"
	meta := Meta{
		Locked:                 &LockedContract{Model: "model"},
		WorkflowSnapshot:       &WorkflowSnapshot{CompletionMode: mode},
		OriginalThinkingEffort: &effort,
		UsageState:             &UsageState{},
	}
	projected := ProjectSnapshotBoundary(meta, SnapshotBoundaryWorkflowContextReset)
	if projected.Locked != nil || projected.WorkflowSnapshot != nil ||
		projected.UsageState != nil || projected.OriginalThinkingEffort != nil {
		t.Fatal("workflow context reset retained expired snapshots")
	}
	if meta.Locked == nil || meta.WorkflowSnapshot == nil ||
		meta.UsageState == nil || meta.OriginalThinkingEffort == nil {
		t.Fatal("workflow context reset mutated source metadata")
	}
}

func TestWorkflowSnapshotRejectsInvalidSelection(t *testing.T) {
	store := newSessionTestStore(t)
	if err := store.SetWorkflowSnapshot(WorkflowSnapshot{CompletionMode: sessioncontract.WorkflowCompletionModeTool}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkflowSnapshot(WorkflowSnapshot{}); err == nil {
		t.Fatal("accepted missing completion mode")
	}
	if store.Meta().WorkflowSnapshot.CompletionMode != sessioncontract.WorkflowCompletionModeTool {
		t.Fatal("invalid selection replaced workflow snapshot")
	}
}
