package projectview

import (
	"context"
	"errors"
	"testing"

	"core/server/operation"
	"core/server/workflow"
	"core/server/workflowstore"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestProjectMutationsRejectAfterServerShutdownButReadsRemainAvailable(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	svc := newProjectViewMetadataService(t, store)
	if err := svc.operations.Close(); err != nil {
		t.Fatal(err)
	}
	workspace := generatedProjectWorkspaceSelector(serverapi.NewProjectWorkspaceSelectorForRoot(binding.CanonicalRoot))
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{"create", func() error {
			_, err := svc.CreateProject(t.Context(), &projectpb.CreateProjectRequest{WorkspaceRoot: t.TempDir(), DisplayName: "New", ProjectKey: new("NEW")})
			return err
		}},
		{"update", func() error {
			_, err := svc.UpdateProject(t.Context(), &projectpb.UpdateProjectRequest{ProjectId: binding.ProjectID, DisplayName: "Changed"})
			return err
		}},
		{"set default workspace", func() error {
			_, err := svc.SetDefaultWorkspace(t.Context(), &projectpb.SetDefaultWorkspaceRequest{ProjectId: binding.ProjectID, Workspace: workspace})
			return err
		}},
		{"attach workspace", func() error {
			_, err := svc.AttachWorkspaceToProject(t.Context(), &projectpb.AttachWorkspaceRequest{ProjectId: binding.ProjectID, WorkspaceRoot: t.TempDir()})
			return err
		}},
		{"rebind workspace", func() error {
			_, err := svc.RebindWorkspace(t.Context(), &projectpb.RebindWorkspaceRequest{OldWorkspaceRoot: binding.CanonicalRoot, NewWorkspaceRoot: t.TempDir()})
			return err
		}},
		{"unlink workspace", func() error {
			_, err := svc.UnlinkWorkspaceFromProject(t.Context(), &projectpb.UnlinkWorkspaceRequest{ProjectId: binding.ProjectID, Workspace: workspace})
			return err
		}},
		{"delete", func() error {
			_, err := svc.DeleteProject(t.Context(), &projectpb.DeleteProjectRequest{ProjectId: binding.ProjectID})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, operation.ErrOperationOwnerClosed) {
				t.Fatalf("mutation during shutdown = %v", err)
			}
		})
	}
	projects, err := svc.ListProjects(t.Context(), &emptypb.Empty{})
	if err != nil || len(projects.GetProjects()) != 1 {
		t.Fatalf("project read after operation shutdown: result=%v error=%v", projects, err)
	}
	caller, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := svc.GetProjectEdit(caller, &projectpb.ProjectEditGetRequest{ProjectId: binding.ProjectID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled project read = %v", err)
	}
}

type cancelingProjectExecution struct {
	cancel context.CancelFunc
	called bool
}

func (e *cancelingProjectExecution) EnsureTaskQuiescent(workflow.TaskID) error {
	e.called = true
	e.cancel()
	return nil
}

func TestDeleteProjectCallerCancellationDoesNotAbortAcceptedWork(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	svc := newProjectViewMetadataService(t, store)
	wf, err := svc.workflowStore.CreateWorkflow(t.Context(), workflowstore.CreateWorkflowRequest{Name: "Delete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.workflowStore.LinkWorkflowWithDefaultPolicy(t.Context(), binding.ProjectID, wf.ID, workflowstore.WorkflowLinkDefaultAlways); err != nil {
		t.Fatal(err)
	}
	task, err := svc.workflowStore.CreateTask(t.Context(), workflowstore.CreateTaskRequest{ProjectID: binding.ProjectID, Title: "Delete"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	execution := &cancelingProjectExecution{cancel: cancel}
	svc.workflowExecution = execution
	result, err := svc.DeleteProject(ctx, &projectpb.DeleteProjectRequest{ProjectId: binding.ProjectID})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err == nil && !result.Deleted {
		t.Fatalf("delete result = %v", result)
	}
	// Deletion owns the Task mutation boundary when it cancels the caller.
	// Acquiring that same boundary waits for deletion, without canceling its work.
	if err := svc.taskMutations.RunMany(t.Context(), []workflow.TaskID{task.ID}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !execution.called {
		t.Fatal("delete did not reach execution quiescence")
	}
	projects, err := svc.ListProjects(t.Context(), &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != 0 {
		t.Fatal("caller cancellation aborted accepted project deletion")
	}
}
