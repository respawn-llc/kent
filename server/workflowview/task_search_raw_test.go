package workflowview

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"core/server/metadata"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

func TestTaskSearchRawFTS5UsesMarkerFreeKnownColumnSnippets(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	task := createTaskSearchTask(
		t,
		fixture,
		"Different title",
		strings.Repeat("prefix ", 40)+"needle "+strings.Repeat("suffix ", 40),
	)
	response, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:    "body:needle",
		Context:  2,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("raw Search: %v", err)
	}
	if len(response.Groups) != 1 || response.Groups[0].TaskId != string(task.ID) {
		t.Fatalf("raw search response = %+v", response)
	}
	hits := response.Groups[0].Hits
	if len(hits) != 1 || hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY || hits[0].GetFts5() == nil || hits[0].GetFts5().Snippet != "eedl" {
		t.Fatalf("raw search hits = %+v", hits)
	}
}

func TestTaskSearchRawFTS5PreservesSourceLocalExpressionSemantics(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	task := createTaskSearchTask(t, fixture, "needle title", "needle body")
	if _, err := fixture.store.AddComment(fixture.ctx, task.ID, "needle comment", "user", "user-1"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	createTaskSearchTask(t, fixture, "alphaone", "betatwo")
	createTaskSearchTask(t, fixture, "ab", "a")

	response, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:            taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:           "needle",
		Context:         serverapi.TaskSearchDefaultContext,
		IncludeComments: true,
		PageSize:        serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("raw Search: %v", err)
	}
	if len(response.Groups) != 1 || response.Groups[0].TaskId != string(task.ID) {
		t.Fatalf("raw response = %+v", response)
	}
	hits := response.Groups[0].Hits
	if len(hits) != 3 ||
		hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_TITLE ||
		hits[1].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY ||
		hits[2].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_COMMENT {
		t.Fatalf("raw source order = %+v, want title/body/comment", hits)
	}

	withoutComments, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:    "comment:needle",
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("raw Comment-only Search without inclusion: %v", err)
	}
	if len(withoutComments.Groups) != 0 {
		t.Fatalf("raw Comment-only Search without inclusion = %+v, want no matches", withoutComments)
	}

	splitTerms, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:    "alphaone betatwo",
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("raw split-term Search: %v", err)
	}
	if len(splitTerms.Groups) != 0 {
		t.Fatalf("raw split-term Search = %+v, want no matches", splitTerms)
	}

	for _, test := range []struct {
		name            string
		query           string
		includeComments bool
		wantKind        taskpb.SearchSourceKind
	}{
		{name: "title column", query: "title:needle", wantKind: taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_TITLE},
		{name: "body column", query: "body:needle", wantKind: taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY},
		{name: "comment column", query: "comment:needle", includeComments: true, wantKind: taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_COMMENT},
		{name: "body phrase", query: `body:"needle body"`, wantKind: taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY},
	} {
		t.Run(test.name, func(t *testing.T) {
			filtered, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
				Mode:            taskpb.SearchMode_SEARCH_MODE_FTS5,
				Query:           test.query,
				Context:         serverapi.TaskSearchDefaultContext,
				IncludeComments: test.includeComments,
				PageSize:        serverapi.TaskSearchDefaultPageSize,
			})
			if err != nil {
				t.Fatalf("raw Search: %v", err)
			}
			if len(filtered.Groups) != 1 ||
				filtered.Groups[0].TaskId != string(task.ID) ||
				len(filtered.Groups[0].Hits) != 1 ||
				filtered.Groups[0].Hits[0].Source.Kind != test.wantKind {
				t.Fatalf("raw %s response = %+v", test.name, filtered)
			}
		})
	}

	boolean, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:            taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:           "title:needle OR comment:needle",
		Context:         serverapi.TaskSearchDefaultContext,
		IncludeComments: true,
		PageSize:        serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("raw boolean Search: %v", err)
	}
	if len(boolean.Groups) != 1 ||
		len(boolean.Groups[0].Hits) != 2 ||
		boolean.Groups[0].Hits[0].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_TITLE ||
		boolean.Groups[0].Hits[1].Source.Kind != taskpb.SearchSourceKind_SEARCH_SOURCE_KIND_COMMENT {
		t.Fatalf("raw boolean response = %+v, want title then Comment", boolean)
	}

	for _, rawTerm := range []string{"a", "ab"} {
		if _, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
			Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
			Query:    rawTerm,
			Context:  serverapi.TaskSearchDefaultContext,
			PageSize: serverapi.TaskSearchDefaultPageSize,
		}); err != nil {
			t.Fatalf("short raw term %q was rejected: %v", rawTerm, err)
		}
	}
}

