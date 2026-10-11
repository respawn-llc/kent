package worktree

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/llm"
	"core/server/metadata"
	"core/server/runtime"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/sessionruntime"
	"core/server/tools"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/clientui"
	"core/shared/config"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/worktreecontract"
	"google.golang.org/protobuf/proto"
)

type deleteInFlightStartLifecycle struct {
	*testsetup.StartBarrier
}

func (l *deleteInFlightStartLifecycle) ResourceReady(ctx context.Context, _ sessionruntime.AgentResourceDescriptor, _ *runtime.Engine, _ sessionruntime.AgentResourceRetainer) error {
	return l.ArriveAndWait(ctx)
}

func (l *deleteInFlightStartLifecycle) ResourceDraining(context.Context, sessionruntime.AgentResourceDescriptor) error {
	return nil
}

type deleteActivityTestLLMClient struct{}

func (deleteActivityTestLLMClient) Generate(context.Context, llm.Request, llm.StreamCallbacks) (llm.Response, error) {
	return llm.Response{
		Assistant: llm.Message{
			Role:    llm.RoleAssistant,
			Content: textutil.Value("finished"),
			Phase:   textutil.Value(llm.MessagePhaseFinal),
		},
		Usage: llm.Usage{WindowTokens: 200000},
	}, nil
}

func (deleteActivityTestLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.InferProviderCapabilities("openai")
}

type deleteActivityObservedLLMClient struct {
	requests chan struct{}
}

func (c deleteActivityObservedLLMClient) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	select {
	case c.requests <- struct{}{}:
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
	return deleteActivityTestLLMClient{}.Generate(ctx, llm.Request{}, llm.StreamCallbacks{})
}

func (deleteActivityObservedLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.InferProviderCapabilities("openai")
}

type deleteActivityEditLLMClient struct {
	mu    sync.Mutex
	calls int
}

func (c *deleteActivityEditLLMClient) Generate(context.Context, llm.Request, llm.StreamCallbacks) (llm.Response, error) {
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.mu.Unlock()
	if call == 1 {
		return llm.Response{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Content: textutil.Value("editing the relative file"),
				Phase:   textutil.Value(llm.MessagePhaseCommentary),
			},
			ToolCalls: []llm.ToolCall{{
				ID:    "delete-retarget-relative-edit",
				Name:  string(toolspec.ToolEdit),
				Input: json.RawMessage(`{"path":"relative-edit.txt","old_string":"before","new_string":"after"}`),
			}},
			Usage: llm.Usage{WindowTokens: 200000},
		}, nil
	}
	return deleteActivityTestLLMClient{}.Generate(context.Background(), llm.Request{}, llm.StreamCallbacks{})
}

func (*deleteActivityEditLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.InferProviderCapabilities("openai")
}

type deleteActivityGatedLLMClient struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
}

func newDeleteActivityGatedLLMClient() *deleteActivityGatedLLMClient {
	return &deleteActivityGatedLLMClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *deleteActivityGatedLLMClient) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	c.startedOnce.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return deleteActivityTestLLMClient{}.Generate(ctx, llm.Request{}, llm.StreamCallbacks{})
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
}

func (c *deleteActivityGatedLLMClient) unblock() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func (*deleteActivityGatedLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.InferProviderCapabilities("openai")
}

type deleteActivityReviewerClient struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	releaseOnce sync.Once
}

func newDeleteActivityReviewerClient() *deleteActivityReviewerClient {
	return &deleteActivityReviewerClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *deleteActivityReviewerClient) Generate(ctx context.Context, _ llm.Request, _ llm.StreamCallbacks) (llm.Response, error) {
	c.startedOnce.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return llm.Response{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Content: textutil.Value(`{"suggestions":[]}`),
			},
			Usage: llm.Usage{WindowTokens: 200000},
		}, nil
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
}

func (*deleteActivityReviewerClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.InferProviderCapabilities("openai")
}

func (c *deleteActivityReviewerClient) Release() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func deleteActivityTestRuntimePlan(t *testing.T, env *serviceTestEnv, workdir string) sessionruntime.AgentRuntimePlan {
	return deleteActivityRuntimePlan(t, env, workdir, deleteActivityTestLLMClient{}, "off", nil)
}

func deleteActivityRuntimePlan(
	t *testing.T,
	env *serviceTestEnv,
	workdir string,
	client llm.Client,
	reviewerFrequency string,
	reviewerClientFactory runtimewire.RuntimeClientFactory,
	onEvent ...func(runtime.Event),
) sessionruntime.AgentRuntimePlan {
	t.Helper()
	settings := env.cfg.Settings
	settings.Model = "gpt-6-sol"
	settings.ModelContextWindow = 200000
	settings.Reviewer.Frequency = reviewerFrequency
	settings.Reviewer.Model = "gpt-6-sol"
	settings.Reviewer.ThinkingLevel = "low"
	var eventObserver func(runtime.Event)
	if len(onEvent) > 0 {
		eventObserver = onEvent[0]
	}
	plan, err := sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
		MainWorkspaceRoot:     workdir,
		Settings:              settings,
		QuestionsEnabled:      textutil.Value(true),
		AutoCompactionEnabled: textutil.Value(true),
		FilesystemContext: func() tools.FilesystemContext {
			context, err := runtimewire.NewFilesystemContext(workdir, workdir, "test")
			if err != nil {
				t.Fatalf("NewFilesystemContext: %v", err)
			}
			return context
		}(),
		Client:                client,
		ReviewerClientFactory: reviewerClientFactory,
		OnEvent:               eventObserver,
	})
	if err != nil {
		t.Fatalf("NewAgentRuntimePlan: %v", err)
	}
	return plan
}

type deleteTargetState struct {
	sessionTarget *worktreepb.SessionExecutionTarget
	reminder      *session.WorktreeReminderState
	topology      serviceTestWorktree
	record        metadata.WorktreeRecord
	git           GitWorktree
	root          string
}

type deleteActivityResult struct {
	result *worktreepb.DeleteSuccess
	err    error
}

type deleteRemovalBarrierRunner struct {
	delegate gitCommandRunner
	barrier  *testsetup.StartBarrier
}

func (r *deleteRemovalBarrierRunner) Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) >= 2 && args[0] == "worktree" && args[1] == "remove" {
		if err := r.barrier.ArriveAndWait(ctx); err != nil {
			return nil, err
		}
	}
	return r.delegate.Output(ctx, dir, args...)
}

func (r *deleteRemovalBarrierRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, int, error) {
	return r.delegate.Run(ctx, dir, args...)
}

