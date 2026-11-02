package app

import (
	"errors"
	"fmt"

	"core/shared/llmerrors"
	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

type chatSettingsRejectedError struct {
	reason settingspb.MutationRejectionReason
}

func (e *chatSettingsRejectedError) Error() string {
	return chatSettingsRejectionNotices[e.reason]
}

func agentSettingsFailureSummary(err error, request uiAgentPickerRequest) string {
	var rejected *chatSettingsRejectedError
	if errors.As(err, &rejected) {
		return rejected.Error()
	}
	var preparation *serverapi.ChatSettingsAgentPreparationError
	if errors.As(err, &preparation) {
		switch preparation.Category {
		case serverapi.ChatSettingsAgentInvalidConfiguration:
			return fmt.Sprintf("Check the configuration for Agent %s before retrying.", preparation.Agent)
		case serverapi.ChatSettingsAgentProviderUnavailable:
			return fmt.Sprintf("Check the provider connection for Agent %s before retrying.", preparation.Agent)
		}
	}
	if message := llmerrors.UserFacingError(err); message != "" {
		return message
	}
	switch request {
	case uiAgentPickerLoad:
		return "Could not load Agents."
	case uiAgentPickerApply:
		return "Could not change Agent."
	default:
		panic("unknown Agent picker request")
	}
}

func (m *uiModel) agentSettingsFailureNotice(err error, request uiAgentPickerRequest) tea.Cmd {
	m.logf("chat_settings.request_failed request=%d err=%q", request, err)
	message := agentSettingsFailureSummary(err, request)
	if m.agentPicker != nil {
		message += " Press r or Enter to retry."
	} else {
		message += " Open /agent to retry."
	}
	return m.showTransientStatusNotice(uiStatusNotice{Text: message, Kind: uiStatusNoticeError})
}
