package workflowsvc

import (
	"context"

	"core/server/workflow"
	"core/server/workflow/label"
	"core/server/workflowstore"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/workflowcontract"
)

func (s *Service) CreateWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelCreateRequest) (*pb.ProjectLabelCreateSuccess, error) {
	record, err := s.store.CreateProjectLabel(ctx, req.ProjectId, req.Name)
	if err != nil {
		return nil, err
	}
	response := &pb.ProjectLabelCreateSuccess{Label: workflowProjectLabel(record)}
	s.publishProjectEvent(ctx, req.ProjectId, workflowcontract.EventResourceLabel, workflowcontract.EventActionCreated, response.Label.Id)
	return response, nil
}

func (s *Service) ListWorkflowProjectLabels(ctx context.Context, req *pb.ProjectLabelCatalogRequest) (*pb.ProjectLabelCatalogSuccess, error) {
	records, err := s.store.ListProjectLabels(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	labels := make([]*pb.ProjectLabel, 0, len(records))
	for _, record := range records {
		labels = append(labels, workflowProjectLabel(record))
	}
	return &pb.ProjectLabelCatalogSuccess{
		Catalog: &pb.ProjectLabelCatalog{
			ProjectId: req.ProjectId,
			Labels:    labels,
		},
	}, nil
}

func (s *Service) RenameWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelRenameRequest) (*pb.ProjectLabelRenameSuccess, error) {
	id, err := label.IDFromProto(req.LabelId)
	if err != nil {
		return nil, err
	}
	record, err := s.store.RenameProjectLabel(ctx, req.ProjectId, id, req.Name)
	if err != nil {
		return nil, err
	}
	response := &pb.ProjectLabelRenameSuccess{Label: workflowProjectLabel(record)}
	s.publishProjectEvent(ctx, req.ProjectId, workflowcontract.EventResourceLabel, workflowcontract.EventActionRenamed, response.Label.Id)
	return response, nil
}

func (s *Service) DeleteWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelDeleteRequest) (*pb.ProjectLabelDeleteSuccess, error) {
	id, err := label.IDFromProto(req.LabelId)
	if err != nil {
		return nil, err
	}
	record, err := s.store.DeleteProjectLabel(ctx, req.ProjectId, id)
	if err != nil {
		return nil, err
	}
	response := &pb.ProjectLabelDeleteSuccess{LabelId: record.ID.String()}
	s.publishProjectEvent(ctx, req.ProjectId, workflowcontract.EventResourceLabel, workflowcontract.EventActionDeleted, response.LabelId)
	return response, nil
}

func (s *Service) ReorderWorkflowProjectLabels(ctx context.Context, req *pb.ProjectLabelReorderRequest) (*pb.ProjectLabelReorderSuccess, error) {
	orderedIDs := make([]label.ID, 0, len(req.LabelIds))
	for _, rawID := range req.LabelIds {
		id, err := label.IDFromProto(rawID)
		if err != nil {
			return nil, err
		}
		orderedIDs = append(orderedIDs, id)
	}
	result, err := s.store.ReorderProjectLabels(ctx, req.ProjectId, orderedIDs)
	if err != nil {
		return nil, err
	}
	labels := make([]*pb.ProjectLabel, 0, len(result.Labels))
	for _, record := range result.Labels {
		labels = append(labels, workflowProjectLabel(record))
	}
	response := &pb.ProjectLabelReorderSuccess{
		Catalog: &pb.ProjectLabelCatalog{
			ProjectId: req.ProjectId,
			Labels:    labels,
		},
	}
	if result.Changed {
		s.publishProjectEvent(
			ctx,
			req.ProjectId,
			workflowcontract.EventResourceLabel,
			workflowcontract.EventActionReordered,
			req.ProjectId,
		)
	}
	return response, nil
}

func (s *Service) GetWorkflowTaskLabels(ctx context.Context, req *taskpb.LabelsGetRequest) (*taskpb.LabelsGetSuccess, error) {
	ids, err := s.store.GetTaskLabelIDs(ctx, workflow.TaskID(req.TaskId))
	if err != nil {
		return nil, err
	}
	return &taskpb.LabelsGetSuccess{
		Assignment: &taskpb.AssignedLabelIds{
			TaskId:   req.TaskId,
			LabelIds: labelIDs(ids),
		},
	}, nil
}

func (s *Service) UpdateWorkflowTaskLabels(ctx context.Context, req *taskpb.LabelsUpdateRequest) (*taskpb.LabelsUpdateSuccess, error) {
	scope, err := s.store.GetTaskLabelScope(ctx, workflow.TaskID(req.TaskId))
	if err != nil {
		return nil, err
	}
	ids, err := s.store.UpdateTaskLabels(ctx, workflowstore.TaskLabelUpdateRequest{
		TaskID:         workflow.TaskID(req.TaskId),
		AddLabelIDs:    req.AddLabelIds,
		RemoveLabelIDs: req.RemoveLabelIds,
	})
	if err != nil {
		return nil, err
	}
	response := &taskpb.LabelsUpdateSuccess{
		Assignment: &taskpb.AssignedLabelIds{
			TaskId:   req.TaskId,
			LabelIds: labelIDs(ids),
		},
	}
	s.publishProjectWorkflowEvent(ctx, scope.ProjectID, scope.WorkflowID, workflowcontract.EventResourceTask, workflowcontract.EventActionLabelsChanged, req.TaskId)
	return response, nil
}

func workflowProjectLabel(record workflowstore.ProjectLabelRecord) *pb.ProjectLabel {
	return &pb.ProjectLabel{
		Id:   record.ID.String(),
		Name: record.Name.String(),
	}
}

func (s *Service) publishProjectEvent(ctx context.Context, projectID string, resource workflowcontract.EventResource, action workflowcontract.EventAction, primaryEntityID string, relatedIDs ...string) {
	s.publishWorkflowEvent(ctx, workflowcontract.Event{
		ProjectID:       &projectID,
		Resource:        resource,
		Action:          action,
		PrimaryEntityID: primaryEntityID,
		RelatedIDs:      relatedIDs,
	})
}

func labelIDs(ids []label.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
