package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"core/prompts"
	"core/server/metadata"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/server/workflowsvc"
	"core/server/workflowview"
	"core/shared/client"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/sessionenv"
)

type workflowCommandLoopbackRemote struct {
	client.WorkflowClient
	cfg                   config.App
	binding               metadata.Binding
	projectBindingsByRoot map[string]serverapi.ProjectBinding
	metadataStore         *metadata.Store
	store                 *workflowstore.Store
}

func (r *workflowCommandLoopbackRemote) Close() error { return nil }

type failingWorkflowEdgeUpdateRemote struct {
	*workflowCommandLoopbackRemote
	failUpdateEdge bool
}

func (r *failingWorkflowEdgeUpdateRemote) UpdateWorkflowEdge(ctx context.Context, req serverapi.WorkflowEdgeUpdateRequest) (serverapi.WorkflowEdgeUpdateResponse, error) {
	if r.failUpdateEdge {
		return serverapi.WorkflowEdgeUpdateResponse{}, errors.New("edge update failed")
	}
	return r.workflowCommandLoopbackRemote.UpdateWorkflowEdge(ctx, req)
}

func (r *workflowCommandLoopbackRemote) ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	if binding, ok := r.projectBindingsByRoot[req.Path]; ok {
		return serverapi.ProjectResolvePathResponse{CanonicalRoot: req.Path, Binding: &binding}, nil
	}
	if req.Path != r.cfg.WorkspaceRoot {
		return serverapi.ProjectResolvePathResponse{}, nil
	}
	return serverapi.ProjectResolvePathResponse{Binding: &serverapi.ProjectBinding{ProjectID: r.binding.ProjectID, WorkspaceID: r.binding.WorkspaceID, CanonicalRoot: r.cfg.WorkspaceRoot}}, nil
}

func TestWorkflowAndTaskCommandsUseWorkflowAPI(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Workflow").ID
	if workflowID == "" {
		t.Fatal("workflow create did not return a workflow id")
	}

	inspectOut, _ := runWorkflowRootCommandOK(t, "workflow", "inspect", workflowID)
	if !strings.Contains(inspectOut, "backlog (start)") || !strings.Contains(inspectOut, "done (terminal)") {
		t.Fatalf("inspect output = %q, want auto-created backlog and done", inspectOut)
	}

	listOut, _ := runWorkflowRootCommandOK(t, "workflow", "list")
	if !strings.Contains(listOut, workflowID) {
		t.Fatalf("workflow list output = %q, want workflow id", listOut)
	}

	if validation, code := workflowValidateJSONForTest(t, workflowID); code == 0 || validation.Valid {
		t.Fatalf("invalid workflow validate code=%d valid=%v", code, validation.Valid)
	}

	if workflowNodeAddForTest(t, workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work").NodeID == "" {
		t.Fatal("workflow node add did not return a node id")
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session", "--prompt", "Do work")
	edge := workflowEdgeAddForTest(t, workflowID, "--from", "implement", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "new_session")
	if edge.EdgeID == "" || edge.TransitionGroupID == "" {
		t.Fatalf("workflow edge add did not return edge and group ids: %+v", edge)
	}

	link := workflowLinkForTest(t, binding.ProjectID, workflowID, "--default")
	if link.ID == "" {
		t.Fatalf("workflow link did not return a link id: %+v", link)
	}
	if !link.Default {
		t.Fatalf("workflow link default = false, want true: %+v", link)
	}

	defaultOut, _ := runWorkflowRootCommandOK(t, "workflow", "default", binding.ProjectID, workflowID)
	if !strings.Contains(defaultOut, workflowID) {
		t.Fatalf("default output = %q, want workflow id %s", defaultOut, workflowID)
	}

	unlinkOut, _ := runWorkflowRootCommandOK(t, "workflow", "unlink", binding.ProjectID, workflowID)
	if !strings.Contains(unlinkOut, workflowID) {
		t.Fatalf("unlink output = %q, want workflow id %s", unlinkOut, workflowID)
	}

	runWorkflowRootCommandOK(t, "workflow", "link", binding.ProjectID, workflowID, "--default")
	if validation, code := workflowValidateJSONForTest(t, workflowID); code != 0 || !validation.Valid {
		t.Fatalf("validate code=%d valid=%v, want valid", code, validation.Valid)
	}

	taskOut, _ := runWorkflowRootCommandOK(t, "task", "create", "--title", "Task", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID)
	shortID := taskDetailHeadingShortID(t, taskOut)
	if !strings.Contains(taskOut, shortID+": Task\n") || !strings.Contains(taskOut, "Body:\n```md\nBody\n```\n") || !strings.Contains(taskOut, "Status: open\n") {
		t.Fatalf("task create output = %q, want task show output", taskOut)
	}
	taskResp, err := remote.GetWorkflowTask(context.Background(), serverapi.WorkflowTaskGetRequest{ProjectID: binding.ProjectID, ShortID: shortID})
	if err != nil {
		t.Fatalf("GetWorkflowTask after create: %v", err)
	}
	taskID := taskResp.Task.Summary.ID

	taskListOut, _ := runWorkflowRootCommandOK(t, "task", "list", "--project", binding.ProjectID)
	if !strings.Contains(taskListOut, shortID+": Task.") || !strings.Contains(taskListOut, "Status: open") {
		t.Fatalf("task list output = %q, want short id and open backlog status", taskListOut)
	}
	taskListJSONOut, _ := runWorkflowRootCommandOK(t, "task", "list", "--project", binding.ProjectID, "--json")
	if !strings.Contains(taskListJSONOut, shortID) || !strings.Contains(taskListJSONOut, taskID) {
		t.Fatalf("task list json output = %q, want ids", taskListJSONOut)
	}

	taskShowOut, _ := runWorkflowRootCommandOK(t, "task", "show", "--project", binding.ProjectID, shortID)
	if !strings.Contains(taskShowOut, shortID+": Task\n") || !strings.Contains(taskShowOut, "Body:\n```md\nBody\n```\n") || !strings.Contains(taskShowOut, "Status: open\n") {
		t.Fatalf("task show output = %q, want summary block", taskShowOut)
	}
	taskShowJSONOut, _ := runWorkflowRootCommandOK(t, "task", "show", "--project", binding.ProjectID, "--json", shortID)
	var taskShowJSON taskShowOutput
	if err := json.Unmarshal([]byte(taskShowJSONOut), &taskShowJSON); err != nil {
		t.Fatalf("task show --json output = %q, want JSON: %v", taskShowJSONOut, err)
	}
	if taskShowJSON.Summary.ID != taskID || taskShowJSON.Summary.ShortID != shortID || taskShowJSON.Body != "Body" || taskShowJSON.PlacementCount == 0 {
		t.Fatalf("task show --json output = %+v, want bounded task detail summary", taskShowJSON)
	}
	var taskShowJSONFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(taskShowJSONOut), &taskShowJSONFields); err != nil {
		t.Fatalf("task show --json output = %q, want JSON object: %v", taskShowJSONOut, err)
	}
	for _, omitted := range []string{"attention", "placements", "runs", "transitions", "comments"} {
		if _, ok := taskShowJSONFields[omitted]; ok {
			t.Fatalf("task show --json output = %q, did not expect unbounded %q array", taskShowJSONOut, omitted)
		}
	}
	taskShowOut, _ = runWorkflowRootCommandOK(t, "task", "show", "--project", "missing-project", taskID)
	if !strings.Contains(taskShowOut, shortID+": Task\n") {
		t.Fatalf("task show by full id output = %q, want task short id", taskShowOut)
	}

	commentOut, _ := runWorkflowRootCommandOK(t, "task", "comment", "add", "--project", binding.ProjectID, "--body", "note", shortID)
	commentID := labeledOutputValue(t, commentOut, "comment_id")
	if commentID == "" {
		t.Fatalf("comment output = %q, want comment id", commentOut)
	}
	runWorkflowRootCommandOK(t, "task", "comment", "replace", "--body", "edited", commentID)
	commentListOut, _ := runWorkflowRootCommandOK(t, "task", "comment", "list", "--project", binding.ProjectID, shortID)
	if !strings.Contains(commentListOut, "Comments (1):\nUser at ") || !strings.Contains(commentListOut, "edited") {
		t.Fatalf("comment list output = %q, want rendered comment", commentListOut)
	}
	runWorkflowRootCommandOK(t, "task", "comment", "delete", commentID)

	startResp, err := remote.StartWorkflowTask(context.Background(), serverapi.WorkflowTaskStartRequest{TaskID: taskID})
	if err != nil {
		t.Fatalf("StartWorkflowTask for resume command: %v", err)
	}
	runID := startResp.RunID
	claimed, err := remote.store.ClaimRun(context.Background(), workflow.RunID(runID), 0)
	if err != nil {
		t.Fatalf("ClaimRun for resume command: %v", err)
	}
	resumeSessionID := createWorkflowCommandTestSession(t, cfg, binding, remote.metadataStore)
	if err := remote.store.AttachRunSession(context.Background(), workflow.RunID(runID), claimed.Generation, resumeSessionID); err != nil {
		t.Fatalf("AttachRunSession for resume command: %v", err)
	}
	if err := remote.store.InterruptRunGeneration(context.Background(), workflow.RunID(runID), claimed.Generation, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration for resume command: %v", err)
	}
	resumeOut, _ := runWorkflowRootCommandOK(t, "task", "resume", "--project", binding.ProjectID, shortID)
	if !strings.Contains(resumeOut, "Resumed task "+shortID+" in session "+resumeSessionID+".\n") || !strings.Contains(resumeOut, "Current node: implement\n") {
		t.Fatalf("resume output = %q, want readable resume message", resumeOut)
	}

	cancelOut, _ := runWorkflowRootCommandOK(t, "task", "cancel", "--project", binding.ProjectID, "--reason", "test", shortID)
	if cancelOut != "Canceled task "+shortID+".\n" {
		t.Fatalf("cancel output = %q, want readable cancel message", cancelOut)
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

func TestTaskCreateAcceptsSourceWorkspace(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := setupLinkedWorkflow(t, binding.ProjectID, "Source Workflow")
	createOut, _ := runWorkflowRootCommandOK(t, "task", "create", "--title", "Sourced", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID, "--source-workspace", binding.WorkspaceID)
	shortID := taskDetailHeadingShortID(t, createOut)
	resp, err := remote.GetWorkflowTask(context.Background(), serverapi.WorkflowTaskGetRequest{ProjectID: binding.ProjectID, ShortID: shortID})
	if err != nil {
		t.Fatalf("GetWorkflowTask after create: %v", err)
	}
	if resp.Task.Summary.SourceWorkspaceID != binding.WorkspaceID {
		t.Fatalf("created task source workspace = %q, want %q", resp.Task.Summary.SourceWorkspaceID, binding.WorkspaceID)
	}
}

func TestTaskEditUpdatesFields(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := setupLinkedWorkflow(t, binding.ProjectID, "Edit Workflow")
	createOut, _ := runWorkflowRootCommandOK(t, "task", "create", "--title", "Original", "--body", "Original body", "--workflow", workflowID, "--project", binding.ProjectID)
	shortID := taskDetailHeadingShortID(t, createOut)

	editOut, _ := runWorkflowRootCommandOK(t, "task", "edit", "--project", binding.ProjectID, "--title", "Retitled", shortID)
	if editOut != "Edited task "+shortID+".\n" {
		t.Fatalf("task edit output = %q, want confirmation line", editOut)
	}
	resp, err := remote.GetWorkflowTask(context.Background(), serverapi.WorkflowTaskGetRequest{ProjectID: binding.ProjectID, ShortID: shortID})
	if err != nil {
		t.Fatalf("GetWorkflowTask after title edit: %v", err)
	}
	if resp.Task.Summary.Title != "Retitled" || resp.Task.Body != "Original body" {
		t.Fatalf("after title edit title=%q body=%q, want retitled with unchanged body", resp.Task.Summary.Title, resp.Task.Body)
	}

	runWorkflowRootCommandOK(t, "task", "edit", "--project", binding.ProjectID, "--body", "Edited body", shortID)
	resp, err = remote.GetWorkflowTask(context.Background(), serverapi.WorkflowTaskGetRequest{ProjectID: binding.ProjectID, ShortID: shortID})
	if err != nil {
		t.Fatalf("GetWorkflowTask after body edit: %v", err)
	}
	if resp.Task.Summary.Title != "Retitled" || resp.Task.Body != "Edited body" {
		t.Fatalf("after body edit title=%q body=%q, want unchanged title with edited body", resp.Task.Summary.Title, resp.Task.Body)
	}

	runWorkflowRootCommandOK(t, "task", "edit", "--project", binding.ProjectID, "--source-workspace", binding.WorkspaceID, shortID)
	resp, err = remote.GetWorkflowTask(context.Background(), serverapi.WorkflowTaskGetRequest{ProjectID: binding.ProjectID, ShortID: shortID})
	if err != nil {
		t.Fatalf("GetWorkflowTask after source workspace edit: %v", err)
	}
	if resp.Task.Summary.SourceWorkspaceID != binding.WorkspaceID {
		t.Fatalf("after source workspace edit source=%q, want %q", resp.Task.Summary.SourceWorkspaceID, binding.WorkspaceID)
	}

	jsonOut, _ := runWorkflowRootCommandOK(t, "task", "edit", "--project", binding.ProjectID, "--json", "--title", "JSON title", shortID)
	var updateResp serverapi.WorkflowTaskUpdateResponse
	if err := json.Unmarshal([]byte(jsonOut), &updateResp); err != nil {
		t.Fatalf("task edit --json output = %q, want JSON: %v", jsonOut, err)
	}
	if updateResp.Task.Title != "JSON title" || updateResp.Task.ShortID != shortID {
		t.Fatalf("task edit --json task = %+v, want updated summary", updateResp.Task)
	}
}

func TestTaskEditValidation(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := setupLinkedWorkflow(t, binding.ProjectID, "Edit Validation Workflow")
	createOut, _ := runWorkflowRootCommandOK(t, "task", "create", "--title", "Original", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID)
	shortID := taskDetailHeadingShortID(t, createOut)

	if _, stderr, code := runWorkflowRootCommand("task", "edit", "--project", binding.ProjectID); code != 2 || !strings.Contains(stderr, "requires <short-id-or-task-id>") {
		t.Fatalf("task edit without id code=%d stderr=%q, want positional requirement", code, stderr)
	}
	if _, stderr, code := runWorkflowRootCommand("task", "edit", "--project", binding.ProjectID, shortID); code != 2 || !strings.Contains(stderr, "at least one of") {
		t.Fatalf("task edit without fields code=%d stderr=%q, want field requirement", code, stderr)
	}
	if _, stderr, code := runWorkflowRootCommand("task", "edit", "--project", binding.ProjectID, "--body", "x", "--body-file", "/tmp/x", shortID); code != 2 || !strings.Contains(stderr, "--body cannot be combined with --body-file") {
		t.Fatalf("task edit body conflict code=%d stderr=%q, want mutual exclusion error", code, stderr)
	}
}

func TestWorkflowCommandsRenderReadableOutput(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	// Readable mode must surface the structural identifiers each command produces or operates
	// on (ids, keys, names, modes), without leaking entity UUIDs on update commands. These
	// assertions check the rendered data, not the human-facing wording, which is free to change.
	createOut, _ := runWorkflowRootCommandOK(t, "workflow", "create", "Readable Workflow")
	if !strings.Contains(createOut, "Readable Workflow") || !strings.Contains(createOut, "workflow-") {
		t.Fatalf("workflow create output = %q, want workflow name and generated id", createOut)
	}
	workflowID := workflowCreateForTest(t, "Readable Workflow Two").ID

	nodeOut, _ := runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "implement", "--kind", "agent", "--display-name", "Implement", "--agent", "workflow-test", "--prompt", "Do work", "--completion-mode", "tool")
	if !strings.Contains(nodeOut, "implement") || !strings.Contains(nodeOut, "node-") {
		t.Fatalf("node add output = %q, want node key and generated id", nodeOut)
	}

	// node update mutates an existing entity, so it must surface the key but not the node id.
	nodeUpdateOut, _ := runWorkflowRootCommandOK(t, "workflow", "node", "update", workflowID, "implement", "--display-name", "Implement It")
	if !strings.Contains(nodeUpdateOut, "implement") || strings.Contains(nodeUpdateOut, "node-") {
		t.Fatalf("node update output = %q, want node key without node id", nodeUpdateOut)
	}

	edgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session", "--prompt", "Do work")
	for _, token := range []string{"edge-", "backlog", "implement", "new_session"} {
		if !strings.Contains(edgeOut, token) {
			t.Fatalf("edge add output = %q, want token %q (generated id, route nodes, context mode)", edgeOut, token)
		}
	}

	edgeID := workflowEdgeAddForTest(t, workflowID, "--from", "implement", "--transition", "review", "--edge-key", "review", "--to", "done", "--context", "new_session").EdgeID
	// edge update mutates an existing entity, so it must surface the key and target but not leak the edge id.
	edgeUpdateOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--edge-key", "rereview")
	if !strings.Contains(edgeUpdateOut, "rereview") || !strings.Contains(edgeUpdateOut, "done") || strings.Contains(edgeUpdateOut, edgeID) {
		t.Fatalf("edge update output = %q, want edge key and target without the edge id", edgeUpdateOut)
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "implement", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "new_session")

	// edge add applies the approval gate and selected-node context source; verify the persisted
	// definition (structured data) rather than the readable phrasing.
	runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "implement", "--transition", "gate", "--edge-key", "gated", "--to", "done", "--context", "compact_and_continue_session", "--requires-approval", "--context-source", "node:backlog")
	gatedEdge := workflowEdgeByKeyForTest(t, workflowInspectDefinitionForTest(t, workflowID), "gated")
	if !gatedEdge.RequiresApproval {
		t.Fatalf("gated edge requires-approval not applied: %+v", gatedEdge)
	}
	if gatedEdge.ContextSource.Kind != "selected_node" || gatedEdge.ContextSource.NodeKey != "backlog" {
		t.Fatalf("gated edge context source = %+v, want selected_node backlog", gatedEdge.ContextSource)
	}

	// Readable inspect must surface each node/edge's data values (keys, role, completion mode,
	// rendered ids) without pinning the surrounding punctuation.
	inspectOut, _ := runWorkflowRootCommandOK(t, "workflow", "inspect", workflowID)
	for _, want := range []string{
		"implement",     // node key
		"workflow-test", // agent role
		"tool",          // completion mode
		"backlog",       // transition source node key
		"edge-",         // rendered edge id
	} {
		if !strings.Contains(inspectOut, want) {
			t.Fatalf("inspect output = %q, want token %q", inspectOut, want)
		}
	}

	// validate reports valid only when the graph is complete; --mode draft must be accepted.
	if validation, code := workflowValidateJSONForTest(t, workflowID, "--mode", "draft"); code != 0 || !validation.Valid {
		t.Fatalf("validate --mode draft code=%d valid=%v, want valid", code, validation.Valid)
	}

	// link/default/unlink confirmations must reference the operands (ids) the caller passed.
	linkOut, _ := runWorkflowRootCommandOK(t, "workflow", "link", binding.ProjectID, workflowID, "--default")
	if !strings.Contains(linkOut, workflowID) || !strings.Contains(linkOut, binding.ProjectID) || !strings.Contains(linkOut, "default") {
		t.Fatalf("link output = %q, want workflow, project, and default marker", linkOut)
	}
	defaultOut, _ := runWorkflowRootCommandOK(t, "workflow", "default", binding.ProjectID, workflowID)
	if !strings.Contains(defaultOut, workflowID) || !strings.Contains(defaultOut, binding.ProjectID) {
		t.Fatalf("default output = %q, want workflow and project", defaultOut)
	}
	unlinkOut, _ := runWorkflowRootCommandOK(t, "workflow", "unlink", binding.ProjectID, workflowID)
	if !strings.Contains(unlinkOut, workflowID) || !strings.Contains(unlinkOut, binding.ProjectID) {
		t.Fatalf("unlink output = %q, want workflow and project", unlinkOut)
	}
}

