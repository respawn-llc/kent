package app

import (
	"strconv"
	"strings"
	"time"

	"builder/cli/app/internal/submissionerror"
	"builder/cli/tui"
	"builder/shared/transcript"

	bubblespinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Operator-facing turn-start failures must stay visible in ongoing scrollback.
// Plain "error" remains reserved for detail-only diagnostics and raw failures.
func (m *uiModel) appendOperatorErrorFeedback(text string) tea.Cmd {
	return m.appendLocalEntry(string(transcript.EntryRoleDeveloperErrorFeedback), text)
}

func (c uiInputController) appendLocalEntry(role, text string) tea.Cmd {
	if c.model == nil {
		return nil
	}
	return c.model.appendLocalEntry(role, text)
}

func (c uiInputController) appendLocalEntryWithNoticeID(role, text, noticeID string) tea.Cmd {
	if c.model == nil {
		return nil
	}
	return c.model.appendLocalEntryWithNoticeID(role, text, noticeID)
}

func (c uiInputController) appendSystemFeedback(text string) tea.Cmd {
	return c.appendLocalEntry("system", text)
}

func (c uiInputController) appendErrorFeedback(text string) tea.Cmd {
	return c.appendLocalEntry("error", text)
}

func (c uiInputController) appendLocalEntryWithStatus(role, text string, status tea.Cmd) tea.Cmd {
	return sequenceCmds(c.appendLocalEntry(role, text), status)
}

func (c uiInputController) appendSystemFeedbackWithStatus(text string, status tea.Cmd) tea.Cmd {
	return c.appendLocalEntryWithStatus("system", text, status)
}

func (c uiInputController) appendErrorFeedbackWithStatus(text string, status tea.Cmd) tea.Cmd {
	return c.appendLocalEntryWithStatus("error", text, status)
}

func (c uiInputController) appendSystemFeedbackWithMirroredStatus(text string, kind uiStatusNoticeKind) tea.Cmd {
	noticeID := c.model.nextLocalNoticeID()
	return sequenceCmds(
		c.appendLocalEntryWithNoticeID("system", text, noticeID),
		c.model.setTransientStatusWithKindAndNoticeID(text, kind, noticeID),
	)
}

type uiInputController struct {
	model *uiModel
}

var pendingToolSpinner = bubblespinner.Spinner{
	Frames: []string{"⢎ ", "⠎⠁", "⠊⠑", "⠈⠱", " ⡱", "⢀⡰", "⢄⡠", "⢆⡀"},
	FPS:    80 * time.Millisecond,
}
var spinnerTickInterval = pendingToolSpinner.FPS
var transientStatusDuration = 8 * time.Second
var updateNoticeDuration = 5 * time.Second
var spinnerTickRearmGrace = 3 * time.Second
var scheduleTransientStatusClear = func(duration time.Duration, token uint64) tea.Cmd {
	if duration <= 0 {
		duration = transientStatusDuration
	}
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return clearTransientStatusMsg{token: token}
	})
}
var processListRefreshInterval = 1500 * time.Millisecond
var rollbackDoubleEscWindow = 500 * time.Millisecond
var csiShiftEnterDedupWindow = 120 * time.Millisecond

func (m *uiModel) nextLocalNoticeID() string {
	if m == nil {
		return ""
	}
	m.localNoticeSequence++
	return "local-notice-" + strconv.FormatUint(m.localNoticeSequence, 10)
}

func waitProcessListRefresh() tea.Cmd {
	return tea.Tick(processListRefreshInterval, func(time.Time) tea.Msg {
		return processListRefreshTickMsg{}
	})
}

func tickSpinner(token uint64, delay time.Duration) tea.Cmd {
	if delay <= 0 {
		delay = spinnerTickInterval
	}
	return tea.Tick(delay, func(now time.Time) tea.Msg {
		return spinnerTickMsg{token: token, at: now}
	})
}

func (m *uiModel) shouldAnimateSpinner() bool {
	if m == nil {
		return false
	}
	return m.busy || m.reviewerRunning || m.processListHasRunningEntries() || m.worktrees.loading || m.worktrees.create.submitting || m.worktrees.deleteConfirm.submitting
}

func (m *uiModel) ensureSpinnerTicking() tea.Cmd {
	return m.reconcileSpinnerTicking(false)
}

func (m *uiModel) rearmSpinnerTicking() tea.Cmd {
	return m.reconcileSpinnerTicking(true)
}

