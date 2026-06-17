package app

import (
	"strings"

	"core/shared/serverapi"
)

func (m *uiModel) reviewerInvocationState() (bool, string) {
	mode := strings.ToLower(strings.TrimSpace(m.cachedRuntimeStatus().ReviewerFrequency))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(m.reviewerMode))
	}
	if mode == "" {
		mode = "off"
	}
	return mode != "off", mode
}

func (m *uiModel) fastModeState() (bool, bool) {
	status := m.cachedRuntimeStatus()
	if !status.FastModeAvailable && m.fastModeAvailable {
		status.FastModeAvailable = true
	}
	return status.FastModeAvailable, status.FastModeEnabled
}

func fastModeToggleStatusMessage(enabled bool, changed bool) string {
	return serverapi.FastModeToggleStatusMessage(enabled, changed)
}

func reviewerToggleStatusMessage(enabled bool, mode string, changed bool) string {
	return serverapi.ReviewerToggleStatusMessage(enabled, mode, changed)
}

func questionsToggleStatusMessage(enabled bool, changed bool) string {
	return serverapi.QuestionsToggleStatusMessage(enabled, changed)
}

func autoCompactionToggleStatusMessage(enabled bool, changed bool, compactionMode string) string {
	return serverapi.AutoCompactionToggleStatusMessage(enabled, changed, compactionMode)
}
