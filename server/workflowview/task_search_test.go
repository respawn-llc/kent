package workflowview

import (
	"context"
	"core/internal/testharness/workflowfixture"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

func TestNewTaskSearchRequiresTaskStatusProjection(t *testing.T) {
	if _, err := NewTaskSearch(nil, nil); err == nil {
		t.Fatal("NewTaskSearch accepted absent metadata and projection")
	}
	store := testsetup.OpenStore(t, t.TempDir())
	if _, err := NewTaskSearch(store, nil); err == nil {
		t.Fatal("NewTaskSearch accepted absent projection")
	}
}

func TestTaskSearchFindsAndFiltersCanonicalTaskSources(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	first := createTaskSearchTask(t, fixture, "needle title", "needle body needle")
	comment, err := fixture.store.AddComment(fixture.ctx, first.ID, "needle comment", "user", "user-1")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	otherBinding, err := fixture.metadata.RegisterWorkspaceBinding(fixture.ctx, t.TempDir())
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if _, err := fixture.store.LinkWorkflow(fixture.ctx, otherBinding.ProjectID, fixture.workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow second project: %v", err)
	}
	if _, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  otherBinding.ProjectID,
		WorkflowID: &fixture.workflowID,
		Title:      "needle elsewhere",
		Body:       "other",
	}); err != nil {
		t.Fatalf("CreateTask second project: %v", err)
	}

	response, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:            taskpb.SearchMode_SEARCH_MODE_LITERAL,
		Query:           "needle",
		Context:         serverapi.TaskSearchDefaultContext,
		ProjectIds:      []string{fixture.binding.ProjectID},
		StatusKinds:     []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG},
		IncludeComments: true,
		PageSize:        serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Groups) != 1 || response.Groups[0].TaskId != string(first.ID) {
		t.Fatalf("filtered search response = %+v", response)
	}
	group := response.Groups[0]
	if group.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG ||
		group.TotalHitCount != 4 ||
		len(group.Hits) != 4 ||
		group.Hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_TITLE ||
		group.Hits[1].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY ||
		group.Hits[2].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY ||
		group.Hits[3].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_COMMENT ||
		group.Hits[3].Source.CommentId == nil ||
		*group.Hits[3].Source.CommentId != comment.ID {
		t.Fatalf("filtered search group = %+v", group)
	}

	withoutComments, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:    "comment:needle",
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("Search without Comments: %v", err)
	}
	if len(withoutComments.Groups) != 0 {
		t.Fatalf("Comment-excluded search = %+v", withoutComments)
	}
}

func TestTaskSearchProjectScopedNumericShortIDRanksExactTaskFirst(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	if _, err := fixture.metadata.DB().Exec(
		`UPDATE projects SET next_task_seq = 345 WHERE id = ?`,
		fixture.binding.ProjectID,
	); err != nil {
		t.Fatalf("set current Project Task sequence: %v", err)
	}
	exact := createTaskSearchTask(t, fixture, "Exact identifier", "ordinary body")
	text := createTaskSearchTask(t, fixture, "345 title match", "ordinary body")

	otherBinding, err := fixture.metadata.RegisterWorkspaceBinding(fixture.ctx, t.TempDir())
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if _, err := fixture.store.LinkWorkflow(fixture.ctx, otherBinding.ProjectID, fixture.workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow second Project: %v", err)
	}
	if _, err := fixture.metadata.DB().Exec(
		`UPDATE projects SET next_task_seq = 345 WHERE id = ?`,
		otherBinding.ProjectID,
	); err != nil {
		t.Fatalf("set second Project Task sequence: %v", err)
	}
	if _, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  otherBinding.ProjectID,
		WorkflowID: &fixture.workflowID,
		Title:      "Other Project exact identifier",
		Body:       "ordinary body",
	}); err != nil {
		t.Fatalf("CreateTask second Project: %v", err)
	}

	response, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:       taskpb.SearchMode_SEARCH_MODE_LITERAL,
		Query:      "345",
		Context:    serverapi.TaskSearchDefaultContext,
		ProjectIds: []string{fixture.binding.ProjectID},
		PageSize:   serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("Search numeric Short ID: %v", err)
	}
	if len(response.Groups) != 2 ||
		response.Groups[0].TaskId != string(exact.ID) ||
		response.Groups[1].TaskId != string(text.ID) {
		t.Fatalf("Project-scoped numeric Short ID response = %+v", response)
	}
	hits := response.Groups[0].Hits
	if len(hits) != 1 ||
		hits[0].Ordinal != 1 ||
		hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_SHORT_ID ||
		hits[0].GetLiteral() == nil ||
		hits[0].GetLiteral().Match != "345" {
		t.Fatalf("exact Short ID hits = %+v", hits)
	}
}

