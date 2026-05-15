package workflowrunner

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"builder/server/llm"
	"builder/server/metadata"
	"builder/server/registry"
	askquestion "builder/server/tools/askquestion"
	"builder/server/workflow"
	"builder/server/workflowruntime/workflowtest"
	"builder/server/workflowscheduler"
	"builder/server/workflowstore"
	"builder/server/workflowview"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
)

func TestSchedulerRunsNewSessionWorkflowNodeWithStructuredOutput(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput, workflowtest.FinalAnswer(`{"transition_id":"done","commentary":"finished structured","summary":"structured ok"}`))

	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Process(context.Background()); err != nil {
		t.Fatalf("Process: %v", err)
	}
	fixture.waitForCompletedRun(t, task.ID)

	detail, err := fixture.view.GetTask(context.Background(), string(task.ID))
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !detail.Summary.Done {
		t.Fatalf("task summary done = false, detail=%+v", detail)
	}
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || strings.TrimSpace(runs[0].SessionID) == "" || runs[0].CompletedAt == 0 {
		t.Fatalf("run not attached/completed: %+v", runs)
	}
	transitions, err := fixture.store.ListTransitions(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 2 || transitions[1].TransitionID != "done" || transitions[1].Commentary != "finished structured" || transitions[1].OutputValues["summary"] != "structured ok" {
		t.Fatalf("completion transition = %+v", transitions)
	}
	reqs := fixture.client.Requests()
	if len(reqs) == 0 {
		t.Fatal("fake model was not called")
	}
	first := reqs[0]
	if first.StructuredOutput == nil {
		t.Fatalf("structured output schema missing in request: %+v", first)
	}
	assertPromptContains(t, first, []string{"Task title: Run workflow", "Task body:\nBody for workflow", "Node key: agent", "Completion mode: structured_output", "summary: Summary.", "done (Done)"})
	fixture.assertRunSessionUsesTaskWorktree(t, runs[0].SessionID)
	if scheduler.ActiveCount() != 0 {
		t.Fatalf("scheduler active count = %d, want 0 after runtime finish", scheduler.ActiveCount())
	}
}

func TestSchedulerRunsNewSessionWorkflowNodeWithCompleteNodeTool(t *testing.T) {
	input := json.RawMessage(`{"transition_id":"done","commentary":"finished tool","summary":"tool ok"}`)
	fixture := newStarterFixture(t, config.WorkflowCompletionModeTool, workflowtest.ToolBatch("complete", llm.ToolCall{ID: "call-complete", Name: "complete_node", Input: input}))

	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Process(context.Background()); err != nil {
		t.Fatalf("Process: %v", err)
	}
	fixture.waitForCompletedRun(t, task.ID)

	transitions, err := fixture.store.ListTransitions(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 2 || transitions[1].Commentary != "finished tool" || transitions[1].OutputValues["summary"] != "tool ok" {
		t.Fatalf("completion transition = %+v", transitions)
	}
	reqs := fixture.client.Requests()
	if len(reqs) == 0 || !requestHasTool(reqs[0], "complete_node") {
		t.Fatalf("complete_node not exposed in request: %+v", reqs)
	}
}

func TestWorkflowRuntimeAskQuestionWaitsAndResumesSameRunSession(t *testing.T) {
	completeInput := json.RawMessage(`{"transition_id":"done","commentary":"answered and finished","summary":"question ok"}`)
	fixture := newStarterFixture(t, config.WorkflowCompletionModeTool,
		workflowtest.AskQuestion("call-ask", []byte(`{"question":"Need direction?","suggestions":["ship","stop"],"recommended_option_index":1}`)),
		workflowtest.ToolBatch("complete", llm.ToolCall{ID: "call-complete", Name: "complete_node", Input: completeInput}),
	)
	role := fixture.cfg.Settings.Subagents["coder"]
	role.Settings.EnabledTools = map[toolspec.ID]bool{toolspec.ToolAskQuestion: true}
	role.Sources["tools."+toolspec.ConfigName(toolspec.ToolAskQuestion)] = "test"
	fixture.cfg.Settings.Subagents["coder"] = role
	fixture.rebuildStarter(t)
	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Process(context.Background()); err != nil {
		t.Fatalf("Process: %v", err)
	}
	waiting := fixture.waitForWaitingAsk(t, task.ID, "call-ask")
	pending := fixture.runtimes.ListPendingPrompts(waiting.SessionID)
	if len(pending) != 1 || pending[0].Request.ID != "call-ask" || pending[0].Request.Question != "Need direction?" {
		t.Fatalf("pending prompts = %+v", pending)
	}
	if err := fixture.runtimes.SubmitPromptResponse(waiting.SessionID, askquestion.Response{RequestID: "call-ask", Answer: "Ship it"}, nil); err != nil {
		t.Fatalf("SubmitPromptResponse: %v", err)
	}
	fixture.waitForCompletedRun(t, task.ID)
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].SessionID != waiting.SessionID || runs[0].WaitingAskID != "" {
		t.Fatalf("run after answer = %+v, want same session and cleared waiting ask", runs)
	}
	transitions, err := fixture.store.ListTransitions(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 2 || transitions[1].Commentary != "answered and finished" || transitions[1].OutputValues["summary"] != "question ok" {
		t.Fatalf("completion transition = %+v", transitions)
	}
	reqs := fixture.client.Requests()
	if len(reqs) == 0 {
		t.Fatal("fake model was not called")
	}
	if !requestHasTool(reqs[0], string(toolspec.ToolAskQuestion)) {
		t.Fatalf("ask_question missing in workflow request: %+v", reqs[0].Tools)
	}
	if !requestHasTool(reqs[0], string(toolspec.ToolCompleteNode)) {
		t.Fatalf("complete_node missing in workflow request: %+v", reqs[0].Tools)
	}
}

func TestWorkflowRuntimeStarterCloseCancelsInFlightRun(t *testing.T) {
	client := newBlockingClient()
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput, workflowtest.FinalAnswer("{}"))
	fixture.clientFactory = func(workflowscheduler.StartRunRequest) llm.Client { return client }
	fixture.rebuildStarter(t)
	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Process(context.Background()); err != nil {
		t.Fatalf("Process: %v", err)
	}
	client.waitForCall(t)
	if err := fixture.starter.Close(); err != nil {
		t.Fatalf("starter.Close: %v", err)
	}
	fixture.waitForInterruptedRun(t, scheduler, task.ID, ReasonRuntimeCanceled)
}

