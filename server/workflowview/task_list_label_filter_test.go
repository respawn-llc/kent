package workflowview

import (
	"slices"
	"testing"

	"core/internal/testharness/workflowfixture"
	"core/server/workflow"
	"core/server/workflowstore"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
)

func TestProjectTaskListStageUsesUngroupedNodeName(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	started := fixture.startTask(t, "Stage")
	page, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId: &fixture.binding.ProjectID, LabelFilter: noLabelFilter(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 1 || page.Tasks[0].TaskId != string(started.task.ID) ||
		page.Tasks[0].StageName == nil || *page.Tasks[0].StageName != "Agent" {
		t.Fatalf("Stage projection = %+v", page.Tasks)
	}
	workflowfixture.SaveStoreGraph(t, fixture.ctx, fixture.store, fixture.workflowID, func(_ workflow.Definition, req *workflowstore.WorkflowGraphSaveRequest) {
		for i := range req.Nodes {
			if req.Nodes[i].ID == fixture.agentNodeID {
				req.Nodes[i].DisplayName = ""
			}
		}
	})
	page, err = fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId: &fixture.binding.ProjectID, LabelFilter: noLabelFilter(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Tasks[0].StageName == nil || *page.Tasks[0].StageName != "agent" {
		t.Fatalf("unnamed ungrouped Node Stage = %+v", page.Tasks)
	}
}

func TestProjectTaskListStageUsesNamedGroupDuringParallelWork(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	groupID := runtimeids.NewGraphEntityID()
	otherID := workflow.NodeID(runtimeids.NewGraphEntityID())
	joinID := workflow.NodeID(runtimeids.NewGraphEntityID())
	otherTransition := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
	joinTransition := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
	workflowfixture.SaveStoreGraph(t, fixture.ctx, fixture.store, fixture.workflowID, func(definition workflow.Definition, req *workflowstore.WorkflowGraphSaveRequest) {
		terminalID := currentNodeViewNodeIDByKind(t, definition, workflow.NodeKindTerminal)
		startID := currentNodeViewNodeIDByKind(t, definition, workflow.NodeKindStart)
		var startTransition workflow.TransitionGroupID
		for _, transition := range req.TransitionGroups {
			if transition.SourceNodeID == startID {
				startTransition = transition.ID
			}
		}
		req.NodeGroups = append(req.NodeGroups, workflowstore.NodeGroupRecord{
			ID: groupID, WorkflowID: fixture.workflowID, Key: "parallel", DisplayName: new("Delivery"),
		})
		for i := range req.Nodes {
			if req.Nodes[i].ID == fixture.agentNodeID {
				req.Nodes[i].GroupID = &groupID
			}
		}
		req.Nodes = append(req.Nodes,
			workflowstore.NodeRecord{ID: otherID, WorkflowID: fixture.workflowID, Key: "other", Kind: workflow.NodeKindAgent, DisplayName: "Other", SubagentRole: "coder", GroupID: &groupID},
			workflowstore.NodeRecord{ID: joinID, WorkflowID: fixture.workflowID, Key: "join", Kind: workflow.NodeKindJoin, DisplayName: "Join", GroupID: &groupID},
		)
		for i := range req.Edges {
			if req.Edges[i].TargetNodeID == terminalID {
				req.Edges[i].TargetNodeID = joinID
			}
		}
		req.TransitionGroups = append(req.TransitionGroups,
			workflowstore.TransitionGroupRecord{ID: otherTransition, WorkflowID: fixture.workflowID, SourceNodeID: otherID, TransitionID: "other_done", DisplayName: "Done"},
			workflowstore.TransitionGroupRecord{ID: joinTransition, WorkflowID: fixture.workflowID, SourceNodeID: joinID, TransitionID: "joined", DisplayName: "Done"},
		)
		for _, edge := range []struct {
			group  workflow.TransitionGroupID
			target workflow.NodeID
			key    workflow.ModelKey
		}{{startTransition, otherID, "a_other"}, {otherTransition, joinID, "other_done"}, {joinTransition, terminalID, "joined"}} {
			prompt := ""
			if edge.target == otherID {
				prompt = "Work."
			}
			req.Edges = append(req.Edges, workflowstore.EdgeRecord{
				ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: fixture.workflowID,
				TransitionGroupID: edge.group, TargetNodeID: edge.target, Key: edge.key,
				AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured,
				ContextMode: workflow.ContextModeNewSession, PromptTemplate: prompt,
			})
		}
	})
	task := fixture.createBacklogTask(t, "Parallel stage")
	plan, err := fixture.store.PlanTaskStart(fixture.ctx, task.ID, &workflowstore.ExecutionTargetCandidate{
		Snapshot: workflowstore.ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeNone, Provenance: workflowstore.ExecutionTargetProvenanceResolved},
		Root:     workflowstore.ExecutionRoot{SourceWorkspaceID: fixture.binding.WorkspaceID, SourceWorkspaceRoot: fixture.binding.CanonicalRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.CommitTaskStart(fixture.ctx, plan, workflowfixture.PrepareCurrentNodeSessions(t, fixture.ctx, fixture.metadata, plan.StartContexts())); err != nil {
		t.Fatal(err)
	}
	page, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId: &fixture.binding.ProjectID, LabelFilter: noLabelFilter(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 1 || len(page.Tasks[0].Status.NodeIds) != 2 ||
		page.Tasks[0].StageName == nil || *page.Tasks[0].StageName != "Delivery" {
		t.Fatalf("parallel Stage = %+v", page.Tasks)
	}
	workflowfixture.SaveStoreGraph(t, fixture.ctx, fixture.store, fixture.workflowID, func(_ workflow.Definition, req *workflowstore.WorkflowGraphSaveRequest) {
		req.NodeGroups[0].DisplayName = nil
	})
	page, err = fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId: &fixture.binding.ProjectID, LabelFilter: noLabelFilter(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 1 || page.Tasks[0].StageName == nil || *page.Tasks[0].StageName != "Agent" {
		t.Fatalf("unnamed parallel Group Stage = %+v, want first stored member Agent", page.Tasks)
	}
	workflowfixture.SaveStoreGraph(t, fixture.ctx, fixture.store, fixture.workflowID, func(_ workflow.Definition, req *workflowstore.WorkflowGraphSaveRequest) {
		for i := range req.Nodes {
			if req.Nodes[i].ID == fixture.agentNodeID {
				req.Nodes[i].DisplayName = ""
			}
		}
	})
	page, err = fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId: &fixture.binding.ProjectID, LabelFilter: noLabelFilter(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Tasks[0].StageName == nil || *page.Tasks[0].StageName != "agent" {
		t.Fatalf("unnamed Group first member key Stage = %+v", page.Tasks)
	}
}

func TestProjectTaskListStageIncludesBacklogAndDone(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	backlog := fixture.createBacklogTask(t, "Backlog")
	done := fixture.startTask(t, "Done")
	if _, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
		Source: done.currentNode, TransitionID: "done",
	}); err != nil {
		t.Fatal(err)
	}
	page, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
		ProjectId: &fixture.binding.ProjectID, LabelFilter: noLabelFilter(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tasks) != 2 {
		t.Fatalf("Tasks = %+v", page.Tasks)
	}
	for _, item := range page.Tasks {
		want := "Done"
		if item.TaskId == string(backlog.ID) {
			want = "Backlog"
		}
		if item.StageName == nil || *item.StageName != want {
			t.Fatalf("Stage = %v, want %s", item.StageName, want)
		}
	}
}

func TestProjectTaskListGroupPagesProjectCanonicalRowDataInServerOrder(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	alpha, err := fixture.store.CreateProjectLabel(fixture.ctx, fixture.binding.ProjectID, "Alpha")
	if err != nil {
		t.Fatalf("CreateProjectLabel Alpha: %v", err)
	}
	beta, err := fixture.store.CreateProjectLabel(fixture.ctx, fixture.binding.ProjectID, "Beta")
	if err != nil {
		t.Fatalf("CreateProjectLabel Beta: %v", err)
	}
	create := func(title string) workflowstore.TaskRecord {
		t.Helper()
		task, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
			ProjectID:  fixture.binding.ProjectID,
			WorkflowID: &fixture.workflowID,
			Title:      title,
			LabelIDs:   []string{alpha.ID.String(), beta.ID.String()},
		})
		if err != nil {
			t.Fatalf("CreateTask %q: %v", title, err)
		}
		return task
	}
	first := create("First")
	second := create("Second")
	fixture.setTaskUpdatedAt(t, first.ID, 1_000)
	fixture.setTaskUpdatedAt(t, second.ID, 1_000)
	blocker := createViewTask(t, fixture, "Blocker")
	if _, err := fixture.store.AddTaskDependency(fixture.ctx, workflowstore.TaskDependencyAddRequest{
		BlockerTaskID: blocker.ID,
		BlockedTaskID: first.ID,
	}); err != nil {
		t.Fatalf("AddTaskDependency: %v", err)
	}

	projectID := fixture.binding.ProjectID
	limit := int32(1)
	group := taskpb.ProjectTaskGroup_PROJECT_TASK_GROUP_BACKLOG
	request := &taskpb.ListRequest{
		ProjectId: &projectID,
		Group:     &group,
		LabelFilter: &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_Named{Named: &taskpb.NamedLabelFilter{
			Mode:     taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY,
			LabelIds: []string{alpha.ID.String()},
		}},
		},
		Sort: []*taskpb.ListSort{{
			Field:     taskpb.ListSortField_LIST_SORT_FIELD_UPDATED,
			Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_DESC,
		}},
		Limit: &limit,
	}
	wantLabels := []*workflowpb.ProjectLabel{
		{Id: beta.ID.String(), Name: "Beta"},
		{Id: alpha.ID.String(), Name: "Alpha"},
	}
	var gotIDs []string
	for index := 0; index < 2; index++ {
		page, err := fixture.tasks.List(fixture.ctx, request)
		if err != nil {
			t.Fatalf("TaskList.List page %d: %v", index, err)
		}
		if len(page.Tasks) != 1 {
			t.Fatalf("page %d tasks = %+v, want one Task", index, page.Tasks)
		}
		item := page.Tasks[0]
		gotIDs = append(gotIDs, item.TaskId)
		if item.WorkflowName == nil || *item.WorkflowName == "" {
			t.Fatalf("project-wide item workflow name = %v", item.WorkflowName)
		}
		if !slices.EqualFunc(item.Labels, wantLabels, func(left, right *workflowpb.ProjectLabel) bool {
			return proto.Equal(left, right)
		}) {
			t.Fatalf("item labels = %+v", item.Labels)
		}
		if item.TaskId == string(first.ID) {
			if item.DependencyProgress == nil ||
				item.DependencyProgress.SatisfiedCount != 0 ||
				item.DependencyProgress.TotalCount != 1 {
				t.Fatalf("item dependency progress = %+v, want 0/1", item.DependencyProgress)
			}
		} else if item.DependencyProgress != nil {
			t.Fatalf("dependency-free item progress = %+v, want nil", item.DependencyProgress)
		}
		if index == 1 {
			if page.NextOffset != nil {
				t.Fatalf("final page next offset = %v, want nil", page.NextOffset)
			}
		} else {
			if page.NextOffset == nil {
				t.Fatal("first page next offset is nil")
			}
			request.Offset = page.NextOffset
		}
	}
	if !slices.Contains(gotIDs, string(first.ID)) || !slices.Contains(gotIDs, string(second.ID)) {
		t.Fatalf("paged Task IDs = %v, want %s and %s", gotIDs, first.ID, second.ID)
	}
}

func requireCurrentNodeTaskListIDs(t *testing.T, items []*taskpb.ListItem, want []string) {
	t.Helper()
	got := make(map[string]bool, len(items))
	for _, item := range items {
		got[item.TaskId] = true
	}
	if len(got) != len(want) {
		t.Fatalf("task list IDs = %v, want %v", got, want)
	}
	for _, taskID := range want {
		if !got[taskID] {
			t.Fatalf("task list IDs = %v, missing %s", got, taskID)
		}
	}
}

func TestCurrentNodeTaskListLabelExclusionsFilterTasks(t *testing.T) {
	fixture := newCurrentNodeLabelFilterFixture(t)
	projectID := fixture.binding.ProjectID
	for _, tt := range fixture.exclusionCases() {
		t.Run(tt.name, func(t *testing.T) {
			response, err := fixture.tasks.List(fixture.ctx, &taskpb.ListRequest{
				ProjectId:   &projectID,
				WorkflowId:  proto.String(fixture.workflowID.String()),
				LabelFilter: tt.filter,
			})
			if err != nil {
				t.Fatalf("TaskList.List: %v", err)
			}
			requireCurrentNodeTaskListIDs(t, response.Tasks, tt.want)
		})
	}
}

func TestCurrentNodeTaskListDependencyFilterRunsBeforeSortAndPagination(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	alpha, err := fixture.store.CreateProjectLabel(fixture.ctx, fixture.binding.ProjectID, "alpha")
	if err != nil {
		t.Fatalf("CreateProjectLabel: %v", err)
	}
	started := func(title string, labelIDs ...string) startedCurrentNodeViewTask {
		t.Helper()
		taskRecord, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
			ProjectID:  fixture.binding.ProjectID,
			WorkflowID: &fixture.workflowID,
			Title:      title,
			LabelIDs:   labelIDs,
		})
		if err != nil {
			t.Fatalf("CreateTask %q: %v", title, err)
		}
		return fixture.startExistingTask(t, taskRecord)
	}
	alphaFirst := started("Alpha first", alpha.ID.String())
	alphaSecond := started("Alpha second", alpha.ID.String())
	alphaBlocked := started("Alpha blocked", alpha.ID.String())
	betaUnblocked := started("Beta unblocked")
	for _, task := range []startedCurrentNodeViewTask{alphaFirst, alphaSecond, alphaBlocked, betaUnblocked} {
		fixture.setTaskUpdatedAt(t, task.task.ID, 1_000)
	}
	blocker, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  fixture.binding.ProjectID,
		WorkflowID: &fixture.workflowID,
		Title:      "Open blocker",
	})
	if err != nil {
		t.Fatalf("CreateTask blocker: %v", err)
	}
	if _, err := fixture.store.AddTaskDependency(fixture.ctx, workflowstore.TaskDependencyAddRequest{
		BlockerTaskID: blocker.ID,
		BlockedTaskID: alphaBlocked.task.ID,
	}); err != nil {
		t.Fatalf("AddTaskDependency: %v", err)
	}

	projectID := fixture.binding.ProjectID
	workflowID := fixture.workflowID
	limit := int32(1)
	unblocked := true
	alphaFilter := &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_Named{Named: &taskpb.NamedLabelFilter{
		Mode:     taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY,
		LabelIds: []string{alpha.ID.String()},
	}},
	}
	request := &taskpb.ListRequest{
		ProjectId:        &projectID,
		WorkflowId:       proto.String(workflowID.String()),
		ColumnKeys:       []string{"agent"},
		StatusKinds:      []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE},
		LabelFilter:      alphaFilter,
		DependencyFilter: &unblocked,
		Sort: []*taskpb.ListSort{{
			Field:     taskpb.ListSortField_LIST_SORT_FIELD_TITLE,
			Direction: taskpb.ListSortDirection_LIST_SORT_DIRECTION_ASC,
		}},
		Limit: &limit,
	}
	var got []string
	for {
		page, err := fixture.tasks.List(fixture.ctx, request)
		if err != nil {
			t.Fatalf("TaskList.List: %v", err)
		}
		for _, item := range page.Tasks {
			got = append(got, item.TaskId)
		}
		if page.NextOffset == nil {
			break
		}
		request.Offset = page.NextOffset
	}
	want := []string{string(alphaFirst.task.ID), string(alphaSecond.task.ID)}
	if !equalStrings(got, want) {
		t.Fatalf("filtered task-list pagination = %v, want %v", got, want)
	}

	blocked := false
	request.DependencyFilter = &blocked
	request.Offset = nil
	blockedPage, err := fixture.tasks.List(fixture.ctx, request)
	if err != nil {
		t.Fatalf("TaskList.List blocked: %v", err)
	}
	if len(blockedPage.Tasks) != 1 || blockedPage.Tasks[0].TaskId != string(alphaBlocked.task.ID) {
		t.Fatalf("blocked task-list page = %+v, want blocked Task %q", blockedPage.Tasks, alphaBlocked.task.ID)
	}

	request.DependencyFilter = &unblocked
	request.AttentionKinds = []taskpb.TaskAttentionKind{taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_QUESTION}
	noAttention, err := fixture.tasks.List(fixture.ctx, request)
	if err != nil {
		t.Fatalf("TaskList.List attention intersection: %v", err)
	}
	if len(noAttention.Tasks) != 0 {
		t.Fatalf("attention intersection = %+v, want no Tasks", noAttention.Tasks)
	}
}
