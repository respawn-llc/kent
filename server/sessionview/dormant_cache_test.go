package sessionview

import (
	"context"
	"fmt"
	"testing"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/transcript/patchformat"
)

func TestDormantTranscriptCacheReusesEntryForUnchangedRevision(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleUser, Content: "hello"}); err != nil {
		t.Fatalf("append user message: %v", err)
	}

	buildCalls := 0
	cache := newDormantTranscriptCacheWithLimit(dormantTranscriptCacheMaxEntries, func(_ context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
		buildCalls++
		meta := store.Meta()
		return dormantTranscriptCacheEntry{
			sessionDir:                   store.Dir(),
			sessionID:                    meta.SessionID,
			revision:                     meta.LastSequence,
			lastCommittedAssistantAnswer: "done",
			newestSegment:                dormantSegmentFixture([]string{"tail-1", "tail-2"}),
		}, nil
	})

	entry, err := cache.get(context.Background(), store)
	if err != nil {
		t.Fatalf("cache get: %v", err)
	}
	if len(entry.newestSegment.Snapshot.Entries) != 2 || entry.lastCommittedAssistantAnswer != "done" {
		t.Fatalf("unexpected cache entry: %+v", entry)
	}
	entry, err = cache.get(context.Background(), store)
	if err != nil {
		t.Fatalf("cache get second time: %v", err)
	}
	if len(entry.newestSegment.Snapshot.Entries) != 2 {
		t.Fatalf("unexpected second cache entry: %+v", entry)
	}
	if buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1", buildCalls)
	}
}

func TestDormantTranscriptCacheInvalidatesOnRevisionAdvance(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleUser, Content: "hello"}); err != nil {
		t.Fatalf("append first message: %v", err)
	}

	buildCalls := 0
	cache := newDormantTranscriptCacheWithLimit(dormantTranscriptCacheMaxEntries, func(_ context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
		buildCalls++
		meta := store.Meta()
		return dormantTranscriptCacheEntry{
			sessionDir: store.Dir(),
			sessionID:  meta.SessionID,
			revision:   meta.LastSequence,
		}, nil
	})

	if _, err := cache.get(context.Background(), store); err != nil {
		t.Fatalf("cache get: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal}); err != nil {
		t.Fatalf("append second message: %v", err)
	}
	if _, err := cache.get(context.Background(), store); err != nil {
		t.Fatalf("cache get after revision advance: %v", err)
	}
	if buildCalls != 2 {
		t.Fatalf("build calls = %d, want 2", buildCalls)
	}
}

func TestDormantTranscriptCacheEvictsLeastRecentlyUsedEntry(t *testing.T) {
	root := t.TempDir()
	storeA, err := session.Create(root, "ws", root)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}
	storeB, err := session.Create(root, "ws", root)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}
	storeC, err := session.Create(root, "ws", root)
	if err != nil {
		t.Fatalf("create store C: %v", err)
	}
	buildCalls := 0
	cache := newDormantTranscriptCacheWithLimit(2, func(_ context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
		buildCalls++
		meta := store.Meta()
		return dormantTranscriptCacheEntry{sessionDir: store.Dir(), sessionID: meta.SessionID, revision: meta.LastSequence}, nil
	})

	if _, err := cache.get(context.Background(), storeA); err != nil {
		t.Fatalf("cache get A: %v", err)
	}
	if _, err := cache.get(context.Background(), storeB); err != nil {
		t.Fatalf("cache get B: %v", err)
	}
	if _, err := cache.get(context.Background(), storeA); err != nil {
		t.Fatalf("cache re-get A: %v", err)
	}
	if _, err := cache.get(context.Background(), storeC); err != nil {
		t.Fatalf("cache get C: %v", err)
	}
	if _, err := cache.get(context.Background(), storeB); err != nil {
		t.Fatalf("cache re-get B after eviction: %v", err)
	}
	if buildCalls != 4 {
		t.Fatalf("build calls = %d, want 4", buildCalls)
	}
}

