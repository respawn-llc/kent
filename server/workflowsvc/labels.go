package workflowsvc

import (
	"context"
	"errors"

	"core/server/workflow"
	"core/server/workflow/label"
	"core/server/workflowstore"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

func (s *Service) CreateWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelCreateRequest) (*pb.ProjectLabelCreateSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	record, err := s.store.CreateProjectLabel(ctx, req.ProjectId, req.Name)
	if err != nil {
		return nil, err
	}
	response := &pb.ProjectLabelCreateSuccess{Label: workflowProjectLabel(record)}
	s.publishProjectEvent(ctx, req.ProjectId, serverapi.WorkflowProjectEventResourceLabel, serverapi.WorkflowProjectEventActionCreated, response.Label.Id)
	return response, nil
}

func (s *Service) ListWorkflowProjectLabels(ctx context.Context, req *pb.ProjectLabelCatalogRequest) (*pb.ProjectLabelCatalogSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
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
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := label.ParseID(req.LabelId)
	if err != nil {
		return nil, err
	}
	record, err := s.store.RenameProjectLabel(ctx, req.ProjectId, id, req.Name)
	if err != nil {
		return nil, err
	}
	response := &pb.ProjectLabelRenameSuccess{Label: workflowProjectLabel(record)}
	s.publishProjectEvent(ctx, req.ProjectId, serverapi.WorkflowProjectEventResourceLabel, serverapi.WorkflowProjectEventActionRenamed, response.Label.Id)
	return response, nil
}

func (s *Service) DeleteWorkflowProjectLabel(ctx context.Context, req *pb.ProjectLabelDeleteRequest) (*pb.ProjectLabelDeleteSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := label.ParseID(req.LabelId)
	if err != nil {
		return nil, err
	}
	record, err := s.store.DeleteProjectLabel(ctx, req.ProjectId, id)
	if err != nil {
		return nil, err
	}
	response := &pb.ProjectLabelDeleteSuccess{LabelId: record.ID.String()}
	s.publishProjectEvent(ctx, req.ProjectId, serverapi.WorkflowProjectEventResourceLabel, serverapi.WorkflowProjectEventActionDeleted, response.LabelId)
	return response, nil
}

func (s *Service) ReorderWorkflowProjectLabels(ctx context.Context, req *pb.ProjectLabelReorderRequest) (*pb.ProjectLabelReorderSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	orderedIDs := make([]label.ID, 0, len(req.LabelIds))
	for _, rawID := range req.LabelIds {
		id, err := label.ParseID(rawID)
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
			serverapi.WorkflowProjectEventResourceLabel,
			serverapi.WorkflowProjectEventActionReordered,
			req.ProjectId,
		)
	}
	return response, nil
}

func (s *Service) GetWorkflowTaskLabels(ctx context.Context, req *taskpb.LabelsGetRequest) (*taskpb.LabelsGetSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
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
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
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
	s.publishProjectWorkflowEvent(ctx, scope.ProjectID, scope.WorkflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionLabelsChanged, req.TaskId)
	return response, nil
}

func workflowProjectLabel(record workflowstore.ProjectLabelRecord) *pb.ProjectLabel {
	return &pb.ProjectLabel{
		Id:   record.ID.String(),
		Name: record.Name.String(),
	}
}

type workflowLabelErrorScope struct {
	projectID *string
	taskID    *string
}

func workflowLabelError(err error, scope workflowLabelErrorScope) error {
	var nameErr *label.NameError
	if errors.As(err, &nameErr) {
		field := "name"
		return &serverapi.WorkflowLabelError{
			Reason:    serverapi.WorkflowLabelErrorReasonInvalidName,
			ProjectID: scope.projectID,
			Field:     &field,
		}
	}
	var conflictErr workflowstore.ProjectLabelNameConflictError
	if errors.As(err, &conflictErr) {
		projectID := conflictErr.ProjectID
		return &serverapi.WorkflowLabelError{
			Reason:    serverapi.WorkflowLabelErrorReasonNameConflict,
			ProjectID: &projectID,
		}
	}
	var limitErr workflowstore.ProjectLabelLimitError
	if errors.As(err, &limitErr) {
		projectID := limitErr.ProjectID
		limit := limitErr.Limit
		return &serverapi.WorkflowLabelError{
			Reason:    serverapi.WorkflowLabelErrorReasonCatalogLimit,
			ProjectID: &projectID,
			Limit:     &limit,
		}
	}
	var projectLabelNotFound workflowstore.ProjectLabelNotFoundError
	if errors.As(err, &projectLabelNotFound) {
		projectID := projectLabelNotFound.ProjectID
		labelID := projectLabelNotFound.LabelID
		return &serverapi.WorkflowLabelError{
			Reason:    serverapi.WorkflowLabelErrorReasonLabelNotFound,
			ProjectID: &projectID,
			LabelID:   &labelID,
		}
	}
	var orderErr workflowstore.ProjectLabelOrderError
	if errors.As(err, &orderErr) {
		projectID := orderErr.ProjectID
		field := "label_ids"
		return &serverapi.WorkflowLabelError{
			Reason:    serverapi.WorkflowLabelErrorReasonInvalidMutation,
			ProjectID: &projectID,
			Field:     &field,
		}
	}
	var taskNotFound workflowstore.TaskLabelTaskNotFoundError
	if errors.As(err, &taskNotFound) {
		taskID := taskNotFound.TaskID
		return &serverapi.WorkflowLabelError{
			Reason: serverapi.WorkflowLabelErrorReasonTaskNotFound,
			TaskID: &taskID,
		}
	}
	var labelNotFound workflowstore.TaskLabelNotFoundError
	if errors.As(err, &labelNotFound) {
		labelID := labelNotFound.LabelID
		return &serverapi.WorkflowLabelError{
			Reason:  serverapi.WorkflowLabelErrorReasonLabelNotFound,
			LabelID: &labelID,
		}
	}
	var wrongProject workflowstore.TaskLabelWrongProjectError
	if errors.As(err, &wrongProject) {
		projectID := wrongProject.TaskProjectID
		taskID := wrongProject.TaskID
		labelID := wrongProject.LabelID
		return &serverapi.WorkflowLabelError{
			Reason:    serverapi.WorkflowLabelErrorReasonWrongProject,
			ProjectID: &projectID,
			TaskID:    &taskID,
			LabelID:   &labelID,
		}
	}
	var mutationErr workflowstore.TaskLabelMutationError
	if errors.As(err, &mutationErr) {
		field := mutationErr.Field
		return &serverapi.WorkflowLabelError{
			Reason:  serverapi.WorkflowLabelErrorReasonInvalidMutation,
			TaskID:  scope.taskID,
			LabelID: mutationErr.LabelID,
			Field:   &field,
			Limit:   mutationErr.Limit,
		}
	}
	if errors.Is(err, serverapi.ErrProjectNotFound) {
		return &serverapi.WorkflowLabelError{
			Reason:    serverapi.WorkflowLabelErrorReasonProjectNotFound,
			ProjectID: scope.projectID,
		}
	}
	return err
}

func (s *Service) publishProjectEvent(ctx context.Context, projectID string, resource serverapi.WorkflowProjectEventResource, action serverapi.WorkflowProjectEventAction, primaryEntityID string, relatedIDs ...string) {
	s.publishWorkflowEvent(ctx, workflowstore.WorkflowEventRecord{
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
