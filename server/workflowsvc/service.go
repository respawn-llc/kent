package workflowsvc

import (
	"context"
	"errors"

	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/server/workflowview"
	"builder/shared/serverapi"
)

type Service struct {
	store         *workflowstore.Store
	view          *workflowview.Service
	roleResolver  workflow.RoleResolver
	taskWorktrees taskWorktreeEnsurer
	runtimeCancel taskRuntimeCanceler
	schedulerWake schedulerNotifier
}

type taskWorktreeEnsurer interface {
	EnsureTaskWorktree(ctx context.Context, taskID string) error
}

type taskRuntimeCanceler interface {
	CancelTaskRuns(ctx context.Context, taskID workflow.TaskID) error
}

type schedulerNotifier interface {
	Notify()
}

type Option func(*Service)

func WithTaskWorktreeEnsurer(ensurer taskWorktreeEnsurer) Option {
	return func(s *Service) {
		s.taskWorktrees = ensurer
	}
}

func WithTaskRuntimeCanceler(canceler taskRuntimeCanceler) Option {
	return func(s *Service) {
		s.runtimeCancel = canceler
	}
}

func WithSchedulerNotifier(notifier schedulerNotifier) Option {
	return func(s *Service) {
		s.schedulerWake = notifier
	}
}

func New(store *workflowstore.Store, view *workflowview.Service, roleResolver workflow.RoleResolver, opts ...Option) (*Service, error) {
	if store == nil {
		return nil, errors.New("workflow store is required")
	}
	if view == nil {
		return nil, errors.New("workflow view is required")
	}
	service := &Service{store: store, view: view, roleResolver: roleResolver}
	for _, opt := range opts {
		opt(service)
	}
	return service, nil
}

func (s *Service) CreateWorkflow(ctx context.Context, req serverapi.WorkflowCreateRequest) (serverapi.WorkflowCreateResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowCreateResponse{}, err
	}
	created, err := s.store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: req.Name, Description: req.Description})
	if err != nil {
		return serverapi.WorkflowCreateResponse{}, err
	}
	return serverapi.WorkflowCreateResponse{Workflow: workflowRecord(created)}, nil
}

func (s *Service) UpdateWorkflow(ctx context.Context, req serverapi.WorkflowUpdateRequest) (serverapi.WorkflowGetResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowGetResponse{}, err
	}
	if err := s.store.UpdateWorkflowInfo(ctx, workflow.WorkflowID(req.WorkflowID), req.Name, req.Description); err != nil {
		return serverapi.WorkflowGetResponse{}, err
	}
	return s.GetWorkflow(ctx, serverapi.WorkflowGetRequest{WorkflowID: req.WorkflowID})
}

func (s *Service) ListWorkflows(ctx context.Context, _ serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error) {
	rows, err := s.store.ListWorkflows(ctx)
	if err != nil {
		return serverapi.WorkflowListResponse{}, err
	}
	out := make([]serverapi.WorkflowRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, workflowRecord(row))
	}
	return serverapi.WorkflowListResponse{Workflows: out}, nil
}

func (s *Service) GetWorkflow(ctx context.Context, req serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowGetResponse{}, err
	}
	def, _, err := s.view.GetDefinition(ctx, req.WorkflowID)
	if err != nil {
		return serverapi.WorkflowGetResponse{}, err
	}
	return serverapi.WorkflowGetResponse{Definition: def}, nil
}

func (s *Service) AddWorkflowNode(ctx context.Context, req serverapi.WorkflowNodeAddRequest) (serverapi.WorkflowNodeAddResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowNodeAddResponse{}, err
	}
	revision, err := s.store.AddNode(ctx, workflowstore.NodeRecord{ID: workflow.NodeID(req.NodeID), WorkflowID: workflow.WorkflowID(req.WorkflowID), Key: workflow.ModelKey(req.Key), Kind: workflow.NodeKind(req.Kind), DisplayName: req.DisplayName, SubagentRole: req.SubagentRole, PromptTemplate: req.PromptTemplate, OutputFields: outputFields(req.OutputFields)})
	if err != nil {
		return serverapi.WorkflowNodeAddResponse{}, err
	}
	return serverapi.WorkflowNodeAddResponse{GraphRevision: revision}, nil
}