func TestDormantTranscriptPageCacheReturnsMutationSafeCopies(t *testing.T) {
	cache := newDormantTranscriptPageCacheWithLimit(dormantTranscriptPageCacheMaxEntries)
	key := dormantTranscriptPageCacheKey{
		sessionDir:       "dir",
		sessionID:        "session-1",
		revision:         1,
		cacheWarningMode: config.CacheWarningModeDefault,
		cursor:           1024,
	}
	buildCalls := 0
	build := func() (clientui.TranscriptPage, error) {
		buildCalls++
		return clientui.TranscriptPage{
			SessionID: "session-1",
			Entries: []clientui.ChatEntry{{
				Role: "tool_call",
				Text: "original",
				ToolCall: &clientui.ToolCallMeta{
					ToolName:    "patch",
					Suggestions: []string{"first"},
					RenderHint:  &clientui.ToolRenderHint{Path: "old"},
					PatchRender: &patchformat.RenderedPatch{DetailLines: []patchformat.RenderedLine{{Text: "old diff"}}},
				},
			}},
		}, nil
	}

	first, err := cache.getOrBuild(key, build)
	if err != nil {
		t.Fatalf("getOrBuild first: %v", err)
	}
	first.Entries[0].Text = "mutated"
	first.Entries[0].ToolCall.ToolName = "mutated"
	first.Entries[0].ToolCall.Suggestions[0] = "mutated"
	first.Entries[0].ToolCall.RenderHint.Path = "mutated"
	first.Entries[0].ToolCall.PatchRender.DetailLines[0].Text = "mutated"

	second, err := cache.getOrBuild(key, build)
	if err != nil {
		t.Fatalf("getOrBuild second: %v", err)
	}
	if buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1", buildCalls)
	}
	entry := second.Entries[0]
	if entry.Text != "original" || entry.ToolCall.ToolName != "patch" || entry.ToolCall.Suggestions[0] != "first" || entry.ToolCall.RenderHint.Path != "old" || entry.ToolCall.PatchRender.DetailLines[0].Text != "old diff" {
		t.Fatalf("cached page mutated through returned copy: %+v", entry)
	}
}

func TestDormantTranscriptPageCacheKeyIncludesCacheWarningMode(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	meta := store.Meta()
	defaultKey := dormantTranscriptPageCacheKeyForStore(store, meta, clientui.ConversationFreshnessEstablished, config.CacheWarningModeDefault, 1024)
	verboseKey := dormantTranscriptPageCacheKeyForStore(store, meta, clientui.ConversationFreshnessEstablished, config.CacheWarningModeVerbose, 1024)
	if defaultKey == verboseKey {
		t.Fatalf("expected cache-warning mode to contribute to dormant page cache key: %+v", defaultKey)
	}
}

func TestServiceUsesDormantCacheForMainViewAndTailCoveredPages(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(dir, "ws", dir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetName("incident triage"); err != nil {
		t.Fatalf("set name: %v", err)
	}
	if _, _, err := store.AppendEvent("step-1", "message", llm.Message{Role: llm.RoleUser, Content: "seed"}); err != nil {
		t.Fatalf("append seed message: %v", err)
	}

	buildCalls := 0
	cache := newDormantTranscriptCacheWithLimit(dormantTranscriptCacheMaxEntries, func(_ context.Context, store *session.Store) (dormantTranscriptCacheEntry, error) {
		buildCalls++
		meta := store.Meta()
		return dormantTranscriptCacheEntry{
			sessionDir:                   store.Dir(),
			sessionID:                    meta.SessionID,
			revision:                     meta.LastSequence,
			lastCommittedAssistantAnswer: "done",
			newestSegment:                dormantSegmentFixture(buildTailTexts(0, 500)),
		}, nil
	})

	dormantSource := newDormantSessionSnapshotSource(nil)
	dormantSource.dormant = cache
	svc := &Service{snapshots: &resolvedSessionSnapshotSource{sessions: NewStaticSessionResolver(store), dormant: dormantSource}}
	mainViewResp, err := svc.GetSessionMainView(context.Background(), serverapi.SessionMainViewRequest{SessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get session main view: %v", err)
	}
	if got := mainViewResp.MainView.Status.LastCommittedAssistantFinalAnswer; got != "done" {
		t.Fatalf("last committed assistant final answer = %q, want done", got)
	}

	pageResp, err := svc.GetSessionTranscriptPage(context.Background(), serverapi.SessionTranscriptPageRequest{SessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("get default transcript page: %v", err)
	}
	if got := len(pageResp.Transcript.Entries); got != 500 {
		t.Fatalf("default newest-segment entry count = %d, want 500", got)
	}
	if pageResp.Transcript.Entries[0].Text != "line 0" {
		t.Fatalf("unexpected first entry: %q", pageResp.Transcript.Entries[0].Text)
	}
	if buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1", buildCalls)
	}
}

func dormantSegmentFixture(texts []string) runtime.TranscriptSegmentPage {
	entries := make([]runtime.ChatEntry, 0, len(texts))
	for _, text := range texts {
		entries = append(entries, runtime.ChatEntry{Role: "assistant", Text: text})
	}
	return runtime.TranscriptSegmentPage{Snapshot: runtime.ChatSnapshot{Entries: entries}}
}

func buildTailTexts(offset, count int) []string {
	texts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		texts = append(texts, fmt.Sprintf("line %d", offset+i))
	}
	return texts
}