func TestWorkflowRuntimeStarterCancelTaskRunsStopsLiveRuntimeAfterTaskCancel(t *testing.T) {
	client := newBlockingClient()
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput, workflowtest.FinalAnswer("{}"))
	fixture.clientFactory = func(workflowscheduler.StartRunRequest) llm.Client { return client }
	fixture.rebuildStarter(t)
	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Process(context.Background()); err != nil {
		t.Fatalf("Process: %v", err)
	}
	client.waitForCall(t)
	if err := fixture.store.CancelTask(context.Background(), task.ID, "test cancel"); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	if err := fixture.starter.CancelTaskRuns(context.Background(), task.ID); err != nil {
		t.Fatalf("CancelTaskRuns: %v", err)
	}
	client.waitForReturn(t)
}

func TestSchedulerRunsNextAgentWithBoundInputsAndTaskWorktreeContext(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput,
		workflowtest.FinalAnswer(`{"transition_id":"next","commentary":"first comments","summary":"first summary"}`),
		workflowtest.FinalAnswer(`{"transition_id":"done","commentary":"second done","summary":"second summary"}`),
	)
	workflowID := createChainedStarterWorkflow(t, fixture.store)
	if _, err := fixture.store.LinkWorkflow(context.Background(), fixture.projectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow chained: %v", err)
	}
	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("scheduler.Start: %v", err)
	}
	fixture.waitForRunCount(t, task.ID, 2)
	fixture.waitForAllRunsCompleted(t, task.ID, 2)

	reqs := fixture.client.Requests()
	if len(reqs) < 2 {
		t.Fatalf("fake model request count = %d, want 2", len(reqs))
	}
	assertPromptContains(t, reqs[1], []string{
		"Bound input values:",
		"prior_summary: first summary",
		"task_title: Run workflow",
		"Node prompt:\nUse Run workflow and first summary.",
	})
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	worktreeRoot := fixture.assertRunSessionUsesTaskWorktree(t, runs[1].SessionID)
	if strings.TrimSpace(runs[0].SessionID) == "" || strings.TrimSpace(runs[1].SessionID) == "" || runs[0].SessionID == runs[1].SessionID {
		t.Fatalf("runs = %+v, want new_session edge to create separate target session", runs)
	}
	assertPromptContains(t, reqs[1], []string{"\nCWD: " + worktreeRoot + "\n"})
}

