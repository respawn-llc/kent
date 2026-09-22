package sqlitegen

import (
	"context"
	"testing"
)

func TestProjectSessionSummariesCountVisibleSessionsByJoinKey(t *testing.T) {
	db := openSQLiteFixture(t)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
CREATE TABLE projects (
	id TEXT PRIMARY KEY,
	display_name TEXT NOT NULL,
	project_key TEXT NOT NULL,
	primary_workspace_id TEXT,
	updated_at_unix_ms INTEGER NOT NULL
);
CREATE TABLE workspaces (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	canonical_root_path TEXT NOT NULL,
	updated_at_unix_ms INTEGER NOT NULL,
	created_at_unix_ms INTEGER NOT NULL
);
CREATE TABLE sessions (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	workspace_id TEXT NOT NULL,
	launch_visible INTEGER NOT NULL,
	updated_at_unix_ms INTEGER NOT NULL,
	locked_json TEXT NOT NULL
);
CREATE INDEX sessions_project_summary_idx
	ON sessions(project_id, updated_at_unix_ms)
	WHERE launch_visible <> 0;
CREATE INDEX sessions_workspace_summary_idx
	ON sessions(workspace_id, updated_at_unix_ms)
	WHERE launch_visible <> 0;
INSERT INTO projects VALUES ('project-1', 'Kent', 'KENT', 'workspace-1', 100);
INSERT INTO workspaces VALUES ('workspace-1', 'project-1', '/workspace/one', 110, 10);
INSERT INTO workspaces VALUES ('workspace-2', 'project-1', '/workspace/two', 120, 20);
INSERT INTO sessions VALUES ('visible-1', 'project-1', 'workspace-1', 1, 200, 'wide payload');
INSERT INTO sessions VALUES ('visible-2', 'project-1', 'workspace-1', 1, 300, 'wide payload');
INSERT INTO sessions VALUES ('hidden', 'project-1', 'workspace-1', 0, 400, 'wide payload');`); err != nil {
		t.Fatalf("create project summary fixture: %v", err)
	}

	queries := New(db)
	summaries, err := queries.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("get project summary: %v", err)
	}
	if len(summaries) != 1 || summaries[0].SessionCount != 2 || summaries[0].LatestActivityUnixMs != 300 {
		t.Fatalf("project summaries = %+v, want two visible sessions and latest activity 300", summaries)
	}
}
