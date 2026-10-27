package runtime

import (
	"context"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/server/tools"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/transcriptmodel"
)

func TestCommittedCacheResponseObserverFailureRetainsLineage(t *testing.T) {
	t.Parallel()
	observerErr := errors.New("cache response observer failure")
	gate := sessiontest.NewPersistenceGate(runtimeTestSessionPersistence)
	store := mustCreateTestSessionAt(t, t.TempDir(), session.WithPersistenceObserver(gate))
	client := &fakeClient{responses: []llm.Response{
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(7)}},
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(0)}},
	}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:            "gpt-6-sol",
		CacheWarningMode: config.CacheWarningModeDefault,
	})
	baselineSequence := store.Meta().LastSequence
	gate.FailWhen(func(snapshot session.PersistedStoreSnapshot) bool {
		return snapshot.Meta.LastSequence > baselineSequence+1
	}, observerErr)

	if _, err := generateTestActiveStep(
		context.Background(),
		engine,
		"first",
		client,
		cacheLineageRequest("conversation", transcript.CacheWarningScopeConversation, "alpha"),
	); !errors.Is(err, observerErr) {
		t.Fatalf("first cache observation error = %v, want observer error", err)
	}
	if _, err := generateTestActiveStep(
		context.Background(),
		engine,
		"second",
		client,
		cacheLineageRequest("conversation", transcript.CacheWarningScopeConversation, "beta"),
	); err != nil {
		t.Fatalf("second cache observation: %v", err)
	}

	assertBoundedCacheWarning(t, store, session.CacheScopeConversation, session.CacheWarningReasonNonPostfix, 7)
}

func TestVerboseCacheReuseDropPersistsTypedWarning(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(4)}},
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(0)}},
	}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:            "gpt-6-sol",
		CacheWarningMode: config.CacheWarningModeVerbose,
	})

	if _, err := generateTestActiveStep(
		context.Background(),
		engine,
		"first",
		client,
		cacheLineageRequest("conversation", transcript.CacheWarningScopeConversation, "alpha"),
	); err != nil {
		t.Fatalf("first cache observation: %v", err)
	}
	if _, err := generateTestActiveStep(
		context.Background(),
		engine,
		"second",
		client,
		cacheLineageRequest("conversation", transcript.CacheWarningScopeConversation, "alpha", "omega"),
	); err != nil {
		t.Fatalf("second cache observation: %v", err)
	}

	assertBoundedCacheWarning(t, store, session.CacheScopeConversation, session.CacheWarningReasonReuseDropped, 4)
}

func TestReviewerCacheLineagePersistsScopedWarning(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(8)}},
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(6)}},
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(10)}},
		{Usage: llm.Usage{CachedInputTokens: textutil.Value(0)}},
	}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model:            "gpt-6-sol",
		CacheWarningMode: config.CacheWarningModeVerbose,
	})
	requests := []llm.Request{
		cacheLineageRequest("conversation", transcript.CacheWarningScopeConversation, "alpha"),
		cacheLineageRequest("reviewer", transcript.CacheWarningScopeReviewer, "beta"),
		cacheLineageRequest("conversation", transcript.CacheWarningScopeConversation, "alpha", "omega"),
		cacheLineageRequest("reviewer", transcript.CacheWarningScopeReviewer, "gamma"),
	}
	for index, request := range requests {
		if _, err := generateTestActiveStep(context.Background(), engine, "cache-lineage", client, request); err != nil {
			t.Fatalf("cache observation %d: %v", index, err)
		}
	}

	assertBoundedCacheWarning(t, store, session.CacheScopeReviewer, session.CacheWarningReasonNonPostfix, 6)
}

func TestRebuiltAgentModelChangeWarnsAtDispatch(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	client := &fakeClient{responses: []llm.Response{{}, {}}}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", CacheWarningMode: config.CacheWarningModeVerbose,
	})
	key := store.Meta().SessionID
	for _, contents := range [][]string{{"first"}, {"first", "second"}} {
		request := cacheLineageRequest(key, transcript.CacheWarningScopeConversation, contents...)
		if _, err := generateTestActiveStep(t.Context(), engine, "original-agent", client, request); err != nil {
			t.Fatalf("original Agent dispatch: %v", err)
		}
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close original Agent: %v", err)
	}
	reopened := mustOpenTestSession(t, store.Dir())
	replacementClient := &fakeClient{responses: []llm.Response{{}, {}}}
	var warningsEmitted []Event
	replacement := mustNewTestEngine(t, reopened, replacementClient, tools.NewRegistry(), Config{
		Model: "gpt-6-astra", CacheWarningMode: config.CacheWarningModeVerbose,
		OnEvent: func(event Event) {
			if event.Kind == EventCacheWarning {
				warningsEmitted = append(warningsEmitted, event)
			}
		},
	})
	for _, contents := range [][]string{{"first", "second", "third"}, {"first", "second", "third", "fourth"}} {
		request := cacheLineageRequest(key, transcript.CacheWarningScopeConversation, contents...)
		request.Model = "gpt-6-astra"
		if _, err := generateTestActiveStep(t.Context(), replacement, "replacement-agent", replacementClient, request); err != nil {
			t.Fatalf("replacement Agent dispatch: %v", err)
		}
	}
	warnings := persistedCacheWarnings(t, reopened)
	if len(warnings) != 1 {
		t.Fatalf("cache warnings after model replacement and postfix follow-up = %+v, want one", warnings)
	}
	if warnings[0].Scope != transcript.CacheWarningScopeConversation ||
		warnings[0].Reason != transcript.CacheWarningReasonNonPostfix ||
		warnings[0].CacheKey == nil || *warnings[0].CacheKey != key ||
		warnings[0].LostInputTokens != nil {
		t.Fatalf("replacement cache warning = %+v", warnings[0])
	}
	if len(warningsEmitted) != 1 || warningsEmitted[0].CacheWarningVisibility != transcript.EntryVisibilityOngoing {
		t.Fatalf("live replacement cache warning = %+v, want one Ongoing warning", warningsEmitted)
	}
}

func cacheLineageRequest(cacheKey string, scope transcript.CacheWarningScope, contents ...string) llm.Request {
	items := make([]llm.ResponseItem, 0, len(contents))
	for _, content := range contents {
		items = append(items, llm.ItemsFromMessages([]llm.Message{{
			Role:    llm.RoleUser,
			Content: textutil.Value(content),
		}})...)
	}
	return llm.Request{
		Model:            "gpt-6-sol",
		PromptCacheKey:   cacheKey,
		PromptCacheScope: scope,
		Items:            items, ReasoningEffort: "high",
	}
}

func assertBoundedCacheWarning(
	t *testing.T,
	store *session.Store,
	scope session.CacheScope,
	reason session.CacheWarningReason,
	lostInputTokens int,
) {
	t.Helper()
	window, err := mustMaterializeTestEventLog(t, store).ReadRecentRecords(8)
	if err != nil {
		t.Fatalf("read bounded cache-lineage records: %v", err)
	}
	var warnings []session.CacheWarningRecord
	for _, record := range window.Records {
		warning, ok := mustSessionEventPayload(record).(session.CacheWarningRecord)
		if ok {
			warnings = append(warnings, warning)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("typed cache warnings = %+v, want one", warnings)
	}
	warning := warnings[0]
	if warning.Scope != scope || warning.Reason != reason ||
		warning.LostInputTokens == nil || *warning.LostInputTokens != lostInputTokens {
		t.Fatalf("typed cache warning = %+v", warning)
	}
}
