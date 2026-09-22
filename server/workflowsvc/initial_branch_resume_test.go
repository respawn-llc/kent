package workflowsvc

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/serverapi"
)

func TestServiceTaskResumeEligibilityRejectsExplicitBranchBeforePendingMutation(t *testing.T) {
	ctx, service, binding := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	setWorkflowServiceExecutionTargetPolicy(t, ctx, service, workflowID, &pb.ExecutionTargetConfiguration{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD})
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	taskID := workflow.TaskID(task.Task.ID)
	authority := sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{})
	controller, err := workflowexecution.NewCurrentNodeController(service.store, initialBranchControllerRunner{}, authority, service.taskMutations, workflowexecution.CurrentNodeControllerConfig{
		AgentConcurrency: 1,
	})
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
	targets := &recordingExecutionTargetInfrastructure{}
	service.executionTargets = targets
	branchName := "feature/ineligible-resume"
	_, err = service.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{
		TaskID: task.Task.ID, SetupOperationID: serverapi.NewWorkflowSetupOperationID(), BranchName: &branchName,
	})
	var conflict *workflowexecution.TaskResumeConflictError
	if !errors.As(err, &conflict) || conflict.TaskID != taskID {
		t.Fatalf("ResumeWorkflowTask error = %T %v, want typed conflict", err, err)
	}
	target, err := service.store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Task.PendingInitialManagedBranchName == nil || *target.Task.PendingInitialManagedBranchName != task.Task.ShortID {
		t.Fatalf("ineligible Resume changed pending branch: %+v", target.Task)
	}
	if targets.initialBranchInspections != 0 || targets.materializeRequest.TaskID != "" {
		t.Fatalf("Resume reached target preparation before eligibility: %+v", targets)
	}
}

func TestServiceTaskResumeRequiresSelectionForMissingLockedWorktree(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	taskID := workflow.TaskID(task.Task.ID)
	worktreeID := "worktree-" + task.Task.ID
	root := filepath.Join(t.TempDir(), "task-worktree")
	bindWorkflowServiceManagedWorktree(t, ctx, metadataStore, binding.WorkspaceID, taskID, worktreeID, root, false)
	target, err := service.store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	ref, oid := "HEAD", strings.Repeat("8", 40)
	started, err := service.currentNodeExecution.StartTask(ctx, taskID, &workflowstore.ExecutionTargetCandidate{
		Snapshot: workflowstore.ExecutionTargetSnapshot{Mode: workflow.ExecutionTargetModeHead, RequestedRef: &ref, CommitOID: &oid, Provenance: workflowstore.ExecutionTargetProvenanceResolved},
		Root:     workflowstore.ExecutionRoot{SourceWorkspaceID: target.SourceWorkspaceID, SourceWorkspaceRoot: target.SourceWorkspaceRoot, Managed: &workflowstore.ManagedExecutionRoot{WorktreeID: worktreeID, Root: root}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.InterruptCurrentNode(ctx, started.Mutation.Created[0].Reference, workflow.CurrentNodeInterruptionReasonUserInterrupt, workflow.NewCurrentNodeInterruptionDetail(string(workflow.CurrentNodeInterruptionReasonUserInterrupt), nil)); err != nil {
		t.Fatal(err)
	}
	requestedBranch, existingBranch := "feature/attempted-rename", task.Task.ShortID
	expected := &serverapi.WorkflowTaskInitialBranchError{
		Reason:     serverapi.WorkflowTaskInitialBranchErrorReasonPostCreationMismatch,
		BranchName: requestedBranch, ExistingBranchName: &existingBranch,
	}
	targets := &recordingExecutionTargetInfrastructure{initialBranchAssertionErr: expected}
	service.executionTargets = targets
	response, err := service.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{
		TaskID: task.Task.ID, SetupOperationID: serverapi.NewWorkflowSetupOperationID(), BranchName: &requestedBranch,
	})
	if !errors.Is(err, expected) || response.Applied != nil {
		t.Fatalf("Resume branch mismatch = %+v, %v", response, err)
	}
	if targets.restoreTaskID != "" {
		t.Fatalf("Resume restored target before validating branch: %+v", targets)
	}
	targets.initialBranchAssertionErr = nil
	targets.restoreErr = &serverapi.WorkflowLockedExecutionTargetError{Cause: serverapi.WorkflowLockedExecutionTargetCauseMissingBranch}
	response, err = service.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{
		TaskID: task.Task.ID, SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
	})
	if err != nil || response.SelectionRequired == nil || response.SelectionRequired.Details.GetOriginalTargetUnavailable() == nil || response.Applied != nil {
		t.Fatalf("Resume = %+v, %v; want original target selection", response, err)
	}
	nodes, err := service.store.ListCurrentNodes(ctx, taskID)
	if err != nil || len(nodes) != 1 || nodes[0].Scheduling == nil || nodes[0].Scheduling.State != workflow.CurrentNodeSchedulingInterrupted {
		t.Fatalf("failed Resume changed interrupted execution: %+v, %v", nodes, err)
	}
}
