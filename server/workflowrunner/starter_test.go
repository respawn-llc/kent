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

	"core/server/llm"
	"core/server/metadata"
	"core/server/registry"
	"core/server/session"
	"core/server/sessionpath"
	askquestion "core/server/tools/askquestion"
	"core/server/workflow"
	"core/server/workflowruntime/workflowtest"
	"core/server/workflowscheduler"
	"core/server/workflowstore"
	"core/server/workflowview"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

func TestSchedulerRunsNewSessionWorkflowNodeWithStructuredOutput(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput, workflowtest.FinalAnswer(`{"commentary":"finished structured"}`))

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
	if len(transitions) != 2 || transitions[1].TransitionID != "done" || transitions[1].Commentary != "finished structured" || len(transitions[1].OutputValues) != 0 {
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
	assertNoUserPrompt(t, first)
	fixture.assertRunSessionUsesTaskWorktree(t, runs[0].SessionID)
	if scheduler.ActiveCount() != 0 {
		t.Fatalf("scheduler active count = %d, want 0 after runtime finish", scheduler.ActiveCount())
	}
}

func TestSchedulerRunsNewSessionWorkflowNodeWithCompleteNodeTool(t *testing.T) {
	input := json.RawMessage(`{"commentary":"finished tool"}`)
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
	if len(transitions) != 2 || transitions[1].Commentary != "finished tool" || len(transitions[1].OutputValues) != 0 {
		t.Fatalf("completion transition = %+v", transitions)
	}
	reqs := fixture.client.Requests()
	if len(reqs) == 0 || !requestHasTool(reqs[0], "complete_node") {
		t.Fatalf("complete_node not exposed in request: %+v", reqs)
	}
}

