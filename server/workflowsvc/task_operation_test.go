package workflowsvc

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"

	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
)

type blockingTaskDetail struct {
	WorkflowTaskDetailReadModel
	entered chan context.Context
	release <-chan struct{}
	exited  chan error
}

func (r blockingTaskDetail) GetTask(ctx context.Context, taskID string) (*taskpb.TaskDetail, error) {
	r.entered <- ctx
	<-r.release
	result, err := r.WorkflowTaskDetailReadModel.GetTask(ctx, taskID)
	r.exited <- err
	return result, err
}

type blockingTaskCleanup struct {
	entered chan context.Context
	release <-chan struct{}
}

func (blockingTaskCleanup) EnsureTaskWorktreeDeletable(context.Context, string) error {
	return nil
}

func (c blockingTaskCleanup) DeleteTaskWorktree(ctx context.Context, _ string) (*worktreepb.BranchCleanupOutcome, error) {
	c.entered <- ctx
	<-c.release
	return nil, ctx.Err()
}

func ownServiceTaskOperations(t *testing.T, service *Service) {
	t.Helper()
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller, err := workflowexecution.NewCurrentNodeController(
		service.store, pendingAgentControllerRunner{}, authority, service.taskMutations,
		workflowexecution.CurrentNodeControllerConfig{AgentConcurrency: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Error(err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	service.currentNodeExecution = controller
}

func TestTaskUpdateAndDeleteDetachObservationWithoutCancelingAcceptedWork(t *testing.T) {
	for _, operation := range []string{"update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			ctx, service, binding := newWorkflowServiceTestContext(t)
			workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
			linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
			task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
			ownServiceTaskOperations(t, service)
			entered, release := make(chan context.Context, 1), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			detail := service.readModels.TaskDetail
			projected := make(chan error, 1)
			if operation == "update" {
				service.readModels.TaskDetail = blockingTaskDetail{
					WorkflowTaskDetailReadModel: detail, entered: entered, release: release, exited: projected,
				}
			} else {
				service.taskWorktreeCleanup = blockingTaskCleanup{entered: entered, release: release}
			}
			caller, cancel := context.WithCancel(ctx)
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				var err error
				if operation == "update" {
					_, err = service.UpdateWorkflowTask(caller, &taskpb.UpdateRequest{TaskId: task.Task.Id, Title: proto.String("Updated")})
				} else {
					_, err = service.DeleteWorkflowTask(caller, &taskpb.DeleteRequest{TaskId: task.Task.Id})
				}
				finished <- err
			}()
			var owned context.Context
			select {
			case owned = <-entered:
			case err := <-finished:
				t.Fatalf("operation returned before dependency: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("operation did not reach dependency")
			}
			cancel()
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("observer = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled observer remained attached")
			}
			if owned.Err() != nil {
				t.Fatalf("caller canceled accepted work: %v", owned.Err())
			}
			unblock()
			if operation == "update" {
				select {
				case err := <-projected:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("accepted Update did not project its committed result")
				}
				after, err := detail.GetTask(ctx, task.Task.Id)
				if err != nil || after.Summary.Title != "Updated" {
					t.Fatalf("accepted update = %+v, %v", after, err)
				}
			} else {
				err := service.taskMutations.Run(ctx, workflow.TaskID(task.Task.Id), func(ctx context.Context) error {
					_, err := detail.GetTask(ctx, task.Task.Id)
					return err
				})
				if !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("accepted Delete did not finish after cleanup: %v", err)
				}
			}
		})
	}
}

func admitWorkflowServiceTask(ctx context.Context, service *Service, taskID workflow.TaskID) (workflowstore.StartTaskResult, error) {
	target, err := service.store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		return workflowstore.StartTaskResult{}, err
	}
	return service.currentNodeExecution.StartTask(ctx, taskID, &workflowstore.ExecutionTargetCandidate{
		Snapshot: workflowstore.ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeNone, Provenance: workflowstore.ExecutionTargetProvenanceResolved},
		Root:     workflowstore.ExecutionRoot{SourceWorkspaceID: target.SourceWorkspaceID, SourceWorkspaceRoot: target.SourceWorkspaceRoot},
	})
}

