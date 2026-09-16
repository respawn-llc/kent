package core

import (
	"context"
	"database/sql"
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
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/worktreecontract"
)

func TestWorkflowMissingTargetReplacementUsesSourceWorkspace(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeNone, replacementWithoutSetupFailure)
}

func TestWorkflowMissingTargetReplacementAttachesRetainedBranch(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeCustomRef, replacementWithoutSetupFailure)
}

func TestWorkflowMissingTargetReplacementCreatesFreshBranch(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementWithoutSetupFailure)
}

func TestWorkflowReplacementSetupFailureRetriesRetainedTarget(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementRetrySetup)
}

func TestWorkflowReplacementSetupFailureCanChooseSourceWorkspace(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementChooseSourceWorkspace)
}

func TestWorkflowMoveReplacementStopsWorkAndRetainsMoveOnSetupFailure(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementMoveRetrySetup)
}

func TestWorkflowMoveReplacementFailureCanResumeOriginalCurrentNode(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementMoveResumeSetup)
}

func TestWorkflowMoveFromDoneAppliesDestinationBeforeReplacementSetup(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementMoveFromDone)
}

func TestWorkflowReplacementSetupFailureCanChooseFreshBranch(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementChooseFreshBranch)
}

func TestWorkflowReplacementPreservesSetupChangedCheckout(t *testing.T) {
	testWorkflowMissingTargetReplacement(t, serverapi.WorkflowExecutionTargetModeHead, replacementPreserveChangedCheckout)
}

type replacementRecoveryAction uint8

const (
	replacementWithoutSetupFailure replacementRecoveryAction = iota
	replacementRetrySetup
	replacementChooseSourceWorkspace
	replacementMoveRetrySetup
	replacementMoveResumeSetup
	replacementMoveFromDone
	replacementChooseFreshBranch
	replacementPreserveChangedCheckout
)

