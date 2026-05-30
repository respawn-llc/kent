package workflowview

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"builder/server/metadata"
	"builder/server/metadata/sqlitegen"
	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/shared/clientui"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
)

func TestBoardAndTaskDetailUseDurableWorkflowMetadataOnly(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	comment, err := workflowStore.AddComment(ctx, task.ID, "note", "user", "nek")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	deletedComment, err := workflowStore.AddComment(ctx, task.ID, "deleted", "user", "nek")
	if err != nil {
		t.Fatalf("AddComment deleted: %v", err)
	}
	if err := workflowStore.DeleteComment(ctx, deletedComment.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if _, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "done"}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.WorkflowPicker) != 1 || len(board.Cards) != 0 || len(board.DonePreview) != 1 || board.NextPageToken != "" {
		t.Fatalf("board = %+v", board)
	}
	if len(board.Columns) < 2 || board.Columns[0].Node.Kind != string(workflow.NodeKindStart) {
		t.Fatalf("board column ordering = %+v", board.Columns)
	}
	foundDoneNodeTask := false
	for _, column := range board.Columns {
		if column.Node.Kind == string(workflow.NodeKindTerminal) && column.TaskCount == 1 {
			foundDoneNodeTask = true
		}
	}
	if !foundDoneNodeTask {
		t.Fatalf("board columns do not contain task on terminal node: %+v", board.Columns)
	}
	doneColumn := workflowViewColumnByKind(t, board, workflow.NodeKindTerminal)
	donePage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: doneColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards done: %v", err)
	}
	if len(donePage.Cards) != 1 || donePage.Cards[0].Status.Kind != "done" {
		t.Fatalf("done cards = %+v, want done task card", donePage.Cards)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !detail.Summary.Done || len(detail.Placements) != 3 || len(detail.Runs) != 1 || len(detail.Transitions) != 2 || len(detail.Comments) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Comments[0].ID != comment.ID || detail.Transitions[0].TransitionID != "start" || detail.Transitions[1].TransitionID != "done" || detail.Transitions[1].Edges[0].EdgeKey != "done" {
		t.Fatalf("detail history mismatch: %+v", detail)
	}
}

func TestBoardDoesNotAdvertiseHiddenDoneCardsWithoutFetchPath(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	for index := 0; index < 2; index++ {
		task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Done task", Body: "Body"})
		if err != nil {
			t.Fatalf("CreateTask %d: %v", index, err)
		}
		started, err := workflowStore.StartTask(ctx, task.ID)
		if err != nil {
			t.Fatalf("StartTask %d: %v", index, err)
		}
		if _, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "done"}); err != nil {
			t.Fatalf("CompleteRun %d: %v", index, err)
		}
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID, DonePreviewLimit: 1}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.DonePreview) != 1 {
		t.Fatalf("done preview count = %d, want 1", len(board.DonePreview))
	}
	if board.HasHiddenDoneCards {
		t.Fatalf("has hidden done cards = true without hidden-done fetch path")
	}
}

func TestBoardAndTaskDetailProjectTaskSourceWorkspaceAndBody(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	source, err := store.AttachWorkspaceToProject(ctx, binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatalf("AttachWorkspaceToProject source: %v", err)
	}
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: strings.Repeat("a", 120), SourceWorkspaceID: source.WorkspaceID})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	backlogColumn := workflowViewColumnByKind(t, board, workflow.NodeKindStart)
	backlogPage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: backlogColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards backlog: %v", err)
	}
	if len(backlogPage.Cards) != 1 || backlogPage.Cards[0].SourceWorkspace.WorkspaceID != source.WorkspaceID || backlogPage.Cards[0].BodyPreview == "" {
		t.Fatalf("node cards = %+v, want source workspace %q and body preview", backlogPage.Cards, source.WorkspaceID)
	}
	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Summary.SourceWorkspaceID != source.WorkspaceID || detail.SourceWorkspace.WorkspaceID != source.WorkspaceID || detail.Body != strings.Repeat("a", 120) {
		t.Fatalf("detail = %+v, want source workspace %q and body", detail, source.WorkspaceID)
	}
	if detail.Summary.BodyPreview == "" || detail.Summary.CreatedAtUnixMs == 0 || detail.Summary.UpdatedAtUnixMs == 0 {
		t.Fatalf("detail summary missing preview/timestamps: %+v", detail.Summary)
	}
}

func TestBoardAndTaskDetailProjectParallelBranchPlacements(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewFanoutWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	split, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "split", OutputValues: map[string]string{"summary": "plan"}})
	if err != nil {
		t.Fatalf("CompleteRun split: %v", err)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	for _, column := range board.Columns {
		if column.Node.Kind == string(workflow.NodeKindJoin) || column.Node.Key == "join" {
			t.Fatalf("board columns include hidden join node: %+v", board.Columns)
		}
	}
	planColumn := workflowViewColumnByKey(t, board, "plan")
	if len(planColumn.Node.OutputFields) != 1 || planColumn.Node.OutputFields[0].Name != "summary" || planColumn.Node.OutputFields[0].Description != "Plan summary." {
		t.Fatalf("plan board output fields = %+v, want derived downstream summary", planColumn.Node.OutputFields)
	}
	branchColumn := workflowViewColumnByKey(t, board, "impl_a")
	if len(branchColumn.Node.TransitionOutputFields) != 1 || branchColumn.Node.TransitionOutputFields[0].Name != "summary" || branchColumn.Node.TransitionOutputFields[0].Description != "Plan summary." {
		t.Fatalf("branch transition output fields = %+v, want target required inputs", branchColumn.Node.TransitionOutputFields)
	}
	branchPage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: branchColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards branch: %v", err)
	}
	if len(branchPage.Cards) != 1 || len(branchPage.Cards[0].ActiveNodeIDs) != 2 {
		t.Fatalf("board task summary = %+v, want two active branch nodes", branchPage.Cards)
	}
	activeBranchPlacements := 0
	for _, nodeID := range branchPage.Cards[0].ActiveNodeIDs {
		if nodeID != "" {
			activeBranchPlacements++
		}
	}
	if activeBranchPlacements != 2 {
		t.Fatalf("board active nodes = %+v, want two branch nodes", branchPage.Cards[0].ActiveNodeIDs)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	detailBranchPlacements := 0
	for _, placement := range detail.Placements {
		if placement.ParallelBatchTransitionID == string(split.TransitionID) && placement.ParallelBranchEdgeID != "" {
			detailBranchPlacements++
		}
	}
	if detailBranchPlacements != 2 {
		t.Fatalf("detail placements = %+v, want two branch placements with batch/branch ids", detail.Placements)
	}
}

