package workflowstore

import (
	"errors"
	"testing"

	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestWorkflowLinkReportsMissingProject(t *testing.T) {
	ctx, store, _ := newTestStoreContext(t)
	created, err := store.CreateWorkflow(ctx, CreateWorkflowRequest{Name: "Link target"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.LinkWorkflow(ctx, "missing-project", created.ID, false)
	if !errors.Is(err, serverapi.ErrProjectNotFound) {
		t.Fatalf("link to missing Project = %v", err)
	}
}

func TestWorkflowLinkReportsMissingWorkflow(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	_, err := store.LinkWorkflow(ctx, binding.ProjectID, runtimeids.NewWorkflowID(), false)
	if !errors.Is(err, serverapi.ErrWorkflowNotFound) {
		t.Fatalf("link to missing Workflow = %v", err)
	}
}

func TestWorkflowDefaultReportsMissingEntities(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	for _, test := range []struct {
		projectID string
		want      error
	}{
		{"missing-project", serverapi.ErrProjectNotFound},
		{binding.ProjectID, serverapi.ErrWorkflowNotFound},
	} {
		_, err := store.SetDefaultProjectWorkflowLink(ctx, test.projectID, runtimeids.NewWorkflowID())
		if !errors.Is(err, test.want) {
			t.Fatalf("set default for %q = %v, want %v", test.projectID, err, test.want)
		}
	}
}
