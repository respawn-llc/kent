package metadata

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"core/server/session"
	"core/shared/config"
	"core/shared/sessioncontract"
)

func TestSessionPageSeparatesCategoriesAndMapsLegacyToMain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	updatedAt := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	importSessionPageFixture(t, store, cfg, binding, "00000000-0000-4000-8000-000000000101", nil, updatedAt.Add(2*time.Minute))
	importSessionPageFixture(t, store, cfg, binding, "00000000-0000-4000-8000-000000000102", sessionCategoryForPageTest(sessioncontract.SessionCategoryMain), updatedAt.Add(time.Minute))
	importSessionPageFixture(t, store, cfg, binding, "00000000-0000-4000-8000-000000000103", sessionCategoryForPageTest(sessioncontract.SessionCategorySubagent), updatedAt)

	mainPage, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, 0, 10)
	if err != nil {
		t.Fatalf("ListSessionPage main: %v", err)
	}
	requireSessionPageIDs(t, mainPage, "00000000-0000-4000-8000-000000000101", "00000000-0000-4000-8000-000000000102")

	subagentPage, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategorySubagent, 0, 10)
	if err != nil {
		t.Fatalf("ListSessionPage subagent: %v", err)
	}
	requireSessionPageIDs(t, subagentPage, "00000000-0000-4000-8000-000000000103")
}

func TestSessionPageReflectsPersistedCategoryPromotion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	updatedAt := time.Now().UTC()
	category := sessioncontract.SessionCategorySubagent
	importSessionPageFixture(t, store, cfg, binding, "00000000-0000-4000-8000-000000000104", &category, updatedAt)

	snapshot, err := store.ResolvePersistedSession(ctx, "00000000-0000-4000-8000-000000000104")
	if err != nil {
		t.Fatalf("ResolvePersistedSession: %v", err)
	}
	promoted := sessioncontract.SessionCategoryMain
	snapshot.Meta.Category = &promoted
	snapshot.Meta.UpdatedAt = updatedAt.Add(time.Second)
	if err := store.ImportSessionSnapshot(ctx, session.PersistedStoreSnapshot{
		SessionDir: snapshot.SessionDir,
		Meta:       *snapshot.Meta,
	}); err != nil {
		t.Fatalf("ImportSessionSnapshot promotion: %v", err)
	}

	mainPage, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, 0, 10)
	if err != nil {
		t.Fatalf("ListSessionPage main: %v", err)
	}
	requireSessionPageIDs(t, mainPage, "00000000-0000-4000-8000-000000000104")

	subagentPage, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategorySubagent, 0, 10)
	if err != nil {
		t.Fatalf("ListSessionPage subagent: %v", err)
	}
	requireSessionPageIDs(t, subagentPage)
}

func TestSessionPageUsesStableRecencyOrderAcrossAdjacentOffsets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	updatedAt := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"00000000-0000-4000-8000-000000000004", "00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001"} {
		importSessionPageFixture(t, store, cfg, binding, id, sessionCategoryForPageTest(sessioncontract.SessionCategoryMain), updatedAt)
	}

	first, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, 0, 2)
	if err != nil {
		t.Fatalf("ListSessionPage first: %v", err)
	}
	requireSessionPageIDs(t, first, "00000000-0000-4000-8000-000000000004", "00000000-0000-4000-8000-000000000003")
	if first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("first next offset = %v, want 2", first.NextOffset)
	}

	second, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, *first.NextOffset, 2)
	if err != nil {
		t.Fatalf("ListSessionPage second: %v", err)
	}
	requireSessionPageIDs(t, second, "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001")
	if second.NextOffset != nil {
		t.Fatalf("second next offset = %v, want nil", second.NextOffset)
	}

	beyond, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, 10, 2)
	if err != nil {
		t.Fatalf("ListSessionPage beyond end: %v", err)
	}
	requireSessionPageIDs(t, beyond)
	if beyond.NextOffset != nil {
		t.Fatalf("beyond next offset = %v, want nil", beyond.NextOffset)
	}
}

func TestSessionPageAcceptsLiveRepeatAndSkipAfterRecencyChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	base := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	for index, id := range []string{"00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000001"} {
		importSessionPageFixture(t, store, cfg, binding, id, sessionCategoryForPageTest(sessioncontract.SessionCategoryMain), base.Add(-time.Duration(index)*time.Minute))
	}
	first, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, 0, 2)
	if err != nil {
		t.Fatalf("ListSessionPage first: %v", err)
	}
	requireSessionPageIDs(t, first, "00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-000000000002")
	if first.NextOffset == nil {
		t.Fatal("expected next offset")
	}

	importSessionPageFixture(t, store, cfg, binding, "00000000-0000-4000-8000-000000000001", sessionCategoryForPageTest(sessioncontract.SessionCategoryMain), base.Add(time.Minute))
	second, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, *first.NextOffset, 2)
	if err != nil {
		t.Fatalf("ListSessionPage second: %v", err)
	}
	requireSessionPageIDs(t, second, "00000000-0000-4000-8000-000000000002")

	refreshed, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, 0, 2)
	if err != nil {
		t.Fatalf("ListSessionPage refreshed: %v", err)
	}
	requireSessionPageIDs(t, refreshed, "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000003")
}

func TestSessionPageDefaultWindowTrimsOneHundredAndOneRowLookahead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, cfg, binding := newMetadataTestStore(t)
	base := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	for index := range MaxSessionPageSize + 1 {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
		importSessionPageFixture(t, store, cfg, binding, id, sessionCategoryForPageTest(sessioncontract.SessionCategoryMain), base.Add(time.Duration(index)*time.Second))
	}

	page, err := store.ListSessionPage(ctx, binding.ProjectID, sessioncontract.SessionCategoryMain, 0, MaxSessionPageSize)
	if err != nil {
		t.Fatalf("ListSessionPage: %v", err)
	}
	if len(page.Sessions) != MaxSessionPageSize {
		t.Fatalf("session count = %d, want %d", len(page.Sessions), MaxSessionPageSize)
	}
	if page.NextOffset == nil || *page.NextOffset != MaxSessionPageSize {
		t.Fatalf("next offset = %v, want %d", page.NextOffset, MaxSessionPageSize)
	}
}

func importSessionPageFixture(
	t *testing.T,
	store *Store,
	cfg config.App,
	binding Binding,
	id string,
	category *sessioncontract.SessionCategory,
	updatedAt time.Time,
) {
	t.Helper()
	err := store.ImportSessionSnapshot(t.Context(), session.PersistedStoreSnapshot{
		SessionDir: filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions", id),
		Meta: session.Meta{
			SessionID:          id,
			Category:           category,
			Name:               &id,
			WorkspaceRoot:      binding.CanonicalRoot,
			WorkspaceContainer: filepath.Base(binding.CanonicalRoot),
			CreatedAt:          updatedAt.Add(-time.Hour),
			UpdatedAt:          updatedAt,
		},
	})
	if err != nil {
		t.Fatalf("ImportSessionSnapshot(%s): %v", id, err)
	}
}

func sessionCategoryForPageTest(category sessioncontract.SessionCategory) *sessioncontract.SessionCategory {
	return &category
}

func requireSessionPageIDs(t *testing.T, page SessionPage, want ...string) {
	t.Helper()
	if len(page.Sessions) != len(want) {
		t.Fatalf("session count = %d, want %d: %+v", len(page.Sessions), len(want), page.Sessions)
	}
	for index := range want {
		if got := page.Sessions[index].SessionID.String(); got != want[index] {
			t.Fatalf("session %d = %q, want %q", index, got, want[index])
		}
	}
}
