package transport

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/server/workflowsvc"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
)

type gatewayShutdownTaskRunner struct {
	gatewayFailingRunner
	entered chan context.Context
}

func (r gatewayShutdownTaskRunner) PrepareCurrentNode(ctx context.Context, _ workflowstore.CurrentNodeStartContext, _ workflowruntime.TaskPromptDelivery) (workflowexecution.CurrentNodePreparation, error) {
	r.entered <- ctx
	<-ctx.Done()
	return workflowexecution.CurrentNodePreparation{}, context.Cause(ctx)
}

func TestGatewayCloseDetachesAcceptedPublicTaskBeforeControllerShutdown(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	defer func() { _ = appCore.Close() }()
	task := createGatewaySearchableTask(t, appCore)
	definition, err := appCore.WorkflowClient().GetWorkflow(t.Context(), &pb.GetRequest{WorkflowId: task.WorkflowId})
	if err != nil {
		t.Fatal(err)
	}
	graph := protoapi.WorkflowGraphDraftFromDefinition(definition.Definition)
	for _, node := range graph.Nodes {
		if node.Kind == pb.NodeKind_WORKFLOW_NODE_KIND_AGENT {
			node.SubagentRole = proto.String(workflow.DefaultAgentRole)
		}
	}
	saved, err := appCore.WorkflowClient().SaveWorkflowGraph(t.Context(), &pb.GraphSaveRequest{
		WorkflowId: task.WorkflowId, ExpectedVersion: definition.Definition.Workflow.Version, Graph: graph,
	})
	if err != nil || !saved.Saved {
		t.Fatalf("save executable workflow: %+v, %v", saved, err)
	}
	entered := make(chan context.Context, 1)
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	defer func() { _ = authority.Close(context.Background()) }()
	controller, err := workflowexecution.NewCurrentNodeController(
		gatewayWorkflowStore(t, appCore),
		gatewayShutdownTaskRunner{entered: entered},
		authority,
		workflowexecution.NewTaskMutationCoordinator(),
		workflowexecution.CurrentNodeControllerConfig{AgentConcurrency: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = controller.Close() }()
	service, ok := appCore.WorkflowClient().(*workflowsvc.Service)
	if !ok {
		t.Fatalf("unexpected workflow service %T", appCore.WorkflowClient())
	}
	workflowsvc.WithCurrentNodeExecution(controller)(service)
	gateway, err := NewGateway(appCore, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	sendGatewayDescriptor(t, conn, "start",
		taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Start"),
		&taskpb.StartRequest{
			TaskId: task.Id, SetupOperationId: worktreecontract.NewSetupOperationID().String(),
			ExecutionTarget: &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE},
		})
	var owned context.Context
	select {
	case owned = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("public Task Start did not reach controller preparation")
	}
	closed := make(chan struct{})
	go func() {
		gateway.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		// Release the operation so a regression does not deadlock test cleanup.
		_ = controller.Close()
		t.Fatal("Gateway waited for controller-owned work before controller shutdown")
	}
	if owned.Err() != nil {
		t.Fatalf("Gateway canceled accepted Task work: %v", owned.Err())
	}
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if owned.Err() == nil {
		t.Fatal("controller shutdown did not cancel its accepted Task work")
	}
}