func TestBoardGroupsHideJoinNodesAndJoinOnlyGroups(t *testing.T) {
	def := serverapi.WorkflowDefinition{
		NodeGroups: []serverapi.WorkflowNodeGroup{
			{GroupID: "group-visible", GroupKey: "visible", DisplayName: "Visible", SortOrder: 1},
			{GroupID: "group-join-only", GroupKey: "join_only", DisplayName: "Join Only", SortOrder: 2},
		},
		Nodes: []serverapi.WorkflowNode{
			{ID: "node-agent", GroupID: "group-visible", Kind: string(workflow.NodeKindAgent)},
			{ID: "node-join-visible-group", GroupID: "group-visible", Kind: string(workflow.NodeKindJoin)},
			{ID: "node-join-only", GroupID: "group-join-only", Kind: string(workflow.NodeKindJoin)},
		},
	}

	groups := boardGroups(def)
	if len(groups) != 1 || groups[0].GroupID != "group-visible" {
		t.Fatalf("groups = %+v, want only visible group", groups)
	}
	if len(groups[0].NodeIDs) != 1 || groups[0].NodeIDs[0] != "node-agent" {
		t.Fatalf("visible group node ids = %+v, want agent only", groups[0].NodeIDs)
	}
}

func TestBoardSelectsWorkflowAndReturnsPickerAndGroups(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	defaultWorkflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, defaultWorkflowID, true); err != nil {
		t.Fatalf("LinkWorkflow default: %v", err)
	}
	selected, err := workflowStore.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Selected Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow selected: %v", err)
	}
	if _, _, err := workflowStore.AddNodeGroup(ctx, workflowstore.NodeGroupRecord{WorkflowID: selected.ID, Key: "impl", DisplayName: "Implementation", SortOrder: 10}); err != nil {
		t.Fatalf("AddNodeGroup: %v", err)
	}
	def, _, err := workflowStore.GetDefinition(ctx, selected.ID)
	if err != nil {
		t.Fatalf("GetDefinition selected: %v", err)
	}
	start := workflowViewNodeByKind(t, def, workflow.NodeKindStart)
	done := workflowViewNodeByKind(t, def, workflow.NodeKindTerminal)
	agentID := workflow.NodeID("node-selected-agent-" + string(selected.ID))
	if _, err := workflowStore.AddNode(ctx, workflowstore.NodeRecord{ID: agentID, WorkflowID: selected.ID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", GroupKey: "impl", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode selected: %v", err)
	}
	startGroup := workflow.TransitionGroupID("group-selected-start-" + string(selected.ID))
	doneGroup := workflow.TransitionGroupID("group-selected-done-" + string(selected.ID))
	if _, err := workflowStore.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: startGroup, WorkflowID: selected.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddTransitionGroup start: %v", err)
	}
	if _, err := workflowStore.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-selected-start-" + string(selected.ID)), WorkflowID: selected.ID, TransitionGroupID: startGroup, Key: "start", TargetNodeID: agentID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := workflowStore.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: doneGroup, WorkflowID: selected.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := workflowStore.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-selected-done-" + string(selected.ID)), WorkflowID: selected.ID, TransitionGroupID: doneGroup, Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge done: %v", err)
	}
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, selected.ID, false); err != nil {
		t.Fatalf("LinkWorkflow selected: %v", err)
	}
	defaultTask, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: defaultWorkflowID, Title: "Default task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask default: %v", err)
	}
	selectedTask, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: selected.ID, Title: "Selected task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask selected: %v", err)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID, WorkflowID: string(selected.ID)}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if board.SelectedWorkflow.WorkflowID != string(selected.ID) {
		t.Fatalf("selected workflow = %+v, want %s", board.SelectedWorkflow, selected.ID)
	}
	if len(board.WorkflowPicker) != 2 || !board.WorkflowPicker[0].IsProjectDefault {
		t.Fatalf("picker = %+v, want default first and two workflows", board.WorkflowPicker)
	}
	selectedBacklog := workflowViewColumnByKind(t, board, workflow.NodeKindStart)
	selectedPage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(selected.ID), NodeID: selectedBacklog.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards selected: %v", err)
	}
	if len(selectedPage.Cards) != 1 || selectedPage.Cards[0].TaskID != string(selectedTask.ID) || selectedPage.Cards[0].TaskID == string(defaultTask.ID) {
		t.Fatalf("cards = %+v, want only selected workflow task %s", selectedPage.Cards, selectedTask.ID)
	}
	if len(board.Groups) != 1 || board.Groups[0].Key != "impl" || len(board.Groups[0].NodeIDs) != 1 || board.Groups[0].NodeIDs[0] != string(agentID) {
		t.Fatalf("groups = %+v, want implementation group with agent", board.Groups)
	}
	if board.Project.ProjectKey != "WOR" || board.GeneratedAtUnixMs == 0 {
		t.Fatalf("project/generated fields missing: %+v", board)
	}
}

func TestBoardPickerShowsOnlyActiveWorkflowLinks(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	defaultWorkflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, defaultWorkflowID, true); err != nil {
		t.Fatalf("LinkWorkflow default: %v", err)
	}
	removedWorkflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	removedLink, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, removedWorkflowID, false)
	if err != nil {
		t.Fatalf("LinkWorkflow removed: %v", err)
	}
	if result, err := workflowStore.UnlinkProjectWorkflow(ctx, removedLink.ID, ""); err != nil || !result.Unlinked {
		t.Fatalf("UnlinkProjectWorkflow: %v", err)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	var removed serverapi.WorkflowPickerItem
	for _, item := range board.WorkflowPicker {
		if item.WorkflowID == string(removedWorkflowID) {
			removed = item
			break
		}
	}
	if removed.WorkflowID != "" {
		t.Fatalf("removed workflow should not be in picker, got %+v", removed)
	}
}

func TestTaskDetailPrefersActiveWorkflowLink(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	link, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true)
	if err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Title: "Historical", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "done"}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Workflow.WorkflowID != string(workflowID) || !detail.Workflow.IsProjectDefault || !detail.Workflow.ValidForTaskCreation {
		t.Fatalf("workflow link = %+v, want active default link", detail.Workflow)
	}
	_ = link
}

func TestBoardColumnTaskCountsUseFullSelectedWorkflow(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	taskIDs := []string{}
	for _, title := range []string{"Task A", "Task B"} {
		task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Title: title, Body: "Body"})
		if err != nil {
			t.Fatalf("CreateTask %s: %v", title, err)
		}
		taskIDs = append(taskIDs, string(task.ID))
	}
	for _, taskID := range taskIDs {
		if _, err := store.DB().ExecContext(ctx, `UPDATE tasks SET updated_at_unix_ms = 123 WHERE id = ?`, taskID); err != nil {
			t.Fatalf("force task timestamp: %v", err)
		}
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID, PageSize: 1}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	if len(board.Cards) != 1 || board.NextPageToken == "" {
		t.Fatalf("legacy board page = %+v next=%q, want one card with next page", board.Cards, board.NextPageToken)
	}
	backlogCount := 0
	for _, column := range board.Columns {
		if column.IsBacklog {
			backlogCount = column.TaskCount
			break
		}
	}
	if backlogCount != 2 {
		t.Fatalf("backlog count = %d, want full selected workflow count 2", backlogCount)
	}
	backlogColumn := workflowViewColumnByKind(t, board, workflow.NodeKindStart)
	firstPage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: backlogColumn.Node.NodeID, PageSize: 1}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards first: %v", err)
	}
	if len(firstPage.Cards) != 1 || firstPage.NextPageToken == "" {
		t.Fatalf("first node page = %+v, want one card with next page", firstPage)
	}
	secondPage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: backlogColumn.Node.NodeID, PageSize: 1, PageToken: firstPage.NextPageToken}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards second: %v", err)
	}
	if len(secondPage.Cards) != 1 || secondPage.Cards[0].TaskID == firstPage.Cards[0].TaskID {
		t.Fatalf("second node page = %+v first=%+v, want distinct next card", secondPage, firstPage)
	}
	doneColumn := workflowViewColumnByKind(t, board, workflow.NodeKindTerminal)
	if _, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: doneColumn.Node.NodeID, PageSize: 1, PageToken: firstPage.NextPageToken}, workflow.StaticRoleResolver{"coder": true}); err == nil || !strings.Contains(err.Error(), "page_token") {
		t.Fatalf("ListBoardNodeCards with token from other node error = %v", err)
	}
}

