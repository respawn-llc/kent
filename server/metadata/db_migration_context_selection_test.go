package metadata

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestContextSelectionMigrationRestrictsAmbiguousBranches(t *testing.T) {
	for _, tc := range []struct {
		name, state                 string
		branch, session, restricted bool
	}{
		{"ready", "ready", true, true, true},
		{"admitted_healthy_clone", "admitted", true, true, true},
		{"interrupted", "interrupted", true, true, true},
		{"no_session", "ready", true, false, false},
		{"serial", "ready", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			db, err := openDatabaseAtVersionForTest(t, root, filepath.Join(root, "db", "main.sqlite3"), 93)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UnixMilli()
			execSeed(t, db, "project", `INSERT INTO projects (id,display_name,created_at_unix_ms,updated_at_unix_ms,metadata_json) VALUES ('project-context','Project',?,?,'{}')`, now, now)
			seedWorkflowGraph(t, db, "project-context", now)
			execSeed(t, db, "task", workflowSeedTaskSQL, "task-context", "link-1", 1, "Context", now, now)
			var branch, session, reason, detail, interrupted any
			if tc.branch {
				insertTaskActiveFanout(t, db, "task-context")
				insertTaskActiveFanoutBranch(t, db, "task-context", "branch")
				insertTaskActiveFanoutBranch(t, db, "task-context", "sibling")
				execSeed(t, db, "unambiguous sibling", `INSERT INTO task_current_nodes (task_id,node_id,transition_branch_key,scheduling_state) VALUES ('task-context',?,'sibling','ready')`, workflowGraphSeedID(t, db, "node-agent"))
				branch = "branch"
			}
			if tc.session {
				session = "550e8400-e29b-41d4-a716-446655440091"
				execSeed(t, db, "session", `INSERT INTO sessions (id,project_id,artifact_relpath,category,created_at_unix_ms,updated_at_unix_ms,metadata_json) VALUES (?,'project-context','preserved/chat','main',?,?,'{}')`, session, now, now)
			}
			if tc.state == "interrupted" {
				reason, detail, interrupted = "setup_failed", `{"Code":"setup_failed","Fields":{"error":"preserve diagnostics"}}`, int64(1000)
			}
			execSeed(t, db, "current", `INSERT INTO task_current_nodes (task_id,node_id,transition_branch_key,session_id,scheduling_state,interruption_reason,interruption_detail_json,interrupted_at_unix_ms) VALUES ('task-context',?,?,?,?,?,?,?)`,
				workflowGraphSeedID(t, db, "node-agent"), branch, session, tc.state, reason, detail, interrupted)
			execSeed(t, db, "completed task", workflowSeedTaskSQL, "task-completed", "link-1", 2, "Completed", now, now)
			execSeed(t, db, "terminal", `INSERT INTO task_current_nodes (task_id,node_id) VALUES ('task-completed',?)`, workflowGraphSeedID(t, db, "node-done"))
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := Open(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			var state string
			var gotReason, gotDetail, gotSession *string
			if err := store.DB().QueryRowContext(t.Context(), `SELECT scheduling_state,interruption_reason,interruption_detail_json,session_id FROM task_current_nodes WHERE task_id='task-context' AND transition_branch_key IS ?`, branch).Scan(&state, &gotReason, &gotDetail, &gotSession); err != nil {
				t.Fatal(err)
			}
			if (gotSession != nil) != tc.session {
				t.Fatal("Session binding changed")
			}
			if gotSession != nil && *gotSession != session {
				t.Fatal("Session identity changed")
			}
			if tc.restricted {
				if state != "interrupted" || gotReason == nil || *gotReason != "context_selection_required" {
					t.Fatalf("restriction missing: %s %v", state, gotReason)
				}
				var diagnostic struct{ Fields map[string]string }
				if err := json.Unmarshal([]byte(*gotDetail), &diagnostic); err != nil {
					t.Fatal(err)
				}
				if diagnostic.Fields["previous_scheduling_state"] != tc.state {
					t.Fatal("lost original scheduling")
				}
				if detail != nil && diagnostic.Fields["previous_interruption_detail"] != detail {
					t.Fatal("lost original diagnostics")
				}
				if tc.state == "interrupted" {
					if diagnostic.Fields["previous_interruption_reason"] != reason || diagnostic.Fields["previous_interrupted_at_unix_ms"] != "1000" {
						t.Fatal("lost original interruption reason or time")
					}
				} else {
					for _, absent := range []string{"previous_interruption_reason", "previous_interruption_detail", "previous_interrupted_at_unix_ms"} {
						if _, present := diagnostic.Fields[absent]; present {
							t.Fatalf("invented absent diagnostic %s", absent)
						}
					}
				}
			} else if state != tc.state {
				t.Fatalf("unrelated position changed: %s", state)
			}
			if tc.branch {
				var siblingState string
				if err := store.DB().QueryRowContext(t.Context(), `SELECT scheduling_state FROM task_current_nodes WHERE task_id='task-context' AND transition_branch_key='sibling'`).Scan(&siblingState); err != nil {
					t.Fatal(err)
				}
				want := "ready"
				if tc.restricted {
					want = "interrupted"
				}
				if siblingState != want {
					t.Fatalf("sibling state = %s, want %s", siblingState, want)
				}
			}
			var terminalState *string
			if err := store.DB().QueryRowContext(t.Context(), `SELECT scheduling_state FROM task_current_nodes WHERE task_id='task-completed'`).Scan(&terminalState); err != nil {
				t.Fatal(err)
			}
			if terminalState != nil {
				t.Fatal("completed Task changed")
			}
			if tc.session {
				var path string
				if err := store.DB().QueryRowContext(t.Context(), `SELECT artifact_relpath FROM sessions WHERE id=?`, session).Scan(&path); err != nil {
					t.Fatal(err)
				}
				if path != "preserved/chat" {
					t.Fatal("Session artifact changed")
				}
			}
		})
	}
}