func TestWorkflowRuntimeContinueSessionReusesSourceRunSession(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput,
		workflowtest.FinalAnswer(`{"transition_id":"next","commentary":"first comments","summary":"first summary"}`),
		workflowtest.FinalAnswer(`{"transition_id":"done","commentary":"second done","summary":"second summary"}`),
	)
	workflowID := createChainedStarterWorkflowWithContextMode(t, fixture.store, workflow.ContextModeContinueSession, "coder")
	if _, err := fixture.store.LinkWorkflow(context.Background(), fixture.projectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow chained: %v", err)
	}
	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("scheduler.Start: %v", err)
	}
	fixture.waitForRunCount(t, task.ID, 2)
	fixture.waitForAllRunsCompleted(t, task.ID, 2)

	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 || strings.TrimSpace(runs[0].SessionID) == "" || runs[0].SessionID != runs[1].SessionID {
		t.Fatalf("runs = %+v, want same session reused across continue_session edge", runs)
	}
	reqs := fixture.client.Requests()
	if len(reqs) < 2 {
		t.Fatalf("fake model request count = %d, want 2", len(reqs))
	}
	assertPromptContains(t, reqs[1], []string{"Node key: implement", "prior_summary: first summary"})
}

func TestWorkflowRuntimeContinueSessionKeepsLockedSetupAfterRoleConfigDrift(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput,
		workflowtest.FinalAnswer(`{"transition_id":"next","commentary":"first comments","summary":"first summary"}`),
		workflowtest.FinalAnswer(`{"transition_id":"done","commentary":"second done","summary":"second summary"}`),
	)
	workflowID := createChainedStarterWorkflowWithContextMode(t, fixture.store, workflow.ContextModeContinueSession, "coder")
	if _, err := fixture.store.LinkWorkflow(context.Background(), fixture.projectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow chained: %v", err)
	}
	task := fixture.createStartedTask(t)
	firstScheduler := fixture.scheduler(t)

	if err := firstScheduler.Process(context.Background()); err != nil {
		t.Fatalf("first Process: %v", err)
	}
	fixture.waitForRunCount(t, task.ID, 2)
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns after first run: %v", err)
	}
	if len(runs) != 2 || runs[0].CompletedAt == 0 || runs[1].StartedAt != 0 {
		t.Fatalf("runs after first process = %+v, want completed source and unstarted target", runs)
	}
	role := fixture.cfg.Settings.Subagents["coder"]
	role.Settings.Model = "gpt-5.5-drifted"
	role.Sources["model"] = "drifted-test"
	fixture.cfg.Settings.Subagents["coder"] = role
	fixture.rebuildStarter(t)
	secondScheduler := fixture.scheduler(t)

	if err := secondScheduler.Process(context.Background()); err != nil {
		t.Fatalf("second Process: %v", err)
	}
	fixture.waitForAllRunsCompleted(t, task.ID, 2)
	reqs := fixture.client.Requests()
	if len(reqs) < 2 {
		t.Fatalf("fake model request count = %d, want 2", len(reqs))
	}
	if reqs[0].Model == "" || reqs[1].Model != reqs[0].Model || reqs[1].Model == "gpt-5.5-drifted" {
		t.Fatalf("request models = %q then %q, want locked source session model reused after config drift", reqs[0].Model, reqs[1].Model)
	}
}

