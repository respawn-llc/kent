package sessionview

import (
	"context"
	"strings"
	"sync"

	"core/server/runtime"
	"core/server/runtimeview"
	"core/server/session"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/transcript/patchformat"
)

const dormantTranscriptCacheMaxEntries = 16
const dormantTranscriptPageCacheMaxEntries = 64

type dormantTranscriptCache struct {
	mu      sync.RWMutex
	entries map[string]dormantTranscriptCacheEntry
	build   func(context.Context, *session.Store) (dormantTranscriptCacheEntry, error)
	maxSize int
	clock   uint64
}

type dormantTranscriptCacheEntry struct {
	sessionDir                   string
	sessionID                    string
	revision                     int64
	totalEntries                 int
	lastCommittedAssistantAnswer string
	ongoingTail                  runtime.TranscriptWindowSnapshot
	activeRun                    *clientui.RunView
	lastUsed                     uint64
}

func newDormantTranscriptCacheWithLimit(limit int, build func(context.Context, *session.Store) (dormantTranscriptCacheEntry, error)) *dormantTranscriptCache {
	if build == nil {
		build = buildDormantTranscriptCacheEntry
	}
	if limit <= 0 {
		limit = dormantTranscriptCacheMaxEntries
	}
	return &dormantTranscriptCache{entries: make(map[string]dormantTranscriptCacheEntry), build: build, maxSize: limit}
}

func (c *dormantTranscriptCache) get(ctx context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
	if c == nil || store == nil {
		return dormantTranscriptCacheEntry{}, nil
	}
	meta := store.Meta()
	key := dormantTranscriptCacheKey(store.Dir(), meta.SessionID)
	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok && strings.TrimSpace(entry.sessionDir) == strings.TrimSpace(store.Dir()) &&
		strings.TrimSpace(entry.sessionID) == strings.TrimSpace(meta.SessionID) &&
		entry.revision == meta.LastSequence {
		entry.lastUsed = c.nextStampLocked()
		c.entries[key] = entry
		c.mu.Unlock()
		return entry, nil
	}
	c.mu.Unlock()
	built, err := c.build(ctx, store)
	if err != nil {
		return dormantTranscriptCacheEntry{}, err
	}
	c.mu.Lock()
	if existing, ok := c.entries[key]; ok &&
		strings.TrimSpace(existing.sessionDir) == strings.TrimSpace(store.Dir()) &&
		strings.TrimSpace(existing.sessionID) == strings.TrimSpace(meta.SessionID) &&
		existing.revision == meta.LastSequence {
		existing.lastUsed = c.nextStampLocked()
		c.entries[key] = existing
		c.mu.Unlock()
		return existing, nil
	}
	built.lastUsed = c.nextStampLocked()
	c.entries[key] = built
	c.evictIfNeededLocked()
	c.mu.Unlock()
	return built, nil
}

func (c *dormantTranscriptCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	c.clock = 0
}

func (c *dormantTranscriptCache) nextStampLocked() uint64 {
	c.clock++
	return c.clock
}

