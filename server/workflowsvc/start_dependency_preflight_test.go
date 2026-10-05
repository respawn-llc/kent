package workflowsvc

import (
	"context"
	"core/shared/protoapi"
	"errors"
	"testing"

	"core/server/workflow"
	"core/server/workflowstore"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
)

type countingTaskDependencyReadModel struct {
	count              int
	unsatisfiedBlocker *int
}

func (r *countingTaskDependencyReadModel) GetTaskDependencies(context.Context, string) (*taskpb.TaskDependencies, error) {
	r.count++
	return &taskpb.TaskDependencies{}, errors.New("unexpected dependency projection")
}

func (r *countingTaskDependencyReadModel) CountUnsatisfiedBlockers(context.Context, string) (int, error) {
	r.count++
	if r.unsatisfiedBlocker != nil {
		return *r.unsatisfiedBlocker, nil
	}
	return 0, errors.New("unexpected dependency count")
}

func (r *countingTaskDependencyReadModel) ListTaskDependencies(context.Context, string, *taskpb.DependencyDirection) (*taskpb.DependencyListSuccess, error) {
	r.count++
	return &taskpb.DependencyListSuccess{}, errors.New("unexpected dependency list")
}

func TestStartDependencyPreflightWarnsBeforeExecutionTargetWorkAndProceedSkipsRecheck(t *testing.T) {
	ctx, service, projectID, workflowID, _ := newWorkflowServiceOrdinaryTaskFixture(t)
	blocker, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId: projectID, WorkflowId: proto.String(workflowID.String()), Title: "blocker", LabelIds: []string{},
	})
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	blocked, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{
		ProjectId: projectID, WorkflowId: proto.String(workflowID.String()), Title: "blocked", LabelIds: []string{},
	})
	if err != nil {
		t.Fatalf("create blocked: %v", err)
	}
	if _, err := service.AddWorkflowTaskDependency(ctx, &taskpb.DependencyAddRequest{
		BlockerTaskId: blocker.Task.Id, BlockedTaskId: blocked.Task.Id,
	}); err != nil {
		t.Fatalf("add dependency: %v", err)
	}
	targets := &recordingExecutionTargetInfrastructure{}
	service.executionTargets = targets
	branchName := "feature/dependency-warning"

	warning, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:           blocked.Task.Id,
		SetupOperationId: worktreecontract.NewSetupOperationID().String(),
		BranchName:       &branchName,
	})
	if err != nil {
		t.Fatalf("start warning: %v", err)
	}
	if warning.GetDependencyConfirmationRequired() == nil ||
		warning.GetDependencyConfirmationRequired().UnsatisfiedDependencyCount != 1 {
		t.Fatalf("warning = %+v", warning)
	}
	if err := protoapi.Validate(warning); err != nil {
		t.Fatalf("warning Validate: %v", err)
	}
	if targets.resolveSelection != (workflow.ExecutionTargetSelection{}) || targets.materializeTaskID != "" || targets.restoreTaskID != "" {
		t.Fatalf("target infrastructure used during warning: %+v", targets)
	}
	targetContext, err := service.store.GetTaskExecutionTargetContext(ctx, workflow.TaskID(blocked.Task.Id))
	if err != nil {
		t.Fatalf("GetTaskExecutionTargetContext: %v", err)
	}
	if targetContext.Task.PendingInitialManagedBranchName == nil ||
		*targetContext.Task.PendingInitialManagedBranchName != blocked.Task.ShortId {
		t.Fatalf("pending branch after dependency warning = %v, want unchanged %q", targetContext.Task.PendingInitialManagedBranchName, blocked.Task.ShortId)
	}

	proceeded, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:                     blocked.Task.Id,
		SetupOperationId:           worktreecontract.NewSetupOperationID().String(),
		ProceedDespiteDependencies: true,
	})
	if err != nil {
		t.Fatalf("proceeded start: %v", err)
	}
	if proceeded.GetDependencyConfirmationRequired() != nil {
		t.Fatalf("proceeded start returned dependency warning: %+v", proceeded)
	}
}

func TestStartRequestValidationReturnsBeforeDependencyReader(t *testing.T) {
	ctx, service, _, _, _ := newWorkflowServiceOrdinaryTaskFixture(t)
	reader := &countingTaskDependencyReadModel{}
	service.readModels.TaskDependencies = reader
	if _, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{}); err == nil {
		t.Fatal("invalid start request returned nil error")
	}
	if reader.count != 0 {
		t.Fatalf("dependency reader count = %d, want 0", reader.count)
	}
}

func TestStartDependencyPreflightReturnsBeforeTargetInfrastructure(t *testing.T) {
	ctx, service, _, _, taskID := newWorkflowServiceOrdinaryTaskFixture(t)
	unsatisfied := 1
	reader := &countingTaskDependencyReadModel{unsatisfiedBlocker: &unsatisfied}
	service.readModels.TaskDependencies = reader
	service.executionTargets = nil

	_, err := service.StartWorkflowTask(ctx, &taskpb.StartRequest{
		TaskId:           taskID,
		SetupOperationId: worktreecontract.NewSetupOperationID().String(),
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD,
		},
	})
	if err != nil {
		t.Fatalf("StartWorkflowTask: %v", err)
	}
	if reader.count != 1 {
		t.Fatalf("dependency reader count = %d, want 1", reader.count)
	}
}