func (s *Service) AddWorkflowTransitionGroup(ctx context.Context, req serverapi.WorkflowTransitionGroupAddRequest) (serverapi.WorkflowTransitionGroupAddResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTransitionGroupAddResponse{}, err
	}
	revision, err := s.store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID(req.GroupID), WorkflowID: workflow.WorkflowID(req.WorkflowID), SourceNodeID: workflow.NodeID(req.SourceNodeID), TransitionID: workflow.TransitionID(req.TransitionID), DisplayName: req.DisplayName})
	if err != nil {
		return serverapi.WorkflowTransitionGroupAddResponse{}, err
	}
	return serverapi.WorkflowTransitionGroupAddResponse{GraphRevision: revision}, nil
}

func (s *Service) AddWorkflowEdge(ctx context.Context, req serverapi.WorkflowEdgeAddRequest) (serverapi.WorkflowEdgeAddResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowEdgeAddResponse{}, err
	}
	revision, err := s.store.AddEdge(ctx, workflowstore.EdgeRecord{ID: workflow.EdgeID(req.EdgeID), WorkflowID: workflow.WorkflowID(req.WorkflowID), TransitionGroupID: workflow.TransitionGroupID(req.TransitionGroupID), Key: workflow.ModelKey(req.Key), TargetNodeID: workflow.NodeID(req.TargetNodeID), RequiresApproval: req.RequiresApproval, ContextMode: workflow.ContextMode(req.ContextMode), InputBindings: inputBindings(req.InputBindings), OutputRequirements: outputRequirements(req.OutputRequirements)})
	if err != nil {
		return serverapi.WorkflowEdgeAddResponse{}, err
	}
	return serverapi.WorkflowEdgeAddResponse{GraphRevision: revision}, nil
}

func (s *Service) LinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowLinkProjectRequest) (serverapi.WorkflowLinkProjectResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowLinkProjectResponse{}, err
	}
	link, err := s.store.LinkWorkflow(ctx, req.ProjectID, workflow.WorkflowID(req.WorkflowID), req.Default)
	if err != nil {
		return serverapi.WorkflowLinkProjectResponse{}, err
	}
	return serverapi.WorkflowLinkProjectResponse{Link: projectWorkflowLink(link)}, nil
}

func (s *Service) ListProjectWorkflowLinks(ctx context.Context, req serverapi.WorkflowListProjectLinksRequest) (serverapi.WorkflowListProjectLinksResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowListProjectLinksResponse{}, err
	}
	links, err := s.store.ListProjectWorkflowLinks(ctx, req.ProjectID)
	if err != nil {
		return serverapi.WorkflowListProjectLinksResponse{}, err
	}
	out := make([]serverapi.ProjectWorkflowLink, 0, len(links))
	for _, link := range links {
		out = append(out, projectWorkflowLink(link))
	}
	return serverapi.WorkflowListProjectLinksResponse{Links: out}, nil
}

func (s *Service) SetDefaultProjectWorkflowLink(ctx context.Context, req serverapi.WorkflowSetDefaultProjectLinkRequest) (serverapi.WorkflowSetDefaultProjectLinkResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowSetDefaultProjectLinkResponse{}, err
	}
	link, err := s.store.SetDefaultProjectWorkflowLink(ctx, req.ProjectID, workflow.WorkflowID(req.WorkflowID))
	if err != nil {
		return serverapi.WorkflowSetDefaultProjectLinkResponse{}, err
	}
	return serverapi.WorkflowSetDefaultProjectLinkResponse{Link: projectWorkflowLink(link)}, nil
}

func (s *Service) UnlinkWorkflowFromProject(ctx context.Context, req serverapi.WorkflowUnlinkProjectRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return s.store.UnlinkProjectWorkflow(ctx, req.LinkID, req.ReplacementDefaultLinkID)
}