func TestWorkflowCommandsRenderJSONOutput(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "JSON Workflow").ID
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work")

	listOut, _ := runWorkflowRootCommandOK(t, "workflow", "list", "--json")
	var list workflowListOutput
	if err := json.Unmarshal([]byte(listOut), &list); err != nil {
		t.Fatalf("workflow list --json = %q, want JSON: %v", listOut, err)
	}
	foundWorkflow := false
	for _, record := range list.Workflows {
		if record.ID == workflowID {
			foundWorkflow = true
		}
	}
	if !foundWorkflow {
		t.Fatalf("workflow list --json = %+v, want created workflow", list.Workflows)
	}

	inspectOut, _ := runWorkflowRootCommandOK(t, "workflow", "inspect", "--json", workflowID)
	var def serverapi.WorkflowDefinition
	if err := json.Unmarshal([]byte(inspectOut), &def); err != nil {
		t.Fatalf("workflow inspect --json = %q, want JSON: %v", inspectOut, err)
	}
	if def.Workflow.ID != workflowID || len(def.Nodes) == 0 {
		t.Fatalf("workflow inspect --json = %+v, want definition with nodes", def)
	}

	validateOut, _, code := runWorkflowRootCommand("workflow", "validate", "--json", workflowID)
	if code == 0 {
		t.Fatalf("workflow validate --json code=%d, want non-zero for invalid workflow", code)
	}
	var validation serverapi.WorkflowValidateResponse
	if err := json.Unmarshal([]byte(validateOut), &validation); err != nil {
		t.Fatalf("workflow validate --json = %q, want JSON: %v", validateOut, err)
	}
	if validation.Valid || len(validation.Errors) == 0 {
		t.Fatalf("workflow validate --json = %+v, want invalid with errors", validation)
	}
}

func TestWorkflowCreateAcceptsTrailingJSONFlag(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	// Flags may surround the name positional in any order: a trailing --json after the name, and
	// a leading --description before the name, must both be parsed as flags rather than folded
	// into the workflow name.
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "name then json", args: []string{"workflow", "create", "Trailing Flag Flow", "--json"}, want: "Trailing Flag Flow"},
		{name: "leading description then name then json", args: []string{"workflow", "create", "--description", "scripted", "Leading Desc Flow", "--json"}, want: "Leading Desc Flow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, code := runWorkflowRootCommand(tc.args...)
			if code != 0 {
				t.Fatalf("workflow create exit=%d out=%q", code, out)
			}
			var record serverapi.WorkflowRecord
			if err := json.Unmarshal([]byte(out), &record); err != nil {
				t.Fatalf("workflow create %v = %q, want JSON: %v", tc.args, out, err)
			}
			if record.Name != tc.want {
				t.Fatalf("workflow name = %q, want %q", record.Name, tc.want)
			}
		})
	}
}

