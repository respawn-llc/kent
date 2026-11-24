package workflowsvc

import (
	"context"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
	"errors"
	"testing"
)

type failingCreationDetail struct {
	WorkflowTaskDetailReadModel
	cause error
}

func (d failingCreationDetail) GetTask(context.Context, string) (*taskpb.TaskDetail, error) {
	return nil, d.cause
}

func TestCreationFailureCarriesCommittedIdentity(t *testing.T) {
	ctx, service, binding := newWorkflowServiceTestContext(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	cause := errors.New("projection failed")
	detail := service.readModels.TaskDetail
	service.readModels.TaskDetail = failingCreationDetail{detail, cause}
	_, err := service.CreateWorkflowTask(ctx, &taskpb.CreateRequest{ProjectId: binding.ProjectID, Title: "Committed"})
	var committed *serverapi.CommittedTaskCreationError
	if !errors.As(err, &committed) || !errors.Is(err, cause) {
		t.Fatalf("missing committed identity: %v", err)
	}
	if committed.ProjectID != binding.ProjectID {
		t.Fatalf("%+v", committed)
	}
	task, err := detail.GetTask(ctx, committed.TaskID)
	if err != nil || task.Summary.ShortId != committed.ShortID {
		t.Fatalf("%+v %v", task, err)
	}
}
