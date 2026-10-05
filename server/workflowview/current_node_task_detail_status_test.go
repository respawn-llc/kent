package workflowview

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"testing"

	"core/internal/testharness/workflowfixture"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"core/shared/protoapi"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func TestTaskDetailDependenciesUseOneStatusObservation(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	calls := 0
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		countingTaskStatusLiveObservationSource{
			source: fixture.quiescence,
			calls:  &calls,
		},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	dependencies, err := NewTaskDependencies(fixture.metadata, projection, fixture.dependencyCounter)
	if err != nil {
		t.Fatalf("NewTaskDependencies: %v", err)
	}
	detail, err := NewTaskDetail(fixture.metadata, projection, dependencies)
	if err != nil {
		t.Fatalf("NewTaskDetail: %v", err)
	}
	blocker := createViewTask(t, fixture, "Blocker")
	blocked := createViewTask(t, fixture, "Blocked")
	if _, err := fixture.store.AddTaskDependency(fixture.ctx, workflowstore.TaskDependencyAddRequest{
		BlockerTaskID: blocker.ID,
		BlockedTaskID: blocked.ID,
	}); err != nil {
		t.Fatalf("AddTaskDependency: %v", err)
	}

	projected, err := detail.GetTask(fixture.ctx, string(blocked.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	if calls != 1 {
		t.Fatalf("live observations for one Task detail request = %d, want 1", calls)
	}
	if projected.Dependencies.BlockerCount != 1 ||
		projected.Dependencies.UnsatisfiedBlockerCount != 1 ||
		len(projected.Dependencies.Directions) != 2 {
		t.Fatalf("detail dependencies = %+v", projected.Dependencies)
	}
}

func TestTaskDetailProjectsCurrentNodeAndDirectRetainedSession(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Detail task")
	sessionID := fixture.bindCurrentNodeSession(t, started)
	fixture.quiescence.blocked[started.task.ID] = true

	detail, err := fixture.detail.GetTask(fixture.ctx, string(started.task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	if detail.Summary.Id != string(started.task.ID) ||
		len(detail.CurrentNodes) != 1 ||
		detail.CurrentNodes[0].NodeId != string(fixture.agentNodeID) ||
		detail.CurrentNodes[0].SessionId == nil ||
		*detail.CurrentNodes[0].SessionId != sessionID.String() ||
		detail.RetainedSessionCount != 1 ||
		detail.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE ||
		detail.Actions.CanDelete ||
		detail.Actions.CanStart {
		t.Fatalf("task detail = %+v, want Current Node and directly retained Session", detail)
	}
	delete(fixture.quiescence.blocked, started.task.ID)
	quiescent, err := fixture.detail.GetTask(fixture.ctx, string(started.task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask quiescent: %v", err)
	}
	if !quiescent.Actions.CanDelete {
		t.Fatalf("quiescent task actions = %+v, want can_delete true", quiescent.Actions)
	}
}

func TestTaskDetailMaterializesAndOrdersLiveScripts(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Live Scripts")
	scriptNodeIDs := []workflow.NodeID{
		workflow.NodeID("node-" + uuid.NewString()),
		workflow.NodeID("node-" + uuid.NewString()),
	}
	slices.Sort(scriptNodeIDs)
	scriptPaths := []string{"scripts/a.sh", "scripts/b.sh"}
	for _, script := range scriptPaths {
		path := filepath.Join(fixture.binding.CanonicalRoot, script)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	joinNodeID := workflow.NodeID("node-" + uuid.NewString())
	splitAEdgeID := workflow.EdgeID("edge-" + uuid.NewString())
	splitBEdgeID := workflow.EdgeID("edge-" + uuid.NewString())
	joinAEdgeID := workflow.EdgeID("edge-" + uuid.NewString())
	joinBEdgeID := workflow.EdgeID("edge-" + uuid.NewString())
	finishEdgeID := workflow.EdgeID("edge-" + uuid.NewString())
	nodes := []workflowstore.NodeRecord{
		{ID: scriptNodeIDs[0], WorkflowID: fixture.workflowID, Key: "script_a", Kind: workflow.NodeKindScript, DisplayName: "Script A", ScriptPath: scriptPaths[0]},
		{ID: scriptNodeIDs[1], WorkflowID: fixture.workflowID, Key: "script_b", Kind: workflow.NodeKindScript, DisplayName: "Script B", ScriptPath: scriptPaths[1]},
		{ID: joinNodeID, WorkflowID: fixture.workflowID, Key: "join", Kind: workflow.NodeKindJoin, DisplayName: "Join", JoinInputProviders: []workflow.JoinInputProvider{{InputName: "joined", ProviderEdgeID: joinAEdgeID}}},
	}
	groupIDs := []workflow.TransitionGroupID{
		workflow.TransitionGroupID("group-" + uuid.NewString()),
		workflow.TransitionGroupID("group-" + uuid.NewString()),
		workflow.TransitionGroupID("group-" + uuid.NewString()),
		workflow.TransitionGroupID("group-" + uuid.NewString()),
	}
	groups := []workflowstore.TransitionGroupRecord{
		{ID: groupIDs[0], WorkflowID: fixture.workflowID, SourceNodeID: fixture.agentNodeID, TransitionID: "split", DisplayName: "Split"},
		{ID: groupIDs[1], WorkflowID: fixture.workflowID, SourceNodeID: scriptNodeIDs[0], TransitionID: "join_a", DisplayName: "Join"},
		{ID: groupIDs[2], WorkflowID: fixture.workflowID, SourceNodeID: scriptNodeIDs[1], TransitionID: "join_b", DisplayName: "Join"},
		{ID: groupIDs[3], WorkflowID: fixture.workflowID, SourceNodeID: joinNodeID, TransitionID: "finish", DisplayName: "Done"},
	}
	workflowfixture.SaveStoreGraph(t, fixture.ctx, fixture.store, fixture.workflowID, func(definition workflow.Definition, request *workflowstore.WorkflowGraphSaveRequest) {
		terminalNodeID := currentNodeViewNodeIDByKind(t, definition, workflow.NodeKindTerminal)
		request.Nodes = append(request.Nodes, nodes...)
		request.TransitionGroups = append(request.TransitionGroups, groups...)
		request.Edges = append(request.Edges,
			workflowstore.EdgeRecord{ID: splitAEdgeID, WorkflowID: fixture.workflowID, TransitionGroupID: groupIDs[0], Key: "split_a", TargetNodeID: scriptNodeIDs[0], AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession},
			workflowstore.EdgeRecord{ID: splitBEdgeID, WorkflowID: fixture.workflowID, TransitionGroupID: groupIDs[0], Key: "split_b", TargetNodeID: scriptNodeIDs[1], AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession},
			workflowstore.EdgeRecord{ID: joinAEdgeID, WorkflowID: fixture.workflowID, TransitionGroupID: groupIDs[1], Key: "join_a", TargetNodeID: joinNodeID, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession, Parameters: []workflow.Parameter{{Key: "joined", Description: "Joined output.", Purpose: workflow.ParameterPurposeOrdinary}}},
			workflowstore.EdgeRecord{ID: joinBEdgeID, WorkflowID: fixture.workflowID, TransitionGroupID: groupIDs[2], Key: "join_b", TargetNodeID: joinNodeID, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession},
			workflowstore.EdgeRecord{ID: finishEdgeID, WorkflowID: fixture.workflowID, TransitionGroupID: groupIDs[3], Key: "finish", TargetNodeID: terminalNodeID, AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, ContextMode: workflow.ContextModeNewSession},
		)
	})
	split, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
		Source:       started.currentNode,
		TransitionID: "split",
	})
	if err != nil || len(split.Mutation.Created) != 2 {
		t.Fatalf("CompleteCurrentNode Script fanout: result=%+v err=%v", split, err)
	}
	executions := make([]sessionruntime.TaskExecution, 0, len(split.Mutation.Created))
	for _, currentNode := range split.Mutation.Created {
		path := scriptPaths[0]
		if currentNode.Reference.NodeID == scriptNodeIDs[1] {
			path = scriptPaths[1]
		}
		executions = append(executions, sessionruntime.TaskExecution{
			Ref: sessionruntime.WorkflowExecutionRef{
				ProjectID:   fixture.binding.ProjectID,
				WorkflowID:  fixture.workflowID,
				CurrentNode: currentNode.Reference,
			},
			Script: &sessionruntime.TaskScriptExecutionTarget{Path: path},
		})
	}
	slices.Reverse(executions)

	detail := taskDetailWithObservation(t, fixture, workflowexecution.WorkflowTaskExecutionObservation{
		Executions: map[workflow.TaskID]sessionruntime.TaskExecutionSnapshot{
			started.task.ID: {Executions: executions},
		},
		Quiescence: map[workflow.TaskID]bool{started.task.ID: false},
	})
	projected, err := detail.GetTask(fixture.ctx, string(started.task.ID))
	if err != nil {
		t.Fatalf("TaskDetail.GetTask: %v", err)
	}
	branchA := "split_a"
	branchB := "split_b"
	want := []*taskpb.CurrentScript{
		{CurrentNode: &taskpb.AttentionCurrentNode{NodeId: string(scriptNodeIDs[0]), TransitionBranchKey: &branchA}, Path: scriptPaths[0]},
		{CurrentNode: &taskpb.AttentionCurrentNode{NodeId: string(scriptNodeIDs[1]), TransitionBranchKey: &branchB}, Path: scriptPaths[1]},
	}
	if projected.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_RUNNING ||
		!projected.Actions.CanInterrupt ||
		len(projected.LiveSessions) != 0 ||
		!reflect.DeepEqual(projected.CurrentScripts, want) {
		t.Fatalf("live Script detail = %+v, want ordered Scripts %+v", projected, want)
	}
}

func TestTaskDetailProjectsLiveAgentStates(t *testing.T) {
	tests := []struct {
		name          string
		queued        bool
		approval      bool
		wantStatus    taskpb.TaskStatusKind
		wantAttention int
		wantInterrupt bool
	}{
		{name: "queued", queued: true, wantStatus: taskpb.TaskStatusKind_TASK_STATUS_KIND_QUEUED},
		{name: "running", wantStatus: taskpb.TaskStatusKind_TASK_STATUS_KIND_RUNNING, wantInterrupt: true},
		{name: "Approval", approval: true, wantStatus: taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL, wantAttention: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCurrentNodeViewFixture(t, false)
			started := fixture.startTask(t, "Live "+test.name)
			sessionID := fixture.bindCurrentNodeSession(t, started)
			execution := sessionruntime.TaskExecution{
				Ref: sessionruntime.WorkflowExecutionRef{
					ProjectID:   fixture.binding.ProjectID,
					WorkflowID:  fixture.workflowID,
					CurrentNode: started.currentNode,
				},
				Agent:  &sessionruntime.TaskAgentExecutionTarget{SessionID: sessionID},
				Queued: test.queued,
			}
			if test.approval {
				execution.PendingPrompts = []sessionruntime.PendingPromptReference{{
					ToolCallID: "approval",
					Kind:       sessionruntime.PendingPromptKindSessionApproval,
				}}
			}
			detail := taskDetailWithObservation(t, fixture, workflowexecution.WorkflowTaskExecutionObservation{
				Executions: map[workflow.TaskID]sessionruntime.TaskExecutionSnapshot{
					started.task.ID: {Executions: []sessionruntime.TaskExecution{execution}},
				},
				Quiescence: map[workflow.TaskID]bool{started.task.ID: false},
			})
			projected, err := detail.GetTask(fixture.ctx, string(started.task.ID))
			if err != nil {
				t.Fatalf("TaskDetail.GetTask: %v", err)
			}
			if projected.Status.Kind != test.wantStatus ||
				int(projected.AttentionCount) != test.wantAttention ||
				projected.Actions.CanInterrupt != test.wantInterrupt ||
				len(projected.LiveSessions) != 1 ||
				projected.LiveSessions[0].SessionId != sessionID.String() ||
				len(projected.CurrentScripts) != 0 {
				t.Fatalf("live Agent detail = %+v", projected)
			}
		})
	}
}

func taskDetailWithObservation(
	t *testing.T,
	fixture currentNodeViewFixture,
	observation workflowexecution.WorkflowTaskExecutionObservation,
) *TaskDetail {
	t.Helper()
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		staticTaskStatusLiveObservationSource{observation: observation},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	dependencies, err := NewTaskDependencies(fixture.metadata, projection, fixture.dependencyCounter)
	if err != nil {
		t.Fatalf("NewTaskDependencies: %v", err)
	}
	detail, err := NewTaskDetail(fixture.metadata, projection, dependencies)
	if err != nil {
		t.Fatalf("NewTaskDetail: %v", err)
	}
	return detail
}

func TestTaskListPaginatesStableSortAndEvaluatesOffsetAgainstEachRequest(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := []startedCurrentNodeViewTask{
		fixture.startTask(t, "List A"),
		fixture.startTask(t, "List B"),
		fixture.startTask(t, "List C"),
	}
	for _, task := range started {
		fixture.setTaskUpdatedAt(t, task.task.ID, 2_000)
	}
	for _, task := range started[1:] {
		if err := fixture.store.InterruptCurrentNode(
			fixture.ctx,
			task.currentNode,
			workflow.CurrentNodeInterruptionReason("server_restart"),
			workflow.CurrentNodeInterruptionDetail{Code: "restart"},
		); err != nil {
			t.Fatalf("InterruptCurrentNode: %v", err)
		}
	}
	projectID := fixture.binding.ProjectID
	workflowID := fixture.workflowID
	limit := int32(1)
	request := &taskpb.ListRequest{
		ProjectId:   &projectID,
		WorkflowId:  proto.String(workflowID.String()),
		LabelFilter: noLabelFilter(),
		Sort: []*taskpb.ListSort{{
			Field:     taskpb.ListSortField_LIST_SORT_FIELD_UPDATED,
			Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_DESC,
		}},
		Limit: &limit,
	}
	var got []string
	for {
		page, err := fixture.tasks.List(fixture.ctx, request)
		if err != nil {
			t.Fatalf("TaskList.List stable page: %v", err)
		}
		if len(page.Tasks) != 1 {
			t.Fatalf("stable page = %+v, want one Task", page.Tasks)
		}
		got = append(got, page.Tasks[0].TaskId)
		if page.NextOffset == nil {
			break
		}
		request.Offset = page.NextOffset
	}
	want := []string{
		string(started[0].task.ID),
		string(started[1].task.ID),
		string(started[2].task.ID),
	}
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("stable pagination order = %v, want %v", got, want)
	}

	offset := int32(1)
	filtered, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId:   &projectID,
		WorkflowId:  proto.String(workflowID.String()),
		StatusKinds: []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED},
		LabelFilter: noLabelFilter(),
		Sort: []*taskpb.ListSort{{
			Field:     taskpb.ListSortField_LIST_SORT_FIELD_TITLE,
			Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC,
		}},
		Offset: &offset,
		Limit:  &limit,
	})
	if err != nil {
		t.Fatalf("TaskList.List changed filter: %v", err)
	}
	if len(filtered.Tasks) != 1 || filtered.Tasks[0].TaskId != string(started[2].task.ID) {
		t.Fatalf("changed-filter offset page = %+v, want second interrupted Task", filtered.Tasks)
	}
}

