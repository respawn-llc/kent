package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"core/cli/app/internal/oauthadapter"

	ansi "github.com/charmbracelet/x/ansi"
)

func TestInteractiveAuthOAuthPresenterRendersBrowserManualFallback(t *testing.T) {
	var out bytes.Buffer
	presenter := interactiveAuthOAuthPresenter{
		interactor: &interactiveAuthInteractor{stderr: &out},
		theme:      "dark",
	}

	presenter.ShowBrowserAuto(oauthadapter.BrowserAuthSession{AuthorizeURL: "https://auth.example/authorize"}, errors.New("blocked"))

	plain := ansi.Strip(out.String())
	for _, want := range []string{
		"Sign in with OpenAI Codex using browser",
		"https://auth.example/authorize",
		"Kent could not open your browser automatically (blocked). Open the URL manually.",
		"Waiting for browser callback...",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected output to contain %q, got %q", want, plain)
		}
	}
}

func TestInteractiveAuthOAuthPresenterRendersDeviceCode(t *testing.T) {
	var out bytes.Buffer
	presenter := interactiveAuthOAuthPresenter{
		interactor: &interactiveAuthInteractor{stderr: &out},
		theme:      "dark",
	}

	presenter.ShowDeviceCode(oauthadapter.DeviceCode{VerificationURL: "https://auth.example/device", UserCode: "ABCD-EFGH"})

	plain := ansi.Strip(out.String())
	for _, want := range []string{
		"Sign in with OpenAI Codex using device code",
		"https://auth.example/device",
		"Code: ABCD-EFGH",
		"Waiting for authorization...",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("expected output to contain %q, got %q", want, plain)
		}
	}
}

func TestInteractiveAuthPromptWritesLabelAndTrimsLineEnding(t *testing.T) {
	var out bytes.Buffer
	interactor := &interactiveAuthInteractor{
		stdin:  strings.NewReader("code-123\r\n"),
		stderr: &out,
	}

	got, err := interactor.prompt("Paste code: ")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got != "code-123" {
		t.Fatalf("prompt value = %q", got)
	}
	if out.String() != "Paste code: " {
		t.Fatalf("prompt label = %q", out.String())
	}
}
