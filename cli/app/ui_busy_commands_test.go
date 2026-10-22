package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"core/cli/app/commands"
	"core/shared/runtimeinput"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestDefaultRegistryBusyContract(t *testing.T) {
	registry := commands.NewDefaultRegistry()
	want := map[string]commands.ActiveRunPolicy{
		"exit": commands.ActiveRunPolicyAllowed, "login": commands.ActiveRunPolicyAllowed,
		"new": commands.ActiveRunPolicyAllowed, "resume": commands.ActiveRunPolicyAllowed,
		"logout": commands.ActiveRunPolicyAllowed, "compact": commands.ActiveRunPolicyAllowed,
		"name": commands.ActiveRunPolicyAllowed, "thinking": commands.ActiveRunPolicyAllowed,
		"fast": commands.ActiveRunPolicyAllowed, "supervisor": commands.ActiveRunPolicyAllowed,
		"autocompaction": commands.ActiveRunPolicyAllowed, "questions": commands.ActiveRunPolicyAllowed,
		"status": commands.ActiveRunPolicyAllowed, "goal": commands.ActiveRunPolicyAllowed,
		"ps": commands.ActiveRunPolicyAllowed, "worktree": commands.ActiveRunPolicyAllowed,
		"copy": commands.ActiveRunPolicyAllowed, "back": commands.ActiveRunPolicyAllowed,
		"review": commands.ActiveRunPolicyAllowed, "init": commands.ActiveRunPolicyAllowed,
	}
	for _, command := range registry.Commands() {
		policy, ok := want[command.Name]
		if !ok || command.ActiveRunPolicy != policy {
			t.Fatalf("command %q policy = %v, want %v", command.Name, command.ActiveRunPolicy, policy)
		}
		delete(want, command.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing built-in commands: %+v", want)
	}
}

func TestBusyEnterOpensReadOverlays(t *testing.T) {
	for input, mode := range map[string]uiInputMode{
		"/status": uiInputModeStatus,
		"/goal":   uiInputModeGoal,
		"/ps":     uiInputModeProcessList,
	} {
		t.Run(input, func(t *testing.T) {
			model := busyCommandTestModel()
			model.sessionID = "busy-navigation-session"
			testSetMainInput(model, input)
			next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated := next.(*uiModel)
			if updated.inputMode() != mode || !updated.isBusy() {
				t.Fatalf("input mode = %v, busy = %t; want %v/true", updated.inputMode(), updated.isBusy(), mode)
			}
			requireBusyCommandQueuesEmpty(t, updated)
		})
	}
}

func TestBusyEnterDispatchesWorktreeTransitions(t *testing.T) {
	for _, input := range []string{"/wt switch feature", "/wt leave"} {
		t.Run(input, func(t *testing.T) {
			client := &worktreeCommandTestClient{}
			model := busyCommandTestModel()
			model.worktreeClient = client
			testSetMainInput(model, input)
			next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated := next.(*uiModel)
			if cmd == nil || testMainInput(updated) != "" || !updated.worktrees.switchPending {
				t.Fatalf("transition result = cmd %v input %q pending %t", cmd, testMainInput(updated), updated.worktrees.switchPending)
			}
			_ = cmd()
			if got := len(client.enterRequests) + len(client.leaveRequests); got != 1 {
				t.Fatalf("transition requests = enter %d leave %d, want one", len(client.enterRequests), len(client.leaveRequests))
			}
		})
	}
}

func TestBusyEnterOpensBareWorktreePickerAliases(t *testing.T) {
	for _, input := range []string{"/worktree", "/wt"} {
		t.Run(input, func(t *testing.T) {
			client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
			model := busyCommandTestModel()
			model.worktreeClient = client
			testSetMainInput(model, input)

			next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated := next.(*uiModel)
			if cmd == nil || updated.inputMode() != uiInputModeWorktree {
				t.Fatalf("bare picker result = cmd %v mode %v, want worktree overlay", cmd, updated.inputMode())
			}
			if len(client.enterRequests) != 0 || len(client.deleteRequests) != 0 || len(client.createRequests) != 0 {
				t.Fatalf(
					"bare picker performed a worktree mutation: enter=%+v delete=%+v create=%+v",
					client.enterRequests,
					client.deleteRequests,
					client.createRequests,
				)
			}
		})
	}
}

func TestBusyEnterRoutesDirectWorktreeCommands(t *testing.T) {
	for _, input := range []string{"/wt status", "/wt create", "/wt delete feature"} {
		t.Run(input, func(t *testing.T) {
			client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
			model := busyCommandTestModel()
			model.worktreeClient = client
			testSetMainInput(model, input)
			_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			for range collectCmdMessages(t, cmd) {
			}
			if len(client.listRequests) != 1 {
				t.Fatalf("worktree list requests = %d, want direct owner call", len(client.listRequests))
			}
		})
	}
}

func TestBusyCatalogPromptCommandRemainsTypedRuntimeSubmission(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedTestUIModel(client,
		WithUIConversationFreshness(runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED),
		WithUIPromptCommandCatalogEntries([]commands.PromptCommandCatalogEntry{{Name: "prompt:inspect", Preview: "Inspect"}}))
	model.commandRegistry = commands.NewDefaultRegistryWithPromptCatalog(model.promptCatalogEntries)
	model.setRuntimeActivityBusyForTest(true)
	model.activity = uiActivityRunning
	testSetMainInput(model, "/prompt:inspect cli/app")
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(*uiModel)
	for _, msg := range collectCmdMessages(t, cmd) {
		model = updateUIModel(t, model, msg)
	}
	if client.submitCalls != 1 || model.exitAction != UIActionNone ||
		client.submitInput.Kind != runtimeinput.KindPromptCommand ||
		client.submitInput.PromptCommand == nil ||
		client.submitInput.PromptCommand.Name != "prompt:inspect" {
		t.Fatalf("busy prompt result = calls %d action %q input %+v", client.submitCalls, model.exitAction, client.submitInput)
	}
}

func TestBusyEnterDispatchesCompact(t *testing.T) {
	model := busyCommandTestModel()
	testSetMainInput(model, "/compact now")
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*uiModel)
	if cmd == nil || testMainInput(updated) != "" || updated.isCompacting() {
		t.Fatalf("compact dispatch = cmd %v, input %q, compacting %t", cmd, testMainInput(updated), updated.isCompacting())
	}
	testSetMainInput(updated, "/compact again")
	next, repeatCmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if repeatCmd == nil || updated.isCompacting() || len(updated.injectedQueue) != 0 {
		t.Fatalf("repeat compact = cmd %v compacting %t injected %+v", repeatCmd, updated.isCompacting(), updated.injectedQueue)
	}
}

