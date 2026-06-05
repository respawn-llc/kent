package app

import (
	"strings"
	"testing"

	"builder/cli/tui"
	"builder/shared/clientui"
	"builder/shared/transcript"

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

func TestGoalCommandOpensGoalOverlay(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyGoalCmdMessagesForTest(t, next.(*uiModel), cmd)
	if !updated.goal.isOpen() {
		t.Fatal("expected /goal to open goal overlay")
	}
	if updated.inputMode() != uiInputModeGoal {
		t.Fatalf("input mode = %q, want goal", updated.inputMode())
	}
	if updated.view.Mode() != tui.ModeOngoing {
		t.Fatalf("view mode = %q, want ongoing", updated.view.Mode())
	}
	if updated.surface() != uiSurfaceGoal {
		t.Fatalf("surface = %q, want goal", updated.surface())
	}
	if cmd == nil {
		t.Fatal("expected /goal to emit overlay transition command")
	}
	plain := stripANSIAndTrimRight(updated.View())
	for _, want := range []string{"Goal", "Status: active", "ship feature"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected goal overlay to contain %q, got %q", want, plain)
		}
	}
}

func TestGoalCommandOpensGoalOverlayWhileBusy(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.setBusy(true)
	m.activity = uiActivityRunning
	m.input = "/goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.goal.isOpen() {
		t.Fatal("expected /goal to open goal overlay while busy")
	}
	if updated.inputMode() != uiInputModeGoal {
		t.Fatalf("input mode = %q, want goal", updated.inputMode())
	}
	if plain := stripANSIAndTrimRight(updated.View()); !strings.Contains(plain, "Status: active") || !strings.Contains(plain, "ship feature") {
		t.Fatalf("expected busy goal overlay content, got %q", plain)
	}
}

func TestGoalSetRendersCommittedGoalFeedbackBeforeLaterRuntimeEvents(t *testing.T) {
	runtimeEvents := make(chan clientui.Event, 2)
	runtimeEvents <- clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		CommittedTranscriptChanged: true,
		TranscriptRevision:         1,
		CommittedEntryCount:        1,
		CommittedEntryStart:        0,
		CommittedEntryStartSet:     true,
		TranscriptEntries: []clientui.ChatEntry{{
			Role:        string(transcript.EntryRoleGoalFeedback),
			Text:        "goal detail",
			OngoingText: `Goal set: "ship feature"`,
			Visibility:  clientui.EntryVisibilityAll,
		}},
	}
	runtimeEvents <- clientui.Event{Kind: clientui.EventAssistantDelta, AssistantDelta: "later model output"}
	client := &runtimeControlFakeClient{}
	m := newSizedProjectedRuntimeEventsUIModel(client, runtimeEvents, 100, 20)
	m.input = "/goal ship feature"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if client.setGoalArg != "ship feature" {
		t.Fatalf("set goal arg = %q, want ship feature", client.setGoalArg)
	}
	raw := waitRuntimeEvent(runtimeEvents)()
	batch, ok := raw.(runtimeEventBatchMsg)
	if !ok {
		t.Fatalf("expected runtime event batch, got %T", raw)
	}
	if len(batch.events) != 1 || batch.events[0].AssistantDelta != "" {
		t.Fatalf("expected first runtime batch to contain only goal feedback, got %+v", batch.events)
	}
	next, _ := updated.Update(batch)
	updated = next.(*uiModel)

	if view := stripANSIAndTrimRight(updated.view.OngoingSnapshot()); !strings.Contains(view, `Goal set: "ship feature"`) {
		t.Fatalf("expected goal feedback in ongoing transcript before later event, got %q", view)
	}
	if view := stripANSIAndTrimRight(updated.view.OngoingSnapshot()); strings.Contains(view, "later model output") {
		t.Fatalf("later runtime event rendered before explicit delivery: %q", view)
	}
}

