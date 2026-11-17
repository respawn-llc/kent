package workflowstore

import (
	"context"
	"reflect"
	"testing"

	"core/internal/testharness/testsetup"
)

func TestTaskPRURLSet(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	url := "https://git.example/team/repo/merge_requests/42"
	updated, err := store.UpdateTask(ctx, UpdateTaskRequest{
		TaskID: task.ID, PRURLChange: &TaskPRURLChange{URL: &url},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PRURL == nil || *updated.PRURL != url {
		t.Fatalf("PR URL = %v, want %q", updated.PRURL, url)
	}
	replacement := "http://localhost:8080/repo/pull/2"
	updated, err = store.UpdateTask(ctx, UpdateTaskRequest{TaskID: task.ID, PRURLChange: &TaskPRURLChange{URL: &replacement}})
	if err != nil || updated.PRURL == nil || *updated.PRURL != replacement {
		t.Fatalf("replace = %+v, %v", updated, err)
	}
	title := "Updated title"
	updated, err = store.UpdateTask(ctx, UpdateTaskRequest{TaskID: task.ID, Title: &title})
	if err != nil || updated.PRURL == nil || *updated.PRURL != replacement {
		t.Fatalf("omitted change = %+v, %v", updated, err)
	}
	updated, err = store.UpdateTask(ctx, UpdateTaskRequest{TaskID: task.ID, PRURLChange: &TaskPRURLChange{}})
	if err != nil || updated.PRURL != nil {
		t.Fatalf("clear = %+v, %v", updated, err)
	}
}

func TestTaskPRURLPreservesSourceAndExecution(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	createLinkedValidWorkflow(t, ctx, store, binding.ProjectID)
	source := "https://git.example/repo/issues/1"
	task := createTask(t, ctx, store, CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Task", SourceURL: source})
	current := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	url := "https://git.example/repo/pull/2"
	for _, state := range []string{"active", "completed"} {
		t.Run(state, func(t *testing.T) {
			before, err := store.ListCurrentNodes(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := store.UpdateTask(ctx, UpdateTaskRequest{TaskID: task.ID, PRURLChange: &TaskPRURLChange{URL: &url}})
			if err != nil || updated.PRURL == nil || *updated.PRURL != url || updated.SourceURL != source {
				t.Fatalf("edit = %+v, %v", updated, err)
			}
			after, err := store.ListCurrentNodes(ctx, task.ID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("execution changed: before=%+v after=%+v err=%v", before, after, err)
			}
		})
		if state == "active" {
			if _, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{Source: current.Reference, TransitionID: "done"}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestTaskPRURLReopens(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	open := testsetup.StoreOpener(t, root)
	db := open()
	binding, err := db.RegisterWorkspaceBinding(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Workflow"})
	if err != nil {
		t.Fatal(err)
	}
	linkWorkflow(t, ctx, store, binding.ProjectID, created.ID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	url := "https://git.example/repo/pull/1"
	if _, err := store.UpdateTask(ctx, UpdateTaskRequest{TaskID: task.ID, PRURLChange: &TaskPRURLChange{URL: &url}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = open()
	store, err = New(db)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := store.UpdateTask(ctx, UpdateTaskRequest{TaskID: task.ID})
	if err != nil || reopened.PRURL == nil || *reopened.PRURL != url {
		t.Fatalf("reopened = %+v, %v", reopened, err)
	}
}
