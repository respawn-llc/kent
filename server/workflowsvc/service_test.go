package workflowsvc

import (
	"context"
	"strings"
	"testing"

	"builder/server/metadata"
	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/server/workflowview"
	"builder/shared/config"
	"builder/shared/serverapi"
)

func TestServiceCreatesValidatesLinksAndStartsDefaultWorkflowTask(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)

	created, err := service.CreateWorkflow(ctx, serverapi.WorkflowCreateRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, err := service.GetWorkflow(ctx, serverapi.WorkflowGetRequest{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	startID := workflowServiceNodeIDByKind(t, def.Definition, "start")
	doneID := workflowServiceNodeIDByKind(t, def.Definition, "terminal")
	agentID := "node-agent"
	if _, err := service.AddWorkflowNode(ctx, serverapi.WorkflowNodeAddRequest{WorkflowID: created.Workflow.ID, NodeID: agentID, Key: "agent", Kind: "agent", DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []serverapi.WorkflowOutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddWorkflowNode: %v", err)
	}
	if _, err := service.AddWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupAddRequest{WorkflowID: created.Workflow.ID, GroupID: "group-start", SourceNodeID: startID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddWorkflowTransitionGroup start: %v", err)
	}
	if _, err := service.AddWorkflowEdge(ctx, serverapi.WorkflowEdgeAddRequest{WorkflowID: created.Workflow.ID, EdgeID: "edge-start", TransitionGroupID: "group-start", Key: "start", TargetNodeID: agentID, ContextMode: "new_session"}); err != nil {
		t.Fatalf("AddWorkflowEdge start: %v", err)
	}
	if _, err := service.AddWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupAddRequest{WorkflowID: created.Workflow.ID, GroupID: "group-done", SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddWorkflowTransitionGroup done: %v", err)
	}
	if _, err := service.AddWorkflowEdge(ctx, serverapi.WorkflowEdgeAddRequest{WorkflowID: created.Workflow.ID, EdgeID: "edge-done", TransitionGroupID: "group-done", Key: "done", TargetNodeID: doneID, ContextMode: "new_session", OutputRequirements: []serverapi.WorkflowOutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddWorkflowEdge done: %v", err)
	}
	validated, err := service.ValidateWorkflow(ctx, serverapi.WorkflowValidateRequest{WorkflowID: created.Workflow.ID, Mode: serverapi.WorkflowValidationModeExecution})
	if err != nil {
		t.Fatalf("ValidateWorkflow: %v", err)
	}
	if !validated.Valid || len(validated.Errors) != 0 {
		t.Fatalf("validated = %+v, want valid", validated)
	}
	for _, mode := range []serverapi.WorkflowValidationMode{serverapi.WorkflowValidationModeDraft, serverapi.WorkflowValidationModeTaskCreation, serverapi.WorkflowValidationModeExecution} {
		if _, err := service.ValidateWorkflow(ctx, serverapi.WorkflowValidateRequest{WorkflowID: created.Workflow.ID, Mode: mode}); err != nil {
			t.Fatalf("ValidateWorkflow mode %q: %v", mode, err)
		}
	}
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: created.Workflow.ID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	if !strings.HasPrefix(task.Task.ShortID, "WOR-1") || task.Task.WorkflowID != created.Workflow.ID {
		t.Fatalf("task response = %+v", task.Task)
	}
	started, err := service.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: task.Task.ID})
	if err != nil {
		t.Fatalf("StartWorkflowTask: %v", err)
	}
	if started.RunID == "" || started.PlacementID == "" {
		t.Fatalf("start response = %+v", started)
	}
}

func TestServiceCreatesAndUpdatesTaskSourceWorkspaceBeforeStart(t *testing.T) {
	ctx := context.Background()
	service, binding, metadataStore := newWorkflowServiceTestServiceWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	source, err := metadataStore.AttachWorkspaceToProject(ctx, binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatalf("AttachWorkspaceToProject source: %v", err)
	}

	created, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", SourceWorkspaceID: source.WorkspaceID})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	if created.Task.SourceWorkspaceID != source.WorkspaceID || created.Task.BodyPreview != "" {
		t.Fatalf("created task = %+v", created.Task)
	}
	updated, err := service.UpdateWorkflowTask(ctx, serverapi.WorkflowTaskUpdateRequest{TaskID: created.Task.ID, Title: "Updated", Body: "Details", SourceWorkspaceID: binding.WorkspaceID})
	if err != nil {
		t.Fatalf("UpdateWorkflowTask: %v", err)
	}
	if updated.Task.Title != "Updated" || updated.Task.SourceWorkspaceID != binding.WorkspaceID || updated.Task.BodyPreview != "Details" {
		t.Fatalf("updated task = %+v", updated.Task)
	}
	started, err := service.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: created.Task.ID})
	if err != nil {
		t.Fatalf("StartWorkflowTask: %v", err)
	}
	if started.RunID == "" {
		t.Fatalf("start response = %+v", started)
	}
	if _, err := service.UpdateWorkflowTask(ctx, serverapi.WorkflowTaskUpdateRequest{TaskID: created.Task.ID, Title: "Too late", Body: "", SourceWorkspaceID: binding.WorkspaceID}); err == nil || !strings.Contains(err.Error(), "automation starts") {
		t.Fatalf("UpdateWorkflowTask after start error = %v", err)
	}
	events, err := service.store.ListWorkflowEventsAfter(ctx, binding.ProjectID, 0, 100)
	if err != nil {
		t.Fatalf("ListWorkflowEvents: %v", err)
	}
	actions := map[string]bool{}
	for _, event := range events {
		if event.Resource == "task" {
			actions[event.Action] = true
		}
	}
	if !actions["created"] || !actions["updated"] || !actions["started"] {
		t.Fatalf("task events = %+v, want created/updated/started", events)
	}
}