func TestExecutableMoveWithProceedSkipsDependencyReaderBeforeTargetInfrastructurePreflight(t *testing.T) {
	ctx, service, binding := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceChainedWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	definition, err := service.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: workflowID.String()})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	reader := &countingTaskDependencyReadModel{}
	service.readModels.TaskDependencies = reader
	service.currentNodeExecution = newManualMoveExecutionStub(service)
	service.executionTargets = nil

	_, err = service.MoveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:                     task.Task.Id,
		TargetNodeId:               workflowServiceNodeIDByKey(t, definition.Definition, "plan"),
		ProceedDespiteDependencies: true,
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD,
		},
	})
	if !errors.Is(err, errExecutionTargetInfrastructureRequired) {
		t.Fatalf("MoveWorkflowTask error = %v, want required target infrastructure", err)
	}
	if reader.count != 0 {
		t.Fatalf("dependency reader count = %d, want 0", reader.count)
	}
}

func TestExecutableMoveDependencyPreflightRequiresExplicitProceed(t *testing.T) {
	ctx, service, binding := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceChainedWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	blocker := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	blocked := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	if _, err := service.AddWorkflowTaskDependency(ctx, &taskpb.DependencyAddRequest{
		BlockerTaskId: blocker.Task.Id,
		BlockedTaskId: blocked.Task.Id,
	}); err != nil {
		t.Fatalf("AddWorkflowTaskDependency: %v", err)
	}
	definition, err := service.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: workflowID.String()})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	targetNodeID := workflowServiceNodeIDByKey(t, definition.Definition, "plan")
	execution := newManualMoveExecutionStub(service)
	service.currentNodeExecution = execution
	targets := &recordingExecutionTargetInfrastructure{}
	service.executionTargets = targets

	warning, err := service.MoveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:       blocked.Task.Id,
		TargetNodeId: targetNodeID,
	})
	if err != nil {
		t.Fatalf("MoveWorkflowTask warning: %v", err)
	}
	if warning.GetDependencyConfirmationRequired() == nil ||
		warning.GetDependencyConfirmationRequired().UnsatisfiedDependencyCount != 1 {
		t.Fatalf("warning = %+v", warning)
	}
	if err := protoapi.Validate(warning); err != nil {
		t.Fatalf("warning Validate: %v", err)
	}
	if len(execution.interruptTaskIDs) != 0 || len(execution.started) != 0 {
		t.Fatalf("execution used during dependency warning: interrupts=%v starts=%v", execution.interruptTaskIDs, execution.started)
	}
	if targets.resolveSelection != (workflow.ExecutionTargetSelection{}) || targets.materializeTaskID != "" {
		t.Fatalf("target infrastructure used during dependency warning: %+v", targets)
	}

	proceeded, err := service.MoveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:                     blocked.Task.Id,
		TargetNodeId:               targetNodeID,
		ExecutionTarget:            &taskpb.ExecutionTargetSelection{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE},
		ProceedDespiteDependencies: true,
	})
	if err != nil {
		t.Fatalf("MoveWorkflowTask proceeded: %v", err)
	}
	if proceeded.GetApplied() == nil ||
		proceeded.GetApplied() == nil || len(proceeded.GetApplied().CurrentNodes) != 1 {
		t.Fatalf("proceeded response = %+v", proceeded)
	}
	if proceeded.GetDependencyConfirmationRequired() != nil {
		t.Fatalf("proceeded response retained dependency count: %+v", proceeded)
	}
}

func TestExecutableMoveWithProceedSkipsDependencyReaderBeforeTargetCompatibilityPreflight(t *testing.T) {
	ctx, service, binding := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceChainedWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	startWorkflowServiceTask(t, ctx, service, task.Task.Id)
	definition, err := service.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: workflowID.String()})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	reader := &countingTaskDependencyReadModel{}
	service.readModels.TaskDependencies = reader
	service.currentNodeExecution = newManualMoveExecutionStub(service)

	_, err = service.MoveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:                     task.Task.Id,
		TargetNodeId:               workflowServiceNodeIDByKey(t, definition.Definition, "implement"),
		ProceedDespiteDependencies: true,
		Values:                     []*taskpb.NodeOutputValues{{NodeKey: "plan", Outputs: []*taskpb.NamedValue{{Name: "prior_summary", Value: "manual plan"}}}},
		ExecutionTarget: &taskpb.ExecutionTargetSelection{
			Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE,
		},
	})
	if !errors.Is(err, workflowstore.ErrExecutionTargetAlreadyLocked) {
		t.Fatalf("MoveWorkflowTask error = %v, want locked-target compatibility error", err)
	}
	if reader.count != 0 {
		t.Fatalf("dependency reader count = %d, want 0", reader.count)
	}
}
