package workflowsvc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"builder/server/requestmemo"
	askquestion "builder/server/tools/askquestion"
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
	prompts       pendingPromptResponder
	questionMemo  *requestmemo.Memo[taskQuestionAnswerMemoRequest, struct{}]
}

type taskWorktreeEnsurer interface {
	EnsureTaskWorktree(ctx context.Context, taskID string) error
}

type taskRuntimeCanceler interface {
	CancelTaskRuns(ctx context.Context, taskID workflow.TaskID) error
}

type taskRuntimeRunCanceler interface {
	CancelRun(ctx context.Context, runID workflow.RunID) error
}

type schedulerNotifier interface {
	Notify()
}

type pendingPromptResponder interface {
	SubmitPromptResponse(sessionID string, resp askquestion.Response, err error) error
}

type taskQuestionAnswerMemoRequest struct {
	TaskID               string
	RunID                string
	AskID                string
	ErrorMessage         string
	Answer               string
	SelectedOptionNumber int
	FreeformAnswer       string
}

type workflowProjectSubscription struct {
	store        *workflowstore.Store
	projectID    string
	nextSequence int64
	closed       atomic.Bool
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

func WithPromptResponder(responder pendingPromptResponder) Option {
	return func(s *Service) {
		s.prompts = responder
	}
}

func New(store *workflowstore.Store, view *workflowview.Service, roleResolver workflow.RoleResolver, opts ...Option) (*Service, error) {
	if store == nil {
		return nil, errors.New("workflow store is required")
	}
	if view == nil {
		return nil, errors.New("workflow view is required")
	}
	service := &Service{store: store, view: view, roleResolver: roleResolver, questionMemo: requestmemo.New[taskQuestionAnswerMemoRequest, struct{}]()}
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

func (s *Service) publishWorkflowEvent(ctx context.Context, projectID string, workflowID string, resource string, action string, changedIDs ...string) {
	if _, err := s.store.RecordWorkflowEvent(ctx, workflowstore.WorkflowEventRecord{ProjectID: projectID, WorkflowID: workflowID, Resource: resource, Action: action, ChangedIDs: changedIDs}); err != nil {
		slog.Warn("record workflow event failed", "project_id", strings.TrimSpace(projectID), "workflow_id", strings.TrimSpace(workflowID), "resource", strings.TrimSpace(resource), "action", strings.TrimSpace(action), "changed_ids", changedIDs, "error", err)
	}
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
	revision, err := s.store.AddNode(ctx, workflowstore.NodeRecord{ID: workflow.NodeID(req.NodeID), WorkflowID: workflow.WorkflowID(req.WorkflowID), Key: workflow.ModelKey(req.Key), Kind: workflow.NodeKind(req.Kind), DisplayName: req.DisplayName, GroupKey: req.GroupKey, SubagentRole: req.SubagentRole, PromptTemplate: req.PromptTemplate, OutputFields: outputFields(req.OutputFields)})
	if err != nil {
		return serverapi.WorkflowNodeAddResponse{}, err
	}
	s.publishWorkflowEvent(ctx, "", req.WorkflowID, "workflow", "node_added", req.NodeID)
	return serverapi.WorkflowNodeAddResponse{GraphRevision: revision}, nil
}

func (s *Service) AddWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupAddRequest) (serverapi.WorkflowNodeGroupResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowNodeGroupResponse{}, err
	}
	group, revision, err := s.store.AddNodeGroup(ctx, workflowstore.NodeGroupRecord{ID: req.GroupID, WorkflowID: workflow.WorkflowID(req.WorkflowID), Key: workflow.ModelKey(req.GroupKey), DisplayName: req.DisplayName, SortOrder: int64(req.SortOrder), MetadataJSON: req.MetadataJSON})
	if err != nil {
		return serverapi.WorkflowNodeGroupResponse{}, err
	}
	s.publishWorkflowEvent(ctx, "", req.WorkflowID, "workflow", "node_group_added", group.ID)
	return serverapi.WorkflowNodeGroupResponse{Group: workflowNodeGroup(group), GraphRevision: revision}, nil
}

