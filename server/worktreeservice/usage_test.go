package worktree

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/internal/testharness/workflowfixture"
	"core/server/agentruntime"
	"core/server/llm"
	"core/server/metadata"
	"core/server/registry"
	"core/server/runtimeactivity"
	"core/server/sessionruntime"
	"core/server/workflowstore"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/textutil"
	"core/shared/toolspec"

	"github.com/google/uuid"
)

func TestListWorktreesMarksNonterminalTaskWorktreeInUse(t *testing.T) {
	env := newServiceTestEnv(t)
	task, _ := createTaskWorktreeTestTask(t, env)
	materialized, err := materializeInitialTaskWorktree(env.ctx, env.service, InitialTaskWorktreeMaterializationRequest{
		TaskID:         task.ID,
		ResolvedTarget: resolveTaskWorktreeTestHEAD(t, env, env.workspaceRoot),
	})
	if err != nil {
		t.Fatalf("materialize task Worktree: %v", err)
	}

	list, err := env.service.ListWorktrees(env.ctx, &worktreepb.ListRequest{
		SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: env.session.Meta().SessionID}},
	})
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	entry := listEntryForWorktreeID(t, list.Worktrees, taskWorktreeID(materialized.Worktree))
	if !entry.GetProjection().GetIsInUse() {
		t.Fatalf("Task Worktree projection = %+v, want In Use", entry.GetProjection())
	}

	workspaceList, err := env.service.ListWorkspaceWorktrees(env.ctx, &worktreepb.WorkspaceListRequest{
		ProjectId:   env.binding.ProjectID,
		WorkspaceId: env.binding.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("ListWorkspaceWorktrees: %v", err)
	}
	workspaceEntry := listEntryForWorktreeID(t, workspaceList.Worktrees, taskWorktreeID(materialized.Worktree))
	if !workspaceEntry.GetProjection().GetIsInUse() {
		t.Fatalf("workspace Task Worktree projection = %+v, want In Use", workspaceEntry.GetProjection())
	}

	resolved, err := env.service.ResolveWorktreeSelector(env.ctx, &worktreepb.SelectorResolveRequest{
		SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: env.session.Meta().SessionID}},
		Selector:  entry.GetProjection().GetSelector(),
	})
	if err != nil {
		t.Fatalf("ResolveWorktreeSelector: %v", err)
	}
	if !resolved.GetWorktree().GetProjection().GetIsInUse() {
		t.Fatalf("resolved Task Worktree projection = %+v, want In Use", resolved.GetWorktree().GetProjection())
	}
}

func TestListWorktreesDoesNotMarkWorktreeWithoutTasksInUse(t *testing.T) {
	env := newServiceTestEnv(t)
	worktree := mustCreateWorktree(t, env, "feature/unused")

	entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktree.WorktreeID)
	if entry.GetProjection().GetIsInUse() {
		t.Fatalf("Worktree projection = %+v, want unused", entry.GetProjection())
	}
}

func TestListWorktreesIgnoresMissingSessionFromStaleRuntimeSnapshot(t *testing.T) {
	env := newServiceTestEnv(t)
	worktree := mustCreateWorktree(t, env, "feature/stale-runtime-session")
	env.service.runtimeActivitySnapshotReader = fixedRuntimeActivitySnapshotReader{{
		SessionID: uuid.NewString(),
		Activity:  &runtimepb.Activity{State: runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING},
	}}

	entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktree.WorktreeID)
	if entry.GetProjection().GetIsInUse() {
		t.Fatalf("Worktree projection = %+v, want unused without a persisted Session target", entry.GetProjection())
	}
}

func TestListWorktreesExcludesTerminalTaskFromInUse(t *testing.T) {
	env := newServiceTestEnv(t)
	task, materialized, _ := materializeAndLockTaskWorktree(t, env)
	completeWorktreeTask(t, env, task)

	entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, taskWorktreeID(materialized.Worktree))
	if entry.GetProjection().GetIsInUse() {
		t.Fatalf("terminal Task Worktree projection = %+v, want unused", entry.GetProjection())
	}
}

func TestListWorktreesMarksMissingTaskWorktreeInUse(t *testing.T) {
	env := newServiceTestEnv(t)
	task, _ := createTaskWorktreeTestTask(t, env)
	materialized, err := materializeInitialTaskWorktree(env.ctx, env.service, InitialTaskWorktreeMaterializationRequest{
		TaskID:         task.ID,
		ResolvedTarget: resolveTaskWorktreeTestHEAD(t, env, env.workspaceRoot),
	})
	if err != nil {
		t.Fatalf("materialize task Worktree: %v", err)
	}
	worktreeID := taskWorktreeID(materialized.Worktree)
	if err := env.service.git.Remove(env.ctx, env.workspaceRoot, taskWorktreeRoot(materialized.Worktree), true); err != nil {
		t.Fatalf("remove task Worktree: %v", err)
	}

	entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktreeID)
	if entry.GetTopology().GetMissing() == nil || !entry.GetProjection().GetIsInUse() {
		t.Fatalf("Missing Task Worktree entry = %+v, want Missing and In Use", entry)
	}
}

