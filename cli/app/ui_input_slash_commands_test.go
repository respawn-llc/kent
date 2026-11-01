package app

import (
	"testing"

	"core/cli/app/commands"
	"core/shared/runtimeinput"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPromptCommandTokenReservesNamespaceCaseInsensitively(t *testing.T) {
	for _, input := range []string{"/prompt:missing", "/PROMPT:missing", "/prompt:"} {
		if _, ok := promptCommandToken(input); !ok {
			t.Fatalf("prompt command token %q was not reserved", input)
		}
	}
}

func TestUnknownPromptCommandFailsClosedBeforeOrdinarySubmission(t *testing.T) {
	model := newProjectedStaticUIModel()
	handled, _, cmd := model.inputController().handleEnteredSlashCommandInput("/prompt:doesnotexist", "/prompt:doesnotexist")
	if !handled {
		t.Fatal("unknown prompt command was not handled locally")
	}
	if cmd == nil {
		t.Fatal("unknown prompt command did not produce local feedback")
	}
}

func TestUnknownPromptCommandFailsClosedThroughEnterPath(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "/prompt:doesnotexist")

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("unknown prompt command did not produce local feedback")
	}
	updated := next.(*uiModel)
	if len(updated.injectedQueue) != 0 {
		t.Fatalf("unknown prompt command entered submission path: %+v", updated.injectedQueue)
	}
}

func TestFreshBuiltinPromptTransitionBlocksWhenDisconnected(t *testing.T) {
	model := newProjectedStaticUIModel()
	model.setRuntimeDisconnected(true)
	invocation := runtimeinput.NewBuiltinPromptCommand(runtimeinput.BuiltinPromptCommandReview, "")

	next, cmd := model.inputController().applyCommandResult(
		commands.Result{PromptCommand: &invocation, FreshConversation: true},
		"/review",
	)
	if cmd == nil {
		t.Fatal("disconnected fresh prompt transition did not produce a blocking command")
	}
	updated := next.(*uiModel)
	if updated.exitAction == UIActionNewSession {
		t.Fatal("disconnected fresh prompt transition incorrectly started a new session")
	}
}
