package client

import (
	"context"
	"testing"

	"core/shared/serverapi"
	"core/shared/servicecontract"
)

type fakeWorkflowService struct {
	servicecontract.WorkflowService
	created serverapi.WorkflowCreateRequest
}

func (s *fakeWorkflowService) CreateWorkflow(ctx context.Context, req serverapi.WorkflowCreateRequest) (serverapi.WorkflowCreateResponse, error) {
	s.created = req
	return serverapi.WorkflowCreateResponse{Workflow: serverapi.WorkflowRecord{ID: "workflow-1", Name: req.Name}}, nil
}

func TestLoopbackWorkflowClientCallsService(t *testing.T) {
	service := &fakeWorkflowService{}
	client := NewLoopbackWorkflowClient(service)
	resp, err := client.CreateWorkflow(context.Background(), serverapi.WorkflowCreateRequest{Name: "Workflow"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if resp.Workflow.ID != "workflow-1" || service.created.Name != "Workflow" {
		t.Fatalf("response=%+v service=%+v", resp, service.created)
	}
}