func TestWorkflowSubcommandsAcceptLeadingJSONFlag(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Leading JSON Flow").ID

	// --json placed before the workflow ref, with the node flags after it, must still parse.
	nodeOut, _, code := runWorkflowRootCommand("workflow", "node", "add", "--json", workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work")
	if code != 0 {
		t.Fatalf("node add --json (leading) exit=%d out=%q", code, nodeOut)
	}
	var node workflowNodeOutput
	if err := json.Unmarshal([]byte(nodeOut), &node); err != nil {
		t.Fatalf("node add --json (leading) = %q, want JSON: %v", nodeOut, err)
	}
	if node.Key != "implement" {
		t.Fatalf("node key = %q, want implement", node.Key)
	}

	// A read command with --json before its positional must behave the same way.
	inspectOut, _, code := runWorkflowRootCommand("workflow", "inspect", "--json", workflowID)
	if code != 0 {
		t.Fatalf("inspect --json (leading) exit=%d out=%q", code, inspectOut)
	}
	var def serverapi.WorkflowDefinition
	if err := json.Unmarshal([]byte(inspectOut), &def); err != nil {
		t.Fatalf("inspect --json (leading) = %q, want JSON: %v", inspectOut, err)
	}
	if def.Workflow.ID != workflowID {
		t.Fatalf("inspect workflow id = %q, want %q", def.Workflow.ID, workflowID)
	}
}

func TestWorkflowEdgeUpdateTogglesRequiresApproval(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Approval Toggle").ID
	workflowNodeAddForTest(t, workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work")
	edgeID := workflowEdgeAddForTest(t, workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session", "--prompt", "Go").EdgeID

	// Read the edge's persisted approval flag so the test asserts the applied effect, not prose.
	edgeRequiresApproval := func() bool {
		return workflowEdgeByKeyForTest(t, workflowInspectDefinitionForTest(t, workflowID), "start").RequiresApproval
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--requires-approval")
	if !edgeRequiresApproval() {
		t.Fatal("edge update --requires-approval did not enable the approval gate")
	}

	// --requires-approval=false must clear the gate under partial-update semantics.
	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--requires-approval=false")
	if edgeRequiresApproval() {
		t.Fatal("edge update --requires-approval=false did not clear the approval gate")
	}
}

func TestWorkflowEditCommandsUpdateNodeAndEdgeMetadata(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Editable Workflow").ID
	if workflowID == "" {
		t.Fatal("workflow create did not return a workflow id")
	}
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	startEdgeID := workflowEdgeAddForTest(t, workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.").EdgeID
	if startEdgeID == "" {
		t.Fatal("start edge add did not return an edge id")
	}
	edgeID := workflowEdgeAddForTest(t, workflowID, "--from", "triaging", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "continue_session", "--context-source", "node:triaging").EdgeID
	if edgeID == "" {
		t.Fatal("edge add did not return an edge id")
	}

	updateNodeOut, _ := runWorkflowRootCommandOK(t, "workflow", "node", "update", workflowID, "triaging", "--prompt", "Decide whether the ticket is actionable.")
	if !strings.Contains(updateNodeOut, "triaging") {
		t.Fatalf("node update output = %q, want node key", updateNodeOut)
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, startEdgeID, "--transition", "start_review")
	if validation, code := workflowValidateJSONForTest(t, workflowID); code != 0 || !validation.Valid {
		t.Fatalf("validate code=%d valid=%v, want start branch prompt preserved", code, validation.Valid)
	}

	updateEdgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--transition", "not_actionable", "--edge-key", "not_actionable")
	if !strings.Contains(updateEdgeOut, "not_actionable") {
		t.Fatalf("edge update output = %q, want edge key", updateEdgeOut)
	}
	if strings.Contains(updateEdgeOut, edgeID) {
		t.Fatalf("edge update output = %q, did not expect edge id", updateEdgeOut)
	}

	// Verify the retargeted edge from the persisted definition (transition id, context source,
	// target) rather than the readable inspect prose.
	def := workflowInspectDefinitionForTest(t, workflowID)
	updatedEdge := workflowEdgeByKeyForTest(t, def, "not_actionable")
	if updatedEdge.ContextSource.Kind != "selected_node" || updatedEdge.ContextSource.NodeKey != "triaging" {
		t.Fatalf("edge context source = %+v, want selected_node triaging", updatedEdge.ContextSource)
	}
	if got := workflowNodeKeyForID(def, updatedEdge.TargetNodeID); got != "done" {
		t.Fatalf("edge target = %q, want done", got)
	}
	if group := workflowTransitionGroupForID(def, updatedEdge.TransitionGroupID); group.TransitionID != "not_actionable" {
		t.Fatalf("edge transition id = %q, want not_actionable", group.TransitionID)
	}
}

func TestWorkflowEdgeUpdatePreservesPromptAndParameters(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Parameter Preservation Workflow").ID
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeID := workflowEdgeAddForTest(t, workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.").EdgeID
	if edgeID == "" {
		t.Fatal("edge add did not return an edge id")
	}

	ctx := context.Background()
	edge := workflowCommandStoredEdgeByID(t, ctx, remote.store, workflowID, edgeID)
	edge.Parameters = []workflow.Parameter{{Key: "summary", Description: "Summary."}}
	if _, err := remote.store.UpdateEdge(ctx, workflowCommandEdgeRecord(edge)); err != nil {
		t.Fatalf("UpdateEdge seed parameters: %v", err)
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--transition-display-name", "Start Review")

	updated := workflowCommandStoredEdgeByID(t, ctx, remote.store, workflowID, edgeID)
	if updated.PromptTemplate != "Triage." {
		t.Fatalf("edge prompt = %q, want preserved prompt", updated.PromptTemplate)
	}
	if len(updated.Parameters) != 1 || updated.Parameters[0].Key != "summary" || updated.Parameters[0].Description != "Summary." {
		t.Fatalf("edge parameters = %+v, want preserved summary parameter", updated.Parameters)
	}
}

func TestWorkflowNodeUpdatePreservesCanonicalWiringFields(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &preservingNodeUpdateRemote{}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	_, stderr, code := runWorkflowRootCommand("workflow", "node", "update", "workflow-1", "join", "--display-name", "Updated Join")
	if code != 0 {
		t.Fatalf("workflow node update exit=%d stderr=%q", code, stderr)
	}
	if remote.updateReq.DisplayName != "Updated Join" {
		t.Fatalf("update request display name = %q, want Updated Join", remote.updateReq.DisplayName)
	}
	if len(remote.updateReq.InputFields) != 1 || remote.updateReq.InputFields[0].Name != "handoff" || remote.updateReq.InputFields[0].Description != "Branch handoff." {
		t.Fatalf("update request input fields = %+v, want existing fields preserved", remote.updateReq.InputFields)
	}
	if len(remote.updateReq.JoinInputProviders) != 1 || remote.updateReq.JoinInputProviders[0].InputName != "handoff" || remote.updateReq.JoinInputProviders[0].ProviderEdgeID != "edge-branch-join" {
		t.Fatalf("update request join providers = %+v, want existing providers preserved", remote.updateReq.JoinInputProviders)
	}
}

type preservingNodeUpdateRemote struct {
	client.WorkflowClient
	updateReq serverapi.WorkflowNodeUpdateRequest
}

func (r *preservingNodeUpdateRemote) Close() error { return nil }

func (r *preservingNodeUpdateRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *preservingNodeUpdateRemote) ListWorkflows(context.Context, serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error) {
	return serverapi.WorkflowListResponse{Workflows: []serverapi.WorkflowRecord{{ID: "workflow-1", Name: "Workflow"}}}, nil
}

func (r *preservingNodeUpdateRemote) GetWorkflow(context.Context, serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error) {
	return serverapi.WorkflowGetResponse{Definition: serverapi.WorkflowDefinition{
		Workflow: serverapi.WorkflowRecord{ID: "workflow-1", Name: "Workflow"},
		Nodes: []serverapi.WorkflowNode{{
			ID:          "node-join",
			WorkflowID:  "workflow-1",
			Key:         "join",
			Kind:        "join",
			DisplayName: "Join",
			InputFields: []serverapi.WorkflowInputField{{
				Name:        "handoff",
				Description: "Branch handoff.",
			}},
			JoinInputProviders: []serverapi.WorkflowJoinInputProvider{{
				InputName:      "handoff",
				ProviderEdgeID: "edge-branch-join",
			}},
		}},
	}}, nil
}

func (r *preservingNodeUpdateRemote) UpdateWorkflowNode(_ context.Context, req serverapi.WorkflowNodeUpdateRequest) (serverapi.WorkflowNodeUpdateResponse, error) {
	r.updateReq = req
	return serverapi.WorkflowNodeUpdateResponse{Version: 2}, nil
}

func TestWorkflowNodeAddSetsCompletionMode(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &completionModeCaptureRemote{}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	_, stderr, code := runWorkflowRootCommand("workflow", "node", "add", "workflow-1", "--key", "implement", "--kind", "agent", "--completion-mode", "tool")
	if code != 0 {
		t.Fatalf("workflow node add exit=%d stderr=%q", code, stderr)
	}
	if remote.addReq.CompletionMode != "tool" {
		t.Fatalf("add request completion mode = %q, want tool", remote.addReq.CompletionMode)
	}
}

func TestWorkflowNodeUpdateSetsCompletionMode(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &completionModeCaptureRemote{}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	_, stderr, code := runWorkflowRootCommand("workflow", "node", "update", "workflow-1", "implement", "--completion-mode", "shell_command")
	if code != 0 {
		t.Fatalf("workflow node update exit=%d stderr=%q", code, stderr)
	}
	if remote.updateReq.CompletionMode != "shell_command" {
		t.Fatalf("update request completion mode = %q, want shell_command", remote.updateReq.CompletionMode)
	}
}

func TestWorkflowNodeUpdatePreservesCompletionMode(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &completionModeCaptureRemote{}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	_, stderr, code := runWorkflowRootCommand("workflow", "node", "update", "workflow-1", "implement", "--display-name", "Implement It")
	if code != 0 {
		t.Fatalf("workflow node update exit=%d stderr=%q", code, stderr)
	}
	if remote.updateReq.CompletionMode != "structured_output" {
		t.Fatalf("update request completion mode = %q, want preserved structured_output", remote.updateReq.CompletionMode)
	}
}

type completionModeCaptureRemote struct {
	client.WorkflowClient
	addReq    serverapi.WorkflowNodeAddRequest
	updateReq serverapi.WorkflowNodeUpdateRequest
}

func (r *completionModeCaptureRemote) Close() error { return nil }

func (r *completionModeCaptureRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *completionModeCaptureRemote) GetWorkflow(context.Context, serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error) {
	return serverapi.WorkflowGetResponse{Definition: serverapi.WorkflowDefinition{
		Workflow: serverapi.WorkflowRecord{ID: "workflow-1", Name: "Workflow"},
		Nodes: []serverapi.WorkflowNode{{
			ID:             "node-implement",
			WorkflowID:     "workflow-1",
			Key:            "implement",
			Kind:           "agent",
			DisplayName:    "Implement",
			SubagentRole:   "workflow-test",
			PromptTemplate: "Implement.",
			CompletionMode: "structured_output",
		}},
	}}, nil
}

func (r *completionModeCaptureRemote) AddWorkflowNode(_ context.Context, req serverapi.WorkflowNodeAddRequest) (serverapi.WorkflowNodeAddResponse, error) {
	r.addReq = req
	return serverapi.WorkflowNodeAddResponse{Version: 2}, nil
}

func (r *completionModeCaptureRemote) UpdateWorkflowNode(_ context.Context, req serverapi.WorkflowNodeUpdateRequest) (serverapi.WorkflowNodeUpdateResponse, error) {
	r.updateReq = req
	return serverapi.WorkflowNodeUpdateResponse{Version: 3}, nil
}

func TestWorkflowEditCommandsRejectLegacyWiringFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "node add output",
			args: []string{"workflow", "node", "add", "workflow-1", "--key", "agent", "--kind", "agent", "--output", "summary=Summary"},
		},
		{
			name: "node update output",
			args: []string{"workflow", "node", "update", "workflow-1", "agent", "--output", "summary=Summary"},
		},
		{
			name: "edge add input",
			args: []string{"workflow", "edge", "add", "workflow-1", "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "agent", "--context", "new_session", "--input", "summary=transition_output:summary"},
		},
		{
			name: "edge update output requirement",
			args: []string{"workflow", "edge", "update", "workflow-1", "edge-1", "--require-output", "summary"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := runWorkflowRootCommand(tt.args...)
			if code != 2 || !strings.Contains(stderr, "flag provided but not defined") {
				t.Fatalf("exit=%d stderr=%q, want undefined flag parse failure", code, stderr)
			}
		})
	}
}

func TestWorkflowEdgeAddRejectsMalformedContextSourceSelector(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Context Source Workflow").ID
	if _, nodeErr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "fast", "--prompt", "Triage."); code != 0 {
		t.Fatalf("workflow node add exit=%d stderr=%q", code, nodeErr)
	}
	_, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "continue_session", "--context-source", "triaging")
	if code == 0 || !strings.Contains(edgeErr, "context source selector") {
		t.Fatalf("workflow edge add exit=%d stderr=%q, want selector error", code, edgeErr)
	}
}

func TestWorkflowEdgeUpdateRollsBackTransitionGroupWhenEdgeUpdateFails(t *testing.T) {
	cfg, _, loopback := newWorkflowCommandLoopback(t)
	remote := &failingWorkflowEdgeUpdateRemote{workflowCommandLoopbackRemote: loopback}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Rollback Workflow").ID
	if _, nodeErr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage."); code != 0 {
		t.Fatalf("workflow node add exit=%d stderr=%q", code, nodeErr)
	}
	edgeID := workflowEdgeAddForTest(t, workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.").EdgeID
	remote.failUpdateEdge = true

	_, updateErr, code := runWorkflowRootCommand("workflow", "edge", "update", workflowID, edgeID, "--transition", "changed")
	if code == 0 || !strings.Contains(updateErr, "edge update failed") {
		t.Fatalf("workflow edge update code=%d stderr=%q, want edge update failure", code, updateErr)
	}
	def, _, err := loopback.store.GetDefinition(context.Background(), workflow.WorkflowID(workflowID))
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	var group workflow.TransitionGroup
	for _, edge := range def.Edges {
		if string(edge.ID) != edgeID {
			continue
		}
		for _, candidate := range def.TransitionGroups {
			if candidate.ID == edge.TransitionGroupID {
				group = candidate
				break
			}
		}
	}
	if group.TransitionID != "start" {
		t.Fatalf("transition group after failed update = %+v, want original start", group)
	}
}

func TestParseWorkflowParameters(t *testing.T) {
	parsed, err := parseWorkflowParameters([]string{"plan_file_path=Path to the plan doc", "  changes = What to fix  "})
	if err != nil {
		t.Fatalf("parseWorkflowParameters: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("parsed = %+v, want 2 parameters", parsed)
	}
	if parsed[0].Key != "plan_file_path" || parsed[0].Description != "Path to the plan doc" {
		t.Fatalf("parsed[0] = %+v", parsed[0])
	}
	if parsed[1].Key != "changes" || parsed[1].Description != "What to fix" {
		t.Fatalf("parsed[1] = %+v", parsed[1])
	}
	for _, bad := range []string{"keyonly", "=description", "key=", "  =  ", "bad key=desc", "Bad=desc", "1bad=desc", "bad-key=desc", "commentary=desc", "transition=desc"} {
		if _, err := parseWorkflowParameters([]string{bad}); err == nil {
			t.Fatalf("parseWorkflowParameters(%q) = nil error, want failure", bad)
		}
	}
	if _, err := parseWorkflowParameters([]string{"summary=first", "summary=second"}); err == nil {
		t.Fatalf("parseWorkflowParameters with duplicate keys = nil error, want failure")
	}
}

func TestWorkflowEdgeAddSetsParametersAndTransitionDescription(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Parameter Authoring Workflow").ID
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeID := workflowEdgeAddForTest(t, workflowID,
		"--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session",
		"--prompt", "Use {{.Params.plan_file_path}}.",
		"--transition-description", "Pick when starting the work.",
		"--param", "plan_file_path=Path to the plan doc").EdgeID

	ctx := context.Background()
	def, _, err := remote.store.GetDefinition(ctx, workflow.WorkflowID(workflowID))
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	var edge workflow.Edge
	for _, candidate := range def.Edges {
		if string(candidate.ID) == edgeID {
			edge = candidate
		}
	}
	if len(edge.Parameters) != 1 || edge.Parameters[0].Key != "plan_file_path" || edge.Parameters[0].Description != "Path to the plan doc" {
		t.Fatalf("edge parameters = %+v, want plan_file_path parameter", edge.Parameters)
	}
	var group workflow.TransitionGroup
	for _, candidate := range def.TransitionGroups {
		if candidate.ID == edge.TransitionGroupID {
			group = candidate
		}
	}
	if group.Description != "Pick when starting the work." {
		t.Fatalf("transition group description = %q, want authored description", group.Description)
	}
}

func TestWorkflowEdgeUpdateSetsParametersAndTransitionDescription(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Parameter Update Workflow").ID
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeID := workflowEdgeAddForTest(t, workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.").EdgeID

	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID,
		"--transition-description", "Pick when design is unnecessary.",
		"--param", "plan_file_path=Path to the plan doc",
		"--param", "changes=Requested changes")

	ctx := context.Background()
	def, _, err := remote.store.GetDefinition(ctx, workflow.WorkflowID(workflowID))
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	var edge workflow.Edge
	for _, candidate := range def.Edges {
		if string(candidate.ID) == edgeID {
			edge = candidate
		}
	}
	if len(edge.Parameters) != 2 || edge.Parameters[0].Key != "plan_file_path" || edge.Parameters[1].Key != "changes" {
		t.Fatalf("edge parameters = %+v, want plan_file_path and changes", edge.Parameters)
	}
	var group workflow.TransitionGroup
	for _, candidate := range def.TransitionGroups {
		if candidate.ID == edge.TransitionGroupID {
			group = candidate
		}
	}
	if group.Description != "Pick when design is unnecessary." {
		t.Fatalf("transition group description = %q, want authored description", group.Description)
	}
}

func TestWorkflowEdgeUpdateClearsParameters(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Parameter Clear Workflow").ID
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeID := workflowEdgeAddForTest(t, workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.", "--param", "plan_file_path=Path to the plan doc").EdgeID

	if _, _, code := runWorkflowRootCommand("workflow", "edge", "update", workflowID, edgeID, "--param", "x=y", "--clear-params"); code != 2 {
		t.Fatalf("combined --param/--clear-params exit=%d, want rejection exit 2", code)
	}

	ctx := context.Background()
	rejectedEdge := workflowCommandStoredEdgeByID(t, ctx, remote.store, workflowID, edgeID)
	if len(rejectedEdge.Parameters) != 1 {
		t.Fatalf("edge parameters after rejected update = %+v, want unchanged", rejectedEdge.Parameters)
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--clear-params")

	edge := workflowCommandStoredEdgeByID(t, ctx, remote.store, workflowID, edgeID)
	if len(edge.Parameters) != 0 {
		t.Fatalf("edge parameters = %+v, want cleared", edge.Parameters)
	}
	if edge.PromptTemplate != "Triage." {
		t.Fatalf("edge prompt = %q, want preserved", edge.PromptTemplate)
	}
}

func TestTaskHumanOnlyActionsAreDeniedInsideKentSession(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-agent")
	previous := workflowCommandRemoteOpener
	workflowCommandRemoteOpener = func(context.Context, string) (config.App, workflowCommandRemote, error) {
		t.Fatal("human-only task command opened workflow remote")
		return config.App{}, nil, nil
	}
	defer func() {
		workflowCommandRemoteOpener = previous
	}()

	for _, args := range [][]string{
		{"task", "start", "TASK-1"},
		{"task", "cancel", "TASK-1"},
		{"task", "resume", "TASK-1"},
		{"task", "approve", "transition-1"},
		{"task", "move", "TASK-1", "node-1"},
		{"task", "comment", "delete", "comment-1"},
	} {
		stdout, stderr, code := runWorkflowRootCommand(args...)
		if code != 1 {
			t.Fatalf("%v exit = %d stderr=%q", args, code, stderr)
		}
		if stdout != "" {
			t.Fatalf("%v stdout = %q, want empty", args, stdout)
		}
		if stderr != prompts.WorkflowHumanOnlyTaskActionDeniedPrompt+"\n" {
			t.Fatalf("%v stderr = %q, want denied prompt", args, stderr)
		}
	}
}

func TestTaskSafeActionsRemainAvailableInsideKentSession(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-agent")
	_, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, remote.cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Safe Task Workflow").ID
	if workflowID == "" {
		t.Fatal("workflow create did not return a workflow id")
	}
	if _, nodeErr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work"); code != 0 {
		t.Fatalf("workflow node add exit=%d stderr=%q", code, nodeErr)
	}
	if _, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session", "--prompt", "Do work"); code != 0 {
		t.Fatalf("workflow start edge add exit=%d stderr=%q", code, edgeErr)
	}
	if _, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "implement", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "new_session"); code != 0 {
		t.Fatalf("workflow done edge add exit=%d stderr=%q", code, edgeErr)
	}
	if _, linkErr, code := runWorkflowRootCommand("workflow", "link", binding.ProjectID, workflowID, "--default"); code != 0 {
		t.Fatalf("workflow link exit=%d stderr=%q", code, linkErr)
	}

	taskOut, taskErr, code := runWorkflowRootCommand("task", "create", "--title", "Safe Task", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID, "--source-url", "https://github.com/respawn-llc/kent/issues/123")
	if code != 0 {
		t.Fatalf("task create exit=%d stderr=%q", code, taskErr)
	}
	if !strings.Contains(taskOut, "Imported from: https://github.com/respawn-llc/kent/issues/123\n") {
		t.Fatalf("task create output = %q, want source URL", taskOut)
	}
	shortID := taskDetailHeadingShortID(t, taskOut)
	if _, listErr, code := runWorkflowRootCommand("task", "list", "--project", binding.ProjectID); code != 0 {
		t.Fatalf("task list exit=%d stderr=%q", code, listErr)
	}
	if _, showErr, code := runWorkflowRootCommand("task", "show", "--project", binding.ProjectID, shortID); code != 0 {
		t.Fatalf("task show exit=%d stderr=%q", code, showErr)
	}
	commentOut, commentErr, code := runWorkflowRootCommand("task", "comment", "add", "--project", binding.ProjectID, "--author", "user", "--author-id", "octocat", "--body", "note", shortID)
	if code != 0 {
		t.Fatalf("task comment add exit=%d stderr=%q", code, commentErr)
	}
	commentID := labeledOutputValue(t, commentOut, "comment_id")
	if commentID == "" {
		t.Fatalf("task comment add output = %q", commentOut)
	}
	commentListOut, commentListErr, code := runWorkflowRootCommand("task", "comment", "list", "--project", binding.ProjectID, shortID)
	if code != 0 {
		t.Fatalf("task comment list exit=%d stderr=%q", code, commentListErr)
	}
	if !strings.Contains(commentListOut, "octocat at ") {
		t.Fatalf("task comment list output = %q, want author id", commentListOut)
	}
	if _, replaceErr, code := runWorkflowRootCommand("task", "comment", "replace", "--body", "edited", commentID); code != 0 {
		t.Fatalf("task comment replace exit=%d stderr=%q", code, replaceErr)
	}
}

type pagedTaskListRemote struct {
	client.WorkflowClient
	board    serverapi.WorkflowBoard
	pages    map[string]serverapi.WorkflowBoard
	requests []serverapi.WorkflowBoardRequest
}

func (r *pagedTaskListRemote) Close() error { return nil }

func (r *pagedTaskListRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *pagedTaskListRemote) GetWorkflowBoard(_ context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
	r.requests = append(r.requests, req)
	if strings.TrimSpace(req.PageToken) == "" {
		return serverapi.WorkflowBoardResponse{Board: r.board}, nil
	}
	return serverapi.WorkflowBoardResponse{Board: r.pages[req.PageToken]}, nil
}

func TestTaskListUsesDefaultPageSizeAndPrintsNextPageToken(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &pagedTaskListRemote{
		board: serverapi.WorkflowBoard{
			ProjectID:        "project-1",
			SelectedWorkflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1"},
			Cards:            []serverapi.WorkflowBoardTaskCard{testTaskCard("task-a", "BLD-1", "A")},
			NextPageToken:    "next",
		},
		pages: map[string]serverapi.WorkflowBoard{
			"next": {
				ProjectID:        "project-1",
				SelectedWorkflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1"},
				Cards: []serverapi.WorkflowBoardTaskCard{
					testTaskCard("task-b", "BLD-2", "B"),
					testTaskCard("task-a", "BLD-1", "A"),
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
	if strings.Count(stdout, "BLD-1:") != 1 || strings.Contains(stdout, "BLD-2:") {
		t.Fatalf("task list output = %q, want only first page cards", stdout)
	}
	if strings.Contains(stdout, "short_id\t") {
		t.Fatalf("task list output = %q, want human-readable blocks without TSV header", stdout)
	}
	if !strings.Contains(stdout, "BLD-1: A.\nStatus: open\n") {
		t.Fatalf("task list output = %q, want readable open status block", stdout)
	}
	if !strings.Contains(stderr, "Next page token: `next`") {
		t.Fatalf("task list stderr = %q, want next page token", stderr)
	}
	if len(remote.requests) != 1 || remote.requests[0].PageSize != 100 || remote.requests[0].PageToken != "" {
		t.Fatalf("board requests = %+v, want one default-sized first page request", remote.requests)
	}
}

func TestTaskListUsesRequestedPageSizeAndToken(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &pagedTaskListRemote{
		board: serverapi.WorkflowBoard{
			ProjectID:        "project-1",
			SelectedWorkflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1"},
			Cards:            []serverapi.WorkflowBoardTaskCard{testTaskCard("task-a", "BLD-1", "A")},
			NextPageToken:    "next",
		},
		pages: map[string]serverapi.WorkflowBoard{
			"next": {
				ProjectID:        "project-1",
				SelectedWorkflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1"},
				Cards:            []serverapi.WorkflowBoardTaskCard{testTaskCard("task-b", "BLD-2", "B")},
				DonePreview:      []serverapi.WorkflowBoardTaskCard{testDoneTaskCard("task-c", "BLD-3", "C")},
			},
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "list", "--project", "project-1", "--page-size", "1", "--page-token", "next")
	if code != 0 {
		t.Fatalf("task list exit=%d stderr=%q", code, stderr)
	}
	// The requested open page holds BLD-2; BLD-1 is on the earlier page. Because
	// the open stream is exhausted (no next token), the bounded done preview is
	// surfaced so done tasks stay reachable even though open cards filled the page.
	if strings.Contains(stdout, "BLD-1:") || strings.Count(stdout, "BLD-2:") != 1 || strings.Count(stdout, "BLD-3:") != 1 {
		t.Fatalf("task list output = %q, want the open page plus the surfaced done preview", stdout)
	}
	if !strings.Contains(stdout, "BLD-2: B.\nStatus: open\n") {
		t.Fatalf("task list output = %q, want readable open status block", stdout)
	}
	if !strings.Contains(stdout, "BLD-3: C.\nStatus: done\n") {
		t.Fatalf("task list output = %q, want surfaced done task", stdout)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("task list stderr = %q, want no next page token", stderr)
	}
	if len(remote.requests) != 1 || remote.requests[0].PageSize != 1 || remote.requests[0].PageToken != "next" {
		t.Fatalf("board requests = %+v, want requested page size and token", remote.requests)
	}
}

func TestTaskListJSONOutputsStructuredPage(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &pagedTaskListRemote{
		board: serverapi.WorkflowBoard{
			ProjectID:        "project-1",
			SelectedWorkflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1"},
			Cards: []serverapi.WorkflowBoardTaskCard{{
				TaskID:        "task-a",
				ShortID:       "BLD-1",
				WorkflowID:    "workflow-1",
				Title:         "A",
				ActiveNodeIDs: []string{"node-1"},
				Status:        serverapi.WorkflowTaskStatus{Kind: "running", RunIDs: []string{"run-1"}},
			}},
			NextPageToken: "next",
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "list", "--project", "project-1", "--json")
	if code != 0 {
		t.Fatalf("task list --json exit=%d stderr=%q", code, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("task list --json stderr = %q, want empty stderr on success", stderr)
	}
	var output taskListOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("task list --json output = %q, want JSON: %v", stdout, err)
	}
	if output.ProjectID != "project-1" || output.NextPageToken != "next" {
		t.Fatalf("task list --json output = %+v, want project and next page token", output)
	}
	if len(output.Tasks) != 1 || output.Tasks[0].TaskID != "task-a" || output.Tasks[0].Status != "running" {
		t.Fatalf("task list --json tasks = %+v, want running task-a", output.Tasks)
	}
}

func TestTaskListStatusMapping(t *testing.T) {
	tests := []struct {
		name string
		task serverapi.WorkflowTaskSummary
		want string
	}{
		{name: "open", task: serverapi.WorkflowTaskSummary{}, want: "open"},
		{name: "running", task: serverapi.WorkflowTaskSummary{ActiveNodeIDs: []string{"node-1"}}, want: "running"},
		{name: "done", task: serverapi.WorkflowTaskSummary{Done: true, ActiveNodeIDs: []string{"node-1"}}, want: "done"},
		{name: "canceled", task: serverapi.WorkflowTaskSummary{CanceledAt: 1, Done: true, ActiveNodeIDs: []string{"node-1"}}, want: "canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskListStatus(tt.task); got != tt.want {
				t.Fatalf("taskListStatus(%+v) = %q, want %q", tt.task, got, tt.want)
			}
		})
	}
}

func TestTaskListHelpIncludesPaginationAndJSONFlags(t *testing.T) {
	_, stderr, code := runWorkflowRootCommand("task", "list", "--help")
	if code != 0 {
		t.Fatalf("task list --help exit=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"kent task list [--project <project>] [--page-size <n>] [--page-token <token>] [--json]", "-json", "-page-size", "-page-token"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("task list --help stderr = %q, want %q", stderr, want)
		}
	}
}

func TestTaskShowHelpIncludesJSONFlag(t *testing.T) {
	_, stderr, code := runWorkflowRootCommand("task", "show", "--help")
	if code != 0 {
		t.Fatalf("task show --help exit=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"kent task show <short-id-or-task-id> [--json]", "-json"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("task show --help stderr = %q, want %q", stderr, want)
		}
	}
}

func TestReadTaskBodyFlagRequiresInlineOrFileBody(t *testing.T) {
	if _, err := readTaskBodyFlag(" \t\n", ""); err == nil {
		t.Fatal("expected missing body flags to fail")
	}
}

func TestTaskCommentAuthorForAddUsesUserWithoutKentSession(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	remote := &commentAuthorRemote{}
	got := taskCommentAuthorForAdd(context.Background(), remote, "task-1", "", false)
	if got.Kind != "user" || got.ID != "" {
		t.Fatalf("taskCommentAuthorForAdd = %+v, want user without author id", got)
	}
}

func TestTaskCommentAuthorForAddUsesWorkflowRunRole(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-workflow")
	remote := &commentAuthorRemote{task: serverapi.WorkflowTaskDetail{
		Runs: []serverapi.WorkflowRun{{SessionID: "session-workflow", Role: "code_review", NodeID: "node-review"}},
	}}
	got := taskCommentAuthorForAdd(context.Background(), remote, "task-1", "", false)
	if got.Kind != "agent" || got.ID != "code_review" {
		t.Fatalf("taskCommentAuthorForAdd = %+v, want workflow role agent", got)
	}
}

func TestTaskCommentAuthorForAddUsesWorkflowNodeWhenRoleMissing(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-workflow")
	remote := &commentAuthorRemote{task: serverapi.WorkflowTaskDetail{
		Placements: []serverapi.WorkflowPlacement{{NodeID: "node-implement", NodeKey: "implement"}},
		Runs:       []serverapi.WorkflowRun{{SessionID: "session-workflow", NodeID: "node-implement"}},
	}}
	got := taskCommentAuthorForAdd(context.Background(), remote, "task-1", "", false)
	if got.Kind != "agent" || got.ID != "Node implement agent" {
		t.Fatalf("taskCommentAuthorForAdd = %+v, want workflow node agent", got)
	}
}

func TestTaskCommentAuthorForAddUsesDeterministicCurrentWorkflowRun(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-workflow")
	remote := &commentAuthorRemote{task: serverapi.WorkflowTaskDetail{
		Status: serverapi.WorkflowTaskStatus{RunIDs: []string{"run-current"}},
		Placements: []serverapi.WorkflowPlacement{
			{NodeID: "node-old", NodeKey: "old"},
			{NodeID: "node-current", NodeKey: "current"},
		},
		Runs: []serverapi.WorkflowRun{
			{ID: "run-old", SessionID: "session-workflow", NodeID: "node-old", StartedAtUnixMs: 20},
			{ID: "run-current", SessionID: "session-workflow", NodeID: "node-current", StartedAtUnixMs: 10},
		},
	}}
	got := taskCommentAuthorForAdd(context.Background(), remote, "task-1", "", false)
	if got.Kind != "agent" || got.ID != "Node current agent" {
		t.Fatalf("taskCommentAuthorForAdd = %+v, want current workflow run agent", got)
	}
}

func TestTaskCommentAuthorForAddUsesLatestWorkflowRunWhenNoneCurrent(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-workflow")
	remote := &commentAuthorRemote{task: serverapi.WorkflowTaskDetail{
		Placements: []serverapi.WorkflowPlacement{
			{NodeID: "node-old", NodeKey: "old"},
			{NodeID: "node-new", NodeKey: "new"},
		},
		Runs: []serverapi.WorkflowRun{
			{ID: "run-old", SessionID: "session-workflow", NodeID: "node-old", StartedAtUnixMs: 10},
			{ID: "run-new", SessionID: "session-workflow", NodeID: "node-new", StartedAtUnixMs: 20},
		},
	}}
	got := taskCommentAuthorForAdd(context.Background(), remote, "task-1", "", false)
	if got.Kind != "agent" || got.ID != "Node new agent" {
		t.Fatalf("taskCommentAuthorForAdd = %+v, want latest workflow run agent", got)
	}
}

func TestTaskCommentAuthorForAddUsesSessionFallbackForNonWorkflowAgent(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-other")
	remote := &commentAuthorRemote{sessionName: "triage"}
	got := taskCommentAuthorForAdd(context.Background(), remote, "task-1", "", false)
	if got.Kind != "agent" || got.ID != "Session triage agent" {
		t.Fatalf("taskCommentAuthorForAdd = %+v, want session fallback agent", got)
	}
}

func TestTaskCommentListUsesReadablePaginatedOutput(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &commentListRemote{
		taskID: "task-1",
		comments: []serverapi.WorkflowTaskComment{
			{ID: "comment-old", TaskID: "task-1", Author: "user", Body: "old", CreatedAtUnixMs: 1735689600000},
			{ID: "comment-new", TaskID: "task-1", Author: "agent", AuthorID: "reviewer", Body: "new", CreatedAtUnixMs: 1735776000000},
			{ID: "comment-extra", TaskID: "task-1", Author: "user", Body: "extra", CreatedAtUnixMs: 1735862400000},
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "comment", "list", "task-1", "--page-size", "2")
	if code != 0 {
		t.Fatalf("task comment list exit=%d stderr=%q", code, stderr)
	}
	want := "Comments (2):\nUser at 2025-01-03T00:00:00Z UTC:\nextra\n---\nreviewer at 2025-01-02T00:00:00Z UTC:\nnew\n"
	if stdout != want {
		t.Fatalf("task comment list output = %q, want %q", stdout, want)
	}
	if !strings.Contains(stderr, "Next page token: `2`") {
		t.Fatalf("task comment list stderr = %q, want next page token", stderr)
	}
	if len(remote.listRequests) != 1 || remote.listRequests[0].TaskID != "task-1" || remote.listRequests[0].PageSize != 2 || remote.listRequests[0].PageToken != "" {
		t.Fatalf("comment list requests = %+v, want first page request", remote.listRequests)
	}
}

func TestTaskCommentListUsesPageToken(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &commentListRemote{
		taskID: "task-1",
		comments: []serverapi.WorkflowTaskComment{
			{ID: "comment-old", TaskID: "task-1", Author: "user", Body: "old", CreatedAtUnixMs: 1735689600000},
			{ID: "comment-new", TaskID: "task-1", Author: "agent", AuthorID: "reviewer", Body: "new", CreatedAtUnixMs: 1735776000000},
			{ID: "comment-extra", TaskID: "task-1", Author: "user", Body: "extra", CreatedAtUnixMs: 1735862400000},
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "comment", "list", "task-1", "--page-size", "2", "--page-token", "2")
	if code != 0 {
		t.Fatalf("task comment list exit=%d stderr=%q", code, stderr)
	}
	want := "Comments (1):\nUser at 2025-01-01T00:00:00Z UTC:\nold\n"
	if stdout != want {
		t.Fatalf("task comment list output = %q, want %q", stdout, want)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("task comment list stderr = %q, want empty", stderr)
	}
	if len(remote.listRequests) != 1 || remote.listRequests[0].PageSize != 2 || remote.listRequests[0].PageToken != "2" {
		t.Fatalf("comment list requests = %+v, want second page request", remote.listRequests)
	}
}

func TestTaskCommentsPluralListAliasUsesCommentList(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &commentListRemote{
		taskID: "task-1",
		comments: []serverapi.WorkflowTaskComment{
			{ID: "comment-1", TaskID: "task-1", Author: "agent", AuthorID: "reviewer", Body: "note", CreatedAtUnixMs: 1735689600000},
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "comments", "list", "task-1")
	if code != 0 {
		t.Fatalf("task comments list exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "reviewer at 2025-01-01T00:00:00Z UTC:\nnote\n") {
		t.Fatalf("task comments list output = %q, want comment output", stdout)
	}
	if len(remote.listRequests) != 1 || remote.listRequests[0].TaskID != "task-1" {
		t.Fatalf("comment list requests = %+v, want plural alias to route to list", remote.listRequests)
	}
}

func TestTaskCommentsPluralAddAliasUsesCommentAdd(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &commentAddRemote{taskID: "task-1"}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "comments", "add", "task-1", "--body", "note", "--author", "user")
	if code != 0 {
		t.Fatalf("task comments add exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "comment_id\tcomment-1\n") {
		t.Fatalf("task comments add output = %q, want comment id", stdout)
	}
	if len(remote.addRequests) != 1 || remote.addRequests[0].TaskID != "task-1" || remote.addRequests[0].Body != "note" {
		t.Fatalf("comment add requests = %+v, want plural alias to route to add", remote.addRequests)
	}
}

type commentAddRemote struct {
	client.WorkflowClient
	taskID      string
	addRequests []serverapi.WorkflowTaskCommentAddRequest
}

func (r *commentAddRemote) Close() error { return nil }

func (r *commentAddRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *commentAddRemote) GetWorkflowTask(_ context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	return serverapi.WorkflowTaskGetResponse{Task: serverapi.WorkflowTaskDetail{Summary: serverapi.WorkflowTaskSummary{ID: strings.TrimSpace(req.TaskID)}}}, nil
}

func (r *commentAddRemote) AddWorkflowTaskComment(_ context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error) {
	r.addRequests = append(r.addRequests, req)
	return serverapi.WorkflowTaskCommentAddResponse{Comment: serverapi.WorkflowTaskComment{ID: "comment-1", TaskID: r.taskID, Body: req.Body, Author: req.Author, AuthorID: req.AuthorID}}, nil
}

func TestResolveWorkflowTaskIDUsesDirectShortIDLookup(t *testing.T) {
	remote := &directTaskResolveRemote{}
	taskID, err := resolveWorkflowTaskID(context.Background(), config.App{WorkspaceRoot: t.TempDir()}, remote, "project-1", "BLD-123")
	if err != nil {
		t.Fatalf("resolveWorkflowTaskID: %v", err)
	}
	if taskID != "task-123" {
		t.Fatalf("resolveWorkflowTaskID = %q, want task-123", taskID)
	}
	if len(remote.taskRequests) != 1 || remote.taskRequests[0].ProjectID != "project-1" || remote.taskRequests[0].ShortID != "BLD-123" {
		t.Fatalf("task requests = %+v, want direct project short-id lookup", remote.taskRequests)
	}
	if remote.boardRequests != 0 {
		t.Fatalf("board requests = %d, want none for short-id resolution", remote.boardRequests)
	}
}

type directTaskResolveRemote struct {
	client.WorkflowClient
	taskRequests  []serverapi.WorkflowTaskGetRequest
	boardRequests int
}

func (r *directTaskResolveRemote) Close() error { return nil }

func (r *directTaskResolveRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, errors.New("unexpected project path resolution")
}

func (r *directTaskResolveRemote) GetWorkflowTask(_ context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	r.taskRequests = append(r.taskRequests, req)
	return serverapi.WorkflowTaskGetResponse{Task: serverapi.WorkflowTaskDetail{Summary: serverapi.WorkflowTaskSummary{ID: "task-123", ProjectID: req.ProjectID, ShortID: req.ShortID}}}, nil
}

func (r *directTaskResolveRemote) GetWorkflowBoard(context.Context, serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
	r.boardRequests++
	return serverapi.WorkflowBoardResponse{}, errors.New("unexpected board fetch")
}

type commentListRemote struct {
	client.WorkflowClient
	taskID       string
	comments     []serverapi.WorkflowTaskComment
	listRequests []serverapi.WorkflowTaskCommentListRequest
}

func (r *commentListRemote) Close() error { return nil }

func (r *commentListRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *commentListRemote) GetWorkflowTask(_ context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	return serverapi.WorkflowTaskGetResponse{Task: serverapi.WorkflowTaskDetail{Summary: serverapi.WorkflowTaskSummary{ID: r.taskID, ShortID: "TASK-1"}}}, nil
}

func (r *commentListRemote) ListWorkflowTaskComments(_ context.Context, req serverapi.WorkflowTaskCommentListRequest) (serverapi.WorkflowTaskCommentListResponse, error) {
	r.listRequests = append(r.listRequests, req)
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = taskCommentListDefaultPageSize
	}
	offset := 0
	if strings.TrimSpace(req.PageToken) != "" {
		parsed, err := strconv.Atoi(req.PageToken)
		if err != nil {
			return serverapi.WorkflowTaskCommentListResponse{}, err
		}
		offset = parsed
	}
	sortedComments := sortedTaskCommentsByCreatedAt(r.comments)
	if offset >= len(sortedComments) {
		return serverapi.WorkflowTaskCommentListResponse{}, nil
	}
	end := offset + pageSize
	nextPageToken := ""
	if end < len(sortedComments) {
		nextPageToken = strconv.Itoa(end)
	} else {
		end = len(sortedComments)
	}
	return serverapi.WorkflowTaskCommentListResponse{Comments: sortedComments[offset:end], NextPageToken: nextPageToken}, nil
}

type commentAuthorRemote struct {
	client.WorkflowClient
	task        serverapi.WorkflowTaskDetail
	sessionName string
}

func (r *commentAuthorRemote) Close() error { return nil }

func (r *commentAuthorRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *commentAuthorRemote) GetWorkflowTask(context.Context, serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	return serverapi.WorkflowTaskGetResponse{Task: r.task}, nil
}

func (r *commentAuthorRemote) GetSessionMainView(context.Context, serverapi.SessionMainViewRequest) (serverapi.SessionMainViewResponse, error) {
	return serverapi.SessionMainViewResponse{MainView: clientui.RuntimeMainView{
		Session: clientui.RuntimeSessionView{SessionName: r.sessionName},
	}}, nil
}

func TestTaskShowFindsSameProjectTaskOutsideSelectedWorkflow(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	defaultWorkflowID := createRunnableWorkflowForCommandTest(t, "Default Workflow")
	if _, linkErr, code := runWorkflowRootCommand("workflow", "link", binding.ProjectID, defaultWorkflowID, "--default"); code != 0 {
		t.Fatalf("default workflow link exit=%d stderr=%q", code, linkErr)
	}
	otherWorkflowID := createRunnableWorkflowForCommandTest(t, "Other Workflow")
	if _, linkErr, code := runWorkflowRootCommand("workflow", "link", binding.ProjectID, otherWorkflowID); code != 0 {
		t.Fatalf("other workflow link exit=%d stderr=%q", code, linkErr)
	}
	taskOut, taskErr, code := runWorkflowRootCommand("task", "create", "--title", "Other Task", "--body", "Body", "--workflow", otherWorkflowID, "--project", binding.ProjectID)
	if code != 0 {
		t.Fatalf("task create exit=%d stderr=%q", code, taskErr)
	}
	shortID := taskDetailHeadingShortID(t, taskOut)
	showOut, showErr, code := runWorkflowRootCommand("task", "show", "--project", binding.ProjectID, shortID)
	if code != 0 {
		t.Fatalf("task show exit=%d stderr=%q", code, showErr)
	}
	if !strings.Contains(showOut, shortID+": Other Task\n") {
		t.Fatalf("task show output = %q, want task short id %s", showOut, shortID)
	}
	if strings.Contains(showOut, "Note:") {
		t.Fatalf("task show output = %q, did not expect cross-project note", showOut)
	}
}

func TestTaskShowUsesRegisteredTaskWorktreeRootAsCurrentProject(t *testing.T) {
	cfg, binding, remote := newWorkflowCommandLoopback(t)
	worktreeRoot := t.TempDir()
	worktreeCfg := cfg
	worktreeCfg.WorkspaceRoot = worktreeRoot
	remote.projectBindingsByRoot = map[string]serverapi.ProjectBinding{
		worktreeRoot: {
			ProjectID:     binding.ProjectID,
			ProjectKey:    binding.ProjectKey,
			ProjectName:   binding.ProjectName,
			WorkspaceID:   binding.WorkspaceID,
			CanonicalRoot: worktreeRoot,
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, worktreeCfg, remote)
	defer restore()

	workflowID := createRunnableWorkflowForCommandTest(t, "Task Worktree Workflow")
	if _, linkErr, code := runWorkflowRootCommand("workflow", "link", binding.ProjectID, workflowID, "--default"); code != 0 {
		t.Fatalf("workflow link exit=%d stderr=%q", code, linkErr)
	}
	taskOut, taskErr, code := runWorkflowRootCommand("task", "create", "--title", "Worktree Task", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID)
	if code != 0 {
		t.Fatalf("task create exit=%d stderr=%q", code, taskErr)
	}
	shortID := taskDetailHeadingShortID(t, taskOut)

	showOut, showErr, code := runWorkflowRootCommand("task", "show", shortID)
	if code != 0 {
		t.Fatalf("task show from worktree root exit=%d stderr=%q", code, showErr)
	}
	if !strings.Contains(showOut, shortID+": Worktree Task\n") {
		t.Fatalf("task show output = %q, want task short id %s", showOut, shortID)
	}
}

func TestTaskShowWarnsWhenShortIDBelongsToAnotherKnownProject(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &crossProjectTaskShowRemote{}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "show", "--project", "project-current", "OTH-1")
	if code != 0 {
		t.Fatalf("task show exit=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "Note:") {
		t.Fatalf("task show output = %q, did not expect cross-project note in stdout", stdout)
	}
	if !strings.Contains(stderr, "Note: This task belongs to another project OTH") {
		t.Fatalf("task show stderr = %q, want cross-project note", stderr)
	}
	if !strings.Contains(stdout, "OTH-1: Other Task\n") {
		t.Fatalf("task show output = %q, want other task", stdout)
	}
}

func TestTaskShowFallsBackAfterRemoteScopedShortIDNotFound(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &crossProjectTaskShowRemote{scopedErr: serverapi.ErrWorkflowTaskNotFound}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("task", "show", "--project", "project-current", "OTH-1")
	if code != 0 {
		t.Fatalf("task show exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "OTH-1: Other Task\n") {
		t.Fatalf("task show output = %q, want global fallback task", stdout)
	}
	if remote.unscopedCalls != 1 {
		t.Fatalf("unscoped calls = %d, want one fallback lookup", remote.unscopedCalls)
	}
}

func TestTaskShowSurfacesScopedShortIDLookupErrors(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &crossProjectTaskShowRemote{scopedErr: errors.New("backend unavailable")}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	_, stderr, code := runWorkflowRootCommand("task", "show", "--project", "project-current", "OTH-1")
	if code == 0 {
		t.Fatalf("task show exit=%d, want failure", code)
	}
	if !strings.Contains(stderr, "backend unavailable") {
		t.Fatalf("task show stderr = %q, want scoped lookup error", stderr)
	}
	if remote.unscopedCalls != 0 {
		t.Fatalf("unscoped calls = %d, want no fallback after scoped lookup error", remote.unscopedCalls)
	}
}

func TestTaskShowSurfacesUnscopedShortIDLookupErrors(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &crossProjectTaskShowRemote{unscopedErr: errors.New("ambiguous short id")}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	_, stderr, code := runWorkflowRootCommand("task", "show", "--project", "project-current", "OTH-1")
	if code == 0 {
		t.Fatalf("task show exit=%d, want failure", code)
	}
	if !strings.Contains(stderr, "ambiguous short id") {
		t.Fatalf("task show stderr = %q, want unscoped lookup error", stderr)
	}
	if strings.Contains(stderr, "not found") {
		t.Fatalf("task show stderr = %q, want raw unscoped lookup error", stderr)
	}
}

func createRunnableWorkflowForCommandTest(t *testing.T, name string) string {
	t.Helper()
	workflowID := workflowCreateForTest(t, name).ID
	if _, nodeErr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work"); code != 0 {
		t.Fatalf("workflow node add exit=%d stderr=%q", code, nodeErr)
	}
	if _, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session", "--prompt", "Do work"); code != 0 {
		t.Fatalf("workflow start edge add exit=%d stderr=%q", code, edgeErr)
	}
	if _, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "implement", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "new_session"); code != 0 {
		t.Fatalf("workflow done edge add exit=%d stderr=%q", code, edgeErr)
	}
	return workflowID
}

type crossProjectTaskShowRemote struct {
	client.WorkflowClient
	scopedErr     error
	unscopedErr   error
	unscopedCalls int
}

func (r *crossProjectTaskShowRemote) Close() error { return nil }

func (r *crossProjectTaskShowRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *crossProjectTaskShowRemote) GetWorkflowTask(_ context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	if req.ProjectID == "project-current" && req.ShortID == "OTH-1" {
		if r.scopedErr != nil {
			return serverapi.WorkflowTaskGetResponse{}, r.scopedErr
		}
		return serverapi.WorkflowTaskGetResponse{}, sql.ErrNoRows
	}
	if req.ProjectID == "" && req.ShortID == "OTH-1" {
		r.unscopedCalls++
		if r.unscopedErr != nil {
			return serverapi.WorkflowTaskGetResponse{}, r.unscopedErr
		}
		return serverapi.WorkflowTaskGetResponse{Task: serverapi.WorkflowTaskDetail{
			Summary: serverapi.WorkflowTaskSummary{ID: "task-other", ProjectID: "project-other", WorkflowID: "workflow-other", ShortID: "OTH-1", Title: "Other Task"},
			Project: serverapi.ProjectBoardProject{ProjectID: "project-other", ProjectKey: "OTH", DisplayName: "Other"},
		}}, nil
	}
	return serverapi.WorkflowTaskGetResponse{}, sql.ErrNoRows
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

func TestTaskListStatusFromCardStatus(t *testing.T) {
	cases := map[string]string{
		"backlog":          "open",
		"active":           "open",
		"":                 "open",
		"running":          "running",
		"interrupted":      "running",
		"waiting_question": "running",
		"waiting_approval": "running",
		"done":             "done",
		"canceled":         "canceled",
	}
	for kind, want := range cases {
		if got := taskListStatusFromCardStatus(serverapi.WorkflowTaskStatus{Kind: kind}); got != want {
			t.Fatalf("taskListStatusFromCardStatus(%q) = %q, want %q", kind, got, want)
		}
	}
}

func testDoneTaskCard(taskID string, shortID string, title string) serverapi.WorkflowBoardTaskCard {
	return serverapi.WorkflowBoardTaskCard{
		TaskID:     taskID,
		ShortID:    shortID,
		Title:      title,
		WorkflowID: "workflow-1",
		Status:     serverapi.WorkflowTaskStatus{Kind: "done"},
	}
}

func TestWriteTaskDetailIncludesParallelBranchIDs(t *testing.T) {
	var stdout bytes.Buffer
	writeTaskDetail(&stdout, serverapi.WorkflowTaskDetail{
		Summary: serverapi.WorkflowTaskSummary{
			ID:              "task-1",
			ShortID:         "WOR-1",
			WorkflowID:      "workflow-1",
			Title:           "Task",
			CreatedAtUnixMs: 1735689600000,
		},
		Project:         serverapi.ProjectBoardProject{ProjectID: "project-1", DisplayName: "Project"},
		Workflow:        serverapi.WorkflowPickerItem{WorkflowID: "workflow-1", DisplayName: "Workflow"},
		Body:            "Do the work.",
		SourceWorkspace: serverapi.ProjectWorkspaceSummary{RootPath: "/workspace"},
		ManagedWorktree: &serverapi.WorktreeView{CanonicalRoot: "/workspace-task"},
		SourceURL:       "https://example.test/source",
		Status:          serverapi.WorkflowTaskStatus{Kind: "backlog"},
		Runs: []serverapi.WorkflowRun{
			{ID: "run-1"},
			{ID: "run-2"},
		},
	})

	output := stdout.String()
	for _, want := range []string{
		"WOR-1: Task\n",
		"Body:\n```md\nDo the work.\n```\n",
		"Status: open\n",
		"Project: \"Project\" (project-1)\n",
		"Workflow: \"Workflow\" (workflow-1)\n",
		"Created at 2025-01-01T00:00:00Z UTC\n",
		"Total agent runs: 2\n",
		"Main workspace: /workspace\n",
		"Worktree: /workspace-task\n",
		"Imported from: https://example.test/source\n",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("task detail output = %q, want %q", output, want)
		}
	}
	if strings.Contains(output, "placements") || strings.Contains(output, "transitions") {
		t.Fatalf("task detail output = %q, did not expect internal placement/transition dump", output)
	}
}

func TestWriteTaskDetailComments(t *testing.T) {
	var stdout bytes.Buffer
	writeTaskDetail(&stdout, serverapi.WorkflowTaskDetail{
		Summary:  serverapi.WorkflowTaskSummary{ShortID: "WOR-1", Title: "Task", CreatedAtUnixMs: 1735689600000},
		Project:  serverapi.ProjectBoardProject{ProjectID: "project-1", DisplayName: "Project"},
		Workflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1", DisplayName: "Workflow"},
		Comments: []serverapi.WorkflowTaskComment{
			{ID: "comment-old", Author: "user", Body: "old", CreatedAtUnixMs: 1735689600000},
			{ID: "comment-new", Author: "agent", AuthorID: "reviewer", Body: "new", CreatedAtUnixMs: 1735776000000},
		},
	})

	output := stdout.String()
	want := "Comments (2):\nreviewer at 2025-01-02T00:00:00Z UTC:\nnew\n---\nUser at 2025-01-01T00:00:00Z UTC:\nold\n"
	if !strings.Contains(output, want) {
		t.Fatalf("task detail output = %q, want sorted comments block %q", output, want)
	}
}

func TestWriteTaskDetailCommentOverflowPointsToCommentCommand(t *testing.T) {
	comments := make([]serverapi.WorkflowTaskComment, 10)
	for i := range comments {
		comments[i] = serverapi.WorkflowTaskComment{ID: fmt.Sprintf("comment-%d", i), Author: "user", Body: "comment", CreatedAtUnixMs: 1735689600000 + int64(i)}
	}
	var stdout bytes.Buffer
	writeTaskDetail(&stdout, serverapi.WorkflowTaskDetail{
		Summary:  serverapi.WorkflowTaskSummary{ShortID: "WOR-1", Title: "Task", CreatedAtUnixMs: 1735689600000},
		Project:  serverapi.ProjectBoardProject{ProjectID: "project-1", DisplayName: "Project"},
		Workflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1", DisplayName: "Workflow"},
		Comments: comments,
	})

	output := stdout.String()
	if !strings.Contains(output, "Comments under this task: 10. `kent task comment list WOR-1` to show them.\n") {
		t.Fatalf("task detail output = %q, want comment overflow pointer", output)
	}
	if strings.Contains(output, "Comments (10):") {
		t.Fatalf("task detail output = %q, did not expect inline overflow comments", output)
	}
}

func TestTaskMutationOutputRenderers(t *testing.T) {
	task := serverapi.WorkflowTaskDetail{
		Summary:  serverapi.WorkflowTaskSummary{ID: "task-1", ShortID: "BLD-1", Title: "Task"},
		Workflow: serverapi.WorkflowPickerItem{WorkflowID: "workflow-1", DisplayName: "Workflow"},
		Placements: []serverapi.WorkflowPlacement{
			{ID: "placement-1", NodeID: "node-1", NodeKey: "implement"},
			{ID: "placement-2", NodeID: "node-2", NodeKey: "review"},
		},
		Runs: []serverapi.WorkflowRun{
			{ID: "run-1", PlacementID: "placement-1", NodeID: "node-1", SessionID: "session-1"},
			{ID: "run-2", PlacementID: "placement-2", NodeID: "node-2", SessionID: "session-2"},
		},
		Transitions: []serverapi.WorkflowTaskTransition{
			{
				ID:            "transition-1",
				SourceNodeKey: "implement",
				TransitionID:  "done",
				Edges: []serverapi.WorkflowTransitionEdge{
					{EdgeKey: "done", TargetNodeKey: "review", State: "applied"},
				},
			},
		},
	}

	var start bytes.Buffer
	writeTaskStartResult(&start, task, serverapi.WorkflowTaskStartResponse{RunID: "run-1", PlacementID: "placement-1", TransitionID: "transition-start"})
	if got, want := start.String(), "Started task BLD-1 in session session-1 using workflow \"Workflow\" (workflow-1).\nFirst node: implement\n"; got != want {
		t.Fatalf("start output = %q, want %q", got, want)
	}

	var resume bytes.Buffer
	writeTaskResumeResult(&resume, task, serverapi.WorkflowTaskResumeResponse{RunID: "run-1", PlacementID: "placement-1", NodeID: "node-1", SessionID: "session-1"})
	if got, want := resume.String(), "Resumed task BLD-1 in session session-1.\nCurrent node: implement\n"; got != want {
		t.Fatalf("resume output = %q, want %q", got, want)
	}

	var approve bytes.Buffer
	writeTaskTransitionResult(&approve, "Approved transition of", task, "transition-1", []string{"run-2"})
	if got, want := approve.String(), "Approved transition of BLD-1 from `implement` to `done`.\nBecause of this, started node review in session session-2.\n"; got != want {
		t.Fatalf("approve output = %q, want %q", got, want)
	}

	var move bytes.Buffer
	writeTaskTransitionResult(&move, "Moved task", task, "transition-1", nil)
	if got, want := move.String(), "Moved task BLD-1 from `implement` to `done`.\n"; got != want {
		t.Fatalf("move output = %q, want %q", got, want)
	}
}

func TestTaskStartSessionPollingTimeoutReportsStartedTask(t *testing.T) {
	remote := &taskSessionPollingRemote{task: serverapi.WorkflowTaskDetail{
		Summary: serverapi.WorkflowTaskSummary{ID: "task-1", ShortID: "BLD-1", Title: "Task"},
		Runs:    []serverapi.WorkflowRun{{ID: "run-1"}},
	}}
	_, err := waitForWorkflowTaskRunSession(context.Background(), remote, "task-1", "run-1", 10*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatalf("waitForWorkflowTaskRunSession succeeded, want timeout")
	}
	if got := err.Error(); !strings.Contains(got, "started task BLD-1 with run run-1") || !strings.Contains(got, "session id was not assigned within") {
		t.Fatalf("timeout error = %q, want started task context and timeout detail", got)
	}
}

func TestTaskStartCommandPollsForSessionAndPrintsReadableOutput(t *testing.T) {
	restorePolling := replaceTaskStartSessionPolling(t, 50*time.Millisecond, time.Millisecond)
	defer restorePolling()
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &taskStartPollingRemote{
		projectID:   "project-1",
		taskID:      "task-1",
		shortID:     "BLD-1",
		workflowID:  "workflow-1",
		workflow:    "Workflow",
		placementID: "placement-1",
		runID:       "run-1",
		sessionID:   "session-1",
		nodeID:      "node-1",
		nodeKey:     "implement",
	}
	restoreRemote := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restoreRemote()

	stdout, stderr, code := runWorkflowRootCommand("task", "start", "--project", "project-1", "BLD-1")
	if code != 0 {
		t.Fatalf("task start exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	want := "Started task BLD-1 in session session-1 using workflow \"Workflow\" (workflow-1).\nFirst node: implement\n"
	if stdout != want {
		t.Fatalf("task start stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("task start stderr = %q, want empty", stderr)
	}
	if remote.taskIDDetailCalls < 2 {
		t.Fatalf("task detail calls = %d, want polling before session assignment", remote.taskIDDetailCalls)
	}
}

func replaceTaskStartSessionPolling(t *testing.T, timeout time.Duration, interval time.Duration) func() {
	t.Helper()
	originalTimeout := taskStartSessionPollTimeout
	originalInterval := taskStartSessionPollInterval
	taskStartSessionPollTimeout = timeout
	taskStartSessionPollInterval = interval
	return func() {
		taskStartSessionPollTimeout = originalTimeout
		taskStartSessionPollInterval = originalInterval
	}
}

type taskSessionPollingRemote struct {
	client.WorkflowClient
	task serverapi.WorkflowTaskDetail
}

func (r *taskSessionPollingRemote) Close() error { return nil }

func (r *taskSessionPollingRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *taskSessionPollingRemote) GetWorkflowTask(context.Context, serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	return serverapi.WorkflowTaskGetResponse{Task: r.task}, nil
}

type taskStartPollingRemote struct {
	client.WorkflowClient
	projectID         string
	taskID            string
	shortID           string
	workflowID        string
	workflow          string
	placementID       string
	runID             string
	sessionID         string
	nodeID            string
	nodeKey           string
	taskIDDetailCalls int
}

func (r *taskStartPollingRemote) Close() error { return nil }

func (r *taskStartPollingRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{Binding: &serverapi.ProjectBinding{ProjectID: r.projectID}}, nil
}

func (r *taskStartPollingRemote) GetWorkflowTask(_ context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	if req.ProjectID == r.projectID && req.ShortID == r.shortID {
		return serverapi.WorkflowTaskGetResponse{Task: r.taskDetail("")}, nil
	}
	if req.TaskID == r.taskID {
		r.taskIDDetailCalls++
		if r.taskIDDetailCalls == 1 {
			return serverapi.WorkflowTaskGetResponse{Task: r.taskDetail("")}, nil
		}
		return serverapi.WorkflowTaskGetResponse{Task: r.taskDetail(r.sessionID)}, nil
	}
	return serverapi.WorkflowTaskGetResponse{}, sql.ErrNoRows
}

func (r *taskStartPollingRemote) StartWorkflowTask(context.Context, serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error) {
	return serverapi.WorkflowTaskStartResponse{TransitionID: "transition-1", PlacementID: r.placementID, RunID: r.runID}, nil
}

func (r *taskStartPollingRemote) taskDetail(sessionID string) serverapi.WorkflowTaskDetail {
	return serverapi.WorkflowTaskDetail{
		Summary:  serverapi.WorkflowTaskSummary{ID: r.taskID, ShortID: r.shortID, WorkflowID: r.workflowID, ProjectID: r.projectID, Title: "Task"},
		Workflow: serverapi.WorkflowPickerItem{WorkflowID: r.workflowID, DisplayName: r.workflow},
		Placements: []serverapi.WorkflowPlacement{
			{ID: r.placementID, TaskID: r.taskID, NodeID: r.nodeID, NodeKey: r.nodeKey},
		},
		Runs: []serverapi.WorkflowRun{
			{ID: r.runID, TaskID: r.taskID, PlacementID: r.placementID, NodeID: r.nodeID, SessionID: sessionID},
		},
	}
}

func TestWorkflowCommandValidationErrorsAreActionable(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowID := workflowCreateForTest(t, "Workflow").ID
	_, stderr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "Bad-Key", "--kind", "agent")
	if code == 0 || !strings.Contains(stderr, "key must start with a lowercase letter") {
		t.Fatalf("invalid node code=%d stderr=%q, want actionable key validation", code, stderr)
	}
}

func TestWorkflowHelpAdvertisesJSONContract(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := workflowSubcommand([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("workflow help exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--json") {
		t.Fatalf("workflow help did not advertise json contract stderr=%q", stderr.String())
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
	remote := &workflowCommandLoopbackRemote{WorkflowClient: client.NewLoopbackWorkflowClient(service), cfg: cfg, binding: binding, metadataStore: metadataStore, store: store}
	return cfg, binding, remote
}

func createWorkflowCommandTestSession(t *testing.T, cfg config.App, binding metadata.Binding, metadataStore *metadata.Store) string {
	t.Helper()
	store, err := session.Create(
		filepath.Join(filepath.Join(cfg.PersistenceRoot, "projects"), binding.ProjectID, "sessions"),
		filepath.Base(cfg.WorkspaceRoot),
		cfg.WorkspaceRoot,
		metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	return store.Meta().SessionID
}

func replaceWorkflowCommandRemoteOpener(t *testing.T, cfg config.App, remote workflowCommandRemote) func() {
	t.Helper()
	original := workflowCommandRemoteOpener
	workflowCommandRemoteOpener = func(context.Context, string) (config.App, workflowCommandRemote, error) {
		return cfg, remote, nil
	}
	return func() { workflowCommandRemoteOpener = original }
}

// setupLinkedWorkflow creates a minimal valid workflow (a single agent node
// wired between the auto-created backlog and done nodes), links it to the
// project as default, and returns its id so task create has a usable workflow.
func setupLinkedWorkflow(t *testing.T, projectID string, name string) string {
	t.Helper()
	workflowID := workflowCreateForTest(t, name).ID
	if workflowID == "" {
		t.Fatal("workflow create did not return a workflow id")
	}
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work")
	runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session", "--prompt", "Do work")
	runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "implement", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "new_session")
	runWorkflowRootCommandOK(t, "workflow", "link", projectID, workflowID, "--default")
	return workflowID
}

func runWorkflowRootCommand(args ...string) (string, string, int) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := rootCommand(args, strings.NewReader(""), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func runWorkflowRootCommandOK(t *testing.T, args ...string) (string, string) {
	t.Helper()
	stdout, stderr, code := runWorkflowRootCommand(args...)
	if code != 0 {
		t.Fatalf("%s exit=%d stdout=%q stderr=%q", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout, stderr
}

// workflowCreateForTest runs `workflow create --json` and decodes the created
// record so tests can extract the generated workflow id without parsing prose.
func workflowCreateForTest(t *testing.T, args ...string) serverapi.WorkflowRecord {
	t.Helper()
	full := append([]string{"workflow", "create", "--json"}, args...)
	out, _ := runWorkflowRootCommandOK(t, full...)
	var record serverapi.WorkflowRecord
	if err := json.Unmarshal([]byte(out), &record); err != nil {
		t.Fatalf("decode workflow create json %q: %v", out, err)
	}
	return record
}

func workflowNodeAddForTest(t *testing.T, args ...string) workflowNodeOutput {
	t.Helper()
	full := append([]string{"workflow", "node", "add"}, args...)
	full = append(full, "--json")
	out, _ := runWorkflowRootCommandOK(t, full...)
	var node workflowNodeOutput
	if err := json.Unmarshal([]byte(out), &node); err != nil {
		t.Fatalf("decode workflow node add json %q: %v", out, err)
	}
	return node
}

func workflowEdgeAddForTest(t *testing.T, args ...string) workflowEdgeOutput {
	t.Helper()
	full := append([]string{"workflow", "edge", "add"}, args...)
	full = append(full, "--json")
	out, _ := runWorkflowRootCommandOK(t, full...)
	var edge workflowEdgeOutput
	if err := json.Unmarshal([]byte(out), &edge); err != nil {
		t.Fatalf("decode workflow edge add json %q: %v", out, err)
	}
	return edge
}

func workflowLinkForTest(t *testing.T, args ...string) serverapi.ProjectWorkflowLink {
	t.Helper()
	full := append([]string{"workflow", "link"}, args...)
	full = append(full, "--json")
	out, _ := runWorkflowRootCommandOK(t, full...)
	var link serverapi.ProjectWorkflowLink
	if err := json.Unmarshal([]byte(out), &link); err != nil {
		t.Fatalf("decode workflow link json %q: %v", out, err)
	}
	return link
}

// workflowInspectDefinitionForTest reads the persisted graph via `workflow inspect --json` so
// tests can assert applied structure instead of the readable rendering.
func workflowInspectDefinitionForTest(t *testing.T, workflowRef string) serverapi.WorkflowDefinition {
	t.Helper()
	out, _ := runWorkflowRootCommandOK(t, "workflow", "inspect", "--json", workflowRef)
	var def serverapi.WorkflowDefinition
	if err := json.Unmarshal([]byte(out), &def); err != nil {
		t.Fatalf("decode workflow inspect json %q: %v", out, err)
	}
	return def
}

// workflowValidateJSONForTest runs `workflow validate --json` with the given args and returns the
// structured response plus the process exit code, so callers assert validity via the typed Valid
// field and the exit-code contract rather than the human-readable validation prose.
func workflowValidateJSONForTest(t *testing.T, args ...string) (serverapi.WorkflowValidateResponse, int) {
	t.Helper()
	out, _, code := runWorkflowRootCommand(append([]string{"workflow", "validate", "--json"}, args...)...)
	var resp serverapi.WorkflowValidateResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("workflow validate --json %v = %q, want JSON: %v", args, out, err)
	}
	return resp, code
}

func workflowEdgeByKeyForTest(t *testing.T, def serverapi.WorkflowDefinition, key string) serverapi.WorkflowEdge {
	t.Helper()
	for _, edge := range def.Edges {
		if edge.Key == key {
			return edge
		}
	}
	t.Fatalf("edge %q not found in definition %+v", key, def.Edges)
	return serverapi.WorkflowEdge{}
}

func workflowNodeKeyForID(def serverapi.WorkflowDefinition, nodeID string) string {
	for _, node := range def.Nodes {
		if node.ID == nodeID {
			return node.Key
		}
	}
	return ""
}

func workflowTransitionGroupForID(def serverapi.WorkflowDefinition, groupID string) serverapi.WorkflowTransitionGroup {
	for _, group := range def.TransitionGroups {
		if group.ID == groupID {
			return group
		}
	}
	return serverapi.WorkflowTransitionGroup{}
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

func taskDetailHeadingShortID(t *testing.T, output string) string {
	t.Helper()
	firstLine, _, _ := strings.Cut(output, "\n")
	shortID, _, ok := strings.Cut(firstLine, ": ")
	if !ok || strings.TrimSpace(shortID) == "" {
		t.Fatalf("task detail heading not found in output %q", output)
	}
	return shortID
}

func workflowCommandStoredEdgeByID(t *testing.T, ctx context.Context, store *workflowstore.Store, workflowID string, edgeID string) workflow.Edge {
	t.Helper()
	def, _, err := store.GetDefinition(ctx, workflow.WorkflowID(workflowID))
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	for _, edge := range def.Edges {
		if string(edge.ID) == edgeID {
			return edge
		}
	}
	t.Fatalf("missing edge %s in %+v", edgeID, def.Edges)
	return workflow.Edge{}
}

func workflowCommandEdgeRecord(edge workflow.Edge) workflowstore.EdgeRecord {
	return workflowstore.EdgeRecord{
		ID:                 edge.ID,
		WorkflowID:         edge.WorkflowID,
		TransitionGroupID:  edge.TransitionGroupID,
		Key:                edge.Key,
		TargetNodeID:       edge.TargetNodeID,
		RequiresApproval:   edge.RequiresApproval,
		ContextMode:        edge.ContextMode,
		ContextSource:      edge.ContextSource,
		InputBindings:      edge.InputBindings,
		PromptTemplate:     edge.PromptTemplate,
		Parameters:         edge.Parameters,
		OutputRequirements: edge.OutputRequirements,
	}
}

func TestWorkflowListPaginatesAndResolutionDoesNotDrainPages(t *testing.T) {
	cfg := config.App{WorkspaceRoot: t.TempDir()}
	remote := &pagedWorkflowListRemote{
		delayAfterFirstPage: true,
		pages: map[string]serverapi.WorkflowListResponse{
			"": {
				Workflows: []serverapi.WorkflowRecord{
					{ID: "workflow-1", Name: "First", Version: 1},
				},
				NextPageToken: "next",
			},
			"next": {
				Workflows: []serverapi.WorkflowRecord{
					{ID: "workflow-2", Name: "Second", Version: 2},
				},
			},
		},
		definitions: map[string]serverapi.WorkflowDefinition{
			"workflow-2": {Workflow: serverapi.WorkflowRecord{ID: "workflow-2", Name: "Second", Version: 2}},
		},
	}
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	stdout, stderr, code := runWorkflowRootCommand("workflow", "list")
	if code != 0 {
		t.Fatalf("workflow list exit=%d stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "workflow-1: First (v1)") || strings.Contains(stdout, "workflow-2: Second (v2)") {
		t.Fatalf("workflow list output = %q, want only first page records", stdout)
	}
	if !strings.Contains(stderr, "Next page token: `next`") {
		t.Fatalf("workflow list stderr = %q, want next page token", stderr)
	}
	if len(remote.requests) != 1 || remote.requests[0].PageToken != "" || remote.requests[0].PageSize != serverapi.WorkflowListMaxPageSize {
		t.Fatalf("workflow list requests = %+v, want single default-sized first page", remote.requests)
	}

	remote.requests = nil
	remote.deadlines = nil
	resolved, err := resolveWorkflowID(context.Background(), remote, "Second")
	if err != nil {
		t.Fatalf("resolveWorkflowID: %v", err)
	}
	if resolved != "workflow-2" {
		t.Fatalf("resolveWorkflowID = %q, want workflow-2", resolved)
	}
	if len(remote.requests) != 1 || remote.requests[0].ExactName != "Second" || remote.requests[0].PageSize != 2 || remote.requests[0].PageToken != "" {
		t.Fatalf("resolve requests = %+v, want bounded exact-name lookup", remote.requests)
	}
	if len(remote.deadlines) != 3 {
		t.Fatalf("resolve deadlines = %+v, want id get plus exact-name list plus name get deadlines", remote.deadlines)
	}
}

type pagedWorkflowListRemote struct {
	client.WorkflowClient
	definitions         map[string]serverapi.WorkflowDefinition
	pages               map[string]serverapi.WorkflowListResponse
	requests            []serverapi.WorkflowListRequest
	deadlines           []time.Time
	delayAfterFirstPage bool
}

func (r *pagedWorkflowListRemote) Close() error { return nil }

func (r *pagedWorkflowListRemote) ResolveProjectPath(context.Context, serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, nil
}

func (r *pagedWorkflowListRemote) ListWorkflows(ctx context.Context, req serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error) {
	callIndex := len(r.requests)
	r.requests = append(r.requests, req)
	if deadline, ok := ctx.Deadline(); ok {
		r.deadlines = append(r.deadlines, deadline)
	}
	if r.delayAfterFirstPage && callIndex == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if strings.TrimSpace(req.ExactName) != "" {
		matches := []serverapi.WorkflowRecord{}
		for _, page := range r.pages {
			for _, record := range page.Workflows {
				if record.Name == req.ExactName {
					matches = append(matches, record)
				}
			}
		}
		if len(matches) > req.PageSize {
			return serverapi.WorkflowListResponse{Workflows: matches[:req.PageSize], NextPageToken: "more"}, nil
		}
		return serverapi.WorkflowListResponse{Workflows: matches}, nil
	}
	return r.pages[req.PageToken], nil
}

func (r *pagedWorkflowListRemote) GetWorkflow(ctx context.Context, req serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error) {
	if deadline, ok := ctx.Deadline(); ok {
		r.deadlines = append(r.deadlines, deadline)
	}
	def, ok := r.definitions[req.WorkflowID]
	if !ok {
		return serverapi.WorkflowGetResponse{}, sql.ErrNoRows
	}
	return serverapi.WorkflowGetResponse{Definition: def}, nil
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
