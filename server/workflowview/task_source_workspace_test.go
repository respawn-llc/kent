package workflowview

import (
	"testing"

	"core/server/workflowstore"
	"core/shared/serverapi"
)

func TestTaskSourceReadsAttachedWorkspaceBeyondFormerLimit(t *testing.T) {
	f := newCurrentNodeViewFixture(t, false)
	source, err := f.metadata.AttachWorkspaceToProject(f.ctx, f.binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		if _, err := f.metadata.AttachWorkspaceToProject(f.ctx, f.binding.ProjectID, t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	task, err := f.store.CreateTask(f.ctx, workflowstore.CreateTaskRequest{
		ProjectID: f.binding.ProjectID, WorkflowID: &f.workflowID,
		Title: "Source", SourceWorkspaceID: source.WorkspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.startExistingTask(t, task)
	assertTaskSource(t, f, task, string(f.agentNodeID), source.WorkspaceID, source.CanonicalRoot, "available")
}

func TestTaskSourceRetainsDetachedWorkspaceWithClearedReference(t *testing.T) {
	f := newCurrentNodeViewFixture(t, false)
	source, err := f.metadata.AttachWorkspaceToProject(f.ctx, f.binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.store.CreateTask(f.ctx, workflowstore.CreateTaskRequest{
		ProjectID: f.binding.ProjectID, WorkflowID: &f.workflowID,
		Title: "Detached source", SourceWorkspaceID: source.WorkspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	definition, _, err := f.store.GetDefinition(f.ctx, f.workflowID)
	if err != nil {
		t.Fatal(err)
	}
	node := terminalNodeID(t, definition)
	if _, err := f.store.ManualMoveTask(f.ctx, workflowstore.ManualMoveRequest{TaskID: task.ID, TargetNodeID: node}); err != nil {
		t.Fatal(err)
	}
	blockers, err := f.metadata.UnlinkProjectWorkspace(f.ctx, f.binding.ProjectID, source.WorkspaceID)
	if err != nil || len(blockers) != 0 {
		t.Fatalf("detach: blockers=%+v err=%v", blockers, err)
	}
	row, err := f.metadata.Queries().GetTask(f.ctx, string(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if row.SourceWorkspaceID.Valid {
		t.Fatal("detach did not clear source reference")
	}
	assertTaskSource(t, f, task, string(node), source.WorkspaceID, source.CanonicalRoot, "unlinked")
}

func TestTaskSourceWithoutHistoricalFactsUsesDefault(t *testing.T) {
	f := newCurrentNodeViewFixture(t, false)
	started := f.startTask(t, "No source")
	if _, err := f.metadata.DB().ExecContext(f.ctx, "UPDATE tasks SET source_workspace_id = NULL, metadata_json = '{}' WHERE id = ?", started.task.ID); err != nil {
		t.Fatal(err)
	}
	assertTaskSource(t, f, started.task, string(f.agentNodeID), f.binding.WorkspaceID, f.binding.CanonicalRoot, "available")
}

func TestTaskSourceRejectsInvalidHistoricalFacts(t *testing.T) {
	f := newCurrentNodeViewFixture(t, false)
	started := f.startTask(t, "Invalid source")
	for _, payload := range []string{`{"source_workspace_snapshot":{}}`, `{"source_workspace_snapshot":null}`} {
		if _, err := f.metadata.DB().ExecContext(f.ctx, `UPDATE tasks SET source_workspace_id = NULL, metadata_json = ? WHERE id = ?`, payload, started.task.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.detail.GetTask(f.ctx, string(started.task.ID)); err == nil {
			t.Error("Task Detail accepted invalid historical facts")
		}
		if _, err := f.board.ListNodeCards(f.ctx, serverapi.WorkflowBoardNodeCardsListRequest{
			ProjectID: f.binding.ProjectID, WorkflowID: f.workflowID, NodeID: string(f.agentNodeID), PageSize: 20,
			LabelFilter: serverapi.WorkflowTaskLabelFilter{Kind: serverapi.WorkflowTaskLabelFilterKindNone},
		}); err == nil {
			t.Error("Board accepted invalid historical facts")
		}
	}
}

func assertTaskSource(t *testing.T, f currentNodeViewFixture, task workflowstore.TaskRecord, nodeID, workspaceID, root, availability string) {
	t.Helper()
	detail, err := f.detail.GetTask(f.ctx, string(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	page, err := f.board.ListNodeCards(f.ctx, serverapi.WorkflowBoardNodeCardsListRequest{
		ProjectID: f.binding.ProjectID, WorkflowID: f.workflowID, NodeID: nodeID, PageSize: 20,
		LabelFilter: serverapi.WorkflowTaskLabelFilter{Kind: serverapi.WorkflowTaskLabelFilterKindNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Cards) != 1 {
		t.Fatalf("cards = %+v", page.Cards)
	}
	for _, source := range []serverapi.ProjectWorkspaceSummary{detail.SourceWorkspace, page.Cards[0].SourceWorkspace} {
		if source.WorkspaceID != workspaceID || source.RootPath != root || source.Availability != availability {
			t.Errorf("source = %+v; want %s %s %s", source, workspaceID, root, availability)
		}
	}
	if detail.Project.DefaultWorkspaceID != f.binding.WorkspaceID {
		t.Fatalf("default = %s", detail.Project.DefaultWorkspaceID)
	}
}