func TestListWorktreesMarksActiveSessionOnRegisteredExternalWorktreeInUse(t *testing.T) {
	env := newServiceTestEnv(t)
	registeredRoot, _, gitMetadata := createExistingOutsideManagedWorktree(t, env, "feature/registered-external")
	unregisteredRoot, _, _ := createExistingOutsideManagedWorktree(t, env, "feature/unregistered-external")
	worktreeID := uuid.NewString()
	if err := env.store.UpsertWorktreeRecord(env.ctx, metadata.WorktreeRecord{
		ID: worktreeID, WorkspaceID: env.binding.WorkspaceID,
		CanonicalRoot: registeredRoot, DisplayName: "Registered external",
		GitMetadataJSON: gitMetadata, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("register externally created Worktree: %v", err)
	}
	sessionID := env.session.Meta().SessionID
	updateServiceTestSessionTarget(t, env, sessionID, env.binding.WorkspaceID, worktreeID, ".")

	client := newDeleteActivityGatedLLMClient()
	handle := startWorktreeUsageSessionExecution(t, env, sessionID, registeredRoot, client, nil)
	executionStopped := false
	t.Cleanup(func() {
		if !executionStopped {
			stopWorktreeUsageSessionExecution(t, handle, client)
		}
	})
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active Session runtime")
	}

	list := mustListWorktrees(t, env)
	registeredEntry := listEntryForWorktreeID(t, list.Worktrees, worktreeID)
	if registeredEntry.GetTopology().GetRegistered().GetKent().GetManaged() ||
		!registeredEntry.GetProjection().GetIsCurrent() ||
		!registeredEntry.GetProjection().GetIsInUse() {
		t.Fatalf("registered external Worktree entry = %+v, want current and In Use without Kent ownership", registeredEntry)
	}
	workspaceList, err := env.service.ListWorkspaceWorktrees(env.ctx, &worktreepb.WorkspaceListRequest{
		ProjectId:   env.binding.ProjectID,
		WorkspaceId: env.binding.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("ListWorkspaceWorktrees: %v", err)
	}
	workspaceEntry := listEntryForWorktreeID(t, workspaceList.Worktrees, worktreeID)
	if !workspaceEntry.GetProjection().GetIsInUse() {
		t.Fatalf("workspace registered external Worktree projection = %+v, want In Use", workspaceEntry.GetProjection())
	}
	for _, entry := range list.Worktrees {
		if entry.GetTopology().GetMainWorkspace() != nil && entry.GetProjection().GetIsInUse() {
			t.Fatalf("Main Workspace projection = %+v, want not In Use", entry.GetProjection())
		}
		if entry.GetTopology().GetExternal() != nil &&
			entry.GetTopology().GetExternal().GetGit().GetCanonicalRoot() == unregisteredRoot &&
			entry.GetProjection().GetIsInUse() {
			t.Fatalf("unregistered External projection = %+v, want not In Use", entry.GetProjection())
		}
	}

	stopWorktreeUsageSessionExecution(t, handle, client)
	executionStopped = true
	refreshed := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktreeID)
	if refreshed.GetProjection().GetIsInUse() {
		t.Fatalf("refreshed Worktree projection = %+v, want unused after Session becomes Idle", refreshed.GetProjection())
	}
}

func TestListWorktreesDoesNotMarkMainWorkspaceForActiveSession(t *testing.T) {
	env := newServiceTestEnv(t)
	externalRoot, _, _ := createExistingOutsideManagedWorktree(t, env, "feature/main-session-external")
	sessionID := env.session.Meta().SessionID
	client := newDeleteActivityGatedLLMClient()
	handle := startWorktreeUsageSessionExecution(t, env, sessionID, env.workspaceRoot, client, nil)
	executionStopped := false
	t.Cleanup(func() {
		if !executionStopped {
			stopWorktreeUsageSessionExecution(t, handle, client)
		}
	})
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active Main Workspace Session")
	}

	list := mustListWorktrees(t, env)
	var mainWorkspace *worktreepb.ListEntry
	for _, entry := range list.Worktrees {
		if entry.GetTopology().GetMainWorkspace() != nil {
			mainWorkspace = entry
		}
		if entry.GetTopology().GetExternal() != nil &&
			entry.GetTopology().GetExternal().GetGit().GetCanonicalRoot() == externalRoot &&
			entry.GetProjection().GetIsInUse() {
			t.Fatalf("unregistered External projection = %+v, want unused", entry.GetProjection())
		}
	}
	if mainWorkspace == nil ||
		!mainWorkspace.GetProjection().GetIsCurrent() ||
		mainWorkspace.GetProjection().GetIsInUse() {
		t.Fatalf("Main Workspace projection = %+v, want current and unused", mainWorkspace)
	}
	stopWorktreeUsageSessionExecution(t, handle, client)
	executionStopped = true
}