func TestWorkflowRuntimeCompactAndContinueCreatesFreshCrossRoleChildSession(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput,
		workflowtest.FinalAnswer(`{"transition_id":"next","commentary":"first comments","summary":"first summary"}`),
		workflowtest.FinalAnswer(`{"transition_id":"done","commentary":"second done","summary":"second summary"}`),
	)
	workflowID := createChainedStarterWorkflowWithContextMode(t, fixture.store, workflow.ContextModeCompactAndContinueSession, "reviewer")
	if _, err := fixture.store.LinkWorkflow(context.Background(), fixture.projectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow chained: %v", err)
	}
	task := fixture.createStartedTask(t)
	firstScheduler := fixture.scheduler(t)

	if err := firstScheduler.Process(context.Background()); err != nil {
		t.Fatalf("first Process: %v", err)
	}
	fixture.waitForRunCount(t, task.ID, 2)
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns after first run: %v", err)
	}
	if len(runs) != 2 || strings.TrimSpace(runs[0].SessionID) == "" || runs[1].StartedAt != 0 {
		t.Fatalf("runs after first process = %+v, want completed source and unstarted compact target", runs)
	}
	sourceEventSize := fixture.sessionEventsFileSize(t, runs[0].SessionID)
	secondScheduler := fixture.scheduler(t)

	if err := secondScheduler.Process(context.Background()); err != nil {
		t.Fatalf("second Process: %v", err)
	}
	fixture.waitForAllRunsCompleted(t, task.ID, 2)
	runs, err = fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns after second run: %v", err)
	}
	if runs[1].SessionID == "" || runs[1].SessionID == runs[0].SessionID {
		t.Fatalf("runs = %+v, want compact_and_continue_session to use fresh target session", runs)
	}
	targetRecord, err := fixture.metadata.ResolvePersistedSession(context.Background(), runs[1].SessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession target: %v", err)
	}
	if targetRecord.Meta == nil || targetRecord.Meta.ParentSessionID != runs[0].SessionID {
		t.Fatalf("target session parent = %+v, want source session %q", targetRecord.Meta, runs[0].SessionID)
	}
	if got := fixture.sessionEventsFileSize(t, runs[0].SessionID); got != sourceEventSize {
		t.Fatalf("source session events size = %d, want unchanged %d after compact continuation", got, sourceEventSize)
	}
	reqs := fixture.client.Requests()
	if len(reqs) < 2 {
		t.Fatalf("fake model request count = %d, want 2", len(reqs))
	}
	if reqs[1].Model != "gpt-5.4-reviewer" {
		t.Fatalf("second request model = %q, want reviewer role model", reqs[1].Model)
	}
	assertPromptContains(t, reqs[1], []string{"Context mode: compact_and_continue_session", "Source session:", "prior_summary: first summary"})
}

func TestWorkflowRuntimeStartFailsWhenRoleDisappearedAfterTaskStart(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput, workflowtest.FinalAnswer("{}"))
	delete(fixture.cfg.Settings.Subagents, "coder")
	fixture.rebuildStarter(t)
	task := fixture.createStartedTask(t)
	scheduler := fixture.scheduler(t)

	if err := scheduler.Process(context.Background()); err == nil {
		t.Fatalf("expected scheduler start error")
	}
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].InterruptedAt == 0 || runs[0].InterruptionReason != workflowscheduler.ReasonRuntimeStartFailed {
		t.Fatalf("run after missing role = %+v", runs)
	}
	var detail string
	_ = fixture.metadata.DB().QueryRowContext(context.Background(), `SELECT interruption_detail_json FROM task_runs WHERE id = ?`, string(runs[0].ID)).Scan(&detail)
	if !strings.Contains(detail, string(workflow.CodeAgentRoleMissing)) {
		t.Fatalf("interruption detail = %s, want %s", detail, workflow.CodeAgentRoleMissing)
	}
}

type starterFixture struct {
	cfg      config.App
	metadata *metadata.Store
	store    *workflowstore.Store
	view     interface {
		GetTask(context.Context, string) (serverapi.WorkflowTaskDetail, error)
	}
	worktrees     *metadataTaskWorktrees
	client        *workflowtest.ScriptedClient
	clientFactory func(workflowscheduler.StartRunRequest) llm.Client
	runtimes      *registry.RuntimeRegistry
	starter       *Starter
	workflowID    workflow.WorkflowID
	projectID     string
}

