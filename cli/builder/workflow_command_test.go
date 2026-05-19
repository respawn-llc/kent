package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"builder/server/metadata"
	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/server/workflowsvc"
	"builder/server/workflowview"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
)

type workflowCommandLoopbackRemote struct {
	client.WorkflowClient
	cfg     config.App
	binding metadata.Binding
	store   *workflowstore.Store
}

func (r *workflowCommandLoopbackRemote) Close() error { return nil }

func (r *workflowCommandLoopbackRemote) ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	if req.Path != r.cfg.WorkspaceRoot {
		return serverapi.ProjectResolvePathResponse{}, nil
	}
	return serverapi.ProjectResolvePathResponse{Binding: &serverapi.ProjectBinding{ProjectID: r.binding.ProjectID, WorkspaceID: r.binding.WorkspaceID, CanonicalRoot: r.cfg.WorkspaceRoot}}, nil
}

func TestWorkflowAndTaskCommandsUseWorkflowAPI(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowOut, workflowErr, code := runWorkflowRootCommand("workflow", "create", "Workflow")
	if code != 0 {
		t.Fatalf("workflow create exit=%d stderr=%q", code, workflowErr)
	}
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	if workflowID == "" {
		t.Fatalf("workflow create output = %q", workflowOut)
	}

	inspectOut, inspectErr, code := runWorkflowRootCommand("workflow", "inspect", workflowID)
	if code != 0 {
		t.Fatalf("workflow inspect exit=%d stderr=%q", code, inspectErr)
	}
	if !strings.Contains(inspectOut, "backlog") || !strings.Contains(inspectOut, "done") {
		t.Fatalf("inspect output = %q, want auto-created backlog and done", inspectOut)
	}

	listOut, listErr, code := runWorkflowRootCommand("workflow", "list")
	if code != 0 {
		t.Fatalf("workflow list exit=%d stderr=%q", code, listErr)
	}
	if !strings.Contains(listOut, workflowID) {
		t.Fatalf("workflow list output = %q, want workflow id", listOut)
	}

	validateOut, _, code := runWorkflowRootCommand("workflow", "validate", workflowID)
	if code == 0 || !strings.Contains(validateOut, "valid\tfalse") {
		t.Fatalf("invalid workflow validate code=%d output=%q", code, validateOut)
	}

	nodeOut, nodeErr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work")
	if code != 0 {
		t.Fatalf("workflow node add exit=%d stderr=%q", code, nodeErr)
	}
	if labeledOutputValue(t, nodeOut, "node_id") == "" {
		t.Fatalf("node output = %q, want node id", nodeOut)
	}

	if _, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session"); code != 0 {
		t.Fatalf("workflow start edge add exit=%d stderr=%q", code, edgeErr)
	}
	edgeOut, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "implement", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "new_session")
	if code != 0 {
		t.Fatalf("workflow done edge add exit=%d stderr=%q", code, edgeErr)
	}
	if labeledOutputValue(t, edgeOut, "edge_id") == "" || labeledOutputValue(t, edgeOut, "group_id") == "" {
		t.Fatalf("edge output = %q, want edge and group ids", edgeOut)
	}

	linkOut, linkErr, code := runWorkflowRootCommand("workflow", "link", binding.ProjectID, workflowID, "--default")
	if code != 0 {
		t.Fatalf("workflow link exit=%d stderr=%q", code, linkErr)
	}
	linkID := labeledOutputValue(t, linkOut, "link_id")
	if linkID == "" {
		t.Fatalf("link output = %q, want link id", linkOut)
	}

	defaultOut, defaultErr, code := runWorkflowRootCommand("workflow", "default", binding.ProjectID, workflowID)
	if code != 0 {
		t.Fatalf("workflow default exit=%d stderr=%q", code, defaultErr)
	}
	if !strings.Contains(defaultOut, linkID) {
		t.Fatalf("default output = %q, want link id %s", defaultOut, linkID)
	}

	unlinkOut, unlinkErr, code := runWorkflowRootCommand("workflow", "unlink", binding.ProjectID, workflowID)
	if code != 0 {
		t.Fatalf("workflow unlink exit=%d stderr=%q", code, unlinkErr)
	}
	if !strings.Contains(unlinkOut, linkID) {
		t.Fatalf("unlink output = %q, want link id %s", unlinkOut, linkID)
	}

	if _, linkErr, code = runWorkflowRootCommand("workflow", "link", binding.ProjectID, workflowID, "--default"); code != 0 {
		t.Fatalf("workflow relink exit=%d stderr=%q", code, linkErr)
	}
	validateOut, validateErr, code := runWorkflowRootCommand("workflow", "validate", workflowID)
	if code != 0 {
		t.Fatalf("valid workflow validate exit=%d stdout=%q stderr=%q", code, validateOut, validateErr)
	}
	if !strings.Contains(validateOut, "valid\ttrue") {
		t.Fatalf("validate output = %q, want valid true", validateOut)
	}

	taskOut, taskErr, code := runWorkflowRootCommand("task", "create", "--title", "Task", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID)
	if code != 0 {
		t.Fatalf("task create exit=%d stderr=%q", code, taskErr)
	}
	taskID := labeledOutputValue(t, taskOut, "task_id")
	shortID := labeledOutputValue(t, taskOut, "short_id")
	if taskID == "" || shortID == "" {
		t.Fatalf("task output = %q, want task and short ids", taskOut)
	}

	taskListOut, taskListErr, code := runWorkflowRootCommand("task", "list", "--project", binding.ProjectID)
	if code != 0 {
		t.Fatalf("task list exit=%d stderr=%q", code, taskListErr)
	}
	if !strings.Contains(taskListOut, shortID) || !strings.Contains(taskListOut, taskID) {
		t.Fatalf("task list output = %q, want ids", taskListOut)
	}

	taskShowOut, taskShowErr, code := runWorkflowRootCommand("task", "show", "--project", binding.ProjectID, shortID)
	if code != 0 {
		t.Fatalf("task show exit=%d stderr=%q", code, taskShowErr)
	}
	if !strings.Contains(taskShowOut, "placements") || !strings.Contains(taskShowOut, taskID) {
		t.Fatalf("task show output = %q, want placement section", taskShowOut)
	}
	taskShowOut, taskShowErr, code = runWorkflowRootCommand("task", "show", "--project", "missing-project", taskID)
	if code != 0 {
		t.Fatalf("task show by full id exit=%d stderr=%q", code, taskShowErr)
	}
	if !strings.Contains(taskShowOut, taskID) {
		t.Fatalf("task show by full id output = %q, want task id", taskShowOut)
	}

	commentOut, commentErr, code := runWorkflowRootCommand("task", "comment", "add", "--project", binding.ProjectID, "--body", "note", shortID)
	if code != 0 {
		t.Fatalf("comment add exit=%d stderr=%q", code, commentErr)
	}
	commentID := labeledOutputValue(t, commentOut, "comment_id")
	if commentID == "" {
		t.Fatalf("comment output = %q, want comment id", commentOut)
	}
	if _, replaceErr, code := runWorkflowRootCommand("task", "comment", "replace", "--body", "edited", commentID); code != 0 {
		t.Fatalf("comment replace exit=%d stderr=%q", code, replaceErr)
	}
	commentListOut, commentListErr, code := runWorkflowRootCommand("task", "comment", "list", "--project", binding.ProjectID, shortID)
	if code != 0 {
		t.Fatalf("comment list exit=%d stderr=%q", code, commentListErr)
	}
	if !strings.Contains(commentListOut, commentID) || !strings.Contains(commentListOut, "edited") {
		t.Fatalf("comment list output = %q, want edited comment", commentListOut)
	}
	if _, deleteErr, code := runWorkflowRootCommand("task", "comment", "delete", commentID); code != 0 {
		t.Fatalf("comment delete exit=%d stderr=%q", code, deleteErr)
	}

	startOut, startErr, code := runWorkflowRootCommand("task", "start", "--project", binding.ProjectID, shortID)
	if code != 0 {
		t.Fatalf("task start exit=%d stderr=%q", code, startErr)
	}
	runID := labeledOutputValue(t, startOut, "run_id")
	if runID == "" {
		t.Fatalf("start output = %q, want run id", startOut)
	}
	claimed, err := remote.store.ClaimRun(context.Background(), workflow.RunID(runID), 0)
	if err != nil {
		t.Fatalf("ClaimRun for resume command: %v", err)
	}
	if err := remote.store.InterruptRunGeneration(context.Background(), workflow.RunID(runID), claimed.Generation, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration for resume command: %v", err)
	}
	resumeOut, resumeErr, code := runWorkflowRootCommand("task", "resume", "--project", binding.ProjectID, shortID)
	if code != 0 {
		t.Fatalf("task resume exit=%d stderr=%q", code, resumeErr)
	}
	if labeledOutputValue(t, resumeOut, "run_id") != runID {
		t.Fatalf("resume output = %q, want run id %s", resumeOut, runID)
	}

	cancelOut, cancelErr, code := runWorkflowRootCommand("task", "cancel", "--project", binding.ProjectID, "--reason", "test", shortID)
	if code != 0 {
		t.Fatalf("task cancel exit=%d stderr=%q", code, cancelErr)
	}
	if !strings.Contains(cancelOut, taskID) {
		t.Fatalf("cancel output = %q, want task id", cancelOut)
	}

	if _, resumeErr, resumeCode := runWorkflowRootCommand("task", "resume"); resumeCode != 2 || !strings.Contains(resumeErr, "requires <short-id-or-task-id>") {
		t.Fatalf("task resume validation code=%d stderr=%q, want task requirement", resumeCode, resumeErr)
	}
	if _, approveErr, approveCode := runWorkflowRootCommand("task", "approve"); approveCode != 2 || !strings.Contains(approveErr, "requires <transition-id>") {
		t.Fatalf("task approve validation code=%d stderr=%q, want transition id requirement", approveCode, approveErr)
	}
	if _, moveErr, moveCode := runWorkflowRootCommand("task", "move"); moveCode != 2 || !strings.Contains(moveErr, "requires <short-id-or-task-id> <target-node-id>") {
		t.Fatalf("task move validation code=%d stderr=%q, want task and target requirement", moveCode, moveErr)
	}
}

