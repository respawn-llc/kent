package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"builder/prompts"
	"builder/server/metadata"
	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/server/workflowsvc"
	"builder/server/workflowview"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/sessionenv"
)

type workflowCommandLoopbackRemote struct {
	client.WorkflowClient
	cfg                   config.App
	binding               metadata.Binding
	projectBindingsByRoot map[string]serverapi.ProjectBinding
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

	workflowOut, _ := runWorkflowRootCommandOK(t, "workflow", "create", "Workflow")
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	if workflowID == "" {
		t.Fatalf("workflow create output = %q", workflowOut)
	}

	inspectOut, _ := runWorkflowRootCommandOK(t, "workflow", "inspect", workflowID)
	if !strings.Contains(inspectOut, "backlog") || !strings.Contains(inspectOut, "done") {
		t.Fatalf("inspect output = %q, want auto-created backlog and done", inspectOut)
	}

	listOut, _ := runWorkflowRootCommandOK(t, "workflow", "list")
	if !strings.Contains(listOut, workflowID) {
		t.Fatalf("workflow list output = %q, want workflow id", listOut)
	}

	validateOut, _, code := runWorkflowRootCommand("workflow", "validate", workflowID)
	if code == 0 || !strings.Contains(validateOut, "valid\tfalse") {
		t.Fatalf("invalid workflow validate code=%d output=%q", code, validateOut)
	}

	nodeOut, _ := runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "implement", "--kind", "agent", "--agent", "workflow-test", "--prompt", "Do work")
	if labeledOutputValue(t, nodeOut, "node_id") == "" {
		t.Fatalf("node output = %q, want node id", nodeOut)
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "implement", "--context", "new_session", "--prompt", "Do work")
	edgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "implement", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "new_session")
	if labeledOutputValue(t, edgeOut, "edge_id") == "" || labeledOutputValue(t, edgeOut, "group_id") == "" {
		t.Fatalf("edge output = %q, want edge and group ids", edgeOut)
	}

	linkOut, _ := runWorkflowRootCommandOK(t, "workflow", "link", binding.ProjectID, workflowID, "--default")
	linkID := labeledOutputValue(t, linkOut, "link_id")
	if linkID == "" {
		t.Fatalf("link output = %q, want link id", linkOut)
	}
	if got := labeledOutputValue(t, linkOut, "default"); got != "true" {
		t.Fatalf("link default output = %q, want true; output=%q", got, linkOut)
	}

	defaultOut, _ := runWorkflowRootCommandOK(t, "workflow", "default", binding.ProjectID, workflowID)
	if !strings.Contains(defaultOut, linkID) {
		t.Fatalf("default output = %q, want link id %s", defaultOut, linkID)
	}

	unlinkOut, _ := runWorkflowRootCommandOK(t, "workflow", "unlink", binding.ProjectID, workflowID)
	if !strings.Contains(unlinkOut, linkID) {
		t.Fatalf("unlink output = %q, want link id %s", unlinkOut, linkID)
	}

	runWorkflowRootCommandOK(t, "workflow", "link", binding.ProjectID, workflowID, "--default")
	validateOut, _ = runWorkflowRootCommandOK(t, "workflow", "validate", workflowID)
	if !strings.Contains(validateOut, "valid\ttrue") {
		t.Fatalf("validate output = %q, want valid true", validateOut)
	}

	taskOut, _ := runWorkflowRootCommandOK(t, "task", "create", "--title", "Task", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID)
	taskID := labeledOutputValue(t, taskOut, "task_id")
	shortID := labeledOutputValue(t, taskOut, "short_id")
	if taskID == "" || shortID == "" {
		t.Fatalf("task output = %q, want task and short ids", taskOut)
	}

	taskListOut, _ := runWorkflowRootCommandOK(t, "task", "list", "--project", binding.ProjectID)
	if !strings.Contains(taskListOut, shortID) || !strings.Contains(taskListOut, taskID) {
		t.Fatalf("task list output = %q, want ids", taskListOut)
	}

	taskShowOut, _ := runWorkflowRootCommandOK(t, "task", "show", "--project", binding.ProjectID, shortID)
	if !strings.Contains(taskShowOut, "placements") || !strings.Contains(taskShowOut, taskID) {
		t.Fatalf("task show output = %q, want placement section", taskShowOut)
	}
	taskShowOut, _ = runWorkflowRootCommandOK(t, "task", "show", "--project", "missing-project", taskID)
	if !strings.Contains(taskShowOut, taskID) {
		t.Fatalf("task show by full id output = %q, want task id", taskShowOut)
	}

	commentOut, _ := runWorkflowRootCommandOK(t, "task", "comment", "add", "--project", binding.ProjectID, "--body", "note", shortID)
	commentID := labeledOutputValue(t, commentOut, "comment_id")
	if commentID == "" {
		t.Fatalf("comment output = %q, want comment id", commentOut)
	}
	runWorkflowRootCommandOK(t, "task", "comment", "replace", "--body", "edited", commentID)
	commentListOut, _ := runWorkflowRootCommandOK(t, "task", "comment", "list", "--project", binding.ProjectID, shortID)
	if !strings.Contains(commentListOut, commentID) || !strings.Contains(commentListOut, "edited") {
		t.Fatalf("comment list output = %q, want edited comment", commentListOut)
	}
	runWorkflowRootCommandOK(t, "task", "comment", "delete", commentID)

	startOut, _ := runWorkflowRootCommandOK(t, "task", "start", "--project", binding.ProjectID, shortID)
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
	resumeOut, _ := runWorkflowRootCommandOK(t, "task", "resume", "--project", binding.ProjectID, shortID)
	if labeledOutputValue(t, resumeOut, "run_id") != runID {
		t.Fatalf("resume output = %q, want run id %s", resumeOut, runID)
	}

	cancelOut, _ := runWorkflowRootCommandOK(t, "task", "cancel", "--project", binding.ProjectID, "--reason", "test", shortID)
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

