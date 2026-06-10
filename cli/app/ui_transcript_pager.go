package app

import (
	"builder/cli/tui"
	"builder/shared/clientui"
	"builder/shared/transcript"
	"strings"
)

const (
	uiDetailTranscriptPageSize       = 250
	uiDetailTranscriptMaxEntries     = 1000
	uiDetailTranscriptEdgeLineMargin = 24
)

type uiDetailTranscriptWindow struct {
	sessionID    string
	offset       int
	totalEntries int
	entries      []tui.TranscriptEntry
	ongoing      string
	ongoingError string
	loaded       bool
	lastRequest  clientui.TranscriptPageRequest
}

func (w uiDetailTranscriptWindow) page() clientui.TranscriptPage {
	entries := make([]clientui.ChatEntry, 0, len(w.entries))
	for _, entry := range w.entries {
		entries = append(entries, clientui.ChatEntry{
			Visibility:        entry.Visibility,
			RollbackTargetID:  entry.RollbackTargetID,
			Role:              string(entry.Role),
			Text:              entry.Text,
			OngoingText:       entry.OngoingText,
			Phase:             string(entry.Phase),
			MessageType:       string(entry.MessageType),
			SourcePath:        entry.SourcePath,
			CompactLabel:      entry.CompactLabel,
			ToolResultSummary: entry.ToolResultSummary,
			ToolCallID:        entry.ToolCallID,
			ToolCall:          transcriptToolCallMetaClient(entry.ToolCall),
		})
	}
	return clientui.TranscriptPage{
		SessionID:    w.sessionID,
		TotalEntries: w.totalEntries,
		Offset:       w.offset,
		Entries:      entries,
		Ongoing:      w.ongoing,
		OngoingError: w.ongoingError,
	}
}

func (w *uiDetailTranscriptWindow) reset() {
	if w == nil {
		return
	}
	*w = uiDetailTranscriptWindow{}
}

func (w *uiDetailTranscriptWindow) syncTail(page clientui.TranscriptPage) {
	if w == nil {
		return
	}
	if w.loaded && transcriptPageSessionChanged(w.sessionID, page.SessionID) {
		w.replace(page)
		return
	}
	if !w.loaded {
		w.replace(page)
		return
	}
	end := w.offset + len(w.entries)
	pageEnd := page.Offset + len(page.Entries)
	w.totalEntries = page.TotalEntries
	w.ongoing = page.Ongoing
	w.ongoingError = page.OngoingError
	if page.Offset >= end || pageEnd <= w.offset {
		if pageEnd >= page.TotalEntries {
			w.replace(page)
		}
		return
	}
	w.merge(page)
}

func (w *uiDetailTranscriptWindow) apply(page clientui.TranscriptPage) {
	if w == nil {
		return
	}
	if w.loaded && transcriptPageSessionChanged(w.sessionID, page.SessionID) {
		w.replace(page)
		return
	}
	if !w.loaded {
		w.replace(page)
		return
	}
	w.merge(page)
}

func (w uiDetailTranscriptWindow) matchesPage(page clientui.TranscriptPage) bool {
	if !w.loaded {
		return false
	}
	if transcriptPageSessionChanged(w.sessionID, page.SessionID) {
		return false
	}
	totalEntries := max(page.TotalEntries, page.Offset+len(page.Entries))
	if w.offset != page.Offset || w.totalEntries != totalEntries {
		return false
	}
	if w.ongoing != page.Ongoing || w.ongoingError != page.OngoingError {
		return false
	}
	if len(w.entries) != len(page.Entries) {
		return false
	}
	for i := range page.Entries {
		if !transcript.EntryPayloadEqual(transcriptPayloadFromTUIEntry(w.entries[i]), transcriptPayloadFromClientEntry(page.Entries[i])) {
			return false
		}
	}
	return true
}

func (w *uiDetailTranscriptWindow) replace(page clientui.TranscriptPage) {
	if w == nil {
		return
	}
	w.sessionID = strings.TrimSpace(page.SessionID)
	w.offset = page.Offset
	w.totalEntries = max(page.TotalEntries, page.Offset+len(page.Entries))
	w.entries = transcriptEntriesFromPage(page)
	w.ongoing = page.Ongoing
	w.ongoingError = page.OngoingError
	w.loaded = true
	w.lastRequest = clientui.TranscriptPageRequest{Offset: page.Offset, Limit: len(page.Entries)}
	w.trimAround(page.Offset)
}