func TestTaskSearchFullShortIDIgnoresTextCaseSensitivity(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	task := createTaskSearchTaskAtSequence(t, fixture, 345, "Exact identifier", "ordinary body")
	request := taskSearchRequest(strings.ToLower(task.ShortID))
	request.CaseSensitive = true

	response, err := search.Search(fixture.ctx, request)
	if err != nil {
		t.Fatalf("Search lowercase full Short ID: %v", err)
	}
	if len(response.Groups) != 1 ||
		response.Groups[0].TaskId != string(task.ID) ||
		len(response.Groups[0].Hits) != 1 ||
		response.Groups[0].Hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_SHORT_ID ||
		response.Groups[0].Hits[0].GetLiteral() == nil ||
		response.Groups[0].Hits[0].GetLiteral().Match != task.ShortID {
		t.Fatalf("lowercase full Short ID response = %+v", response)
	}
}

func TestTaskSearchGlobalNumericShortIDReturnsEveryProjectWithDeterministicTies(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	first := createTaskSearchTaskAtSequence(t, fixture, 345, "First exact identifier", "ordinary body")
	otherBinding, err := fixture.metadata.RegisterWorkspaceBinding(fixture.ctx, t.TempDir())
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if _, err := fixture.store.LinkWorkflow(fixture.ctx, otherBinding.ProjectID, fixture.workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow second Project: %v", err)
	}
	if _, err := fixture.metadata.DB().Exec(
		`UPDATE projects SET next_task_seq = 345 WHERE id = ?`,
		otherBinding.ProjectID,
	); err != nil {
		t.Fatalf("set second Project Task sequence: %v", err)
	}
	second, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  otherBinding.ProjectID,
		WorkflowID: &fixture.workflowID,
		Title:      "Second exact identifier",
		Body:       "ordinary body",
	})
	if err != nil {
		t.Fatalf("CreateTask second Project: %v", err)
	}

	response, err := search.Search(fixture.ctx, taskSearchRequest("345"))
	if err != nil {
		t.Fatalf("Search global numeric Short ID: %v", err)
	}
	wantTaskIDs := []string{string(first.ID), string(second.ID)}
	slices.Sort(wantTaskIDs)
	if len(response.Groups) != 2 {
		t.Fatalf("global numeric Short ID groups = %+v, want two exact Tasks", response.Groups)
	}
	for index, wantTaskID := range wantTaskIDs {
		group := response.Groups[index]
		if group.TaskId != wantTaskID ||
			len(group.Hits) != 1 ||
			group.Hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_SHORT_ID {
			t.Fatalf("global numeric Short ID group %d = %+v, want Task %s", index, group, wantTaskID)
		}
	}
}

func TestTaskSearchLeadingZeroDoesNotAliasCanonicalTaskSequence(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	createTaskSearchTaskAtSequence(t, fixture, 345, "Exact identifier", "ordinary body")

	response, err := search.Search(fixture.ctx, taskSearchRequest("0345"))
	if err != nil {
		t.Fatalf("Search leading-zero numeric text: %v", err)
	}
	if len(response.Groups) != 0 {
		t.Fatalf("leading-zero numeric text response = %+v, want no canonical sequence alias", response)
	}
}

func TestTaskSearchShortIDMaterializesFirstRepeatedOccurrence(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	if _, err := fixture.metadata.DB().Exec(
		`UPDATE projects SET project_key = 'KENTKENT' WHERE id = ?`,
		fixture.binding.ProjectID,
	); err != nil {
		t.Fatalf("set repeated Project key: %v", err)
	}
	task := createTaskSearchTask(t, fixture, "Repeated identifier", "ordinary body")

	response, err := search.Search(fixture.ctx, taskSearchRequest("KENT"))
	if err != nil {
		t.Fatalf("Search repeated Short ID substring: %v", err)
	}
	if len(response.Groups) != 1 ||
		response.Groups[0].TaskId != string(task.ID) ||
		len(response.Groups[0].Hits) != 1 ||
		response.Groups[0].Hits[0].GetLiteral() == nil {
		t.Fatalf("repeated Short ID response = %+v", response)
	}
	literal := response.Groups[0].Hits[0].GetLiteral()
	if literal.Before != "" || literal.Match != "KENT" || literal.After != "KENT-1" {
		t.Fatalf("repeated Short ID fragment = %+v, want first left-to-right occurrence", literal)
	}
}