func (m *uiModel) reconcileSpinnerTicking(force bool) tea.Cmd {
	if m == nil {
		return nil
	}
	if !m.shouldAnimateSpinner() {
		m.stopSpinnerTicking()
		return nil
	}
	now := uiAnimationNow()
	if m.spinnerTickToken != 0 && m.spinnerClock.Running() && !m.spinnerTickDue.IsZero() {
		rearmAfter := m.spinnerTickDue.Add(spinnerTickRearmGrace)
		if force {
			rearmAfter = m.spinnerTickDue
		}
		if !now.After(rearmAfter) {
			return nil
		}
	}
	if !m.spinnerClock.Running() {
		m.spinnerClock.Start(now)
		m.spinnerFrame = 0
	} else {
		frameCount := len(pendingToolSpinner.Frames)
		if frameCount <= 0 {
			frameCount = 1
		}
		m.spinnerFrame = m.spinnerClock.Frame(now, frameCount, spinnerTickInterval)
	}
	m.spinnerGeneration++
	m.spinnerTickToken = m.spinnerGeneration
	if m.spinnerTickToken == 0 {
		m.spinnerGeneration++
		m.spinnerTickToken = m.spinnerGeneration
	}
	return m.scheduleSpinnerTick(m.spinnerTickToken, now)
}

func (m *uiModel) stopSpinnerTicking() {
	if m == nil {
		return
	}
	m.spinnerTickToken = 0
	m.spinnerTickDue = time.Time{}
	m.spinnerClock.Stop()
}

func (m *uiModel) scheduleSpinnerTick(token uint64, now time.Time) tea.Cmd {
	if m == nil || token == 0 {
		return nil
	}
	if now.IsZero() {
		now = uiAnimationNow()
	}
	delay := m.spinnerClock.NextDelay(now, spinnerTickInterval)
	m.spinnerTickDue = now.Add(delay)
	return tickSpinner(token, delay)
}

func formatSubmissionError(err error) string {
	return submissionerror.Format(err)
}

func isInterruptedRuntimeError(err error) bool {
	return submissionerror.IsInterrupted(err)
}

func (c uiInputController) interruptBusyRuntime() {
	m := c.model
	_ = m.interruptRuntime()
	m.pendingInterrupt = true
}

func parseUserShellCommand(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "$") {
		return "", false
	}
	command := strings.TrimSpace(strings.TrimPrefix(trimmed, "$"))
	if command == "" {
		return "", false
	}
	return command, true
}

func (m *uiModel) appendLocalEntry(role, text string) tea.Cmd {
	return m.appendLocalEntryWithNoticeID(role, text, "")
}

func (m *uiModel) appendLocalEntryWithNoticeID(role, text, noticeID string) tea.Cmd {
	role = strings.TrimSpace(role)
	text = strings.TrimSpace(text)
	noticeID = strings.TrimSpace(noticeID)
	if role == "" || text == "" {
		return nil
	}
	if m.hasRuntimeClient() {
		if err := m.appendRuntimeLocalEntryWithNoticeID(role, text, noticeID); err == nil {
			return nil
		}
	}
	return m.appendLocalEntryFallbackWithNoticeID(role, text, noticeID)
}

func (m *uiModel) appendLocalEntryFallback(role, text string) tea.Cmd {
	return m.appendLocalEntryFallbackWithVisibility(role, text, transcript.EntryVisibilityAuto)
}

func (m *uiModel) appendLocalEntryFallbackWithVisibility(role, text string, visibility transcript.EntryVisibility) tea.Cmd {
	return m.appendLocalEntryFallbackWithNoticeIDAndVisibility(role, text, "", visibility)
}

func (m *uiModel) appendLocalEntryFallbackWithNoticeID(role, text, noticeID string) tea.Cmd {
	return m.appendLocalEntryFallbackWithNoticeIDAndVisibility(role, text, noticeID, transcript.EntryVisibilityAuto)
}

func (m *uiModel) appendLocalEntryFallbackWithNoticeIDAndVisibility(role, text, noticeID string, visibility transcript.EntryVisibility) tea.Cmd {
	if m == nil {
		return nil
	}
	transcriptRole := tui.TranscriptRoleFromWire(role)
	entry := tui.TranscriptEntry{Visibility: transcript.NormalizeEntryVisibility(visibility), Role: transcriptRole, Text: text, NoticeID: strings.TrimSpace(noticeID)}
	m.transcriptEntries = append(m.transcriptEntries, entry)
	m.transcriptTotalEntries = max(m.transcriptTotalEntries, m.transcriptBaseOffset+len(committedTranscriptEntriesForApp(m.transcriptEntries)))
	m.refreshRollbackCandidates()
	m.forwardToView(tui.AppendTranscriptMsg{Visibility: entry.Visibility, Role: transcriptRole, Text: text, NoticeID: entry.NoticeID})
	return m.syncNativeHistoryFromTranscript()
}