func TestTaskListSortsNumericShortIDAsSeventhSelector(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	tasks := make([]startedCurrentNodeViewTask, 0, 11)
	for range 11 {
		task := fixture.startTask(t, "Same title")
		tasks = append(tasks, task)
		if _, err := fixture.metadata.DB().ExecContext(
			fixture.ctx,
			`UPDATE tasks SET created_at_unix_ms = ?, updated_at_unix_ms = ? WHERE id = ?`,
			int64(1_000),
			int64(1_000),
			string(task.task.ID),
		); err != nil {
			t.Fatalf("tie Task timestamps: %v", err)
		}
	}
	projectID := fixture.binding.ProjectID
	workflowID := fixture.workflowID
	limit := int32(3)
	offset := int32(8)
	sortSelectors := []*taskpb.ListSort{
		{Field: taskpb.ListSortField_LIST_SORT_FIELD_LABELS, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
		{Field: taskpb.ListSortField_LIST_SORT_FIELD_CREATED, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
		{Field: taskpb.ListSortField_LIST_SORT_FIELD_UPDATED, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
		{Field: taskpb.ListSortField_LIST_SORT_FIELD_STATUS, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
		{Field: taskpb.ListSortField_LIST_SORT_FIELD_COLUMN, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
		{Field: taskpb.ListSortField_LIST_SORT_FIELD_TITLE, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
		{Field: taskpb.ListSortField_LIST_SORT_FIELD_SHORT_ID, Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC},
	}
	request := &taskpb.ListRequest{
		ProjectId:   &projectID,
		WorkflowId:  proto.String(workflowID.String()),
		LabelFilter: noLabelFilter(),
		Sort:        sortSelectors,
		Offset:      &offset,
		Limit:       &limit,
	}
	ascending, err := fixture.tasks.List(fixture.ctx, request)
	if err != nil {
		t.Fatalf("TaskList.List ascending: %v", err)
	}
	if !slices.Equal(workflowTaskListItemIDs(ascending.Tasks), []string{
		string(tasks[8].task.ID),
		string(tasks[9].task.ID),
		string(tasks[10].task.ID),
	}) {
		t.Fatalf("ascending Short ID page = %+v, want 9/10/11", ascending.Tasks)
	}

	request.Offset = nil
	request.Sort[6].Direction = taskpb.ListSortDirection_LIST_SORT_DIRECTION_DESC
	descending, err := fixture.tasks.List(fixture.ctx, request)
	if err != nil {
		t.Fatalf("TaskList.List descending: %v", err)
	}
	if !slices.Equal(workflowTaskListItemIDs(descending.Tasks), []string{
		string(tasks[10].task.ID),
		string(tasks[9].task.ID),
		string(tasks[8].task.ID),
	}) {
		t.Fatalf("descending Short ID page = %+v, want 11/10/9", descending.Tasks)
	}
}

func workflowTaskListItemIDs(tasks []*taskpb.ListItem) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.TaskId)
	}
	return ids
}

func TestProjectWideTaskListPreservesCardinalityOwnedRowVisibility(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	projectID := fixture.binding.ProjectID
	list := func(offset *int32, limit int32) *taskpb.ListSuccess {
		t.Helper()
		page, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
			ProjectId:   &projectID,
			LabelFilter: noLabelFilter(),
			Offset:      offset,
			Limit:       &limit,
		})
		if err != nil {
			t.Fatalf("TaskList.List: %v", err)
		}
		return page
	}

	none := list(nil, 20)
	if len(none.Tasks) != 0 ||
		none.MatchingWorkflowCardinality != taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_NONE {
		t.Fatalf("empty project-wide page = %+v", none)
	}

	task := fixture.createBacklogTask(t, "Only workflow task")
	one := list(nil, 20)
	if len(one.Tasks) != 1 ||
		one.Tasks[0].TaskId != string(task.ID) ||
		one.Tasks[0].WorkflowId != fixture.workflowID.String() ||
		one.Tasks[0].WorkflowName == nil ||
		one.Tasks[0].ColumnKeys != nil ||
		one.MatchingWorkflowCardinality != taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_ONE {
		t.Fatalf("one-workflow project-wide page = %+v", one)
	}

	secondWorkflowID := currentNodeViewWorkflow(t, fixture.store, false)
	if _, err := fixture.store.LinkWorkflow(fixture.ctx, fixture.binding.ProjectID, secondWorkflowID, false); err != nil {
		t.Fatalf("LinkWorkflow second workflow: %v", err)
	}
	if _, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  fixture.binding.ProjectID,
		WorkflowID: &secondWorkflowID,
		Title:      "Second workflow task",
	}); err != nil {
		t.Fatalf("CreateTask second workflow: %v", err)
	}
	thirdWorkflowID := currentNodeViewWorkflow(t, fixture.store, false)
	if _, err := fixture.store.LinkWorkflow(fixture.ctx, fixture.binding.ProjectID, thirdWorkflowID, false); err != nil {
		t.Fatalf("LinkWorkflow third workflow: %v", err)
	}
	if _, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  fixture.binding.ProjectID,
		WorkflowID: &thirdWorkflowID,
		Title:      "Third workflow task",
	}); err != nil {
		t.Fatalf("CreateTask third workflow: %v", err)
	}
	multiple := list(nil, 20)
	if len(multiple.Tasks) != 3 ||
		multiple.MatchingWorkflowCardinality != taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_MULTIPLE {
		t.Fatalf("multiple-workflow project-wide page = %+v", multiple)
	}
	for _, item := range multiple.Tasks {
		if item.WorkflowName == nil || item.ColumnKeys != nil {
			t.Fatalf("multiple-workflow project-wide item = %+v", item)
		}
	}

	offset := int32(4)
	beyondEnd := list(&offset, 1)
	if len(beyondEnd.Tasks) != 0 ||
		beyondEnd.NextOffset != nil ||
		beyondEnd.MatchingWorkflowCardinality != taskpb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_MULTIPLE {
		t.Fatalf("multiple-workflow beyond-end page = %+v", beyondEnd)
	}
}