func TestBoardNodeCardsArchiveCanceledTaskInDoneNode(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Title: "Canceled backlog", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := workflowStore.CancelTask(ctx, task.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	forceLegacyCanceledBacklogPlacement(t, ctx, store, task.ID, workflowID)
	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	backlogColumn := workflowViewColumnByKind(t, board, workflow.NodeKindStart)
	if backlogColumn.TaskCount != 0 {
		t.Fatalf("backlog count = %d, want canceled task archived out of backlog", backlogColumn.TaskCount)
	}
	backlogPage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: backlogColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards backlog: %v", err)
	}
	if len(backlogPage.Cards) != 0 {
		t.Fatalf("backlog node cards = %+v, want canceled task archived out of backlog", backlogPage.Cards)
	}
	doneColumn := workflowViewColumnByKind(t, board, workflow.NodeKindTerminal)
	if doneColumn.TaskCount != 1 {
		t.Fatalf("done count = %d, want canceled task counted in Done", doneColumn.TaskCount)
	}
	page, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: doneColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards done: %v", err)
	}
	if len(page.Cards) != 1 || page.Cards[0].TaskID != string(task.ID) || page.Cards[0].Status.Kind != "canceled" {
		t.Fatalf("done node cards = %+v, want canceled task", page.Cards)
	}
}

func TestBoardNodeCardsAllowRestartAfterDoneTaskResetToBacklog(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Title: "Restart", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if _, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "done"}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	def, _, err := workflowStore.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := workflowViewNodeByKind(t, def, workflow.NodeKindStart)
	if _, err := workflowStore.ManualMoveTask(ctx, workflowstore.ManualMoveRequest{TaskID: task.ID, TargetNodeID: start.ID}); err != nil {
		t.Fatalf("ManualMoveTask reset: %v", err)
	}
	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	backlogColumn := workflowViewColumnByKind(t, board, workflow.NodeKindStart)
	page, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: backlogColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards backlog: %v", err)
	}
	if len(page.Cards) != 1 || page.Cards[0].TaskID != string(task.ID) {
		t.Fatalf("backlog page = %+v, want reset task", page)
	}
	if !page.Cards[0].Actions.CanStart {
		t.Fatalf("reset backlog card actions = %+v, want restart allowed", page.Cards[0].Actions)
	}
}

func TestBoardNodeCardsDoNotArchiveCanceledTaskInAlternateTerminalNode(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	archiveNodeID := workflow.NodeID("node-archive-" + string(workflowID))
	if _, err := workflowStore.AddNode(ctx, workflowstore.NodeRecord{ID: archiveNodeID, WorkflowID: workflowID, Key: "archive", Kind: workflow.NodeKindTerminal, DisplayName: "Archive"}); err != nil {
		t.Fatalf("AddNode archive: %v", err)
	}
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Title: "Canceled backlog", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := workflowStore.CancelTask(ctx, task.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	forceLegacyCanceledBacklogPlacement(t, ctx, store, task.ID, workflowID)
	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	archiveColumn := workflowViewColumnByKey(t, board, "archive")
	if archiveColumn.TaskCount != 0 {
		t.Fatalf("archive count = %d, want no fallback canceled tasks", archiveColumn.TaskCount)
	}
	page, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: string(archiveNodeID)}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards archive: %v", err)
	}
	if len(page.Cards) != 0 {
		t.Fatalf("archive node cards = %+v, want no fallback canceled tasks", page.Cards)
	}
}

func TestBoardProjectsManualMoveTargetsFromServerPermissions(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	def, _, err := workflowStore.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	agent := workflowViewNodeByKind(t, def, workflow.NodeKindAgent)
	done := workflowViewNodeByKind(t, def, workflow.NodeKindTerminal)
	reviewID := workflow.NodeID("node-review-" + string(workflowID))
	if _, err := workflowStore.AddNode(ctx, workflowstore.NodeRecord{ID: reviewID, WorkflowID: workflowID, Key: "review", Kind: workflow.NodeKindAgent, DisplayName: "Review", SubagentRole: "coder", PromptTemplate: "Review {{.Inputs.summary}}.", InputFields: []workflow.InputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode review: %v", err)
	}
	reviewGroupID := workflow.TransitionGroupID("group-review-" + string(workflowID))
	if _, err := workflowStore.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: reviewGroupID, WorkflowID: workflowID, SourceNodeID: agent.ID, TransitionID: "review", DisplayName: "Review"}); err != nil {
		t.Fatalf("AddTransitionGroup review: %v", err)
	}
	if _, err := workflowStore.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-review-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: reviewGroupID, Key: "review", TargetNodeID: reviewID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge review: %v", err)
	}
	reviewDoneGroupID := workflow.TransitionGroupID("group-review-done-" + string(workflowID))
	if _, err := workflowStore.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: reviewDoneGroupID, WorkflowID: workflowID, SourceNodeID: reviewID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup review done: %v", err)
	}
	if _, err := workflowStore.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-review-done-" + string(workflowID)), WorkflowID: workflowID, TransitionGroupID: reviewDoneGroupID, Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge review done: %v", err)
	}
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := workflowStore.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	activeColumn := workflowViewColumnByKey(t, board, "agent")
	activePage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: activeColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards active: %v", err)
	}
	if len(activePage.Cards) != 1 {
		t.Fatalf("node cards = %+v, want one active card", activePage.Cards)
	}
	if got := activePage.Cards[0].Actions.ManualMoveTargetNodeIDs; len(got) != 1 || got[0] != string(done.ID) {
		t.Fatalf("manual move targets = %+v, want %s", got, done.ID)
	}
}