func TestWorkflowEditCommandsUpdateNodeAndEdgeMetadata(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowOut, _ := runWorkflowRootCommandOK(t, "workflow", "create", "Editable Workflow")
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	if workflowID == "" {
		t.Fatalf("workflow create output = %q, want workflow id", workflowOut)
	}
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	startEdgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.")
	startEdgeID := labeledOutputValue(t, startEdgeOut, "edge_id")
	if startEdgeID == "" {
		t.Fatalf("start edge output = %q, want edge id", startEdgeOut)
	}
	edgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "triaging", "--transition", "done", "--edge-key", "done", "--to", "done", "--context", "continue_session", "--context-source", "node:triaging")
	edgeID := labeledOutputValue(t, edgeOut, "edge_id")
	if edgeID == "" {
		t.Fatalf("edge output = %q, want edge id", edgeOut)
	}

	updateNodeOut, _ := runWorkflowRootCommandOK(t, "workflow", "node", "update", workflowID, "triaging", "--prompt", "Decide whether the ticket is actionable.")
	if !strings.Contains(updateNodeOut, "triaging") {
		t.Fatalf("node update output = %q, want node key", updateNodeOut)
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, startEdgeID, "--transition", "start_review")
	validateOut, _ := runWorkflowRootCommandOK(t, "workflow", "validate", workflowID)
	if !strings.Contains(validateOut, "valid\ttrue") {
		t.Fatalf("validate output = %q, want start branch prompt preserved", validateOut)
	}

	updateEdgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--transition", "not_actionable", "--edge-key", "not_actionable")
	if !strings.Contains(updateEdgeOut, edgeID) {
		t.Fatalf("edge update output = %q, want edge id", updateEdgeOut)
	}

	inspectOut, _ := runWorkflowRootCommandOK(t, "workflow", "inspect", workflowID)
	for _, want := range []string{"\tnot_actionable\tNot Actionable", "context_source\tnot_actionable\tselected_node\ttriaging"} {
		if !strings.Contains(inspectOut, want) {
			t.Fatalf("inspect output = %q, want %q", inspectOut, want)
		}
	}
}

