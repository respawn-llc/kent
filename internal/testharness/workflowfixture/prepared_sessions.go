package workflowfixture

import (
	"context"
	"path/filepath"
	"testing"

	"core/server/metadata"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

// PrepareCurrentNodeSessions creates database cutover inputs without publishing
// Sessions or starting execution.
func PrepareCurrentNodeSessions(
	t testing.TB,
	ctx context.Context,
	store *metadata.Store,
	inputs []workflowstore.CurrentNodeStartContext,
) []workflowstore.PlannedCurrentNodeSession {
	sessions, _ := PrepareCurrentNodeSessionArtifacts(t, ctx, store, inputs)
	return sessions
}

func PrepareCurrentNodeSessionArtifacts(
	t testing.TB,
	ctx context.Context,
	store *metadata.Store,
	inputs []workflowstore.CurrentNodeStartContext,
) ([]workflowstore.PlannedCurrentNodeSession, []session.CreationPlan) {
	t.Helper()
	var sessions []workflowstore.PlannedCurrentNodeSession
	var creations []session.CreationPlan
	for _, input := range inputs {
		if input.Node.Kind != workflow.NodeKindAgent {
			continue
		}
		planned := workflowstore.PlannedCurrentNodeSession{CurrentNode: input.CurrentNode.Reference}
		source := workflow.CanonicalContextSource(input.EnteringEdge.ContextSource)
		clone := input.IsFanoutBranch && input.ContextMode != workflow.ContextModeNewSession &&
			(source.Kind == workflow.ContextSourceImmediateSource || source.Kind == workflow.ContextSourceSelectedNode)
		if input.CurrentNode.SessionID != nil && !clone {
			planned.SessionID = *input.CurrentNode.SessionID
			sessions = append(sessions, planned)
			continue
		}
		if input.ExecutionRoot == nil {
			t.Fatal("prepared Agent Session fixture requires an Execution Root")
		}
		id := runtimeids.NewSessionID()
		descriptor, err := session.NewCreateSessionDescriptor(
			id,
			filepath.Join(store.PersistenceRoot(), "projects", input.Task.ProjectID, "sessions"),
			"workspace",
			input.ExecutionRoot.SourceWorkspaceRoot,
			sessioncontract.SessionCategorySubagent,
		)
		if err != nil {
			t.Fatal(err)
		}
		creation, err := session.PrepareCreation(session.CreationRequest{Descriptor: descriptor})
		if err != nil {
			t.Fatal(err)
		}
		creation, err = creation.WithListingMetadata("Current Node session", input.Task.Title)
		if err != nil {
			t.Fatal(err)
		}
		target := metadata.SessionExecutionTargetUpdate{
			SessionID:  id.String(),
			Workspace:  &metadata.SessionExecutionTargetUpdateWorkspace{ID: input.ExecutionRoot.SourceWorkspaceID},
			CwdRelpath: ".",
		}
		if input.ExecutionRoot.Managed != nil {
			target.Worktree = &metadata.SessionExecutionTargetUpdateWorktree{ID: input.ExecutionRoot.Managed.WorktreeID}
		}
		snapshot, err := store.PrepareSessionSnapshot(ctx, creation.Snapshot(), target)
		if err != nil {
			t.Fatal(err)
		}
		planned.SessionID = id
		planned.Snapshot = &snapshot
		sessions = append(sessions, planned)
		creations = append(creations, creation)
	}
	return sessions, creations
}
