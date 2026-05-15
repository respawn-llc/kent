package workflowscheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"builder/server/metadata"
	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/shared/config"
)

func TestSchedulerSelectsOldestRunnableRunAndRespectsConcurrency(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	first := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	second := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	starter := &recordingStarter{}
	scheduler, err := New(store, starter, Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := scheduler.Process(ctx); err != nil {
		t.Fatalf("Process: %v", err)
	}
	started := starter.requests()
	if len(started) != 1 || started[0].RunID != first.RunID {
		t.Fatalf("started = %+v, want first run %s", started, first.RunID)
	}
	if scheduler.ActiveCount() != 1 {
		t.Fatalf("active count = %d, want 1", scheduler.ActiveCount())
	}
	runs, err := store.ListRuns(ctx, second.TaskID)
	if err != nil {
		t.Fatalf("ListRuns second: %v", err)
	}
	if runs[0].StartedAt != 0 {
		t.Fatalf("second run was durably started despite concurrency cap: %+v", runs[0])
	}
}

func TestSchedulerConcurrentProcessStartsOneRuntimePerRun(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	startedRun := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	starter := &recordingStarter{}
	scheduler, err := New(store, starter, Config{Concurrency: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = scheduler.Process(ctx)
		}()
	}
	wg.Wait()

	started := starter.requests()
	if len(started) != 1 || started[0].RunID != startedRun.RunID {
		t.Fatalf("started = %+v, want one start for %s", started, startedRun.RunID)
	}
}

func TestSchedulerStartProcessesRebuiltRunnableWork(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	startedRun := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	starter := &recordingStarter{}
	scheduler, err := New(store, starter, Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	started := starter.requests()
	if len(started) != 1 || started[0].RunID != startedRun.RunID {
		t.Fatalf("started = %+v, want rebuilt run %s", started, startedRun.RunID)
	}
}

func TestSchedulerDoesNotScheduleCanceledOrInterruptedTasks(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	canceledTask, err := store.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Canceled", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask canceled: %v", err)
	}
	canceledRun, err := store.StartTask(ctx, canceledTask.ID)
	if err != nil {
		t.Fatalf("StartTask canceled: %v", err)
	}
	if err := store.CancelTask(ctx, canceledTask.ID, "stop"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	interrupted := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	if err := store.InterruptRun(ctx, interrupted.RunID, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRun: %v", err)
	}
	starter := &recordingStarter{}
	scheduler, err := New(store, starter, Config{Concurrency: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := scheduler.Process(ctx); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if started := starter.requests(); len(started) != 0 {
		t.Fatalf("started canceled/interrupted runs = %+v; canceled run was %s", started, canceledRun.RunID)
	}
}

func TestSchedulerActiveOwnershipIsMemoryOnly(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	startedRun := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	scheduler, err := New(store, &recordingStarter{}, Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := scheduler.Process(ctx); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if scheduler.ActiveCount() != 1 {
		t.Fatalf("active count = %d, want in-memory ownership", scheduler.ActiveCount())
	}
	restarted, err := New(store, nil, Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New restarted: %v", err)
	}
	if restarted.ActiveCount() != 0 {
		t.Fatalf("restarted active count = %d, want no durable ownership", restarted.ActiveCount())
	}
	if err := restarted.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile restarted: %v", err)
	}
	runs, err := store.ListRuns(ctx, startedRun.TaskID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].InterruptedAt == 0 || runs[0].InterruptionReason != ReasonStartupOrphanedRun {
		t.Fatalf("restarted scheduler did not treat prior active owner as orphaned: %+v", runs[0])
	}
}

func TestSchedulerCloseStopsNewClaims(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	_ = createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	starter := &recordingStarter{}
	scheduler, err := New(store, starter, Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := scheduler.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := scheduler.Process(ctx); err == nil {
		t.Fatalf("expected stopped scheduler error")
	}
	if started := starter.requests(); len(started) != 0 {
		t.Fatalf("stopped scheduler started runs: %+v", started)
	}
}

func TestSchedulerRecoveryInterruptsOrphanedStartedRunsAndKeepsRunnable(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	orphan := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	if _, err := store.ClaimRun(ctx, orphan.RunID, 0); err != nil {
		t.Fatalf("ClaimRun orphan: %v", err)
	}
	runnable := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	scheduler, err := New(store, nil, Config{Concurrency: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := scheduler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	orphanRuns, err := store.ListRuns(ctx, orphan.TaskID)
	if err != nil {
		t.Fatalf("ListRuns orphan: %v", err)
	}
	if orphanRuns[0].InterruptedAt == 0 || orphanRuns[0].InterruptionReason != ReasonStartupOrphanedRun {
		t.Fatalf("orphan run = %+v, want interrupted", orphanRuns[0])
	}
	runnableRuns, err := store.ListRuns(ctx, runnable.TaskID)
	if err != nil {
		t.Fatalf("ListRuns runnable: %v", err)
	}
	if runnableRuns[0].InterruptedAt != 0 || runnableRuns[0].StartedAt != 0 {
		t.Fatalf("runnable run changed during recovery: %+v", runnableRuns[0])
	}
}

func TestSchedulerRecoveryUsesPendingAskResolver(t *testing.T) {
	ctx := context.Background()
	store, binding, metadataStore := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	keep := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	drop := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	if _, err := metadataStore.DB().ExecContext(ctx, `UPDATE task_runs SET waiting_ask_id = 'ask-keep' WHERE id = ?`, keep.RunID); err != nil {
		t.Fatalf("mark keep waiting: %v", err)
	}
	if _, err := metadataStore.DB().ExecContext(ctx, `UPDATE task_runs SET waiting_ask_id = 'ask-drop' WHERE id = ?`, drop.RunID); err != nil {
		t.Fatalf("mark drop waiting: %v", err)
	}
	resolver := pendingAskResolverFunc(func(_ context.Context, _ string, _ workflow.RunID, askID string) (bool, error) {
		return askID == "ask-keep", nil
	})
	scheduler, err := New(store, nil, Config{Concurrency: 5}, WithPendingAskResolver(resolver))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := scheduler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	keepRuns, err := store.ListRuns(ctx, keep.TaskID)
	if err != nil {
		t.Fatalf("ListRuns keep: %v", err)
	}
	if keepRuns[0].InterruptedAt != 0 || keepRuns[0].WaitingAskID != "ask-keep" {
		t.Fatalf("keep waiting run = %+v", keepRuns[0])
	}
	dropRuns, err := store.ListRuns(ctx, drop.TaskID)
	if err != nil {
		t.Fatalf("ListRuns drop: %v", err)
	}
	if dropRuns[0].InterruptedAt == 0 || dropRuns[0].InterruptionReason != ReasonPendingAskUnavailable {
		t.Fatalf("drop waiting run = %+v, want interrupted", dropRuns[0])
	}
}

func TestSchedulerRuntimeStartFailureInterruptsRun(t *testing.T) {
	ctx := context.Background()
	store, binding, _ := newSchedulerTestStore(t)
	workflowID := createSchedulerValidWorkflow(t, ctx, store)
	if _, err := store.LinkWorkflow(ctx, binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	started := createAndStartSchedulerTask(t, ctx, store, binding.ProjectID)
	starter := &recordingStarter{err: errors.New("role missing")}
	scheduler, err := New(store, starter, Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := scheduler.Process(ctx); err == nil {
		t.Fatalf("expected starter error")
	}
	runs, err := store.ListRuns(ctx, started.TaskID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if runs[0].InterruptedAt == 0 || runs[0].InterruptionReason != ReasonRuntimeStartFailed {
		t.Fatalf("run after starter failure = %+v", runs[0])
	}
}

type recordingStarter struct {
	mu      sync.Mutex
	started []StartRunRequest
	err     error
}

func (s *recordingStarter) StartWorkflowRun(_ context.Context, req StartRunRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, req)
	return s.err
}

func (s *recordingStarter) requests() []StartRunRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]StartRunRequest{}, s.started...)
}

type pendingAskResolverFunc func(context.Context, string, workflow.RunID, string) (bool, error)

func (f pendingAskResolverFunc) CanRehydrate(ctx context.Context, sessionID string, runID workflow.RunID, askID string) (bool, error) {
	return f(ctx, sessionID, runID, askID)
}

func newSchedulerTestStore(t *testing.T) (*workflowstore.Store, metadata.Binding, *metadata.Store) {
	t.Helper()
	home := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := config.Load(workspaceRoot, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if err := metadataStore.SetProjectKey(context.Background(), binding.ProjectID, "SCH"); err != nil {
		t.Fatalf("SetProjectKey: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	store, err := workflowstore.New(metadataStore, workflowstore.WithRoleResolver(workflow.StaticRoleResolver{"coder": true}), workflowstore.WithNow(func() time.Time {
		now = now.Add(time.Millisecond)
		return now
	}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	return store, binding, metadataStore
}

func createSchedulerValidWorkflow(t *testing.T, ctx context.Context, store *workflowstore.Store) workflow.WorkflowID {
	t.Helper()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := schedulerNodeByKind(t, def, workflow.NodeKindStart)
	done := schedulerNodeByKind(t, def, workflow.NodeKindTerminal)
	agentID := workflow.NodeID("node-agent-" + string(created.ID))
	if _, err := store.AddNode(ctx, workflowstore.NodeRecord{ID: agentID, WorkflowID: created.ID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Do work.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID("group-start-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"}); err != nil {
		t.Fatalf("AddTransitionGroup start: %v", err)
	}
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-start-" + string(created.ID)), Key: "start", TargetNodeID: agentID, ContextMode: workflow.ContextModeNewSession}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID("group-done-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-done-" + string(created.ID)), Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}}); err != nil {
		t.Fatalf("AddEdge done: %v", err)
	}
	return created.ID
}

type schedulerStartedTask struct {
	TaskID workflow.TaskID
	workflowstore.StartTaskResult
}

func createAndStartSchedulerTask(t *testing.T, ctx context.Context, store *workflowstore.Store, projectID string) schedulerStartedTask {
	t.Helper()
	task, err := store.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: projectID, Title: "Task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	started, err := store.StartTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	return schedulerStartedTask{TaskID: task.ID, StartTaskResult: started}
}

func schedulerNodeByKind(t *testing.T, def workflow.Definition, kind workflow.NodeKind) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if node.Kind == kind {
			return node
		}
	}
	t.Fatalf("node kind %s missing in %+v", kind, def.Nodes)
	return workflow.Node{}
}
