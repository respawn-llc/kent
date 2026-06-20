package app

import (
	"strings"
	"testing"

	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func applyGoalCmdMessagesForTest(t *testing.T, model *uiModel, cmd tea.Cmd) *uiModel {
	t.Helper()
	for _, msg := range collectCmdMessages(t, cmd) {
		next, nextCmd := model.Update(msg)
		model = next.(*uiModel)
		model = applyGoalCmdMessagesForTest(t, model, nextCmd)
	}
	return model
}

func updateGoalForTest(t *testing.T, model *uiModel, msg tea.Msg) *uiModel {
	t.Helper()
	next, cmd := model.Update(msg)
	return applyGoalCmdMessagesForTest(t, next.(*uiModel), cmd)
}

func TestGoalMutationsCoalesceAfterApplyingInFlightCompletion(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: clientui.RuntimeGoalStatusPaused}}
	m := newProjectedClosedUIModel(client)

	firstCmd := m.goalRuntimeCommand(goalRuntimePause, "")
	if firstCmd == nil {
		t.Fatal("expected first goal mutation command")
	}
	secondCmd := m.goalRuntimeCommand(goalRuntimeResume, "")
	if secondCmd != nil {
		t.Fatal("did not expect second goal mutation command while first is in flight")
	}
	firstMsgs := collectCmdMessages(t, firstCmd)
	if client.pauseGoalCalls != 1 || client.resumeGoalCalls != 0 {
		t.Fatalf("unexpected pre-completion goal calls: pause=%d resume=%d", client.pauseGoalCalls, client.resumeGoalCalls)
	}

	var firstDone goalRuntimeDoneMsg
	for _, msg := range firstMsgs {
		if typed, ok := msg.(goalRuntimeDoneMsg); ok {
			firstDone = typed
		}
	}
	next, followUpCmd := m.Update(firstDone)
	updated := next.(*uiModel)
	if followUpCmd == nil {
		t.Fatal("expected follow-up goal mutation command")
	}
	if updated.goal.goal == nil || updated.goal.goal.Status != clientui.RuntimeGoalStatusPaused {
		t.Fatalf("expected pause completion to update goal UI before follow-up, got %+v", updated.goal.goal)
	}
	_ = collectCmdMessages(t, followUpCmd)
	if client.resumeGoalCalls != 1 {
		t.Fatalf("expected serialized resume call after pause completion, got %d", client.resumeGoalCalls)
	}
}

func TestGoalPreflightDoesNotStartMutationAfterLaterMutation(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedClosedUIModel(client)

	checkCmd := m.goalRuntimeCommand(goalRuntimeCheckSet, "older objective")
	if checkCmd == nil {
		t.Fatal("expected goal check command")
	}
	pauseCmd := m.goalRuntimeCommand(goalRuntimePause, "")
	if pauseCmd == nil {
		t.Fatal("expected later goal pause command")
	}

	var pauseDone goalRuntimeDoneMsg
	for _, msg := range collectCmdMessages(t, pauseCmd) {
		if typed, ok := msg.(goalRuntimeDoneMsg); ok {
			pauseDone = typed
		}
	}
	next, _ := m.Update(pauseDone)
	updated := next.(*uiModel)
	if updated.goal.goal == nil || updated.goal.goal.Status != clientui.RuntimeGoalStatusPaused {
		t.Fatalf("expected later pause mutation to apply, got %+v", updated.goal.goal)
	}

	var checkDone goalRuntimeDoneMsg
	for _, msg := range collectCmdMessages(t, checkCmd) {
		if typed, ok := msg.(goalRuntimeDoneMsg); ok {
			checkDone = typed
		}
	}
	next, followCmd := updated.Update(checkDone)
	updated = next.(*uiModel)
	if followCmd != nil {
		t.Fatal("did not expect stale preflight to schedule older set-goal mutation")
	}
	if client.setGoalArg != "" {
		t.Fatalf("stale preflight started set-goal mutation for %q", client.setGoalArg)
	}
	if updated.goal.goal == nil || updated.goal.goal.Status != clientui.RuntimeGoalStatusPaused {
		t.Fatalf("expected stale preflight not to overwrite later pause, got %+v", updated.goal.goal)
	}
}