func TestTaskSearchReflectsTaskAndCommentMutationsImmediately(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	task := createTaskSearchTask(t, fixture, "Mutation", "needle body")
	request := taskSearchRequest("needle")
	assertTaskSearchTask(t, fixture.ctx, search, request, task.ID, taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG)

	replacement := "replacement body"
	if _, err := fixture.store.UpdateTask(fixture.ctx, workflowstore.UpdateTaskRequest{TaskID: task.ID, Body: &replacement}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	assertTaskSearchEmpty(t, fixture.ctx, search, request)

	comment, err := fixture.store.AddComment(fixture.ctx, task.ID, "needle comment", "user", "user-1")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	request.IncludeComments = true
	response, err := search.Search(fixture.ctx, request)
	if err != nil {
		t.Fatalf("Search after comment create: %v", err)
	}
	if len(response.Groups) != 1 || len(response.Groups[0].Hits) != 1 || response.Groups[0].Hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_COMMENT {
		t.Fatalf("search after Comment create = %+v", response)
	}
	if err := fixture.store.DeleteComment(fixture.ctx, comment.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	assertTaskSearchEmpty(t, fixture.ctx, search, request)
}

func TestTaskSearchFiltersDurableCurrentNodeStatuses(t *testing.T) {
	tests := []struct {
		name             string
		requiresApproval bool
		prepare          func(t *testing.T, fixture currentNodeViewFixture, task workflowstore.TaskRecord)
		want             taskpb.TaskStatusKind
	}{
		{
			name: "backlog",
			want: taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG,
		},
		{
			name: "active",
			prepare: func(t *testing.T, fixture currentNodeViewFixture, task workflowstore.TaskRecord) {
				startTaskSearchTask(t, fixture, task)
			},
			want: taskpb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE,
		},
		{
			name: "interrupted",
			prepare: func(t *testing.T, fixture currentNodeViewFixture, task workflowstore.TaskRecord) {
				started := startTaskSearchTask(t, fixture, task)
				if err := fixture.store.InterruptCurrentNode(
					fixture.ctx,
					started.currentNode,
					workflow.CurrentNodeInterruptionReason("server_restart"),
					workflow.CurrentNodeInterruptionDetail{Code: "restart"},
				); err != nil {
					t.Fatalf("InterruptCurrentNode: %v", err)
				}
			},
			want: taskpb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED,
		},
		{
			name:             "waiting approval",
			requiresApproval: true,
			prepare: func(t *testing.T, fixture currentNodeViewFixture, task workflowstore.TaskRecord) {
				started := startTaskSearchTask(t, fixture, task)
				if _, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
					Source:       started.currentNode,
					TransitionID: "done",
				}); err != nil {
					t.Fatalf("CompleteCurrentNode: %v", err)
				}
			},
			want: taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL,
		},
		{
			name: "done",
			prepare: func(t *testing.T, fixture currentNodeViewFixture, task workflowstore.TaskRecord) {
				started := startTaskSearchTask(t, fixture, task)
				if _, err := workflowfixture.CompleteCurrentNode(t, fixture.ctx, fixture.metadata, fixture.store, workflowstore.CurrentNodeCompletionRequest{
					Source:       started.currentNode,
					TransitionID: "done",
				}); err != nil {
					t.Fatalf("CompleteCurrentNode: %v", err)
				}
			},
			want: taskpb.TaskStatusKind_TASK_STATUS_KIND_DONE,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, search := newTaskSearchFixture(t, test.requiresApproval)
			task := createTaskSearchTask(t, fixture, test.name, "needle")
			if test.prepare != nil {
				test.prepare(t, fixture, task)
			}
			request := taskSearchRequest("needle")
			request.StatusKinds = []taskpb.TaskStatusKind{test.want}
			assertTaskSearchTask(t, fixture.ctx, search, request, task.ID, test.want)
		})
	}
}

