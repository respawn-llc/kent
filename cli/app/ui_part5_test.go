package app

import (
	"context"
	"core/server/runtime"
	shelltool "core/server/tools/shell"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSlashPickerShowsFastForOpenAIFirstPartyResponsesProvider(t *testing.T) {
	_, eng := newAppRuntimeEngine(t, statusLineFastClient{}, runtime.Config{})
	m := newProjectedEngineUIModel(eng)
	m.input = "/"
	m.refreshSlashCommandFilterFromInputWithAuth(true)

	state := m.slashCommandPicker()
	if !state.visible {
		t.Fatal("expected slash picker visible")
	}
	if !slashPickerContainsCommand(state, "fast") {
		t.Fatalf("expected /fast in slash picker, got %+v", slashPickerCommandNames(state))
	}
}

func TestSlashPickerHidesFastForNonFirstPartyResponsesProvider(t *testing.T) {
	_, eng := newAppRuntimeEngine(t, statusLineAzureClient{}, runtime.Config{})
	m := newProjectedEngineUIModel(eng)
	m.input = "/"
	m.refreshSlashCommandFilterFromInputWithAuth(true)

	state := m.slashCommandPicker()
	if !state.visible {
		t.Fatal("expected slash picker visible")
	}
	if slashPickerContainsCommand(state, "fast") {
		t.Fatalf("did not expect /fast in slash picker, got %+v", slashPickerCommandNames(state))
	}
}

func TestCalcChatLinesShrinksForQueuedPane(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 40
	m.termHeight = 20
	m.input = "ok"

	base := m.layout().calcChatLines()
	m.queued = queuedInputsForTest("a", "b", "c")
	withThree := m.layout().calcChatLines()
	if withThree != base-3 {
		t.Fatalf("expected chat lines to shrink by 3, base=%d withThree=%d", base, withThree)
	}
	m.queued = queuedInputsForTest("1", "2", "3", "4", "5", "6")
	withOverflowLine := m.layout().calcChatLines()
	if withOverflowLine != base-6 {
		t.Fatalf("expected chat lines to shrink by 6 with overflow line, base=%d withOverflowLine=%d", base, withOverflowLine)
	}
}

func TestPSOverlaySpinnerTickAnimatesRunningEntriesWhileIdle(t *testing.T) {
	manager, err := shelltool.NewManager(shelltool.WithMinimumExecToBgTime(20 * time.Millisecond))
	if err != nil {
		t.Fatalf("new background manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	res, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "printf 'spin\n'; sleep 1"},
		DisplayCommand: "spin-job",
		Workdir:        t.TempDir(),
		YieldTime:      fastBackgroundTestYield,
	})
	if err != nil {
		t.Fatalf("start spin job: %v", err)
	}
	if !res.Backgrounded {
		t.Fatal("expected spin job to move to background")
	}

	m := newProjectedStaticUIModel(withUIBackgroundManagerForTest(manager))
	m.termWidth = 100
	m.termHeight = 14
	m.windowSizeKnown = true
	m.input = "/ps"
	m.setBusy(false)
	m.spinnerFrame = 0

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	updated = completeProcessRefreshForTest(t, updated)
	before := updated.View()
	token := updated.spinnerTickToken
	if token == 0 {
		t.Fatal("expected /ps open to start a spinner loop token for running entries")
	}
	next, cmd := updated.Update(spinnerTickMsg{token: token})
	updated = next.(*uiModel)
	after := updated.View()
	if before == after {
		t.Fatalf("expected /ps spinner tick to animate running entries while idle, before=%q after=%q", before, after)
	}
	if cmd == nil {
		t.Fatal("expected spinner tick to schedule another tick while /ps has running entries")
	}
}

func TestPSOverlayIgnoresStaleSpinnerTickTokens(t *testing.T) {
	manager := newFastBackgroundTestManager(t)

	res, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "printf 'spin\n'; sleep 1"},
		DisplayCommand: "spin-job",
		Workdir:        t.TempDir(),
		YieldTime:      fastBackgroundTestYield,
	})
	if err != nil {
		t.Fatalf("start spin job: %v", err)
	}
	if !res.Backgrounded {
		t.Fatal("expected spin job to move to background")
	}

	m := newProjectedStaticUIModel(withUIBackgroundManagerForTest(manager))
	m.termWidth = 100
	m.termHeight = 14
	m.windowSizeKnown = true
	m.input = "/ps"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	updated = completeProcessRefreshForTest(t, updated)
	currentToken := updated.spinnerTickToken
	if currentToken == 0 {
		t.Fatal("expected active spinner token for running /ps entries")
	}
	before := updated.spinnerFrame
	next, cmd := updated.Update(spinnerTickMsg{token: currentToken + 1})
	updated = next.(*uiModel)
	if updated.spinnerFrame != before {
		t.Fatalf("expected stale spinner token not to advance frame, got %d from %d", updated.spinnerFrame, before)
	}
	if cmd != nil {
		t.Fatalf("did not expect stale spinner tick to schedule another timer, got %T", cmd)
	}
}

func TestPSOverlayIgnoresStaleSpinnerTickAfterRestart(t *testing.T) {
	manager := newFastBackgroundTestManager(t)

	res, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "printf 'spin\n'; sleep 1"},
		DisplayCommand: "spin-job",
		Workdir:        t.TempDir(),
		YieldTime:      fastBackgroundTestYield,
	})
	if err != nil {
		t.Fatalf("start spin job: %v", err)
	}
	if !res.Backgrounded {
		t.Fatal("expected spin job to move to background")
	}

	m := newProjectedStaticUIModel(withUIBackgroundManagerForTest(manager))
	m.termWidth = 100
	m.termHeight = 14
	m.windowSizeKnown = true
	m.input = "/ps"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	updated = completeProcessRefreshForTest(t, updated)
	oldToken := updated.spinnerTickToken
	if oldToken == 0 {
		t.Fatal("expected active spinner token for running /ps entries")
	}
	oldFrame := updated.spinnerFrame

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	updated = next.(*uiModel)
	if updated.spinnerTickToken != 0 {
		t.Fatalf("expected spinner token cleared after closing /ps, got %d", updated.spinnerTickToken)
	}

	updated.input = "/ps"
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	updated = completeProcessRefreshForTest(t, updated)
	newToken := updated.spinnerTickToken
	if newToken == 0 {
		t.Fatal("expected restarted spinner token for running /ps entries")
	}
	if newToken == oldToken {
		t.Fatalf("expected restarted spinner token to differ from stale token, got %d", newToken)
	}

	before := updated.spinnerFrame
	next, cmd := updated.Update(spinnerTickMsg{token: oldToken})
	updated = next.(*uiModel)
	if updated.spinnerFrame != before {
		t.Fatalf("expected stale spinner tick after restart not to advance frame, got %d from %d", updated.spinnerFrame, before)
	}
	if updated.spinnerFrame != oldFrame {
		t.Fatalf("expected stale spinner tick to preserve frame %d, got %d", oldFrame, updated.spinnerFrame)
	}
	if cmd != nil {
		t.Fatalf("did not expect stale spinner tick after restart to schedule another timer, got %T", cmd)
	}
}