func (w *uiDetailTranscriptWindow) merge(page clientui.TranscriptPage) {
	if w == nil {
		return
	}
	if transcriptPageSessionChanged(w.sessionID, page.SessionID) {
		w.replace(page)
		return
	}
	if len(page.Entries) == 0 {
		w.totalEntries = max(w.totalEntries, page.TotalEntries)
		w.ongoing = page.Ongoing
		w.ongoingError = page.OngoingError
		return
	}
	pageEntries := transcriptEntriesFromPage(page)
	currentStart := w.offset
	currentEnd := w.offset + len(w.entries)
	pageStart := page.Offset
	pageEnd := page.Offset + len(pageEntries)
	if pageEnd < currentStart || pageStart > currentEnd {
		w.replace(page)
		return
	}
	mergedStart := min(currentStart, pageStart)
	mergedEnd := max(currentEnd, pageEnd)
	merged := make([]tui.TranscriptEntry, mergedEnd-mergedStart)
	copy(merged[currentStart-mergedStart:], w.entries)
	copy(merged[pageStart-mergedStart:], pageEntries)
	w.offset = mergedStart
	w.entries = merged
	w.totalEntries = max(max(w.totalEntries, page.TotalEntries), mergedEnd)
	w.ongoing = page.Ongoing
	w.ongoingError = page.OngoingError
	w.loaded = true
	w.lastRequest = clientui.TranscriptPageRequest{Offset: page.Offset, Limit: len(page.Entries)}
	w.trimAround(page.Offset)
}

func (w *uiDetailTranscriptWindow) trimAround(anchorOffset int) {
	if w == nil || len(w.entries) <= uiDetailTranscriptMaxEntries {
		return
	}
	if anchorOffset <= w.offset {
		w.entries = append([]tui.TranscriptEntry(nil), w.entries[:uiDetailTranscriptMaxEntries]...)
		return
	}
	anchorLocal := anchorOffset - w.offset
	if anchorLocal < 0 {
		anchorLocal = 0
	}
	if anchorLocal > len(w.entries) {
		anchorLocal = len(w.entries)
	}
	if anchorLocal >= len(w.entries)-uiDetailTranscriptMaxEntries {
		drop := len(w.entries) - uiDetailTranscriptMaxEntries
		w.offset += drop
		w.entries = append([]tui.TranscriptEntry(nil), w.entries[drop:]...)
		return
	}
	half := uiDetailTranscriptMaxEntries / 2
	start := anchorLocal - half
	if start < 0 {
		start = 0
	}
	if start+uiDetailTranscriptMaxEntries > len(w.entries) {
		start = len(w.entries) - uiDetailTranscriptMaxEntries
	}
	w.offset += start
	w.entries = append([]tui.TranscriptEntry(nil), w.entries[start:start+uiDetailTranscriptMaxEntries]...)
}

func (w uiDetailTranscriptWindow) requestedPageForDetailEntry() clientui.TranscriptPageRequest {
	if w.loaded && len(w.entries) > 0 {
		return clientui.TranscriptPageRequest{Offset: w.offset, Limit: len(w.entries)}
	}
	if w.totalEntries <= 0 {
		return clientui.TranscriptPageRequest{Offset: 0, Limit: uiDetailTranscriptPageSize}
	}
	offset := w.totalEntries - uiDetailTranscriptPageSize
	if offset < 0 {
		offset = 0
	}
	return clientui.TranscriptPageRequest{Offset: offset, Limit: uiDetailTranscriptPageSize}
}

func (w uiDetailTranscriptWindow) pageBefore() (clientui.TranscriptPageRequest, bool) {
	if !w.loaded || w.offset <= 0 {
		return clientui.TranscriptPageRequest{}, false
	}
	offset := w.offset - uiDetailTranscriptPageSize
	if offset < 0 {
		offset = 0
	}
	limit := w.offset - offset
	return clientui.TranscriptPageRequest{Offset: offset, Limit: limit}, true
}

func (w uiDetailTranscriptWindow) pageAfter() (clientui.TranscriptPageRequest, bool) {
	if !w.loaded {
		return clientui.TranscriptPageRequest{}, false
	}
	nextOffset := w.offset + len(w.entries)
	if nextOffset >= w.totalEntries {
		return clientui.TranscriptPageRequest{}, false
	}
	limit := uiDetailTranscriptPageSize
	remaining := w.totalEntries - nextOffset
	if remaining < limit {
		limit = remaining
	}
	return clientui.TranscriptPageRequest{Offset: nextOffset, Limit: limit}, true
}

func transcriptPageLooksLikeOngoingTail(page clientui.TranscriptPage) bool {
	if page.TotalEntries <= 0 {
		return true
	}
	if page.Offset+len(page.Entries) != page.TotalEntries {
		return false
	}
	return len(page.Entries) <= uiDetailTranscriptMaxEntries || page.Offset > 0
}

func pageRequestEqual(a, b clientui.TranscriptPageRequest) bool {
	return a.Offset == b.Offset && a.Limit == b.Limit && a.Page == b.Page && a.PageSize == b.PageSize && a.Window == b.Window
}

func transcriptPageSessionChanged(currentSessionID, nextSessionID string) bool {
	trimmedCurrent := strings.TrimSpace(currentSessionID)
	trimmedNext := strings.TrimSpace(nextSessionID)
	if trimmedCurrent == "" || trimmedNext == "" {
		return false
	}
	return trimmedCurrent != trimmedNext
}
