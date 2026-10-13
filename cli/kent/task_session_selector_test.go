package main

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"os"
	"testing"

	"core/server/transport"
	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/protocol"
	"core/shared/runtimeids"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type taskSelectorCapture struct {
	apicontract.WorkflowService
	target *sharedpb.SessionSelector
}

func (s *taskSelectorCapture) InterruptWorkflowTask(ctx context.Context, req *taskpb.InterruptRequest) (*emptypb.Empty, error) {
	s.target = req.SessionId
	return s.WorkflowService.InterruptWorkflowTask(ctx, req)
}

func (s *taskSelectorCapture) CompleteWorkflowTask(ctx context.Context, req *taskpb.CompleteRequest) (*taskpb.CompleteSuccess, error) {
	s.target = req.SessionId
	return s.WorkflowService.CompleteWorkflowTask(ctx, req)
}

func TestWorkflowCommandsSubmitNormalizedSessionSelectors(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	service := fixture.core.WorkflowClient()
	created, err := service.CreateAndLinkWorkflowToProject(t.Context(), &workflowpb.CreateAndLinkProjectRequest{
		ProjectId: fixture.a.ProjectID, Name: "Selectors", DefaultPolicy: workflowpb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_ALWAYS.Enum(),
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.CreateWorkflowTask(t.Context(), &taskpb.CreateRequest{
		ProjectId: fixture.a.ProjectID, WorkflowId: proto.String(created.Workflow.Id), Title: "Selector target",
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := fixture.core.MetadataStore().ResolvePersistedSession(t.Context(), fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	capture := &taskSelectorCapture{WorkflowService: service}
	gateway, err := transport.NewGateway(resumeNoOpGateway{GatewayDependencies: fixture.core, workflows: capture}, protocol.ServerIdentity{
		ProtocolVersion: protocol.Version, ServerID: "selector-test", PID: os.Getpid(),
		PersistenceRootID: config.PersistenceRootHash(fixture.core.Config().PersistenceRoot),
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.Handler())
	t.Cleanup(server.Close)
	host, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KENT_SERVER_HOST", host)
	t.Setenv("KENT_SERVER_PORT", port)
	for _, input := range []string{fixture.sessionID, record.ShortID.String(), "000" + record.ShortID.String(), "9007199254740991"} {
		for _, command := range []string{"interrupt", "complete"} {
			t.Run(command+"/"+input, func(t *testing.T) {
				capture.target = nil
				var stdout, stderr bytes.Buffer
				var code int
				if command == "interrupt" {
					code = taskInterruptSubcommand([]string{task.Task.Id, "--session", input}, &stdout, &stderr)
				} else {
					code = taskCompleteSubcommand([]string{"--force", "--session", input}, &stdout, &stderr)
				}
				// This Session has no Workflow execution or Task association.
				if code != 1 || capture.target == nil {
					t.Fatalf("exit %d, target %+v, stderr %s", code, capture.target, stderr.String())
				}
				target, err := protoapi.SessionSelectorFromProto(capture.target)
				if err != nil {
					t.Fatal(err)
				}
				expected, err := runtimeids.ParseSessionSelector(input)
				if err != nil {
					t.Fatal(err)
				}
				if target != expected {
					t.Fatalf("target %s, expected %s", target, expected)
				}
			})
		}
	}
	for _, args := range [][]string{
		{"--force"},
		{"--force", "--session", record.ShortID.String(), "--task", task.Task.Id},
	} {
		capture.target = nil
		var stdout, stderr bytes.Buffer
		if code := taskCompleteSubcommand(args, &stdout, &stderr); code != 2 || capture.target != nil {
			t.Fatalf("invalid completion selectors: exit %d, target %+v, stderr %s", code, capture.target, stderr.String())
		}
	}
	definition, err := service.GetWorkflow(t.Context(), &workflowpb.GetRequest{WorkflowId: created.Workflow.Id})
	if err != nil {
		t.Fatal(err)
	}
	graph := protoapi.WorkflowGraphDraftFromDefinition(definition.Definition)
	var startID, terminalID string
	for _, node := range graph.Nodes {
		switch node.Kind {
		case workflowpb.NodeKind_WORKFLOW_NODE_KIND_START:
			startID = node.Id
		case workflowpb.NodeKind_WORKFLOW_NODE_KIND_TERMINAL:
			terminalID = node.Id
		}
	}
	agentID, groupID, startGroupID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	graph.Nodes = append(graph.Nodes, &workflowpb.GraphDraftNode{Id: agentID, Key: "agent", Kind: workflowpb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "Agent", SubagentRole: proto.String(config.DefaultSubagentRole)})
	graph.TransitionGroups = append(graph.TransitionGroups, &workflowpb.GraphDraftTransitionGroup{Id: startGroupID, SourceNodeId: startID, TransitionId: "start", DisplayName: "Start"})
	graph.TransitionGroups = append(graph.TransitionGroups, &workflowpb.GraphDraftTransitionGroup{
		Id: groupID, SourceNodeId: agentID, TransitionId: "done", DisplayName: "Done",
	})
	graph.Edges = append(graph.Edges, &workflowpb.GraphDraftEdge{
		Id: uuid.NewString(), TransitionGroupId: startGroupID, Key: "start", TargetNodeId: agentID,
		AssigneeSelection: workflowpb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
		ThinkingSelection: workflowpb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED,
		ContextMode:       workflowpb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION, PromptTemplate: "Work",
		ContextSource: &workflowpb.ContextSource{Kind: workflowpb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE},
	})
	graph.Edges = append(graph.Edges, &workflowpb.GraphDraftEdge{
		Id: uuid.NewString(), TransitionGroupId: groupID, Key: "done", TargetNodeId: terminalID,
		AssigneeSelection: workflowpb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
		ThinkingSelection: workflowpb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED,
		ContextMode:       workflowpb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION,
		ContextSource:     &workflowpb.ContextSource{Kind: workflowpb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE},
	})
	saved, err := service.SaveWorkflowGraph(t.Context(), &workflowpb.GraphSaveRequest{
		WorkflowId: created.Workflow.Id, ExpectedVersion: definition.Definition.Workflow.Version, Graph: graph,
	})
	if err != nil || !saved.Saved {
		t.Fatalf("completion fixture graph: %v, %v", saved, err)
	}
	for _, result := range saved.ValidationResults {
		if !result.Result.Valid {
			t.Fatalf("completion fixture validation: %v", result)
		}
	}
	if _, err := fixture.core.MetadataStore().DB().ExecContext(t.Context(), "UPDATE sessions SET task_id = ? WHERE id = ?", task.Task.Id, fixture.sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.core.MetadataStore().DB().ExecContext(t.Context(), "UPDATE task_current_nodes SET node_id = ?, effective_assignee = ?, assignee_origin = 'configured_fallback' WHERE task_id = ?", agentID, config.DefaultSubagentRole, task.Task.Id); err != nil {
		t.Fatal(err)
	}
	for _, selectorArgs := range [][]string{
		{"--session", " " + record.ShortID.String() + " "},
		{"--session= " + record.ShortID.String() + " "},
	} {
		capture.target = nil
		var stdout, stderr bytes.Buffer
		args := append([]string{"--force", "--transition", "done"}, selectorArgs...)
		if code := taskCompleteSubcommand(args, &stdout, &stderr); code == 0 || capture.target != nil {
			t.Fatalf("padded completion selector reached eligible Task: exit %d, target %v, stderr %s", code, capture.target, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := taskCompleteSubcommand([]string{"--force", "--transition", "done", "--session", "000" + record.ShortID.String()}, &stdout, &stderr); code != 0 {
		t.Fatalf("valid completion after rejected whitespace: exit %d, stderr %s", code, stderr.String())
	}
}