func (s *Service) ValidateWorkflow(ctx context.Context, req serverapi.WorkflowValidateRequest) (serverapi.WorkflowValidateResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowValidateResponse{}, err
	}
	def, _, err := s.store.GetDefinition(ctx, workflow.WorkflowID(req.WorkflowID))
	if err != nil {
		return serverapi.WorkflowValidateResponse{}, err
	}
	mode := workflow.ValidationContext(req.Mode)
	if mode == "" {
		mode = workflow.ValidationContextDraft
	}
	result := workflow.ValidateDefinition(def, workflow.ValidationOptions{Context: mode, RoleResolver: s.roleResolver})
	resp := serverapi.WorkflowValidateResponse{Valid: result.Valid()}
	for _, validationErr := range result.Errors {
		resp.Errors = append(resp.Errors, serverapi.WorkflowValidationError{Code: string(validationErr.Code), Message: validationErr.Message, WorkflowID: string(validationErr.WorkflowID), NodeID: string(validationErr.NodeID), TransitionGroupID: string(validationErr.TransitionGroupID), EdgeID: string(validationErr.EdgeID), RelatedIDs: validationErr.RelatedIDs, BlocksContext: validationErr.BlocksContext})
	}
	return resp, nil
}

func (s *Service) CreateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCreateRequest) (serverapi.WorkflowTaskCreateResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskCreateResponse{}, err
	}
	task, err := s.store.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: req.ProjectID, WorkflowID: workflow.WorkflowID(req.WorkflowID), Title: req.Title, Body: req.Body, SourceURL: req.SourceURL})
	if err != nil {
		return serverapi.WorkflowTaskCreateResponse{}, err
	}
	detail, err := s.view.GetTask(ctx, string(task.ID))
	if err != nil {
		return serverapi.WorkflowTaskCreateResponse{}, err
	}
	return serverapi.WorkflowTaskCreateResponse{Task: detail.Summary}, nil
}

func (s *Service) StartWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskStartResponse{}, err
	}
	started, err := s.StartTaskAutomation(ctx, req.TaskID)
	if err != nil {
		return serverapi.WorkflowTaskStartResponse{}, err
	}
	return serverapi.WorkflowTaskStartResponse{TransitionID: started.TransitionID, PlacementID: string(started.PlacementID), RunID: string(started.RunID)}, nil
}

func (s *Service) StartTaskAutomation(ctx context.Context, taskID string) (workflowstore.StartTaskResult, error) {
	if s.taskWorktrees != nil {
		if err := s.store.ValidateTaskStart(ctx, workflow.TaskID(taskID)); err != nil {
			return workflowstore.StartTaskResult{}, err
		}
		if err := s.taskWorktrees.EnsureTaskWorktree(ctx, taskID); err != nil {
			return workflowstore.StartTaskResult{}, err
		}
	}
	started, err := s.store.StartTask(ctx, workflow.TaskID(taskID))
	if err != nil {
		return workflowstore.StartTaskResult{}, err
	}
	if s.schedulerWake != nil {
		s.schedulerWake.Notify()
	}
	return started, nil
}

func (s *Service) ApproveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskApproveRequest) (serverapi.WorkflowTaskApproveResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskApproveResponse{}, err
	}
	approved, err := s.store.ApproveTransition(ctx, workflow.TransitionID(req.TransitionID))
	if err != nil {
		return serverapi.WorkflowTaskApproveResponse{}, err
	}
	if s.schedulerWake != nil {
		s.schedulerWake.Notify()
	}
	return serverapi.WorkflowTaskApproveResponse{TransitionID: string(approved.TransitionID), State: approved.State, PlacementIDs: placementIDs(approved.PlacementIDs), RunIDs: runIDs(approved.RunIDs)}, nil
}

func (s *Service) MoveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskMoveRequest) (serverapi.WorkflowTaskMoveResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskMoveResponse{}, err
	}
	moved, err := s.store.ManualMoveTask(ctx, workflowstore.ManualMoveRequest{TaskID: workflow.TaskID(req.TaskID), TargetNodeID: workflow.NodeID(req.TargetNodeID), OutputValues: req.OutputValues, Commentary: req.Commentary, Actor: "user"})
	if err != nil {
		return serverapi.WorkflowTaskMoveResponse{}, err
	}
	if s.schedulerWake != nil {
		s.schedulerWake.Notify()
	}
	return serverapi.WorkflowTaskMoveResponse{TransitionID: string(moved.TransitionID), State: moved.State, PlacementIDs: placementIDs(moved.PlacementIDs), RunIDs: runIDs(moved.RunIDs)}, nil
}