func openDeleteActivitySessionDescriptor(t *testing.T, sessionID string) session.SessionDescriptor {
	t.Helper()
	id, err := runtimeids.ParseSessionID(sessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	descriptor, err := session.NewOpenSessionDescriptor(id)
	if err != nil {
		t.Fatalf("NewOpenSessionDescriptor: %v", err)
	}
	return descriptor
}

func readDeleteActivityReminder(t *testing.T, env *serviceTestEnv, sessionID string) *session.WorktreeReminderState {
	t.Helper()
	var reminder *session.WorktreeReminderState
	if err := env.authority.WithSessionStore(env.ctx, openDeleteActivitySessionDescriptor(t, sessionID), func(_ context.Context, store *session.Store) error {
		if current := store.Meta().WorktreeReminder; current != nil {
			reminder = session.CloneWorktreeReminderState(current)
		}
		return nil
	}); err != nil {
		t.Fatalf("read Worktree reminder for Session %q: %v", sessionID, err)
	}
	return reminder
}

func captureDeleteTargetState(t *testing.T, env *serviceTestEnv, sessionID string, worktree serviceTestWorktree) deleteTargetState {
	t.Helper()
	target, err := env.store.ResolveSessionExecutionTarget(env.ctx, sessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget before delete: %v", err)
	}
	record, err := env.store.GetWorktreeRecordByID(env.ctx, worktree.WorktreeID)
	if err != nil {
		t.Fatalf("GetWorktreeRecordByID before delete: %v", err)
	}
	git, found, err := env.service.git.FindCreatedWorktree(env.ctx, env.workspaceRoot, worktree.CanonicalRoot)
	if err != nil {
		t.Fatalf("FindCreatedWorktree before delete: %v", err)
	}
	if !found {
		t.Fatal("busy worktree is absent from Git before delete")
	}
	return deleteTargetState{
		sessionTarget: target,
		reminder:      readDeleteActivityReminder(t, env, sessionID),
		topology:      findWorktreeByID(t, mustListWorktrees(t, env).Worktrees, worktree.WorktreeID),
		record:        record,
		git:           git,
		root:          worktree.CanonicalRoot,
	}
}

func (state deleteTargetState) assertUnchanged(t *testing.T, env *serviceTestEnv, sessionID string, worktreeID string) {
	t.Helper()
	target, err := env.store.ResolveSessionExecutionTarget(env.ctx, sessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget after rejected delete: %v", err)
	}
	if !proto.Equal(target, state.sessionTarget) {
		t.Fatalf("busy session target changed after rejected delete: before=%+v after=%+v", state.sessionTarget, target)
	}
	topology := findWorktreeByID(t, mustListWorktrees(t, env).Worktrees, worktreeID)
	if !reflect.DeepEqual(topology, state.topology) {
		t.Fatalf("busy worktree topology changed after rejected delete: before=%+v after=%+v", state.topology, topology)
	}
	record, err := env.store.GetWorktreeRecordByID(env.ctx, worktreeID)
	if err != nil {
		t.Fatalf("GetWorktreeRecordByID after rejected delete: %v", err)
	}
	if !reflect.DeepEqual(record, state.record) {
		t.Fatalf("busy worktree metadata changed after rejected delete: before=%+v after=%+v", state.record, record)
	}
	git, found, err := env.service.git.FindCreatedWorktree(env.ctx, env.workspaceRoot, state.root)
	if err != nil {
		t.Fatalf("FindCreatedWorktree after rejected delete: %v", err)
	}
	if !found || !reflect.DeepEqual(git, state.git) {
		t.Fatalf("busy Git worktree changed after rejected delete: before=%+v after=%+v found=%t", state.git, git, found)
	}
	if _, err := os.Stat(state.root); err != nil {
		t.Fatalf("busy worktree root changed after rejected delete: %v", err)
	}
}

func (state deleteTargetState) assertReminderUnchanged(t *testing.T, env *serviceTestEnv, sessionID string) {
	t.Helper()
	current := readDeleteActivityReminder(t, env, sessionID)
	if (state.reminder == nil) != (current == nil) {
		t.Fatalf("session Worktree reminder presence changed after rejected delete: before=%+v after=%+v", state.reminder, current)
	}
	if state.reminder != nil && !session.WorktreeReminderStateEqual(*state.reminder, *current) {
		t.Fatalf("session Worktree reminder changed after rejected delete: before=%+v after=%+v", state.reminder, current)
	}
}

func deleteServiceTestWorktree(env *serviceTestEnv, worktreeID string) <-chan deleteActivityResult {
	deleted := make(chan deleteActivityResult, 1)
	go func() {
		result, err := env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, worktreeID))
		deleted <- deleteActivityResult{result: result, err: err}
	}()
	return deleted
}

func assertDeleteBlockedBySession(t *testing.T, err error, sessionID string) {
	t.Helper()
	if !errors.Is(err, worktreecontract.ErrWorktreeBlocked) {
		t.Errorf("DeleteWorktree error = %v, want ErrWorktreeBlocked", err)
	}
	var blocked *worktreecontract.BlockedError
	if !errors.As(err, &blocked) {
		t.Errorf("DeleteWorktree error = %v, want structured blocker details", err)
	} else {
		details := blocked.Details.GetActiveSessions()
		found := false
		if details != nil {
			for _, item := range details.Sessions {
				found = found || item.SessionId == sessionID
			}
		}
		if !found {
			t.Errorf("delete blocker details = %v, want Session %q", details, sessionID)
		}
	}
	var partial *worktreecontract.DeletePartialError
	if errors.As(err, &partial) {
		t.Errorf("delete moved %d Sessions before rejecting blocking activity", partial.RetargetedSessions)
	}
}

type deleteActivityTransitionGate struct {
	started     chan struct{}
	release     chan struct{}
	done        chan struct{}
	err         error
	releaseOnce sync.Once
}

func scheduleDeleteActivityTransition(t *testing.T, engine *runtime.Engine) *deleteActivityTransitionGate {
	t.Helper()
	gate := &deleteActivityTransitionGate{
		started: make(chan struct{}),
		release: make(chan struct{}),
		done:    make(chan struct{}),
	}
	operationID := clientui.NewWorktreeTransitionID()
	ack, err := engine.ScheduleWorktreeTransition(
		context.Background(),
		operationID,
		runtimeinput.PendingWorkWorktreeTransition{Transition: runtimeinput.PendingWorkWorktreeTransitionLeave},
		func(ctx context.Context) error {
			close(gate.started)
			var runErr error
			select {
			case <-gate.release:
			case <-ctx.Done():
				runErr = context.Cause(ctx)
			}
			gate.err = runErr
			close(gate.done)
			return runErr
		},
	)
	if err != nil {
		t.Fatalf("ScheduleWorktreeTransition: %v", err)
	}
	if ack.GetOperationId() != operationID.String() {
		t.Fatalf("scheduled transition ID = %q, want %q", ack.GetOperationId(), operationID)
	}
	return gate
}

func (gate *deleteActivityTransitionGate) unblock() {
	gate.releaseOnce.Do(func() { close(gate.release) })
}