func TestServiceTaskStartValidatesCurrentGraph(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	def, err := service.GetWorkflow(ctx, serverapi.WorkflowGetRequest{WorkflowID: workflowID})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	doneID := workflowServiceNodeIDByKind(t, def.Definition, "terminal")
	if _, err := service.AddWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupAddRequest{WorkflowID: workflowID, GroupID: "group-invalid", SourceNodeID: doneID, TransitionID: "invalid", DisplayName: "Invalid"}); err != nil {
		t.Fatalf("AddWorkflowTransitionGroup invalid: %v", err)
	}
	if _, err := service.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: task.Task.ID}); err == nil || !strings.Contains(err.Error(), string(workflow.CodeTerminalHasOutgoingEdge)) {
		t.Fatalf("expected current graph validation error, got %v", err)
	}
}

func TestServiceTaskStartEnsuresTaskWorktreeBeforeRun(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	ensurer := &recordingTaskWorktreeEnsurer{hook: func(taskID string) {
		runs, err := service.store.ListRuns(ctx, workflow.TaskID(taskID))
		if err != nil {
			t.Fatalf("ListRuns during ensure: %v", err)
		}
		if len(runs) != 0 {
			t.Fatalf("task worktree ensure happened after run creation: %+v", runs)
		}
	}}
	service.taskWorktrees = ensurer
	if _, err := service.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: task.Task.ID}); err != nil {
		t.Fatalf("StartWorkflowTask: %v", err)
	}
	if ensurer.taskID != task.Task.ID {
		t.Fatalf("ensured task id = %q, want %q", ensurer.taskID, task.Task.ID)
	}
}

func TestServiceRejectsUnlinkedWorkflowAndInvalidDefault(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	unlinked, err := service.CreateWorkflow(ctx, serverapi.WorkflowCreateRequest{Name: "Unlinked"})
	if err != nil {
		t.Fatalf("CreateWorkflow unlinked: %v", err)
	}
	if _, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, WorkflowID: unlinked.Workflow.ID, Title: "Task", Body: "Body"}); err == nil {
		t.Fatalf("expected unlinked workflow task create to fail")
	}
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: unlinked.Workflow.ID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject invalid default: %v", err)
	}
	if _, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"}); err == nil || !strings.Contains(err.Error(), "workflow validation failed") {
		t.Fatalf("expected invalid default workflow error, got %v", err)
	}
}