func TestManualMoveTargetsExcludeEdgesWithDerivedRequiredProvisionFields(t *testing.T) {
	def := serverapi.WorkflowDefinition{
		Workflow: serverapi.WorkflowRecord{ID: "workflow-1", Name: "Workflow"},
		Nodes: []serverapi.WorkflowNode{
			{ID: "node-agent", WorkflowID: "workflow-1", Key: "agent", Kind: string(workflow.NodeKindAgent), DisplayName: "Agent"},
			{ID: "node-review", WorkflowID: "workflow-1", Key: "review", Kind: string(workflow.NodeKindAgent), DisplayName: "Review"},
			{ID: "node-done", WorkflowID: "workflow-1", Key: "done", Kind: string(workflow.NodeKindTerminal), DisplayName: "Done"},
		},
		TransitionGroups: []serverapi.WorkflowTransitionGroup{
			{ID: "group-agent-review", WorkflowID: "workflow-1", SourceNodeID: "node-agent", TransitionID: "review", DisplayName: "Review"},
			{ID: "group-agent-done", WorkflowID: "workflow-1", SourceNodeID: "node-agent", TransitionID: "done", DisplayName: "Done"},
		},
		Edges: []serverapi.WorkflowEdge{
			{ID: "edge-review", WorkflowID: "workflow-1", TransitionGroupID: "group-agent-review", Key: "review", TargetNodeID: "node-review"},
			{ID: "edge-done", WorkflowID: "workflow-1", TransitionGroupID: "group-agent-done", Key: "done", TargetNodeID: "node-done"},
		},
		DerivedWiring: serverapi.WorkflowDerivedWiring{
			Edges: []serverapi.WorkflowDerivedEdgeWiring{
				{EdgeID: "edge-review", RequiredProvisionFields: []serverapi.WorkflowOutputField{{Name: "summary", Description: "Summary."}}},
				{EdgeID: "edge-done"},
			},
		},
	}

	targets := manualMoveTargetNodeIDs(
		def,
		[]sqlitegen.TaskNodePlacementRecord{{NodeID: "node-agent", State: "active"}},
		map[string]workflow.NodeKind{
			"node-agent":  workflow.NodeKindAgent,
			"node-review": workflow.NodeKindAgent,
			"node-done":   workflow.NodeKindTerminal,
		},
	)

	if len(targets) != 1 || targets[0] != "node-done" {
		t.Fatalf("manual move targets = %+v, want only terminal target", targets)
	}
}