func TestWorkflowSessionGoalCommandIsBlockedBeforeRuntimeCall(t *testing.T) {
	client := &runtimeControlFakeClient{status: clientui.RuntimeStatus{WorkflowSession: &clientui.WorkflowSessionStatus{RunID: "run-1"}}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal ship workflow"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)

	if client.setGoalArg != "" || client.showGoalCalls != 0 {
		t.Fatalf("goal runtime calls should be blocked, set=%q show=%d", client.setGoalArg, client.showGoalCalls)
	}
	if updated.transientStatus != workflowGoalUnavailableMessage {
		t.Fatalf("transient status = %q, want workflow goal block", updated.transientStatus)
	}
}

func TestGoalClearSuspendedActiveGoalSkipsConfirmation(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active", Suspended: true}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal clear"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if updated.goal.open {
		t.Fatalf("expected suspended active clear to skip confirmation, got %+v", updated.goal)
	}
	if client.clearGoalCalls != 1 {
		t.Fatalf("clear calls = %d, want 1", client.clearGoalCalls)
	}
}

func TestGoalConfirmationEnterUsesSelectedAction(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal clear"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	updated = updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyEnter})
	if updated.goal.open {
		t.Fatal("expected default cancel selection to close overlay")
	}
	if client.clearGoalCalls != 0 {
		t.Fatalf("clear calls after cancel selection = %d, want 0", client.clearGoalCalls)
	}

	m = newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal clear"
	updated = updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	updated = updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyTab})
	updated = updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyEnter})
	if updated.goal.open {
		t.Fatal("expected confirm selection to close overlay")
	}
	if client.clearGoalCalls != 1 {
		t.Fatalf("clear calls after confirm selection = %d, want 1", client.clearGoalCalls)
	}
}

func TestGoalSetWhileBusyCanReplaceActiveGoalWithConfirmation(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "old goal", Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.setBusy(true)
	m.activity = uiActivityRunning
	m.input = "/goal new goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.goal.open || updated.goal.confirmMode != "replace" || updated.goal.pendingObjective != "new goal" {
		t.Fatalf("expected busy replace confirmation overlay, got %+v", updated.goal)
	}
	if client.setGoalArg != "" {
		t.Fatalf("set goal before confirm = %q, want empty", client.setGoalArg)
	}

	updated = updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if updated.goal.open {
		t.Fatal("expected goal overlay closed after confirm")
	}
	if client.setGoalArg != "new goal" {
		t.Fatalf("set goal after confirm = %q, want new goal", client.setGoalArg)
	}
}

func TestGoalConfirmScrollsBodyWhileHorizontalKeysSelect(t *testing.T) {
	objective := strings.Repeat("long objective line that forces the confirmation body to overflow. ", 40)
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: objective, Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 60, 8)
	m.input = "/goal new objective"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if updated.goal.confirmMode != "replace" {
		t.Fatalf("expected replace confirmation, got %+v", updated.goal)
	}
	if updated.goal.confirmSelection != goalConfirmSelectionCancel {
		t.Fatalf("expected default cancel selection, got %d", updated.goal.confirmSelection)
	}

	scrollBeforeDown := updated.goal.scroll
	scrolled := updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyDown})
	if scrolled.goal.scroll == scrollBeforeDown {
		t.Fatalf("expected Down to scroll confirm body, scroll stayed %d", scrolled.goal.scroll)
	}
	if scrolled.goal.confirmSelection != goalConfirmSelectionCancel {
		t.Fatalf("expected Down to leave selection on cancel, got %d", scrolled.goal.confirmSelection)
	}

	scrollBeforeTab := scrolled.goal.scroll
	selected := updateGoalForTest(t, scrolled, tea.KeyMsg{Type: tea.KeyTab})
	if selected.goal.confirmSelection != goalConfirmSelectionConfirm {
		t.Fatalf("expected Tab to move selection to confirm, got %d", selected.goal.confirmSelection)
	}
	if selected.goal.scroll != scrollBeforeTab {
		t.Fatalf("expected Tab to leave scroll unchanged, was %d now %d", scrollBeforeTab, selected.goal.scroll)
	}
}