func TestServiceStartTaskAutomationValidatesEnsuresWorktreeAndRecordsRunnableRun(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	ensurer := &recordingTaskWorktreeEnsurer{hook: func(taskID string) {
		runs, err := service.store.ListRuns(ctx, workflow.TaskID(taskID))
		if err != nil {
			t.Fatalf("ListRuns during ensure: %v", err)
		}
		if len(runs) != 0 {
			t.Fatalf("worktree ensure happened after automation intent: %+v", runs)
		}
	}}
	service.taskWorktrees = ensurer

	started, err := service.StartTaskAutomation(ctx, task.Task.ID)
	if err != nil {
		t.Fatalf("StartTaskAutomation: %v", err)
	}
	if ensurer.taskID != task.Task.ID {
		t.Fatalf("ensured task id = %q, want %q", ensurer.taskID, task.Task.ID)
	}
	runs, err := service.store.ListRuns(ctx, workflow.TaskID(task.Task.ID))
	if err != nil {
		t.Fatalf("ListRuns after automation: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != workflow.RunID(started.RunID) || runs[0].AutomationRequestedAt == 0 {
		t.Fatalf("runs after automation = %+v", runs)
	}
	notifier := &recordingSchedulerNotifier{}
	service.schedulerWake = notifier
	if _, err := service.StartTaskAutomation(ctx, task.Task.ID); err == nil {
		t.Fatalf("expected second start to fail")
	}
	if notifier.count != 0 {
		t.Fatalf("scheduler notified on failed start")
	}
	transitions, err := service.store.ListTransitions(ctx, workflow.TaskID(task.Task.ID))
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 1 || transitions[0].TransitionID != "start" {
		t.Fatalf("start transition not applied: %+v", transitions)
	}
}

func TestServiceStartTaskAutomationNotifiesScheduler(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	notifier := &recordingSchedulerNotifier{}
	service.schedulerWake = notifier

	if _, err := service.StartTaskAutomation(ctx, task.Task.ID); err != nil {
		t.Fatalf("StartTaskAutomation: %v", err)
	}
	if notifier.count != 1 {
		t.Fatalf("scheduler notifications = %d, want 1", notifier.count)
	}
}

func TestServiceCancelTaskCancelsActiveRuntime(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	if _, err := service.StartTaskAutomation(ctx, task.Task.ID); err != nil {
		t.Fatalf("StartTaskAutomation: %v", err)
	}
	canceler := &recordingTaskRuntimeCanceler{}
	service.runtimeCancel = canceler

	if err := service.CancelWorkflowTask(ctx, serverapi.WorkflowTaskCancelRequest{TaskID: task.Task.ID, Reason: "stop"}); err != nil {
		t.Fatalf("CancelWorkflowTask: %v", err)
	}
	if len(canceler.taskIDs) != 1 || canceler.taskIDs[0] != workflow.TaskID(task.Task.ID) {
		t.Fatalf("canceled tasks = %+v", canceler.taskIDs)
	}
}

func TestServiceResumeTaskRequeuesRunAndNotifiesScheduler(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	started, err := service.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: task.Task.ID})
	if err != nil {
		t.Fatalf("StartWorkflowTask: %v", err)
	}
	claimed, err := service.store.ClaimRun(ctx, workflow.RunID(started.RunID), 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	if err := service.store.InterruptRunGeneration(ctx, workflow.RunID(started.RunID), claimed.Generation, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration: %v", err)
	}
	notifier := &recordingSchedulerNotifier{}
	service.schedulerWake = notifier

	resumed, err := service.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{TaskID: task.Task.ID})
	if err != nil {
		t.Fatalf("ResumeWorkflowTask: %v", err)
	}
	if resumed.RunID != started.RunID || resumed.Generation <= claimed.Generation || resumed.PlacementID == "" || resumed.NodeID == "" {
		t.Fatalf("resume response = %+v, want same run requeued", resumed)
	}
	if notifier.count != 1 {
		t.Fatalf("scheduler notifications = %d, want 1", notifier.count)
	}
}

type recordingSchedulerNotifier struct {
	count int
}

func (n *recordingSchedulerNotifier) Notify() {
	n.count++
}

type recordingTaskRuntimeCanceler struct {
	taskIDs []workflow.TaskID
}

func (c *recordingTaskRuntimeCanceler) CancelTaskRuns(_ context.Context, taskID workflow.TaskID) error {
	c.taskIDs = append(c.taskIDs, taskID)
	return nil
}

type recordingTaskWorktreeEnsurer struct {
	taskID string
	hook   func(string)
}

func (e *recordingTaskWorktreeEnsurer) EnsureTaskWorktree(ctx context.Context, taskID string) error {
	e.taskID = taskID
	if e.hook != nil {
		e.hook(taskID)
	}
	return nil
}