func TestWorkflowEdgeUpdatePreservesPromptAndParameters(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowOut, _ := runWorkflowRootCommandOK(t, "workflow", "create", "Parameter Preservation Workflow")
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.")
	edgeID := labeledOutputValue(t, edgeOut, "edge_id")
	if edgeID == "" {
		t.Fatalf("edge output = %q, want edge id", edgeOut)
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

	workflowOut, workflowErr, code := runWorkflowRootCommand("workflow", "create", "Context Source Workflow")
	if code != 0 {
		t.Fatalf("workflow create exit=%d stderr=%q", code, workflowErr)
	}
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
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

	workflowOut, workflowErr, code := runWorkflowRootCommand("workflow", "create", "Rollback Workflow")
	if code != 0 {
		t.Fatalf("workflow create exit=%d stderr=%q", code, workflowErr)
	}
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	if _, nodeErr, code := runWorkflowRootCommand("workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage."); code != 0 {
		t.Fatalf("workflow node add exit=%d stderr=%q", code, nodeErr)
	}
	edgeOut, edgeErr, code := runWorkflowRootCommand("workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.")
	if code != 0 {
		t.Fatalf("workflow edge add exit=%d stderr=%q", code, edgeErr)
	}
	edgeID := labeledOutputValue(t, edgeOut, "edge_id")
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
	for _, bad := range []string{"keyonly", "=description", "key=", "  =  "} {
		if _, err := parseWorkflowParameters([]string{bad}); err == nil {
			t.Fatalf("parseWorkflowParameters(%q) = nil error, want failure", bad)
		}
	}
}

func TestWorkflowEdgeAddSetsParametersAndTransitionDescription(t *testing.T) {
	cfg, _, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, cfg, remote)
	defer restore()

	workflowOut, _ := runWorkflowRootCommandOK(t, "workflow", "create", "Parameter Authoring Workflow")
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID,
		"--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session",
		"--prompt", "Use {{.Params.plan_file_path}}.",
		"--transition-description", "Pick when starting the work.",
		"--param", "plan_file_path=Path to the plan doc")
	edgeID := labeledOutputValue(t, edgeOut, "edge_id")

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

	workflowOut, _ := runWorkflowRootCommandOK(t, "workflow", "create", "Parameter Update Workflow")
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.")
	edgeID := labeledOutputValue(t, edgeOut, "edge_id")

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

	workflowOut, _ := runWorkflowRootCommandOK(t, "workflow", "create", "Parameter Clear Workflow")
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	runWorkflowRootCommandOK(t, "workflow", "node", "add", workflowID, "--key", "triaging", "--kind", "agent", "--display-name", "Triaging", "--agent", "workflow-test", "--prompt", "Triage.")
	edgeOut, _ := runWorkflowRootCommandOK(t, "workflow", "edge", "add", workflowID, "--from", "backlog", "--transition", "start", "--edge-key", "start", "--to", "triaging", "--context", "new_session", "--prompt", "Triage.", "--param", "plan_file_path=Path to the plan doc")
	edgeID := labeledOutputValue(t, edgeOut, "edge_id")

	if _, stderr, code := runWorkflowRootCommand("workflow", "edge", "update", workflowID, edgeID, "--param", "x=y", "--clear-params"); code != 2 || !strings.Contains(stderr, "not both") {
		t.Fatalf("combined --param/--clear-params exit=%d stderr=%q, want rejection", code, stderr)
	}

	runWorkflowRootCommandOK(t, "workflow", "edge", "update", workflowID, edgeID, "--clear-params")

	ctx := context.Background()
	edge := workflowCommandStoredEdgeByID(t, ctx, remote.store, workflowID, edgeID)
	if len(edge.Parameters) != 0 {
		t.Fatalf("edge parameters = %+v, want cleared", edge.Parameters)
	}
	if edge.PromptTemplate != "Triage." {
		t.Fatalf("edge prompt = %q, want preserved", edge.PromptTemplate)
	}
}

