package workflowrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	"core/server/worktree"
	"core/shared/serverapi"
)

func prepareMissingWorktreeCompletion(t *testing.T, f *currentNodeRunnerFixture, task workflowstore.TaskRecord) (workflowstore.ExecutionTargetCandidate, *worktree.Service) {
	t.Helper()
	testsetup.InitializeGitRepository(t, f.workspace)
	git := worktree.NewGitInspector(nil)
	service := worktree.NewService(f.metadata, git, f.authority, f.runtimes, nil, worktree.ServiceOptions{BaseDir: f.cfg.Settings.Worktrees.BaseDir})
	f.executionTargets = service
	t.Cleanup(func() { _ = service.Close() })
	revision, err := git.ResolveHEAD(context.Background(), f.workspace)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.MaterializeInitialTaskWorktree(context.Background(), worktree.InitialTaskWorktreeMaterializationRequest{
		TaskID: task.ID, ResolvedTarget: revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate := workflowstore.ExecutionTargetCandidate{
		Snapshot: workflowstore.ExecutionTargetSnapshot{
			Mode: workflow.ExecutionTargetModeHead, RequestedRef: &revision.RequestedRef,
			ResolvedRef: revision.CanonicalRef, CommitOID: &revision.CommitOID,
			Provenance: workflowstore.ExecutionTargetProvenanceResolved,
		},
		Root: workflowstore.ExecutionRoot{
			SourceWorkspaceID: f.workspaceID, SourceWorkspaceRoot: f.workspace,
			Managed: &workflowstore.ManagedExecutionRoot{
				WorktreeID: created.Worktree.GetRegistered().GetKent().GetWorktreeId(),
				Root:       created.Worktree.GetRegistered().GetGit().GetCanonicalRoot(),
			},
		},
	}
	return candidate, service
}

func (f *currentNodeRunnerFixture) RestoreExecutionTarget(ctx context.Context, req workflow.ExecutionTargetRestoreRequest) error {
	if f.executionTargets == nil {
		return errors.New("managed fixture target was not prepared")
	}
	_, err := f.executionTargets.RestoreLockedTaskWorktree(ctx, worktree.LockedTaskWorktreeRestoreRequest{
		TaskID: req.TaskID, BranchName: req.InitialBranchAssertion,
		SetupOperationID: req.SetupOperationID,
	})
	var locked *worktree.LockedTaskWorktreeError
	if errors.As(err, &locked) {
		return &serverapi.WorkflowLockedExecutionTargetError{Cause: serverapi.WorkflowLockedExecutionTargetCause(locked.Cause)}
	}
	return err
}

func startManagedCompletionTask(t *testing.T, f *currentNodeRunnerFixture, task workflowstore.TaskRecord, candidate workflowstore.ExecutionTargetCandidate) workflow.CurrentNodeReference {
	t.Helper()
	return f.startTaskWithPreparation(t, task, workflowexecution.TaskStartPreparation{
		Prepare: func(context.Context) error { return nil },
		Commit:  func(ctx context.Context) error { return f.store.LockTaskExecutionTarget(ctx, task.ID, &candidate) },
	})
}

func removeCompletionWorktree(ctx context.Context, source, root string) error {
	output, err := exec.CommandContext(ctx, "git", "-C", source, "worktree", "remove", "--force", root).CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove disposable Worktree: %w: %s", err, output)
	}
	return nil
}

func TestCurrentNodeCompletionToDoneWithMissingWorktree(t *testing.T) {
	step := ScriptedFinalAnswer(`{"commentary":"completed source work"}`)
	var f *currentNodeRunnerFixture
	var candidate workflowstore.ExecutionTargetCandidate
	step.BeforeResponse = func(ctx context.Context) error {
		return removeCompletionWorktree(ctx, f.workspace, candidate.Root.Managed.Root)
	}
	f = newCurrentNodeRunnerFixture(t, step)
	task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
	candidate, _ = prepareMissingWorktreeCompletion(t, f, task)
	startManagedCompletionTask(t, f, task, candidate)
	f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		if len(nodes) == 1 && nodes[0].Scheduling != nil && nodes[0].Scheduling.State == workflow.CurrentNodeSchedulingInterrupted {
			t.Fatalf("source completion interrupted: %+v", nodes[0].Scheduling.Interruption)
		}
		return len(nodes) == 1 && nodes[0].Scheduling == nil
	})
	assertMissingCompletionEvidence(t, f, task, candidate)
}