func (c *dormantTranscriptCache) evictIfNeededLocked() {
	if c == nil || c.maxSize <= 0 || len(c.entries) <= c.maxSize {
		return
	}
	oldestKey := ""
	oldestStamp := uint64(0)
	for key, entry := range c.entries {
		if oldestKey == "" || entry.lastUsed < oldestStamp {
			oldestKey = key
			oldestStamp = entry.lastUsed
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func dormantTranscriptCacheKey(sessionDir, sessionID string) string {
	return strings.TrimSpace(sessionDir) + "::" + strings.TrimSpace(sessionID)
}

func buildDormantTranscriptCacheEntry(ctx context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
	meta := store.Meta()
	scan, err := scanDormantTranscript(ctx, store, runtime.PersistedTranscriptScanRequest{
		TrackOngoingTail: true,
		TailLimit:        runtimeview.OngoingTailEntryLimit,
		CacheWarningMode: config.CacheWarningModeDefault,
	})
	if err != nil {
		return dormantTranscriptCacheEntry{}, err
	}
	var activeRun *clientui.RunView
	latestRun, err := store.LatestRun()
	if err != nil {
		return dormantTranscriptCacheEntry{}, err
	}
	if latestRun != nil && latestRun.Status == session.RunStatusRunning {
		activeRun = runtimeview.RunViewFromSessionRecord(meta.SessionID, latestRun)
	}
	return dormantTranscriptCacheEntry{
		sessionDir:                   store.Dir(),
		sessionID:                    meta.SessionID,
		revision:                     meta.LastSequence,
		totalEntries:                 scan.TotalEntries(),
		lastCommittedAssistantAnswer: scan.LastCommittedAssistantFinalAnswer(),
		ongoingTail:                  scan.OngoingTailSnapshot(),
		activeRun:                    activeRun,
	}, nil
}

func (e dormantTranscriptCacheEntry) mainView(meta session.Meta, freshness clientui.ConversationFreshness) clientui.RuntimeMainView {
	status := clientui.RuntimeStatus{
		ConversationFreshness:             freshness,
		ParentSessionID:                   meta.ParentSessionID,
		LastCommittedAssistantFinalAnswer: e.lastCommittedAssistantAnswer,
		Goal:                              runtimeview.GoalFromSessionState(meta.Goal, false),
	}
	if meta.WorkflowSession != nil {
		status.WorkflowSession = &clientui.WorkflowSessionStatus{
			RunID:      strings.TrimSpace(meta.WorkflowSession.RunID),
			TaskID:     strings.TrimSpace(meta.WorkflowSession.TaskID),
			WorkflowID: strings.TrimSpace(meta.WorkflowSession.WorkflowID),
		}
	}
	return clientui.RuntimeMainView{
		Status: status,
		Session: clientui.RuntimeSessionView{
			SessionID:             meta.SessionID,
			SessionName:           meta.Name,
			ConversationFreshness: freshness,
			Transcript: clientui.TranscriptMetadata{
				Revision:            meta.LastSequence,
				CommittedEntryCount: e.totalEntries,
			},
		},
		ActiveRun: e.activeRun,
	}
}

func (e dormantTranscriptCacheEntry) transcriptPageCoveredByTail(meta session.Meta, freshness clientui.ConversationFreshness, req clientui.TranscriptPageRequest) (clientui.TranscriptPage, bool) {
	if req.Limit <= 0 {
		return clientui.TranscriptPage{}, false
	}
	tailOffset := e.ongoingTail.Offset
	tailEntries := e.ongoingTail.Snapshot.Entries
	if req.Offset < tailOffset {
		return clientui.TranscriptPage{}, false
	}
	end := req.Offset + req.Limit
	tailEnd := tailOffset + len(tailEntries)
	if end > tailEnd {
		return clientui.TranscriptPage{}, false
	}
	start := req.Offset - tailOffset
	snapshot := runtime.ChatSnapshot{Entries: cloneDormantChatEntries(tailEntries[start : start+req.Limit])}
	return runtimeview.TranscriptPageFromCollectedChat(
		meta.SessionID,
		meta.Name,
		freshness,
		meta.LastSequence,
		runtimeview.ChatSnapshotFromRuntime(snapshot),
		e.totalEntries,
		req.Offset,
		clientui.TranscriptPageRequest{Offset: req.Offset, Limit: req.Limit},
	), true
}

func cloneDormantChatEntries(entries []runtime.ChatEntry) []runtime.ChatEntry {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]runtime.ChatEntry, 0, len(entries))
	for _, entry := range entries {
		cloned = append(cloned, entry)
	}
	return cloned
}

type dormantTranscriptPageCache struct {
	mu      sync.RWMutex
	entries map[dormantTranscriptPageCacheKey]dormantTranscriptPageCacheEntry
	maxSize int
	clock   uint64
}

type dormantTranscriptPageCacheKey struct {
	sessionDir       string
	sessionID        string
	sessionName      string
	revision         int64
	freshness        clientui.ConversationFreshness
	cacheWarningMode config.CacheWarningMode
	offset           int
	limit            int
}

type dormantTranscriptPageCacheEntry struct {
	page     clientui.TranscriptPage
	lastUsed uint64
}

func newDormantTranscriptPageCacheWithLimit(limit int) *dormantTranscriptPageCache {
	if limit <= 0 {
		limit = dormantTranscriptPageCacheMaxEntries
	}
	return &dormantTranscriptPageCache{
		entries: make(map[dormantTranscriptPageCacheKey]dormantTranscriptPageCacheEntry),
		maxSize: limit,
	}
}

func (c *dormantTranscriptPageCache) getOrBuild(key dormantTranscriptPageCacheKey, build func() (clientui.TranscriptPage, error)) (clientui.TranscriptPage, error) {
	if c == nil || build == nil || key.limit <= 0 {
		return build()
	}
	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok {
		entry.lastUsed = c.nextStampLocked()
		c.entries[key] = entry
		c.mu.Unlock()
		return cloneDormantTranscriptPage(entry.page), nil
	}
	c.mu.Unlock()
	page, err := build()
	if err != nil {
		return clientui.TranscriptPage{}, err
	}
	c.mu.Lock()
	if existing, ok := c.entries[key]; ok {
		existing.lastUsed = c.nextStampLocked()
		c.entries[key] = existing
		c.mu.Unlock()
		return cloneDormantTranscriptPage(existing.page), nil
	}
	storedPage := cloneDormantTranscriptPage(page)
	c.entries[key] = dormantTranscriptPageCacheEntry{page: storedPage, lastUsed: c.nextStampLocked()}
	c.evictIfNeededLocked()
	c.mu.Unlock()
	return cloneDormantTranscriptPage(storedPage), nil
}

func (c *dormantTranscriptPageCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	c.clock = 0
}

func (c *dormantTranscriptPageCache) nextStampLocked() uint64 {
	c.clock++
	return c.clock
}

func (c *dormantTranscriptPageCache) evictIfNeededLocked() {
	if c == nil || c.maxSize <= 0 || len(c.entries) <= c.maxSize {
		return
	}
	oldestKey := dormantTranscriptPageCacheKey{}
	oldestStamp := uint64(0)
	for key, entry := range c.entries {
		if oldestStamp == 0 || entry.lastUsed < oldestStamp {
			oldestKey = key
			oldestStamp = entry.lastUsed
		}
	}
	if oldestStamp != 0 {
		delete(c.entries, oldestKey)
	}
}

func dormantTranscriptPageCacheKeyForStore(store *session.Store, meta session.Meta, freshness clientui.ConversationFreshness, cacheWarningMode config.CacheWarningMode, offset, limit int) dormantTranscriptPageCacheKey {
	return dormantTranscriptPageCacheKey{
		sessionDir:       strings.TrimSpace(store.Dir()),
		sessionID:        strings.TrimSpace(meta.SessionID),
		sessionName:      strings.TrimSpace(meta.Name),
		revision:         meta.LastSequence,
		freshness:        freshness,
		cacheWarningMode: normalizeServiceCacheWarningMode(cacheWarningMode),
		offset:           offset,
		limit:            limit,
	}
}

func cloneDormantTranscriptPage(page clientui.TranscriptPage) clientui.TranscriptPage {
	page.Entries = cloneDormantClientChatEntries(page.Entries)
	return page
}

func cloneDormantClientChatEntries(entries []clientui.ChatEntry) []clientui.ChatEntry {
	if len(entries) == 0 {
		return nil
	}
	cloned := make([]clientui.ChatEntry, 0, len(entries))
	for _, entry := range entries {
		copyEntry := entry
		copyEntry.ToolCall = cloneDormantClientToolCallMeta(entry.ToolCall)
		cloned = append(cloned, copyEntry)
	}
	return cloned
}

func cloneDormantClientToolCallMeta(meta *clientui.ToolCallMeta) *clientui.ToolCallMeta {
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
		copyMeta.PatchRender = cloneDormantRenderedPatch(meta.PatchRender)
	}
	return &copyMeta
}

func cloneDormantRenderedPatch(in *patchformat.RenderedPatch) *patchformat.RenderedPatch {
	if in == nil {
		return nil
	}
	out := &patchformat.RenderedPatch{}
	if len(in.Files) > 0 {
		out.Files = make([]patchformat.RenderedFile, 0, len(in.Files))
		for _, file := range in.Files {
			copyFile := file
			if len(file.Diff) > 0 {
				copyFile.Diff = append([]string(nil), file.Diff...)
			}
			out.Files = append(out.Files, copyFile)
		}
	}
	if len(in.SummaryLines) > 0 {
		out.SummaryLines = append([]patchformat.RenderedLine(nil), in.SummaryLines...)
	}
	if len(in.DetailLines) > 0 {
		out.DetailLines = append([]patchformat.RenderedLine(nil), in.DetailLines...)
	}
	return out
}