func TestListWorktreesKeepsTaskWorktreeInUseUntilBothSourcesEnd(t *testing.T) {
	env := newServiceTestEnv(t)
	task, materialized, _ := materializeAndLockTaskWorktree(t, env)
	worktreeID := taskWorktreeID(materialized.Worktree)
	worktreeRoot := taskWorktreeRoot(materialized.Worktree)
	sessionID := env.session.Meta().SessionID
	updateServiceTestSessionTarget(t, env, sessionID, env.binding.WorkspaceID, worktreeID, ".")

	client := newDeleteActivityGatedLLMClient()
	handle := startWorktreeUsageSessionExecution(t, env, sessionID, worktreeRoot, client, nil)
	executionStopped := false
	t.Cleanup(func() {
		if !executionStopped {
			stopWorktreeUsageSessionExecution(t, handle, client)
		}
	})
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for active Session runtime")
	}

	if entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktreeID); !entry.GetProjection().GetIsInUse() {
		t.Fatalf("Task and Session Worktree projection = %+v, want In Use", entry.GetProjection())
	}
	stopWorktreeUsageSessionExecution(t, handle, client)
	executionStopped = true
	if entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktreeID); !entry.GetProjection().GetIsInUse() {
		t.Fatalf("Task Worktree projection after Session becomes Idle = %+v, want In Use", entry.GetProjection())
	}

	completeWorktreeTask(t, env, task)
	if entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktreeID); entry.GetProjection().GetIsInUse() {
		t.Fatalf("Task Worktree projection after Task terminalizes = %+v, want unused", entry.GetProjection())
	}
}

func TestListWorktreesMarksSessionInUseWhileAwaitingAnswer(t *testing.T) {
	env := newServiceTestEnv(t)
	worktree := mustCreateWorktree(t, env, "feature/question-in-use")
	sessionID := env.session.Meta().SessionID
	updateServiceTestSessionTarget(t, env, sessionID, env.binding.WorkspaceID, worktree.WorktreeID, ".")

	client := &worktreeUsageQuestionLLMClient{started: make(chan struct{})}
	handle := startWorktreeUsageSessionExecution(
		t,
		env,
		sessionID,
		worktree.CanonicalRoot,
		client,
		[]toolspec.ID{toolspec.ToolAskQuestion},
	)
	executionStopped := false
	t.Cleanup(func() {
		if !executionStopped {
			handle.RequestStop()
			waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := handle.Wait(waitCtx); err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("wait for canceled Session execution: %v", err)
			}
		}
	})
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Session model request")
	}
	waitForWorktreeUsageActivityState(
		t,
		env.runtimeRegistry,
		sessionID,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT,
		handle,
	)

	entry := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktree.WorktreeID)
	if !entry.GetProjection().GetIsInUse() {
		t.Fatalf("Worktree projection = %+v, want In Use while awaiting an answer", entry.GetProjection())
	}

	handle.RequestStop()
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := handle.Wait(waitCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait for stopped Session execution = %v, want context canceled", err)
	}
	executionStopped = true
	refreshed := listEntryForWorktreeID(t, mustListWorktrees(t, env).Worktrees, worktree.WorktreeID)
	if refreshed.GetProjection().GetIsInUse() {
		t.Fatalf("refreshed Worktree projection = %+v, want unused after Session stops", refreshed.GetProjection())
	}
}

func listEntryForWorktreeID(t *testing.T, entries []*worktreepb.ListEntry, worktreeID string) *worktreepb.ListEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.GetTopology().GetRegistered().GetKent().GetWorktreeId() == worktreeID ||
			entry.GetTopology().GetMissing().GetKent().GetWorktreeId() == worktreeID {
			return entry
		}
	}
	t.Fatalf("Worktree %q not found in %+v", worktreeID, entries)
	return nil
}

type fixedRuntimeActivitySnapshotReader []runtimeactivity.ActiveSessionSnapshot

func (r fixedRuntimeActivitySnapshotReader) ActiveRuntimeActivitySnapshots(context.Context) ([]runtimeactivity.ActiveSessionSnapshot, error) {
	return r, nil
}

