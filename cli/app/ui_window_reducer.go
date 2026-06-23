package app

import (
	"time"

	"core/cli/tui"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

type uiWindowFeatureReducer struct {
	model *uiModel
}

func (m *uiModel) windowReducer() uiWindowFeatureReducer {
	return uiWindowFeatureReducer{model: m}
}

func (r uiWindowFeatureReducer) Update(msg tea.Msg) uiFeatureUpdateResult {
	m := r.model
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		previousWidth := m.termWidth
		previousHeight := m.termHeight
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		m.windowSizeKnown = true
		m.layout().syncViewport()
		if previousWidth > 0 && previousWidth != msg.Width {
			m.nativeStreamingController.Configure(m.nativeStreamingController.theme, msg.Width)
			m.nativeStreamingWidth = msg.Width
			if m.nativeStreamingController.invalidatedByResize {
				m.nativeStreamingTail = cloneNativeStreamProjectionLines(m.nativeStreamingController.rendered)
			} else {
				m.nativeStreamingTail = cloneNativeStreamProjectionLines(m.nativeStreamingController.rendered[m.nativeStreamingController.enqueuedStableLineCount:])
			}
		}
		if m.nativeHistoryReplayed && previousWidth > 0 && previousWidth != msg.Width {
			committedEntries := committedTranscriptEntriesForApp(m.transcriptEntries)
			if len(committedEntries) == 0 {
				m.resetNativeHistoryState()
				m.nativeHistoryReplayed = true
			} else {
				m.rebaseNativeProjection(m.nativeCommittedProjection(committedEntries), m.transcriptBaseOffset, len(committedEntries))
			}
		}
		if !m.nativeHistoryReplayed {
			return handledUIFeatureUpdate(m, m.syncNativeHistoryFromTranscript())
		}
		if previousWidth > 0 && previousHeight > 0 && previousWidth != msg.Width && m.view.Mode() == tui.ModeOngoing {
			// Only width changes need a native replay: horizontal resize changes the
			// committed scrollback wrapping, while height-only resize affects only the
			// live viewport. After the width has been quiet for the debounce window,
			// clear and replay ongoing history so emitted lines and dividers match.
			m.nativeResizeReplayToken++
			m.nativeResizeReplayAt = nativeResizeReplayNow().Add(nativeResizeReplayDebounce)
			token := m.nativeResizeReplayToken
			return handledUIFeatureUpdate(m, tea.Tick(nativeResizeReplayDebounce, func(time.Time) tea.Msg {
				return nativeResizeReplayMsg{token: token}
			}))
		}
		return handledUIFeatureUpdate(m, nil)
	case nativeResizeReplayMsg:
		if msg.token != m.nativeResizeReplayToken || m.view.Mode() != tui.ModeOngoing {
			return handledUIFeatureUpdate(m, nil)
		}
		if !m.nativeResizeReplayAt.IsZero() {
			remaining := time.Until(m.nativeResizeReplayAt)
			if now := nativeResizeReplayNow(); !now.IsZero() {
				remaining = m.nativeResizeReplayAt.Sub(now)
			}
			if remaining > 0 {
				token := m.nativeResizeReplayToken
				return handledUIFeatureUpdate(m, tea.Tick(remaining, func(time.Time) tea.Msg {
					return nativeResizeReplayMsg{token: token}
				}))
			}
		}
		m.nativeResizeReplayAt = time.Time{}
		if refresh := m.requestNativeResizeCommittedTranscriptSuffix(msg.token); refresh != nil {
			return handledUIFeatureUpdate(m, refresh)
		}
		if replay := m.emitCurrentNativeScrollbackState(true); replay != nil {
			return handledUIFeatureUpdate(m, replay)
		}
		if !m.nativeRenderedProjection.Empty() {
			return handledUIFeatureUpdate(m, nil)
		}
		return handledUIFeatureUpdate(m, tea.ClearScreen)
	case nativeResizeTranscriptSuffixRefreshedMsg:
		if msg.token != m.nativeResizeReplayToken || m.view.Mode() != tui.ModeOngoing {
			return handledUIFeatureUpdate(m, nil)
		}
		if msg.err != nil {
			m.observeRuntimeRequestResult(msg.err)
			if replay := m.emitCurrentNativeScrollbackState(true); replay != nil {
				return handledUIFeatureUpdate(m, replay)
			}
			return handledUIFeatureUpdate(m, tea.ClearScreen)
		}
		m.observeRuntimeRequestResult(nil)
		cmd := m.applyCommittedTranscriptSuffixForNativeReplay(msg.suffix)
		return handledUIFeatureUpdate(m, cmd)
	}
	return uiFeatureUpdateResult{}
}

func (m *uiModel) requestNativeResizeCommittedTranscriptSuffix(token uint64) tea.Cmd {
	if m == nil || !m.hasRuntimeClient() {
		return nil
	}
	runtimeClient := m.runtimeClient()
	client, ok := runtimeClient.(interface {
		RefreshCommittedTranscriptSuffix(clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error)
	})
	if !ok {
		return nil
	}
	return func() tea.Msg {
		suffix, err := client.RefreshCommittedTranscriptSuffix(clientui.CommittedTranscriptSuffixRequest{})
		return nativeResizeTranscriptSuffixRefreshedMsg{token: token, suffix: suffix, err: err}
	}
}

func (m *uiModel) applyCommittedTranscriptSuffixForNativeReplay(suffix clientui.CommittedTranscriptSuffix) tea.Cmd {
	if m == nil {
		return nil
	}
	page := transcriptPageFromCommittedTranscriptSuffix(suffix)
	entries := transcriptEntriesFromPage(page)
	m.runtimeAdapter().applyAuthoritativeRecentTailPage(page, entries, false)
	m.detailTranscript.syncTail(page)
	m.forwardToView(tui.SetConversationMsg{
		BaseOffset:   page.Offset,
		TotalEntries: page.TotalEntries,
		Entries:      entries,
		Ongoing:      page.Streaming,
		OngoingError: page.StreamingError,
	})
	committedEntries := committedTranscriptEntriesForApp(m.transcriptEntries)
	if len(committedEntries) == 0 {
		m.resetNativeHistoryState()
		m.nativeHistoryReplayed = true
		if spacer := m.emitEmptyNativeScrollbackSpacer(true); spacer != nil {
			return spacer
		}
		return tea.ClearScreen
	}
	m.rebaseNativeProjection(m.nativeCommittedProjection(committedEntries), m.transcriptBaseOffset, len(committedEntries))
	if replay := m.emitCurrentNativeScrollbackState(true); replay != nil {
		return replay
	}
	return tea.ClearScreen
}