func TestTaskListDefaultSortUsesCurrentStatusBeforeActivity(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	active := fixture.startTask(t, "Active")
	interrupted := fixture.startTask(t, "Interrupted")
	if err := fixture.store.InterruptCurrentNode(
		fixture.ctx,
		interrupted.currentNode,
		workflow.CurrentNodeInterruptionReason("server_restart"),
		workflow.CurrentNodeInterruptionDetail{Code: "restart"},
	); err != nil {
		t.Fatalf("InterruptCurrentNode: %v", err)
	}
	backlog := fixture.createBacklogTask(t, "Backlog")
	fixture.setTaskUpdatedAt(t, active.task.ID, 3_000)
	fixture.setTaskUpdatedAt(t, interrupted.task.ID, 1_000)
	fixture.setTaskUpdatedAt(t, backlog.ID, 2_000)
	projectID := fixture.binding.ProjectID
	workflowID := fixture.workflowID
	limit := int32(20)

	list, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId:   &projectID,
		WorkflowId:  proto.String(workflowID.String()),
		LabelFilter: noLabelFilter(),
		Limit:       &limit,
	})
	if err != nil {
		t.Fatalf("TaskList.List: %v", err)
	}
	got := make([]taskpb.TaskStatusKind, 0, len(list.Tasks))
	for _, task := range list.Tasks {
		got = append(got, task.Status.Kind)
	}
	want := []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED, taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG, taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE}
	if !slices.Equal(got, want) {
		t.Fatalf("default Task List status order = %v, want %v", got, want)
	}
	activeItem := list.Tasks[2]
	if activeItem.WorkflowName != nil ||
		activeItem.ColumnKeys == nil ||
		!slices.Equal(activeItem.ColumnKeys.Values, []string{"agent"}) {
		t.Fatalf("Workflow-narrowed active Task = %+v, want ordered Current Node column", activeItem)
	}
}