func TestWorkflowRuntimeAskQuestionWaitsAndResumesSameRunSession(t *testing.T) {
	completeInput := json.RawMessage(`{"commentary":"answered and finished"}`)
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
	if len(transitions) != 2 || transitions[1].Commentary != "answered and finished" || len(transitions[1].OutputValues) != 0 {
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

func TestWorkflowRuntimeMultipleAskQuestionsInOneToolBatchResumeSequentially(t *testing.T) {
	completeInput := json.RawMessage(`{"commentary":"answered both and finished"}`)
	fixture := newStarterFixture(t, config.WorkflowCompletionModeTool,
		workflowtest.ToolBatch("questions",
			llm.ToolCall{ID: "call-ask-1", Name: "ask_question", Input: json.RawMessage(`{"question":"First direction?","suggestions":["ship","stop"],"recommended_option_index":1}`)},
			llm.ToolCall{ID: "call-ask-2", Name: "ask_question", Input: json.RawMessage(`{"question":"Second direction?","suggestions":["fast","safe"],"recommended_option_index":2}`)},
		),
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
	first := fixture.waitForWaitingAsk(t, task.ID, "call-ask-1")
	if err := fixture.runtimes.SubmitPromptResponse(first.SessionID, askquestion.Response{RequestID: "call-ask-1", Answer: "Ship it"}, nil); err != nil {
		t.Fatalf("SubmitPromptResponse first: %v", err)
	}
	second := fixture.waitForWaitingAsk(t, task.ID, "call-ask-2")
	if second.SessionID != first.SessionID {
		t.Fatalf("second ask session = %q, want first session %q", second.SessionID, first.SessionID)
	}
	if err := fixture.runtimes.SubmitPromptResponse(second.SessionID, askquestion.Response{RequestID: "call-ask-2", Answer: "Keep it safe"}, nil); err != nil {
		t.Fatalf("SubmitPromptResponse second: %v", err)
	}
	fixture.waitForCompletedRun(t, task.ID)
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].SessionID != first.SessionID || runs[0].WaitingAskID != "" {
		t.Fatalf("run after answers = %+v, want same session and cleared waiting ask", runs)
	}
	askResults := workflowRequestAskQuestionToolMessages(fixture.client.Requests())
	if len(askResults) != 2 {
		t.Fatalf("ask_question tool results = %+v, want two completed asks", askResults)
	}
	seenAskResults := map[string]bool{}
	for _, msg := range askResults {
		seenAskResults[msg.ToolCallID] = true
		trimmed := strings.TrimSpace(msg.Content)
		if strings.HasPrefix(trimmed, "{") {
			var payload map[string]json.RawMessage
			if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
				t.Fatalf("decode ask_question tool message: %v", err)
			}
			if _, ok := payload["error"]; ok {
				t.Fatalf("ask_question tool result contains error payload: %+v", msg)
			}
		}
	}
	if !seenAskResults["call-ask-1"] || !seenAskResults["call-ask-2"] {
		t.Fatalf("ask_question tool result call IDs = %+v, want both asks", askResults)
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
	if !client.returned() {
		t.Fatal("CancelTaskRuns returned before live runtime stopped")
	}
}

func TestSchedulerRunsNextAgentWithBoundInputsAndTaskWorktreeContext(t *testing.T) {
	fixture := newChainedStarterFixture(t)
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
		"Use Run workflow and first summary.",
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

func TestBuildWorkflowTaskInstructionsRendersTransitionParameters(t *testing.T) {
	instructions, err := BuildWorkflowTaskInstructions(workflowstore.RunStartContext{
		Task: workflowstore.TaskRecord{
			ID:         "task-1",
			WorkflowID: "workflow-1",
			ShortID:    "RUN-1",
			Title:      "Task title",
			Body:       "Task body",
		},
		Workflow: workflowstore.WorkflowRecord{ID: "workflow-1"},
		Node: workflowstore.NodeRecord{
			ID:          "node-review",
			Key:         "review",
			DisplayName: "Review",
		},
		PromptTemplate:       "Use {{.Params.direct}} and {{.Params.plan.summary}}.",
		ParameterValues:      map[string]string{"direct": "direct parameter"},
		PriorParameterValues: map[string]map[string]string{"plan": {"summary": "plan parameter"}},
	})
	if err != nil {
		t.Fatalf("BuildWorkflowTaskInstructions: %v", err)
	}
	if instructions.NodePrompt != "Use direct parameter and plan parameter." {
		t.Fatalf("node prompt = %q", instructions.NodePrompt)
	}
}

func TestWorkflowRuntimeContinueSessionReusesSourceRunSession(t *testing.T) {
	fixture := newChainedStarterFixture(t)
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
	assertPromptContains(t, reqs[1], []string{"Use Run workflow and first summary."})
}

func TestWorkflowRuntimeContinueSessionKeepsLockedSetupAfterRoleConfigDrift(t *testing.T) {
	fixture := newChainedStarterFixture(t)
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

func TestReusesExistingSession(t *testing.T) {
	mk := func(mode workflow.ContextMode, runSessionID string, fanout bool) workflowstore.RunStartContext {
		return workflowstore.RunStartContext{
			ContextMode:    mode,
			IsFanoutBranch: fanout,
			Run:            workflowstore.RunRecord{SessionID: runSessionID},
		}
	}
	cases := []struct {
		name string
		in   workflowstore.RunStartContext
		want bool
	}{
		{"new session is disposable", mk(workflow.ContextModeNewSession, "", false), false},
		{"continue reuses source", mk(workflow.ContextModeContinueSession, "", false), true},
		{"compact in-place reuses source", mk(workflow.ContextModeCompactAndContinueSession, "", false), true},
		{"compact fan-out clones a disposable copy", mk(workflow.ContextModeCompactAndContinueSession, "", true), false},
		{"resume reuses the run session", mk(workflow.ContextModeNewSession, "session-1", false), true},
	}
	for _, tc := range cases {
		if got := reusesExistingSession(tc.in); got != tc.want {
			t.Fatalf("%s: reusesExistingSession = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWorkflowRuntimeCompactAndContinueReusesSourceSessionWithRealCompaction(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput,
		workflowtest.FinalAnswer(`{"commentary":"first comments","prior_summary":"first summary"}`),
		workflowtest.FinalAnswer("compacted prior work summary"),
		workflowtest.FinalAnswer(`{"commentary":"second done"}`),
	)
	workflowID := createChainedStarterWorkflowWithContextMode(t, fixture.store, workflow.ContextModeCompactAndContinueSession, "coder")
	if _, err := fixture.store.LinkWorkflow(context.Background(), fixture.projectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow chained: %v", err)
	}
	task := fixture.createStartedTask(t)

	if err := fixture.scheduler(t).Process(context.Background()); err != nil {
		t.Fatalf("first Process: %v", err)
	}
	fixture.waitForRunCount(t, task.ID, 2)
	if err := fixture.scheduler(t).Process(context.Background()); err != nil {
		t.Fatalf("second Process: %v", err)
	}
	fixture.waitForAllRunsCompleted(t, task.ID, 2)

	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %+v, want 2", runs)
	}
	if runs[1].SessionID == "" || runs[1].SessionID != runs[0].SessionID {
		t.Fatalf("runs = %+v, want compact_and_continue_session to reuse the source session in place", runs)
	}
	events := fixture.sessionEventsText(t, runs[0].SessionID)
	if strings.Contains(events, "Workflow compacted continuation context.") {
		t.Fatalf("session events unexpectedly contain the removed compact-continuation stub: %q", events)
	}
	if !strings.Contains(events, "history_replaced") {
		t.Fatalf("session events missing real compaction (history_replaced): %q", events)
	}
	// plan turn + real compaction summary + node turn = at least three model
	// calls, proving compaction ran in place rather than fabricating a stub.
	if reqs := fixture.client.Requests(); len(reqs) < 3 {
		t.Fatalf("model request count = %d, want >=3 (plan, compaction, node turn)", len(reqs))
	}
	// The history_replaced event durably records the run that committed the
	// compaction, so resume reconstructs it and a resumed run (same ID) skips
	// recompaction while a fresh in-place handoff recompacts.
	if runID := fixture.historyReplacedWorkflowRunID(t, runs[1].SessionID); runID != string(runs[1].ID) {
		t.Fatalf("history_replaced workflow_run_id = %q, want run %q", runID, runs[1].ID)
	}
}

func TestWorkflowRuntimeCompactAndContinueAllowsCrossRole(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput,
		workflowtest.FinalAnswer(`{"commentary":"first comments","prior_summary":"first summary"}`),
		workflowtest.FinalAnswer(`{"commentary":"compaction summary"}`),
		workflowtest.FinalAnswer(`{"commentary":"second done"}`),
	)
	workflowID := createChainedStarterWorkflowWithContextMode(t, fixture.store, workflow.ContextModeCompactAndContinueSession, "reviewer")
	if _, err := fixture.store.LinkWorkflow(context.Background(), fixture.projectID, workflowID, true); err != nil {
		t.Fatalf("LinkWorkflow chained: %v", err)
	}
	task := fixture.createStartedTask(t)

	if err := fixture.scheduler(t).Process(context.Background()); err != nil {
		t.Fatalf("first Process: %v", err)
	}
	fixture.waitForRunCount(t, task.ID, 2)
	if err := fixture.scheduler(t).Process(context.Background()); err != nil {
		t.Fatalf("second Process: %v", err)
	}
	fixture.waitForCompletedRunCount(t, task.ID, 2)

	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 || runs[1].InterruptedAt != 0 || runs[1].CompletedAt == 0 || runs[0].SessionID != runs[1].SessionID {
		t.Fatalf("runs = %+v, want cross-role compact_and_continue to complete in source session", runs)
	}
	containerDir := config.ProjectSessionsRoot(fixture.cfg, fixture.projectID)
	sourceDir, err := sessionpath.ResolveScopedSessionDir(containerDir, runs[1].SessionID)
	if err != nil {
		t.Fatalf("ResolveScopedSessionDir: %v", err)
	}
	sourceStore, err := session.Open(sourceDir, fixture.metadata.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("Open source session: %v", err)
	}
	if got := sourceStore.Meta().Continuation; got == nil || got.AgentRole != "reviewer" {
		t.Fatalf("continuation role = %+v, want reviewer", got)
	}
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

func TestWorkflowRuntimeResumeInterruptedRunUsesSameSession(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeTool)
	task := fixture.createStartedTask(t)
	if err := fixture.worktrees.EnsureTaskWorktree(context.Background(), string(task.ID)); err != nil {
		t.Fatalf("EnsureTaskWorktree: %v", err)
	}
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns initial: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("initial runs = %+v", runs)
	}
	claimed, err := fixture.store.ClaimRun(context.Background(), runs[0].ID, 0)
	if err != nil {
		t.Fatalf("ClaimRun: %v", err)
	}
	input, err := fixture.store.GetRunStartContext(context.Background(), claimed.ID)
	if err != nil {
		t.Fatalf("GetRunStartContext: %v", err)
	}
	plan, _, err := fixture.starter.planSession(context.Background(), input)
	if err != nil {
		t.Fatalf("planSession initial: %v", err)
	}
	if _, err := fixture.metadata.ResolvePersistedSession(context.Background(), plan.Store.Meta().SessionID); err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	if err := fixture.store.AttachRunSession(context.Background(), claimed.ID, claimed.Generation, plan.Store.Meta().SessionID); err != nil {
		t.Fatalf("AttachRunSession: %v", err)
	}
	if err := fixture.store.InterruptRunGeneration(context.Background(), claimed.ID, claimed.Generation, "manual", "{}"); err != nil {
		t.Fatalf("InterruptRunGeneration: %v", err)
	}
	runs, err = fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListRuns interrupted: %v", err)
	}
	if len(runs) != 1 || runs[0].InterruptedAt == 0 || strings.TrimSpace(runs[0].SessionID) == "" {
		t.Fatalf("interrupted run session = %+v", runs)
	}
	originalSessionID := runs[0].SessionID
	resumed, err := fixture.store.ResumeTaskRun(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ResumeTaskRun: %v", err)
	}
	resumedInput, err := fixture.store.GetRunStartContext(context.Background(), resumed.ID)
	if err != nil {
		t.Fatalf("GetRunStartContext resumed: %v", err)
	}
	resumePlan, _, err := fixture.starter.planSession(context.Background(), resumedInput)
	if err != nil {
		t.Fatalf("planSession resumed: %v", err)
	}
	if resumed.ID != runs[0].ID || resumePlan.Store.Meta().SessionID != originalSessionID {
		t.Fatalf("resume plan session = %s for run %+v, want same session %s", resumePlan.Store.Meta().SessionID, resumed, originalSessionID)
	}
}

func TestRemoveFanoutCloneDeletesOrphanedClone(t *testing.T) {
	fixture := newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput, workflowtest.FinalAnswer(`{"commentary":"done"}`))
	task := fixture.createStartedTask(t)
	if err := fixture.scheduler(t).Process(context.Background()); err != nil {
		t.Fatalf("Process: %v", err)
	}
	fixture.waitForCompletedRun(t, task.ID)
	runs, err := fixture.store.ListRuns(context.Background(), task.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListRuns = %+v, err %v", runs, err)
	}

	containerDir := config.ProjectSessionsRoot(fixture.cfg, fixture.projectID)
	cloneID, err := fixture.starter.cloneSourceSessionForFanout(containerDir, runs[0].SessionID)
	if err != nil {
		t.Fatalf("cloneSourceSessionForFanout: %v", err)
	}
	cloneDir, err := sessionpath.ResolveScopedSessionDir(containerDir, cloneID)
	if err != nil {
		t.Fatalf("ResolveScopedSessionDir: %v", err)
	}
	if _, err := os.Stat(cloneDir); err != nil {
		t.Fatalf("clone dir should exist after clone: %v", err)
	}

	fixture.starter.removeFanoutClone(context.Background(), containerDir, cloneID)
	if _, err := os.Stat(cloneDir); !os.IsNotExist(err) {
		t.Fatalf("clone dir should be removed, stat err = %v", err)
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
	view, err := workflowview.New(metadataStore)
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

func newChainedStarterFixture(t *testing.T) starterFixture {
	t.Helper()
	return newStarterFixture(t, config.WorkflowCompletionModeStructuredOutput,
		workflowtest.FinalAnswer(`{"commentary":"first comments","prior_summary":"first summary"}`),
		workflowtest.FinalAnswer(`{"commentary":"second done"}`),
	)
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

func (f starterFixture) waitForCompletedRunCount(t *testing.T, taskID workflow.TaskID, count int) {
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
				var detail string
				_ = f.metadata.DB().QueryRowContext(context.Background(), `SELECT interruption_detail_json FROM task_runs WHERE id = ?`, string(run.ID)).Scan(&detail)
				t.Fatalf("run interrupted: %+v detail=%s", run, detail)
			}
			if run.CompletedAt != 0 {
				completed++
			}
		}
		if completed == count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d completed workflow runs", count)
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
	record, err := f.metadata.ResolvePersistedSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	if got, want := filepath.Dir(record.SessionDir), config.ProjectSessionsRoot(f.cfg, f.projectID); got != want {
		t.Fatalf("session dir parent = %q, want project sessions root %q", got, want)
	}
	target, err := f.metadata.ResolveSessionExecutionTarget(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget: %v", err)
	}
	if strings.TrimSpace(target.WorktreeID) == "" || !strings.HasSuffix(target.EffectiveWorkdir, string(filepath.Separator)+"RUN-1") {
		t.Fatalf("session target = %+v, want task worktree", target)
	}
	return target.EffectiveWorkdir
}

func (f starterFixture) sessionEventsText(t *testing.T, sessionID string) string {
	t.Helper()
	record, err := f.metadata.ResolvePersistedSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(record.SessionDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	return string(data)
}

func workflowRequestAskQuestionToolMessages(reqs []llm.Request) []llm.Message {
	messages := []llm.Message{}
	for _, req := range reqs {
		for _, msg := range llm.MessagesFromItems(req.Items) {
			if msg.Role != llm.RoleTool || msg.Name != string(toolspec.ToolAskQuestion) {
				continue
			}
			switch msg.ToolCallID {
			case "call-ask-1", "call-ask-2":
				messages = append(messages, msg)
			}
		}
	}
	return messages
}

// historyReplacedWorkflowRunID decodes the latest history_replaced event in the
// session and returns its recorded workflow run provenance.
func (f starterFixture) historyReplacedWorkflowRunID(t *testing.T, sessionID string) string {
	t.Helper()
	runID := ""
	for _, line := range strings.Split(strings.TrimSpace(f.sessionEventsText(t, sessionID)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Kind    string `json:"kind"`
			Payload struct {
				WorkflowRunID string `json:"workflow_run_id"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode session event: %v", err)
		}
		if event.Kind == "history_replaced" {
			runID = event.Payload.WorkflowRunID
		}
	}
	return runID
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
		Managed:         true,
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
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-start-" + string(created.ID)), Key: "start", TargetNodeID: agentID, ContextMode: workflow.ContextModeNewSession, PromptTemplate: "Implement the task."}); err != nil {
		t.Fatalf("AddEdge start: %v", err)
	}
	if _, err := store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID("group-done-" + string(created.ID)), WorkflowID: created.ID, SourceNodeID: agentID, TransitionID: "done", DisplayName: "Done"}); err != nil {
		t.Fatalf("AddTransitionGroup done: %v", err)
	}
	if _, err := store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: workflow.TransitionGroupID("group-done-" + string(created.ID)), Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession}); err != nil {
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
		{ID: implID, WorkflowID: created.ID, Key: "implement", Kind: workflow.NodeKindAgent, DisplayName: "Implement", SubagentRole: targetRole, PromptTemplate: "Use {{.TaskTitle}} and {{.Inputs.prior_summary}}.", InputFields: []workflow.InputField{{Name: "prior_summary", Description: "Prior summary."}}, OutputFields: []workflow.OutputField{{Name: "summary", Description: "Summary."}}},
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
		{ID: workflow.EdgeID("edge-start-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: startGroup, Key: "start", TargetNodeID: planID, ContextMode: workflow.ContextModeNewSession, PromptTemplate: "Plan the task."},
		{ID: workflow.EdgeID("edge-next-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: nextGroup, Key: "next", TargetNodeID: implID, ContextMode: contextMode, PromptTemplate: "Use {{.TaskTitle}} and {{.Params.prior_summary}}.", Parameters: []workflow.Parameter{{Key: "prior_summary", Description: "Prior summary."}}},
		{ID: workflow.EdgeID("edge-done-" + string(created.ID)), WorkflowID: created.ID, TransitionGroupID: doneGroup, Key: "done", TargetNodeID: done.ID, ContextMode: workflow.ContextModeNewSession},
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
	text := requestPromptText(req)
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			t.Fatalf("request prompt missing %q in:\n%s", needle, text)
		}
	}
}

func requestPromptText(req llm.Request) string {
	var haystack strings.Builder
	for _, item := range req.Items {
		haystack.WriteString(item.Content)
		haystack.WriteString("\n")
	}
	return haystack.String()
}

func assertNoUserPrompt(t *testing.T, req llm.Request) {
	t.Helper()
	userPrompts := []string{}
	for _, item := range req.Items {
		if item.Role == llm.RoleUser {
			userPrompts = append(userPrompts, item.Content)
		}
	}
	if len(userPrompts) == 0 {
		return
	}
	t.Fatalf("request should not include user prompts, got %+v", userPrompts)
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

func (c *blockingClient) returned() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
