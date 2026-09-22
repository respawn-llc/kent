package workflowsvc

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	"core/server/workflow"
	"core/server/workflowstore"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/serverapi"
)

func TestServiceTaskStartMaterializesLatestTaskScopedPendingBranch(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	setWorkflowServiceExecutionTargetPolicy(t, ctx, service, workflowID, &pb.ExecutionTargetConfiguration{
		Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD,
	})
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	task := createDefaultWorkflowServiceTask(t, ctx, service, binding.ProjectID)
	requestedRef := "HEAD"
	commitOID := strings.Repeat("c", 40)
	branchA := "feature/request-a"
	branchB := "feature/request-b"
	materializedBranch := ""
	worktreeID := "worktree-" + task.Task.ID
	worktreeRoot := filepath.Join(t.TempDir(), "task-worktree")
	targets := &recordingExecutionTargetInfrastructure{
		resolution: workflowstore.ExecutionTargetSnapshot{
			Mode: workflow.ExecutionTargetModeHead, RequestedRef: &requestedRef,
			CommitOID: &commitOID, Provenance: workflowstore.ExecutionTargetProvenanceResolved,
		},
	}
	targets.materialize = func(taskID workflow.TaskID) (ExecutionTargetMaterialization, error) {
		targetContext, err := service.store.GetTaskExecutionTargetContext(ctx, taskID)
		if err != nil {
			return ExecutionTargetMaterialization{}, err
		}
		materializedBranch = *targetContext.Task.PendingInitialManagedBranchName
		if err := metadataStore.UpsertWorktreeRecord(ctx, metadata.WorktreeRecord{
			ID: worktreeID, WorkspaceID: binding.WorkspaceID,
			CanonicalRoot: worktreeRoot, Managed: true, CreatedBranch: true,
		}); err != nil {
			return ExecutionTargetMaterialization{}, err
		}
		updated, err := metadataStore.Queries().BindInitialTaskManagedWorktree(ctx, sqlitegen.BindInitialTaskManagedWorktreeParams{
			ManagedWorktreeID: sql.NullString{String: worktreeID, Valid: true},
			UpdatedAtUnixMs:   time.Now().UTC().UnixMilli(), TaskID: string(taskID),
		})
		if err != nil || updated != 1 {
			return ExecutionTargetMaterialization{}, errors.Join(err, errors.New("initial Worktree bind failed"))
		}
		root := workflowstore.ManagedExecutionRoot{WorktreeID: worktreeID, Root: worktreeRoot}
		return ExecutionTargetMaterialization{RetainedRoot: &root}, nil
	}
	service.executionTargets = targets

	if _, err := service.preflightInitiatingActionTarget(ctx, workflow.TaskID(task.Task.ID), nil, &branchA); err != nil {
		t.Fatalf("initial branch preflight: %v", err)
	}
	response, err := service.StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{
		SetupOperationID: serverapi.NewWorkflowSetupOperationID(), TaskID: task.Task.ID, BranchName: &branchB,
	})
	if err != nil || response.Applied == nil {
		t.Fatalf("StartWorkflowTask = %+v, %v; want placed task", response, err)
	}
	if materializedBranch != branchB {
		t.Fatalf("materialized branch = %q, want latest task-scoped branch %q", materializedBranch, branchB)
	}
	if targets.materializeRequest.InitialBranchAssertion == nil ||
		*targets.materializeRequest.InitialBranchAssertion != branchB {
		t.Fatalf("materialization assertion = %v, want originating request %q", targets.materializeRequest.InitialBranchAssertion, branchB)
	}
	targetContext, err := service.store.GetTaskExecutionTargetContext(ctx, workflow.TaskID(task.Task.ID))
	if err != nil {
		t.Fatalf("GetTaskExecutionTargetContext: %v", err)
	}
	if targetContext.Task.PendingInitialManagedBranchName != nil {
		t.Fatalf("pending branch after bind = %v, want consumed", targetContext.Task.PendingInitialManagedBranchName)
	}
}