type pagedTaskListRemote struct {
	client.WorkflowClient
	board    serverapi.WorkflowBoard
	pages    map[string]serverapi.WorkflowBoardNodeCardsListResponse
	requests []serverapi.WorkflowBoardNodeCardsListRequest
}

func (r *pagedTaskListRemote) Close() error { return nil }

func (r *pagedTaskListRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *pagedTaskListRemote) GetWorkflowBoard(context.Context, serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
	return serverapi.WorkflowBoardResponse{Board: r.board}, nil
}

func (r *pagedTaskListRemote) ListWorkflowBoardNodeCards(_ context.Context, req serverapi.WorkflowBoardNodeCardsListRequest) (serverapi.WorkflowBoardNodeCardsListResponse, error) {
	r.requests = append(r.requests, req)
	key := req.NodeID + "|" + req.PageToken
	return r.pages[key], nil
}

func TestTaskListFetchesPaginatedNodeCardsWithoutDuplicates(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &pagedTaskListRemote{
		board: serverapi.WorkflowBoard{
			ProjectID:        "project-1",
			SelectedWorkflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1"},
			Columns: []serverapi.WorkflowBoardColumn{
				{Node: serverapi.WorkflowBoardNodeSummary{NodeID: "node-a"}},
				{Node: serverapi.WorkflowBoardNodeSummary{NodeID: "node-b"}},
			},
		},
		pages: map[string]serverapi.WorkflowBoardNodeCardsListResponse{
			"node-a|": {
				Cards:         []serverapi.WorkflowBoardTaskCard{testTaskCard("task-a", "BLD-1", "A")},
				NextPageToken: "a-next",
			},
			"node-a|a-next": {
				Cards: []serverapi.WorkflowBoardTaskCard{testTaskCard("task-b", "BLD-2", "B")},
			},
			"node-b|": {
				Cards: []serverapi.WorkflowBoardTaskCard{
					testTaskCard("task-b", "BLD-2", "B"),
					testTaskCard("task-c", "BLD-3", "C"),
				},
			},
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "list", "--project", "project-1")
	if code != 0 {
		t.Fatalf("task list exit=%d stderr=%q", code, stderr)
	}
	for _, shortID := range []string{"BLD-1", "BLD-2", "BLD-3"} {
		if got := strings.Count(stdout, shortID+"\t"); got != 1 {
			t.Fatalf("task list output = %q, want %s exactly once, got %d", stdout, shortID, got)
		}
	}
	if len(remote.requests) != 3 || remote.requests[1].NodeID != "node-a" || remote.requests[1].PageToken != "a-next" {
		t.Fatalf("node card requests = %+v, want paginated node-a fetch plus node-b fetch", remote.requests)
	}
}

func testTaskCard(taskID string, shortID string, title string) serverapi.WorkflowBoardTaskCard {
	return serverapi.WorkflowBoardTaskCard{
		TaskID:     taskID,
		ShortID:    shortID,
		Title:      title,
		WorkflowID: "workflow-1",
		Status:     serverapi.WorkflowTaskStatus{Kind: "active"},
	}
}

func TestWriteTaskDetailIncludesParallelBranchIDs(t *testing.T) {
	var stdout bytes.Buffer
	writeTaskDetail(&stdout, serverapi.WorkflowTaskDetail{
		Summary: serverapi.WorkflowTaskSummary{ID: "task-1", ShortID: "WOR-1", WorkflowID: "workflow-1", Title: "Task"},
		Placements: []serverapi.WorkflowPlacement{{
			ID:                        "placement-1",
			TaskID:                    "task-1",
			NodeID:                    "node-impl-a",
			State:                     "active",
			ParallelBatchTransitionID: "transition-split",
			ParallelBranchEdgeID:      "edge-split-a",
		}},
	})

	output := stdout.String()
	if !strings.Contains(output, "placement-1\tnode-impl-a\tactive\ttransition-split\tedge-split-a") {
		t.Fatalf("task detail output = %q, want parallel batch and branch ids", output)
	}
}

func TestWorkflowCommandValidationErrorsAreActionable(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowOut, workflowErr, code := runWorkflowRootCommand("workflow", "create", "Workflow")
	if code != 0 {
		t.Fatalf("workflow create exit=%d stderr=%q", code, workflowErr)
	}
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	_, stderr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "Bad-Key", "--kind", "agent")
	if code == 0 || !strings.Contains(stderr, "key must start with a lowercase letter") {
		t.Fatalf("invalid node code=%d stderr=%q, want actionable key validation", code, stderr)
	}
}

