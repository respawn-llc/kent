package runtimeview

import (
	"core/server/runtime"
	"core/shared/clientui"
)

const OngoingTailEntryLimit = 500
const OngoingTailIncrementalOverlapEntries = 32

func TranscriptPageFromRuntime(engine *runtime.Engine, req clientui.TranscriptPageRequest) clientui.TranscriptPage {
	if engine == nil {
		return clientui.TranscriptPage{}
	}
	req = NormalizeDefaultTranscriptRequest(req)
	if req.Window == clientui.TranscriptWindowOngoingTail {
		return TranscriptPageFromOngoingTailWindow(
			engine.SessionID(),
			engine.SessionName(),
			ConversationFreshnessFromSession(engine.ConversationFreshness()),
			engine.TranscriptRevision(),
			engine.OngoingTailTranscriptWindow(OngoingTailEntryLimit),
			req,
		)
	}
	offset, limit := transcriptOffsetAndLimit(req)
	page := engine.TranscriptPageSnapshot(offset, limit)
	return TranscriptPageFromCollectedChat(
		engine.SessionID(),
		engine.SessionName(),
		ConversationFreshnessFromSession(engine.ConversationFreshness()),
		engine.TranscriptRevision(),
		ChatSnapshotFromRuntime(page.Snapshot),
		page.TotalEntries,
		page.Offset,
		clientui.TranscriptPageRequest{Offset: page.Offset, Limit: limit},
	)
}

func CommittedTranscriptSuffixFromRuntime(engine *runtime.Engine, req clientui.CommittedTranscriptSuffixRequest) clientui.CommittedTranscriptSuffix {
	if engine == nil {
		return clientui.CommittedTranscriptSuffix{}
	}
	req = clientui.NormalizeCommittedTranscriptSuffixRequest(req)
	page := engine.TranscriptPageSnapshot(req.AfterEntryCount, req.Limit)
	return CommittedTranscriptSuffixFromCollectedChat(
		engine.SessionID(),
		engine.SessionName(),
		ConversationFreshnessFromSession(engine.ConversationFreshness()),
		engine.TranscriptRevision(),
		ChatSnapshotFromRuntime(page.Snapshot),
		page.TotalEntries,
		page.Offset,
		req,
	)
}

func TranscriptPageFromOngoingTailWindow(sessionID, sessionName string, freshness clientui.ConversationFreshness, revision int64, window runtime.TranscriptWindowSnapshot, req clientui.TranscriptPageRequest) clientui.TranscriptPage {
	req = NormalizeDefaultTranscriptRequest(req)
	pageReq := ongoingTailTranscriptRequest(req, revision, window)
	return TranscriptPageFromCollectedChat(
		sessionID,
		sessionName,
		freshness,
		revision,
		ChatSnapshotFromRuntime(window.Snapshot),
		window.TotalEntries,
		window.Offset,
		pageReq,
	)
}

func NormalizeDefaultTranscriptRequest(req clientui.TranscriptPageRequest) clientui.TranscriptPageRequest {
	if req == (clientui.TranscriptPageRequest{}) {
		return clientui.TranscriptPageRequest{Window: clientui.TranscriptWindowOngoingTail}
	}
	return req
}

