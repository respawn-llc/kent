package workflowsvc

import (
	"context"
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

	"core/shared/worktreecontract"
)

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
	case got := <-finished:
		if got.err != nil || got.response.GetApplied() == nil {
			t.Fatalf("client cancellation prevented accepted Start: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("accepted Start did not complete after setup")
	}
	after, err := service.store.ListCurrentNodes(ctx, taskID)
	if err != nil || len(after) != 1 || after[0].SessionID == nil || after[0].Scheduling == nil {
		t.Fatalf("cutover did not publish exact execution: %+v, %v", after, err)
	}
	select {
	case got := <-duplicateFinished:
		if got.err != nil || got.response.GetApplied() == nil {
			t.Fatalf("duplicate Start did not succeed: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("duplicate Start did not complete")
	}
	afterDuplicate, err := service.store.ListCurrentNodes(ctx, taskID)
	if err != nil || !reflect.DeepEqual(after, afterDuplicate) {
		t.Fatalf("duplicate Start changed Current Nodes: %+v, %v", afterDuplicate, err)
	}
	if len(targets.setupRequirements) != 1 {
		t.Fatalf("accepted Start ran setup %d times", len(targets.setupRequirements))
	}
}