func (s *Service) UpdateWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupUpdateRequest) (serverapi.WorkflowNodeGroupResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowNodeGroupResponse{}, err
	}
	group, revision, err := s.store.UpdateNodeGroup(ctx, workflowstore.NodeGroupRecord{ID: req.GroupID, WorkflowID: workflow.WorkflowID(req.WorkflowID), Key: workflow.ModelKey(req.GroupKey), DisplayName: req.DisplayName, SortOrder: int64(req.SortOrder), MetadataJSON: req.MetadataJSON})
	if err != nil {
		return serverapi.WorkflowNodeGroupResponse{}, err
	}
	s.publishWorkflowEvent(ctx, "", req.WorkflowID, "workflow", "node_group_updated", group.ID)
	return serverapi.WorkflowNodeGroupResponse{Group: workflowNodeGroup(group), GraphRevision: revision}, nil
}

func (s *Service) DeleteWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupDeleteRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if _, err := s.store.DeleteNodeGroup(ctx, workflow.WorkflowID(req.WorkflowID), req.GroupID); err != nil {
		return err
	}
	s.publishWorkflowEvent(ctx, "", req.WorkflowID, "workflow", "node_group_deleted", req.GroupID)
	return nil
}

func (s *Service) AddWorkflowTransitionGroup(ctx context.Context, req serverapi.WorkflowTransitionGroupAddRequest) (serverapi.WorkflowTransitionGroupAddResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTransitionGroupAddResponse{}, err
	}
	revision, err := s.store.AddTransitionGroup(ctx, workflowstore.TransitionGroupRecord{ID: workflow.TransitionGroupID(req.GroupID), WorkflowID: workflow.WorkflowID(req.WorkflowID), SourceNodeID: workflow.NodeID(req.SourceNodeID), TransitionID: workflow.TransitionID(req.TransitionID), DisplayName: req.DisplayName})
	if err != nil {
		return serverapi.WorkflowTransitionGroupAddResponse{}, err
	}
	s.publishWorkflowEvent(ctx, "", req.WorkflowID, "workflow", "transition_group_added", req.GroupID)
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
	s.publishWorkflowEvent(ctx, "", req.WorkflowID, "workflow", "edge_added", req.EdgeID)
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
	s.publishWorkflowEvent(ctx, req.ProjectID, req.WorkflowID, "workflow_link", "linked", link.ID)
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
	s.publishWorkflowEvent(ctx, req.ProjectID, req.WorkflowID, "workflow_link", "default_changed", link.ID)
	return serverapi.WorkflowSetDefaultProjectLinkResponse{Link: projectWorkflowLink(link)}, nil
}

func (s *Service) UnlinkWorkflowFromProject(ctx context.Context, req serverapi.WorkflowUnlinkProjectRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	link, err := s.store.GetProjectWorkflowLink(ctx, req.LinkID)
	if err != nil {
		return err
	}
	if err := s.store.UnlinkProjectWorkflow(ctx, req.LinkID, req.ReplacementDefaultLinkID); err != nil {
		return err
	}
	s.publishWorkflowEvent(ctx, link.ProjectID, string(link.WorkflowID), "workflow_link", "unlinked", req.LinkID)
	return nil
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
	task, err := s.store.CreateTask(ctx, workflowstore.CreateTaskRequest{ProjectID: req.ProjectID, WorkflowID: workflow.WorkflowID(req.WorkflowID), Title: req.Title, Body: req.Body, SourceURL: req.SourceURL, SourceWorkspaceID: req.SourceWorkspaceID})
	if err != nil {
		return serverapi.WorkflowTaskCreateResponse{}, err
	}
	s.publishWorkflowEvent(ctx, task.ProjectID, string(task.WorkflowID), "task", "created", string(task.ID))
	detail, err := s.view.GetTask(ctx, string(task.ID))
	if err != nil {
		return serverapi.WorkflowTaskCreateResponse{}, err
	}
	return serverapi.WorkflowTaskCreateResponse{Task: detail.Summary}, nil
}

func (s *Service) UpdateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskUpdateRequest) (serverapi.WorkflowTaskUpdateResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskUpdateResponse{}, err
	}
	task, err := s.store.UpdateTask(ctx, workflowstore.UpdateTaskRequest{TaskID: workflow.TaskID(req.TaskID), Title: req.Title, Body: req.Body, SourceWorkspaceID: req.SourceWorkspaceID})
	if err != nil {
		return serverapi.WorkflowTaskUpdateResponse{}, err
	}
	s.publishWorkflowEvent(ctx, task.ProjectID, string(task.WorkflowID), "task", "updated", string(task.ID))
	detail, err := s.view.GetTask(ctx, string(task.ID))
	if err != nil {
		return serverapi.WorkflowTaskUpdateResponse{}, err
	}
	return serverapi.WorkflowTaskUpdateResponse{Task: detail.Summary}, nil
}

