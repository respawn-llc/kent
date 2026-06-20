package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func (r *workflowCommandLoopbackRemote) ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	if binding, ok := r.projectBindingsByRoot[req.Path]; ok {
		return serverapi.ProjectResolvePathResponse{CanonicalRoot: req.Path, Binding: &binding}, nil
	}
	if req.Path != r.cfg.WorkspaceRoot {
		return serverapi.ProjectResolvePathResponse{}, nil
	}
	return serverapi.ProjectResolvePathResponse{Binding: &serverapi.ProjectBinding{ProjectID: r.binding.ProjectID, WorkspaceID: r.binding.WorkspaceID, CanonicalRoot: r.cfg.WorkspaceRoot}}, nil
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

	stdout, stderr, code := runWorkflowRootCommand("workflow", "list", "--json")
	if code != 0 {
		t.Fatalf("workflow list exit=%d stderr=%q", code, stderr)
	}
	var listed workflowListOutput
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("workflow list --json = %q, want JSON: %v", stdout, err)
	}
	if len(listed.Workflows) != 1 || listed.Workflows[0].ID != "workflow-1" {
		t.Fatalf("workflow list --json workflows = %+v, want only first-page workflow-1", listed.Workflows)
	}
	if listed.NextPageToken != "next" {
		t.Fatalf("workflow list --json next page token = %q, want %q", listed.NextPageToken, "next")
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

