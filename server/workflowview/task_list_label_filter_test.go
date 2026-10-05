package workflowview

import (
	"slices"
	"testing"

	"core/server/workflowstore"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"google.golang.org/protobuf/proto"
)

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