func TestGoalLifecycleCommandsDoNotAppendDuplicateLocalFeedback(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		initialGoal *clientui.RuntimeGoal
		drive       func(*testing.T, *uiModel, *runtimeControlFakeClient) *uiModel
		check       func(*testing.T, *runtimeControlFakeClient)
	}{
		{
			name:        "set",
			input:       "/goal ship feature",
			initialGoal: nil,
			check: func(t *testing.T, client *runtimeControlFakeClient) {
				t.Helper()
				if client.setGoalArg != "ship feature" {
					t.Fatalf("set goal arg = %q, want ship feature", client.setGoalArg)
				}
			},
		},
		{
			name:        "pause",
			input:       "/goal pause",
			initialGoal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active"},
			check: func(t *testing.T, client *runtimeControlFakeClient) {
				t.Helper()
				if client.pauseGoalCalls != 1 {
					t.Fatalf("pause calls = %d, want 1", client.pauseGoalCalls)
				}
			},
		},
		{
			name:        "resume",
			input:       "/goal resume",
			initialGoal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "paused"},
			check: func(t *testing.T, client *runtimeControlFakeClient) {
				t.Helper()
				if client.resumeGoalCalls != 1 {
					t.Fatalf("resume calls = %d, want 1", client.resumeGoalCalls)
				}
			},
		},
		{
			name:        "clear",
			input:       "/goal clear",
			initialGoal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active"},
			drive: func(t *testing.T, m *uiModel, client *runtimeControlFakeClient) *uiModel {
				t.Helper()
				updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
				return updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			},
			check: func(t *testing.T, client *runtimeControlFakeClient) {
				t.Helper()
				if client.clearGoalCalls != 1 {
					t.Fatalf("clear calls = %d, want 1", client.clearGoalCalls)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &runtimeControlFakeClient{goal: cloneRuntimeGoal(tt.initialGoal)}
			m := newSizedProjectedClosedUIModel(client, 100, 20)
			m.input = tt.input

			var updated *uiModel
			if tt.drive != nil {
				updated = tt.drive(t, m, client)
			} else {
				updated = updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			}
			tt.check(t, client)
			if client.appendedRole != "" || client.appendedText != "" {
				t.Fatalf("did not expect duplicate local goal feedback, got role=%q text=%q", client.appendedRole, client.appendedText)
			}
			status := stripANSIAndTrimRight(updated.renderStatusLine(120, uiThemeStyles("dark")))
			for _, forbidden := range []string{"Goal set", "Goal paused", "Goal resumed", "Goal cleared"} {
				if strings.Contains(status, forbidden) {
					t.Fatalf("did not expect duplicate transient goal status %q, got %q", forbidden, status)
				}
			}
		})
	}
}

func TestGoalCommandWithoutGoalShowsLocalHint(t *testing.T) {
	m := newSizedProjectedClosedUIModel(&runtimeControlFakeClient{}, 100, 20)
	m.input = "/goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, noGoalHint) {
		t.Fatalf("expected no-goal hint %q, got %q", noGoalHint, plain)
	}
}

func TestGoalClearActiveGoalRequiresConfirmation(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal clear"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.goal.isOpen() || updated.goal.confirmMode != "clear" {
		t.Fatalf("expected clear confirmation overlay, got %+v", updated.goal)
	}
	if client.clearGoalCalls != 0 {
		t.Fatalf("clear calls before confirm = %d, want 0", client.clearGoalCalls)
	}
	if plain := stripANSIAndTrimRight(updated.View()); !strings.Contains(plain, "Clear active goal?") || !strings.Contains(plain, "Tab/arrows toggle") {
		t.Fatalf("expected clear confirmation text, got %q", plain)
	}

	updated = updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if updated.goal.isOpen() {
		t.Fatal("expected goal overlay closed after confirm")
	}
	if client.clearGoalCalls != 1 {
		t.Fatalf("clear calls after confirm = %d, want 1", client.clearGoalCalls)
	}
}