func transcriptOffsetAndLimit(req clientui.TranscriptPageRequest) (int, int) {
	if req.PageSize > 0 {
		offset := req.Page * req.PageSize
		if offset < 0 {
			offset = 0
		}
		return offset, req.PageSize
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	limit := req.Limit
	if limit < 0 {
		limit = 0
	}
	return offset, limit
}

func ongoingTailTranscriptRequest(req clientui.TranscriptPageRequest, revision int64, window runtime.TranscriptWindowSnapshot) clientui.TranscriptPageRequest {
	pageReq := clientui.TranscriptPageRequest{Offset: window.Offset, Limit: window.TotalEntries - window.Offset}
	if req.Window != clientui.TranscriptWindowOngoingTail {
		return pageReq
	}
	if req.KnownRevision <= 0 || req.KnownCommittedEntryCount <= 0 {
		return pageReq
	}
	if req.KnownRevision >= revision {
		return pageReq
	}
	if req.KnownCommittedEntryCount >= window.TotalEntries {
		return pageReq
	}
	if req.KnownCommittedEntryCount < window.Offset {
		return pageReq
	}
	offset := req.KnownCommittedEntryCount - OngoingTailIncrementalOverlapEntries
	if offset < window.Offset {
		offset = window.Offset
	}
	if offset >= window.TotalEntries {
		offset = window.Offset
	}
	pageReq.Offset = offset
	pageReq.Limit = window.TotalEntries - offset
	return pageReq
}

func TranscriptPageFromCollectedChat(sessionID, sessionName string, freshness clientui.ConversationFreshness, revision int64, snapshot clientui.ChatSnapshot, totalEntries, baseOffset int, req clientui.TranscriptPageRequest) clientui.TranscriptPage {
	page := transcriptPageFromNormalizedRequest(
		sessionID,
		sessionName,
		freshness,
		revision,
		snapshot,
		totalEntries,
		baseOffset,
		req,
	)
	page.Ongoing = snapshot.Ongoing
	page.OngoingError = snapshot.OngoingError
	return page
}

func CommittedTranscriptSuffixFromCollectedChat(sessionID, sessionName string, freshness clientui.ConversationFreshness, revision int64, snapshot clientui.ChatSnapshot, totalEntries, baseOffset int, req clientui.CommittedTranscriptSuffixRequest) clientui.CommittedTranscriptSuffix {
	req = clientui.NormalizeCommittedTranscriptSuffixRequest(req)
	if totalEntries < 0 {
		totalEntries = 0
	}
	startEntryCount := req.AfterEntryCount
	if startEntryCount > totalEntries {
		startEntryCount = totalEntries
	}
	if startEntryCount < baseOffset {
		startEntryCount = baseOffset
	}
	if startEntryCount > totalEntries {
		startEntryCount = totalEntries
	}
	total := len(snapshot.Entries)
	start := startEntryCount - baseOffset
	if startEntryCount >= totalEntries {
		start = total
	} else if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start
	if req.Limit > 0 {
		end = start + req.Limit
		if end > total {
			end = total
		}
	}
	entries := cloneChatEntries(snapshot.Entries[start:end])
	nextEntryCount := startEntryCount + len(entries)
	if nextEntryCount > totalEntries {
		nextEntryCount = totalEntries
	}
	return clientui.CommittedTranscriptSuffix{
		SessionID:             sessionID,
		SessionName:           sessionName,
		ConversationFreshness: freshness,
		Revision:              revision,
		CommittedEntryCount:   totalEntries,
		StartEntryCount:       startEntryCount,
		NextEntryCount:        nextEntryCount,
		HasMore:               nextEntryCount < totalEntries,
		Entries:               entries,
	}
}

func TranscriptPageFromChat(sessionID, sessionName string, freshness clientui.ConversationFreshness, revision int64, snapshot clientui.ChatSnapshot, req clientui.TranscriptPageRequest) clientui.TranscriptPage {
	total := len(snapshot.Entries)
	return transcriptPageFromNormalizedRequest(sessionID, sessionName, freshness, revision, snapshot, total, 0, req)
}

func transcriptPageFromNormalizedRequest(sessionID, sessionName string, freshness clientui.ConversationFreshness, revision int64, snapshot clientui.ChatSnapshot, totalEntries, baseOffset int, req clientui.TranscriptPageRequest) clientui.TranscriptPage {
	offset, limit := normalizeTranscriptPageRequest(req, totalEntries, baseOffset)
	total := len(snapshot.Entries)
	start := offset - baseOffset
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := total
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	entries := cloneChatEntries(snapshot.Entries[start:end])
	nextOffset := 0
	hasMore := offset+(end-start) < totalEntries
	if hasMore {
		nextOffset = offset + (end - start)
	}
	return clientui.TranscriptPage{
		SessionID:             sessionID,
		SessionName:           sessionName,
		ConversationFreshness: freshness,
		Revision:              revision,
		TotalEntries:          totalEntries,
		Offset:                offset,
		NextOffset:            nextOffset,
		HasMore:               hasMore,
		Entries:               entries,
		Ongoing:               snapshot.Ongoing,
		OngoingError:          snapshot.OngoingError,
	}
}

func normalizeTranscriptPageRequest(req clientui.TranscriptPageRequest, totalEntries, baseOffset int) (offset, limit int) {
	if req.PageSize > 0 {
		limit = req.PageSize
		offset = req.Page * req.PageSize
	} else {
		offset = req.Offset
		limit = req.Limit
	}
	if offset < 0 {
		offset = 0
	}
	if offset < baseOffset {
		offset = baseOffset
	}
	if totalEntries < 0 {
		totalEntries = 0
	}
	if offset > totalEntries {
		offset = totalEntries
	}
	if limit < 0 {
		limit = 0
	}
	return offset, limit
}

func cloneChatEntries(entries []clientui.ChatEntry) []clientui.ChatEntry {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]clientui.ChatEntry, 0, len(entries))
	for _, entry := range entries {
		copyEntry := entry
		copyEntry.ToolCall = cloneClientToolCallMeta(entry.ToolCall)
		cloned = append(cloned, copyEntry)
	}
	return cloned
}

func cloneClientToolCallMeta(meta *clientui.ToolCallMeta) *clientui.ToolCallMeta {
	if meta == nil {
		return nil
	}
	copyMeta := *meta
	if len(meta.Suggestions) > 0 {
		copyMeta.Suggestions = append([]string(nil), meta.Suggestions...)
	}
	if meta.RenderHint != nil {
		renderHint := *meta.RenderHint
		copyMeta.RenderHint = &renderHint
	}
	if meta.PatchRender != nil {
		copyMeta.PatchRender = cloneRenderedPatch(meta.PatchRender)
	}
	return &copyMeta
}