func TestTaskDetailProjectsCancellationAndInterruptedRun(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := workflowStore.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if err := workflowStore.CancelTask(ctx, task.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Summary.CanceledAt == 0 || detail.Summary.CancelReason != "stop" {
		t.Fatalf("summary does not project cancellation: %+v", detail.Summary)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].InterruptedAtUnixMs == 0 || detail.Runs[0].InterruptionReason != "task_canceled" {
		t.Fatalf("runs do not project interruption: %+v", detail.Runs)
	}
	if detail.Actions.CanResume || detail.Actions.ResumeRunID != "" || detail.Actions.NeedsDetailForResume {
		t.Fatalf("canceled task should not expose resume actions: %+v", detail.Actions)
	}
}

func TestInterruptedTaskStatusUsesAttentionKind(t *testing.T) {
	ctx, _, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := workflowStore.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if err := workflowStore.InterruptRunGeneration(ctx, started.RunID, claimed.Generation, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration: %v", err)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	activeColumn := workflowViewColumnByKey(t, board, "agent")
	activePage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: activeColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards active: %v", err)
	}
	var card serverapi.WorkflowBoardTaskCard
	if len(activePage.Cards) == 1 {
		card = activePage.Cards[0]
	}
	if card.TaskID == "" || card.Status.Kind != "interrupted" || len(card.Status.AttentionTypes) != 1 || card.Status.AttentionTypes[0] != attentionKindInterruptedRun {
		t.Fatalf("board status = %+v", card.Status)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Status.Kind != "interrupted" || len(detail.Status.AttentionTypes) != 1 || detail.Status.AttentionTypes[0] != attentionKindInterruptedRun {
		t.Fatalf("detail status = %+v", detail.Status)
	}
	if len(detail.Attention) != 1 || detail.Attention[0].Kind != attentionKindInterruptedRun || detail.Attention[0].RunID != string(started.RunID) {
		t.Fatalf("detail attention = %+v", detail.Attention)
	}
}

func TestPendingApprovalTaskRemainsVisibleOnSourceBoardColumn(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	requireDoneTransitionApproval(t, ctx, store, workflowID)
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "BUI-7", Body: "Waiting approval"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	pending, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "done"})
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if pending.State != "pending_approval" {
		t.Fatalf("completion state = %q, want pending_approval", pending.State)
	}

	board, err := view.GetBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	sourceColumn := workflowViewColumnByKey(t, board, "agent")
	if sourceColumn.TaskCount != 1 {
		t.Fatalf("source column task count = %d, want pending approval task in source column: %+v", sourceColumn.TaskCount, board.Columns)
	}
	doneColumn := workflowViewColumnByKind(t, board, workflow.NodeKindTerminal)
	if doneColumn.TaskCount != 0 {
		t.Fatalf("done column task count = %d, want pending approval task not done yet", doneColumn.TaskCount)
	}
	sourcePage, err := view.ListBoardNodeCards(ctx, serverapi.WorkflowBoardNodeCardsListRequest{ProjectID: binding.ProjectID, WorkflowID: string(workflowID), NodeID: sourceColumn.Node.NodeID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListBoardNodeCards source: %v", err)
	}
	if len(sourcePage.Cards) != 1 {
		t.Fatalf("source cards = %+v, want pending approval task", sourcePage.Cards)
	}
	card := sourcePage.Cards[0]
	if card.ShortID != task.ShortID || card.Status.Kind != "waiting_approval" || len(card.Status.AttentionTypes) != 1 || card.Status.AttentionTypes[0] != "approval" {
		t.Fatalf("pending approval card = %+v", card)
	}
	if len(card.ActiveNodeIDs) != 1 || card.ActiveNodeIDs[0] != sourceColumn.Node.NodeID {
		t.Fatalf("pending approval active nodes = %+v, want source node %s", card.ActiveNodeIDs, sourceColumn.Node.NodeID)
	}
	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Status.Kind != "waiting_approval" || len(detail.Summary.ActiveNodeIDs) != 1 || detail.Summary.ActiveNodeIDs[0] != sourceColumn.Node.NodeID {
		t.Fatalf("task detail = %+v, want pending approval at source node %s", detail, sourceColumn.Node.NodeID)
	}
	byShortID, err := view.GetTaskByProjectShortID(ctx, binding.ProjectID, task.ShortID)
	if err != nil {
		t.Fatalf("GetTaskByProjectShortID: %v", err)
	}
	if byShortID.Status.Kind != "waiting_approval" || len(byShortID.Summary.ActiveNodeIDs) != 1 || byShortID.Summary.ActiveNodeIDs[0] != sourceColumn.Node.NodeID {
		t.Fatalf("task detail by short id = %+v, want pending approval at source node %s", byShortID, sourceColumn.Node.NodeID)
	}
	byGlobalShortID, err := view.GetTaskByShortID(ctx, task.ShortID)
	if err != nil {
		t.Fatalf("GetTaskByShortID: %v", err)
	}
	if byGlobalShortID.Status.Kind != "waiting_approval" || len(byGlobalShortID.Summary.ActiveNodeIDs) != 1 || byGlobalShortID.Summary.ActiveNodeIDs[0] != sourceColumn.Node.NodeID {
		t.Fatalf("task detail by global short id = %+v, want pending approval at source node %s", byGlobalShortID, sourceColumn.Node.NodeID)
	}
}

func TestTaskDetailProjectsWaitingAskRun(t *testing.T) {
	ctx, store, workflowStore, binding := newWorkflowViewTestContextStore(t)
	view, err := New(store, WithSessionTranscriptProvider(staticTranscriptProvider{pages: map[string]clientui.TranscriptPage{
		"session-view-waiting-ask": transcriptPageWithAsk("ask-view-1", "Waiting ask?"),
	}}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := workflowStore.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	sessionID := "session-view-waiting-ask"
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, launch_visible, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json) VALUES (?, ?, ?, ?, '', '', '', '', 1, 1, 0, 0, 0, 0, 1, '.', '{}', '{}', '{}', '{}')`, sessionID, binding.ProjectID, binding.WorkspaceID, "sessions/"+sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := workflowStore.AttachRunSession(ctx, started.RunID, claimed.Generation, sessionID); err != nil {
		t.Fatalf("AttachRunSession: %v", err)
	}
	if err := workflowStore.SetRunWaitingAsk(ctx, started.RunID, claimed.Generation, "ask-view-1"); err != nil {
		t.Fatalf("SetRunWaitingAsk: %v", err)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].WaitingAskID != "ask-view-1" || detail.Runs[0].SessionID != sessionID {
		t.Fatalf("runs do not project waiting ask: %+v", detail.Runs)
	}
}

func TestTaskDetailPendingQuestionFallsBackWhenTranscriptLookupFails(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := workflowStore.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	sessionID := "session-missing-question-transcript"
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, launch_visible, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json) VALUES (?, ?, ?, ?, '', '', '', '', 1, 1, 0, 0, 0, 0, 1, '.', '{}', '{}', '{}', '{}')`, sessionID, binding.ProjectID, binding.WorkspaceID, "sessions/"+sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := workflowStore.AttachRunSession(ctx, started.RunID, claimed.Generation, sessionID); err != nil {
		t.Fatalf("AttachRunSession: %v", err)
	}
	if err := workflowStore.SetRunWaitingAsk(ctx, started.RunID, claimed.Generation, "ask-missing-transcript"); err != nil {
		t.Fatalf("SetRunWaitingAsk: %v", err)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(detail.Attention) != 1 || detail.Attention[0].Kind != "question" || detail.Attention[0].AskID != "ask-missing-transcript" || detail.Attention[0].Message != pendingQuestionFallbackMessage {
		t.Fatalf("attention = %+v", detail.Attention)
	}
}

func TestTaskDetailProjectsGuiIdentityWorktreeStatusActionsAndAttention(t *testing.T) {
	ctx, store, workflowStore, binding := newWorkflowViewTestContextStore(t)
	view, err := New(store, WithSessionTranscriptProvider(staticTranscriptProvider{pages: map[string]clientui.TranscriptPage{
		"session-detail": transcriptPageWithAsk("ask-detail", "Which path should this task take?"),
	}}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	worktreeID := "worktree-detail"
	if err := store.Queries().UpsertWorktree(ctx, sqlitegen.UpsertWorktreeParams{ID: worktreeID, WorkspaceID: binding.WorkspaceID, CanonicalRootPath: t.TempDir(), BuilderManaged: 1, CreatedBranch: 1, GitMetadataJson: "{}", CreatedAtUnixMs: 1, UpdatedAtUnixMs: 2}); err != nil {
		t.Fatalf("UpsertWorktree: %v", err)
	}
	if _, err := store.Queries().UpdateTaskManagedWorktree(ctx, sqlitegen.UpdateTaskManagedWorktreeParams{ID: string(task.ID), ManagedWorktreeID: sql.NullString{String: worktreeID, Valid: true}, UpdatedAtUnixMs: 3}); err != nil {
		t.Fatalf("UpdateTaskManagedWorktree: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	claimed, err := workflowStore.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	sessionID := "session-detail"
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sessions (id, project_id, workspace_id, worktree_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, launch_visible, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json) VALUES (?, ?, ?, ?, ?, 'Task session', '', '', '', 1, 1, 0, 0, 0, 0, 1, 'subdir', '{}', '{}', '{}', '{}')`, sessionID, binding.ProjectID, binding.WorkspaceID, worktreeID, "sessions/"+sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := workflowStore.AttachRunSession(ctx, started.RunID, claimed.Generation, sessionID); err != nil {
		t.Fatalf("AttachRunSession: %v", err)
	}
	if err := workflowStore.SetRunWaitingAsk(ctx, started.RunID, claimed.Generation, "ask-detail"); err != nil {
		t.Fatalf("SetRunWaitingAsk: %v", err)
	}

	detail, err := view.GetTask(ctx, string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if detail.Project.ProjectID != binding.ProjectID || detail.Project.ProjectKey != "WOR" || detail.Workflow.WorkflowID != string(workflowID) || !detail.Workflow.IsProjectDefault {
		t.Fatalf("identity = project:%+v workflow:%+v", detail.Project, detail.Workflow)
	}
	if detail.ManagedWorktree == nil || detail.ManagedWorktree.WorktreeID != worktreeID || !detail.ManagedWorktree.BuilderManaged || detail.ManagedWorktree.CanonicalRoot == "" {
		t.Fatalf("managed worktree = %+v", detail.ManagedWorktree)
	}
	if detail.Status.Kind != "waiting_question" || !detail.Actions.CanInterrupt {
		t.Fatalf("status/actions = %+v/%+v", detail.Status, detail.Actions)
	}
	if len(detail.Attention) != 1 || detail.Attention[0].Kind != "question" || detail.Attention[0].AskID != "ask-detail" || detail.Attention[0].Message != "Which path should this task take?" {
		t.Fatalf("attention = %+v", detail.Attention)
	}
	if len(detail.Placements) < 2 || detail.Placements[1].NodeDisplayName == "" || detail.Placements[1].NodeKind == "" {
		t.Fatalf("placements missing node metadata: %+v", detail.Placements)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].SessionName != "Task session" || detail.Runs[0].Role != "coder" || detail.Runs[0].Status != "waiting_question" {
		t.Fatalf("runs = %+v", detail.Runs)
	}
}

func TestTaskActivityListMergesDurableTaskEventsAndPaginatesStably(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	comment, err := workflowStore.AddComment(ctx, task.ID, "note", "user", "nek")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := workflowStore.ReplaceComment(ctx, comment.ID, "edited note"); err != nil {
		t.Fatalf("ReplaceComment: %v", err)
	}
	claimed, err := workflowStore.ClaimRun(ctx, started.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if err := workflowStore.InterruptRunGeneration(ctx, started.RunID, claimed.Generation, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration: %v", err)
	}
	if err := workflowStore.CancelTask(ctx, task.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE task_comments SET updated_at_unix_ms = 111 WHERE id = ?`, comment.ID); err != nil {
		t.Fatalf("force comment timestamp: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE task_runs SET started_at_unix_ms = 111, interrupted_at_unix_ms = 111, updated_at_unix_ms = 111 WHERE id = ?`, string(started.RunID)); err != nil {
		t.Fatalf("force run timestamp: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE tasks SET canceled_at_unix_ms = 111, updated_at_unix_ms = 111 WHERE id = ?`, string(task.ID)); err != nil {
		t.Fatalf("force task timestamp: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE task_transitions SET created_at_unix_ms = 111, applied_at_unix_ms = 111 WHERE task_id = ?`, string(task.ID)); err != nil {
		t.Fatalf("force transition timestamp: %v", err)
	}

	first, err := view.ListTaskActivity(ctx, serverapi.WorkflowTaskActivityListRequest{TaskID: string(task.ID), PageSize: 2})
	if err != nil {
		t.Fatalf("ListTaskActivity first: %v", err)
	}
	newComment, err := workflowStore.AddComment(ctx, task.ID, "newer note", "user", "nek")
	if err != nil {
		t.Fatalf("AddComment newer: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE task_comments SET updated_at_unix_ms = 222 WHERE id = ?`, newComment.ID); err != nil {
		t.Fatalf("force newer comment timestamp: %v", err)
	}
	second, err := view.ListTaskActivity(ctx, serverapi.WorkflowTaskActivityListRequest{TaskID: string(task.ID), PageSize: 10, PageToken: first.NextPageToken})
	if err != nil {
		t.Fatalf("ListTaskActivity second: %v", err)
	}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		if seen[item.ActivityID] {
			t.Fatalf("duplicate activity item across pages: %s", item.ActivityID)
		}
		if item.ActivityID == "comment:"+newComment.ID {
			t.Fatalf("newer activity inserted between page fetches leaked into older page: %+v", item)
		}
		seen[item.ActivityID] = true
		kinds[item.Type] = true
	}
	for _, kind := range []string{"comment", "transition", "run_started", "run_interrupted", "task_canceled"} {
		if !kinds[kind] {
			t.Fatalf("activity kinds = %+v, missing %s; items=%+v/%+v", kinds, kind, first.Items, second.Items)
		}
	}
	if first.Items[0].OccurredAtUnixMs != 111 || first.Items[1].OccurredAtUnixMs != 111 || first.NextPageToken == "" {
		t.Fatalf("first page = %+v", first)
	}
}

func TestTaskActivityProjectsApprovalSnapshots(t *testing.T) {
	ctx, store, workflowStore, binding, view := newWorkflowViewTestContextService(t)
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	requireDoneTransitionApproval(t, ctx, store, workflowID)
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := workflowStore.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	pending, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: started.RunID, TransitionID: "done", Commentary: "needs approval", Actor: "agent"})
	if err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	resp, err := view.ListTaskActivity(ctx, serverapi.WorkflowTaskActivityListRequest{TaskID: string(task.ID)})
	if err != nil {
		t.Fatalf("ListTaskActivity: %v", err)
	}
	var transition serverapi.WorkflowTaskTransition
	hasRunCompleted := false
	for _, item := range resp.Items {
		if item.Type == "run_completed" && item.Run != nil && item.Run.ID == string(started.RunID) {
			hasRunCompleted = true
		}
		if item.Type == "transition" && item.Transition != nil && item.Transition.ID == string(pending.TransitionID) {
			transition = *item.Transition
		}
	}
	if !hasRunCompleted {
		t.Fatalf("activity missing run_completed item: %+v", resp.Items)
	}
	if transition.ID == "" || transition.SourceNodeID == "" || transition.SourceNodeDisplayName != "Agent" || transition.TransitionDisplayName != "Done" || transition.WorkflowRevisionSeen == 0 || transition.Actor != "agent" || transition.Commentary != "needs approval" || transition.AppliedAtUnixMs != 0 {
		t.Fatalf("transition snapshot = %+v", transition)
	}
	if len(transition.Edges) != 1 || !transition.Edges[0].RequiresApproval || transition.Edges[0].TargetNodeDisplayName == "" || len(transition.Edges[0].OutputRequirements) != 0 || transition.Edges[0].WorkflowRevisionSeen == 0 {
		t.Fatalf("edge snapshot = %+v", transition.Edges)
	}
}

func TestAttentionListProjectsApprovalQuestionAndInterruptedRun(t *testing.T) {
	ctx, store, workflowStore, binding := newWorkflowViewTestContextStore(t)
	view, err := New(store, WithSessionTranscriptProvider(staticTranscriptProvider{pages: map[string]clientui.TranscriptPage{
		"session-attention-question": transcriptPageWithAsk("ask-attention", "Attention ask?"),
	}}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	workflowID := createWorkflowViewValidWorkflow(t, ctx, workflowStore)
	if _, err := workflowStore.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	requireDoneTransitionApproval(t, ctx, store, workflowID)
	approvalTask, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Approval", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask approval: %v", err)
	}
	approvalStarted, err := workflowStore.StartTask(ctx, approvalTask.ID)
	if err != nil {
		t.Fatalf("StartTask approval: %v", err)
	}
	pendingApproval, err := workflowStore.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: approvalStarted.RunID, TransitionID: "done"})
	if err != nil {
		t.Fatalf("CompleteRun approval: %v", err)
	}
	if pendingApproval.State != "pending_approval" {
		t.Fatalf("approval completion = %+v, want pending_approval", pendingApproval)
	}
	questionTask, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Question", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask question: %v", err)
	}
	questionStarted, err := workflowStore.StartTask(ctx, questionTask.ID)
	if err != nil {
		t.Fatalf("StartTask question: %v", err)
	}
	questionClaimed, err := workflowStore.ClaimRun(ctx, questionStarted.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun question: %v", err)
	}
	sessionID := "session-attention-question"
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sessions (id, project_id, workspace_id, artifact_relpath, name, first_prompt_preview, input_draft, parent_session_id, created_at_unix_ms, updated_at_unix_ms, last_sequence, model_request_count, in_flight_step, agents_injected, launch_visible, cwd_relpath, continuation_json, locked_json, usage_state_json, metadata_json) VALUES (?, ?, ?, ?, '', '', '', '', 1, 1, 0, 0, 0, 0, 1, '.', '{}', '{}', '{}', '{}')`, sessionID, binding.ProjectID, binding.WorkspaceID, "sessions/"+sessionID); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := workflowStore.AttachRunSession(ctx, questionStarted.RunID, questionClaimed.Generation, sessionID); err != nil {
		t.Fatalf("AttachRunSession question: %v", err)
	}
	if err := workflowStore.SetRunWaitingAsk(ctx, questionStarted.RunID, questionClaimed.Generation, "ask-attention"); err != nil {
		t.Fatalf("SetRunWaitingAsk: %v", err)
	}
	interruptedTask, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Interrupted", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask interrupted: %v", err)
	}
	interruptedStarted, err := workflowStore.StartTask(ctx, interruptedTask.ID)
	if err != nil {
		t.Fatalf("StartTask interrupted: %v", err)
	}
	interruptedClaimed, err := workflowStore.ClaimRun(ctx, interruptedStarted.RunID, 0)
	if err != nil {
		t.Fatalf("ClaimRun interrupted: %v", err)
	}
	if err := workflowStore.InterruptRunGeneration(ctx, interruptedStarted.RunID, interruptedClaimed.Generation, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration: %v", err)
	}

	resp, err := view.ListAttention(ctx, serverapi.WorkflowAttentionListRequest{ProjectID: binding.ProjectID}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListAttention: %v", err)
	}
	kinds := map[string]serverapi.WorkflowAttentionItem{}
	for _, item := range resp.Items {
		kinds[item.Kind] = item
	}
	if kinds["approval"].TaskTransitionID != string(pendingApproval.TransitionID) || kinds["question"].AskID != "ask-attention" || kinds["interrupted_run"].RunID != string(interruptedStarted.RunID) {
		t.Fatalf("attention items = %+v", resp.Items)
	}
	taskResp, err := view.ListTaskAttention(ctx, serverapi.WorkflowTaskAttentionListRequest{TaskID: string(questionTask.ID)}, workflow.StaticRoleResolver{"coder": true})
	if err != nil {
		t.Fatalf("ListTaskAttention: %v", err)
	}
	if len(taskResp.Items) != 1 || taskResp.Items[0].Kind != "question" || taskResp.Items[0].TaskID != string(questionTask.ID) {
		t.Fatalf("task attention items = %+v", taskResp.Items)
	}
}

func TestPendingQuestionResolverSearchesBeforeOngoingTail(t *testing.T) {
	entries := make([]clientui.ChatEntry, 0, 650)
	for i := 0; i < 650; i++ {
		entry := clientui.ChatEntry{Role: "assistant", Text: "entry"}
		if i == 20 {
			entry = askTranscriptEntry("ask-old", "Question before tail?")
		}
		entries = append(entries, entry)
	}
	resolver := newPendingQuestionResolver(staticTranscriptProvider{pages: map[string]clientui.TranscriptPage{
		"session-long": {Entries: entries},
	}})

	question, err := resolver.Question(context.Background(), "session-long", "ask-old")
	if err != nil {
		t.Fatalf("Question: %v", err)
	}
	if question != "Question before tail?" {
		t.Fatalf("question = %q", question)
	}
}

func TestPendingQuestionResolverErrorsWhenQuestionMissingFromTranscript(t *testing.T) {
	resolver := newPendingQuestionResolver(staticTranscriptProvider{pages: map[string]clientui.TranscriptPage{
		"session-missing": transcriptPageWithAsk("other-ask", "Other?"),
	}})

	_, err := resolver.Question(context.Background(), "session-missing", "missing-ask")
	if err == nil || !strings.Contains(err.Error(), `pending question "missing-ask" was not found`) {
		t.Fatalf("missing question error = %v", err)
	}
}

func newWorkflowViewTestStore(t *testing.T) (*metadata.Store, *workflowstore.Store, metadata.Binding) {
	t.Helper()
	home := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := config.Load(workspaceRoot, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if err := metadataStore.SetProjectKey(context.Background(), binding.ProjectID, "WOR"); err != nil {
		t.Fatalf("SetProjectKey: %v", err)
	}
	workflowStore, err := workflowstore.New(metadataStore, workflowstore.WithRoleResolver(workflow.StaticRoleResolver{"coder": true}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	return metadataStore, workflowStore, binding
}

func newWorkflowViewTestContextStore(t *testing.T) (context.Context, *metadata.Store, *workflowstore.Store, metadata.Binding) {
	t.Helper()
	store, workflowStore, binding := newWorkflowViewTestStore(t)
	return context.Background(), store, workflowStore, binding
}

func newWorkflowViewTestService(t *testing.T) (*metadata.Store, *workflowstore.Store, metadata.Binding, *Service) {
	t.Helper()
	store, workflowStore, binding := newWorkflowViewTestStore(t)
	view, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return store, workflowStore, binding, view
}

func newWorkflowViewTestContextService(t *testing.T) (context.Context, *metadata.Store, *workflowstore.Store, metadata.Binding, *Service) {
	t.Helper()
	store, workflowStore, binding, view := newWorkflowViewTestService(t)
	return context.Background(), store, workflowStore, binding, view
}

func forceLegacyCanceledBacklogPlacement(t *testing.T, ctx context.Context, store *metadata.Store, taskID workflow.TaskID, workflowID workflow.WorkflowID) {
	t.Helper()
	if _, err := store.DB().ExecContext(ctx, `
DELETE FROM task_node_placements
WHERE task_id = ?
  AND node_id IN (SELECT id FROM workflow_nodes WHERE workflow_id = ? AND kind = 'terminal')`, string(taskID), string(workflowID)); err != nil {
		t.Fatalf("force legacy canceled terminal placement removal: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
UPDATE task_node_placements
SET state = 'active'
WHERE task_id = ?
  AND node_id IN (SELECT id FROM workflow_nodes WHERE workflow_id = ? AND kind = 'start')`, string(taskID), string(workflowID)); err != nil {
		t.Fatalf("force legacy canceled backlog placement: %v", err)
	}
}

func requireDoneTransitionApproval(t *testing.T, ctx context.Context, store *metadata.Store, workflowID workflow.WorkflowID) {
	t.Helper()
	if _, err := store.DB().ExecContext(ctx, `
UPDATE workflow_edges
SET requires_approval = 1
WHERE edge_key = 'done'
  AND EXISTS (
      SELECT 1
      FROM workflow_transition_groups tg
      JOIN workflow_nodes source ON source.id = tg.source_node_id
      WHERE tg.id = workflow_edges.transition_group_id
        AND source.workflow_id = ?
  )`, string(workflowID)); err != nil {
		t.Fatalf("require approval: %v", err)
	}
}

func createWorkflowViewValidWorkflow(t *testing.T, ctx context.Context, store *workflowstore.Store) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := workflowViewNodeByKind(t, def, workflow.NodeKindStart)
	done := workflowViewNodeByKind(t, def, workflow.NodeKindTerminal)
	agentID := workflow.NodeID("node-agent-" + string(created.ID))
	if _, err := store.AddNode(ctx, workflowstore.NodeRecord{ID: agentID, WorkflowID: created.ID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID("group-start-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddTransitionGroup start: %v", err)
	}
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-start-" + string(created.ID)), Key: "start", TargetNodeID: agentID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID("group-done-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-done-" + string(created.ID)), Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge done: %v", err)
	}
	return created.ID
}