func TestServiceAcceptedStartPreparesBeforeCutoverAndSurvivesClientCancellation(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	other := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	taskID := workflow.TaskID(task.Task.Id)
	before, err := service.store.ListCurrentNodes(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller, err := workflowexecution.NewCurrentNodeController(
		service.store,
		pendingAgentControllerRunner{prepare: workflowServiceTestManualMoveAssignments(t, metadataStore)},
		authority, service.taskMutations,
		workflowexecution.CurrentNodeControllerConfig{AgentConcurrency: 2},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Error(err)
		}
		if err := authority.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	service.currentNodeExecution = controller
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseSetup := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseSetup)
	ref, oid := "HEAD", strings.Repeat("a", 40)
	root, worktreeID := filepath.Join(t.TempDir(), "managed"), "worktree-"+task.Task.Id
	targets := &recordingExecutionTargetInfrastructure{
		resolution: workflowstore.ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeHead, RequestedRef: &ref, CommitOID: &oid, Provenance: workflowstore.ExecutionTargetProvenanceResolved},
		materialize: func(id workflow.TaskID) (ExecutionTargetMaterialization, error) {
			close(entered)
			<-release
			bindWorkflowServiceManagedWorktree(t, ctx, metadataStore, binding.WorkspaceID, id, worktreeID, root, false)
			return ExecutionTargetMaterialization{RetainedRoot: &workflowstore.ManagedExecutionRoot{WorktreeID: worktreeID, Root: root}}, nil
		},
	}
	service.executionTargets = targets
	clientCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		response *taskpb.StartSuccess
		err      error
	}
	finished := make(chan result, 1)
	go func() {
		response, err := service.StartWorkflowTask(clientCtx, &taskpb.StartRequest{
			TaskId: task.Task.Id, SetupOperationId: worktreecontract.NewSetupOperationID().String(),
			ExecutionTarget: &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD},
		})
		finished <- result{response: response, err: err}
	}()
	select {
	case <-entered:
	case got := <-finished:
		t.Fatalf("Start returned before setup: %+v", got)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not reach setup")
	}
	cancel()
	select {
	case got := <-finished:
		if !errors.Is(got.err, context.Canceled) || got.response != nil {
			t.Fatalf("canceled observer = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled Start observer remained attached to accepted setup")
	}
	duplicateFinished := make(chan result, 1)
	go func() {
		response, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
			TaskId: task.Task.Id, SetupOperationId: worktreecontract.NewSetupOperationID().String(),
			ExecutionTarget: &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD},
		})
		duplicateFinished <- result{response: response, err: err}
	}()
	during, err := service.store.ListCurrentNodes(ctx, taskID)
	if err != nil || !reflect.DeepEqual(before, during) {
		t.Fatalf("setup published executable Current Nodes: %+v, %v", during, err)
	}
	target, err := service.store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil || target.Task.ExecutionTarget != nil {
		t.Fatalf("setup committed target: %+v, %v", target, err)
	}
	otherFinished := make(chan result, 1)
	go func() {
		response, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
			TaskId: other.Task.Id, SetupOperationId: worktreecontract.NewSetupOperationID().String(),
			ExecutionTarget: &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE},
		})
		otherFinished <- result{response: response, err: err}
	}()
	select {
	case got := <-otherFinished:
		if got.err != nil || got.response.GetApplied() == nil {
			t.Fatalf("unrelated Start failed: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked setup prevented unrelated Task progress")
	}
	releaseSetup()
	select {
	case got := <-duplicateFinished:
		if got.err != nil || got.response.GetApplied() == nil {
			t.Fatalf("duplicate Start did not succeed: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate Start did not complete")
	}
	after, err := service.store.ListCurrentNodes(ctx, taskID)
	if err != nil || len(after) != 1 || after[0].SessionID == nil || after[0].Scheduling == nil {
		t.Fatalf("cutover did not publish exact execution: %+v, %v", after, err)
	}
	afterDuplicate, err := service.store.ListCurrentNodes(ctx, taskID)
	if err != nil || !reflect.DeepEqual(after, afterDuplicate) {
		t.Fatalf("duplicate Start changed Current Nodes: %+v, %v", afterDuplicate, err)
	}
	if len(targets.setupRequirements) != 1 {
		t.Fatalf("accepted Start ran setup %d times", len(targets.setupRequirements))
	}
}