func TestProjectTaskGroupCountsObserveLiveStatusesAcrossLinkedWorkflows(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	active := fixture.startTask(t, "Active")
	fixture.createBacklogTask(t, "Backlog")
	secondWorkflowID := currentNodeViewWorkflow(t, fixture.store, false)
	if _, err := fixture.store.LinkWorkflow(fixture.ctx, fixture.binding.ProjectID, secondWorkflowID, false); err != nil {
		t.Fatalf("LinkWorkflow second workflow: %v", err)
	}
	if _, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  fixture.binding.ProjectID,
		WorkflowID: &secondWorkflowID,
		Title:      "Second backlog",
	}); err != nil {
		t.Fatalf("CreateTask second workflow: %v", err)
	}
	fixture.quiescence.blocked[active.task.ID] = true

	counts, err := fixture.tasks.CountGroups(fixture.ctx, &taskpb.ProjectTaskGroupCountsRequest{
		ProjectId: fixture.binding.ProjectID,
	})
	if err != nil {
		t.Fatalf("CountGroups: %v", err)
	}
	if counts.Counts.Active != 1 || counts.Counts.Backlog != 2 || counts.Counts.Done != 0 {
		t.Fatalf("counts = %+v, want active=1 backlog=2 done=0", counts.Counts)
	}
	if !reflect.DeepEqual(counts.Definitions, protoapi.TaskGroupDefinitions()) {
		t.Fatalf("definitions = %+v, want canonical Project Task groups", counts.Definitions)
	}
}

