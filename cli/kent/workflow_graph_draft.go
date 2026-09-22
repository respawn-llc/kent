package main

import (
	"context"
	"errors"

	"core/shared/apicontract"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
)

type workflowGraphPreview struct {
	Graph    *pb.GraphDraft
	Response *pb.GraphSavePreviewSuccess
}

func previewWorkflowGraphDraft(
	ctx context.Context,
	remote apicontract.WorkflowService,
	current *pb.WorkflowDefinition,
	submitted *pb.GraphDraft,
) (workflowGraphPreview, error) {
	if remote == nil {
		return workflowGraphPreview{}, errors.New("workflow service is required")
	}
	response, err := remote.PreviewWorkflowGraphSave(ctx, &pb.GraphSavePreviewRequest{
		WorkflowId:      current.Workflow.Id,
		ExpectedVersion: current.Workflow.Version,
		Graph:           submitted,
	})
	if err != nil {
		return workflowGraphPreview{}, err
	}
	return workflowGraphPreview{Graph: submitted, Response: response}, nil
}