func TestServiceDefaultWorkflowResolvesWithinProjectOnly(t *testing.T) {
	ctx := context.Background()
	service, bindingA, metadataStore := newWorkflowServiceTestServiceWithMetadata(t)
	workspaceB := t.TempDir()
	cfgB, err := config.Load(workspaceB, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load B: %v", err)
	}
	bindingB, err := metadataStore.RegisterWorkspaceBinding(ctx, cfgB.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding B: %v", err)
	}
	if err := metadataStore.SetProjectKey(ctx, bindingB.ProjectID, "TWO"); err != nil {
		t.Fatalf("SetProjectKey B: %v", err)
	}
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: bindingA.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject A: %v", err)
	}
	if _, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: bindingB.ProjectID, Title: "Task", Body: "Body"}); err == nil {
		t.Fatalf("expected project B task create to fail without project-scoped default workflow")
	}
}

func TestServiceWorkflowUnlinkRejectsNonTerminalAndSoftDisablesTerminalHistory(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	link, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true})
	if err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	if err := service.UnlinkWorkflowFromProject(ctx, serverapi.WorkflowUnlinkProjectRequest{LinkID: link.Link.ID}); err == nil || !strings.Contains(err.Error(), "non-terminal") {
		t.Fatalf("expected non-terminal unlink guard, got %v", err)
	}
	started, err := service.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{TaskID: task.Task.ID})
	if err != nil {
		t.Fatalf("StartWorkflowTask: %v", err)
	}
	if _, err := service.store.CompleteRun(ctx, workflowstore.CompleteRunRequest{RunID: workflow.RunID(started.RunID), TransitionID: "done", OutputValues: map[string]string{"summary": "done"}}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if err := service.UnlinkWorkflowFromProject(ctx, serverapi.WorkflowUnlinkProjectRequest{LinkID: link.Link.ID}); err != nil {
		t.Fatalf("terminal history unlink: %v", err)
	}
	links, err := service.store.ListProjectWorkflowLinks(ctx, binding.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectWorkflowLinks: %v", err)
	}
	if len(links) != 1 || links[0].UnlinkedAtUnixMs == 0 {
		t.Fatalf("links after unlink = %+v", links)
	}
}

func TestServiceCommentsAndReadModels(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: workflowID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}
	task, err := service.CreateWorkflowTask(ctx, serverapi.WorkflowTaskCreateRequest{ProjectID: binding.ProjectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateWorkflowTask: %v", err)
	}
	comment, err := service.AddWorkflowTaskComment(ctx, serverapi.WorkflowTaskCommentAddRequest{TaskID: task.Task.ID, Body: "note", Author: "user"})
	if err != nil {
		t.Fatalf("AddWorkflowTaskComment: %v", err)
	}
	comments, err := service.ListWorkflowTaskComments(ctx, serverapi.WorkflowTaskCommentListRequest{TaskID: task.Task.ID})
	if err != nil {
		t.Fatalf("ListWorkflowTaskComments: %v", err)
	}
	if len(comments.Comments) != 1 || comments.Comments[0].ID != comment.Comment.ID {
		t.Fatalf("comments = %+v", comments)
	}
	board, err := service.GetWorkflowBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID})
	if err != nil {
		t.Fatalf("GetWorkflowBoard: %v", err)
	}
	if len(board.Board.Cards) != 1 {
		t.Fatalf("board = %+v", board.Board)
	}
	detail, err := service.GetWorkflowTask(ctx, serverapi.WorkflowTaskGetRequest{TaskID: task.Task.ID})
	if err != nil {
		t.Fatalf("GetWorkflowTask: %v", err)
	}
	if detail.Task.Summary.ID != task.Task.ID || len(detail.Task.Comments) != 1 {
		t.Fatalf("detail = %+v", detail.Task)
	}
}