func TestTaskListPreservesAllLiveAttentionThroughCanonicalStatus(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Live question and approval")
	sessionID := fixture.bindCurrentNodeSession(t, started)
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		staticTaskStatusLiveObservationSource{
			observation: workflowexecution.WorkflowTaskExecutionObservation{
				Executions: map[workflow.TaskID]sessionruntime.TaskExecutionSnapshot{
					started.task.ID: {
						Executions: []sessionruntime.TaskExecution{{
							Ref: sessionruntime.WorkflowExecutionRef{
								ProjectID:   fixture.binding.ProjectID,
								WorkflowID:  fixture.workflowID,
								CurrentNode: started.currentNode,
							},
							Agent: &sessionruntime.TaskAgentExecutionTarget{SessionID: sessionID},
							PendingPrompts: []sessionruntime.PendingPromptReference{
								{ToolCallID: "question", Kind: sessionruntime.PendingPromptKindQuestion},
								{ToolCallID: "approval", Kind: sessionruntime.PendingPromptKindSessionApproval},
							},
						}},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	taskList, err := NewTaskList(fixture.metadata, mustDefinitionProjection(t, fixture.store), projection)
	if err != nil {
		t.Fatalf("NewTaskList: %v", err)
	}
	projectID := fixture.binding.ProjectID
	limit := int32(20)
	page, err := taskList.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId:      &projectID,
		StatusKinds:    []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION},
		AttentionKinds: []taskpb.TaskAttentionKind{taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_APPROVAL},
		LabelFilter:    noLabelFilter(),
		Limit:          &limit,
	})
	if err != nil {
		t.Fatalf("TaskList.List: %v", err)
	}
	if len(page.Tasks) != 1 ||
		page.Tasks[0].TaskId != string(started.task.ID) ||
		page.Tasks[0].Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION ||
		!slices.Equal(page.Tasks[0].Status.AttentionTypes, []taskpb.TaskAttentionKind{taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_APPROVAL, taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_QUESTION}) {
		t.Fatalf("live question-and-approval Task List page = %+v", page)
	}
}

func TestTaskListProjectsDurableDoneRunningAndQueued(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	done := fixture.startTask(t, "Done")
	running := fixture.startTask(t, "Running")
	queued := fixture.startTask(t, "Queued")
	liveAgent := func(task startedCurrentNodeViewTask, queued bool) sessionruntime.TaskExecution {
		return sessionruntime.TaskExecution{
			Ref: sessionruntime.WorkflowExecutionRef{
				ProjectID:   fixture.binding.ProjectID,
				WorkflowID:  fixture.workflowID,
				CurrentNode: task.currentNode,
			},
			Agent:  &sessionruntime.TaskAgentExecutionTarget{SessionID: fixture.bindCurrentNodeSession(t, task)},
			Queued: queued,
		}
	}
	doneExecution := liveAgent(done, false)
	runningExecution := liveAgent(running, false)
	queuedExecution := liveAgent(queued, true)
	if _, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
		Source:       done.currentNode,
		TransitionID: "done",
	}); err != nil {
		t.Fatalf("CompleteCurrentNode: %v", err)
	}
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		staticTaskStatusLiveObservationSource{
			observation: workflowexecution.WorkflowTaskExecutionObservation{
				Executions: map[workflow.TaskID]sessionruntime.TaskExecutionSnapshot{
					done.task.ID:    {Executions: []sessionruntime.TaskExecution{doneExecution}},
					running.task.ID: {Executions: []sessionruntime.TaskExecution{runningExecution}},
					queued.task.ID:  {Executions: []sessionruntime.TaskExecution{queuedExecution}},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	taskList, err := NewTaskList(fixture.metadata, mustDefinitionProjection(t, fixture.store), projection)
	if err != nil {
		t.Fatalf("NewTaskList: %v", err)
	}
	projectID := fixture.binding.ProjectID
	workflowID := fixture.workflowID
	limit := int32(1)
	request := &taskpb.ListRequest{
		ProjectId:   &projectID,
		WorkflowId:  proto.String(workflowID.String()),
		StatusKinds: []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE, taskpb.TaskStatusKind_TASK_STATUS_KIND_RUNNING, taskpb.TaskStatusKind_TASK_STATUS_KIND_QUEUED},
		LabelFilter: noLabelFilter(),
		Sort: []*taskpb.ListSort{{
			Field:     taskpb.ListSortField_LIST_SORT_FIELD_STATUS,
			Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC,
		}},
		Limit: &limit,
	}
	wantIDs := []string{string(done.task.ID), string(running.task.ID), string(queued.task.ID)}
	wantKinds := []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE, taskpb.TaskStatusKind_TASK_STATUS_KIND_RUNNING, taskpb.TaskStatusKind_TASK_STATUS_KIND_QUEUED}
	for index := range wantIDs {
		page, err := taskList.List(fixture.ctx, request)
		if err != nil {
			t.Fatalf("TaskList.List page %d: %v", index, err)
		}
		if len(page.Tasks) != 1 ||
			page.Tasks[0].TaskId != wantIDs[index] ||
			page.Tasks[0].Status.Kind != wantKinds[index] {
			t.Fatalf("status page %d = %+v, want %s/%s", index, page.Tasks, wantIDs[index], wantKinds[index])
		}
		if index < len(wantIDs)-1 {
			if page.NextOffset == nil {
				t.Fatalf("status page %d has no next offset", index)
			}
			request.Offset = page.NextOffset
		} else if page.NextOffset != nil {
			t.Fatalf("final status page next offset = %v, want nil", page.NextOffset)
		}
	}
}

