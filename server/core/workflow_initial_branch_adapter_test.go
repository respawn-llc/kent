package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/testharness/testsetup"
	"core/internal/testharness/workflowfixture"
	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/metadata"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/server/workflowsvc"
	"core/server/worktree"
	"core/shared/serverapi"
)

func TestTaskExecutionTargetInfrastructureCarriesPostCreationBranchAssertion(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	testsetup.InitializeGitRepository(t, workspace)
	if err := os.WriteFile(filepath.Join(workspace, "reopen.sh"), []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	testsetup.RunGit(t, workspace, "add", "reopen.sh")
	testsetup.RunGit(t, workspace, "commit", "-m", "replacement fixture")
	t.Setenv("HOME", t.TempDir())
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	binding, err := metadata.RegisterBinding(ctx, resolved.Config.PersistenceRoot, resolved.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	appCore := newCoreTestApp(t, resolved.Config, auth.EmptyState())
	if err := appCore.bundles.Persistence.metadataStore.SetProjectKey(ctx, binding.ProjectID, "BRA"); err != nil {
		t.Fatalf("SetProjectKey: %v", err)
	}
	store, err := workflowstore.New(
		appCore.bundles.Persistence.metadataStore,
		workflowstore.WithRoleResolver(configRoleResolver{settings: resolved.Config.Settings}),
	)
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	taskID := createCoreInitialBranchTask(t, store, binding.ProjectID)
	git := worktree.NewGitInspector(nil)
	worktreeService := appCore.bundles.Worktrees.worktrees.(*worktree.Service)
	infrastructure := taskExecutionTargetInfrastructure{service: worktreeService, git: git}
	targetContext, err := store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTaskExecutionTargetContext: %v", err)
	}
	branchA := *targetContext.Task.PendingInitialManagedBranchName
	revision, err := git.ResolveHEAD(ctx, workspace)
	if err != nil {
		t.Fatalf("ResolveHEAD: %v", err)
	}
	materialized, err := worktreeService.MaterializeInitialTaskWorktree(
		ctx,
		worktree.InitialTaskWorktreeMaterializationRequest{
			TaskID:         taskID,
			ResolvedTarget: revision,
		},
	)
	if err != nil {
		t.Fatalf("MaterializeInitialTaskWorktree: %v", err)
	}
	requestedRef := revision.RequestedRef
	commitOID := revision.CommitOID
	snapshot := workflowstore.ExecutionTargetSnapshot{
		Mode: workflow.ExecutionTargetModeHead, RequestedRef: &requestedRef,
		ResolvedRef: revision.CanonicalRef, CommitOID: &commitOID,
		Provenance: workflowstore.ExecutionTargetProvenanceResolved,
	}
	if err := store.LockTaskExecutionTarget(ctx, taskID, &workflowstore.ExecutionTargetCandidate{
		Snapshot: snapshot,
		Root: workflowstore.ExecutionRoot{
			SourceWorkspaceID: binding.WorkspaceID, SourceWorkspaceRoot: binding.CanonicalRoot,
			Managed: &workflowstore.ManagedExecutionRoot{
				WorktreeID: materialized.Worktree.GetRegistered().GetKent().GetWorktreeId(),
				Root:       materialized.Worktree.GetRegistered().GetGit().GetCanonicalRoot(),
			},
		},
	}); err != nil {
		t.Fatalf("LockTaskExecutionTarget: %v", err)
	}
	if err := infrastructure.AssertInitialTaskBranch(ctx, workflowsvc.InitialTaskBranchAssertionRequest{
		TaskID: taskID, BranchName: branchA,
	}); err != nil {
		t.Fatalf("AssertInitialTaskBranch exact assertion: %v", err)
	}
	branchB := "feature/other-branch"
	err = infrastructure.AssertInitialTaskBranch(ctx, workflowsvc.InitialTaskBranchAssertionRequest{
		TaskID: taskID, BranchName: branchB,
	})
	var mismatch *serverapi.WorkflowTaskInitialBranchError
	if !errors.As(err, &mismatch) ||
		mismatch.Reason != serverapi.WorkflowTaskInitialBranchErrorReasonPostCreationMismatch ||
		mismatch.BranchName != branchB ||
		mismatch.ExistingBranchName == nil ||
		*mismatch.ExistingBranchName != branchA {
		t.Fatalf("AssertInitialTaskBranch error = %T %+v, want %q versus %q mismatch", err, err, branchB, branchA)
	}
	if err := infrastructure.ValidateExecutionTarget(ctx, workflowsvc.ExecutionTargetValidationRequest{
		TaskID: taskID, InitialBranchAssertion: &branchA,
	}); err != nil {
		t.Fatalf("ValidateExecutionTarget exact assertion reuse: %v", err)
	}
	err = infrastructure.ValidateExecutionTarget(ctx, workflowsvc.ExecutionTargetValidationRequest{
		TaskID: taskID, InitialBranchAssertion: &branchB,
	})
	mismatch = nil
	if !errors.As(err, &mismatch) ||
		mismatch.Reason != serverapi.WorkflowTaskInitialBranchErrorReasonPostCreationMismatch ||
		mismatch.BranchName != branchB ||
		mismatch.ExistingBranchName == nil ||
		*mismatch.ExistingBranchName != branchA {
		t.Fatalf("MaterializeExecutionTarget error = %T %+v, want %q versus %q mismatch", err, err, branchB, branchA)
	}
	currentNodes, err := store.ListCurrentNodes(ctx, taskID)
	if err != nil || len(currentNodes) != 1 {
		t.Fatalf("current Nodes = %+v: %v", currentNodes, err)
	}
	source := currentNodes[0].Reference
	if _, err := store.CompleteCurrentNode(ctx, workflowstore.CurrentNodeCompletionRequest{Source: source, TransitionID: "done"}); err != nil {
		t.Fatal(err)
	}
	workflowfixture.SaveStoreGraph(t, ctx, store, targetContext.Task.WorkflowID, func(_ workflow.Definition, request *workflowstore.WorkflowGraphSaveRequest) {
		for i := range request.Nodes {
			if request.Nodes[i].ID == source.NodeID {
				request.Nodes[i].Kind = workflow.NodeKindScript
				request.Nodes[i].SubagentRole = ""
				request.Nodes[i].CompletionMode = ""
				request.Nodes[i].ScriptPath = "reopen.sh"
			}
		}
		for i := range request.Edges {
			if request.Edges[i].TargetNodeID == source.NodeID {
				request.Edges[i].PromptTemplate = ""
			}
		}
	})
	originalRoot := materialized.Worktree.GetRegistered().GetGit().GetCanonicalRoot()
	testsetup.RunGit(t, workspace, "worktree", "remove", originalRoot)
	testsetup.RunGit(t, workspace, "branch", "-D", branchA)
	request := serverapi.WorkflowTaskMoveRequest{TaskID: string(taskID), TargetNodeID: string(source.NodeID)}
	selection, err := appCore.bundles.Workflows.workflows.MoveWorkflowTask(ctx, request)
	if err != nil || selection.SelectionRequired == nil || selection.SelectionRequired.Details.GetOriginalTargetUnavailable() == nil {
		t.Fatalf("missing original selection = %+v: %v", selection, err)
	}
	request.ExecutionTarget = &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeHead}
	request.BranchName = &branchB
	applied, err := appCore.bundles.Workflows.workflows.MoveWorkflowTask(ctx, request)
	if err != nil || applied.Applied == nil {
		t.Fatalf("real replacement Move = %+v: %v", applied, err)
	}
	updated, err := store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil || updated.Task.ManagedWorktreeID == nil ||
		*updated.Task.ManagedWorktreeID == materialized.Worktree.GetRegistered().GetKent().GetWorktreeId() {
		t.Fatalf("replacement binding = %+v: %v", updated.Task, err)
	}
	replacement, err := appCore.bundles.Persistence.metadataStore.GetWorktreeRecordByID(ctx, *updated.Task.ManagedWorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	if got := testsetup.RunGit(t, replacement.CanonicalRoot, "branch", "--show-current"); got != branchB {
		t.Fatalf("replacement branch = %q", got)
	}
}

func createCoreInitialBranchTask(t *testing.T, store *workflowstore.Store, projectID string) workflow.TaskID {
	t.Helper()
	ctx := context.Background()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Initial branch"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	agentID := workflow.NodeID(uuid.NewString())
	startGroupID := workflow.TransitionGroupID(uuid.NewString())
	doneGroupID := workflow.TransitionGroupID(uuid.NewString())
	workflowfixture.SaveStoreGraph(t, ctx, store, created.ID, func(definition workflow.Definition, request *workflowstore.WorkflowGraphSaveRequest) {
		nodeID := func(kind workflow.NodeKind) workflow.NodeID {
			for _, node := range definition.Nodes {
				if node.Kind() == kind {
					return workflow.NodeIDOf(node)
				}
			}
			t.Fatalf("workflow has no %q node", kind)
			panic("unreachable")
		}
		request.Nodes = append(request.Nodes, workflowstore.NodeRecord{
			ID: agentID, WorkflowID: created.ID, Key: "agent",
			Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "default",
		})
		request.TransitionGroups = append(request.TransitionGroups,
			workflowstore.TransitionGroupRecord{ID: startGroupID, WorkflowID: created.ID, SourceNodeID: nodeID(workflow.NodeKindStart), TransitionID: "start", DisplayName: "Start"},
			workflowstore.TransitionGroupRecord{ID: doneGroupID, WorkflowID: created.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"},
		)
		request.Edges = append(request.Edges,
			workflowstore.EdgeRecord{
				ID: workflow.EdgeID(uuid.NewString()), WorkflowID: created.ID,
				TransitionGroupID: startGroupID, Key: "start", TargetNodeID: agentID,
				AssigneeSelection: workflow.AssigneeSelectionConfigured,
				ThinkingSelection: workflow.ThinkingSelectionConfigured,
				ContextMode:       workflow.ContextModeNewSession, PromptTemplate: "Do work.",
			},
			workflowstore.EdgeRecord{
				ID: workflow.EdgeID(uuid.NewString()), WorkflowID: created.ID,
				TransitionGroupID: doneGroupID, Key: "done", TargetNodeID: nodeID(workflow.NodeKindTerminal),
				AssigneeSelection: workflow.AssigneeSelectionConfigured,
				ThinkingSelection: workflow.ThinkingSelectionConfigured,
				ContextMode:       workflow.ContextModeNewSession,
			},
		)
	})
	if _, err := store.LinkWorkflow(ctx, projectID, created.ID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := store.CreateTask(ctx, workflowstore.CreateTaskRequest{
		ProjectID: projectID, WorkflowID: &created.ID,
		Title: "Initial branch assertion", Body: "Prepare a managed task worktree.",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := store.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	return task.ID
}