func (gate *deleteActivityTransitionGate) wait(t *testing.T) {
	t.Helper()
	select {
	case <-gate.done:
		if gate.err != nil {
			t.Fatalf("finish accepted Worktree transition: %v", gate.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out finishing accepted Worktree transition")
	}
}

func openDeleteActivityRuntime(
	t *testing.T,
	env *serviceTestEnv,
	target serviceTestWorktree,
	sessionID string,
	ownerID string,
	client llm.Client,
) (sessionruntime.RuntimeAttachment, *runtime.Engine) {
	t.Helper()
	descriptor := openDeleteActivitySessionDescriptor(t, sessionID)
	plan := deleteActivityRuntimePlan(t, env, target.CanonicalRoot, client, "off", nil)
	attachment, err := env.authority.OpenRuntime(context.Background(), sessionruntime.RuntimeOpenRequest{
		SessionID: descriptor.SessionID(),
		OwnerID:   ownerID,
		Runtime:   &plan,
	})
	if err != nil {
		t.Fatalf("OpenRuntime: %v", err)
	}
	var engine *runtime.Engine
	if err := env.authority.WithRuntime(context.Background(), attachment.Resource(), func(_ context.Context, current *runtime.Engine) error {
		engine = current
		return nil
	}); err != nil {
		t.Fatalf("WithRuntime: %v", err)
	}
	return attachment, engine
}

func createDeleteTargetSessionPair(
	t *testing.T,
	env *serviceTestEnv,
	target serviceTestWorktree,
) (idleSessionID, blockingSessionID string, idleState, blockingState deleteTargetState) {
	t.Helper()
	firstSession := createServiceTestSession(t, env.store, env.cfg, env.binding)
	secondSession := createServiceTestSession(t, env.store, env.cfg, env.binding)
	for _, sess := range []*session.Store{firstSession, secondSession} {
		updateServiceTestSessionTarget(t, env, sess.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	}
	page, err := env.store.ListSessionsTargetingWorktreePage(env.ctx, target.WorktreeID, nil)
	if err != nil {
		t.Fatalf("ListSessionsTargetingWorktreePage: %v", err)
	}
	if len(page.Sessions) != 2 {
		t.Fatalf("targeting Sessions = %d, want 2", len(page.Sessions))
	}
	idleSessionID = page.Sessions[0].SessionID
	blockingSessionID = page.Sessions[1].SessionID
	idleState = captureDeleteTargetState(t, env, idleSessionID, target)
	blockingState = captureDeleteTargetState(t, env, blockingSessionID, target)
	return idleSessionID, blockingSessionID, idleState, blockingState
}

func TestDeleteWorktreeRejectsInFlightStartAndCompletesUnrelatedWorktree(t *testing.T) {
	lifecycle := &deleteInFlightStartLifecycle{
		StartBarrier: testsetup.NewStartBarrier(),
	}
	env := newServiceTestEnvWithResourceLifecycle(t, lifecycle)
	defer lifecycle.Unblock()

	busy := mustCreateWorktree(t, env, "feature/delete-in-flight-start-busy")
	unrelated := mustCreateWorktree(t, env, "feature/delete-in-flight-start-unrelated")
	busySession := createServiceTestSession(t, env.store, env.cfg, env.binding)
	updateServiceTestSessionTarget(t, env, busySession.Meta().SessionID, env.binding.WorkspaceID, busy.WorktreeID, ".")

	state := captureDeleteTargetState(t, env, busySession.Meta().SessionID, busy)
	descriptor := openDeleteActivitySessionDescriptor(t, busySession.Meta().SessionID)
	plan := deleteActivityTestRuntimePlan(t, env, busy.CanonicalRoot)
	started := testsetup.Start(func() (sessionruntime.ExecutionHandle, error) {
		return env.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
			Descriptor: descriptor,
			Runtime:    &plan,
			Resource:   sessionruntime.OpenAgentResource{},
			Runner: func(context.Context, sessionruntime.ExecutionScope, sessionruntime.AgentRuntimeBridge) error {
				return nil
			},
		})
	})
	select {
	case <-lifecycle.Entered():
	case result := <-started:
		t.Fatalf("agent start completed before entering resource lifecycle: handle=%v error=%v", result.Value, result.Err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for agent start to enter resource lifecycle")
	}

	busyDeleted := deleteServiceTestWorktree(env, busy.WorktreeID)
	select {
	case result := <-busyDeleted:
		if !errors.Is(result.err, worktreecontract.ErrWorktreeBlocked) {
			t.Fatalf("DeleteWorktree busy target = %+v, %v; want ErrWorktreeBlocked", result.result, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("busy delete waited for the in-flight start")
	}

	state.assertUnchanged(t, env, busySession.Meta().SessionID, busy.WorktreeID)

	unrelatedDeleted := deleteServiceTestWorktree(env, unrelated.WorktreeID)
	select {
	case result := <-unrelatedDeleted:
		if result.err != nil {
			t.Fatalf("DeleteWorktree unrelated = %+v, %v; want completed", result.result, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated delete did not complete while busy start remained held")
	}

	lifecycle.Unblock()
	start := <-started
	if start.Err != nil {
		t.Fatalf("StartAgentExecution: %v", start.Err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := start.Value.Wait(waitCtx); err != nil {
		t.Fatalf("wait for started execution: %v", err)
	}
}

func TestDeleteWorktreeRejectsLiveRunAndCompletesUnrelatedWorktree(t *testing.T) {
	env := newServiceTestEnv(t)
	busy := mustCreateWorktree(t, env, "feature/delete-live-run-busy")
	unrelated := mustCreateWorktree(t, env, "feature/delete-live-run-unrelated")
	busySession := createServiceTestSession(t, env.store, env.cfg, env.binding)
	updateServiceTestSessionTarget(t, env, busySession.Meta().SessionID, env.binding.WorkspaceID, busy.WorktreeID, ".")
	state := captureDeleteTargetState(t, env, busySession.Meta().SessionID, busy)
	plan := deleteActivityTestRuntimePlan(t, env, busy.CanonicalRoot)
	descriptor := openDeleteActivitySessionDescriptor(t, busySession.Meta().SessionID)
	attachment, err := env.authority.OpenRuntime(context.Background(), sessionruntime.RuntimeOpenRequest{
		SessionID: descriptor.SessionID(),
		OwnerID:   "delete-live-run",
		Runtime:   &plan,
	})
	if err != nil {
		t.Fatalf("OpenRuntime: %v", err)
	}
	runStarted := make(chan struct{})
	runRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseRun := func() {
		releaseOnce.Do(func() { close(runRelease) })
	}
	t.Cleanup(releaseRun)
	handle, err := env.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
		Descriptor: descriptor,
		Resource:   sessionruntime.CurrentAgentResource{},
		Runner: func(context.Context, sessionruntime.ExecutionScope, sessionruntime.AgentRuntimeBridge) error {
			close(runStarted)
			<-runRelease
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StartAgentExecution: %v", err)
	}
	select {
	case <-runStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for live execution to begin")
	}

	busyDeleted := deleteServiceTestWorktree(env, busy.WorktreeID)
	select {
	case result := <-busyDeleted:
		if !errors.Is(result.err, worktreecontract.ErrWorktreeBlocked) {
			t.Fatalf("DeleteWorktree busy target = %+v, %v; want ErrWorktreeBlocked", result.result, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("busy delete waited for the live run to finish")
	}
	assertForeignManagementDeleteBlocked(t, env, busy.WorktreeID)
	state.assertUnchanged(t, env, busySession.Meta().SessionID, busy.WorktreeID)

	unrelatedDeleted := deleteServiceTestWorktree(env, unrelated.WorktreeID)
	select {
	case result := <-unrelatedDeleted:
		if result.err != nil {
			t.Fatalf("DeleteWorktree unrelated = %+v, %v; want completed", result.result, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("unrelated delete did not complete while live run remained held")
	}

	releaseRun()
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := handle.Wait(waitCtx); err != nil {
		t.Fatalf("wait for live execution: %v", err)
	}
	if _, err := attachment.Release(waitCtx, sessionruntime.RuntimeReleaseClose); err != nil {
		t.Fatalf("release runtime attachment: %v", err)
	}
}

func TestDeleteWorktreeRejectsRunningReviewer(t *testing.T) {
	env := newServiceTestEnv(t)
	busy := mustCreateWorktree(t, env, "feature/delete-running-reviewer-busy")
	sessionID := env.session.Meta().SessionID
	updateServiceTestSessionTarget(t, env, sessionID, env.binding.WorkspaceID, busy.WorktreeID, ".")
	state := captureDeleteTargetState(t, env, sessionID, busy)
	descriptor := openDeleteActivitySessionDescriptor(t, sessionID)
	reviewer := newDeleteActivityReviewerClient()
	t.Cleanup(reviewer.Release)
	plan := deleteActivityRuntimePlan(
		t,
		env,
		busy.CanonicalRoot,
		deleteActivityTestLLMClient{},
		"all",
		runtimewire.RuntimeClientFactoryFunc(func(context.Context, runtimewire.RuntimeClientRequest) (llm.Client, error) {
			return reviewer, nil
		}),
	)
	attachment, err := env.authority.OpenRuntime(context.Background(), sessionruntime.RuntimeOpenRequest{
		SessionID: descriptor.SessionID(),
		OwnerID:   "delete-running-reviewer",
		Runtime:   &plan,
	})
	if err != nil {
		t.Fatalf("OpenRuntime: %v", err)
	}
	t.Cleanup(func() {
		_, releaseErr := attachment.Release(context.Background(), sessionruntime.RuntimeReleaseClose)
		if releaseErr != nil && !errors.Is(releaseErr, serverapi.ErrRuntimeUnavailable) {
			t.Errorf("release Reviewer Runtime: %v", releaseErr)
		}
	})
	handle, err := env.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
		Descriptor: descriptor,
		Resource:   sessionruntime.CurrentAgentResource{},
		Runner: func(ctx context.Context, _ sessionruntime.ExecutionScope, bridge sessionruntime.AgentRuntimeBridge) error {
			return bridge.WithEngine(ctx, func(engineCtx context.Context, engine *runtime.Engine) error {
				_, submitErr := engine.SubmitUserMessage(engineCtx, "finish before Reviewer")
				return submitErr
			})
		},
	})
	if err != nil {
		t.Fatalf("StartAgentExecution: %v", err)
	}
	select {
	case <-reviewer.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Reviewer provider request")
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := handle.Wait(waitCtx); err != nil {
		t.Fatalf("wait for originating execution: %v", err)
	}
	active, err := env.authority.HasBlockingRuntimeActivity(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("HasBlockingRuntimeActivity: %v", err)
	}
	if !active {
		t.Fatal("running Reviewer was not reported as blocking Runtime activity")
	}
	retired, err := env.authority.RetireIdleRuntime(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("RetireIdleRuntime: %v", err)
	}
	if retired {
		t.Fatal("Runtime retired while its Reviewer provider request was running")
	}

	busyDeleted := deleteServiceTestWorktree(env, busy.WorktreeID)
	select {
	case result := <-busyDeleted:
		if !errors.Is(result.err, worktreecontract.ErrWorktreeBlocked) {
			t.Fatalf("DeleteWorktree busy target = %+v, %v; want ErrWorktreeBlocked", result.result, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("busy delete waited for the Reviewer provider request")
	}
	state.assertUnchanged(t, env, sessionID, busy.WorktreeID)
}

func TestDeleteWorktreeRejectsAcceptedPendingTransitionBeforeMovingSessions(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-pending-transition")
	idleSessionID, blockedSessionID, idleState, blockedState := createDeleteTargetSessionPair(t, env, target)
	attachment, engine := openDeleteActivityRuntime(t, env, target, blockedSessionID, "delete-pending-transition", deleteActivityTestLLMClient{})

	maintenanceStarted := make(chan struct{})
	releaseMaintenance := make(chan struct{})
	maintenanceDone := make(chan error, 1)
	var releaseMaintenanceOnce sync.Once
	unblockMaintenance := func() { releaseMaintenanceOnce.Do(func() { close(releaseMaintenance) }) }
	go func() {
		maintenanceDone <- engine.RunWhenIdle(context.Background(), runtime.ActiveKindRuntimeMaintenance, func() error {
			close(maintenanceStarted)
			<-releaseMaintenance
			return nil
		})
	}()
	select {
	case <-maintenanceStarted:
	case err := <-maintenanceDone:
		t.Fatalf("Runtime maintenance ended before holding the pending transition: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Runtime maintenance")
	}

	var gate *deleteActivityTransitionGate
	transitionScheduled := false
	t.Cleanup(func() {
		if gate != nil {
			gate.unblock()
		}
		unblockMaintenance()
		select {
		case err := <-maintenanceDone:
			if err != nil {
				t.Errorf("finish Runtime maintenance: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("timed out finishing Runtime maintenance")
		}
		if transitionScheduled {
			gate.wait(t)
		}
		if _, err := attachment.Release(context.Background(), sessionruntime.RuntimeReleaseClose); err != nil &&
			!errors.Is(err, serverapi.ErrRuntimeUnavailable) {
			t.Errorf("release pending-transition Runtime: %v", err)
		}
	})
	gate = scheduleDeleteActivityTransition(t, engine)
	transitionScheduled = true
	select {
	case <-gate.started:
		t.Fatal("Worktree transition started before the maintenance boundary was released")
	default:
	}

	deleted := deleteServiceTestWorktree(env, target.WorktreeID)
	var result deleteActivityResult
	select {
	case result = <-deleted:
	case <-time.After(3 * time.Second):
		t.Fatal("delete waited for the accepted pending Worktree transition")
	}
	assertDeleteBlockedBySession(t, result.err, blockedSessionID)
	idleState.assertUnchanged(t, env, idleSessionID, target.WorktreeID)
	blockedState.assertUnchanged(t, env, blockedSessionID, target.WorktreeID)
	idleState.assertReminderUnchanged(t, env, idleSessionID)
	blockedState.assertReminderUnchanged(t, env, blockedSessionID)
	select {
	case <-gate.started:
		t.Error("delete released the accepted Worktree transition")
	default:
	}
}

func TestDeleteWorktreeRejectsInProgressTransitionBeforeMovingSessions(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-in-progress-transition")
	idleSessionID, blockedSessionID, idleState, blockedState := createDeleteTargetSessionPair(t, env, target)
	attachment, engine := openDeleteActivityRuntime(t, env, target, blockedSessionID, "delete-in-progress-transition", deleteActivityTestLLMClient{})
	var gate *deleteActivityTransitionGate
	t.Cleanup(func() {
		if gate != nil {
			gate.unblock()
			gate.wait(t)
		}
		if _, err := attachment.Release(context.Background(), sessionruntime.RuntimeReleaseClose); err != nil &&
			!errors.Is(err, serverapi.ErrRuntimeUnavailable) {
			t.Errorf("release in-progress-transition Runtime: %v", err)
		}
	})
	gate = scheduleDeleteActivityTransition(t, engine)
	select {
	case <-gate.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Worktree transition to begin")
	}

	deleted := deleteServiceTestWorktree(env, target.WorktreeID)
	var result deleteActivityResult
	select {
	case result = <-deleted:
	case <-time.After(3 * time.Second):
		t.Fatal("delete waited for the in-progress Worktree transition")
	}
	assertDeleteBlockedBySession(t, result.err, blockedSessionID)
	idleState.assertUnchanged(t, env, idleSessionID, target.WorktreeID)
	blockedState.assertUnchanged(t, env, blockedSessionID, target.WorktreeID)
	idleState.assertReminderUnchanged(t, env, idleSessionID)
	blockedState.assertReminderUnchanged(t, env, blockedSessionID)
	select {
	case <-gate.done:
		t.Errorf("delete released the in-progress Worktree transition: %v", gate.err)
	default:
	}
}

func TestDeleteWorktreeRejectsAcceptedQueuedInputBeforeMovingSessions(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-queued-input")
	idleSessionID, blockedSessionID, idleState, blockedState := createDeleteTargetSessionPair(t, env, target)
	client := deleteActivityObservedLLMClient{requests: make(chan struct{}, 1)}
	attachment, engine := openDeleteActivityRuntime(t, env, target, blockedSessionID, "delete-queued-input", client)

	maintenanceStarted := make(chan struct{})
	releaseMaintenance := make(chan struct{})
	maintenanceDone := make(chan error, 1)
	maintenanceCompleted := false
	var releaseMaintenanceOnce sync.Once
	unblockMaintenance := func() { releaseMaintenanceOnce.Do(func() { close(releaseMaintenance) }) }
	go func() {
		maintenanceDone <- engine.RunWhenIdle(context.Background(), runtime.ActiveKindRuntimeMaintenance, func() error {
			close(maintenanceStarted)
			<-releaseMaintenance
			return nil
		})
	}()
	select {
	case <-maintenanceStarted:
	case err := <-maintenanceDone:
		t.Fatalf("Runtime maintenance ended before accepting queued input: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Runtime maintenance")
	}
	t.Cleanup(func() {
		unblockMaintenance()
		if !maintenanceCompleted {
			select {
			case err := <-maintenanceDone:
				if err != nil {
					t.Errorf("finish Runtime maintenance: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Error("timed out finishing Runtime maintenance")
			}
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := engine.WaitForScheduledQueuedUserWork(waitCtx); err != nil &&
			!errors.Is(err, runtime.ErrEngineClosed) {
			t.Errorf("finish accepted queued input: %v", err)
		}
		if _, err := attachment.Release(context.Background(), sessionruntime.RuntimeReleaseClose); err != nil &&
			!errors.Is(err, serverapi.ErrRuntimeUnavailable) {
			t.Errorf("release queued-input Runtime: %v", err)
		}
	})

	queued, err := engine.QueueUserInput(context.Background(), runtime.QueuedUserInput{
		ExecutionText:         "accepted before Worktree deletion",
		CanonicalPresentation: "accepted before Worktree deletion",
	})
	if err != nil {
		t.Fatalf("QueueUserInput: %v", err)
	}
	if !engine.HasQueuedUserWork() {
		t.Fatal("accepted user input was not pending before deletion")
	}

	deleted := deleteServiceTestWorktree(env, target.WorktreeID)
	var result deleteActivityResult
	select {
	case result = <-deleted:
	case <-time.After(3 * time.Second):
		t.Fatal("delete waited for accepted queued input")
	}
	assertDeleteBlockedBySession(t, result.err, blockedSessionID)
	if !engine.HasQueuedUserWork() {
		t.Error("blocked delete discarded accepted user input")
	}
	idleState.assertUnchanged(t, env, idleSessionID, target.WorktreeID)
	blockedState.assertUnchanged(t, env, blockedSessionID, target.WorktreeID)
	idleState.assertReminderUnchanged(t, env, idleSessionID)
	blockedState.assertReminderUnchanged(t, env, blockedSessionID)
	select {
	case <-client.requests:
		t.Fatal("accepted input began provider work while maintenance remained held")
	default:
	}

	unblockMaintenance()
	select {
	case err := <-maintenanceDone:
		maintenanceCompleted = true
		if err != nil {
			t.Fatalf("finish Runtime maintenance: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out releasing Runtime maintenance")
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := engine.WaitForScheduledQueuedUserWork(waitCtx); err != nil {
		t.Fatalf("wait for accepted input: %v", err)
	}
	select {
	case <-client.requests:
	case <-time.After(3 * time.Second):
		t.Fatalf("accepted queued input %q was not delivered after deletion was blocked", queued.ID)
	}
}

func TestDeleteWorktreeRejectsSelectedWorkflowBeforeProviderWork(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-selected-workflow")
	idleSessionID, blockedSessionID, idleState, blockedState := createDeleteTargetSessionPair(t, env, target)
	client := deleteActivityObservedLLMClient{requests: make(chan struct{}, 1)}
	attachment, engine := openDeleteActivityRuntime(t, env, target, blockedSessionID, "delete-selected-workflow", client)

	currentNode, err := workflow.NewCurrentNodeReference("task-delete-selected", "node-selected", nil)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}
	workflowRef := sessionruntime.WorkflowExecutionRef{
		ProjectID:   env.binding.ProjectID,
		WorkflowID:  runtimeids.NewWorkflowID(),
		CurrentNode: currentNode,
	}
	workflowSelected := make(chan struct{})
	releaseWorkflow := make(chan struct{})
	var releaseWorkflowOnce sync.Once
	unblockWorkflow := func() { releaseWorkflowOnce.Do(func() { close(releaseWorkflow) }) }
	handle, err := env.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
		Descriptor: openDeleteActivitySessionDescriptor(t, blockedSessionID),
		Workflow: &sessionruntime.WorkflowAgentExecution{
			Reference: workflowRef,
			Config: &workflowruntime.CurrentNodeExecutionConfig{
				Instructions: workflowruntime.TaskInstructions{CurrentNode: currentNode},
			},
		},
		Resource: sessionruntime.CurrentAgentResource{},
		Runner: func(ctx context.Context, _ sessionruntime.ExecutionScope, _ sessionruntime.AgentRuntimeBridge) error {
			close(workflowSelected)
			select {
			case <-releaseWorkflow:
				return nil
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		},
	})
	if err != nil {
		t.Fatalf("StartAgentExecution: %v", err)
	}
	t.Cleanup(func() {
		unblockWorkflow()
		waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := handle.Wait(waitCtx); err != nil {
			t.Errorf("finish selected Workflow execution: %v", err)
		}
		if _, err := attachment.Release(waitCtx, sessionruntime.RuntimeReleaseClose); err != nil &&
			!errors.Is(err, serverapi.ErrRuntimeUnavailable) {
			t.Errorf("release selected-Workflow Runtime: %v", err)
		}
	})
	select {
	case <-workflowSelected:
	case <-time.After(3 * time.Second):
		t.Fatal("selected Workflow did not enter its execution boundary")
	}
	if !engine.CurrentNodeExecutionConfigured() {
		t.Fatal("selected Workflow was not bound to its Runtime")
	}

	deleted := deleteServiceTestWorktree(env, target.WorktreeID)
	var result deleteActivityResult
	select {
	case result = <-deleted:
	case <-time.After(3 * time.Second):
		t.Fatal("delete waited for selected Workflow work")
	}
	assertDeleteBlockedBySession(t, result.err, blockedSessionID)
	idleState.assertUnchanged(t, env, idleSessionID, target.WorktreeID)
	blockedState.assertUnchanged(t, env, blockedSessionID, target.WorktreeID)
	idleState.assertReminderUnchanged(t, env, idleSessionID)
	blockedState.assertReminderUnchanged(t, env, blockedSessionID)
	select {
	case <-client.requests:
		t.Fatal("Workflow requested provider work before reaching its held execution boundary")
	default:
	}
}

func TestDeleteWorktreeRetiresIdleRuntimeAndRetargetsSessionBeforePhysicalRemoval(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-idle-runtime")
	otherSession := createServiceTestSession(t, env.store, env.cfg, env.binding)
	mainSessionID := env.session.Meta().SessionID
	updateServiceTestSessionTarget(t, env, mainSessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	updateServiceTestSessionTarget(t, env, otherSession.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	for _, sessionID := range []string{mainSessionID, otherSession.Meta().SessionID} {
		if reminder := readDeleteActivityReminder(t, env, sessionID); reminder != nil {
			t.Fatalf("Session %q starts with an unexpected Worktree reminder: %+v", sessionID, reminder)
		}
	}
	relativeEditPath := filepath.Join(env.workspaceRoot, "relative-edit.txt")
	if err := os.WriteFile(relativeEditPath, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write Main Workspace relative-edit fixture: %v", err)
	}
	descriptor := openDeleteActivitySessionDescriptor(t, otherSession.Meta().SessionID)
	plan := deleteActivityTestRuntimePlan(t, env, target.CanonicalRoot)
	attachment, err := env.authority.OpenRuntime(context.Background(), sessionruntime.RuntimeOpenRequest{
		SessionID: descriptor.SessionID(),
		OwnerID:   "delete-idle-runtime",
		Runtime:   &plan,
	})
	if err != nil {
		t.Fatalf("OpenRuntime: %v", err)
	}
	t.Cleanup(func() {
		_, releaseErr := attachment.Release(context.Background(), sessionruntime.RuntimeReleaseClose)
		if releaseErr != nil && !errors.Is(releaseErr, serverapi.ErrRuntimeUnavailable) {
			t.Errorf("release idle Runtime: %v", releaseErr)
		}
	})

	barrier := testsetup.NewStartBarrier()
	env.service.git = NewGitInspector(&deleteRemovalBarrierRunner{
		delegate: env.service.git.runner,
		barrier:  barrier,
	})
	deleted := testsetup.Start(func() (*worktreepb.DeleteSuccess, error) {
		return env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, target.WorktreeID))
	})
	select {
	case <-barrier.Entered():
	case result := <-deleted:
		t.Fatalf("DeleteWorktree completed before physical-removal boundary: result=%+v error=%v", result.Value, result.Err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for physical Worktree removal")
	}
	defer barrier.Unblock()

	if err := env.authority.WithRuntime(context.Background(), attachment.Resource(), func(context.Context, *runtime.Engine) error {
		return nil
	}); !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("idle Runtime remained available before physical Worktree removal: %v", err)
	}
	otherTarget, err := env.store.ResolveSessionExecutionTarget(env.ctx, otherSession.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget other Session before physical removal: %v", err)
	}
	if sessionTargetWorktreeID(otherTarget) != "" || otherTarget.EffectiveWorkdir != env.workspaceRoot {
		t.Fatalf("other Session target before physical removal = %+v, want Main Workspace", otherTarget)
	}
	mainTarget, err := env.store.ResolveSessionExecutionTarget(env.ctx, mainSessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget calling Session before physical removal: %v", err)
	}
	if sessionTargetWorktreeID(mainTarget) != "" || mainTarget.EffectiveWorkdir != env.workspaceRoot {
		t.Fatalf("calling Session target before physical removal = %+v, want Main Workspace", mainTarget)
	}
	type exitReminder struct {
		sessionID string
		state     session.WorktreeReminderState
	}
	remindersAtRemoval := make([]exitReminder, 0, 2)
	for _, sessionID := range []string{mainSessionID, otherSession.Meta().SessionID} {
		reminder := readDeleteActivityReminder(t, env, sessionID)
		if reminder == nil ||
			reminder.Mode != session.WorktreeReminderModeExit ||
			reminder.WorktreePath != target.CanonicalRoot ||
			reminder.WorkspaceRoot != env.workspaceRoot ||
			reminder.EffectiveCwd != env.workspaceRoot ||
			reminder.ContextID == nil {
			t.Fatalf("Session %q exit reminder before physical removal = %+v", sessionID, reminder)
		}
		remindersAtRemoval = append(remindersAtRemoval, exitReminder{sessionID: sessionID, state: *reminder})
	}
	if _, err := os.Stat(target.CanonicalRoot); err != nil {
		t.Fatalf("Worktree root changed before physical removal: %v", err)
	}

	barrier.Unblock()
	result := <-deleted
	if result.Err != nil {
		t.Fatalf("DeleteWorktree = %+v, %v; want completed", result.Value, result.Err)
	}
	if _, err := os.Stat(target.CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Worktree root still exists after delete: %v", err)
	}
	for _, reminderAtRemoval := range remindersAtRemoval {
		current := readDeleteActivityReminder(t, env, reminderAtRemoval.sessionID)
		if current == nil || !session.WorktreeReminderStateEqual(reminderAtRemoval.state, *current) {
			t.Errorf("Session %q Worktree reminder changed during physical removal: before=%+v after=%+v", reminderAtRemoval.sessionID, reminderAtRemoval.state, current)
		}
	}

	client := &deleteActivityEditLLMClient{}
	api := sessionruntime.NewAPI(env.store, env.authority, sessionruntime.APIOptions{
		RuntimeClientFactory: runtimewire.RuntimeClientFactoryFunc(func(context.Context, runtimewire.RuntimeClientRequest) (llm.Client, error) {
			return client, nil
		}),
	})
	settings := env.cfg.Settings
	settings.Model = "gpt-6-sol"
	settings.ModelContextWindow = 200000
	settings.Reviewer.Frequency = "off"
	ownerID := "delete-idle-runtime-retargeted"
	activated, err := api.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		SessionID:             otherSession.Meta().SessionID,
		OwnerID:               ownerID,
		ActiveSettings:        settings,
		EnabledToolIDs:        []string{string(toolspec.ToolEdit), string(toolspec.ToolExecCommand)},
		QuestionsEnabled:      textutil.Value(true),
		AutoCompactionEnabled: textutil.Value(true),
		Source:                config.SourceReport{Sources: map[string]config.Origin{}},
	})
	if err != nil {
		t.Fatalf("activate retargeted Session Runtime: %v", err)
	}
	t.Cleanup(func() {
		if _, err := api.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
			Attachment: activated, OwnerID: ownerID, DropOwner: true,
			ClosePolicy: serverapi.SessionRuntimeReleaseClosePolicyCloseIfIdle,
		}); err != nil {
			t.Errorf("release retargeted Session Runtime: %v", err)
		}
	})
	sessionID, err := runtimeids.ParseSessionID(otherSession.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID other Session: %v", err)
	}
	if err := env.authority.WithCurrentRuntime(context.Background(), sessionID, func(ctx context.Context, engine *runtime.Engine) error {
		if _, err := engine.SubmitUserShellCommand(ctx, "pwd > .kent-delete-shell-cwd"); err != nil {
			return err
		}
		_, err := engine.SubmitUserMessage(ctx, "edit relative-edit.txt from Main Workspace")
		return err
	}); err != nil {
		t.Fatalf("run shell and relative-file input on retargeted Session: %v", err)
	}
	shellCwd, err := os.ReadFile(filepath.Join(env.workspaceRoot, ".kent-delete-shell-cwd"))
	if err != nil {
		t.Fatalf("read Main Workspace shell cwd marker: %v", err)
	}
	if got := strings.TrimSpace(string(shellCwd)); got != env.workspaceRoot {
		t.Fatalf("retargeted shell cwd = %q, want Main Workspace %q", got, env.workspaceRoot)
	}
	edited, err := os.ReadFile(relativeEditPath)
	if err != nil {
		t.Fatalf("read relative edit from Main Workspace: %v", err)
	}
	if string(edited) != "after\n" {
		t.Fatalf("relative edit content = %q, want edited Main Workspace file", edited)
	}
}

func TestDeleteTaskWorktreeRejectsInFlightStartUnchanged(t *testing.T) {
	lifecycle := &deleteInFlightStartLifecycle{
		StartBarrier: testsetup.NewStartBarrier(),
	}
	env := newServiceTestEnvWithResourceLifecycle(t, lifecycle)
	defer lifecycle.Unblock()
	task, _ := createTaskWorktreeTestTask(t, env)
	materialized, err := env.service.MaterializeInitialTaskWorktree(env.ctx, InitialTaskWorktreeMaterializationRequest{
		TaskID:         task.ID,
		ResolvedTarget: resolveTaskWorktreeTestHEAD(t, env, env.workspaceRoot),
	})
	if err != nil {
		t.Fatalf("MaterializeInitialTaskWorktree: %v", err)
	}
	busy := serviceTestWorktree{
		WorktreeID:    taskWorktreeID(materialized.Worktree),
		CanonicalRoot: taskWorktreeRoot(materialized.Worktree),
	}
	busySession := createServiceTestSession(t, env.store, env.cfg, env.binding)
	updateServiceTestSessionTarget(t, env, busySession.Meta().SessionID, env.binding.WorkspaceID, busy.WorktreeID, ".")
	state := captureDeleteTargetState(t, env, busySession.Meta().SessionID, busy)
	descriptor := openDeleteActivitySessionDescriptor(t, busySession.Meta().SessionID)
	plan := deleteActivityTestRuntimePlan(t, env, busy.CanonicalRoot)
	started := testsetup.Start(func() (sessionruntime.ExecutionHandle, error) {
		return env.authority.StartAgentExecution(context.Background(), sessionruntime.AgentExecutionRequest{
			Descriptor: descriptor,
			Runtime:    &plan,
			Resource:   sessionruntime.OpenAgentResource{},
			Runner: func(context.Context, sessionruntime.ExecutionScope, sessionruntime.AgentRuntimeBridge) error {
				return nil
			},
		})
	})
	select {
	case <-lifecycle.Entered():
	case result := <-started:
		t.Fatalf("agent start completed before entering resource lifecycle: handle=%v error=%v", result.Value, result.Err)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for agent start to enter resource lifecycle")
	}

	deleted := make(chan error, 1)
	go func() {
		_, deleteErr := env.service.DeleteTaskWorktree(env.ctx, DeleteTaskWorktreeRequest{TaskID: string(task.ID)})
		deleted <- deleteErr
	}()
	select {
	case err := <-deleted:
		if !errors.Is(err, worktreecontract.ErrWorktreeBlocked) {
			t.Fatalf("DeleteTaskWorktree error = %v, want ErrWorktreeBlocked", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DeleteTaskWorktree waited for the in-flight start")
	}
	assertForeignManagementDeleteBlocked(t, env, busy.WorktreeID)
	state.assertUnchanged(t, env, busySession.Meta().SessionID, busy.WorktreeID)

	lifecycle.Unblock()
	start := <-started
	if start.Err != nil {
		t.Fatalf("StartAgentExecution: %v", start.Err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := start.Value.Wait(waitCtx); err != nil {
		t.Fatalf("wait for started execution: %v", err)
	}
}

func TestDeleteWorktreeCurrentTargetRetargetsOtherSession(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-scheduled-current")
	otherSession := createServiceTestSession(t, env.store, env.cfg, env.binding)
	updateServiceTestSessionTarget(t, env, env.session.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	updateServiceTestSessionTarget(t, env, otherSession.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
	request := worktreeDeleteRequest(env, target.WorktreeID)

	result, err := env.service.DeleteWorktree(env.ctx, request)
	if err != nil {
		t.Fatalf("DeleteWorktree current target = %+v, %v; want completed", result, err)
	}
	assertServiceTestSessionTarget(t, env, "", env.workspaceRoot)
	otherTarget, err := env.store.ResolveSessionExecutionTarget(env.ctx, otherSession.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget other session: %v", err)
	}
	if sessionTargetWorktreeID(otherTarget) != "" || otherTarget.EffectiveWorkdir != env.workspaceRoot {
		t.Fatalf("other session target after delete = %+v, want main workspace", otherTarget)
	}
	if _, err := os.Stat(target.CanonicalRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target root still exists: %v", err)
	}
	if _, err := env.store.GetWorktreeRecordByID(env.ctx, target.WorktreeID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("target metadata = %v, want sql.ErrNoRows", err)
	}
}

func TestDeleteWorktreeCurrentTargetForceDeletesBranch(t *testing.T) {
	env := newServiceTestEnv(t)
	target := mustCreateWorktree(t, env, "feature/delete-scheduled-force-branch")
	if err := os.WriteFile(filepath.Join(target.CanonicalRoot, "unmerged.txt"), []byte("unmerged"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, target.CanonicalRoot, "add", "unmerged.txt")
	runGit(t, target.CanonicalRoot, "commit", "-m", "unmerged branch change")
	updateServiceTestSessionTarget(t, env, env.session.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")

	request := worktreeDeleteRequest(env, target.WorktreeID)
	request.BranchCleanupPolicy = worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_FORCE
	_, err := env.service.DeleteWorktree(env.ctx, request)
	if err != nil {
		t.Fatalf("DeleteWorktree: %v", err)
	}
	if exists, err := env.service.git.BranchExists(env.ctx, env.workspaceRoot, target.BranchName); err != nil || exists {
		t.Fatalf("force-deleted branch exists=%v err=%v", exists, err)
	}
	if _, err := env.store.GetWorktreeRecordByID(env.ctx, target.WorktreeID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("target metadata = %v, want sql.ErrNoRows", err)
	}
	assertServiceTestSessionTarget(t, env, "", env.workspaceRoot)
}

func TestDeleteWorktreeRechecksDirtyStateBeforeRemoval(t *testing.T) {
	tests := []struct {
		name              string
		secondStatusError error
		wantKind          worktreepb.DirtyStateKind
		wantCount         int
	}{
		{name: "dirty", wantKind: worktreepb.DirtyStateKind_DIRTY_STATE_DIRTY, wantCount: 1},
		{name: "unknown", secondStatusError: errors.New("status inspection failed"), wantKind: worktreepb.DirtyStateKind_DIRTY_STATE_UNKNOWN},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newServiceTestEnv(t)
			target := mustCreateWorktree(t, env, "feature/delete-scheduled-race-"+test.name)
			updateServiceTestSessionTarget(t, env, env.session.Meta().SessionID, env.binding.WorkspaceID, target.WorktreeID, ".")
			state := captureDeleteTargetState(t, env, env.session.Meta().SessionID, target)
			preview, err := env.service.PreviewWorktreeDelete(env.ctx, &worktreepb.DeletePreviewRequest{
				Scope:    worktreecontract.SessionManagementScope(env.session.Meta().SessionID),
				Selector: target.WorktreeID,
			})
			if err != nil {
				t.Fatalf("PreviewWorktreeDelete: %v", err)
			}
			if preview.GetCleanliness().GetKind() != worktreepb.DirtyStateKind_DIRTY_STATE_CLEAN {
				t.Fatalf("preview cleanliness = %+v, want clean", preview.GetCleanliness())
			}

			runner := newBlockingDeleteStatusRunner(test.secondStatusError)
			env.service.git = NewGitInspector(runner)
			deleteResult := make(chan deleteActivityResult, 1)
			go func() {
				result, deleteErr := env.service.DeleteWorktree(env.ctx, worktreeDeleteRequest(env, preview.DeletionSelector))
				deleteResult <- deleteActivityResult{result: result, err: deleteErr}
			}()
			select {
			case <-runner.statusReached:
			case <-time.After(3 * time.Second):
				t.Fatal("delete did not reach final cleanliness check")
			}
			if test.secondStatusError == nil {
				if err := os.WriteFile(filepath.Join(target.CanonicalRoot, "dirty-after-schedule.txt"), []byte("dirty"), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}
			runner.ReleaseStatus()

			var result deleteActivityResult
			select {
			case result = <-deleteResult:
			case <-time.After(3 * time.Second):
				t.Fatal("DeleteWorktree did not return after cleanliness check")
			}
			var precondition *worktreecontract.DeletePreconditionError
			if !errors.As(result.err, &precondition) ||
				precondition.Details.GetDirtyState().GetKind() != test.wantKind {
				t.Fatalf("DeleteWorktree = %+v, %v; want typed %s precondition", result.result, result.err, test.wantKind)
			}
			if test.wantCount != 0 {
				if precondition.Details.GetDirtyState().DirtyFileCount == nil ||
					precondition.Details.GetDirtyState().GetDirtyFileCount() != int32(test.wantCount) {
					t.Fatalf("dirty precondition = %+v, want count %d", precondition.Details.GetDirtyState(), test.wantCount)
				}
			}
			state.assertUnchanged(t, env, env.session.Meta().SessionID, target.WorktreeID)
		})
	}
}

type blockingDeleteStatusRunner struct {
	statusError   error
	statusReached chan struct{}
	releaseStatus chan struct{}
	releaseOnce   sync.Once
	mu            sync.Mutex
	statusCalls   int
}

func newBlockingDeleteStatusRunner(statusError error) *blockingDeleteStatusRunner {
	return &blockingDeleteStatusRunner{
		statusError:   statusError,
		statusReached: make(chan struct{}),
		releaseStatus: make(chan struct{}),
	}
}

func (r *blockingDeleteStatusRunner) Output(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if len(args) == 3 && args[0] == "status" && args[1] == "--porcelain=v1" && args[2] == "-z" {
		r.mu.Lock()
		r.statusCalls++
		call := r.statusCalls
		if call == 1 {
			close(r.statusReached)
		}
		r.mu.Unlock()
		if call == 1 {
			select {
			case <-r.releaseStatus:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			if r.statusError != nil {
				return nil, r.statusError
			}
		}
	}
	return execGitCommandRunner{}.Output(ctx, dir, args...)
}

func (r *blockingDeleteStatusRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, int, error) {
	return execGitCommandRunner{}.Run(ctx, dir, args...)
}

func (r *blockingDeleteStatusRunner) ReleaseStatus() {
	r.releaseOnce.Do(func() {
		close(r.releaseStatus)
	})
}