func TestCurrentNodeCompletionRestoresExecutableSuccessorWorktree(t *testing.T) {
	for _, kind := range []workflow.NodeKind{workflow.NodeKindAgent, workflow.NodeKindScript} {
		t.Run(string(kind), func(t *testing.T) {
			sourceStep := ScriptedFinalAnswer(`{"commentary":"source completed"}`)
			var f *currentNodeRunnerFixture
			var candidate workflowstore.ExecutionTargetCandidate
			sourceStep.BeforeResponse = func(ctx context.Context) error {
				return removeCompletionWorktree(ctx, f.workspace, candidate.Root.Managed.Root)
			}
			steps := []ScriptedRuntimeStep{sourceStep}
			successor := currentNodeWorkflowStep{kind: kind}
			marker := filepath.Join(t.TempDir(), "executed")
			if kind == workflow.NodeKindAgent {
				successor.role, successor.prompt = "coder", "Continue work."
				step := ScriptedFinalAnswer(`{"commentary":"successor completed"}`)
				step.BeforeResponse = func(context.Context) error {
					_, err := os.Stat(candidate.Root.Managed.Root)
					return err
				}
				steps = append(steps, step)
			} else {
				successor.scriptPath = filepath.Join(t.TempDir(), "successor.sh")
				if err := os.WriteFile(successor.scriptPath, []byte("#!/bin/sh\ntouch "+workflowRunnerShellQuote(marker)+"\nprintf '%s' '{\"commentary\":\"done\"}'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			f = newCurrentNodeRunnerFixture(t, steps...)
			id := createCurrentNodeTwoStepWorkflow(t, f.store, "Restored Worktree", workflow.ContextModeNewSession,
				currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "Do work."}, successor)
			task := f.createTask(t, id)
			candidate, _ = prepareMissingWorktreeCompletion(t, f, task)
			startManagedCompletionTask(t, f, task, candidate)
			f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
				if len(nodes) == 1 && nodes[0].Scheduling != nil && nodes[0].Scheduling.Interruption != nil {
					t.Fatalf("safe successor restoration failed: %+v", nodes[0].Scheduling.Interruption)
				}
				return len(nodes) == 1 && nodes[0].Scheduling == nil
			})
			if _, err := os.Stat(candidate.Root.Managed.Root); err != nil {
				t.Fatalf("successor did not restore its original root: %v", err)
			}
			if kind == workflow.NodeKindScript {
				if _, err := os.Stat(marker); err != nil {
					t.Fatalf("restored Script did not execute: %v", err)
				}
			}
		})
	}
}

func TestCurrentNodeCompletionPausesExecutableSuccessorWithUnavailableWorktree(t *testing.T) {
	for _, kind := range []workflow.NodeKind{workflow.NodeKindAgent, workflow.NodeKindScript} {
		t.Run(string(kind), func(t *testing.T) {
			step := ScriptedFinalAnswer(`{"commentary":"completed source work"}`)
			var f *currentNodeRunnerFixture
			var candidate workflowstore.ExecutionTargetCandidate
			var branch string
			step.BeforeResponse = func(ctx context.Context) error {
				if err := removeCompletionWorktree(ctx, f.workspace, candidate.Root.Managed.Root); err != nil {
					return err
				}
				output, err := exec.CommandContext(ctx, "git", "-C", f.workspace, "branch", "-D", branch).CombinedOutput()
				if err != nil {
					return fmt.Errorf("delete disposable branch: %w: %s", err, output)
				}
				return nil
			}
			f = newCurrentNodeRunnerFixture(t, step)
			successor := currentNodeWorkflowStep{kind: kind}
			marker := filepath.Join(t.TempDir(), "executed")
			if kind == workflow.NodeKindAgent {
				successor.role = "coder"
				successor.prompt = "Continue the work."
			} else {
				successor.scriptPath = filepath.Join(t.TempDir(), "successor.sh")
				if err := os.WriteFile(successor.scriptPath, []byte("#!/bin/sh\ntouch "+workflowRunnerShellQuote(marker)+"\nprintf '%s' '{\"commentary\":\"done\"}'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			workflowID := createCurrentNodeTwoStepWorkflow(t, f.store, "Missing Worktree", workflow.ContextModeNewSession,
				currentNodeWorkflowStep{kind: workflow.NodeKindAgent, role: "coder", prompt: "Do the work."}, successor)
			task := f.createTask(t, workflowID)
			branch = task.ShortID
			candidate, _ = prepareMissingWorktreeCompletion(t, f, task)
			source := startManagedCompletionTask(t, f, task, candidate)
			nodes := f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
				return len(nodes) == 1 && nodes[0].Scheduling != nil &&
					nodes[0].Scheduling.State == workflow.CurrentNodeSchedulingInterrupted
			})
			f.waitForTaskQuiescence(t, task.ID)
			detail := nodes[0].Scheduling.Interruption.Detail
			if nodes[0].Reference.Equal(source) || detail.OriginalExecutionTargetUnavailable == nil || detail.SetupRecovery != nil {
				t.Fatalf("completion did not preserve source and request successor selection: %+v", nodes[0])
			}
			if nodes[0].SessionID != nil || len(f.client.Requests()) != 1 {
				t.Fatal("successor assignment or model execution began before target selection")
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("successor Script executed: %v", err)
			}
			assertMissingCompletionEvidence(t, f, task, candidate)
		})
	}
}

func assertMissingCompletionEvidence(t *testing.T, f *currentNodeRunnerFixture, task workflowstore.TaskRecord, candidate workflowstore.ExecutionTargetCandidate) {
	t.Helper()
	if _, err := os.Stat(candidate.Root.Managed.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completion recreated Worktree: %v", err)
	}
	target, err := f.store.GetTaskExecutionTargetContext(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if target.Task.ManagedWorktreeID == nil || *target.Task.ManagedWorktreeID != candidate.Root.Managed.WorktreeID ||
		target.Task.Title != task.Title || target.Task.Body != task.Body {
		t.Fatalf("completion changed retained Task evidence: %+v", target.Task)
	}
	if count, err := f.store.CountTaskSessions(context.Background(), task.ID); err != nil || count != 1 {
		t.Fatalf("source Session evidence = %d, %v", count, err)
	}
}