func TestWorkflowTaskCommandsDoNotAdvertiseJSONContract(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := workflowSubcommand([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("workflow help exit=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "--json") || strings.Contains(stdout.String(), "--json") {
		t.Fatalf("workflow help advertised json contract stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := taskSubcommand([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("task help exit=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "--json") || strings.Contains(stdout.String(), "--json") {
		t.Fatalf("task help advertised json contract stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func newWorkflowCommandLoopback(t *testing.T) (config.App, metadata.Binding, *workflowCommandLoopbackRemote) {
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
	resolver := workflow.StaticRoleResolver{"workflow-test": true}
	store, err := workflowstore.New(metadataStore, workflowstore.WithRoleResolver(resolver))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	view, err := workflowview.New(metadataStore)
	if err != nil {
		t.Fatalf("workflowview.New: %v", err)
	}
	service, err := workflowsvc.New(store, view, resolver)
	if err != nil {
		t.Fatalf("workflowsvc.New: %v", err)
	}
	remote := &workflowCommandLoopbackRemote{WorkflowClient: client.NewLoopbackWorkflowClient(service), cfg: cfg, binding: binding, store: store}
	return cfg, binding, remote
}

func replaceWorkflowCommandRemoteOpener(t *testing.T, cfg config.App, remote workflowCommandRemote) func() {
	t.Helper()
	original := workflowCommandRemoteOpener
	workflowCommandRemoteOpener = func(context.Context, string) (config.App, workflowCommandRemote, error) {
		return cfg, remote, nil
	}
	return func() { workflowCommandRemoteOpener = original }
}

func runWorkflowRootCommand(args ...string) (string, string, int) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := rootCommand(args, strings.NewReader(""), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func labeledOutputValue(t *testing.T, output string, label string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 2 && fields[0] == label {
			return fields[1]
		}
	}
	if strings.TrimSpace(output) == "" {
		t.Fatalf("label %q not found in empty output", label)
	}
	return ""
}

func TestWorkflowProjectPathResolutionRejectsUnboundPath(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	_, stderr, code := runWorkflowRootCommand("task", "list", "--project", t.TempDir())
	if code == 0 || !strings.Contains(stderr, "workspace is not registered") {
		t.Fatalf("task list unbound path code=%d stderr=%q", code, stderr)
	}
}
