package app

import (
	"core/cli/app/commands"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func refreshSlashCommandFilterForTest(t *testing.T, m *uiModel) {
	t.Helper()
	cmd := m.refreshSlashCommandFilterFromInput()
	for _, msg := range collectCmdMessages(t, cmd) {
		next, _ := m.Update(msg)
		m = next.(*uiModel)
	}
}

func TestSlashCommandEnterIgnoresWhitespaceImmediatelyAfterSlash(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.sessionName = "existing"
	testSetMainInput(m, "/ name")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected /name command to update the window title")
	}
	if updated.sessionName != "" {
		t.Fatalf("expected / name to behave like /name with empty args, got %q", updated.sessionName)
	}
	if testMainInput(updated) != "" {
		t.Fatalf("expected input cleared after slash command execution, got %q", testMainInput(updated))
	}
}

func TestSlashCommandPickerHighlightTracksSelectionAfterViewportScroll(t *testing.T) {
	withTrueColor(t)
	m := newSlashPickerScrollTestModel()

	targetIndex := slashPickerCommandIndex(m.slashCommandPicker(), "goal")
	if targetIndex <= slashCommandPickerLines/2 {
		t.Fatalf("test setup expected /goal past centered viewport threshold, got index %d", targetIndex)
	}
	for step := 0; step < targetIndex; step++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(*uiModel)
	}

	state := m.slashCommandPicker()
	if state.start == 0 {
		t.Fatalf("expected slash picker viewport to scroll for /goal, got %+v", state)
	}
	if testMainInput(m) != "/goal" {
		t.Fatalf("expected logical slash selection to update input to /goal, got %q", testMainInput(m))
	}
	assertActivePickerHighlightedSelection(t, m)
}

func newSlashPickerScrollTestModel() *uiModel {
	r := commands.NewRegistry()
	registerSlashPickerTestCommand := func(name string) {
		r.RegisterWithOptions(name, "test command "+name, commands.RegisterOptions{PreservePromptHistoryDraft: true}, func(string) commands.Result {
			return commands.Result{Handled: true}
		})
	}
	for _, name := range []string{"aa00", "aa01", "aa02", "aa03", "aa04", "aa05", "aa06", "aa07", "aa08", "goal"} {
		registerSlashPickerTestCommand(name)
	}
	m := newProjectedStaticUIModel(WithUICommandRegistry(r))
	testSetMainInput(m, "/")
	m.refreshSlashCommandFilterFromInput()
	return m
}

func slashPickerCommandIndex(state slashCommandPickerState, name string) int {
	for idx, command := range state.matches {
		if command.Name == name {
			return idx
		}
	}
	return -1
}

func assertActivePickerHighlightedSelection(t *testing.T, m *uiModel) {
	t.Helper()
	state := m.activePickerPresentation()
	if !state.visible || len(state.rows) == 0 {
		t.Fatalf("expected visible picker with rows, got %+v", state)
	}
	expectedRow := state.selection - state.start
	if expectedRow < 0 || expectedRow >= state.lineCount {
		t.Fatalf("expected visible selected row, got state %+v", state)
	}
	if state.selection < 0 || state.selection >= len(state.rows) {
		t.Fatalf("selected row index out of range for state %+v", state)
	}
}

func TestSlashCommandPickerShowsResumeWhenCurrentSessionIsOnlyKnownSession(t *testing.T) {
	m := newProjectedStaticUIModel()
	testSetMainInput(m, "/re")
	refreshSlashCommandFilterForTest(t, m)

	state := m.slashCommandPicker()
	if !slashPickerContainsCommand(state, "resume") {
		t.Fatalf("expected /resume without another known session, got %+v", slashPickerCommandNames(state))
	}
}

func TestSlashCommandPickerProjectsAuthCommand(t *testing.T) {
	cases := []struct {
		name    string
		method  authpb.AuthMethod
		visible string
		hidden  string
	}{
		{name: "no auth", method: authpb.AuthMethod_AUTH_METHOD_NONE, visible: "login", hidden: "logout"},
		{name: "api key", method: authpb.AuthMethod_AUTH_METHOD_API_KEY, visible: "login", hidden: "logout"},
		{name: "oauth", method: authpb.AuthMethod_AUTH_METHOD_OAUTH, visible: "logout", hidden: "login"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &staticAuthStatusClient{response: authStatusResponse(tc.method)}
			m := newProjectedStaticUIModel(WithUIStatusConfig(uiStatusConfig{
				AuthStatus: client,
			}))
			testSetMainInput(m, "/")
			refreshSlashCommandFilterForTest(t, m)

			state := m.slashCommandPicker()
			if !slashPickerContainsCommand(state, tc.visible) {
				t.Fatalf("expected /%s in slash picker, got %+v", tc.visible, slashPickerCommandNames(state))
			}
			if !slashPickerContainsCommand(state, tc.hidden) || slashPickerContainsCommand(state, "fast") {
				t.Fatalf("unexpected gated command in slash picker: %+v", slashPickerCommandNames(state))
			}
		})
	}
}

func TestBothAuthSlashCommandsExecute(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "login", input: "/login"},
		{name: "logout", input: "/logout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newProjectedStaticUIModel()
			testSetMainInput(m, tc.input)

			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated := next.(*uiModel)
			if cmd == nil {
				t.Fatalf("expected %s to execute", tc.input)
			}
			if updated.exitAction != UIActionLogout {
				t.Fatalf("expected %s to execute logout/login transition, got %q", tc.input, updated.exitAction)
			}
		})
	}
}

func TestSlashCommandPickerKeepsAuthCommandsWhenAuthStateCannotLoad(t *testing.T) {
	m := newProjectedStaticUIModel(WithUIStatusConfig(uiStatusConfig{
		AuthStatus: &staticAuthStatusClient{err: errors.New("permission denied")},
	}))
	testSetMainInput(m, "/")
	refreshSlashCommandFilterForTest(t, m)

	state := m.slashCommandPicker()
	if !slashPickerContainsCommand(state, "login") || !slashPickerContainsCommand(state, "logout") {
		t.Fatalf("connection management must remain available: %+v", slashPickerCommandNames(state))
	}
}

func TestSlashCommandPickerAlwaysShowsCopyWithoutReadingCachedRuntimeStatus(t *testing.T) {
	client := &runtimeControlFakeClient{}
	m := newProjectedTestUIModel(client)
	testSetMainInput(m, "/co")
	m.refreshSlashCommandFilterFromInput()

	state := m.slashCommandPicker()
	if !slashPickerContainsCommand(state, "copy") {
		t.Fatalf("expected /copy without runtime status, got %+v", slashPickerCommandNames(state))
	}
	if client.refreshMainViewCalls != 0 {
		t.Fatalf("slash picker refreshed runtime status %d times, want 0", client.refreshMainViewCalls)
	}
}