func (s *Service) StartWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskStartResponse{}, err
	}
	started, err := s.StartTaskAutomation(ctx, req.TaskID)
	if err != nil {
		return serverapi.WorkflowTaskStartResponse{}, err
	}
	if detail, detailErr := s.view.GetTask(ctx, req.TaskID); detailErr == nil {
		s.publishWorkflowEvent(ctx, detail.Summary.ProjectID, detail.Summary.WorkflowID, "task", "started", req.TaskID, string(started.RunID))
	}
	return serverapi.WorkflowTaskStartResponse{TransitionID: started.TransitionID, PlacementID: string(started.PlacementID), RunID: string(started.RunID)}, nil
}

func (s *Service) InterruptWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskInterruptRequest) (serverapi.WorkflowTaskInterruptResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskInterruptResponse{}, err
	}
	interrupted, err := s.store.InterruptTaskRun(ctx, workflow.TaskID(req.TaskID), workflow.RunID(req.RunID), req.Reason)
	if err != nil {
		return serverapi.WorkflowTaskInterruptResponse{}, err
	}
	if canceler, ok := s.runtimeCancel.(taskRuntimeRunCanceler); ok {
		if err := canceler.CancelRun(ctx, interrupted.ID); err != nil {
			return serverapi.WorkflowTaskInterruptResponse{}, err
		}
	}
	if detail, detailErr := s.view.GetTask(ctx, req.TaskID); detailErr == nil {
		s.publishWorkflowEvent(ctx, detail.Summary.ProjectID, detail.Summary.WorkflowID, "task", "interrupted", req.TaskID, string(interrupted.ID))
	}
	return serverapi.WorkflowTaskInterruptResponse{RunID: string(interrupted.ID)}, nil
}

func (s *Service) ResumeWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskResumeRequest) (serverapi.WorkflowTaskResumeResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskResumeResponse{}, err
	}
	resumed, err := s.store.ResumeTaskRunByID(ctx, workflow.TaskID(req.TaskID), workflow.RunID(req.RunID))
	if err != nil {
		return serverapi.WorkflowTaskResumeResponse{}, err
	}
	if s.schedulerWake != nil {
		s.schedulerWake.Notify()
	}
	if detail, detailErr := s.view.GetTask(ctx, req.TaskID); detailErr == nil {
		s.publishWorkflowEvent(ctx, detail.Summary.ProjectID, detail.Summary.WorkflowID, "task", "resumed", req.TaskID, string(resumed.ID))
	}
	return serverapi.WorkflowTaskResumeResponse{RunID: string(resumed.ID), PlacementID: string(resumed.PlacementID), NodeID: string(resumed.NodeID), Generation: resumed.Generation, SessionID: resumed.SessionID}, nil
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
	transitionID := strings.TrimSpace(req.TaskTransitionID)
	if transitionID == "" {
		transitionID = strings.TrimSpace(req.TransitionID)
	}
	approved, err := s.store.ApproveTransition(ctx, workflow.TransitionID(transitionID))
	if err != nil {
		return serverapi.WorkflowTaskApproveResponse{}, err
	}
	if s.schedulerWake != nil {
		s.schedulerWake.Notify()
	}
	if taskID, projectID, workflowID, detailErr := s.taskIdentityForTransition(ctx, transitionID); detailErr == nil {
		s.publishWorkflowEvent(ctx, projectID, workflowID, "task", "approved", taskID, transitionID)
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
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "user_canceled"
	}
	if err := s.store.CancelTask(ctx, workflow.TaskID(req.TaskID), reason); err != nil {
		return err
	}
	if detail, detailErr := s.view.GetTask(ctx, req.TaskID); detailErr == nil {
		s.publishWorkflowEvent(ctx, detail.Summary.ProjectID, detail.Summary.WorkflowID, "task", "canceled", req.TaskID)
	}
	if s.runtimeCancel != nil {
		return s.runtimeCancel.CancelTaskRuns(ctx, workflow.TaskID(req.TaskID))
	}
	return nil
}