func TestGoalClearSuspendedActiveGoalSkipsConfirmation(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "ship feature", Status: "active", Suspended: true}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal clear"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if updated.goal.isOpen() {
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
	if updated.goal.isOpen() {
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
	if updated.goal.isOpen() {
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
	if !updated.goal.isOpen() || updated.goal.confirmMode != "replace" || updated.goal.pendingObjective != "new goal" {
		t.Fatalf("expected busy replace confirmation overlay, got %+v", updated.goal)
	}
	if client.setGoalArg != "" {
		t.Fatalf("set goal before confirm = %q, want empty", client.setGoalArg)
	}

	updated = updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if updated.goal.isOpen() {
		t.Fatal("expected goal overlay closed after confirm")
	}
	if client.setGoalArg != "new goal" {
		t.Fatalf("set goal after confirm = %q, want new goal", client.setGoalArg)
	}
}

func TestGoalOverlayRendersObjectiveMarkdown(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "Ship **bold** goal\n\n- one\n- two", Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	raw := updated.View()
	plain := stripANSIAndTrimRight(raw)
	if strings.Contains(plain, "**bold**") {
		t.Fatalf("expected markdown markers rendered away, got %q", plain)
	}
	if !strings.Contains(plain, "bold") || !strings.Contains(plain, "one") || !strings.Contains(plain, "two") {
		t.Fatalf("expected rendered markdown content, got %q", plain)
	}
	if !strings.Contains(raw, "\x1b[") {
		t.Fatalf("expected markdown renderer ANSI styling, got %q", raw)
	}
}

func TestGoalOverlayWrapsLongMarkdownObjective(t *testing.T) {
	objective := "This **important** goal objective must keep the final tail phrase visible after wrapping."
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: objective, Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 32, 20)
	m.input = "/goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "visible after wrapping") {
		t.Fatalf("expected long markdown objective tail to survive wrapping, got %q", plain)
	}
}

func TestGoalOverlayWrapsLongMarkdownListItem(t *testing.T) {
	objective := "- This **important** list item must keep the final list tail visible after wrapping."
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: objective, Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 34, 20)
	m.input = "/goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	plain := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(plain, "after wrapping") {
		t.Fatalf("expected long markdown list item tail to survive wrapping, got %q", plain)
	}
}

func TestGoalOverlayMarkdownCacheRewrapsAfterWidthChange(t *testing.T) {
	objective := "This **important** goal objective must change wrapping when the overlay width changes."
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: objective, Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 80, 20)
	m.input = "/goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	wide := stripANSIAndTrimRight(updated.View())
	if !strings.Contains(wide, "important goal objective must change wrapping") {
		t.Fatalf("expected wide render to keep phrase on one line, got %q", wide)
	}

	updated.termWidth = 32
	narrow := stripANSIAndTrimRight(updated.View())
	if strings.Contains(narrow, "important goal objective must change wrapping") {
		t.Fatalf("expected narrow render to rewrap cached markdown, got %q", narrow)
	}
	if !strings.Contains(narrow, "overlay width changes") {
		t.Fatalf("expected narrow render to preserve tail after rewrap, got %q", narrow)
	}
}

func TestGoalReplaceActiveGoalRequiresConfirmation(t *testing.T) {
	client := &runtimeControlFakeClient{goal: &clientui.RuntimeGoal{ID: "goal-1", Objective: "old goal", Status: "active"}}
	m := newSizedProjectedClosedUIModel(client, 100, 20)
	m.input = "/goal new goal"

	updated := updateGoalForTest(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.goal.isOpen() || updated.goal.confirmMode != "replace" || updated.goal.pendingObjective != "new goal" {
		t.Fatalf("expected replace confirmation overlay, got %+v", updated.goal)
	}
	if client.setGoalArg != "" {
		t.Fatalf("set goal before confirm = %q, want empty", client.setGoalArg)
	}
	if plain := stripANSIAndTrimRight(updated.View()); !strings.Contains(plain, "Replace active goal?") || !strings.Contains(plain, "New: new goal") {
		t.Fatalf("expected replace confirmation text, got %q", plain)
	}

	updated = updateGoalForTest(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if updated.goal.isOpen() {
		t.Fatal("expected goal overlay closed after confirm")
	}
	if client.setGoalArg != "new goal" {
		t.Fatalf("set goal after confirm = %q, want new goal", client.setGoalArg)
	}
}