func (s *Service) CancelWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCancelRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if err := s.store.CancelTask(ctx, workflow.TaskID(req.TaskID), req.Reason); err != nil {
		return err
	}
	if s.runtimeCancel != nil {
		return s.runtimeCancel.CancelTaskRuns(ctx, workflow.TaskID(req.TaskID))
	}
	return nil
}

func (s *Service) AddWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskCommentAddResponse{}, err
	}
	comment, err := s.store.AddComment(ctx, workflow.TaskID(req.TaskID), req.Body, req.Author, req.AuthorID)
	if err != nil {
		return serverapi.WorkflowTaskCommentAddResponse{}, err
	}
	return serverapi.WorkflowTaskCommentAddResponse{Comment: commentRecord(comment)}, nil
}

func (s *Service) ListWorkflowTaskComments(ctx context.Context, req serverapi.WorkflowTaskCommentListRequest) (serverapi.WorkflowTaskCommentListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskCommentListResponse{}, err
	}
	comments, err := s.store.ListComments(ctx, workflow.TaskID(req.TaskID), req.IncludeDeleted)
	if err != nil {
		return serverapi.WorkflowTaskCommentListResponse{}, err
	}
	out := make([]serverapi.WorkflowTaskComment, 0, len(comments))
	for _, comment := range comments {
		out = append(out, commentRecord(comment))
	}
	return serverapi.WorkflowTaskCommentListResponse{Comments: out}, nil
}

func (s *Service) ReplaceWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentReplaceRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return s.store.ReplaceComment(ctx, req.CommentID, req.Body)
}

func (s *Service) DeleteWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentDeleteRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return s.store.DeleteComment(ctx, req.CommentID)
}

func (s *Service) GetWorkflowBoard(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowBoardResponse{}, err
	}
	board, err := s.view.GetBoard(ctx, req.ProjectID)
	if err != nil {
		return serverapi.WorkflowBoardResponse{}, err
	}
	return serverapi.WorkflowBoardResponse{Board: board}, nil
}

func (s *Service) GetWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskGetResponse{}, err
	}
	detail, err := s.view.GetTask(ctx, req.TaskID)
	if err != nil {
		return serverapi.WorkflowTaskGetResponse{}, err
	}
	return serverapi.WorkflowTaskGetResponse{Task: detail}, nil
}

func workflowRecord(row workflowstore.WorkflowRecord) serverapi.WorkflowRecord {
	return serverapi.WorkflowRecord{ID: string(row.ID), Name: row.Name, Description: row.Description, GraphRevision: row.GraphRevision}
}

func projectWorkflowLink(row workflowstore.ProjectWorkflowLinkRecord) serverapi.ProjectWorkflowLink {
	return serverapi.ProjectWorkflowLink{ID: row.ID, ProjectID: row.ProjectID, WorkflowID: string(row.WorkflowID), Default: row.IsDefault, UnlinkedAtUnixMs: row.UnlinkedAtUnixMs}
}

func commentRecord(row workflowstore.CommentRecord) serverapi.WorkflowTaskComment {
	return serverapi.WorkflowTaskComment{ID: row.ID, TaskID: string(row.TaskID), Body: row.Body, Author: row.Author, AuthorID: row.AuthorID, DeletedAt: row.DeletedAt, UpdatedAt: row.UpdatedAt}
}

func outputFields(in []serverapi.WorkflowOutputField) []workflow.OutputField {
	out := make([]workflow.OutputField, 0, len(in))
	for _, field := range in {
		out = append(out, workflow.OutputField{Name: field.Name, Description: field.Description})
	}
	return out
}

func inputBindings(in []serverapi.WorkflowInputBinding) []workflow.InputBinding {
	out := make([]workflow.InputBinding, 0, len(in))
	for _, binding := range in {
		out = append(out, workflow.InputBinding{Name: binding.Name, Source: workflow.BindingSource(binding.Source), Field: binding.Field})
	}
	return out
}

func outputRequirements(in []serverapi.WorkflowOutputRequirement) []workflow.OutputRequirement {
	out := make([]workflow.OutputRequirement, 0, len(in))
	for _, requirement := range in {
		out = append(out, workflow.OutputRequirement{FieldName: requirement.FieldName})
	}
	return out
}

func placementIDs(in []workflow.PlacementID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		out = append(out, string(id))
	}
	return out
}

func runIDs(in []workflow.RunID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		out = append(out, string(id))
	}
	return out
}