func (s *Service) ListWorkflowAttention(ctx context.Context, req serverapi.WorkflowAttentionListRequest) (serverapi.WorkflowAttentionListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowAttentionListResponse{}, err
	}
	return s.view.ListAttention(ctx, req, s.roleResolver)
}

func (s *Service) ListWorkflowTaskAttention(ctx context.Context, req serverapi.WorkflowTaskAttentionListRequest) (serverapi.WorkflowTaskAttentionListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskAttentionListResponse{}, err
	}
	return s.view.ListTaskAttention(ctx, req, s.roleResolver)
}

func (s *Service) AnswerWorkflowTaskQuestion(ctx context.Context, req serverapi.WorkflowTaskQuestionAnswerRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if s == nil || s.prompts == nil {
		return errors.New("prompt responder is required")
	}
	memoReq := taskQuestionAnswerMemoRequest{TaskID: req.TaskID, RunID: req.RunID, AskID: req.AskID, ErrorMessage: req.ErrorMessage, Answer: req.Answer, SelectedOptionNumber: req.SelectedOptionNumber, FreeformAnswer: req.FreeformAnswer}
	_, err := s.questionMemo.Do(ctx, req.ClientRequestID, memoReq, sameTaskQuestionAnswerMemoRequest, func(ctx context.Context) (struct{}, error) {
		run, err := s.store.ResolveTaskWaitingAsk(ctx, workflow.TaskID(req.TaskID), workflow.RunID(req.RunID), req.AskID)
		if err != nil {
			return struct{}{}, err
		}
		if strings.TrimSpace(req.ErrorMessage) != "" {
			if err := s.prompts.SubmitPromptResponse(run.SessionID, askquestion.Response{RequestID: req.AskID}, errors.New(req.ErrorMessage)); err != nil {
				return struct{}{}, err
			}
		} else if err := s.prompts.SubmitPromptResponse(run.SessionID, askquestion.Response{RequestID: req.AskID, Answer: req.Answer, SelectedOptionNumber: req.SelectedOptionNumber, FreeformAnswer: req.FreeformAnswer}, nil); err != nil {
			return struct{}{}, err
		}
		if detail, detailErr := s.view.GetTask(ctx, req.TaskID); detailErr == nil {
			s.publishWorkflowEvent(ctx, detail.Summary.ProjectID, detail.Summary.WorkflowID, "task", "question_answered", req.TaskID, string(run.ID), req.AskID)
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) taskIdentityForTransition(ctx context.Context, transitionID string) (taskID string, projectID string, workflowID string, err error) {
	return s.store.TaskIdentityForTransition(ctx, workflow.TransitionID(transitionID))
}

func sameTaskQuestionAnswerMemoRequest(a taskQuestionAnswerMemoRequest, b taskQuestionAnswerMemoRequest) bool {
	return a.TaskID == b.TaskID &&
		a.RunID == b.RunID &&
		a.AskID == b.AskID &&
		a.ErrorMessage == b.ErrorMessage &&
		a.Answer == b.Answer &&
		a.SelectedOptionNumber == b.SelectedOptionNumber &&
		a.FreeformAnswer == b.FreeformAnswer
}

func (s *Service) AddWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskCommentAddResponse{}, err
	}
	comment, err := s.store.AddComment(ctx, workflow.TaskID(req.TaskID), req.Body, req.Author, req.AuthorID)
	if err != nil {
		return serverapi.WorkflowTaskCommentAddResponse{}, err
	}
	if detail, detailErr := s.view.GetTask(ctx, req.TaskID); detailErr == nil {
		s.publishWorkflowEvent(ctx, detail.Summary.ProjectID, detail.Summary.WorkflowID, "task", "comment_added", req.TaskID, comment.ID)
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
	taskID, projectID, workflowID, err := s.taskIdentityForComment(ctx, req.CommentID)
	if err != nil {
		return err
	}
	if err := s.store.ReplaceComment(ctx, req.CommentID, req.Body); err != nil {
		return err
	}
	s.publishWorkflowEvent(ctx, projectID, workflowID, "task", "comment_updated", taskID, req.CommentID)
	return nil
}

func (s *Service) DeleteWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentDeleteRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	taskID, projectID, workflowID, err := s.taskIdentityForComment(ctx, req.CommentID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteComment(ctx, req.CommentID); err != nil {
		return err
	}
	s.publishWorkflowEvent(ctx, projectID, workflowID, "task", "comment_deleted", taskID, req.CommentID)
	return nil
}

func (s *Service) ListWorkflowTaskActivity(ctx context.Context, req serverapi.WorkflowTaskActivityListRequest) (serverapi.WorkflowTaskActivityListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskActivityListResponse{}, err
	}
	return s.view.ListTaskActivity(ctx, req)
}

func (s *Service) GetWorkflowTaskTeleportTarget(ctx context.Context, req serverapi.WorkflowTaskTeleportTargetRequest) (serverapi.WorkflowTaskTeleportTargetResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowTaskTeleportTargetResponse{}, err
	}
	return s.view.GetTaskTeleportTarget(ctx, req)
}

