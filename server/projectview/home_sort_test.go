package projectview

import (
	"context"
	"fmt"
	"testing"
	"time"

	"builder/server/metadata"
	"builder/server/workflowstore"
	"builder/shared/serverapi"
)

func TestMetadataServiceSortsProjectHomeByLatestTaskActivityOrEdit(t *testing.T) {
	ctx := context.Background()
	store, _, older := newProjectViewMetadataStore(t)
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}
	newer, err := svc.CreateProject(ctx, serverapi.ProjectCreateRequest{
		DisplayName:   "Newer edit",
		ProjectKey:    "NEW",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	taskActivityUnixMs := time.Now().UTC().UnixMilli() + 10_000
	workflowStore, err := workflowstore.New(store, workflowstore.WithNow(func() time.Time {
		return time.UnixMilli(taskActivityUnixMs)
	}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	workflow, err := workflowStore.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Activity Board"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := workflowStore.LinkWorkflow(ctx, older.ProjectID, workflow.ID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	if _, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: older.ProjectID, Title: "Recent task", Body: "Body"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	home, err := svc.ListProjectHome(ctx, serverapi.ProjectHomeListRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("ListProjectHome: %v", err)
	}
	if got := projectHomeIDs(home.Projects); len(got) != 2 || got[0] != older.ProjectID || got[1] != newer.Binding.ProjectID {
		t.Fatalf("project order after task activity = %+v, want [%s %s]", got, older.ProjectID, newer.Binding.ProjectID)
	}

	if _, err := store.DB().ExecContext(ctx, `UPDATE projects SET updated_at_unix_ms = ? WHERE id = ?`, taskActivityUnixMs+1, newer.Binding.ProjectID); err != nil {
		t.Fatalf("touch newer project edit time: %v", err)
	}
	home, err = svc.ListProjectHome(ctx, serverapi.ProjectHomeListRequest{PageSize: 2})
	if err != nil {
		t.Fatalf("ListProjectHome after edit: %v", err)
	}
	if got := projectHomeIDs(home.Projects); len(got) != 2 || got[0] != newer.Binding.ProjectID || got[1] != older.ProjectID {
		t.Fatalf("project order after edit = %+v, want [%s %s]", got, newer.Binding.ProjectID, older.ProjectID)
	}
}

func TestMetadataServiceSortsProjectHomeByTaskChildActivitySources(t *testing.T) {
	for _, tc := range []struct {
		name  string
		touch func(t *testing.T, ctx context.Context, fixture projectHomeActivityFixture)
	}{
		{
			name: "task_node_placements",
			touch: func(t *testing.T, ctx context.Context, fixture projectHomeActivityFixture) {
				t.Helper()
				if _, err := fixture.store.DB().ExecContext(ctx, `UPDATE task_node_placements SET updated_at_unix_ms = ? WHERE task_id = ?`, fixture.highUnixMs, string(fixture.task.ID)); err != nil {
					t.Fatalf("touch placement activity: %v", err)
				}
			},
		},
		{
			name: "task_runs",
			touch: func(t *testing.T, ctx context.Context, fixture projectHomeActivityFixture) {
				t.Helper()
				placementID, nodeID := taskPlacement(t, ctx, fixture.store, string(fixture.task.ID))
				if _, err := fixture.store.DB().ExecContext(ctx, `
INSERT INTO task_runs (
    id, task_id, placement_id, node_id, session_id, run_generation, workflow_revision_seen,
    automation_requested_at_unix_ms, created_at_unix_ms, updated_at_unix_ms, started_at_unix_ms,
    completed_at_unix_ms, interrupted_at_unix_ms, interruption_reason, interruption_detail_json,
    waiting_ask_id, final_answer_violation_count, invalid_completion_count, run_start_snapshot_json,
    metadata_json
) VALUES (?, ?, ?, ?, NULL, 0, 1, 0, ?, ?, 0, 0, 0, '', '{}', '', 0, 0, '{}', '{}')`,
					"run-home-sort", string(fixture.task.ID), placementID, nodeID, fixture.highUnixMs, fixture.highUnixMs,
				); err != nil {
					t.Fatalf("insert run activity: %v", err)
				}
			},
		},
		{
			name: "task_transitions",
			touch: func(t *testing.T, ctx context.Context, fixture projectHomeActivityFixture) {
				t.Helper()
				if _, err := fixture.store.DB().ExecContext(ctx, `
INSERT INTO task_transitions (
    id, task_id, source_run_id, source_placement_id, source_node_id, source_node_key,
    source_node_display_name, transition_group_id, transition_id, transition_display_name,
    workflow_revision_seen, actor, state, commentary, output_values_json, created_at_unix_ms,
    applied_at_unix_ms
) VALUES (?, ?, NULL, NULL, NULL, '', '', NULL, 'manual', 'Manual', 1, 'user', 'applied', '', '{}', ?, ?)`,
					"transition-home-sort", string(fixture.task.ID), fixture.highUnixMs-1, fixture.highUnixMs,
				); err != nil {
					t.Fatalf("insert transition activity: %v", err)
				}
			},
		},
		{
			name: "task_comments",
			touch: func(t *testing.T, ctx context.Context, fixture projectHomeActivityFixture) {
				t.Helper()
				if _, err := fixture.store.DB().ExecContext(ctx, `
INSERT INTO task_comments (
    id, task_id, body, author_kind, author_id, source_run_id, created_at_unix_ms,
    updated_at_unix_ms, deleted_at_unix_ms, metadata_json
) VALUES (?, ?, 'comment', 'user', 'nek', NULL, ?, ?, 0, '{}')`,
					"comment-home-sort", string(fixture.task.ID), fixture.highUnixMs, fixture.highUnixMs,
				); err != nil {
					t.Fatalf("insert comment activity: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newProjectHomeActivityFixture(t, ctx)
			assertProjectHomeOrder(t, ctx, fixture.svc, []string{fixture.newer.Binding.ProjectID, fixture.older.ProjectID})

			tc.touch(t, ctx, fixture)

			home := assertProjectHomeOrder(t, ctx, fixture.svc, []string{fixture.older.ProjectID, fixture.newer.Binding.ProjectID})
			if home.Projects[0].UpdatedAtUnixMs != fixture.highUnixMs {
				t.Fatalf("latest activity timestamp = %d, want %d", home.Projects[0].UpdatedAtUnixMs, fixture.highUnixMs)
			}
		})
	}
}

func BenchmarkMetadataServiceListProjectHomeSummaries(b *testing.B) {
	ctx := context.Background()
	store, _, first := newProjectViewMetadataStore(b)
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		b.Fatalf("NewMetadataService: %v", err)
	}
	workflowStore, err := workflowstore.New(store, workflowstore.WithNow(func() time.Time {
		return time.UnixMilli(1)
	}))
	if err != nil {
		b.Fatalf("workflowstore.New: %v", err)
	}
	workflow, err := workflowStore.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Profile Board"})
	if err != nil {
		b.Fatalf("CreateWorkflow: %v", err)
	}
	projectIDs := []string{first.ProjectID}
	for index := 1; index < 250; index++ {
		created, err := svc.CreateProject(ctx, serverapi.ProjectCreateRequest{
			DisplayName:   fmt.Sprintf("Project %03d", index),
			ProjectKey:    fmt.Sprintf("P%03d", index),
			WorkspaceRoot: b.TempDir(),
		})
		if err != nil {
			b.Fatalf("CreateProject %d: %v", index, err)
		}
		projectIDs = append(projectIDs, created.Binding.ProjectID)
	}
	for projectIndex, projectID := range projectIDs {
		if _, err := workflowStore.LinkWorkflow(ctx, projectID, workflow.ID, true); err != nil {
			b.Fatalf("LinkWorkflow %d: %v", projectIndex, err)
		}
		for taskIndex := 0; taskIndex < 3; taskIndex++ {
			task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: projectID, Title: fmt.Sprintf("Task %d", taskIndex), Body: "Body"})
			if err != nil {
				b.Fatalf("CreateTask %d/%d: %v", projectIndex, taskIndex, err)
			}
			if _, err := workflowStore.AddComment(ctx, task.ID, "Comment", "user", "bench"); err != nil {
				b.Fatalf("AddComment %d/%d: %v", projectIndex, taskIndex, err)
			}
			placementID, nodeID := taskPlacement(b, ctx, store, string(task.ID))
			timestamp := int64(projectIndex*10 + taskIndex + 1)
			if _, err := store.DB().ExecContext(ctx, `
INSERT INTO task_runs (
    id, task_id, placement_id, node_id, session_id, run_generation, workflow_revision_seen,
    automation_requested_at_unix_ms, created_at_unix_ms, updated_at_unix_ms, started_at_unix_ms,
    completed_at_unix_ms, interrupted_at_unix_ms, interruption_reason, interruption_detail_json,
    waiting_ask_id, final_answer_violation_count, invalid_completion_count, run_start_snapshot_json,
    metadata_json
) VALUES (?, ?, ?, ?, NULL, 0, 1, 0, ?, ?, 0, 0, 0, '', '{}', '', 0, 0, '{}', '{}')`,
				fmt.Sprintf("bench-run-%d-%d", projectIndex, taskIndex), string(task.ID), placementID, nodeID, timestamp, timestamp,
			); err != nil {
				b.Fatalf("insert run %d/%d: %v", projectIndex, taskIndex, err)
			}
			if _, err := store.DB().ExecContext(ctx, `
INSERT INTO task_transitions (
    id, task_id, source_run_id, source_placement_id, source_node_id, source_node_key,
    source_node_display_name, transition_group_id, transition_id, transition_display_name,
    workflow_revision_seen, actor, state, commentary, output_values_json, created_at_unix_ms,
    applied_at_unix_ms
) VALUES (?, ?, NULL, NULL, NULL, '', '', NULL, 'manual', 'Manual', 1, 'user', 'applied', '', '{}', ?, ?)`,
				fmt.Sprintf("bench-transition-%d-%d", projectIndex, taskIndex), string(task.ID), timestamp, timestamp,
			); err != nil {
				b.Fatalf("insert transition %d/%d: %v", projectIndex, taskIndex, err)
			}
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := svc.ListProjectHome(ctx, serverapi.ProjectHomeListRequest{PageSize: 40}); err != nil {
			b.Fatalf("ListProjectHome: %v", err)
		}
	}
}

type projectHomeActivityFixture struct {
	store      *metadata.Store
	svc        *Service
	older      metadata.Binding
	newer      serverapi.ProjectCreateResponse
	task       workflowstore.TaskRecord
	highUnixMs int64
}

func newProjectHomeActivityFixture(t *testing.T, ctx context.Context) projectHomeActivityFixture {
	t.Helper()
	store, _, older := newProjectViewMetadataStore(t)
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}
	newer, err := svc.CreateProject(ctx, serverapi.ProjectCreateRequest{
		DisplayName:   "Newer project",
		ProjectKey:    "NEW",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	workflowStore, err := workflowstore.New(store, workflowstore.WithNow(func() time.Time {
		return time.UnixMilli(1)
	}))
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	workflow, err := workflowStore.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: "Activity Board"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := workflowStore.LinkWorkflow(ctx, older.ProjectID, workflow.ID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}
	task, err := workflowStore.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: older.ProjectID, Title: "Low task", Body: "Body"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return projectHomeActivityFixture{
		store:      store,
		svc:        svc,
		older:      older,
		newer:      newer,
		task:       task,
		highUnixMs: time.Now().UTC().UnixMilli() + 10_000,
	}
}

func assertProjectHomeOrder(t testing.TB, ctx context.Context, svc *Service, want []string) serverapi.ProjectHomeListResponse {
	t.Helper()
	home, err := svc.ListProjectHome(ctx, serverapi.ProjectHomeListRequest{PageSize: len(want)})
	if err != nil {
		t.Fatalf("ListProjectHome: %v", err)
	}
	got := projectHomeIDs(home.Projects)
	if len(got) != len(want) {
		t.Fatalf("project count = %d, want %d: %+v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("project order = %+v, want %+v", got, want)
		}
	}
	return home
}

func taskPlacement(t testing.TB, ctx context.Context, store *metadata.Store, taskID string) (string, string) {
	t.Helper()
	var placementID string
	var nodeID string
	if err := store.DB().QueryRowContext(ctx, `SELECT id, node_id FROM task_node_placements WHERE task_id = ? LIMIT 1`, taskID).Scan(&placementID, &nodeID); err != nil {
		t.Fatalf("get task placement: %v", err)
	}
	return placementID, nodeID
}

func projectHomeIDs(projects []serverapi.ProjectHomeSummary) []string {
	out := make([]string, 0, len(projects))
	for _, project := range projects {
		out = append(out, project.ProjectID)
	}
	return out
}