func TestTaskHumanOnlyActionsAreDeniedInsideBuilderSession(t *testing.T) {
	t.Setenv(sessionenv.BuilderSessionID, "session-agent")
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

func TestTaskSafeActionsRemainAvailableInsideBuilderSession(t *testing.T) {
	t.Setenv(sessionenv.BuilderSessionID, "session-agent")
	_, binding, remote := newWorkflowCommandLoopback(t)
	restore := replaceWorkflowCommandRemoteOpener(t, remote.cfg, remote)
	defer restore()

	workflowOut, workflowErr, code := runWorkflowRootCommand("workflow", "create", "Safe Task Workflow")
	if code != 0 {
		t.Fatalf("workflow create exit=%d stderr=%q", code, workflowErr)
	}
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
	if workflowID == "" {
		t.Fatalf("workflow create output = %q", workflowOut)
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

	taskOut, taskErr, code := runWorkflowRootCommand("task", "create", "--title", "Safe Task", "--body", "Body", "--workflow", workflowID, "--project", binding.ProjectID)
	if code != 0 {
		t.Fatalf("task create exit=%d stderr=%q", code, taskErr)
	}
	shortID := labeledOutputValue(t, taskOut, "short_id")
	if shortID == "" {
		t.Fatalf("task create output = %q", taskOut)
	}
	if _, listErr, code := runWorkflowRootCommand("task", "list", "--project", binding.ProjectID); code != 0 {
		t.Fatalf("task list exit=%d stderr=%q", code, listErr)
	}
	if _, showErr, code := runWorkflowRootCommand("task", "show", "--project", binding.ProjectID, shortID); code != 0 {
		t.Fatalf("task show exit=%d stderr=%q", code, showErr)
	}
	commentOut, commentErr, code := runWorkflowRootCommand("task", "comment", "add", "--project", binding.ProjectID, "--body", "note", shortID)
	if code != 0 {
		t.Fatalf("task comment add exit=%d stderr=%q", code, commentErr)
	}
	commentID := labeledOutputValue(t, commentOut, "comment_id")
	if commentID == "" {
		t.Fatalf("task comment add output = %q", commentOut)
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

func TestTaskListFetchesPaginatedBoardCardsWithoutDuplicates(t *testing.T) {
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
	for _, shortID := range []string{"BLD-1", "BLD-2"} {
		if got := strings.Count(stdout, shortID+"\t"); got != 1 {
			t.Fatalf("task list output = %q, want %s exactly once, got %d", stdout, shortID, got)
		}
	}
	if len(remote.requests) != 2 || remote.requests[1].PageToken != "next" {
		t.Fatalf("board requests = %+v, want initial fetch plus next page", remote.requests)
	}
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
	taskID := labeledOutputValue(t, taskOut, "task_id")
	shortID := labeledOutputValue(t, taskOut, "short_id")
	showOut, showErr, code := runWorkflowRootCommand("task", "show", "--project", binding.ProjectID, shortID)
	if code != 0 {
		t.Fatalf("task show exit=%d stderr=%q", code, showErr)
	}
	if !strings.Contains(showOut, "task_id\t"+taskID) {
		t.Fatalf("task show output = %q, want task id %s", showOut, taskID)
	}
	if strings.Contains(showOut, "[Note:") {
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
	taskID := labeledOutputValue(t, taskOut, "task_id")
	shortID := labeledOutputValue(t, taskOut, "short_id")

	showOut, showErr, code := runWorkflowRootCommand("task", "show", shortID)
	if code != 0 {
		t.Fatalf("task show from worktree root exit=%d stderr=%q", code, showErr)
	}
	if !strings.Contains(showOut, "task_id\t"+taskID) {
		t.Fatalf("task show output = %q, want task id %s", showOut, taskID)
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
	if !strings.HasPrefix(stdout, "[Note: This task belongs to another project OTH]\n") {
		t.Fatalf("task show output = %q, want cross-project note first", stdout)
	}
	if !strings.Contains(stdout, "task_id\ttask-other") {
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
	if !strings.Contains(stdout, "task_id\ttask-other") {
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
	workflowOut, workflowErr, code := runWorkflowRootCommand("workflow", "create", name)
	if code != 0 {
		t.Fatalf("workflow create exit=%d stderr=%q", code, workflowErr)
	}
	workflowID := labeledOutputValue(t, workflowOut, "workflow_id")
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

func runWorkflowRootCommandOK(t *testing.T, args ...string) (string, string) {
	t.Helper()
	stdout, stderr, code := runWorkflowRootCommand(args...)
	if code != 0 {
		t.Fatalf("%s exit=%d stdout=%q stderr=%q", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout, stderr
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

func TestWorkflowListAndResolutionFetchAllWorkflowPages(t *testing.T) {
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
	if !strings.Contains(stdout, "workflow-1\tFirst\t1") || !strings.Contains(stdout, "workflow-2\tSecond\t2") {
		t.Fatalf("workflow list output = %q, want records from both pages", stdout)
	}
	if len(remote.requests) != 2 || remote.requests[0].PageToken != "" || remote.requests[1].PageToken != "next" {
		t.Fatalf("workflow list requests = %+v, want initial request plus next page", remote.requests)
	}
	if len(remote.deadlines) != 2 || !remote.deadlines[1].After(remote.deadlines[0]) {
		t.Fatalf("workflow list deadlines = %+v, want fresh per-page RPC deadlines", remote.deadlines)
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
	if len(remote.requests) != 2 || remote.requests[1].PageToken != "next" {
		t.Fatalf("resolve requests = %+v, want all workflow pages", remote.requests)
	}
	if len(remote.deadlines) != 3 || !remote.deadlines[1].After(remote.deadlines[0]) {
		t.Fatalf("resolve deadlines = %+v, want fresh per-page list deadlines and get deadline", remote.deadlines)
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
	return r.pages[req.PageToken], nil
}

func (r *pagedWorkflowListRemote) GetWorkflow(ctx context.Context, req serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error) {
	if deadline, ok := ctx.Deadline(); ok {
		r.deadlines = append(r.deadlines, deadline)
	}
	return serverapi.WorkflowGetResponse{Definition: r.definitions[req.WorkflowID]}, nil
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
