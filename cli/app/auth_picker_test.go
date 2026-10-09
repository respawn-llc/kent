package app

import (
	"errors"
	"testing"

	authpb "core/shared/protoapi/gen/kent/api/auth"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAuthMethodPickerSelectsSecondOption(t *testing.T) {
	m, result, err := newAuthMethodPickerModel(authInteraction{Theme: "dark", Modes: []authpb.BootstrapMode{
		authpb.BootstrapMode_BOOTSTRAP_MODE_BROWSER_CALLBACK_URL, authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
	}})
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*onboardingModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*onboardingModel)
	if result.Choice != authMethodChoiceDevice {
		t.Fatalf("choice=%q want %q", result.Choice, authMethodChoiceDevice)
	}
}

func TestStartupPickerEnterDoesNothingWhenThereAreNoItems(t *testing.T) {
	m := newStartupPickerModel("**Header**", "Header", "dark", startupPickerNotice{}, nil)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*startupPickerModel)
	if cmd != nil {
		t.Fatal("did not expect quit command for empty picker")
	}
	if updated.result.ChoiceID != "" || updated.result.Canceled {
		t.Fatalf("expected empty result for empty picker, got %+v", updated.result)
	}
}

func TestAuthMethodPickerCancel(t *testing.T) {
	m, _, err := newAuthMethodPickerModel(authInteraction{Theme: "dark"})
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(*onboardingModel)
	if !m.canceled {
		t.Fatal("expected canceled result")
	}
}

func TestAuthMethodPickerMarksRemoteFlowFailureAsErrorNotice(t *testing.T) {
	notice := authMethodPickerNoticeForRequest(authInteraction{FlowErr: errors.New("remote failure")})
	if notice.Kind != startupPickerNoticeError {
		t.Fatalf("notice kind = %q, want %q", notice.Kind, startupPickerNoticeError)
	}
}