func TestServiceWorkflowProjectSubscriptionReplaysEvents(t *testing.T) {
	ctx := context.Background()
	service, binding := newWorkflowServiceTestService(t)
	created, err := service.CreateWorkflow(ctx, serverapi.WorkflowCreateRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := service.LinkWorkflowToProject(ctx, serverapi.WorkflowLinkProjectRequest{ProjectID: binding.ProjectID, WorkflowID: created.Workflow.ID, Default: true}); err != nil {
		t.Fatalf("LinkWorkflowToProject: %v", err)
	}

	sub, err := service.SubscribeWorkflowProject(ctx, serverapi.WorkflowProjectSubscribeRequest{ProjectID: binding.ProjectID, AfterSequence: 0})
	if err != nil {
		t.Fatalf("SubscribeWorkflowProject: %v", err)
	}
	defer func() { _ = sub.Close() }()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatalf("subscription Next: %v", err)
	}
	if event.Sequence == 0 || event.ProjectID != binding.ProjectID || event.WorkflowID != created.Workflow.ID || event.Resource != "workflow_link" {
		t.Fatalf("event = %+v, want workflow link event", event)
	}
	board, err := service.GetWorkflowBoard(ctx, serverapi.WorkflowBoardRequest{ProjectID: binding.ProjectID})
	if err != nil {
		t.Fatalf("GetWorkflowBoard: %v", err)
	}
	if board.Board.LatestEventSequence < event.Sequence {
		t.Fatalf("board watermark = %d, want >= event %d", board.Board.LatestEventSequence, event.Sequence)
	}
}

func newWorkflowServiceTestService(t *testing.T) (*Service, metadata.Binding) {
	t.Helper()
	service, binding, _ := newWorkflowServiceTestServiceWithMetadata(t)
	return service, binding
}

func newWorkflowServiceTestServiceWithMetadata(t *testing.T) (*Service, metadata.Binding, *metadata.Store) {
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
	resolver := workflow.StaticRoleResolver{"coder": true}
	store, err := workflowstore.New(metadataStore, workflowstore.WithRoleResolver(resolver))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	view, err := workflowview.New(metadataStore)
	if err != nil {
		t.Fatalf("workflowview.New: %v", err)
	}
	service, err := New(store, view, resolver)
	if err != nil {
		t.Fatalf("workflowsvc.New: %v", err)
	}
	return service, binding, metadataStore
}

func createWorkflowServiceValidWorkflow(t *testing.T, ctx context.Context, service *Service) string {
	t.Helper()
	created, err := service.CreateWorkflow(ctx, serverapi.WorkflowCreateRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, err := service.GetWorkflow(ctx, serverapi.WorkflowGetRequest{WorkflowID: created.Workflow.ID})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	startID := workflowServiceNodeIDByKind(t, def.Definition, "start")
	doneID := workflowServiceNodeIDByKind(t, def.Definition, "terminal")
	if _, err := service.AddWorkflowNode(ctx, serverapi.WorkflowNodeAddRequest{WorkflowID: created.Workflow.ID, NodeID: "node-agent-" + created.Workflow.ID, Key: "agent", Kind: "agent", DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []serverapi.WorkflowOutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddWorkflowNode: %v", err)
	}
	agentID := "node-agent-" + created.Workflow.ID
	if _, err := service.AddWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupAddRequest{WorkflowID: created.Workflow.ID, GroupID: "group-start-" + created.Workflow.ID, SourceNodeID: startID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddWorkflowTransitionGroup start: %v", err)
	}
	if _, err := service.AddWorkflowEdge(ctx, serverapi.WorkflowEdgeAddRequest{WorkflowID: created.Workflow.ID, EdgeID: "edge-start-" + created.Workflow.ID, TransitionGroupID: "group-start-" + created.Workflow.ID, Key: "start", TargetNodeID: agentID, ContextMode: "new_session"}); err != nil {
		t.Fatalf("AddWorkflowEdge start: %v", err)
	}
	if _, err := service.AddWorkflowTransitionGroup(ctx, serverapi.WorkflowTransitionGroupAddRequest{WorkflowID: created.Workflow.ID, GroupID: "group-done-" + created.Workflow.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddWorkflowTransitionGroup done: %v", err)
	}
	if _, err := service.AddWorkflowEdge(ctx, serverapi.WorkflowEdgeAddRequest{WorkflowID: created.Workflow.ID, EdgeID: "edge-done-" + created.Workflow.ID, TransitionGroupID: "group-done-" + created.Workflow.ID, Key: "done", TargetNodeID: doneID, ContextMode: "new_session", OutputRequirements: []serverapi.WorkflowOutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddWorkflowEdge done: %v", err)
	}
	return created.Workflow.ID
}

func workflowServiceNodeIDByKind(t *testing.T, def serverapi.WorkflowDefinition, kind string) string {
	t.Helper()
	for _, node := range def.Nodes {
		if node.Kind == kind {
			return node.ID
		}
	}
	t.Fatalf("missing node kind %q in %+v", kind, def.Nodes)
	return ""
}