func TestTaskDetailProjectsDurableCurrentStateMatrix(t *testing.T) {
	t.Run("interrupted", func(t *testing.T) {
		fixture := newCurrentNodeViewFixture(t, false)
		started := fixture.startTask(t, "Interrupted")
		if err := fixture.store.InterruptCurrentNode(
			fixture.ctx,
			started.currentNode,
			workflow.CurrentNodeInterruptionReason("server_restart"),
			workflow.CurrentNodeInterruptionDetail{Code: "restart"},
		); err != nil {
			t.Fatalf("InterruptCurrentNode: %v", err)
		}
		detail, err := fixture.detail.GetTask(fixture.ctx, string(started.task.ID))
		if err != nil {
			t.Fatalf("TaskDetail.GetTask: %v", err)
		}
		if detail.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED ||
			detail.AttentionCount != 1 ||
			!detail.Actions.CanResume {
			t.Fatalf("interrupted detail = %+v", detail)
		}
	})

	t.Run("waiting approval", func(t *testing.T) {
		fixture := newCurrentNodeViewFixture(t, true)
		started := fixture.startTask(t, "Approval")
		completed, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
			Source:       started.currentNode,
			TransitionID: "done",
		})
		if err != nil {
			t.Fatalf("CompleteCurrentNode: %v", err)
		}
		if completed.PendingApproval == nil {
			t.Fatal("pending Approval is missing")
		}
		detail, err := fixture.detail.GetTask(fixture.ctx, string(started.task.ID))
		if err != nil {
			t.Fatalf("TaskDetail.GetTask: %v", err)
		}
		if detail.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL ||
			detail.AttentionCount != 1 ||
			len(detail.CurrentNodes) != 1 ||
			detail.CurrentNodes[0].NodeId != string(fixture.agentNodeID) {
			t.Fatalf("waiting-Approval detail = %+v", detail)
		}
	})

	t.Run("done", func(t *testing.T) {
		fixture := newCurrentNodeViewFixture(t, false)
		started := fixture.startTask(t, "Done")
		if _, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
			Source:       started.currentNode,
			TransitionID: "done",
		}); err != nil {
			t.Fatalf("CompleteCurrentNode: %v", err)
		}
		detail, err := fixture.detail.GetTask(fixture.ctx, string(started.task.ID))
		if err != nil {
			t.Fatalf("TaskDetail.GetTask: %v", err)
		}
		if detail.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE ||
			!detail.Summary.Done ||
			detail.AttentionCount != 0 {
			t.Fatalf("done detail = %+v", detail)
		}
	})
}