func TestTaskSearchRawFTS5ExcludesShortIDs(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	createTaskSearchTaskAtSequence(t, fixture, 345, "Exact identifier", "ordinary body")

	response, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:    "345",
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("raw Search for Short ID text: %v", err)
	}
	if len(response.Groups) != 0 {
		t.Fatalf("raw Short ID Search = %+v, want no Short ID source", response)
	}
}

func TestTaskSearchRawSchemaFailuresRemainOperational(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*metadata.Store) error
	}{
		{
			name: "missing FTS",
			mutate: func(store *metadata.Store) error {
				_, err := store.DB().Exec("DROP TABLE task_search_fts")
				return err
			},
		},
		{
			name: "ordinary same-name table",
			mutate: func(store *metadata.Store) error {
				if _, err := store.DB().Exec("DROP TABLE task_search_fts"); err != nil {
					return err
				}
				_, err := store.DB().Exec("CREATE TABLE task_search_fts (title TEXT, body TEXT, comment TEXT)")
				return err
			},
		},
		{
			name: "incomplete content view",
			mutate: func(store *metadata.Store) error {
				if _, err := store.DB().Exec("DROP VIEW task_search_content"); err != nil {
					return err
				}
				_, err := store.DB().Exec("CREATE VIEW task_search_content AS SELECT document_id, NULL AS title, NULL AS body FROM task_search_documents")
				return err
			},
		},
		{
			name: "nonpartial mapping index",
			mutate: func(store *metadata.Store) error {
				if _, err := store.DB().Exec("DROP INDEX task_search_documents_task_title_unique"); err != nil {
					return err
				}
				_, err := store.DB().Exec("CREATE INDEX task_search_documents_task_title_unique ON task_search_documents(task_id)")
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, search := newTaskSearchFixture(t, false)
			createTaskSearchTask(t, fixture, "Schema contract", "needle")
			if err := test.mutate(fixture.metadata); err != nil {
				t.Fatalf("mutate schema: %v", err)
			}
			_, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
				Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
				Query:    `"`,
				Context:  serverapi.TaskSearchDefaultContext,
				PageSize: serverapi.TaskSearchDefaultPageSize,
			})
			var searchErr *serverapi.TaskSearchError
			if err == nil || errors.As(err, &searchErr) {
				t.Fatalf("schema failure error = %T %v", err, err)
			}
		})
	}
}

func TestTaskSearchRawFTS5SQLiteErrorsRemainOperational(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	_, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:     taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:    `"`,
		Context:  serverapi.TaskSearchDefaultContext,
		PageSize: serverapi.TaskSearchDefaultPageSize,
	})
	var searchErr *serverapi.TaskSearchError
	if err == nil || errors.As(err, &searchErr) {
		t.Fatalf("raw FTS5 SQLite error = %T %v, want a generic operational error", err, err)
	}
}

func TestTaskSearchSearchDoesNotPreflightEntirePersistenceCorpus(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	healthy := createTaskSearchTask(t, fixture, "Healthy", "healthy needle")
	if _, err := fixture.metadata.DB().Exec(`DROP TRIGGER task_search_task_insert`); err != nil {
		t.Fatalf("drop Task insert trigger: %v", err)
	}
	if _, err := fixture.metadata.DB().Exec(`
CREATE TRIGGER task_search_task_insert
AFTER INSERT ON tasks
BEGIN
    SELECT 1;
END`); err != nil {
		t.Fatalf("create no-op Task insert trigger: %v", err)
	}
	for index := range 64 {
		createTaskSearchTask(t, fixture, fmt.Sprintf("Drift %d", index), "unrelated")
	}
	response, err := search.Search(fixture.ctx, taskSearchRequest("healthy needle"))
	if err != nil {
		t.Fatalf("Search across persistence drift: %v", err)
	}
	if len(response.Groups) != 1 || response.Groups[0].TaskId != string(healthy.ID) {
		t.Fatalf("Search across persistence drift = %+v, want the valid indexed Task", response)
	}
}

func TestTaskSearchRanksEquivalentBodyBeforeComment(t *testing.T) {
	fixture, search := newTaskSearchFixture(t, false)
	bodyTask := createTaskSearchTask(t, fixture, "Body", "needle")
	commentTask := createTaskSearchTask(t, fixture, "Comment", "other")
	if _, err := fixture.store.AddComment(fixture.ctx, commentTask.ID, "needle", "user", "user-1"); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	response, err := search.Search(fixture.ctx, &taskpb.SearchRequest{
		Mode:            taskpb.SearchMode_SEARCH_MODE_FTS5,
		Query:           "needle",
		Context:         serverapi.TaskSearchDefaultContext,
		IncludeComments: true,
		PageSize:        serverapi.TaskSearchDefaultPageSize,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Groups) != 2 ||
		response.Groups[0].TaskId != string(bodyTask.ID) ||
		response.Groups[1].TaskId != string(commentTask.ID) {
		t.Fatalf("ranked groups = %+v", response.Groups)
	}
}
