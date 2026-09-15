package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/scriptedllm"
	"core/internal/testharness/testsetup"
	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/llm"
	"core/server/metadata"
	"core/server/runtimewire"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/config"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/serverapi"
	"core/shared/worktreecontract"
)

func TestWorkflowMissingTargetReplacementUsesSourceWorkspace(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeNone)
}

func TestWorkflowMissingTargetReplacementAttachesRetainedBranch(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeCustomRef)
}

func testWorkflowMissingTargetReplacement(t *testing.T, mode serverapi.WorkflowExecutionTargetMode) {
	t.Helper()
	ctx := context.Background()
	workspace := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	testsetup.InitializeGitRepository(t, workspace)
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspace})
	if err != nil {
		t.Fatal(err)
	}
	resolved.Config.Settings.Model = "gpt-5"
	resolved.Config.Settings.Reviewer.Frequency = "off"
	resolved.Config.Settings.Workflow.CompletionMode = config.WorkflowCompletionModeStructuredOutput
	binding, err := metadata.RegisterBinding(ctx, resolved.Config.PersistenceRoot, resolved.Config.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	first := testsetup.NewStartBarrier()
	second := testsetup.NewStartBarrier()
	t.Cleanup(first.Unblock)
	t.Cleanup(second.Unblock)
	firstStep := scriptedllm.FinalAnswer(`{"commentary":"completed"}`)
	firstStep.BeforeResponse = first.ArriveAndWait
	secondStep := scriptedllm.FinalAnswer(`{"commentary":"completed"}`)
	secondStep.BeforeResponse = second.ArriveAndWait
	client := scriptedllm.NewClient(scriptedllm.Script{Steps: []scriptedllm.Step{firstStep, secondStep}})
	app := newCoreTestAppWithOptions(t, resolved.Config, auth.State{
		Scope:  auth.ScopeGlobal,
		Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}},
	}, Options{RuntimeClientFactory: runtimewire.RuntimeClientFactoryFunc(func(context.Context, runtimewire.RuntimeClientRequest) (llm.Client, error) {
		return client, nil
	})})
	if err := app.MetadataStore().SetProjectKey(ctx, binding.ProjectID, "REC"); err != nil {
		t.Fatal(err)
	}
	store, err := workflowstore.New(app.MetadataStore(), workflowstore.WithRoleResolver(configRoleResolver{settings: resolved.Config.Settings}))
	if err != nil {
		t.Fatal(err)
	}
	taskID := createCoreInitialBranchTask(t, store, binding.ProjectID)
	nodes, err := store.ListCurrentNodes(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InterruptCurrentNode(ctx, nodes[0].Reference, workflow.CurrentNodeInterruptionReasonUserInterrupt,
		workflow.NewCurrentNodeInterruptionDetail("user_interrupt", nil)); err != nil {
		t.Fatal(err)
	}
	api := app.WorkflowClient()
	_, err = api.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{
		TaskID: string(taskID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
		ExecutionTarget: &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeHead},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitRecoveryModel(t, first, store, taskID)
	before, err := store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil || before.Task.ManagedWorktreeID == nil {
		t.Fatalf("initial managed target: %+v, %v", before, err)
	}
	oldID := *before.Task.ManagedWorktreeID
	old, err := app.MetadataStore().GetWorktreeRecordByID(ctx, oldID)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("retained committed Task content")
	if err := os.WriteFile(filepath.Join(old.CanonicalRoot, "retained.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	testsetup.RunGit(t, old.CanonicalRoot, "add", "retained.txt")
	testsetup.RunGit(t, old.CanonicalRoot, "commit", "-m", "retained Task work")
	ref := "refs/heads/" + before.Task.ShortID
	commit := strings.TrimSpace(testsetup.RunGit(t, binding.CanonicalRoot, "rev-parse", ref))
	if old.CreationBaseCommitOID == nil || *old.CreationBaseCommitOID == commit {
		t.Fatal("fixture branch must advance beyond its creation base")
	}
	if _, err := api.InterruptWorkflowTask(ctx, serverapi.WorkflowTaskInterruptRequest{TaskID: string(taskID), Reason: "user_interrupt"}); err != nil {
		t.Fatal(err)
	}
	_, err = app.WorktreeClient().DeleteWorktree(ctx, &worktreepb.DeleteRequest{
		Scope:    worktreecontract.WorkspaceManagementScope(binding.ProjectID, binding.WorkspaceID, nil),
		Selector: oldID, BranchCleanupPolicy: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN,
	})
	if err != nil {
		t.Fatal(err)
	}
	selection := &serverapi.WorkflowExecutionTargetSelection{Mode: mode}
	if mode == serverapi.WorkflowExecutionTargetModeCustomRef {
		selection.CustomRef = &ref
	}
	response, err := api.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{
		TaskID: string(taskID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
		ExecutionTarget: selection,
	})
	if err != nil || response.Outcome != serverapi.WorkflowExecutionTargetActionOutcomeApplied {
		t.Fatalf("explicit recovery: %+v, %v", response, err)
	}
	waitRecoveryModel(t, second, store, taskID)
	after, err := store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Task.Title != before.Task.Title || after.Task.Body != before.Task.Body ||
		after.Task.ExecutionTarget.Mode != workflow.ExecutionTargetMode(mode) {
		t.Fatalf("replacement did not preserve Task content on selected target: %+v", after.Task)
	}
	if _, err := app.MetadataStore().GetWorktreeRecordByID(ctx, oldID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("stale record remains: %v", err)
	}
	expectedRoot := binding.CanonicalRoot
	if mode == serverapi.WorkflowExecutionTargetModeNone {
		if after.Task.ManagedWorktreeID != nil {
			t.Fatalf("source Workspace retained managed binding: %v", after.Task.ManagedWorktreeID)
		}
		if _, err := os.Stat(old.CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old root was recreated: %v", err)
		}
	} else {
		if after.Task.ManagedWorktreeID == nil || *after.Task.ManagedWorktreeID == oldID {
			t.Fatalf("replacement did not create a fresh binding: %v", after.Task.ManagedWorktreeID)
		}
		record, err := app.MetadataStore().GetWorktreeRecordByID(ctx, *after.Task.ManagedWorktreeID)
		if err != nil || record.CanonicalRoot != old.CanonicalRoot ||
			record.CreationBaseCommitOID == nil || *record.CreationBaseCommitOID != commit {
			t.Fatalf("replacement did not reuse normal root with fresh base facts: %+v, %v", record, err)
		}
		actual, err := os.ReadFile(filepath.Join(record.CanonicalRoot, "retained.txt"))
		if err != nil || string(actual) != string(content) {
			t.Fatalf("retained branch content lost: %q, %v", actual, err)
		}
		expectedRoot = record.CanonicalRoot
	}
	if current := strings.TrimSpace(testsetup.RunGit(t, binding.CanonicalRoot, "rev-parse", ref)); current != commit {
		t.Fatalf("retained branch was changed: %s != %s", current, commit)
	}
	nodes, err = store.ListCurrentNodes(ctx, taskID)
	if err != nil || len(nodes) != 1 || nodes[0].SessionID == nil {
		t.Fatalf("resumed Current Node: %+v, %v", nodes, err)
	}
	target, err := app.MetadataStore().ResolveSessionExecutionTarget(ctx, nodes[0].SessionID.String())
	if err != nil || target.EffectiveWorkdir != expectedRoot {
		t.Fatalf("Session did not resume on selected target: %v, %v", target, err)
	}
	second.Unblock()
	testsetup.RequireUntil(t, time.Now().Add(10*time.Second), 10*time.Millisecond, func() bool {
		current, err := store.ListCurrentNodes(ctx, taskID)
		return err == nil && len(current) == 1 && current[0].Scheduling == nil
	}, "recovered Task did not complete")
	if count, err := store.CountTaskSessions(ctx, taskID); err != nil || count != 1 {
		t.Fatalf("source Session association was lost: %d, %v", count, err)
	}
}

func waitRecoveryModel(t *testing.T, barrier *testsetup.StartBarrier, store *workflowstore.Store, taskID workflow.TaskID) {
	t.Helper()
	select {
	case <-barrier.Entered():
	case <-time.After(10 * time.Second):
		nodes, err := store.ListCurrentNodes(context.Background(), taskID)
		details, encodeErr := json.Marshal(nodes)
		t.Fatalf("Task model execution did not begin: nodes=%s read=%v encode=%v", details, err, encodeErr)
	}
}