func TestBusyNavigationCommandsStartTheirExistingTransitions(t *testing.T) {
	for input, action := range map[string]UIAction{
		"/exit": UIActionExit, "/new": UIActionNewSession, "/resume": UIActionResume,
	} {
		t.Run(input, func(t *testing.T) {
			model := busyCommandTestModel()
			testSetMainInput(model, input)
			next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated := next.(*uiModel)
			if cmd == nil || updated.exitAction != action {
				t.Fatalf("transition = cmd %v, action %q; want action %q", cmd, updated.exitAction, action)
			}
		})
	}
}

func TestCompactionDispatchKeepsInputEditableWithoutLocalRuntimeBlocking(t *testing.T) {
	client := &runtimeControlFakeClient{}
	model := newProjectedTestUIModel(client)
	model.startupCmds = nil

	if cmd := model.inputController().startCompaction("  /compact   tighten  summary  ", "tighten  summary"); cmd == nil {
		t.Fatal("expected compaction command")
	}
	_ = model.inputController().compactCmd(
		"  /compact   tighten  summary  ",
		optionalCompactionGuidance("tighten  summary"),
	)()
	if client.compactRequest.Admission.Guidance == nil ||
		*client.compactRequest.Admission.Guidance != "tighten summary" {
		t.Fatalf("compaction admission = %+v", client.compactRequest.Admission)
	}
	buttonClient := &runtimeControlFakeClient{}
	_ = newProjectedTestUIModel(buttonClient).inputController().compactCmd("", nil)()
	if buttonClient.compactRequest.Admission.Guidance != nil {
		t.Fatalf("button compaction guidance = %v, want absent", buttonClient.compactRequest.Admission.Guidance)
	}
	if model.isCompacting() || model.blocksRuntimeInput() || model.layout().mainInputPrefix() != "› " {
		t.Fatalf("compaction state = compacting %t, blocked %t, prefix %q", model.isCompacting(), model.blocksRuntimeInput(), model.layout().mainInputPrefix())
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("steer during compaction")})
	updated := next.(*uiModel)
	next, queueCmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if queueCmd == nil || testMainInput(updated) != "" || len(updated.injectedQueue) != 1 ||
		updated.injectedQueue[0].Text != "steer during compaction" || updated.isCompacting() {
		t.Fatalf(
			"submission = cmd %v, input %q, injected %+v, compacting %t",
			queueCmd,
			testMainInput(updated),
			updated.injectedQueue,
			updated.isCompacting(),
		)
	}
}

func TestRejectedCompactionAtPendingWorkCapacityRestoresVerbatimInput(t *testing.T) {
	client := &runtimeControlFakeClient{
		err: serverapi.NewRuntimeCommandNotAcceptedError(&serverapi.PendingWorkCapacityError{}),
	}
	model := newProjectedTestUIModel(client)
	submittedText := "  /compact   preserve guidance  "

	cmd := model.inputController().startCompaction(submittedText, "preserve guidance")
	for _, msg := range collectCmdMessages(t, cmd) {
		model = updateUIModel(t, model, msg)
	}

	if got := testMainInput(model); got != submittedText {
		t.Fatalf("restored composer = %q, want verbatim %q", got, submittedText)
	}
}

func busyCommandTestModel() *uiModel {
	model := newProjectedStaticUIModel(WithUIConversationFreshness(runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED))
	model.sessionID = "11111111-1111-4111-8111-111111111111"
	model.setRuntimeActivityBusyForTest(true)
	model.activity = uiActivityRunning
	return model
}

func requireBusyCommandQueuesEmpty(t *testing.T, model *uiModel) {
	t.Helper()
	if len(model.injectedQueue) != 0 {
		t.Fatalf("injected = %+v", model.injectedQueue)
	}
}