func TestTaskSearchFiltersQueuedAndRunningCurrentNodeExecutions(t *testing.T) {
	shellPath, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh is unavailable: %v", err)
	}
	fixture, search := newTaskSearchFixture(t, false)
	queued := startTaskSearchTask(t, fixture, createTaskSearchTask(t, fixture, "Queued", "needle"))
	running := startTaskSearchTask(t, fixture, createTaskSearchTask(t, fixture, "Running", "needle"))
	runningHandle := startTaskSearchScript(t, fixture, shellPath, running)
	t.Cleanup(func() {
		runningHandle.RequestStop()
		_, _ = runningHandle.Wait(context.Background())
	})
	testsetup.RequireUntil(t, time.Now().Add(3*time.Second), 10*time.Millisecond, func() bool {
		snapshots, snapshotErr := fixture.authority.CurrentWorkflowTaskExecutionSnapshots()
		if snapshotErr != nil {
			return false
		}
		return len(snapshots[running.task.ID].Executions) == 1 &&
			!snapshots[running.task.ID].Executions[0].Queued
	}, "timed out waiting for running Current Node execution")
	snapshots, err := fixture.authority.CurrentScopedTaskExecutionSnapshots(
		fixture.binding.ProjectID,
		fixture.workflowID,
		[]workflow.TaskID{running.task.ID, queued.task.ID},
	)
	if err != nil {
		t.Fatalf("CurrentScopedTaskExecutionSnapshots: %v", err)
	}
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		staticTaskStatusLiveObservationSource{
			observation: workflowexecution.WorkflowTaskExecutionObservation{
				Executions: snapshots,
				ConcurrencyQueued: map[workflow.TaskID][]workflow.CurrentNodeReference{
					queued.task.ID: {queued.currentNode},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	search = newTaskSearch(t, fixture.metadata, projection)
	for _, test := range []struct {
		task workflowstore.TaskRecord
		kind taskpb.TaskStatusKind
	}{
		{task: queued.task, kind: taskpb.TaskStatusKind_TASK_STATUS_KIND_QUEUED},
		{task: running.task, kind: taskpb.TaskStatusKind_TASK_STATUS_KIND_RUNNING},
	} {
		request := taskSearchRequest("needle")
		request.StatusKinds = []taskpb.TaskStatusKind{test.kind}
		assertTaskSearchTask(t, fixture.ctx, search, request, test.task.ID, test.kind)
	}
}

func TestTaskSearchFiltersWaitingQuestionCurrentNodeExecution(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	task := createTaskSearchTask(t, fixture, "Question", "needle")
	question := fixture.startCurrentNodeQuestion(t, startTaskSearchTask(t, fixture, task))
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		&currentNodeViewStatusObservationSource{
			authority: question.authority,
			blocked:   fixture.quiescence.blocked,
		},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	search, err := NewTaskSearch(fixture.metadata, projection)
	if err != nil {
		t.Fatalf("NewTaskSearch: %v", err)
	}
	request := taskSearchRequest("needle")
	request.StatusKinds = []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION}
	assertTaskSearchTask(t, fixture.ctx, search, request, task.ID, taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION)
	question.resolve(t, fixture.ctx)
}

func TestTaskSearchProjectsLiveSessionApprovalStatus(t *testing.T) {
	fixture := newCurrentNodeViewFixture(t, false)
	task := createTaskSearchTask(t, fixture, "Approval", "needle")
	started := fixture.startTask(t, "Approval execution")
	sessionID := fixture.bindCurrentNodeSession(t, started)
	projection, err := NewTaskStatusProjection(
		fixture.store,
		NewTaskProjector(),
		staticTaskStatusLiveObservationSource{
			observation: workflowexecution.WorkflowTaskExecutionObservation{
				Executions: map[workflow.TaskID]sessionruntime.TaskExecutionSnapshot{
					task.ID: {
						Executions: []sessionruntime.TaskExecution{{
							Agent: &sessionruntime.TaskAgentExecutionTarget{SessionID: sessionID},
							PendingPrompts: []sessionruntime.PendingPromptReference{{
								ToolCallID: "approval",
								Kind:       sessionruntime.PendingPromptKindSessionApproval,
							}},
						}},
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewTaskStatusProjection: %v", err)
	}
	search, err := NewTaskSearch(fixture.metadata, projection)
	if err != nil {
		t.Fatalf("NewTaskSearch: %v", err)
	}
	request := taskSearchRequest("needle")
	request.StatusKinds = []taskpb.TaskStatusKind{taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL}
	response, err := search.Search(fixture.ctx, request)
	if err != nil {
		t.Fatalf("TaskSearch.Search: %v", err)
	}
	if len(response.Groups) != 1 ||
		response.Groups[0].TaskId != string(task.ID) ||
		response.Groups[0].Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL ||
		len(response.Groups[0].Status.AttentionTypes) != 1 ||
		response.Groups[0].Status.AttentionTypes[0] != taskpb.TaskAttentionKind_TASK_ATTENTION_KIND_APPROVAL {
		t.Fatalf("live approval search response = %+v", response)
	}
}

func newTaskSearchFixture(t *testing.T, requiresApproval bool) (currentNodeViewFixture, *TaskSearch) {
	t.Helper()
	fixture := newCurrentNodeViewFixture(t, requiresApproval)
	search := newTaskSearch(t, fixture.metadata, fixture.projection)
	return fixture, search
}

func newTaskSearch(
	t *testing.T,
	metadataStore *metadata.Store,
	projection *TaskStatusProjection,
) *TaskSearch {
	t.Helper()
	search, err := NewTaskSearch(metadataStore, projection)
	if err != nil {
		t.Fatalf("NewTaskSearch: %v", err)
	}
	return search
}

func createTaskSearchTask(t *testing.T, fixture currentNodeViewFixture, title string, body string) workflowstore.TaskRecord {
	t.Helper()
	task, err := fixture.store.CreateTask(fixture.ctx, workflowstore.CreateTaskRequest{
		ProjectID:  fixture.binding.ProjectID,
		WorkflowID: &fixture.workflowID,
		Title:      title,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func createTaskSearchTaskAtSequence(
	t *testing.T,
	fixture currentNodeViewFixture,
	sequence int,
	title string,
	body string,
) workflowstore.TaskRecord {
	t.Helper()
	if _, err := fixture.metadata.DB().Exec(
		`UPDATE projects SET next_task_seq = ? WHERE id = ?`,
		sequence,
		fixture.binding.ProjectID,
	); err != nil {
		t.Fatalf("set current Project Task sequence: %v", err)
	}
	return createTaskSearchTask(t, fixture, title, body)
}

func startTaskSearchTask(t *testing.T, fixture currentNodeViewFixture, task workflowstore.TaskRecord) startedCurrentNodeViewTask {
	t.Helper()
	return fixture.startExistingTask(t, task)
}

func startTaskSearchScript(
	t *testing.T,
	fixture currentNodeViewFixture,
	shellPath string,
	started startedCurrentNodeViewTask,
) sessionruntime.ExecutionHandle {
	t.Helper()
	detached, err := fixture.authority.PrepareDetachedScriptExecution(fixture.ctx, sessionruntime.DetachedScriptExecutionRequest{
		Workflow: sessionruntime.WorkflowExecutionRef{
			ProjectID:   fixture.binding.ProjectID,
			WorkflowID:  fixture.workflowID,
			CurrentNode: started.currentNode,
		},
		Command: sessionruntime.ScriptCommand{
			Path: shellPath,
			Args: []string{"-c", "while :; do sleep 1; done"},
		},
	})
	if err != nil {
		t.Fatalf("PrepareDetachedScriptExecution: %v", err)
	}
	handle, launch, err := detached.Publish(context.Background(), func() error { return nil }, nil)
	if err != nil {
		t.Fatalf("Publish detached Script execution: %v", err)
	}
	launch()
	return handle
}

func taskSearchRequest(query string) *taskpb.SearchRequest {
	return &taskpb.SearchRequest{
		Mode:     taskpb.SearchMode_SEARCH_MODE_LITERAL,
		Query:    query,
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	}
}

func assertTaskSearchTask(t *testing.T, ctx context.Context, search *TaskSearch, request *taskpb.SearchRequest, taskID workflow.TaskID, kind taskpb.TaskStatusKind) {
	t.Helper()
	response, err := search.Search(ctx, request)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Groups) != 1 ||
		response.Groups[0].TaskId != string(taskID) ||
		response.Groups[0].Status.Kind != kind {
		t.Fatalf("search response = %+v", response)
	}
}

func assertTaskSearchEmpty(t *testing.T, ctx context.Context, search *TaskSearch, request *taskpb.SearchRequest) {
	t.Helper()
	response, err := search.Search(ctx, request)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Groups) != 0 || response.NextOffset != nil {
		t.Fatalf("empty search response = %+v", response)
	}
}
