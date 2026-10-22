package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"core/server/transport"
	"core/shared/apicontract"
	"core/shared/config"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/protocol"
	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
)

type taskSetupObservationService struct {
	apicontract.WorkflowService
	subscription *taskSetupWaitingSubscription
}

type resumeNoOpService struct {
	apicontract.WorkflowService
	calls int
}

func (s *resumeNoOpService) ResumeWorkflowTask(context.Context, *taskpb.ResumeRequest) (*taskpb.ResumeSuccess, error) {
	s.calls++
	return &taskpb.ResumeSuccess{Outcome: &taskpb.ResumeSuccess_NoOp{
		NoOp: &taskpb.ResumeApplied{CurrentNodes: []*taskpb.AttentionCurrentNode{{NodeId: "already-running"}}},
	}}, nil
}

type resumeNoOpGateway struct {
	transport.GatewayDependencies
	workflows apicontract.WorkflowService
}

func (g resumeNoOpGateway) WorkflowClient() apicontract.WorkflowService { return g.workflows }

func TestTaskResumeCommandReportsNoOpWithoutRetry(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	service := fixture.core.WorkflowClient()
	created, err := service.CreateAndLinkWorkflowToProject(t.Context(), &workflowpb.CreateAndLinkProjectRequest{
		ProjectId: fixture.a.ProjectID, Name: "Resume", DefaultPolicy: workflowpb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_ALWAYS.Enum(),
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.CreateWorkflowTask(t.Context(), &taskpb.CreateRequest{
		ProjectId: fixture.a.ProjectID, WorkflowId: proto.String(created.Workflow.Id), Title: "Already resumed",
	})
	if err != nil {
		t.Fatal(err)
	}
	noOp := &resumeNoOpService{WorkflowService: service}
	gateway, err := transport.NewGateway(resumeNoOpGateway{GatewayDependencies: fixture.core, workflows: noOp}, protocol.ServerIdentity{
		ProtocolVersion: protocol.Version, ServerID: "resume-test", PID: os.Getpid(),
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
	var stdout, stderr bytes.Buffer
	code := taskResumeSubcommand([]string{task.Task.Id}, &stdout, &stderr)
	if code != 0 || noOp.calls != 1 {
		t.Fatalf("Resume no-op: exit=%d calls=%d stdout=%q stderr=%q", code, noOp.calls, stdout.String(), stderr.String())
	}
}

func (s taskSetupObservationService) SubscribeWorktreeSetup(context.Context, *worktreepb.SetupSubscribeRequest) (apicontract.WorktreeSetupSubscription, error) {
	return s.subscription, nil
}

type taskSetupWaitingSubscription struct {
	closed chan struct{}
}

func (*taskSetupWaitingSubscription) Next(ctx context.Context) (*worktreepb.SetupEvent, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *taskSetupWaitingSubscription) Close() error {
	close(s.closed)
	return nil
}

func TestTaskSetupObservationBoundsPreparationRequest(t *testing.T) {
	subscription := &taskSetupWaitingSubscription{closed: make(chan struct{})}
	service := taskSetupObservationService{subscription: subscription}
	_, _, err := runWorkflowMutationWithSetupProgress(
		t.Context(), service, io.Discard,
		func(ctx context.Context, _ worktreecontract.SetupOperationID) (bool, error) {
			deadline, present := ctx.Deadline()
			if !present || time.Until(deadline) > workflowTaskSetupObservationTimeout {
				t.Error("preparation request has no bounded observation deadline")
			}
			return false, nil
		},
		func(applied bool) bool { return applied },
	)
	if err != nil {
		t.Fatal(err)
	}
	<-subscription.closed
}

func TestTaskSetupObservationCancellationDoesNotReplayMutation(t *testing.T) {
	subscription := &taskSetupWaitingSubscription{closed: make(chan struct{})}
	service := taskSetupObservationService{subscription: subscription}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	_, _, err := runWorkflowMutationWithSetupProgress(
		ctx, service, io.Discard,
		func(ctx context.Context, _ worktreecontract.SetupOperationID) (bool, error) {
			calls++
			cancel()
			<-ctx.Done()
			return false, ctx.Err()
		},
		func(applied bool) bool { return applied },
	)
	var observationErr *worktreeSetupObservationError
	if !errors.As(err, &observationErr) || !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("observation cancellation: calls=%d err=%v", calls, err)
	}
	<-subscription.closed
}
