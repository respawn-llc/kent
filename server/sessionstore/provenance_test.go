package session

import (
	"testing"

	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

func TestInitializeCreationContextSetsTypedProvenanceAtomically(t *testing.T) {
	root := t.TempDir()
	parentAgent := newSessionTestStoreAt(t, root)
	source := newSessionTestLazyStoreAt(t, root)
	if err := InitializeCreationContext(source, parentAgent, SessionCreationSourceParentAgent, ChildContextOptions{}); err != nil {
		t.Fatalf("initialize delegated source: %v", err)
	}

	tests := []struct {
		name            string
		source          *Store
		kind            SessionCreationSourceKind
		wantPrevious    *runtimeids.SessionID
		wantParentAgent *runtimeids.SessionID
	}{
		{name: "independent", kind: SessionCreationSourceIndependent},
		{
			name:            "previous session preserves delegation ancestry",
			source:          source,
			kind:            SessionCreationSourcePreviousSession,
			wantPrevious:    mustProvenanceSessionID(t, source.Meta().SessionID),
			wantParentAgent: mustProvenanceSessionID(t, parentAgent.Meta().SessionID),
		},
		{
			name:            "parent agent records immediate caller",
			source:          source,
			kind:            SessionCreationSourceParentAgent,
			wantParentAgent: mustProvenanceSessionID(t, source.Meta().SessionID),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			child := newSessionTestLazyStoreAt(t, root)
			if err := InitializeCreationContext(child, test.source, test.kind, ChildContextOptions{InheritContinuation: true}); err != nil {
				t.Fatalf("InitializeCreationContext: %v", err)
			}
			meta := child.Meta()
			assertOptionalProvenanceID(t, "previous", meta.PreviousSessionID, test.wantPrevious)
			assertOptionalProvenanceID(t, "parent agent", meta.ParentAgentSessionID, test.wantParentAgent)
			if test.source != nil &&
				(meta.WorkspaceRoot != test.source.Meta().WorkspaceRoot ||
					meta.WorkspaceContainer != test.source.Meta().WorkspaceContainer) {
				t.Fatalf("workspace context = %q/%q, want source %q/%q",
					meta.WorkspaceRoot,
					meta.WorkspaceContainer,
					test.source.Meta().WorkspaceRoot,
					test.source.Meta().WorkspaceContainer,
				)
			}
		})
	}
}

func TestForkDetachesOwnershipWhileClonePreservesParentAgentAncestry(t *testing.T) {
	root := t.TempDir()
	parentAgent := newSessionTestStoreAt(t, root)
	source := newSessionTestLazyStoreAt(t, root)
	if err := InitializeCreationContext(source, parentAgent, SessionCreationSourceParentAgent, ChildContextOptions{}); err != nil {
		t.Fatalf("initialize source ancestry: %v", err)
	}
	if err := source.SetWorkflowSnapshot(WorkflowSnapshot{CompletionMode: sessioncontract.WorkflowCompletionModeTool}); err != nil {
		t.Fatal(err)
	}
	if err := source.SetContinuationContext(ContinuationContext{AgentRole: new("workflow-agent")}); err != nil {
		t.Fatal(err)
	}
	if err := source.SetConnectionID(config.ConnectionID("workflow-connection")); err != nil {
		t.Fatal(err)
	}
	markSessionTestLocked(t, source, sessionTestLockedContract())
	if err := source.EnsureDurable(); err != nil {
		t.Fatalf("persist source ancestry: %v", err)
	}
	sourceLog := mustMaterializeSessionTestEventLog(t, source)
	target, _, err := sourceLog.AppendRecord(stringPointer("step"), userMessagePayload(t, "fork target"))
	if err != nil {
		t.Fatalf("append fork target: %v", err)
	}

	forked, _, err := ForkAtUserMessage(sourceLog, target.Seq(), "forked", testSessionCategory, ForkThinking{Desired: "medium", PreserveNativeUpdates: true})
	if err != nil {
		t.Fatalf("ForkAtUserMessage: %v", err)
	}
	cloned, err := CloneSession(sourceLog, new("cloned"), testSessionCategory, ForkThinking{Desired: "medium", PreserveNativeUpdates: true})
	if err != nil {
		t.Fatalf("CloneSession: %v", err)
	}
	for name, derived := range map[string]*Store{"forked": forked, "cloned": cloned} {
		meta := derived.Meta()
		assertOptionalProvenanceID(t, name+" previous", meta.PreviousSessionID, mustProvenanceSessionID(t, source.Meta().SessionID))
		if meta.SessionID == source.Meta().SessionID {
			t.Fatal("child reused source identity")
		}
		if name == "forked" {
			assertOptionalProvenanceID(t, name+" parent agent", meta.ParentAgentSessionID, nil)
			if meta.WorkflowSnapshot != nil || meta.Continuation != nil || meta.ConnectionID != nil {
				t.Fatalf("interactive fork retained workflow execution context: %+v", meta)
			}
		} else {
			assertOptionalProvenanceID(t, name+" parent agent", meta.ParentAgentSessionID, mustProvenanceSessionID(t, parentAgent.Meta().SessionID))
			if meta.WorkflowSnapshot != nil || meta.Continuation != nil || meta.ConnectionID != nil || meta.Locked != nil {
				t.Fatalf("workflow clone retained source execution settings: %+v", meta)
			}
		}
	}
	if source.Meta().WorkflowSnapshot == nil || source.Meta().Continuation == nil || source.Meta().ConnectionID == nil || source.Meta().ParentAgentSessionID == nil {
		t.Fatal("fork changed source execution context")
	}
}

func mustProvenanceSessionID(t *testing.T, raw string) *runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("ParseSessionID(%q): %v", raw, err)
	}
	return &id
}

func assertOptionalProvenanceID(t *testing.T, label string, got *runtimeids.SessionID, want *runtimeids.SessionID) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("%s ID = %q, want absent", label, got.String())
		}
		return
	}
	if got == nil || *got != *want {
		t.Fatalf("%s ID = %v, want %q", label, got, want.String())
	}
}

func TestNavigationTargetSessionIDPrefersPreviousAndReturnsCopy(t *testing.T) {
	previous := mustProvenanceSessionID(t, runtimeids.NewSessionID().String())
	parentAgent := mustProvenanceSessionID(t, runtimeids.NewSessionID().String())
	meta := Meta{PreviousSessionID: previous, ParentAgentSessionID: parentAgent}

	target := NavigationTargetSessionID(meta)
	assertOptionalProvenanceID(t, "navigation target", target, previous)
	if target == previous {
		t.Fatal("navigation target aliases persisted provenance")
	}
}

func TestNavigationTargetSessionIDFallsBackToParentAgent(t *testing.T) {
	parentAgent := mustProvenanceSessionID(t, runtimeids.NewSessionID().String())
	target := NavigationTargetSessionID(Meta{ParentAgentSessionID: parentAgent})
	assertOptionalProvenanceID(t, "navigation target", target, parentAgent)
}