func newStarterFixture(t *testing.T, mode config.WorkflowCompletionMode, steps ...workflowtest.Step) starterFixture {
	t.Helper()
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.Settings.Workflow.CompletionMode = mode
	cfg.Settings.Reviewer.Frequency = "off"
	cfg.Settings.Subagents["coder"] = config.SubagentRole{Description: "Coder", Settings: config.Settings{Model: "gpt-5.4-mini"}, Sources: map[string]string{"model": "test"}}
	cfg.Settings.Subagents["reviewer"] = config.SubagentRole{Description: "Reviewer", Settings: config.Settings{Model: "gpt-5.4-reviewer"}, Sources: map[string]string{"model": "test"}}
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	if err := metadataStore.SetProjectKey(context.Background(), binding.ProjectID, "RUN"); err != nil {
		t.Fatalf("SetProjectKey: %v", err)
	}
	store, err := workflowstore.New(metadataStore, workflowstore.WithRoleResolver(workflow.StaticRoleResolver{"coder": true, "reviewer": true}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	view, err := newWorkflowView(metadataStore)
	if err != nil {
		t.Fatalf("workflowview.New: %v", err)
	}
	worktrees := &metadataTaskWorktrees{t: t, metadata: metadataStore, workspaceID: binding.WorkspaceID, root: filepath.Join(home, "task-worktrees")}
	client := workflowtest.NewScriptedClient(llm.ProviderCapabilities{ProviderID: "fake", SupportsResponsesAPI: mode == config.WorkflowCompletionModeStructuredOutput}, steps...)
	clientFactory := func(workflowscheduler.StartRunRequest) llm.Client { return client }
	runtimes := registry.NewRuntimeRegistry()
	starter, err := NewStarter(cfg, metadataStore, store, nil, nil, nil, runtimes, StarterOptions{
		ClientFactory: clientFactory,
		Worktrees:     worktrees,
	})
	if err != nil {
		t.Fatalf("NewStarter: %v", err)
	}
	t.Cleanup(func() { _ = starter.Close() })
	workflowID := createStarterWorkflow(t, store)
	if _, err := store.LinkWorkflow(context.Background(), binding.ProjectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	return starterFixture{cfg: cfg, metadata: metadataStore, store: store, view: view, worktrees: worktrees, client: client, clientFactory: clientFactory, runtimes: runtimes, starter: starter, workflowID: workflowID, projectID: binding.ProjectID}
}

func (f *starterFixture) rebuildStarter(t *testing.T) {
	t.Helper()
	if f.starter != nil {
		_ = f.starter.Close()
	}
	if f.runtimes == nil {
		f.runtimes = registry.NewRuntimeRegistry()
	}
	starter, err := NewStarter(f.cfg, f.metadata, f.store, nil, nil, nil, f.runtimes, StarterOptions{
		ClientFactory: f.clientFactory,
		Worktrees:     f.worktrees,
	})
	if err != nil {
		t.Fatalf("NewStarter: %v", err)
	}
	f.starter = starter
	t.Cleanup(func() { _ = starter.Close() })
}

func (f starterFixture) scheduler(t *testing.T) *workflowscheduler.Service {
	t.Helper()
	scheduler, err := workflowscheduler.New(f.store, f.starter, workflowscheduler.Config{Concurrency: 1})
	if err != nil {
		t.Fatalf("scheduler.New: %v", err)
	}
	f.starter.SetRuntimeFinished(scheduler.RuntimeFinished)
	t.Cleanup(func() { _ = scheduler.Close() })
	return scheduler
}

func (f starterFixture) createStartedTask(t *testing.T) workflowstore.TaskRecord {
	t.Helper()
	task, err := f.store.CreateTask(context.Background(), workflowstore.CreateTaskRequest{ProjectID: f.projectID, Title: "Run workflow", Body: "Body for workflow"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := f.store.StartTask(context.Background(), task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	return task
}

func (f starterFixture) waitForCompletedRun(t *testing.T, taskID workflow.TaskID) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := f.store.ListRuns(context.Background(), taskID)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(runs) == 1 && runs[0].CompletedAt != 0 {
			return
		}
		if len(runs) == 1 && runs[0].InterruptedAt != 0 {
			var detail string
			_ = f.metadata.DB().QueryRowContext(context.Background(), `SELECT interruption_detail_json FROM task_runs WHERE id = ?`, string(runs[0].ID)).Scan(&detail)
			t.Fatalf("run interrupted: %+v detail=%s", runs[0], detail)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for workflow run completion")
}

func (f starterFixture) waitForWaitingAsk(t *testing.T, taskID workflow.TaskID, askID string) workflowstore.RunRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := f.store.ListRuns(context.Background(), taskID)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(runs) == 1 && runs[0].WaitingAskID == askID {
			if strings.TrimSpace(runs[0].SessionID) == "" {
				t.Fatalf("waiting run has no session: %+v", runs[0])
			}
			if runs[0].CompletedAt != 0 || runs[0].InterruptedAt != 0 {
				t.Fatalf("waiting run has terminal outcome: %+v", runs[0])
			}
			return runs[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for workflow run ask %s", askID)
	return workflowstore.RunRecord{}
}

func (f starterFixture) waitForRunCount(t *testing.T, taskID workflow.TaskID, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := f.store.ListRuns(context.Background(), taskID)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(runs) == count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d workflow runs", count)
}

func (f starterFixture) waitForAllRunsCompleted(t *testing.T, taskID workflow.TaskID, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := f.store.ListRuns(context.Background(), taskID)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		completed := 0
		for _, run := range runs {
			if run.InterruptedAt != 0 {
				t.Fatalf("run interrupted: %+v", run)
			}
			if run.CompletedAt != 0 {
				completed++
			}
		}
		if len(runs) == count && completed == count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d completed workflow runs", count)
}

func (f starterFixture) waitForInterruptedRun(t *testing.T, scheduler *workflowscheduler.Service, taskID workflow.TaskID, reason string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := f.store.ListRuns(context.Background(), taskID)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(runs) == 1 && runs[0].InterruptedAt != 0 {
			if runs[0].InterruptionReason != reason {
				t.Fatalf("interruption reason = %q, want %q", runs[0].InterruptionReason, reason)
			}
			if scheduler.ActiveCount() != 0 {
				t.Fatalf("scheduler active count = %d, want 0", scheduler.ActiveCount())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for workflow run interruption")
}

func (f starterFixture) assertRunSessionUsesTaskWorktree(t *testing.T, sessionID string) string {
	t.Helper()
	target, err := f.metadata.ResolveSessionExecutionTarget(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget: %v", err)
	}
	if strings.TrimSpace(target.WorktreeID) == "" || !strings.HasSuffix(target.EffectiveWorkdir, string(filepath.Separator)+"RUN-1") {
		t.Fatalf("session target = %+v, want task worktree", target)
	}
	return target.EffectiveWorkdir
}

func (f starterFixture) sessionEventsFileSize(t *testing.T, sessionID string) int64 {
	t.Helper()
	record, err := f.metadata.ResolvePersistedSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	info, err := os.Stat(filepath.Join(record.SessionDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("stat events.jsonl: %v", err)
	}
	return info.Size()
}

type metadataTaskWorktrees struct {
	t           *testing.T
	metadata    *metadata.Store
	workspaceID string
	root        string
}

func (e *metadataTaskWorktrees) EnsureTaskWorktree(ctx context.Context, taskID string) error {
	if e == nil || e.metadata == nil {
		return nil
	}
	var shortID string
	if err := e.metadata.DB().QueryRowContext(ctx, `SELECT short_id FROM tasks WHERE id = ?`, taskID).Scan(&shortID); err != nil {
		return err
	}
	worktreeID := "worktree-" + taskID
	worktreeRoot := filepath.Join(e.root, shortID)
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		return err
	}
	if err := e.metadata.UpsertWorktreeRecord(ctx, metadata.WorktreeRecord{
		ID:              worktreeID,
		WorkspaceID:     e.workspaceID,
		CanonicalRoot:   worktreeRoot,
		DisplayName:     shortID,
		Availability:    "available",
		BuilderManaged:  true,
		CreatedBranch:   true,
		GitMetadataJSON: `{}`,
	}); err != nil {
		return err
	}
	result, err := e.metadata.DB().ExecContext(ctx, `UPDATE tasks SET managed_worktree_id = ?, updated_at_unix_ms = ? WHERE id = ?`, sql.NullString{String: worktreeID, Valid: true}, time.Now().UTC().UnixMilli(), taskID)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func createStarterWorkflow(t *testing.T, store *workflowstore.Store) workflow.WorkflowID {
	t.Helper()
	ctx := context.Background()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Runner Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := starterNodeByKind(t, def, workflow.NodeKindStart)
	done := starterNodeByKind(t, def, workflow.NodeKindTerminal)
	agentID := workflow.NodeID("node-agent-" + string(created.ID))
	if _, err := store.AddNode(ctx, workflowstore.NodeRecord{ID: agentID, WorkflowID: created.ID, Key: "agent", Kind: workflow.NodeKindAgent, DisplayName: "Agent", SubagentRole: "coder", PromptTemplate: "Implement the task.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}}); err != nil {
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

func createChainedStarterWorkflow(t *testing.T, store *workflowstore.Store) workflow.WorkflowID {
	t.Helper()
	return createChainedStarterWorkflowWithContextMode(t, store, workflow.ContextModeNewSession, "coder")
}

func createChainedStarterWorkflowWithContextMode(t *testing.T, store *workflowstore.Store, contextMode workflow.ContextMode, targetRole string) workflow.WorkflowID {
	t.Helper()
	ctx := context.Background()
	created, err := store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Chained Runner Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	def, _, err := store.GetDefinition(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	start := starterNodeByKind(t, def, workflow.NodeKindStart)
	done := starterNodeByKind(t, def, workflow.NodeKindTerminal)
	planID := workflow.NodeID("node-plan-" + string(created.ID))
	implID := workflow.NodeID("node-impl-" + string(created.ID))
	for _, node := range []workflowstore.NodeRecord{
		{ID: planID, WorkflowID: created.ID, Key: "plan", Kind: workflow.NodeKindAgent, DisplayName: "Plan", SubagentRole: "coder", PromptTemplate: "Plan the task.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
		{ID: implID, WorkflowID: created.ID, Key: "implement", Kind: workflow.NodeKindAgent, DisplayName: "Implement", SubagentRole: targetRole, PromptTemplate: "Use {{.Inputs.task_title}} and {{.Inputs.prior_summary}}.", OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
	} {
		if _, err := store.AddNode(ctx, node); err != nil {
			t.Fatalf("AddNode %s: %v", node.Key, err)
		}
	}
	startGroup := workflow.TransitionGroupID("group-start-" + string(created.ID))
	nextGroup := workflow.TransitionGroupID("group-next-" + string(created.ID))
	doneGroup := workflow.TransitionGroupID("group-done-" + string(created.ID))
	for _, group := range []workflowstore.TransitionGroupRecord{
		{ID: startGroup, WorkflowID: created.ID, SourceNodeID: start.ID, TransitionID: "start", DisplayName: "Start"},
		{ID: nextGroup, WorkflowID: created.ID, SourceNodeID: planID, TransitionID: "next", DisplayName: "Next"},
		{ID: doneGroup, WorkflowID: created.ID, SourceNodeID: implID, TransitionID: "done", DisplayName: "Done"},
	} {
		if _, err := store.AddTransitionGroup(ctx, group); err != nil {
			t.Fatalf("AddTransitionGroup %s: %v", group.TransitionID, err)
		}
	}
	for _, edge := range []workflowstore.EdgeRecord{
		{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: startGroup, Key: "start", TargetNodeID: planID, ContextMode: workflow.ContextModeNewSession},
		{ID: workflow.EdgeID("edge-next-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: nextGroup, Key: "next", TargetNodeID: implID, ContextMode: contextMode, InputBindings: []workflow.InputBinding{{Name: "task_title", Source: workflow.BindingSourceTask, Field: "title"}, {Name: "prior_summary", Source: workflow.BindingSourceTransitionOutput, Field: "summary"}}, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
		{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: doneGroup, Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession, OutputRequirements: []workflow.OutputRequirement{{FieldName: "summary"}}},
	} {
		if _, err := store.AddEdge(ctx, edge); err != nil {
			t.Fatalf("AddEdge %s: %v", edge.Key, err)
		}
	}
	return created.ID
}

func starterNodeByKind(t *testing.T, def workflow.Definition, kind workflow.NodeKind) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if node.Kind == kind {
			return node
		}
	}
	t.Fatalf("node kind %s missing", kind)
	return workflow.Node{}
}

func assertPromptContains(t *testing.T, req llm.Request, needles []string) {
	t.Helper()
	var haystack strings.Builder
	for _, item := range req.Items {
		haystack.WriteString(item.Content)
		haystack.WriteString("\n")
	}
	text := haystack.String()
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			t.Fatalf("request prompt missing %q in:\n%s", needle, text)
		}
	}
}

func requestHasTool(req llm.Request, name string) bool {
	for _, tool := range req.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

type blockingClient struct {
	called chan struct{}
	done   chan struct{}
	once   sync.Once
}

func newBlockingClient() *blockingClient {
	return &blockingClient{called: make(chan struct{}), done: make(chan struct{})}
}

func (c *blockingClient) Generate(ctx context.Context, req llm.Request) (llm.Response, error) {
	c.once.Do(func() { close(c.called) })
	defer close(c.done)
	<-ctx.Done()
	return llm.Response{}, ctx.Err()
}

func (c *blockingClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{ProviderID: "fake", SupportsResponsesAPI: true}, nil
}

func (c *blockingClient) waitForCall(t *testing.T) {
	t.Helper()
	select {
	case <-c.called:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fake model call")
	}
}

func (c *blockingClient) waitForReturn(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for fake model return")
	}
}

func newWorkflowView(store *metadata.Store) (interface {
	GetTask(context.Context, string) (serverapi.WorkflowTaskDetail, error)
}, error) {
	return workflowview.New(store)
}