func (s *Service) GetWorkflowBoard(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.WorkflowBoardResponse{}, err
	}
	board, err := s.view.GetBoard(ctx, req, s.roleResolver)
	if err != nil {
		return serverapi.WorkflowBoardResponse{}, err
	}
	return serverapi.WorkflowBoardResponse{Board: board}, nil
}

func (s *Service) SubscribeWorkflowProject(ctx context.Context, req serverapi.WorkflowProjectSubscribeRequest) (serverapi.WorkflowProjectSubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return &workflowProjectSubscription{store: s.store, projectID: strings.TrimSpace(req.ProjectID), nextSequence: req.AfterSequence + 1}, nil
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

func workflowNodeGroup(row workflowstore.NodeGroupRecord) serverapi.WorkflowNodeGroup {
	return serverapi.WorkflowNodeGroup{GroupID: row.ID, WorkflowID: string(row.WorkflowID), GroupKey: string(row.Key), DisplayName: row.DisplayName, SortOrder: int(row.SortOrder), MetadataJSON: row.MetadataJSON}
}

func (s *workflowProjectSubscription) Next(ctx context.Context) (serverapi.WorkflowProjectEvent, error) {
	if s == nil || s.store == nil {
		return serverapi.WorkflowProjectEvent{}, errors.New("workflow project subscription is required")
	}
	for {
		if s.closed.Load() {
			return serverapi.WorkflowProjectEvent{}, io.EOF
		}
		events, err := s.store.ListWorkflowEventsAfter(ctx, s.projectID, s.nextSequence-1, 1)
		if err != nil {
			return serverapi.WorkflowProjectEvent{}, err
		}
		if len(events) > 0 {
			event := events[0]
			s.nextSequence = event.Sequence + 1
			return workflowProjectEvent(event), nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return serverapi.WorkflowProjectEvent{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *workflowProjectSubscription) Close() error {
	if s != nil {
		s.closed.Store(true)
	}
	return nil
}

func workflowProjectEvent(row workflowstore.WorkflowEventRecord) serverapi.WorkflowProjectEvent {
	return serverapi.WorkflowProjectEvent{Sequence: row.Sequence, ProjectID: row.ProjectID, WorkflowID: row.WorkflowID, Resource: row.Resource, Action: row.Action, ChangedIDs: row.ChangedIDs, OccurredAtUnixMs: row.OccurredAtUnixMs}
}

func projectWorkflowLink(row workflowstore.ProjectWorkflowLinkRecord) serverapi.ProjectWorkflowLink {
	return serverapi.ProjectWorkflowLink{ID: row.ID, ProjectID: row.ProjectID, WorkflowID: string(row.WorkflowID), Default: row.IsDefault, UnlinkedAtUnixMs: row.UnlinkedAtUnixMs}
}

func commentRecord(row workflowstore.CommentRecord) serverapi.WorkflowTaskComment {
	return serverapi.WorkflowTaskComment{ID: row.ID, TaskID: string(row.TaskID), Body: row.Body, Author: row.Author, AuthorID: row.AuthorID, DeletedAt: row.DeletedAt, CreatedAtUnixMs: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func (s *Service) taskIdentityForComment(ctx context.Context, commentID string) (taskID string, projectID string, workflowID string, err error) {
	return s.store.TaskIdentityForComment(ctx, strings.TrimSpace(commentID))
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
