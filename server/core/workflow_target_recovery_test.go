package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/scriptedllm"
	"core/internal/testharness/testsetup"
	"core/internal/testharness/workflowfixture"
	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/llm"
	"core/server/metadata"
	"core/server/runtimewire"
	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/config"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/worktreecontract"
)

type taskRecoveryFixture struct {
	app           *Core
	store         *workflowstore.Store
	binding       metadata.Binding
	task          workflowstore.TaskRecord
	original      metadata.WorktreeRecord
	first, second *testsetup.StartBarrier
	client        *scriptedllm.Client
	moveTarget    workflow.NodeID
	stoppedTarget chan *string
}

func newTaskRecoveryFixture(t *testing.T) *taskRecoveryFixture {
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
	first, second := testsetup.NewStartBarrier(), testsetup.NewStartBarrier()
	f := &taskRecoveryFixture{first: first, second: second, stoppedTarget: make(chan *string, 1)}
	t.Cleanup(first.Unblock)
	t.Cleanup(second.Unblock)
	firstStep := scriptedllm.FinalAnswer(`{"transition":"done","commentary":"completed"}`)
	firstStep.BeforeResponse = func(ctx context.Context) error {
		waitErr := first.ArriveAndWait(ctx)
		target, err := f.store.GetTaskExecutionTargetContext(context.Background(), f.task.ID)
		if err != nil {
			return errors.Join(waitErr, err)
		}
		f.stoppedTarget <- target.Task.ManagedWorktreeID
		return waitErr
	}
	secondStep := scriptedllm.FinalAnswer(`{"commentary":"completed"}`)
	secondStep.BeforeResponse = second.ArriveAndWait
	client := scriptedllm.NewClient(scriptedllm.Script{Steps: []scriptedllm.Step{firstStep, secondStep}})
	app := newCoreTestAppWithOptions(t, resolved.Config, auth.EmptyState(),

		Options{RuntimeClientFactory: runtimewire.RuntimeClientFactoryFunc(func(context.Context, runtimewire.RuntimeClientRequest) (llm.Client, error) {
			return client, nil
		})})
	if err := app.MetadataStore().SetProjectKey(ctx, binding.ProjectID, "REC"); err != nil {
		t.Fatal(err)
	}
	store, err := workflowstore.New(app.MetadataStore(), workflowstore.WithRoleResolver(configRoleResolver{app: resolved.Config}))
	if err != nil {
		t.Fatal(err)
	}
	taskID := createCoreInitialBranchTask(t, store, binding.ProjectID)
	target, err := store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	f.task, f.store, f.app, f.binding, f.client = target.Task, store, app, binding, client
	definition, _, err := store.GetDefinition(ctx, target.Task.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	var agent *workflow.NodeID
	for _, node := range definition.Nodes {
		if node.Kind() == workflow.NodeKindAgent {
			id := workflow.NodeIDOf(node)
			agent = &id
		}
	}
	if agent == nil {
		t.Fatal("recovery fixture has no Agent Node")
	}
	f.moveTarget = addCoreRecoveryMoveTarget(t, store, taskID, *agent)
	_, err = app.WorkflowClient().StartWorkflowTask(ctx, serverapi.WorkflowTaskStartRequest{
		TaskID: string(taskID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
		ExecutionTarget: &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeHead},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitRecoveryModel(t, first, store, taskID)
	target, err = store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil || target.Task.ManagedWorktreeID == nil {
		t.Fatalf("initial managed target: %+v, %v", target, err)
	}
	original, err := app.MetadataStore().GetWorktreeRecordByID(ctx, *target.Task.ManagedWorktreeID)
	if err != nil {
		t.Fatal(err)
	}
	f.task, f.original = target.Task, original
	return f
}

func (f *taskRecoveryFixture) interruptAndDelete(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.app.WorkflowClient().InterruptWorkflowTask(ctx, serverapi.WorkflowTaskInterruptRequest{
		TaskID: string(f.task.ID), Reason: "user_interrupt",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.WorktreeClient().DeleteWorktree(ctx, &worktreepb.DeleteRequest{
		Scope:    worktreecontract.WorkspaceManagementScope(f.binding.ProjectID, f.binding.WorkspaceID, nil),
		Selector: f.original.ID, BranchCleanupPolicy: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN,
	}); err != nil {
		t.Fatal(err)
	}
}

func (f *taskRecoveryFixture) resume(t *testing.T, selection *serverapi.WorkflowExecutionTargetSelection, branch *string) serverapi.WorkflowTaskResumeResponse {
	t.Helper()
	response, err := f.app.WorkflowClient().ResumeWorkflowTask(context.Background(), serverapi.WorkflowTaskResumeRequest{
		TaskID: string(f.task.ID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
		ExecutionTarget: selection, BranchName: branch,
	})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func (f *taskRecoveryFixture) resumeSetupFailure(t *testing.T, selection *serverapi.WorkflowExecutionTargetSelection, branch *string) *worktreepb.SetupRetainedDetails {
	t.Helper()
	response, err := f.app.WorkflowClient().ResumeWorkflowTask(context.Background(), serverapi.WorkflowTaskResumeRequest{
		TaskID: string(f.task.ID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
		ExecutionTarget: selection, BranchName: branch,
	})
	var retained *serverapi.WorkflowSetupRetainedError
	if !errors.As(err, &retained) || response.Applied != nil {
		t.Fatalf("Resume setup failure lost its typed diagnostics: %+v, %v", response, err)
	}
	if retained.Details == nil || retained.Details.Worktree == nil ||
		retained.Details.Worktree.Kent == nil || retained.Details.Worktree.Git == nil {
		t.Fatalf("Resume setup failure omitted retained Worktree facts: %+v", retained.Details)
	}
	return retained.Details
}

func (f *taskRecoveryFixture) setup(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(f.binding.CanonicalRoot, ".kent")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(fmt.Sprintf("[worktrees]\nsetup_script = %q\n", path)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f *taskRecoveryFixture) assertEvidence(t *testing.T) workflowstore.TaskExecutionTargetContext {
	t.Helper()
	target, err := f.store.GetTaskExecutionTargetContext(context.Background(), f.task.ID)
	if err != nil || target.Task.Title != f.task.Title || target.Task.Body != f.task.Body {
		t.Fatalf("Task evidence changed: %+v, %v", target, err)
	}
	if count, err := f.store.CountTaskSessions(context.Background(), f.task.ID); err != nil || count < 1 {
		t.Fatalf("source Session evidence lost: %d, %v", count, err)
	}
	return target
}

func TestWorkflowMissingTargetRestoresRetainedBranch(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	content := []byte("retained committed Task content")
	if err := os.WriteFile(filepath.Join(f.original.CanonicalRoot, "retained.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	testsetup.RunGit(t, f.original.CanonicalRoot, "add", "retained.txt")
	testsetup.RunGit(t, f.original.CanonicalRoot, "commit", "-m", "retained Task work")
	f.interruptAndDelete(t)
	if result := f.resume(t, nil, nil); result.Applied == nil {
		t.Fatalf("Resume was not accepted: %+v", result)
	}
	waitRecoveryModel(t, f.second, f.store, f.task.ID)
	target := f.assertEvidence(t)
	if target.Task.ManagedWorktreeID == nil || *target.Task.ManagedWorktreeID != f.original.ID {
		t.Fatalf("restoration replaced the original binding: %+v", target.Task)
	}
	actual, err := os.ReadFile(filepath.Join(f.original.CanonicalRoot, "retained.txt"))
	if err != nil || string(actual) != string(content) {
		t.Fatalf("restoration lost committed content: %q, %v", actual, err)
	}
}

func TestWorkflowRestoredOriginalCanResumeAfterSetupFailure(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	f.interruptAndDelete(t)
	attempts := filepath.Join(t.TempDir(), "attempts")
	f.setup(t, fmt.Sprintf("printf x >> %q\nexit 1\n", attempts))
	f.resumeSetupFailure(t, nil, nil)
	f.waitInterrupted(t)
	if len(f.client.Requests()) != 1 {
		t.Fatal("restoration setup failure started Agent work")
	}
	if _, err := os.Stat(f.original.CanonicalRoot); err != nil {
		t.Fatalf("restoration did not retain the original checkout: %v", err)
	}
	f.resume(t, nil, nil)
	waitRecoveryModel(t, f.second, f.store, f.task.ID)
	data, err := os.ReadFile(attempts)
	if err != nil || len(data) != 1 {
		t.Fatalf("later Resume reran original restoration setup: %q, %v", data, err)
	}
	target := f.assertEvidence(t)
	if target.Task.ManagedWorktreeID == nil || *target.Task.ManagedWorktreeID != f.original.ID {
		t.Fatal("restored original target lost its binding")
	}
}

func TestWorkflowRestoredOriginalAcceptsSessionMessageAfterSetupFailure(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	f.interruptAndDelete(t)
	attempts := filepath.Join(t.TempDir(), "attempts")
	f.setup(t, fmt.Sprintf("printf x >> %q\nexit 1\n", attempts))
	f.resumeSetupFailure(t, nil, nil)
	node := f.waitInterrupted(t)
	if node.SessionID == nil {
		t.Fatal("restoration failure lost the retained Session")
	}
	cfg := f.app.Config()
	attachment, err := f.app.SessionRuntimeClient().ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		SessionID: node.SessionID.String(), OwnerID: "recovery-test",
		ActiveSettings: cfg.Settings, Source: cfg.Source,
		QuestionsEnabled: textutil.Value(true), AutoCompactionEnabled: textutil.Value(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.app.SessionRuntimeClient().ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
			Attachment: attachment, OwnerID: "recovery-test", DropOwner: true, ClosePolicy: serverapi.SessionRuntimeReleaseClosePolicyDetachOnly,
		})
	})
	const message = "Continue on the restored original checkout"
	submitted := testsetup.Start(func() (*runtimepb.SubmitUserTurnSuccess, error) {
		return f.app.RuntimeControlClient().SubmitUserTurn(context.Background(), &runtimepb.SubmitUserTurnRequest{
			SessionId: node.SessionID.String(), Input: &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: message}},
		})
	})
	waitRecoveryModel(t, f.second, f.store, f.task.ID)
	delivered := false
	for _, item := range f.client.Requests()[1].Items {
		if item.Role != nil && *item.Role == llm.RoleUser && item.Content != nil && *item.Content == message {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("retained Session message was not delivered")
	}
	if data, err := os.ReadFile(attempts); err != nil || len(data) != 1 {
		t.Fatalf("message reran original restoration setup: %q, %v", data, err)
	}
	f.second.Unblock()
	select {
	case outcome := <-submitted:
		if outcome.Err != nil {
			t.Fatal(outcome.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Session submission did not finish")
	}
}

func TestWorkflowUnavailableTargetReplacementUsesSourceWorkspace(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	marker := f.makeOriginalUnavailable(t)
	if result := f.resume(t, nil, nil); result.SelectionRequired == nil || result.SelectionRequired.Details.GetOriginalTargetUnavailable() == nil {
		t.Fatalf("unsafe original target did not request selection: %+v", result)
	}
	result := f.resume(t, &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeNone}, nil)
	if result.Applied == nil {
		t.Fatalf("explicit replacement was not accepted: %+v", result)
	}
	waitRecoveryModel(t, f.second, f.store, f.task.ID)
	target := f.assertEvidence(t)
	if target.Task.ManagedWorktreeID != nil || target.Task.ExecutionTarget.Mode != workflow.ExecutionTargetModeNone {
		t.Fatalf("Task did not switch to source Workspace: %+v", target.Task)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("replacement changed original operator files: %v", err)
	}
}

func (f *taskRecoveryFixture) makeOriginalUnavailable(t *testing.T) string {
	t.Helper()
	f.interruptAndDelete(t)
	if err := os.MkdirAll(f.original.CanonicalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(f.original.CanonicalRoot, "operator.txt")
	if err := os.WriteFile(marker, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	return marker
}

func TestWorkflowUnavailableTargetReplacementCreatesFreshBranch(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	marker := f.makeOriginalUnavailable(t)
	ref := strings.TrimSpace(testsetup.RunGit(t, f.binding.CanonicalRoot, "symbolic-ref", "HEAD"))
	selection := &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeCustomRef, CustomRef: &ref}
	_, err := f.app.WorkflowClient().ResumeWorkflowTask(context.Background(), serverapi.WorkflowTaskResumeRequest{
		TaskID: string(f.task.ID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(), ExecutionTarget: selection,
	})
	var collision *serverapi.WorkflowTaskInitialBranchError
	if !errors.As(err, &collision) || collision.Reason != serverapi.WorkflowTaskInitialBranchErrorReasonLocalCollision {
		t.Fatalf("retained branch name was not protected: %v", err)
	}
	branch := "fresh-task-target"
	f.resume(t, selection, &branch)
	waitRecoveryModel(t, f.second, f.store, f.task.ID)
	target := f.assertEvidence(t)
	if target.Task.ManagedWorktreeID == nil || *target.Task.ManagedWorktreeID == f.original.ID {
		t.Fatalf("replacement did not create a fresh binding: %+v", target.Task)
	}
	record, err := f.app.MetadataStore().GetWorktreeRecordByID(context.Background(), *target.Task.ManagedWorktreeID)
	if err != nil || record.CanonicalRoot == f.original.CanonicalRoot {
		t.Fatalf("replacement reused the occupied original location: %+v, %v", record, err)
	}
	if actual := strings.TrimSpace(testsetup.RunGit(t, record.CanonicalRoot, "branch", "--show-current")); actual != branch {
		t.Fatalf("replacement branch = %q, want %q", actual, branch)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("replacement changed original operator files: %v", err)
	}
}

func TestWorkflowMoveReplacementStopsWorkBeforeSetup(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	testsetup.RunGit(t, f.binding.CanonicalRoot, "worktree", "remove", "--force", f.original.CanonicalRoot)
	if err := os.MkdirAll(f.original.CanonicalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	f.setup(t, "exit 1\n")
	ref, branch := "no-such-ref", "move-replacement"
	request := serverapi.WorkflowTaskMoveRequest{
		TaskID: string(f.task.ID), TargetNodeID: string(f.moveTarget), BranchName: &branch,
		ExecutionTarget: &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeCustomRef, CustomRef: &ref},
	}
	if _, err := f.app.WorkflowClient().MoveWorkflowTask(context.Background(), request); err == nil {
		t.Fatal("invalid ref was accepted")
	}
	select {
	case <-f.stoppedTarget:
		t.Fatal("invalid selection stopped existing work")
	default:
	}
	request.ExecutionTarget = &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeHead}
	response, err := f.app.WorkflowClient().MoveWorkflowTask(context.Background(), request)
	var retained *serverapi.WorkflowSetupRetainedError
	if !errors.As(err, &retained) || response.Applied != nil {
		t.Fatalf("failed replacement Move result: %+v, %v", response, err)
	}
	select {
	case target := <-f.stoppedTarget:
		if target == nil || *target != f.original.ID {
			t.Fatal("replacement changed target before stopping original work")
		}
	default:
		t.Fatal("replacement setup ran before stopping original work")
	}
	target := f.assertEvidence(t)
	if target.Task.ManagedWorktreeID == nil || *target.Task.ManagedWorktreeID != f.original.ID {
		t.Fatal("failed candidate replaced the authoritative target")
	}
	if len(f.client.Requests()) != 1 {
		t.Fatal("failed replacement started destination work")
	}
}

func TestWorkflowFailedCustomRefReplacementRequiresFreshAttempt(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	f.makeOriginalUnavailable(t)
	script := f.setup(t, "git checkout -b setup-created-branch\nprintf preserved > setup-output\nexit 1\n")
	ref := strings.TrimSpace(testsetup.RunGit(t, f.binding.CanonicalRoot, "symbolic-ref", "HEAD"))
	selection := &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeCustomRef, CustomRef: &ref}
	branch := "failed-custom-base"
	recovery := f.resumeSetupFailure(t, selection, &branch)
	if recovery.RecoveryDisposition != worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_FRESH_REPLACEMENT {
		t.Fatal("failed candidate recovery did not require original-target replacement")
	}
	after := f.assertEvidence(t)
	if after.Task.ManagedWorktreeID == nil || *after.Task.ManagedWorktreeID != f.original.ID || len(f.client.Requests()) != 1 {
		t.Fatal("failed candidate was adopted or executed")
	}
	failedRoot := recovery.Worktree.Git.CanonicalRoot
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBranch := "fresh-custom-base"
	f.resume(t, selection, &newBranch)
	waitRecoveryModel(t, f.second, f.store, f.task.ID)
	after = f.assertEvidence(t)
	if after.Task.ManagedWorktreeID == nil || *after.Task.ManagedWorktreeID == recovery.Worktree.Kent.WorktreeId {
		t.Fatal("fresh attempt adopted the failed candidate")
	}
	if data, err := os.ReadFile(filepath.Join(failedRoot, "setup-output")); err != nil || string(data) != "preserved" {
		t.Fatalf("fresh attempt changed failed candidate files: %q, %v", data, err)
	}
	if branch := strings.TrimSpace(testsetup.RunGit(t, failedRoot, "branch", "--show-current")); branch != "setup-created-branch" {
		t.Fatalf("fresh attempt changed the failed candidate's checkout: %s", branch)
	}
}

func TestWorkflowMoveFromDoneRetainsFailedCandidateWithoutAdoption(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	f.first.Unblock()
	testsetup.RequireUntil(t, time.Now().Add(10*time.Second), 10*time.Millisecond, func() bool {
		nodes, err := f.store.ListCurrentNodes(context.Background(), f.task.ID)
		live, liveErr := f.app.bundles.Runtime.runtimeAuthority.HasLiveWorkflowTaskExecution(f.task.ID)
		return err == nil && liveErr == nil && !live && len(nodes) == 1 && nodes[0].Scheduling == nil
	}, "source Task did not reach Done")
	testsetup.RunGit(t, f.binding.CanonicalRoot, "worktree", "remove", "--force", f.original.CanonicalRoot)
	if err := os.MkdirAll(f.original.CanonicalRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	script := f.setup(t, "printf preserved > setup-output\nexit 1\n")
	branch := "done-failed-target"
	request := serverapi.WorkflowTaskMoveRequest{
		TaskID: string(f.task.ID), TargetNodeID: string(f.moveTarget), BranchName: &branch,
		ExecutionTarget: &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeHead},
	}
	result, err := f.app.WorkflowClient().MoveWorkflowTask(context.Background(), request)
	var retained *serverapi.WorkflowSetupRetainedError
	if !errors.As(err, &retained) || result.Applied != nil ||
		retained.Details.RecoveryDisposition != worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_FRESH_REPLACEMENT {
		t.Fatalf("Done Move failed candidate: %+v, %v", result, err)
	}
	nodes, err := f.store.ListCurrentNodes(context.Background(), f.task.ID)
	if err != nil || len(nodes) != 1 || nodes[0].Scheduling != nil {
		t.Fatalf("failed replacement changed Done: %+v, %v", nodes, err)
	}
	failedID := retained.Details.Worktree.Kent.WorktreeId
	if target := f.assertEvidence(t); target.Task.ManagedWorktreeID != nil && *target.Task.ManagedWorktreeID == failedID {
		t.Fatal("failed replacement became the Task target")
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	branch = "done-fresh-target"
	result, err = f.app.WorkflowClient().MoveWorkflowTask(context.Background(), request)
	if err != nil || result.Applied == nil {
		t.Fatalf("fresh Done Move attempt failed: %+v, %v", result, err)
	}
	waitRecoveryModel(t, f.second, f.store, f.task.ID)
	if _, err := f.app.MetadataStore().GetWorktreeRecordByID(context.Background(), failedID); err != nil {
		t.Fatalf("fresh attempt lost failed candidate: %v", err)
	}
}

func TestWorkflowMoveClientCancellationDoesNotCancelReplacementSetup(t *testing.T) {
	f := newTaskRecoveryFixture(t)
	f.makeOriginalUnavailable(t)
	attempts := filepath.Join(t.TempDir(), "attempts")
	release := filepath.Join(t.TempDir(), "release")
	f.setup(t, fmt.Sprintf("printf x >> %q\nwhile [ ! -f %q ]; do sleep 0.01; done\nexit 1\n", attempts, release))
	t.Cleanup(func() { _ = os.WriteFile(release, []byte("release"), 0o600) })
	caller, cancel := context.WithCancel(context.Background())
	defer cancel()
	branch := "canceled-client-target"
	result := testsetup.Start(func() (serverapi.WorkflowTaskMoveResponse, error) {
		return f.app.WorkflowClient().MoveWorkflowTask(caller, serverapi.WorkflowTaskMoveRequest{
			TaskID: string(f.task.ID), TargetNodeID: string(f.moveTarget), BranchName: &branch,
			ExecutionTarget: &serverapi.WorkflowExecutionTargetSelection{Mode: serverapi.WorkflowExecutionTargetModeHead},
		})
	})
	testsetup.RequireUntil(t, time.Now().Add(10*time.Second), 10*time.Millisecond, func() bool {
		data, err := os.ReadFile(attempts)
		return err == nil && len(data) == 1
	}, "replacement setup did not start")
	cancel()
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case outcome := <-result:
		var retained *serverapi.WorkflowSetupRetainedError
		if !errors.As(outcome.Err, &retained) || outcome.Value.Applied != nil ||
			retained.Details.RecoveryDisposition != worktreepb.SetupRecoveryDisposition_SETUP_RECOVERY_DISPOSITION_FRESH_REPLACEMENT {
			t.Fatalf("accepted Move lost its setup outcome after client cancellation: %+v, %v", outcome.Value, outcome.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("accepted Move did not finish")
	}
	if target := f.assertEvidence(t); target.Task.ManagedWorktreeID == nil || *target.Task.ManagedWorktreeID != f.original.ID {
		t.Fatal("failed candidate was adopted after client cancellation")
	}
}

func addCoreRecoveryMoveTarget(t *testing.T, store *workflowstore.Store, taskID workflow.TaskID, source workflow.NodeID) workflow.NodeID {
	t.Helper()
	ctx := context.Background()
	targetContext, err := store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	workflowID := targetContext.Task.WorkflowID
	target := workflow.NodeID(runtimeids.NewGraphEntityID())
	incoming := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
	outgoing := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
	workflowfixture.SaveStoreGraph(t, ctx, store, workflowID, func(definition workflow.Definition, request *workflowstore.WorkflowGraphSaveRequest) {
		var terminal *workflow.NodeID
		for _, node := range definition.Nodes {
			if node.Kind() == workflow.NodeKindTerminal {
				id := workflow.NodeIDOf(node)
				terminal = &id
			}
		}
		if terminal == nil {
			t.Fatal("fixture has no Done Node")
		}
		request.Nodes = append(request.Nodes, workflowstore.NodeRecord{
			ID: target, WorkflowID: workflowID, Key: "move_target", Kind: workflow.NodeKindAgent,
			DisplayName: "Move target", SubagentRole: "default",
		})
		request.TransitionGroups = append(request.TransitionGroups,
			workflowstore.TransitionGroupRecord{ID: incoming, WorkflowID: workflowID, SourceNodeID: source, TransitionID: "move", DisplayName: "Move"},
			workflowstore.TransitionGroupRecord{ID: outgoing, WorkflowID: workflowID, SourceNodeID: target, TransitionID: "finish", DisplayName: "Done"},
		)
		request.Edges = append(request.Edges,
			workflowstore.EdgeRecord{ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: workflowID, TransitionGroupID: incoming,
				Key: "move", TargetNodeID: target, ContextMode: workflow.ContextModeNewSession,
				AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured, PromptTemplate: "Continue work."},
			workflowstore.EdgeRecord{ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: workflowID, TransitionGroupID: outgoing,
				Key: "finish", TargetNodeID: *terminal, ContextMode: workflow.ContextModeNewSession,
				AssigneeSelection: workflow.AssigneeSelectionConfigured, ThinkingSelection: workflow.ThinkingSelectionConfigured},
		)
	})
	return target
}

func (f *taskRecoveryFixture) waitInterrupted(t *testing.T) workflow.CurrentNode {
	t.Helper()
	var current []workflow.CurrentNode
	testsetup.RequireUntil(t, time.Now().Add(10*time.Second), 10*time.Millisecond, func() bool {
		var err error
		current, err = f.store.ListCurrentNodes(context.Background(), f.task.ID)
		return err == nil && len(current) == 1 && current[0].Scheduling != nil && current[0].Scheduling.Interruption != nil
	}, "Task preparation did not persist interruption")
	return current[0]
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