func testWorkflowMissingTargetReplacement(t *testing.T, mode serverapi.WorkflowExecutionTargetMode, recoveryAction replacementRecoveryAction) {
	t.Helper()
	ctx := context.Background()
	moving := recoveryAction == replacementMoveRetrySetup || recoveryAction == replacementMoveResumeSetup || recoveryAction == replacementMoveFromDone
	fromDone := recoveryAction == replacementMoveFromDone
	var store *workflowstore.Store
	var taskID workflow.TaskID
	stoppedTarget := make(chan *string, 1)
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
	firstStep := scriptedllm.FinalAnswer(`{"transition":"done","commentary":"completed"}`)
	firstStep.BeforeResponse = func(ctx context.Context) error {
		err := first.ArriveAndWait(ctx)
		if moving {
			target, readErr := store.GetTaskExecutionTargetContext(context.Background(), taskID)
			if readErr != nil {
				return errors.Join(err, readErr)
			}
			stoppedTarget <- target.Task.ManagedWorktreeID
		}
		return err
	}
	secondStep := scriptedllm.FinalAnswer(`{"commentary":"completed"}`)
	if recoveryAction == replacementMoveResumeSetup {
		secondStep = scriptedllm.FinalAnswer(`{"transition":"done","commentary":"completed"}`)
	}
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
	store, err = workflowstore.New(app.MetadataStore(), workflowstore.WithRoleResolver(configRoleResolver{settings: resolved.Config.Settings}))
	if err != nil {
		t.Fatal(err)
	}
	taskID = createCoreInitialBranchTask(t, store, binding.ProjectID)
	nodes, err := store.ListCurrentNodes(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	origin := nodes[0].Reference
	var moveTarget *workflow.NodeID
	if moving {
		value := addCoreRecoveryMoveTarget(t, store, taskID, origin.NodeID)
		moveTarget = &value
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
	if moving && !fromDone {
		testsetup.RunGit(t, binding.CanonicalRoot, "worktree", "remove", "--force", old.CanonicalRoot)
	} else {
		if fromDone {
			first.Unblock()
			testsetup.RequireUntil(t, time.Now().Add(10*time.Second), 10*time.Millisecond, func() bool {
				current, readErr := store.ListCurrentNodes(ctx, taskID)
				live, liveErr := app.bundles.Runtime.runtimeAuthority.HasLiveWorkflowTaskExecution(taskID)
				if readErr == nil && liveErr == nil && len(current) == 1 && current[0].Scheduling == nil && !live {
					origin = current[0].Reference
					return true
				}
				return false
			}, "source Task did not reach quiescent Done")
		} else {
			if _, err := api.InterruptWorkflowTask(ctx, serverapi.WorkflowTaskInterruptRequest{TaskID: string(taskID), Reason: "user_interrupt"}); err != nil {
				t.Fatal(err)
			}
		}
		_, err = app.WorktreeClient().DeleteWorktree(ctx, &worktreepb.DeleteRequest{
			Scope:    worktreecontract.WorkspaceManagementScope(binding.ProjectID, binding.WorkspaceID, nil),
			Selector: oldID, BranchCleanupPolicy: worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_RETAIN,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	selection := &serverapi.WorkflowExecutionTargetSelection{Mode: mode}
	if mode == serverapi.WorkflowExecutionTargetModeCustomRef {
		selection.CustomRef = &ref
	}
	var branchName *string
	selectedCommit := commit
	if mode == serverapi.WorkflowExecutionTargetModeHead {
		var err error
		if moving {
			_, err = api.MoveWorkflowTask(ctx, serverapi.WorkflowTaskMoveRequest{
				TaskID: string(taskID), TargetNodeID: string(*moveTarget), ExecutionTarget: selection,
			})
		} else {
			_, err = api.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{
				TaskID: string(taskID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(), ExecutionTarget: selection,
			})
		}
		var collision *serverapi.WorkflowTaskInitialBranchError
		if !errors.As(err, &collision) || collision.Reason != serverapi.WorkflowTaskInitialBranchErrorReasonLocalCollision {
			t.Fatalf("occupied replacement branch was not rejected before preparation: %v", err)
		}
		name := "replacement-branch"
		branchName = &name
		selectedCommit = strings.TrimSpace(testsetup.RunGit(t, binding.CanonicalRoot, "rev-parse", "HEAD"))
	}
	var permitPath, attemptsPath string
	if recoveryAction != replacementWithoutSetupFailure {
		setupRoot := t.TempDir()
		permitPath = filepath.Join(setupRoot, "permit")
		attemptsPath = filepath.Join(setupRoot, "attempts")
		scriptPath := filepath.Join(setupRoot, "setup.sh")
		script := fmt.Sprintf("#!/bin/sh\nprintf x >> %q\ntest -f %q\n", attemptsPath, permitPath)
		if recoveryAction == replacementPreserveChangedCheckout {
			script = fmt.Sprintf("#!/bin/sh\nprintf x >> %q\ngit checkout -b setup_user_branch\nexit 1\n", attemptsPath)
		}
		if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		configDir := filepath.Join(binding.CanonicalRoot, ".kent")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(fmt.Sprintf("[worktrees]\nsetup_script = %q\n", scriptPath)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var response serverapi.WorkflowTaskResumeResponse
	var moveRequest *serverapi.WorkflowTaskMoveRequest
	var moveFailure *serverapi.WorkflowSetupRetainedError
	if moving {
		moveRequest = &serverapi.WorkflowTaskMoveRequest{
			TaskID: string(taskID), TargetNodeID: string(*moveTarget), ExecutionTarget: selection,
			BranchName: branchName, Commentary: "Move after cleanup",
		}
		moved, err := api.MoveWorkflowTask(ctx, *moveRequest)
		if !errors.As(err, &moveFailure) || moved.Applied != nil || moveFailure.SetupOperationID == nil {
			t.Fatalf("Move did not stop work and leave failed setup unapplied: %+v, %v", moved, err)
		}
		if (moveFailure.AppliedMove != nil) != fromDone {
			t.Fatalf("Move failure reported wrong applied disposition: %+v", moveFailure)
		}
		select {
		case atStop := <-stoppedTarget:
			if atStop == nil || *atStop != oldID {
				t.Fatalf("target changed while source execution was active: %v", atStop)
			}
		default:
			t.Fatal("source execution was not stopped")
		}
		current, err := store.ListCurrentNodes(ctx, taskID)
		expectedNode := origin.NodeID
		if fromDone {
			expectedNode = *moveTarget
		}
		if err != nil || len(current) != 1 || current[0].Reference.NodeID != expectedNode {
			t.Fatalf("failed Move changed original Current Node: %+v, %v", current, err)
		}
	} else {
		response, err = api.ResumeWorkflowTask(ctx, serverapi.WorkflowTaskResumeRequest{
			TaskID: string(taskID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
			ExecutionTarget: selection, BranchName: branchName,
		})
		if err != nil || response.Outcome != serverapi.WorkflowExecutionTargetActionOutcomeApplied {
			t.Fatalf("explicit recovery: %+v, %v", response, err)
		}
	}
	var attemptsBefore []byte
	var failedTargetID *string
	if recoveryAction != replacementWithoutSetupFailure {
		testsetup.RequireUntil(t, time.Now().Add(10*time.Second), 10*time.Millisecond, func() bool {
			current, err := store.ListCurrentNodes(ctx, taskID)
			return err == nil && len(current) == 1 && current[0].Scheduling != nil &&
				current[0].Scheduling.Interruption != nil && current[0].Scheduling.Interruption.Detail.SetupRecovery != nil
		}, "replacement setup failure was not persisted")
		failed, err := store.GetTaskExecutionTargetContext(ctx, taskID)
		if err != nil || failed.Task.ManagedWorktreeID == nil || *failed.Task.ManagedWorktreeID == oldID {
			t.Fatalf("failed replacement was not retained as current: %+v, %v", failed, err)
		}
		failedTargetID = failed.Task.ManagedWorktreeID
		if moving {
			current, err := store.ListCurrentNodes(ctx, taskID)
			if err != nil || current[0].Scheduling.Interruption.Detail.SetupRecovery.SetupOperationID != moveFailure.SetupOperationID.UUID {
				t.Fatalf("Move failure correlation did not match canonical recovery: %+v, %v", current, err)
			}
		}
		if len(client.Requests()) != 1 {
			t.Fatal("executable work started before setup succeeded")
		}
		attemptsBefore, err = os.ReadFile(attemptsPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(attemptsBefore) != 1 {
			t.Fatalf("Task action ran setup %d times, want one attempt", len(attemptsBefore))
		}
		if err := os.WriteFile(permitPath, []byte("permitted"), 0o600); err != nil {
			t.Fatal(err)
		}
		retry := serverapi.WorkflowTaskResumeRequest{
			TaskID: string(taskID), SetupOperationID: serverapi.NewWorkflowSetupOperationID(),
		}
		if recoveryAction == replacementChooseSourceWorkspace || recoveryAction == replacementPreserveChangedCheckout {
			mode = serverapi.WorkflowExecutionTargetModeNone
			retry.ExecutionTarget = &serverapi.WorkflowExecutionTargetSelection{Mode: mode}
		}
		if recoveryAction == replacementChooseFreshBranch {
			name := "second-replacement"
			branchName = &name
			retry.ExecutionTarget = &serverapi.WorkflowExecutionTargetSelection{Mode: mode}
			retry.BranchName = branchName
		}
		if recoveryAction == replacementMoveRetrySetup {
			moved, err := api.MoveWorkflowTask(ctx, *moveRequest)
			if err != nil || moved.Outcome != serverapi.WorkflowExecutionTargetActionOutcomeApplied {
				t.Fatalf("retry original Move: %+v, %v", moved, err)
			}
		} else {
			response, err = api.ResumeWorkflowTask(ctx, retry)
			if err != nil || response.Outcome != serverapi.WorkflowExecutionTargetActionOutcomeApplied {
				t.Fatalf("retry observed setup failure: %+v, %v", response, err)
			}
		}
	}
	waitRecoveryModel(t, second, store, taskID)
	after, err := store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if recoveryAction == replacementRetrySetup || moving {
		attempts, err := os.ReadFile(attemptsPath)
		if err != nil || len(attempts) <= len(attemptsBefore) {
			t.Fatalf("observed setup failure was skipped on Resume: before=%d after=%d error=%v", len(attemptsBefore), len(attempts), err)
		}
		if after.Task.ManagedWorktreeID == nil || *after.Task.ManagedWorktreeID != *failedTargetID {
			t.Fatalf("setup retry abandoned its retained replacement: %v", after.Task.ManagedWorktreeID)
		}
	}
	if recoveryAction == replacementChooseFreshBranch &&
		(after.Task.ManagedWorktreeID == nil || *after.Task.ManagedWorktreeID == *failedTargetID) {
		t.Fatal("choosing a different branch reused the failed replacement identity")
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
		if recoveryAction == replacementPreserveChangedCheckout {
			retained, err := app.MetadataStore().GetWorktreeRecordByID(ctx, *failedTargetID)
			if err != nil || retained.CanonicalRoot != old.CanonicalRoot {
				t.Fatalf("setup-changed artifact was not retained: %+v, %v", retained, err)
			}
			if branch := strings.TrimSpace(testsetup.RunGit(t, old.CanonicalRoot, "branch", "--show-current")); branch != "setup_user_branch" {
				t.Fatalf("setup checkout was changed: %q", branch)
			}
		} else if _, err := os.Stat(old.CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old root was recreated: %v", err)
		}
	} else {
		if after.Task.ManagedWorktreeID == nil || *after.Task.ManagedWorktreeID == oldID {
			t.Fatalf("replacement did not create a fresh binding: %v", after.Task.ManagedWorktreeID)
		}
		record, err := app.MetadataStore().GetWorktreeRecordByID(ctx, *after.Task.ManagedWorktreeID)
		if err != nil || record.CanonicalRoot != old.CanonicalRoot ||
			record.CreationBaseCommitOID == nil || *record.CreationBaseCommitOID != selectedCommit {
			t.Fatalf("replacement did not reuse normal root with fresh base facts: %+v, %v", record, err)
		}
		actual, err := os.ReadFile(filepath.Join(record.CanonicalRoot, "retained.txt"))
		if mode == serverapi.WorkflowExecutionTargetModeCustomRef && (err != nil || string(actual) != string(content)) {
			t.Fatalf("retained branch content lost: %q, %v", actual, err)
		}
		if mode == serverapi.WorkflowExecutionTargetModeHead {
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fresh source HEAD replacement reused old branch content: %v", err)
			}
			if actualBranch := strings.TrimSpace(testsetup.RunGit(t, record.CanonicalRoot, "branch", "--show-current")); actualBranch != *branchName {
				t.Fatalf("fresh replacement branch = %q, want %q", actualBranch, *branchName)
			}
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
	if recoveryAction == replacementMoveResumeSetup && !nodes[0].Reference.Equal(origin) {
		t.Fatal("ordinary Resume applied the previously failed Move")
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
	expectedSessions := int64(1)
	if recoveryAction == replacementMoveRetrySetup || fromDone {
		expectedSessions = 2
	}
	if count, err := store.CountTaskSessions(ctx, taskID); err != nil || count != expectedSessions {
		t.Fatalf("source Session association was lost: %d, %v", count, err)
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