func completeWorktreeTask(t *testing.T, env *serviceTestEnv, task workflowstore.TaskRecord) {
	t.Helper()
	store, err := workflowstore.New(env.store, workflowstore.WithRoleResolver(testsetup.QuestionsEnabled("workflow-test")))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	nodes, err := store.ListCurrentNodes(env.ctx, task.ID)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("Task current Nodes = %+v: %v", nodes, err)
	}
	if _, err := workflowfixture.CompleteCurrentNode(t, env.ctx, env.store, store, workflowstore.CurrentNodeCompletionRequest{
		Source: nodes[0].Reference, TransitionID: "done",
	}); err != nil {
		t.Fatalf("complete Task: %v", err)
	}
}

func startWorktreeUsageSessionExecution(
	t *testing.T,
	env *serviceTestEnv,
	sessionID string,
	worktreeRoot string,
	client llm.Client,
	enabledTools []toolspec.ID,
) sessionruntime.ExecutionHandle {
	t.Helper()
	plan := deleteActivityRuntimePlanWithTools(t, env, worktreeRoot, client, "off", nil, enabledTools, nil)
	handle, err := env.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
		Descriptor: openDeleteActivitySessionDescriptor(t, sessionID),
		Runtime:    &plan,
		Resource:   sessionruntime.OpenAgentResource{},
		Runner: func(ctx context.Context, _ sessionruntime.ExecutionScope, bridge sessionruntime.AgentRuntimeBridge) error {
			return bridge.WithEngine(ctx, func(engineCtx context.Context, engine *runtime.Engine) error {
				_, err := engine.SubmitUserMessage(engineCtx, "work")
				return err
			})
		},
	})
	if err != nil {
		t.Fatalf("StartAgentExecution: %v", err)
	}
	return handle
}

func stopWorktreeUsageSessionExecution(t *testing.T, handle sessionruntime.ExecutionHandle, client *deleteActivityGatedLLMClient) {
	t.Helper()
	client.unblock()
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := handle.Wait(waitCtx); err != nil {
		t.Fatalf("wait for Session execution: %v", err)
	}
}

func waitForWorktreeUsageActivityState(
	t *testing.T,
	runtimeRegistry *registry.RuntimeRegistry,
	sessionID string,
	state runtimepb.ActivityState,
	handle sessionruntime.ExecutionHandle,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		snapshots, err := runtimeRegistry.ActiveRuntimeActivitySnapshots(ctx)
		if err != nil {
			t.Fatalf("read active Session activity snapshots: %v", err)
		}
		for _, snapshot := range snapshots {
			if snapshot.SessionID == sessionID && snapshot.Activity.GetState() == state {
				return
			}
		}
		select {
		case <-ctx.Done():
			view, available := runtimeRegistry.RuntimeMainViewSnapshot(sessionID)
			select {
			case <-handle.Done():
				_, runErr := handle.Wait(context.Background())
				t.Fatalf(
					"Session %q finished before activity state %s; execution error: %v; main view: %+v (available=%t); pending prompts: %+v",
					sessionID,
					state,
					runErr,
					view,
					available,
					runtimeRegistry.ListPendingPrompts(sessionID),
				)
			default:
			}
			t.Fatalf(
				"Session %q did not reach activity state %s; active snapshots: %+v; main view: %+v (available=%t); pending prompts: %+v",
				sessionID,
				state,
				snapshots,
				view,
				available,
				runtimeRegistry.ListPendingPrompts(sessionID),
			)
		case <-ticker.C:
		}
	}
}

type worktreeUsageQuestionLLMClient struct {
	calls   atomic.Int32
	started chan struct{}
}

func (c *worktreeUsageQuestionLLMClient) Generate(context.Context, llm.Request, llm.StreamCallbacks) (llm.Response, error) {
	if c.calls.Add(1) == 1 {
		close(c.started)
		return llm.Response{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Content: textutil.Value("working"),
				Phase:   textutil.Value(llm.MessagePhaseCommentary),
			},
			ToolCalls: []llm.ToolCall{{
				ID:    "worktree-usage-question",
				Name:  string(toolspec.ToolAskQuestion),
				Input: json.RawMessage(`{"question":"Proceed?"}`),
			}},
			Usage: llm.Usage{WindowTokens: 200000},
		}, nil
	}
	return llm.Response{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Content: textutil.Value("finished"),
			Phase:   textutil.Value(llm.MessagePhaseFinal),
		},
		Usage: llm.Usage{WindowTokens: 200000},
	}, nil
}

func (*worktreeUsageQuestionLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.InferProviderCapabilities("openai")
}