func createWorkflowViewFanoutWorkflow(t *testing.T, ctx context.Context, store *workflowstore.Store) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Fanout Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := workflowViewNodeByKind(t, def, workflow.NodeKindStart)
	done := workflowViewNodeByKind(t, def, workflow.NodeKindTerminal)
	planID := workflow.NodeID("node-plan-" + string(created.ID))
	implAID := workflow.NodeID("node-impl-a-" + string(created.ID))
	implBID := workflow.NodeID("node-impl-b-" + string(created.ID))
	joinID := workflow.NodeID("node-join-" + string(created.ID))
	synthID := workflow.NodeID("node-synth-" + string(created.ID))
	for _, node := range []workflowstore.NodeRecord{
		{ID: planID, WorkflowID: created.ID, Key: "plan", Kind: workflow.NodeKindAgent, DisplayName: "Plan", SubagentRole: "coder", PromptTemplate: "Plan."},
		{ID: implAID, WorkflowID: created.ID, Key: "impl_a", Kind: workflow.NodeKindAgent, DisplayName: "Implement A", SubagentRole: "coder", PromptTemplate: "A.", InputFields: []workflow.InputField{{Name: "summary", Description: "Plan summary."}}},
		{ID: implBID, WorkflowID: created.ID, Key: "impl_b", Kind: workflow.NodeKindAgent, DisplayName: "Implement B", SubagentRole: "coder", PromptTemplate: "B.", InputFields: []workflow.InputField{{Name: "summary", Description: "Plan summary."}}},
		{ID: joinID, WorkflowID: created.ID, Key: "join", Kind: workflow.NodeKindJoin, DisplayName: "Join"},
		{ID: synthID, WorkflowID: created.ID, Key: "synth", Kind: workflow.NodeKindAgent, DisplayName: "Synthesize", SubagentRole: "coder", PromptTemplate: "Synthesize.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
	} {
		if _, err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("AddNode %s: %v", node.Key, err)
		}
	}
	startGroup := workflow.TransitionGroupID("group-start-" + string(created.ID))
	splitGroup := workflow.TransitionGroupID("group-split-" + string(created.ID))
	joinAGroup := workflow.TransitionGroupID("group-join-a-" + string(created.ID))
	joinBGroup := workflow.TransitionGroupID("group-join-b-" + string(created.ID))
	synthGroup := workflow.TransitionGroupID("group-join-synth-" + string(created.ID))
	doneGroup := workflow.TransitionGroupID("group-synth-done-" + string(created.ID))
	for _, group := range []workflowstore.TransitionGroupRecord{
		{ID: startGroup, WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"},
		{ID: splitGroup, WorkflowID: created.ID, SourceNodeID: planID, TransitionID: "split", DisplayName: "Split"},
		{ID: joinAGroup, WorkflowID: created.ID, SourceNodeID: implAID, TransitionID: "join", DisplayName: "Join"},
		{ID: joinBGroup, WorkflowID: created.ID, SourceNodeID: implBID, TransitionID: "join", DisplayName: "Join"},
		{ID: synthGroup, WorkflowID: created.ID, SourceNodeID: joinID, TransitionID: "done", DisplayName: "Done"},
		{ID: doneGroup, WorkflowID: created.ID, SourceNodeID: synthID, TransitionID: "done", DisplayName: "Done"},
	} {
		if _, err := store.AddTransitionGroup(ctx, group); err != nil {
			t.Fatalf("AddTransitionGroup %s: %v", group.TransitionID, err)
		}
	}
	for _, edge := range []workflowstore.EdgeRecord{
		{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: startGroup, Key: "start", TargetNodeID: planID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-split-a-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: splitGroup, Key: "split_a", TargetNodeID: implAID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-split-b-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: splitGroup, Key: "split_b", TargetNodeID: implBID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-join-a-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: joinAGroup, Key: "join_a", TargetNodeID: joinID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
		{ID: workflow.EdgeID("edge-join-b-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: joinBGroup, Key: "join_b", TargetNodeID: joinID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
		{ID: workflow.EdgeID("edge-join-synth-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: synthGroup, Key: "synth", TargetNodeID: synthID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-synth-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: doneGroup, Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
	} {
		if _, err := store.AddEdge(ctx, edge); err != nil {
			t.Fatalf("AddEdge %s: %v", edge.Key, err)
		}
	}
	return created.ID
}

func workflowViewNodeByKind(t *testing.T, def workflow.Definition, kind workflow.NodeKind) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if node.Kind == kind {
			return node
		}
	}
	t.Fatalf("missing node kind %q in %+v", kind, def.Nodes)
	return workflow.Node{}
}

func workflowViewColumnByKind(t *testing.T, board serverapi.WorkflowBoard, kind workflow.NodeKind) serverapi.WorkflowBoardColumn {
	t.Helper()
	for _, column := range board.Columns {
		if column.Node.Kind == string(kind) {
			return column
		}
	}
	t.Fatalf("missing board column kind %q in %+v", kind, board.Columns)
	return serverapi.WorkflowBoardColumn{}
}

func workflowViewColumnByKey(t *testing.T, board serverapi.WorkflowBoard, key string) serverapi.WorkflowBoardColumn {
	t.Helper()
	for _, column := range board.Columns {
		if column.Node.Key == key {
			return column
		}
	}
	t.Fatalf("missing board column key %q in %+v", key, board.Columns)
	return serverapi.WorkflowBoardColumn{}
}

func TestWorkflowViewRejectsMissingIDs(t *testing.T) {
	store, _, _ := newWorkflowViewTestStore(t)
	view, err := New(store)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := view.GetBoard(context.Background(), serverapi.WorkflowBoardRequest{ProjectID: " "}, workflow.StaticRoleResolver{}); err == nil || !strings.Contains(err.Error(), "project_id") {
		t.Fatalf("GetBoard missing id error = %v", err)
	}
	if _, err := view.GetBoard(context.Background(), serverapi.WorkflowBoardRequest{ProjectID: "project-1", PageSize: -1}, workflow.StaticRoleResolver{}); err == nil || !strings.Contains(err.Error(), "page_size") {
		t.Fatalf("GetBoard negative page size error = %v", err)
	}
	if _, err := view.GetTask(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("GetTask missing id error = %v", err)
	}
}

type staticTranscriptProvider struct {
	pages map[string]clientui.TranscriptPage
}

func (p staticTranscriptProvider) GetSessionTranscriptPage(_ context.Context, req serverapi.SessionTranscriptPageRequest) (serverapi.SessionTranscriptPageResponse, error) {
	entries := append([]clientui.ChatEntry(nil), p.pages[strings.TrimSpace(req.SessionID)].Entries...)
	total := len(entries)
	offset := req.Offset
	limit := req.Limit
	if req.Window == clientui.TranscriptWindowOngoingTail {
		offset = total - 500
		if offset < 0 {
			offset = 0
		}
		limit = total - offset
	}
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 || offset+limit > total {
		limit = total - offset
	}
	page := clientui.TranscriptPage{TotalEntries: total, Offset: offset, NextOffset: offset + limit, Entries: entries[offset : offset+limit]}
	page.HasMore = page.NextOffset < total
	return serverapi.SessionTranscriptPageResponse{Transcript: page}, nil
}

func transcriptPageWithAsk(askID string, question string) clientui.TranscriptPage {
	return clientui.TranscriptPage{Entries: []clientui.ChatEntry{askTranscriptEntry(askID, question)}}
}

func askTranscriptEntry(askID string, question string) clientui.ChatEntry {
	return clientui.ChatEntry{
		Role:       "tool_call",
		ToolCallID: askID,
		ToolCall:   &clientui.ToolCallMeta{ToolName: string(toolspec.ToolAskQuestion), Question: question},
	}
}
