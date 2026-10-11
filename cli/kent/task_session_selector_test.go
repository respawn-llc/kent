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
}
