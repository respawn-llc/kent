package app

import (
	"strings"

	"core/cli/app/internal/nativescrollback"
	"core/cli/tui"

	xansi "github.com/charmbracelet/x/ansi"
)

func setNativeCurrentProjectionForTest(m *uiModel, projection tui.TranscriptProjection, baseOffset int, committedCount int) {
	if m == nil {
		return
	}
	m.nativeScrollbackLedger.SetCurrentProjection(projection, baseOffset, committedCount)
}

func setNativeRenderedProjectionForTest(m *uiModel, projection tui.TranscriptProjection, baseOffset int) {
	if m == nil {
		return
	}
	m.nativeScrollbackLedger.ScheduleRenderedProjectionCommit(projection, baseOffset, false)
	m.applyNativeRenderedProjectionCommitIfReady()
}

func setNativeCurrentAndRenderedProjectionForTest(m *uiModel, projection tui.TranscriptProjection, baseOffset int, committedCount int) {
	setNativeCurrentProjectionForTest(m, projection, baseOffset, committedCount)
	setNativeRenderedProjectionForTest(m, projection, baseOffset)
}

func setCommittedDeliveryForTest(m *uiModel, committedCount int, revision int64) {
	if m == nil {
		return
	}
	m.nativeScrollbackLedger.ResetCommittedDelivery(committedCount, revision)
}

func committedDeliveryStateForTest(m *uiModel) nativescrollback.CommittedDeliveryState {
	if m == nil {
		return nativescrollback.CommittedDeliveryState{}
	}
	return m.nativeScrollbackLedger.CommittedDeliveryState()
}

func seedNativeAssistantStreamForTest(m *uiModel, text string) {
	if m == nil {
		return
	}
	m.nativeScrollbackLedger.ApplyAssistantStreamSource(nativescrollback.AssistantStreamInput{
		Source: text,
		Theme:  m.theme,
		Width:  m.nativeReplayRenderWidth(),
	})
}

func joinedPlainProjectionLines(lines []tui.TranscriptProjectionLine) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, xansi.Strip(line.Text))
	}
	return strings.Join(parts, "\n")
}
