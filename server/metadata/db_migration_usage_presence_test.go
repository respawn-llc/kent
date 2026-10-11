package metadata

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"core/server/session"
)

func TestUsagePresenceCutoverPreservesHistoricalFactsAndNewZero(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.sqlite3")
	db, err := openDatabaseAtVersionForTest(t, root, path, 94)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES ('usage-project', 'Project', 1, 1)`)
	for _, fixture := range []struct{ id, usage string }{
		{"absent", `{"input_tokens":0,"output_tokens":0,"cached_input_tokens":0,"has_cached_input_tokens":true,"estimated_provider_tokens":50,"total_input_tokens":100,"total_cached_input_tokens":25}`},
		{"present", `{"input_tokens":80,"output_tokens":12,"cached_input_tokens":40,"has_cached_input_tokens":true,"estimated_provider_tokens":50,"total_input_tokens":100,"total_cached_input_tokens":25}`},
	} {
		execSeed(t, db, "session", `INSERT INTO sessions (
id, project_id, artifact_relpath, name, input_draft, locked_json, continuation_json, usage_state_json, metadata_json, last_sequence, created_at_unix_ms, updated_at_unix_ms
) VALUES (?, 'usage-project', ?, '', '', '{}', '{}', ?, '{}', 0, 1, 1)`, fixture.id, "sessions/"+fixture.id, fixture.usage)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAtPath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	read := func(id string) session.UsageState {
		t.Helper()
		var encoded string
		if err := store.DB().QueryRowContext(t.Context(), `SELECT usage_state_json FROM sessions WHERE id = ?`, id).Scan(&encoded); err != nil {
			t.Fatal(err)
		}
		var state session.UsageState
		if err := json.Unmarshal([]byte(encoded), &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	absent := read("absent")
	if absent.InputTokens != nil || absent.OutputTokens != nil || absent.CachedInputTokens != nil || absent.ReportedContextTokens != nil {
		t.Fatalf("historical zero retained presence: %+v", absent)
	}
	present := read("present")
	if present.InputTokens == nil || *present.InputTokens != 80 || present.OutputTokens == nil || *present.OutputTokens != 12 ||
		present.CachedInputTokens == nil || *present.CachedInputTokens != 40 || present.ReportedContextTokens == nil || *present.ReportedContextTokens != 80 ||
		present.EstimatedProviderTokens != 50 || present.TotalInputTokens != 100 || present.TotalCachedInputTokens != 25 {
		t.Fatalf("historical facts changed: %+v", present)
	}
	execSeed(t, store.DB(), "new explicit zero", `UPDATE sessions SET usage_state_json = '{"input_tokens":0,"output_tokens":0,"cached_input_tokens":0,"reported_context_tokens":0,"estimated_provider_tokens":50}' WHERE id = 'absent'`)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenAtPath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	zero := read("absent")
	if zero.InputTokens == nil || zero.OutputTokens == nil || zero.CachedInputTokens == nil || zero.ReportedContextTokens == nil ||
		*zero.InputTokens != 0 || *zero.OutputTokens != 0 || *zero.CachedInputTokens != 0 || *zero.ReportedContextTokens != 0 {
		t.Fatalf("new zero lost after reopen: %+v", zero)
	}
}
