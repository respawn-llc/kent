package workflowsvc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"core/server/sessionruntime"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowruntime"
	"core/server/workflowscript"
	"core/server/workflowstore"
	"core/server/workflowview"
	"core/server/worktree"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"core/shared/worktreecontract"

	"google.golang.org/protobuf/types/known/emptypb"
)

type Service struct {
	store                *workflowstore.Store
	readModels           ReadModels
	roleResolver         workflow.RoleResolver
	executionTargets     executionTargetInfrastructure
	taskWorktreeCleanup  taskWorktreeDeleter
	events               *workflowProjectEventBroker
	attentionFinalizer   workflowAttentionFinalizer
	setupEvents          workflowTaskSetupEventPublisher
	taskMutations        *workflowexecution.TaskMutationCoordinator
	currentNodeExecution taskExecution
}

type taskExecution interface {
	RunTaskOperation(context.Context, func(context.Context) error) error
	StartTask(context.Context, workflow.TaskID, *workflowstore.ExecutionTargetCandidate) (workflowstore.StartTaskResult, error)
	PromoteConcurrencyQueuedTask(context.Context, workflow.TaskID) ([]workflow.CurrentNode, bool, error)
	PreflightTaskResume(context.Context, workflow.TaskID) (workflowexecution.TaskResumePreflight, error)
	ResumeTask(context.Context, workflow.TaskID, *workflowstore.ExecutionTargetCandidate) (workflowexecution.TaskResumeResult, error)
	ApplyPendingApproval(context.Context, workflow.ApprovalID) (workflowstore.PendingApprovalApplyResult, error)
	ApplyManualMove(context.Context, workflowstore.ManualMovePreparation, *workflowstore.ExecutionTargetCandidate) (workflowstore.ManualMoveResult, error)
	InterruptForManualMove(context.Context, workflow.TaskID, func() error) error
	Interrupt(context.Context, workflowexecution.InterruptSelector) error
	EnsureTaskQuiescent(workflow.TaskID) error
	CompleteSessionCurrentNode(context.Context, runtimeids.SessionID, runtimeids.RunID, runtimeids.StepID, string, map[string]string, string) (workflowruntime.CompletionResult, error)
}

type initiatingActionTargetDecision struct {
	prepared          *preparedInitiatingActionTarget
	selectionRequired *taskpb.SelectionRequired
}

type preparedInitiatingActionTarget struct {
	candidate                *workflowstore.ExecutionTargetCandidate
	retainedWorktree         *worktreepb.RegisteredFacts
	retainedPreviousWorktree *worktreepb.RetainedPreviousWorktree
	setupResult              *worktree.WorktreeSetupResult
}

type initiatingActionTargetPreflight struct {
	purpose                worktree.TaskExecutionRootPreparationPurpose
	context                workflowstore.TaskExecutionTargetContext
	selection              workflow.ExecutionTargetSelection
	explicit               bool
	initialBranchAssertion *string
	pendingBranchReplaced  bool
	originalUnavailable    *serverapi.WorkflowLockedExecutionTargetError
}

type initiatingActionRequest struct {
	taskID                  workflow.TaskID
	setupOperationID        *worktreecontract.SetupOperationID
	requiresExecutionTarget bool
	targetPreflight         initiatingActionTargetPreflight
	afterTargetResolution   func() error
}

type initiatingActionResult[T any] struct {
	applied                  *T
	selectionRequired        *taskpb.SelectionRequired
	retainedPreviousWorktree *worktreepb.RetainedPreviousWorktree
}

type initiatingActionPreflight struct {
	unsatisfiedDependencyCount int
	target                     initiatingActionTargetPreflight
}

type manualMovePreflight struct {
	preparation                workflowstore.ManualMovePreparation
	unsatisfiedDependencyCount int
	target                     initiatingActionTargetPreflight
}

type manualMoveNoOpBeforeInterruptError struct {
	currentNodes []workflow.CurrentNode
}

func (e *manualMoveNoOpBeforeInterruptError) Error() string {
	return "manual move became a no-op before interruption"
}

type executionTargetInfrastructure interface {
	InspectProspectiveInitialTaskBranch(context.Context, InitialTaskBranchInspectionRequest) error
	AssertInitialTaskBranch(context.Context, InitialTaskBranchAssertionRequest) error
	ResolveExecutionTarget(context.Context, ExecutionTargetResolveRequest) (workflowstore.ExecutionTargetSnapshot, error)
	MaterializeExecutionTarget(context.Context, ExecutionTargetMaterializeRequest) (ExecutionTargetMaterialization, error)
	RestoreExecutionTarget(context.Context, workflow.ExecutionTargetRestoreRequest) error
	InspectExecutionTarget(context.Context, workflow.ExecutionTargetRestoreRequest) error
	InspectReplacementBranch(context.Context, workflow.TaskID, *string) error
}

type InitialTaskBranchInspectionRequest struct {
	SourceWorkspaceRoot string
	BranchName          string
}

type InitialTaskBranchAssertionRequest struct {
	TaskID     workflow.TaskID
	BranchName string
}

type ExecutionTargetResolveRequest struct {
	SourceWorkspaceRoot string
	Selection           workflow.ExecutionTargetSelection
}

type ExecutionTargetMaterializeRequest struct {
	Purpose                worktree.TaskExecutionRootPreparationPurpose
	TaskID                 workflow.TaskID
	SetupOperationID       *worktreecontract.SetupOperationID
	Snapshot               workflowstore.ExecutionTargetSnapshot
	SetupRequirement       worktreecontract.SetupRequirement
	InitialBranchAssertion *string
}

type ExecutionTargetMaterialization struct {
	RetainedRoot             *workflowstore.ManagedExecutionRoot
	SetupResult              *worktree.WorktreeSetupResult
	RetainedWorktree         *worktreepb.RegisteredFacts
	RetainedPreviousWorktree *worktreepb.RetainedPreviousWorktree
}

var errExecutionTargetInfrastructureRequired = errors.New("execution target infrastructure is required")

type taskWorktreeDeleter interface {
	EnsureTaskWorktreeDeletable(ctx context.Context, taskID string) error
	DeleteTaskWorktree(ctx context.Context, taskID string) error
}

type workflowAttentionFinalizer interface {
	FinalizeTaskResolution(workflowstore.TaskAttentionResolution)
	PublishPendingApproval(context.Context, workflow.ApprovalID)
}

type workflowTaskSetupEventPublisher interface {
	PublishWorkflowTaskSetupEvent(*worktreepb.SetupEvent)
}

const (
	workflowAttentionFinalizationTimeout = 5 * time.Second
)

type Option func(*Service)

func WithCurrentNodeExecution(execution taskExecution) Option {
	return func(s *Service) {
		s.currentNodeExecution = execution
	}
}

func WithExecutionTargetInfrastructure(infrastructure executionTargetInfrastructure) Option {
	return func(s *Service) {
		s.executionTargets = infrastructure
	}
}

func WithTaskWorktreeDeleter(deleter taskWorktreeDeleter) Option {
	return func(s *Service) {
		s.taskWorktreeCleanup = deleter
	}
}

func WithWorkflowAttentionFinalizer(finalizer workflowAttentionFinalizer) Option {
	return func(s *Service) {
		s.attentionFinalizer = finalizer
	}
}

func WithWorkflowTaskSetupEventPublisher(publisher workflowTaskSetupEventPublisher) Option {
	return func(s *Service) {
		s.setupEvents = publisher
	}
}

func New(store *workflowstore.Store, readModels ReadModels, roleResolver workflow.RoleResolver, taskMutations *workflowexecution.TaskMutationCoordinator, opts ...Option) (*Service, error) {
	if store == nil {
		return nil, errors.New("workflow store is required")
	}
	if taskMutations == nil {
		return nil, errors.New("task mutation coordinator is required")
	}
	if err := readModels.validate(); err != nil {
		return nil, err
	}
	events := newWorkflowProjectEventBroker()
	store.SetWorkflowEventPublisher(events)
	service := &Service{store: store, readModels: readModels, roleResolver: roleResolver, events: events, taskMutations: taskMutations}
	for _, opt := range opts {
		opt(service)
	}
	if service.currentNodeExecution == nil {
		return nil, errors.New("current node workflow execution is required")
	}
	return service, nil
}

func (s *Service) CreateWorkflow(ctx context.Context, req *pb.CreateRequest) (*pb.CreateSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	created, err := s.store.CreateWorkflow(ctx, workflowstore.CreateWorkflowRequest{Name: req.Name, Description: req.Description})
	if err != nil {
		return nil, err
	}
	record, err := workflowview.ProjectRecord(created)
	if err != nil {
		return nil, err
	}
	return &pb.CreateSuccess{Workflow: record}, nil
}

func (s *Service) CreateAndLinkWorkflowToProject(ctx context.Context, request *pb.CreateAndLinkProjectRequest) (*pb.CreateAndLinkProjectSuccess, error) {
	if err := protoapi.Validate(request); err != nil {
		return nil, err
	}
	created, link, err := s.store.CreateAndLinkWorkflow(ctx, workflowstore.CreateAndLinkWorkflowRequest{
		Name:          request.Name,
		Description:   request.Description,
		ProjectID:     request.ProjectId,
		DefaultPolicy: workflowStoreDefaultPolicy(request.GetDefaultPolicy()),
	})
	if err != nil {
		return nil, err
	}
	s.publishProjectWorkflowEvent(ctx, request.ProjectId, created.ID, serverapi.WorkflowProjectEventResourceWorkflowLink, serverapi.WorkflowProjectEventActionLinked, link.ID)
	record, err := workflowview.ProjectRecord(created)
	if err != nil {
		return nil, err
	}
	return &pb.CreateAndLinkProjectSuccess{Workflow: record, Link: projectWorkflowLink(link)}, nil
}

func (s *Service) publishWorkflowEvent(ctx context.Context, event workflowstore.WorkflowEventRecord) {
	if err := s.store.PublishWorkflowEvent(ctx, event); err != nil {
		slog.Warn("publish workflow event failed", "project_id", event.ProjectID, "workflow_id", event.WorkflowID, "resource", event.Resource, "action", event.Action, "primary_entity_id", event.PrimaryEntityID, "related_ids", event.RelatedIDs, "error", err)
	}
}

func (s *Service) publishProjectWorkflowEvent(ctx context.Context, projectID string, workflowID runtimeids.WorkflowID, resource serverapi.WorkflowProjectEventResource, action serverapi.WorkflowProjectEventAction, primaryEntityID string, relatedIDs ...string) {
	s.publishWorkflowEvent(ctx, workflowstore.WorkflowEventRecord{
		ProjectID:       &projectID,
		WorkflowID:      &workflowID,
		Resource:        resource,
		Action:          action,
		PrimaryEntityID: primaryEntityID,
		RelatedIDs:      relatedIDs,
	})
}

func (s *Service) publishGlobalWorkflowEvent(ctx context.Context, workflowID runtimeids.WorkflowID, resource serverapi.WorkflowProjectEventResource, action serverapi.WorkflowProjectEventAction, primaryEntityID string, relatedIDs ...string) {
	s.publishWorkflowEvent(ctx, workflowstore.WorkflowEventRecord{
		WorkflowID:      &workflowID,
		Resource:        resource,
		Action:          action,
		PrimaryEntityID: primaryEntityID,
		RelatedIDs:      relatedIDs,
	})
}

func (s *Service) publishLinkedWorkflowEvent(ctx context.Context, workflowID runtimeids.WorkflowID, resource serverapi.WorkflowProjectEventResource, action serverapi.WorkflowProjectEventAction, primaryEntityID string, relatedIDs ...string) {
	s.publishGlobalWorkflowEvent(ctx, workflowID, resource, action, primaryEntityID, relatedIDs...)
	links, err := s.store.ListWorkflowProjectLinks(ctx, workflowID)
	if err != nil {
		slog.Warn("list workflow project links for event failed", "workflow_id", workflowID.String(), "resource", resource, "action", action, "primary_entity_id", strings.TrimSpace(primaryEntityID), "related_ids", relatedIDs, "error", err)
		return
	}
	seen := map[string]bool{}
	for _, link := range links {
		projectID := strings.TrimSpace(link.ProjectID)
		if projectID == "" || seen[projectID] {
			continue
		}
		seen[projectID] = true
		s.publishProjectWorkflowEvent(ctx, projectID, workflowID, resource, action, primaryEntityID, relatedIDs...)
	}
}

func (s *Service) UpdateWorkflow(ctx context.Context, req *pb.UpdateRequest) (*pb.GetSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateWorkflowInfo(ctx, workflowID, req.Name, req.Description); err != nil {
		return nil, err
	}
	s.publishLinkedWorkflowEvent(ctx, workflowID, serverapi.WorkflowProjectEventResourceWorkflow, serverapi.WorkflowProjectEventActionUpdated, workflowID.String())
	return s.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: workflowID.String()})
}

func (s *Service) ListWorkflows(ctx context.Context, req *pb.ListRequest) (*pb.ListSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	limit := serverapi.OffsetPaginationMaxLimit
	if req.Limit != nil {
		limit = int(*req.Limit)
	}
	var workflowID *runtimeids.WorkflowID
	if req.WorkflowId != nil {
		id, err := runtimeids.ParseWorkflowID(*req.WorkflowId)
		if err != nil {
			return nil, err
		}
		workflowID = &id
	}
	rows, err := s.store.ListWorkflows(ctx, workflowstore.ListWorkflowsRequest{
		Offset:     int(req.GetOffset()),
		Limit:      limit,
		Query:      req.Query,
		ProjectID:  req.ProjectId,
		WorkflowID: workflowID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*pb.WorkflowRecord, 0, len(rows.Workflows))
	for _, row := range rows.Workflows {
		record, err := workflowview.ProjectRecord(row)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	var nextOffset *int64
	if rows.NextOffset != nil {
		offset := int64(*rows.NextOffset)
		nextOffset = &offset
	}
	return &pb.ListSuccess{Workflows: out, ProjectId: rows.ProjectID, NextOffset: nextOffset}, nil
}

func (s *Service) GetWorkflow(ctx context.Context, req *pb.GetRequest) (*pb.GetSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	def, _, err := s.readModels.Definitions.GetDefinition(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	return &pb.GetSuccess{Definition: def}, nil
}

func (s *Service) LinkWorkflowToProject(ctx context.Context, request *pb.LinkProjectRequest) (*pb.LinkProjectSuccess, error) {
	if err := protoapi.Validate(request); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(request.WorkflowId)
	if err != nil {
		return nil, err
	}
	link, err := s.store.LinkWorkflowWithDefaultPolicy(ctx, request.ProjectId, workflowID, workflowStoreDefaultPolicy(request.GetDefaultPolicy()))
	if err != nil {
		return nil, err
	}
	s.publishProjectWorkflowEvent(ctx, request.ProjectId, workflowID, serverapi.WorkflowProjectEventResourceWorkflowLink, serverapi.WorkflowProjectEventActionLinked, link.ID)
	return &pb.LinkProjectSuccess{Link: projectWorkflowLink(link)}, nil
}

func (s *Service) ListProjectWorkflowLinks(ctx context.Context, req *pb.ListProjectLinksRequest) (*pb.ListProjectLinksSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	links, err := s.store.ListProjectWorkflowLinks(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	out := make([]*pb.ProjectWorkflowLink, 0, len(links))
	for _, link := range links {
		out = append(out, projectWorkflowLink(link))
	}
	return &pb.ListProjectLinksSuccess{Links: out}, nil
}

func (s *Service) SetDefaultProjectWorkflowLink(ctx context.Context, req *pb.SetDefaultProjectLinkRequest) (*pb.SetDefaultProjectLinkSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	link, err := s.store.SetDefaultProjectWorkflowLink(ctx, req.ProjectId, workflowID)
	if err != nil {
		return nil, err
	}
	s.publishProjectWorkflowEvent(ctx, req.ProjectId, workflowID, serverapi.WorkflowProjectEventResourceWorkflowLink, serverapi.WorkflowProjectEventActionDefaultChanged, link.ID)
	return &pb.SetDefaultProjectLinkSuccess{Link: projectWorkflowLink(link)}, nil
}

func (s *Service) UnlinkWorkflowFromProject(ctx context.Context, req *pb.UnlinkProjectRequest) (*pb.UnlinkProjectSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	result, err := s.store.UnlinkProjectWorkflow(ctx, req.LinkId, req.GetReplacementDefaultLinkId())
	resp := workflowUnlinkProjectResponse(result)
	if err != nil {
		return resp, err
	}
	if result.Unlinked {
		s.publishProjectWorkflowEvent(ctx, result.ProjectID, result.WorkflowID, serverapi.WorkflowProjectEventResourceWorkflowLink, serverapi.WorkflowProjectEventActionUnlinked, req.LinkId)
	}
	return resp, nil
}

func (s *Service) PreviewWorkflowDelete(ctx context.Context, req *pb.DeletePreviewRequest) (*pb.DeletePreviewSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	impact, err := s.store.PreviewWorkflowDelete(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	return &pb.DeletePreviewSuccess{Impact: workflowDeleteImpact(impact)}, nil
}

func (s *Service) DeleteWorkflow(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	taskIDs, err := s.store.ListWorkflowTaskIDs(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	var response *pb.DeleteSuccess
	err = s.taskMutations.RunMany(ctx, taskIDs, func(ctx context.Context) error {
		if err := s.ensureWorkflowTasksQuiescent(ctx, workflowID); err != nil {
			return err
		}
		var err error
		response, err = s.deleteWorkflow(ctx, req)
		return err
	})
	return response, err
}

func (s *Service) ensureWorkflowTasksQuiescent(ctx context.Context, workflowID runtimeids.WorkflowID) error {
	if s == nil || s.currentNodeExecution == nil {
		return errors.New("current node workflow execution is required")
	}
	taskIDs, err := s.store.ListWorkflowTaskIDs(ctx, workflowID)
	if err != nil {
		return err
	}
	for _, taskID := range taskIDs {
		if err := s.currentNodeExecution.EnsureTaskQuiescent(taskID); err != nil {
			return err
		}
	}
	return nil
}

func runWorkflowGraphMutation[T any](ctx context.Context, service *Service, workflowID runtimeids.WorkflowID, mutation func(context.Context) (T, error)) (T, error) {
	var result T
	if service == nil {
		return result, errors.New("workflow service is required")
	}
	taskIDs, err := service.store.ListWorkflowTaskIDs(ctx, workflowID)
	if err != nil {
		return result, err
	}
	err = service.taskMutations.RunMany(ctx, taskIDs, func(ctx context.Context) error {
		var mutationErr error
		result, mutationErr = mutation(ctx)
		return mutationErr
	})
	return result, err
}

func (s *Service) deleteWorkflow(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListWorkflowProjectLinks(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	result, err := s.store.DeleteWorkflow(ctx, workflowstore.WorkflowDeleteRequest{
		WorkflowID:           workflowID,
		Confirmed:            req.Confirmed,
		ExpectedVersion:      req.ExpectedVersion,
		ExpectedProjectCount: req.ExpectedProjectCount,
		ExpectedLinkCount:    req.ExpectedLinkCount,
		ExpectedTaskCount:    req.ExpectedTaskCount,
		CleanupArtifacts:     req.CleanupArtifacts,
	})
	if err != nil {
		return nil, err
	}
	resp := workflowDeleteResponse(result)
	if !resp.Deleted {
		return resp, nil
	}
	s.finalizeWorkflowAttentionResolution(ctx, result)
	s.publishGlobalWorkflowEvent(ctx, workflowID, serverapi.WorkflowProjectEventResourceWorkflow, serverapi.WorkflowProjectEventActionDeleted, workflowID.String())
	seen := map[string]bool{}
	for _, link := range links {
		projectID := strings.TrimSpace(link.ProjectID)
		if projectID == "" || seen[projectID] {
			continue
		}
		seen[projectID] = true
		s.publishProjectWorkflowEvent(ctx, projectID, workflowID, serverapi.WorkflowProjectEventResourceWorkflow, serverapi.WorkflowProjectEventActionDeleted, workflowID.String())
	}
	return resp, nil
}

func (s *Service) ValidateWorkflow(ctx context.Context, req *pb.ValidateRequest) (*pb.ValidateResponse, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	def, _, err := s.store.GetDefinition(ctx, id)
	if err != nil {
		return nil, err
	}
	mode := workflow.ValidationContextDraft
	if req.Mode != nil {
		value, err := protoapi.WorkflowValidationMode.Decode(*req.Mode)
		if err != nil {
			return nil, err
		}
		mode = workflow.ValidationContext(value)
	}
	result := workflowscript.EvaluateDefinition(def, []workflow.ValidationContext{mode}, s.roleResolver, nil)[mode]
	return workflowValidationResponse(def.ID, result)
}

func (s *Service) ValidateWorkflowScriptPath(ctx context.Context, req *pb.ScriptPathValidateRequest) (*pb.ValidateResponse, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	def, _, err := s.store.GetDefinition(ctx, id)
	if err != nil {
		return nil, err
	}
	diagnostics := workflowscript.Validate(workflowscript.ValidationRequest{RawPath: req.ScriptPath})
	validationErrors := make([]workflow.ValidationError, 0, len(diagnostics))
	nodeID := workflow.NodeID(req.NodeId)
	for _, diagnostic := range diagnostics {
		validationErrors = append(validationErrors, workflow.ValidationError{
			Code: workflow.ValidationErrorCode(diagnostic.Code), Message: diagnostic.Message,
			NodeID: &nodeID, BlocksContext: diagnostic.Blocking,
		})
	}
	return workflowValidationResponse(def.ID, workflow.ValidationResult{Errors: validationErrors})
}

func (s *Service) ValidateWorkflowGraphDraft(ctx context.Context, req *pb.GraphValidateDraftRequest) (*pb.GraphValidateDraftSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	def, err := s.workflowGraphDraftDefinition(ctx, id, req.Metadata, req.Graph)
	if err != nil {
		return nil, err
	}
	results, err := s.workflowGraphValidationResultsForDefinition(def, req.Modes)
	if err != nil {
		return nil, err
	}
	wiring, err := workflowview.DerivedWiring(def, s.roleResolver)
	if err != nil {
		return nil, err
	}
	return &pb.GraphValidateDraftSuccess{Results: results, DerivedWiring: wiring}, nil
}

func (s *Service) DeriveWorkflowGraphWiring(ctx context.Context, req *pb.GraphDeriveWiringRequest) (*pb.GraphDeriveWiringSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	def, err := s.workflowGraphDraftDefinition(ctx, id, nil, req.Graph)
	if err != nil {
		return nil, err
	}
	wiring, err := workflowview.DerivedWiring(def, s.roleResolver)
	if err != nil {
		return nil, err
	}
	return &pb.GraphDeriveWiringSuccess{DerivedWiring: wiring}, nil
}

func (s *Service) PreviewWorkflowGraphSave(ctx context.Context, req *pb.GraphSavePreviewRequest) (*pb.GraphSavePreviewSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	storeRequest, err := workflowGraphStoreSaveRequest(id, req.ExpectedVersion, req.Metadata, req.Graph, nil)
	if err != nil {
		return nil, err
	}
	result, err := s.store.PreviewWorkflowGraphSave(ctx, storeRequest)
	if err != nil {
		return nil, err
	}
	projected, err := workflowGraphSaveResponse(result)
	if err != nil {
		return nil, err
	}
	return &pb.GraphSavePreviewSuccess{
		CurrentVersion: projected.CurrentVersion, Changed: projected.Changed,
		ValidationResults: projected.ValidationResults, Impact: projected.Impact, Blockers: projected.Blockers,
		CanSave: projected.CanSave, ConfirmationRequired: projected.ConfirmationRequired,
	}, nil
}

func (s *Service) SaveWorkflowGraph(ctx context.Context, req *pb.GraphSaveRequest) (*pb.GraphSaveSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	id, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	result, err := runWorkflowGraphMutation(ctx, s, id, func(ctx context.Context) (workflowstore.WorkflowGraphSaveResult, error) {
		storeRequest, err := workflowGraphStoreSaveRequest(id, req.ExpectedVersion, req.Metadata, req.Graph, req.Confirmation)
		if err != nil {
			return workflowstore.WorkflowGraphSaveResult{}, err
		}
		return s.store.SaveWorkflowGraph(ctx, storeRequest)
	})
	if err != nil {
		return nil, err
	}
	resp, err := workflowGraphSaveResponse(result)
	if err != nil {
		return nil, err
	}
	if result.Saved {
		definition, _, err := workflowview.ProjectDefinition(result.Definition, result.Record, s.roleResolver)
		if err != nil {
			return nil, err
		}
		resp.Definition = definition
		resp.CurrentVersion = result.Record.Version
		if result.Changed {
			s.publishLinkedWorkflowEvent(ctx, id, serverapi.WorkflowProjectEventResourceWorkflow, serverapi.WorkflowProjectEventActionGraphSaved, id.String())
		}
	}
	return resp, nil
}

func (s *Service) CreateWorkflowTask(ctx context.Context, req *taskpb.CreateRequest) (*taskpb.CreateSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	var workflowID *runtimeids.WorkflowID
	if req.WorkflowId != nil {
		id, err := runtimeids.ParseWorkflowID(*req.WorkflowId)
		if err != nil {
			return nil, err
		}
		workflowID = &id
	}
	taskRequest := workflowstore.CreateTaskRequest{
		ProjectID:         req.ProjectId,
		WorkflowID:        workflowID,
		Title:             req.Title,
		Body:              req.GetBody(),
		SourceURL:         req.GetSourceUrl(),
		SourceWorkspaceID: req.GetSourceWorkspaceId(),
		LabelIDs:          req.LabelIds,
	}
	for _, requestedIntent := range req.DependencyIntents {
		intent := workflow.TaskDependencyCreateIntent{
			RelatedTaskID: workflow.TaskID(requestedIntent.RelatedTaskId),
		}
		switch requestedIntent.NewTaskRole {
		case taskpb.DependencyRole_DEPENDENCY_ROLE_BLOCKER:
			intent.NewTaskRole = workflow.TaskDependencyRoleBlocker
		case taskpb.DependencyRole_DEPENDENCY_ROLE_BLOCKED:
			intent.NewTaskRole = workflow.TaskDependencyRoleBlocked
		}
		taskRequest.DependencyIntents = append(taskRequest.DependencyIntents, intent)
	}
	task, err := s.store.CreateTask(ctx, taskRequest)
	if err != nil {
		return nil, err
	}
	s.publishProjectWorkflowEvent(ctx, task.ProjectID, task.WorkflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionCreated, string(task.ID))
	if len(req.DependencyIntents) > 0 {
		relatedIDs := make([]string, 0, len(req.DependencyIntents))
		for _, intent := range req.DependencyIntents {
			relatedIDs = append(relatedIDs, intent.RelatedTaskId)
		}
		s.publishProjectWorkflowEvent(ctx, task.ProjectID, task.WorkflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionDependenciesChanged, string(task.ID), relatedIDs...)
	}
	detail, err := s.readModels.TaskDetail.GetTask(ctx, string(task.ID))
	if err != nil {
		return nil, err
	}
	return &taskpb.CreateSuccess{Task: detail.Summary}, nil
}

func (s *Service) AddWorkflowTaskDependency(ctx context.Context, req *taskpb.DependencyAddRequest) (*taskpb.DependencyMutationSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	result, err := s.store.AddTaskDependency(ctx, workflowstore.TaskDependencyAddRequest{
		BlockerTaskID: workflow.TaskID(req.BlockerTaskId),
		BlockedTaskID: workflow.TaskID(req.BlockedTaskId),
	})
	if err != nil {
		return nil, err
	}
	if result.Outcome == workflowstore.TaskDependencyAdded {
		s.publishProjectWorkflowEvent(ctx, result.ProjectID, result.WorkflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionDependenciesChanged, string(result.BlockerTaskID), string(result.BlockedTaskID))
	}
	return workflowDependencyMutationSuccess(result.Outcome, result.BlockerTaskID, result.BlockerShortID, result.BlockedTaskID, result.BlockedShortID)
}

func (s *Service) RemoveWorkflowTaskDependency(ctx context.Context, req *taskpb.DependencyRemoveRequest) (*taskpb.DependencyMutationSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	result, err := s.store.RemoveTaskDependency(ctx, workflowstore.TaskDependencyRemoveRequest{
		BlockerTaskID: workflow.TaskID(req.BlockerTaskId),
		BlockedTaskID: workflow.TaskID(req.BlockedTaskId),
	})
	if err != nil {
		return nil, err
	}
	if result.Outcome == workflowstore.TaskDependencyRemoved {
		s.publishProjectWorkflowEvent(ctx, result.ProjectID, result.WorkflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionDependenciesChanged, string(result.BlockerTaskID), string(result.BlockedTaskID))
	}
	return workflowDependencyMutationSuccess(result.Outcome, result.BlockerTaskID, result.BlockerShortID, result.BlockedTaskID, result.BlockedShortID)
}

func workflowDependencyMutationSuccess(
	outcome workflowstore.TaskDependencyMutationOutcome,
	blockerID workflow.TaskID, blockerShortID string,
	blockedID workflow.TaskID, blockedShortID string,
) (*taskpb.DependencyMutationSuccess, error) {
	code, err := protoapi.TaskDependencyMutationOutcome.Encode(string(outcome))
	if err != nil {
		return nil, err
	}
	return &taskpb.DependencyMutationSuccess{
		Outcome: code, BlockerTaskId: string(blockerID), BlockerShortId: blockerShortID,
		BlockedTaskId: string(blockedID), BlockedShortId: blockedShortID,
	}, nil
}

func (s *Service) ListWorkflowTaskDependencies(ctx context.Context, req *taskpb.DependencyListRequest) (*taskpb.DependencyListSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	return s.readModels.TaskDependencies.ListTaskDependencies(ctx, req.TaskId, req.Direction)
}

func workflowTaskStartError(err error) error {
	var conflict workflowstore.TaskStartConflictError
	if !errors.As(err, &conflict) {
		return err
	}
	switch conflict.Reason {
	case workflowstore.TaskStartConflictAlreadyStarted:
		return &serverapi.WorkflowTaskStartConflictError{
			TaskID: string(conflict.TaskID),
			Reason: serverapi.WorkflowTaskStartConflictAlreadyStarted,
		}
	default:
		return err
	}
}

func (s *Service) UpdateWorkflowTask(ctx context.Context, req *taskpb.UpdateRequest) (*taskpb.UpdateSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	task, err := s.store.UpdateTask(ctx, workflowstore.UpdateTaskRequest{TaskID: workflow.TaskID(req.TaskId), Title: req.Title, Body: req.Body, SourceWorkspaceID: req.GetSourceWorkspaceId()})
	if err != nil {
		return nil, err
	}
	s.publishProjectWorkflowEvent(ctx, task.ProjectID, task.WorkflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionUpdated, string(task.ID))
	detail, err := s.readModels.TaskDetail.GetTask(ctx, string(task.ID))
	if err != nil {
		return nil, err
	}
	return &taskpb.UpdateSuccess{Task: detail.Summary}, nil
}

func (s *Service) StartWorkflowTask(ctx context.Context, req *taskpb.StartRequest) (*taskpb.StartSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflowTaskMutation(ctx, workflow.TaskID(req.TaskId), req.InvokingSessionId); err != nil {
		return nil, err
	}
	if s.currentNodeExecution == nil {
		return nil, errors.New("current node workflow execution is required")
	}
	var response *taskpb.StartSuccess
	err := s.currentNodeExecution.RunTaskOperation(ctx, func(ctx context.Context) error {
		var err error
		response, err = workflowexecution.RunTaskMutation(ctx, s.taskMutations, workflow.TaskID(req.TaskId), func(ctx context.Context) (*taskpb.StartSuccess, error) {
			return s.startWorkflowTask(ctx, req)
		})
		return err
	})
	return response, err
}

func (s *Service) startWorkflowTask(ctx context.Context, req *taskpb.StartRequest) (*taskpb.StartSuccess, error) {
	taskID := workflow.TaskID(req.TaskId)
	selection, err := workflowTaskExecutionSelection(req.ExecutionTarget)
	if err != nil {
		return nil, err
	}
	preflight, err := workflowexecution.RunTaskMutation(ctx, s.taskMutations, taskID, func(ctx context.Context) (initiatingActionPreflight, error) {
		if err := s.currentNodeExecution.EnsureTaskQuiescent(taskID); err != nil {
			return initiatingActionPreflight{}, err
		}
		if err := s.store.ValidateTaskStart(ctx, taskID); err != nil {
			return initiatingActionPreflight{}, err
		}
		if req.ProceedDespiteDependencies {
			target, err := s.preflightInitiatingActionTarget(ctx, taskID, selection, req.BranchName)
			return initiatingActionPreflight{target: target}, err
		}
		count, err := s.readModels.TaskDependencies.CountUnsatisfiedBlockers(ctx, req.TaskId)
		if err != nil {
			return initiatingActionPreflight{}, err
		}
		if count > 0 {
			return initiatingActionPreflight{unsatisfiedDependencyCount: count}, nil
		}
		target, err := s.preflightInitiatingActionTarget(ctx, taskID, selection, req.BranchName)
		return initiatingActionPreflight{target: target}, err
	})
	if err != nil {
		return nil, workflowTaskStartError(err)
	}
	if preflight.unsatisfiedDependencyCount > 0 {
		count, err := protoapi.Int32(preflight.unsatisfiedDependencyCount, "unsatisfied_dependency_count")
		if err != nil {
			return nil, err
		}
		return &taskpb.StartSuccess{
			Outcome: &taskpb.StartSuccess_DependencyConfirmationRequired{
				DependencyConfirmationRequired: &taskpb.DependencyConfirmationRequired{UnsatisfiedDependencyCount: count},
			},
		}, nil
	}
	target := preflight.target
	if target.context.Task.ExecutionTarget == nil && !target.explicit &&
		target.context.Policy.Mode == workflow.ExecutionTargetModeAskOnFirstExecution {
		return &taskpb.StartSuccess{
			Outcome: &taskpb.StartSuccess_SelectionRequired{SelectionRequired: serverapi.NewWorkflowPolicyTargetSelectionRequirement()},
		}, nil
	}
	setupOperationID, err := worktreecontract.ParseSetupOperationID(req.SetupOperationId)
	if err != nil {
		return nil, err
	}
	observation, err := newTaskSetupObservation(setupOperationID, target.selection, s.setupEvents)
	if err != nil {
		return nil, err
	}
	decision, err := s.initiatingActionTarget(ctx, taskID, &setupOperationID, target)
	var prepared preparedInitiatingActionTarget
	if decision.prepared != nil {
		prepared = *decision.prepared
	}
	if err != nil {
		observation.finish(prepared, err)
		return nil, err
	}
	if decision.selectionRequired != nil {
		return &taskpb.StartSuccess{
			Outcome: &taskpb.StartSuccess_SelectionRequired{SelectionRequired: decision.selectionRequired},
		}, nil
	}
	started, err := s.currentNodeExecution.StartTask(
		ctx,
		taskID,
		prepared.candidate,
	)
	observation.finish(prepared, err)
	if err != nil {
		return nil, workflowTaskStartError(err)
	}
	if len(started.Mutation.Created) != 1 {
		return nil, errors.New("task start did not create exactly one current node")
	}
	if detail, detailErr := s.readModels.TaskDetail.GetTask(ctx, req.TaskId); detailErr == nil {
		workflowID, err := runtimeids.ParseWorkflowID(detail.Summary.WorkflowId)
		if err != nil {
			return nil, err
		}
		s.publishProjectWorkflowEvent(ctx, detail.Summary.ProjectId, workflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionStarted, req.TaskId)
	}
	return &taskpb.StartSuccess{
		Outcome: &taskpb.StartSuccess_Applied{
			Applied: &taskpb.StartApplied{CurrentNodes: workflowview.ProjectCurrentNodes(started.Mutation.Created)},
		},
	}, nil
}

func workflowTaskExecutionSelection(value *taskpb.ExecutionTargetSelection) (*workflow.ExecutionTargetSelection, error) {
	if value == nil {
		return nil, nil
	}
	mode, err := protoapi.WorkflowExecutionTargetMode.Decode(value.Mode)
	if err != nil {
		return nil, err
	}
	return &workflow.ExecutionTargetSelection{Mode: workflow.ExecutionTargetMode(mode), CustomRef: value.CustomRef}, nil
}

func (s *Service) prepareInitiatingActionTarget(
	ctx context.Context,
	taskID workflow.TaskID,
	setupOperationID *worktreecontract.SetupOperationID,
	target initiatingActionTargetPreflight,
) (preparedInitiatingActionTarget, error) {
	decision, preparationErr := s.initiatingActionTarget(ctx, taskID, setupOperationID, target)
	if decision.selectionRequired != nil && decision.selectionRequired.GetOriginalTargetUnavailable() != nil {
		return preparedInitiatingActionTarget{}, workflowexecution.NewTaskStartPreparationError(
			errors.New("original execution target requires selection"),
			workflow.CurrentNodeInterruptionDetail{
				Code:                               "workflow_original_target_unavailable",
				OriginalExecutionTargetUnavailable: decision.selectionRequired.GetOriginalTargetUnavailable(),
			},
		)
	}
	if target.context.Task.ExecutionTarget != nil && target.purpose != worktree.TaskExecutionRootReplacement {
		if preparationErr == nil && (decision.prepared != nil || decision.selectionRequired != nil) {
			preparationErr = errors.New("locked Task target returned an initial target decision")
		}
		return preparedInitiatingActionTarget{}, preparationErr
	}
	if decision.prepared == nil {
		return preparedInitiatingActionTarget{}, preparationErr
	}
	return *decision.prepared, preparationErr
}

func coordinateInitiatingAction[T any](ctx context.Context, service *Service, req initiatingActionRequest, apply func(context.Context, *workflowstore.ExecutionTargetCandidate) (*T, error)) (initiatingActionResult[T], error) {
	target := req.targetPreflight
	if req.requiresExecutionTarget && (target.context.Task.ExecutionTarget == nil || target.purpose == worktree.TaskExecutionRootReplacement) {
		if target.originalUnavailable != nil && !target.explicit {
			return initiatingActionResult[T]{selectionRequired: serverapi.NewWorkflowOriginalTargetSelectionRequirement(target.originalUnavailable.Cause)}, nil
		}
		var required *taskpb.SelectionRequired
		var err error
		_, required, err = service.resolveInitiatingActionTarget(ctx, target)
		if err != nil || required != nil {
			return initiatingActionResult[T]{selectionRequired: required}, err
		}
	}
	if req.afterTargetResolution != nil {
		if err := req.afterTargetResolution(); err != nil {
			return initiatingActionResult[T]{}, err
		}
	}
	return workflowexecution.RunTaskMutation(ctx, service.taskMutations, req.taskID, func(ctx context.Context) (initiatingActionResult[T], error) {
		if err := service.currentNodeExecution.EnsureTaskQuiescent(req.taskID); err != nil {
			return initiatingActionResult[T]{}, err
		}
		var prepared preparedInitiatingActionTarget
		if req.requiresExecutionTarget {
			var explicit *workflow.ExecutionTargetSelection
			if target.explicit {
				explicit = &target.selection
			}
			fresh, err := service.preflightInitiatingActionTarget(ctx, req.taskID, explicit, target.initialBranchAssertion)
			if err != nil {
				return initiatingActionResult[T]{}, err
			}
			if fresh.originalUnavailable != nil && !fresh.explicit {
				return initiatingActionResult[T]{selectionRequired: serverapi.NewWorkflowOriginalTargetSelectionRequirement(fresh.originalUnavailable.Cause)}, nil
			}
			if fresh.context.Task.ExecutionTarget == nil || fresh.purpose == worktree.TaskExecutionRootReplacement {
				snapshot, required, resolutionErr := service.resolveInitiatingActionTarget(ctx, fresh)
				if resolutionErr != nil || required != nil {
					return initiatingActionResult[T]{selectionRequired: required}, resolutionErr
				}
				prepared, err = service.materializeInitiatingActionTarget(ctx, req.taskID, req.setupOperationID, fresh, snapshot, worktreecontract.SetupRequirementRequired)
			} else {
				prepared, err = service.prepareInitiatingActionTarget(ctx, req.taskID, req.setupOperationID, fresh)
			}
			if err != nil {
				return initiatingActionResult[T]{retainedPreviousWorktree: prepared.retainedPreviousWorktree}, err
			}
		}
		applied, err := apply(ctx, prepared.candidate)
		return initiatingActionResult[T]{applied: applied, retainedPreviousWorktree: prepared.retainedPreviousWorktree}, err
	})
}

func (s *Service) preflightInitiatingActionTarget(
	ctx context.Context,
	taskID workflow.TaskID,
	explicit *workflow.ExecutionTargetSelection,
	requestedBranchName *string,
) (initiatingActionTargetPreflight, error) {
	targetContext, err := s.store.GetTaskExecutionTargetContext(ctx, taskID)
	if err != nil {
		return initiatingActionTargetPreflight{}, err
	}
	if targetContext.Task.ExecutionTarget != nil {
		selection := workflow.ExecutionTargetSelection{Mode: targetContext.Task.ExecutionTarget.Mode}
		if selection.Mode == workflow.ExecutionTargetModeCustomRef {
			selection.CustomRef = targetContext.Task.ExecutionTarget.RequestedRef
		}
		if targetContext.Task.ExecutionTarget.Mode != workflow.ExecutionTargetModeNone {
			if s.executionTargets == nil {
				return initiatingActionTargetPreflight{}, errExecutionTargetInfrastructureRequired
			}
			inspectionErr := s.executionTargets.InspectExecutionTarget(ctx, workflow.ExecutionTargetRestoreRequest{TaskID: taskID})
			var unavailable *serverapi.WorkflowLockedExecutionTargetError
			if errors.As(inspectionErr, &unavailable) {
				preflight := initiatingActionTargetPreflight{
					context: targetContext, selection: selection, explicit: explicit != nil,
					purpose:             worktree.TaskExecutionRootReplacement,
					originalUnavailable: unavailable, initialBranchAssertion: requestedBranchName,
				}
				if explicit != nil {
					preflight.selection = *explicit
					if explicit.Mode == workflow.ExecutionTargetModeNone && requestedBranchName != nil {
						return initiatingActionTargetPreflight{}, &serverapi.WorkflowTaskInitialBranchError{
							Reason: serverapi.WorkflowTaskInitialBranchErrorReasonNoManagedTarget, BranchName: *requestedBranchName,
						}
					}
				}
				return preflight, nil
			}
			if inspectionErr != nil {
				return initiatingActionTargetPreflight{}, inspectionErr
			}
		}
		if explicit != nil {
			return initiatingActionTargetPreflight{}, workflowstore.ErrExecutionTargetAlreadyLocked
		}
		branchAssertion, pendingBranchReplaced, err := s.preflightInitialTaskBranch(ctx, targetContext, selection, requestedBranchName)
		if err != nil {
			return initiatingActionTargetPreflight{}, err
		}
		return initiatingActionTargetPreflight{
			context:                targetContext,
			selection:              selection,
			initialBranchAssertion: branchAssertion,
			pendingBranchReplaced:  pendingBranchReplaced,
		}, nil
	}
	selection := workflow.ExecutionTargetSelection{
		Mode:      targetContext.Policy.Mode,
		CustomRef: targetContext.Policy.CustomRef,
	}
	if explicit == nil && targetContext.Policy.Mode == workflow.ExecutionTargetModeAskOnFirstExecution {
		return initiatingActionTargetPreflight{
			context:   targetContext,
			selection: selection,
		}, nil
	}
	if explicit != nil {
		selection = *explicit
	}
	if selection.Mode != workflow.ExecutionTargetModeNone || targetContext.Task.ManagedWorktreeID != nil {
		if s.executionTargets == nil {
			return initiatingActionTargetPreflight{}, errExecutionTargetInfrastructureRequired
		}
	}
	branchAssertion, pendingBranchReplaced, err := s.preflightInitialTaskBranch(ctx, targetContext, selection, requestedBranchName)
	if err != nil {
		return initiatingActionTargetPreflight{}, err
	}
	return initiatingActionTargetPreflight{
		context:                targetContext,
		selection:              selection,
		explicit:               explicit != nil,
		initialBranchAssertion: branchAssertion,
		pendingBranchReplaced:  pendingBranchReplaced,
	}, nil
}

func (s *Service) preflightInitialTaskBranch(
	ctx context.Context,
	targetContext workflowstore.TaskExecutionTargetContext,
	selection workflow.ExecutionTargetSelection,
	requestedBranchName *string,
) (*string, bool, error) {
	if selection.Mode == workflow.ExecutionTargetModeNone {
		if requestedBranchName != nil {
			return nil, false, &serverapi.WorkflowTaskInitialBranchError{
				Reason:     serverapi.WorkflowTaskInitialBranchErrorReasonNoManagedTarget,
				BranchName: *requestedBranchName,
			}
		}
		return nil, false, nil
	}
	if targetContext.Task.ManagedWorktreeID != nil {
		if requestedBranchName != nil {
			if err := s.executionTargets.AssertInitialTaskBranch(ctx, InitialTaskBranchAssertionRequest{
				TaskID:     targetContext.Task.ID,
				BranchName: *requestedBranchName,
			}); err != nil {
				return nil, false, err
			}
		}
		return requestedBranchName, false, nil
	}
	if targetContext.Task.ExecutionTarget != nil {
		if requestedBranchName != nil {
			return nil, false, operationCannotCreateInitialWorktreeError(*requestedBranchName)
		}
		return nil, false, nil
	}
	branchName := requestedBranchName
	if branchName == nil {
		branchName = targetContext.Task.PendingInitialManagedBranchName
	}
	if branchName == nil {
		return nil, false, fmt.Errorf("task %q has no pending initial managed branch", targetContext.Task.ID)
	}
	if err := s.executionTargets.InspectProspectiveInitialTaskBranch(ctx, InitialTaskBranchInspectionRequest{
		SourceWorkspaceRoot: targetContext.SourceWorkspaceRoot,
		BranchName:          *branchName,
	}); err != nil {
		return nil, false, err
	}
	if requestedBranchName != nil && targetContext.Task.ExecutionTarget == nil {
		if err := s.store.ReplacePendingInitialManagedBranchName(ctx, targetContext.Task.ID, *requestedBranchName); err != nil {
			return nil, false, err
		}
		return requestedBranchName, true, nil
	}
	return requestedBranchName, false, nil
}

func operationCannotCreateInitialWorktreeError(branchName string) *serverapi.WorkflowTaskInitialBranchError {
	return &serverapi.WorkflowTaskInitialBranchError{
		Reason:     serverapi.WorkflowTaskInitialBranchErrorReasonOperationCannotCreateWorktree,
		BranchName: branchName,
	}
}

func (s *Service) initiatingActionTarget(ctx context.Context, taskID workflow.TaskID, setupOperationID *worktreecontract.SetupOperationID, preflight initiatingActionTargetPreflight) (initiatingActionTargetDecision, error) {
	targetContext := preflight.context
	if preflight.purpose == worktree.TaskExecutionRootReplacement {
		if !preflight.explicit {
			return initiatingActionTargetDecision{selectionRequired: serverapi.NewWorkflowOriginalTargetSelectionRequirement(preflight.originalUnavailable.Cause)}, nil
		}
		return s.resolveAndMaterializeInitiatingActionTarget(ctx, taskID, setupOperationID, preflight)
	}
	if targetContext.Task.ExecutionTarget != nil {
		if targetContext.Task.ExecutionTarget.Mode != workflow.ExecutionTargetModeNone {
			branchAssertion := preflight.initialBranchAssertion
			if preflight.purpose == worktree.TaskExecutionRootReplacement {
				branchAssertion = nil
			}
			if err := s.executionTargets.RestoreExecutionTarget(ctx, workflow.ExecutionTargetRestoreRequest{
				TaskID:                 taskID,
				SetupOperationID:       setupOperationID,
				InitialBranchAssertion: branchAssertion,
			}); err != nil {
				var unavailable *serverapi.WorkflowLockedExecutionTargetError
				if !errors.As(err, &unavailable) {
					return initiatingActionTargetDecision{}, err
				}
				if !preflight.explicit {
					return initiatingActionTargetDecision{selectionRequired: serverapi.NewWorkflowOriginalTargetSelectionRequirement(unavailable.Cause)}, nil
				}
				return s.resolveAndMaterializeInitiatingActionTarget(ctx, taskID, setupOperationID, preflight)
			}
		}
		if preflight.purpose == worktree.TaskExecutionRootReplacement && (preflight.explicit || preflight.initialBranchAssertion != nil) {
			return initiatingActionTargetDecision{}, workflowstore.ErrExecutionTargetAlreadyLocked
		}
		return initiatingActionTargetDecision{}, nil
	}
	return s.resolveAndMaterializeInitiatingActionTarget(ctx, taskID, setupOperationID, preflight)
}

func (s *Service) resolveAndMaterializeInitiatingActionTarget(ctx context.Context, taskID workflow.TaskID, setupOperationID *worktreecontract.SetupOperationID, preflight initiatingActionTargetPreflight) (initiatingActionTargetDecision, error) {
	snapshot, selectionRequired, err := s.resolveInitiatingActionTarget(ctx, preflight)
	if err != nil || selectionRequired != nil {
		return initiatingActionTargetDecision{selectionRequired: selectionRequired}, err
	}
	prepared, err := s.materializeInitiatingActionTarget(ctx, taskID, setupOperationID, preflight, snapshot, worktreecontract.SetupRequirementRequired)
	return initiatingActionTargetDecision{prepared: &prepared}, err
}

func (s *Service) preflightManualMoveTarget(ctx context.Context, prepared workflowstore.ManualMovePreparation, explicit *workflow.ExecutionTargetSelection, branch *string) (initiatingActionTargetPreflight, error) {
	preflight, err := s.preflightInitiatingActionTarget(ctx, prepared.TaskID(), explicit, branch)
	if err == nil && prepared.ReopensCompletedTask() && preflight.context.Task.ExecutionTarget != nil &&
		preflight.purpose != worktree.TaskExecutionRootReplacement && branch != nil {
		return initiatingActionTargetPreflight{}, workflowstore.ErrExecutionTargetAlreadyLocked
	}
	return preflight, err
}

func (s *Service) resolveInitiatingActionTarget(
	ctx context.Context,
	preflight initiatingActionTargetPreflight,
) (*workflowstore.ExecutionTargetSnapshot, *taskpb.SelectionRequired, error) {
	targetContext := preflight.context
	if !preflight.explicit && targetContext.Policy.Mode == workflow.ExecutionTargetModeAskOnFirstExecution {
		return nil, serverapi.NewWorkflowPolicyTargetSelectionRequirement(), nil
	}
	selection := preflight.selection
	if selection.Mode == workflow.ExecutionTargetModeNone {
		if targetContext.Task.ManagedWorktreeID == nil {
			return nil, nil, nil
		}
		return &workflowstore.ExecutionTargetSnapshot{
			Mode: selection.Mode, Provenance: workflowstore.ExecutionTargetProvenanceResolved,
		}, nil, nil
	}
	snapshot, err := s.executionTargets.ResolveExecutionTarget(ctx, ExecutionTargetResolveRequest{
		SourceWorkspaceRoot: targetContext.SourceWorkspaceRoot,
		Selection:           selection,
	})
	if err != nil {
		if !preflight.explicit {
			if requirement, ok := configuredTargetSelectionRequirement(selection, err); ok {
				return nil, requirement, nil
			}
		}
		if preflight.explicit && selection.Mode == workflow.ExecutionTargetModeCustomRef {
			if resolutionErr, ok := explicitExecutionTargetResolutionError(err); ok {
				return nil, nil, resolutionErr
			}
		}
		return nil, nil, err
	}
	if snapshot.Mode != selection.Mode {
		return nil, nil, errors.New("resolved execution target mode does not match selection")
	}
	if err := snapshot.Validate(); err != nil {
		return nil, nil, err
	}
	if preflight.purpose == worktree.TaskExecutionRootReplacement {
		if err := s.executionTargets.InspectReplacementBranch(ctx, targetContext.Task.ID, preflight.initialBranchAssertion); err != nil {
			return nil, nil, err
		}
	}
	return &snapshot, nil, nil
}

func (s *Service) materializeInitiatingActionTarget(
	ctx context.Context,
	taskID workflow.TaskID,
	setupOperationID *worktreecontract.SetupOperationID,
	preflight initiatingActionTargetPreflight,
	snapshot *workflowstore.ExecutionTargetSnapshot,
	setupRequirement worktreecontract.SetupRequirement,
) (preparedInitiatingActionTarget, error) {
	targetContext := preflight.context
	if snapshot != nil {
		materialization, materializationErr := s.executionTargets.MaterializeExecutionTarget(ctx, ExecutionTargetMaterializeRequest{
			Purpose:                preflight.purpose,
			TaskID:                 taskID,
			SetupOperationID:       setupOperationID,
			Snapshot:               *snapshot,
			SetupRequirement:       setupRequirement,
			InitialBranchAssertion: preflight.initialBranchAssertion,
		})
		prepared := preparedInitiatingActionTarget{
			retainedWorktree:         materialization.RetainedWorktree,
			retainedPreviousWorktree: materialization.RetainedPreviousWorktree,
			setupResult:              materialization.SetupResult,
		}
		if materialization.SetupResult != nil {
			if validationErr := materialization.SetupResult.Validate(); validationErr != nil {
				return prepared, errors.Join(materializationErr, validationErr)
			}
			if failed := materialization.SetupResult.Failed; failed != nil {
				if materializationErr != nil {
					return prepared, materializationErr
				}
				if failed.RetainedWorktree != nil && failed.ScriptPath != nil {
					retained, err := worktreecontract.NewSetupRetainedError(
						failed.RetainedWorktree, *failed.ScriptPath, failed.Diagnostic,
						failed.RetainedPreviousWorktree, errors.New(failed.Diagnostic),
					)
					if err != nil {
						return prepared, err
					}
					retained.Details.RecoveryDisposition = failed.RecoveryDisposition
					return prepared, retained
				}
				return prepared, errors.New(failed.Diagnostic)
			}
		}
		if snapshot.Mode == workflow.ExecutionTargetModeNone {
			prepared.candidate = &workflowstore.ExecutionTargetCandidate{
				Snapshot: *snapshot,
				Root:     workflowstore.ExecutionRoot{SourceWorkspaceID: targetContext.SourceWorkspaceID, SourceWorkspaceRoot: targetContext.SourceWorkspaceRoot},
			}
			return prepared, materializationErr
		}
		if materialization.RetainedRoot == nil {
			if materializationErr == nil {
				return prepared, errors.New("execution target materialization returned no managed root")
			}
			return prepared, materializationErr
		}
		prepared.candidate = &workflowstore.ExecutionTargetCandidate{
			Snapshot: *snapshot,
			Root: workflowstore.ExecutionRoot{
				SourceWorkspaceID:   targetContext.SourceWorkspaceID,
				SourceWorkspaceRoot: targetContext.SourceWorkspaceRoot,
				Managed:             materialization.RetainedRoot,
			},
		}
		if err := prepared.candidate.Validate(); err != nil {
			return prepared, errors.Join(materializationErr, err)
		}
		return prepared, materializationErr
	}
	candidate := &workflowstore.ExecutionTargetCandidate{
		Snapshot: workflowstore.ExecutionTargetSnapshot{
			Mode:       workflow.ExecutionTargetModeNone,
			Provenance: workflowstore.ExecutionTargetProvenanceResolved,
		},
		Root: workflowstore.ExecutionRoot{
			SourceWorkspaceID:   targetContext.SourceWorkspaceID,
			SourceWorkspaceRoot: targetContext.SourceWorkspaceRoot,
		},
	}
	return preparedInitiatingActionTarget{candidate: candidate}, nil
}

func configuredTargetSelectionRequirement(selection workflow.ExecutionTargetSelection, err error) (*taskpb.SelectionRequired, bool) {
	unavailable, ok := configuredExecutionTargetUnavailable(selection, err)
	if !ok {
		return nil, false
	}
	return configuredTargetSelectionRequirementFromUnavailable(*unavailable), true
}

func configuredExecutionTargetUnavailable(
	selection workflow.ExecutionTargetSelection,
	err error,
) (*workflow.ConfiguredExecutionTargetUnavailable, bool) {
	cause, ok := executionTargetUnavailableCause(err)
	if !ok {
		return nil, false
	}
	unavailable := &workflow.ConfiguredExecutionTargetUnavailable{
		Mode:  selection.Mode,
		Cause: cause,
	}
	if selection.Mode == workflow.ExecutionTargetModeCustomRef {
		requestedRef := *selection.CustomRef
		unavailable.RequestedRef = &requestedRef
	}
	return unavailable, true
}

func configuredTargetSelectionRequirementFromUnavailable(
	unavailable workflow.ConfiguredExecutionTargetUnavailable,
) *taskpb.SelectionRequired {
	return serverapi.NewWorkflowConfiguredTargetSelectionRequirement(
		serverapi.WorkflowExecutionTargetMode(unavailable.Mode), unavailable.RequestedRef,
		serverapi.WorkflowExecutionTargetUnavailableCause(unavailable.Cause),
	)
}

func configuredTargetResumeSelection(nodes []workflow.CurrentNode) (*taskpb.SelectionRequired, error) {
	for _, node := range nodes {
		if node.Scheduling == nil || node.Scheduling.Interruption == nil {
			continue
		}
		unavailable := node.Scheduling.Interruption.Detail.ConfiguredExecutionTargetUnavailable
		if unavailable == nil {
			continue
		}
		if err := unavailable.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configured execution target interruption: %w", err)
		}
		return configuredTargetSelectionRequirementFromUnavailable(*unavailable), nil
	}
	return nil, nil
}

func executionTargetUnavailableCause(err error) (workflow.ExecutionTargetUnavailableCause, bool) {
	var revisionErr *worktree.GitRevisionResolutionError
	if errors.As(err, &revisionErr) {
		switch revisionErr.Kind {
		case worktree.GitRevisionResolutionErrorInvalidRevision:
			return workflow.ExecutionTargetUnavailableCauseInvalidRevision, true
		case worktree.GitRevisionResolutionErrorNonCommit:
			return workflow.ExecutionTargetUnavailableCauseNonCommit, true
		case worktree.GitRevisionResolutionErrorGitFailure:
			return workflow.ExecutionTargetUnavailableCauseGitFailure, true
		}
	}
	var defaultBranchErr *worktree.GitDefaultBranchResolutionError
	if errors.As(err, &defaultBranchErr) {
		switch defaultBranchErr.Kind {
		case worktree.GitDefaultBranchResolutionErrorMissing:
			return workflow.ExecutionTargetUnavailableCauseDefaultBranchMissing, true
		case worktree.GitDefaultBranchResolutionErrorAmbiguous:
			return workflow.ExecutionTargetUnavailableCauseDefaultBranchAmbiguous, true
		case worktree.GitDefaultBranchResolutionErrorGitFailure:
			return workflow.ExecutionTargetUnavailableCauseGitFailure, true
		}
	}
	return "", false
}

func explicitExecutionTargetResolutionError(err error) (*serverapi.WorkflowExecutionTargetResolutionError, bool) {
	var revisionErr *worktree.GitRevisionResolutionError
	if !errors.As(err, &revisionErr) {
		return nil, false
	}
	code := serverapi.WorkflowExecutionTargetResolutionErrorCode("")
	switch revisionErr.Kind {
	case worktree.GitRevisionResolutionErrorInvalidRevision:
		code = serverapi.WorkflowExecutionTargetResolutionErrorInvalidRevision
	case worktree.GitRevisionResolutionErrorNonCommit:
		code = serverapi.WorkflowExecutionTargetResolutionErrorNonCommit
	case worktree.GitRevisionResolutionErrorGitFailure:
		code = serverapi.WorkflowExecutionTargetResolutionErrorGitFailure
	default:
		return nil, false
	}
	return &serverapi.WorkflowExecutionTargetResolutionError{
		Code:         code,
		RequestedRef: revisionErr.RequestedRef,
	}, true
}

func (s *Service) InterruptWorkflowTask(ctx context.Context, req *taskpb.InterruptRequest) (*emptypb.Empty, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflowTaskMutation(ctx, workflow.TaskID(req.TaskId), req.InvokingSessionId); err != nil {
		return nil, err
	}
	selector := workflowexecution.InterruptSelector{TaskID: workflow.TaskID(req.TaskId)}
	if req.SessionId != nil {
		sessionID, err := runtimeids.ParseSessionID(*req.SessionId)
		if err != nil {
			return nil, err
		}
		selector.SessionID = &sessionID
	}
	if s.currentNodeExecution == nil {
		return nil, workflowexecution.ErrNoInterruptibleExecution
	}
	if err := s.currentNodeExecution.Interrupt(ctx, selector); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) ResumeWorkflowTask(ctx context.Context, req *taskpb.ResumeRequest) (*taskpb.ResumeSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflowTaskMutation(ctx, workflow.TaskID(req.TaskId), req.InvokingSessionId); err != nil {
		return nil, err
	}
	if s.currentNodeExecution == nil {
		return nil, errors.New("current node workflow execution is required")
	}
	var response *taskpb.ResumeSuccess
	err := s.currentNodeExecution.RunTaskOperation(ctx, func(ctx context.Context) error {
		var err error
		response, err = workflowexecution.RunTaskMutation(ctx, s.taskMutations, workflow.TaskID(req.TaskId), func(ctx context.Context) (*taskpb.ResumeSuccess, error) {
			return s.resumeWorkflowTaskAuthorized(ctx, req, workflow.TaskID(req.TaskId))
		})
		return err
	})
	return response, workflowContextSelectionError(err)
}

func (s *Service) resumeWorkflowTaskAuthorized(
	ctx context.Context,
	req *taskpb.ResumeRequest,
	taskID workflow.TaskID,
) (*taskpb.ResumeSuccess, error) {
	promoted, handled, err := s.currentNodeExecution.PromoteConcurrencyQueuedTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if handled {
		if detail, detailErr := s.readModels.TaskDetail.GetTask(ctx, req.TaskId); detailErr == nil {
			workflowID, err := runtimeids.ParseWorkflowID(detail.Summary.WorkflowId)
			if err != nil {
				return nil, err
			}
			s.publishProjectWorkflowEvent(
				ctx,
				detail.Summary.ProjectId,
				workflowID,
				serverapi.WorkflowProjectEventResourceTask,
				serverapi.WorkflowProjectEventActionResumed,
				req.TaskId,
			)
		}
		return &taskpb.ResumeSuccess{
			Outcome: &taskpb.ResumeSuccess_Applied{
				Applied: &taskpb.ResumeApplied{CurrentNodes: workflowview.ProjectCurrentNodes(promoted)},
			},
		}, nil
	}
	preflight, err := s.currentNodeExecution.PreflightTaskResume(ctx, taskID)
	if err != nil {
		return nil, err
	}
	switch preflight.Outcome {
	case workflowexecution.TaskResumePreflightNoOp:
		return &taskpb.ResumeSuccess{
			Outcome: &taskpb.ResumeSuccess_NoOp{
				NoOp: &taskpb.ResumeApplied{CurrentNodes: workflowview.ProjectCurrentNodes(preflight.CurrentNodes)},
			},
		}, nil
	case workflowexecution.TaskResumePreflightResumable:
	default:
		return nil, fmt.Errorf(
			"task resume preflight returned invalid outcome %q",
			preflight.Outcome,
		)
	}
	interrupted := preflight.CurrentNodes
	if req.ExecutionTarget == nil {
		selectionRequired, err := configuredTargetResumeSelection(interrupted)
		if err != nil {
			return nil, err
		}
		if selectionRequired != nil {
			return &taskpb.ResumeSuccess{
				Outcome: &taskpb.ResumeSuccess_SelectionRequired{SelectionRequired: selectionRequired},
			}, nil
		}
	}
	selection, err := workflowTaskExecutionSelection(req.ExecutionTarget)
	if err != nil {
		return nil, err
	}
	target, err := s.preflightInitiatingActionTarget(ctx, taskID, selection, req.BranchName)
	if err != nil {
		return nil, err
	}
	if target.originalUnavailable != nil && !target.explicit {
		return &taskpb.ResumeSuccess{
			Outcome: &taskpb.ResumeSuccess_SelectionRequired{
				SelectionRequired: serverapi.NewWorkflowOriginalTargetSelectionRequirement(target.originalUnavailable.Cause),
			},
		}, nil
	}
	if target.purpose == worktree.TaskExecutionRootReplacement {
		if err := s.currentNodeExecution.EnsureTaskQuiescent(taskID); err != nil {
			return nil, err
		}
	}
	setupOperationID, err := worktreecontract.ParseSetupOperationID(req.SetupOperationId)
	if err != nil {
		return nil, err
	}
	observation, err := newTaskSetupObservation(setupOperationID, target.selection, s.setupEvents)
	if err != nil {
		return nil, err
	}
	var prepared preparedInitiatingActionTarget
	if target.context.Task.ExecutionTarget == nil || target.purpose == worktree.TaskExecutionRootReplacement {
		snapshot, selectionRequired, resolutionErr := s.resolveInitiatingActionTarget(ctx, target)
		if resolutionErr != nil {
			return nil, resolutionErr
		}
		if selectionRequired != nil {
			return &taskpb.ResumeSuccess{
				Outcome: &taskpb.ResumeSuccess_SelectionRequired{SelectionRequired: selectionRequired},
			}, nil
		}
		prepared, err = s.materializeInitiatingActionTarget(ctx, taskID, &setupOperationID, target, snapshot, worktreecontract.SetupRequirementRequired)
	} else if target.context.Task.ExecutionTarget.Mode != workflow.ExecutionTargetModeNone {
		prepared, err = s.prepareInitiatingActionTarget(ctx, taskID, &setupOperationID, target)
	}
	if err != nil {
		observation.finish(prepared, err)
		return nil, err
	}
	resumeResult, err := s.currentNodeExecution.ResumeTask(ctx, taskID, prepared.candidate)
	observation.finish(prepared, err)
	if err != nil {
		return nil, err
	}
	if resumeResult.Outcome == workflowexecution.TaskResumeNoOp {
		return &taskpb.ResumeSuccess{
			Outcome: &taskpb.ResumeSuccess_NoOp{
				NoOp: &taskpb.ResumeApplied{CurrentNodes: workflowview.ProjectCurrentNodes(resumeResult.CurrentNodes)},
			},
		}, nil
	}
	resumed := resumeResult.CurrentNodes
	if len(resumed) == 0 {
		return nil, &workflowexecution.TaskResumeConflictError{TaskID: taskID}
	}
	if detail, detailErr := s.readModels.TaskDetail.GetTask(ctx, req.TaskId); detailErr == nil {
		workflowID, err := runtimeids.ParseWorkflowID(detail.Summary.WorkflowId)
		if err != nil {
			return nil, err
		}
		s.publishProjectWorkflowEvent(ctx, detail.Summary.ProjectId, workflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionResumed, req.TaskId)
	}
	return &taskpb.ResumeSuccess{
		Outcome: &taskpb.ResumeSuccess_Applied{
			Applied: &taskpb.ResumeApplied{CurrentNodes: workflowview.ProjectCurrentNodes(resumed)},
		},
	}, nil
}

func (s *Service) ApproveWorkflowTask(ctx context.Context, req *taskpb.ApproveRequest) (*taskpb.ApproveSuccess, error) {
	response, err := s.approveWorkflowTask(ctx, req)
	return response, workflowContextSelectionError(err)
}

func workflowContextSelectionError(err error) error {
	var restriction *workflowstore.TaskContextSelectionRequiredError
	if errors.As(err, &restriction) {
		return &serverapi.WorkflowTaskContextSelectionRequiredError{TaskID: string(restriction.TaskID)}
	}
	return err
}

func (s *Service) approveWorkflowTask(ctx context.Context, req *taskpb.ApproveRequest) (*taskpb.ApproveSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s.currentNodeExecution == nil {
		return nil, errors.New("current node workflow execution is required")
	}
	approvalID, err := workflow.ParseApprovalID(req.ApprovalId)
	if err != nil {
		return nil, err
	}
	if req.InvokingSessionId != nil {
		approval, err := s.store.PendingApproval(ctx, approvalID)
		if err != nil {
			return nil, err
		}
		if err := s.authorizeWorkflowTaskMutation(ctx, approval.Source.TaskID, req.InvokingSessionId); err != nil {
			return nil, err
		}
	}
	approved, err := s.currentNodeExecution.ApplyPendingApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	s.finalizeTaskAttentionResolution(approved.TaskAttentionResolution)
	taskID := string(approved.ResolvedApproval.Source.TaskID)
	if detail, detailErr := s.readModels.TaskDetail.GetTask(ctx, taskID); detailErr == nil {
		workflowID, err := runtimeids.ParseWorkflowID(detail.Summary.WorkflowId)
		if err != nil {
			return nil, err
		}
		s.publishProjectWorkflowEvent(ctx, detail.Summary.ProjectId, workflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionApproved, taskID, req.ApprovalId)
	}
	return &taskpb.ApproveSuccess{
		Outcome: &taskpb.ApproveSuccess_Applied{Applied: &taskpb.ApproveApplied{
			TaskId:       taskID,
			CurrentNodes: workflowview.ProjectCurrentNodes(approved.Mutation.Created),
		}},
	}, nil
}

func (s *Service) MoveWorkflowTask(ctx context.Context, req *taskpb.MoveRequest) (*taskpb.MoveSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if err := s.authorizeWorkflowTaskMutation(ctx, workflow.TaskID(req.TaskId), req.InvokingSessionId); err != nil {
		return nil, err
	}
	if s.currentNodeExecution == nil {
		return nil, errors.New("current node workflow execution is required")
	}
	var response *taskpb.MoveSuccess
	err := s.currentNodeExecution.RunTaskOperation(ctx, func(ctx context.Context) error {
		var err error
		response, err = s.moveWorkflowTask(ctx, req)
		return err
	})
	return response, err
}

func (s *Service) PreviewWorkflowTaskMove(ctx context.Context, req *taskpb.MovePreviewRequest) (*taskpb.MovePreviewSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	preview, err := s.store.PreviewManualMove(ctx, workflowstore.ManualMoveRequest{
		TaskID:       workflow.TaskID(req.TaskId),
		TargetNodeID: workflow.NodeID(req.TargetNodeId),
	})
	if err != nil {
		return nil, err
	}
	if preview.Outcome == workflowstore.ManualMovePreviewOutcomeNoOp {
		return &taskpb.MovePreviewSuccess{
			Outcome: &taskpb.MovePreviewSuccess_NoOp{
				NoOp: &taskpb.MovePreviewNoOp{CurrentNodes: workflowview.ProjectCurrentNodes(preview.CurrentNodes)},
			},
		}, nil
	}
	if preview.Outcome == workflowstore.ManualMovePreviewOutcomeBlocked {
		reason, err := manualMovePreviewBlocker(preview.Blocker)
		if err != nil {
			return nil, err
		}
		return &taskpb.MovePreviewSuccess{
			Outcome: &taskpb.MovePreviewSuccess_Blocked{Blocked: &taskpb.MovePreviewBlocked{Reason: reason}},
		}, nil
	}
	switch preview.Outcome {
	case workflowstore.ManualMovePreviewOutcomeDirect:
		return &taskpb.MovePreviewSuccess{
			Outcome: &taskpb.MovePreviewSuccess_Direct{Direct: &taskpb.MovePreviewDirect{}},
		}, nil
	case workflowstore.ManualMovePreviewOutcomeTransition:
		choices := make([]*taskpb.MoveTransitionChoice, 0, len(preview.Choices))
		for _, choice := range preview.Choices {
			requiredValues := make([]*taskpb.MoveRequiredValue, 0, len(choice.RequiredValues))
			for _, value := range choice.RequiredValues {
				requiredValues = append(requiredValues, &taskpb.MoveRequiredValue{
					NodeKey:       string(value.NodeKey),
					OutputName:    value.OutputName,
					Description:   value.Description,
					ResolvedValue: value.ResolvedValue,
				})
			}
			choices = append(choices, &taskpb.MoveTransitionChoice{
				TransitionKey:         string(choice.TransitionKey),
				Label:                 choice.Label,
				SourceNodeDisplayName: workflow.NodeDisplayName(choice.SourceNode),
				RequiredValues:        requiredValues,
			})
		}
		return &taskpb.MovePreviewSuccess{
			Outcome: &taskpb.MovePreviewSuccess_Transition{Transition: &taskpb.MovePreviewTransition{Choices: choices}},
		}, nil
	default:
		return nil, fmt.Errorf("manual move preview outcome %q is invalid", preview.Outcome)
	}
}

func (s *Service) moveWorkflowTask(ctx context.Context, req *taskpb.MoveRequest) (*taskpb.MoveSuccess, error) {
	values := make(map[workflow.ModelKey]map[string]string, len(req.Values))
	for _, node := range req.Values {
		converted := make(map[string]string, len(node.Outputs))
		for _, output := range node.Outputs {
			converted[output.Name] = output.Value
		}
		values[workflow.ModelKey(node.NodeKey)] = converted
	}
	var transitionKey *workflow.TransitionID
	if req.TransitionKey != nil {
		value := workflow.TransitionID(*req.TransitionKey)
		transitionKey = &value
	}
	moveRequest := workflowstore.ManualMoveRequest{
		TaskID:        workflow.TaskID(req.TaskId),
		TargetNodeID:  workflow.NodeID(req.TargetNodeId),
		TransitionKey: transitionKey,
		Values:        values,
		Commentary:    req.GetCommentary(),
	}
	prepared, err := s.store.PrepareManualMove(ctx, moveRequest)
	if err != nil {
		return nil, err
	}
	if req.BranchName != nil && (prepared.IsNoOp() || !prepared.RequiresExecutionTarget()) {
		return nil, operationCannotCreateInitialWorktreeError(*req.BranchName)
	}
	if prepared.IsNoOp() {
		return &taskpb.MoveSuccess{
			Outcome: &taskpb.MoveSuccess_NoOp{
				NoOp: &taskpb.MoveNoOp{CurrentNodes: workflowview.ProjectCurrentNodes(prepared.CurrentNodes())},
			},
		}, nil
	}
	var targetPreflight initiatingActionTargetPreflight
	if prepared.RequiresExecutionTarget() {
		if !req.ProceedDespiteDependencies {
			count, countErr := s.readModels.TaskDependencies.CountUnsatisfiedBlockers(ctx, req.TaskId)
			if countErr != nil {
				return nil, countErr
			}
			if count > 0 {
				count, err := protoapi.Int32(count, "unsatisfied_dependency_count")
				if err != nil {
					return nil, err
				}
				return &taskpb.MoveSuccess{
					Outcome: &taskpb.MoveSuccess_DependencyConfirmationRequired{
						DependencyConfirmationRequired: &taskpb.DependencyConfirmationRequired{UnsatisfiedDependencyCount: count},
					},
				}, nil
			}
		}
		selection, selectionErr := workflowTaskExecutionSelection(req.ExecutionTarget)
		if selectionErr != nil {
			return nil, selectionErr
		}
		targetPreflight, err = s.preflightManualMoveTarget(ctx, prepared, selection, req.BranchName)
		if err != nil {
			return nil, err
		}
	}
	coordinated, err := coordinateInitiatingAction(ctx, s, initiatingActionRequest{
		taskID:                  moveRequest.TaskID,
		requiresExecutionTarget: prepared.RequiresExecutionTarget(),
		targetPreflight:         targetPreflight,
		afterTargetResolution: func() error {
			return s.currentNodeExecution.InterruptForManualMove(ctx, moveRequest.TaskID, func() error {
				preview, err := s.store.PreviewManualMove(ctx, moveRequest)
				if err != nil {
					return err
				}
				if preview.Outcome == workflowstore.ManualMovePreviewOutcomeNoOp {
					return &manualMoveNoOpBeforeInterruptError{
						currentNodes: append([]workflow.CurrentNode(nil), preview.CurrentNodes...),
					}
				}
				return nil
			})
		},
	}, func(ctx context.Context, candidate *workflowstore.ExecutionTargetCandidate) (*workflowstore.ManualMoveResult, error) {
		moved, err := s.currentNodeExecution.ApplyManualMove(ctx, prepared, candidate)
		if err != nil && moved.Outcome != workflowstore.ManualMoveResultOutcomeApplied &&
			moved.Outcome != workflowstore.ManualMoveResultOutcomeNoOp {
			return nil, err
		}
		return &moved, err
	})
	var noOpBeforeInterrupt *manualMoveNoOpBeforeInterruptError
	if errors.As(err, &noOpBeforeInterrupt) {
		if targetPreflight.pendingBranchReplaced {
			return nil, workflowexecution.ErrManualMoveLifecycleConflict
		}
		return &taskpb.MoveSuccess{
			Outcome: &taskpb.MoveSuccess_NoOp{NoOp: &taskpb.MoveNoOp{
				CurrentNodes:             workflowview.ProjectCurrentNodes(noOpBeforeInterrupt.currentNodes),
				RetainedPreviousWorktree: coordinated.retainedPreviousWorktree,
			}},
		}, nil
	}
	if err != nil && coordinated.applied == nil {
		return nil, err
	}
	if coordinated.selectionRequired != nil {
		return &taskpb.MoveSuccess{
			Outcome: &taskpb.MoveSuccess_SelectionRequired{SelectionRequired: coordinated.selectionRequired},
		}, nil
	}
	if coordinated.applied == nil {
		return nil, errors.New("coordinated task move returned no applied result")
	}
	if err != nil {
		slog.Error(
			"manual move committed with post-commit lifecycle error",
			"task_id", req.TaskId,
			"target_node_id", req.TargetNodeId,
			"error", err,
		)
	}
	moved := *coordinated.applied
	if err := moved.Validate(); err != nil {
		return nil, err
	}
	if moved.Outcome == workflowstore.ManualMoveResultOutcomeNoOp {
		if targetPreflight.pendingBranchReplaced {
			return nil, workflowexecution.ErrManualMoveLifecycleConflict
		}
		return &taskpb.MoveSuccess{
			Outcome: &taskpb.MoveSuccess_NoOp{NoOp: &taskpb.MoveNoOp{
				CurrentNodes:             workflowview.ProjectCurrentNodes(moved.CurrentNodes),
				RetainedPreviousWorktree: coordinated.retainedPreviousWorktree,
			}},
		}, nil
	}
	s.finalizeTaskAttentionResolution(moved.TaskAttentionResolution)
	if detail, detailErr := s.readModels.TaskDetail.GetTask(ctx, req.TaskId); detailErr == nil {
		workflowID, err := runtimeids.ParseWorkflowID(detail.Summary.WorkflowId)
		if err != nil {
			return nil, err
		}
		s.publishProjectWorkflowEvent(ctx, detail.Summary.ProjectId, workflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionMoved, req.TaskId)
	}
	return &taskpb.MoveSuccess{
		Outcome: &taskpb.MoveSuccess_Applied{Applied: &taskpb.MoveApplied{
			CurrentNodes:             workflowview.ProjectCurrentNodes(moved.Mutation.Created),
			RetainedPreviousWorktree: coordinated.retainedPreviousWorktree,
		}},
	}, nil
}

func manualMovePreviewBlocker(blocker workflowstore.ManualMoveBlocker) (taskpb.MovePreviewBlocker, error) {
	switch blocker {
	case workflowstore.ManualMoveBlockerInvalidWorkflow:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_INVALID_WORKFLOW, nil
	case workflowstore.ManualMoveBlockerNoSourcePosition:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_NO_SOURCE_POSITION, nil
	case workflowstore.ManualMoveBlockerUnsupportedDestination:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_UNSUPPORTED_DESTINATION, nil
	case workflowstore.ManualMoveBlockerLifecycleConflict:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_LIFECYCLE_CONFLICT, nil
	case workflowstore.ManualMoveBlockerContextSessionUnavailable:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_CONTEXT_SESSION_UNAVAILABLE, nil
	case workflowstore.ManualMoveBlockerNoUsableTransition:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_NO_USABLE_TRANSITION, nil
	case workflowstore.ManualMoveBlockerParallelBranchRequiresFanOut:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_PARALLEL_BRANCH_REQUIRES_FAN_OUT, nil
	default:
		return taskpb.MovePreviewBlocker_MOVE_PREVIEW_BLOCKER_UNSPECIFIED, fmt.Errorf("manual move blocker %q is invalid", blocker)
	}
}

func (s *Service) authorizeWorkflowTaskMutation(
	ctx context.Context,
	targetTaskID workflow.TaskID,
	invokingSessionID *string,
) error {
	if invokingSessionID == nil {
		return nil
	}
	sessionID, err := runtimeids.ParseSessionID(*invokingSessionID)
	if err != nil {
		return err
	}
	invokingTaskID, err := s.store.TaskIDForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if invokingTaskID != nil && *invokingTaskID == targetTaskID {
		return &serverapi.WorkflowTaskMutationSelfTargetError{TaskID: string(targetTaskID)}
	}
	return nil
}

func (s *Service) CompleteWorkflowTask(ctx context.Context, req *taskpb.CompleteRequest) (*taskpb.CompleteSuccess, error) {
	return s.completeWorkflowTask(ctx, req)
}

func (s *Service) completeWorkflowTask(ctx context.Context, req *taskpb.CompleteRequest) (*taskpb.CompleteSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s.currentNodeExecution == nil {
		return nil, errors.New("current node workflow execution is required")
	}
	if req.ActorKind == taskpb.CompleteActorKind_COMPLETE_ACTOR_KIND_USER {
		return s.forceCompleteWorkflowTask(ctx, req)
	}
	if req.ActorKind == taskpb.CompleteActorKind_COMPLETE_ACTOR_KIND_AGENT {
		sessionID, parseErr := runtimeids.ParseSessionID(req.GetAgentSessionId())
		if parseErr != nil {
			return nil, parseErr
		}
		runID, err := runtimeids.ParseRunID(req.GetRunId())
		if err != nil {
			return nil, err
		}
		stepID, err := runtimeids.ParseStepID(req.GetStepId())
		if err != nil {
			return nil, err
		}
		values := make(map[string]string, len(req.OutputValues))
		for _, output := range req.OutputValues {
			values[output.Name] = output.Value
		}
		outcome, err := s.currentNodeExecution.CompleteSessionCurrentNode(
			ctx, sessionID, runID, stepID,
			req.GetTransitionId(), values, req.GetCommentary(),
		)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sessionruntime.ErrExecutionNoLongerLive) {
				return nil, serverapi.ErrWorkflowTaskCompleteTargetNotFound
			}
			return nil, err
		}
		if !outcome.IsApplied() {
			return nil, errors.New("current node completion returned no applied result")
		}
		completed := outcome.CommittedResult
		var taskID workflow.TaskID
		if completed.PendingApproval != nil {
			taskID = completed.PendingApproval.Source.TaskID
		} else {
			if len(completed.Mutation.Removed) != 1 {
				return nil, errors.New("current node completion did not remove exactly one source")
			}
			taskID = completed.Mutation.Removed[0].TaskID
		}
		completion := &taskpb.AgentCompletion{
			TaskId:       string(taskID),
			CurrentNodes: workflowview.ProjectCurrentNodes(completed.Mutation.Created),
			Handoff: &taskpb.CompletionHandoff{
				SourceNodeDisplayName:  completed.Handoff.SourceNodeDisplayName,
				DestinationDisplayName: completed.Handoff.DestinationDisplayName,
			},
		}
		if completed.PendingApproval != nil {
			approvalID := completed.PendingApproval.ID.String()
			completion.PendingApprovalId = &approvalID
			if s.attentionFinalizer != nil {
				finalizeCtx, cancel := workflowAttentionContext(ctx)
				defer cancel()
				s.attentionFinalizer.PublishPendingApproval(finalizeCtx, completed.PendingApproval.ID)
			}
		}
		return &taskpb.CompleteSuccess{Outcome: &taskpb.CompleteSuccess_AgentCompletion{AgentCompletion: completion}}, nil
	}
	return nil, errors.New("workflow task completion actor is invalid")
}

func (s *Service) forceCompleteWorkflowTask(
	ctx context.Context,
	req *taskpb.CompleteRequest,
) (*taskpb.CompleteSuccess, error) {
	taskID, source, target, err := s.resolveForcedCompletionMove(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.currentNodeExecution.Interrupt(ctx, workflowexecution.InterruptSelector{TaskID: taskID}); err != nil &&
		!errors.Is(err, workflowexecution.ErrNoInterruptibleExecution) {
		return nil, err
	}
	var transitionKey *string
	if target.Kind() != workflow.NodeKindTerminal {
		value := req.GetTransitionId()
		transitionKey = &value
	}
	var values []*taskpb.NodeOutputValues
	if len(req.OutputValues) != 0 {
		values = []*taskpb.NodeOutputValues{{NodeKey: string(workflow.NodeKey(source)), Outputs: req.OutputValues}}
	}
	moved, err := s.moveWorkflowTask(ctx, &taskpb.MoveRequest{
		TaskId:        string(taskID),
		TargetNodeId:  string(workflow.NodeIDOf(target)),
		TransitionKey: transitionKey,
		Values:        values,
		Commentary:    req.Commentary,
	})
	if err != nil {
		return nil, err
	}
	return &taskpb.CompleteSuccess{
		Outcome: &taskpb.CompleteSuccess_ForcedMove{ForcedMove: &taskpb.ForcedCompletionMove{
			TaskId:       string(taskID),
			TargetNodeId: string(workflow.NodeIDOf(target)),
			Outcome:      moved,
		}},
	}, nil
}

func (s *Service) resolveForcedCompletionMove(
	ctx context.Context,
	req *taskpb.CompleteRequest,
) (workflow.TaskID, workflow.Node, workflow.Node, error) {
	var taskID workflow.TaskID
	if req.TaskId != nil {
		taskID = workflow.TaskID(*req.TaskId)
	} else {
		sessionID, err := runtimeids.ParseSessionID(req.GetSessionId())
		if err != nil {
			return "", nil, nil, err
		}
		resolved, err := s.store.TaskIDForSession(ctx, sessionID)
		if err != nil {
			return "", nil, nil, err
		}
		if resolved == nil {
			return "", nil, nil, serverapi.ErrWorkflowTaskCompleteTargetNotFound
		}
		taskID = *resolved
	}
	currentNodes, err := s.store.ListCurrentNodes(ctx, taskID)
	if err != nil {
		return "", nil, nil, err
	}
	if len(currentNodes) != 1 {
		if len(currentNodes) == 0 {
			return "", nil, nil, serverapi.ErrWorkflowTaskCompleteTargetNotFound
		}
		return "", nil, nil, serverapi.WorkflowTaskCompleteSelectorAmbiguousError{}
	}
	scope, err := s.store.TaskExecutionScope(ctx, taskID)
	if err != nil {
		return "", nil, nil, err
	}
	definition, _, err := s.store.GetDefinition(ctx, scope.WorkflowID)
	if err != nil {
		return "", nil, nil, err
	}
	source, err := workflowNodeByID(definition, currentNodes[0].Reference.NodeID)
	if err != nil {
		return "", nil, nil, err
	}
	var target workflow.Node
	for _, group := range definition.TransitionGroups {
		if group.SourceNodeID != workflow.NodeIDOf(source) || group.TransitionID != workflow.TransitionID(req.GetTransitionId()) {
			continue
		}
		for _, edge := range definition.Edges {
			if edge.TransitionGroupID != group.ID {
				continue
			}
			target, err = workflowNodeByID(definition, edge.TargetNodeID)
			if err != nil {
				return "", nil, nil, err
			}
			break
		}
		break
	}
	if target == nil {
		return "", nil, nil, errors.New("forced completion transition is unavailable")
	}
	return taskID, source, target, nil
}

func workflowNodeByID(definition workflow.Definition, nodeID workflow.NodeID) (workflow.Node, error) {
	for _, node := range definition.Nodes {
		if workflow.NodeIDOf(node) == nodeID {
			return node, nil
		}
	}
	return nil, fmt.Errorf("workflow node %q is absent", nodeID)
}

func (s *Service) DeleteWorkflowTask(ctx context.Context, req *taskpb.DeleteRequest) (*emptypb.Empty, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if s.taskWorktreeCleanup != nil {
		if err := s.taskWorktreeCleanup.EnsureTaskWorktreeDeletable(ctx, req.TaskId); err != nil {
			return nil, err
		}
	}
	err := s.taskMutations.Run(ctx, workflow.TaskID(req.TaskId), func(ctx context.Context) error {
		if s.currentNodeExecution == nil {
			return errors.New("current node workflow execution is required")
		}
		if err := s.currentNodeExecution.EnsureTaskQuiescent(workflow.TaskID(req.TaskId)); err != nil {
			return err
		}
		if s.taskWorktreeCleanup != nil {
			if err := s.taskWorktreeCleanup.DeleteTaskWorktree(ctx, req.TaskId); err != nil {
				return err
			}
		}
		result, err := s.store.DeleteTask(ctx, workflow.TaskID(req.TaskId))
		if err != nil {
			return err
		}
		s.finalizeTaskAttentionResolution(result.TaskAttentionResolution)
		s.publishProjectWorkflowEvent(ctx, result.ProjectID, result.WorkflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionDeleted, req.TaskId)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) finalizeWorkflowAttentionResolution(ctx context.Context, result workflowstore.WorkflowDeleteResult) {
	s.finalizeTaskAttentionResolution(result.TaskAttentionResolution)
}

func (s *Service) finalizeTaskAttentionResolution(resolution workflowstore.TaskAttentionResolution) {
	if s == nil || s.attentionFinalizer == nil {
		return
	}
	s.attentionFinalizer.FinalizeTaskResolution(resolution)
}

func workflowAttentionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		return context.WithTimeout(context.Background(), workflowAttentionFinalizationTimeout)
	}
	return context.WithTimeout(context.WithoutCancel(ctx), workflowAttentionFinalizationTimeout)
}

func (s *Service) ListWorkflowAttention(ctx context.Context, req *taskpb.AttentionListRequest) (*taskpb.AttentionListSuccess, error) {
	return s.readModels.Attention.List(ctx, req)
}

func (s *Service) ListWorkflowTaskAttention(ctx context.Context, req *taskpb.TaskAttentionListRequest) (*taskpb.TaskAttentionListSuccess, error) {
	return s.readModels.Attention.ListTask(ctx, req)
}

func (s *Service) ListWorkflowTaskSessions(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.SessionListSuccess, error) {
	return s.readModels.TaskSessions.List(ctx, req)
}

func (s *Service) AddWorkflowTaskComment(ctx context.Context, req *taskpb.CommentAddRequest) (*taskpb.CommentAddSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	author, err := protoapi.TaskCommentAuthor.Decode(req.Author)
	if err != nil {
		return nil, err
	}
	comment, err := s.store.AddComment(ctx, workflow.TaskID(req.TaskId), req.Body, author, req.GetAuthorId())
	if err != nil {
		return nil, err
	}
	if detail, detailErr := s.readModels.TaskDetail.GetTask(ctx, req.TaskId); detailErr == nil {
		workflowID, err := runtimeids.ParseWorkflowID(detail.Summary.WorkflowId)
		if err != nil {
			return nil, err
		}
		s.publishProjectWorkflowEvent(ctx, detail.Summary.ProjectId, workflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionCommentAdded, req.TaskId, comment.ID)
	}
	projected, err := workflowview.Comment(comment)
	if err != nil {
		return nil, err
	}
	return &taskpb.CommentAddSuccess{Comment: projected}, nil
}

func (s *Service) ListWorkflowTaskComments(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.CommentListSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	window := workflowview.TaskPageWindow(req)
	totalCount, err := s.store.CountTaskComments(ctx, workflow.TaskID(req.TaskId))
	if err != nil {
		return nil, err
	}
	comments, err := s.store.ListCommentsPage(ctx, workflow.TaskID(req.TaskId), window.Offset, window.Limit+1)
	if err != nil {
		return nil, err
	}
	out := make([]*taskpb.Comment, 0, len(comments))
	for _, comment := range comments {
		projected, err := workflowview.Comment(comment)
		if err != nil {
			return nil, err
		}
		out = append(out, projected)
	}
	page := serverapi.FinalizeWorkflowOffsetPage(window, out)
	next, err := workflowview.TaskNextOffset(page.NextOffset)
	if err != nil {
		return nil, err
	}
	return &taskpb.CommentListSuccess{
		Items: page.Items, NextOffset: next, TotalCount: totalCount,
	}, nil
}

func (s *Service) ReplaceWorkflowTaskComment(ctx context.Context, req *taskpb.CommentReplaceRequest) (*emptypb.Empty, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	taskID, projectID, workflowID, err := s.store.TaskIdentityForComment(ctx, strings.TrimSpace(req.CommentId))
	if err != nil {
		return nil, err
	}
	if err := s.store.ReplaceComment(ctx, req.CommentId, req.Body); err != nil {
		return nil, err
	}
	s.publishProjectWorkflowEvent(ctx, projectID, workflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionCommentUpdated, taskID, req.CommentId)
	return &emptypb.Empty{}, nil
}

func (s *Service) DeleteWorkflowTaskComment(ctx context.Context, req *taskpb.CommentDeleteRequest) (*emptypb.Empty, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	taskID, projectID, workflowID, err := s.store.TaskIdentityForComment(ctx, strings.TrimSpace(req.CommentId))
	if err != nil {
		return nil, err
	}
	if err := s.store.DeleteComment(ctx, req.CommentId); err != nil {
		return nil, err
	}
	s.publishProjectWorkflowEvent(ctx, projectID, workflowID, serverapi.WorkflowProjectEventResourceTask, serverapi.WorkflowProjectEventActionCommentDeleted, taskID, req.CommentId)
	return &emptypb.Empty{}, nil
}

func (s *Service) ListWorkflowTaskActivity(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.ActivityListSuccess, error) {
	return s.readModels.Activity.List(ctx, req)
}

func (s *Service) ListWorkflowTasks(ctx context.Context, req *taskpb.ListRequest) (*taskpb.ListSuccess, error) {
	return s.readModels.TaskList.List(ctx, req)
}

func (s *Service) GetWorkflowProjectTaskGroupCounts(ctx context.Context, req *taskpb.ProjectTaskGroupCountsRequest) (*taskpb.ProjectTaskGroupCountsSuccess, error) {
	return s.readModels.TaskList.CountGroups(ctx, req)
}

func (s *Service) SearchWorkflowTasks(ctx context.Context, req *taskpb.SearchRequest) (*taskpb.SearchSuccess, error) {
	return s.readModels.TaskSearch.Search(ctx, req)
}

func (s *Service) GetWorkflowBoard(ctx context.Context, req *taskpb.BoardGetRequest) (*taskpb.BoardGetSuccess, error) {
	board, err := s.readModels.Board.Get(ctx, req)
	if err != nil {
		return nil, err
	}
	return &taskpb.BoardGetSuccess{Board: board}, nil
}

func (s *Service) ListWorkflowBoardNodeCards(ctx context.Context, req *taskpb.BoardNodeCardsListRequest) (*taskpb.BoardNodeCardsListSuccess, error) {
	return s.readModels.Board.ListNodeCards(ctx, req)
}

func (s *Service) SubscribeWorkflowProject(ctx context.Context, req *pb.ProjectSubscribeRequest) (apicontract.WorkflowEventSubscription, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	return s.events.subscribe(req.ProjectId, nil)
}

func (s *Service) SubscribeWorkflow(ctx context.Context, req *pb.WorkflowSubscribeRequest) (apicontract.WorkflowEventSubscription, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if _, err := s.GetWorkflow(ctx, &pb.GetRequest{WorkflowId: req.WorkflowId}); err != nil {
		return nil, err
	}
	workflowID, err := runtimeids.ParseWorkflowID(req.WorkflowId)
	if err != nil {
		return nil, err
	}
	return s.events.subscribe(nil, &workflowID)
}

func (s *Service) GetWorkflowTask(ctx context.Context, req *taskpb.GetRequest) (*taskpb.GetSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	var (
		detail *taskpb.TaskDetail
		err    error
	)
	if req.TaskId != nil {
		detail, err = s.readModels.TaskDetail.GetTask(ctx, *req.TaskId)
	} else if req.ProjectId != nil {
		detail, err = s.readModels.TaskDetail.GetTaskByProjectShortID(ctx, *req.ProjectId, req.GetShortId())
	} else {
		detail, err = s.readModels.TaskDetail.GetTaskByShortID(ctx, req.GetShortId())
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.Join(serverapi.ErrWorkflowTaskNotFound, err)
		}
		return nil, err
	}
	return &taskpb.GetSuccess{Task: detail}, nil
}

func projectWorkflowLink(row workflowstore.ProjectWorkflowLinkRecord) *pb.ProjectWorkflowLink {
	return &pb.ProjectWorkflowLink{Id: row.ID, ProjectId: row.ProjectID, WorkflowId: row.WorkflowID.String(), Default: row.IsDefault}
}

func workflowStoreDefaultPolicy(policy pb.ProjectLinkDefaultMode) workflowstore.WorkflowLinkDefaultPolicy {
	switch policy {
	case pb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_ALWAYS:
		return workflowstore.WorkflowLinkDefaultAlways
	case pb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_IF_PROJECT_HAS_NONE:
		return workflowstore.WorkflowLinkDefaultIfProjectHasNone
	default:
		return workflowstore.WorkflowLinkDefaultNever
	}
}

func workflowUnlinkProjectResponse(result workflowstore.ProjectWorkflowUnlinkResult) *pb.UnlinkProjectSuccess {
	resp := &pb.UnlinkProjectSuccess{LinkId: result.LinkID, Unlinked: result.Unlinked}
	for _, blocker := range result.Blockers {
		dto := &pb.UnlinkProjectBlocker{Code: blocker.Code, Message: blocker.Message, Count: int32(blocker.Count)}
		for _, task := range blocker.Tasks {
			dto.Tasks = append(dto.Tasks, &pb.UnlinkTaskReference{TaskId: string(task.TaskID), ShortId: task.ShortID, Title: task.Title})
		}
		resp.Blockers = append(resp.Blockers, dto)
	}
	return resp
}

func workflowDeleteResponse(result workflowstore.WorkflowDeleteResult) *pb.DeleteSuccess {
	resp := &pb.DeleteSuccess{Deleted: result.Deleted, Impact: workflowDeleteImpact(result.Impact)}
	for _, blocker := range result.Blockers {
		resp.Blockers = append(resp.Blockers, &pb.DeleteBlocker{Code: blocker.Code, Message: blocker.Message, Count: blocker.Count})
	}
	return resp
}

func workflowDeleteImpact(impact workflowstore.WorkflowDeleteImpact) *pb.DeleteImpact {
	return &pb.DeleteImpact{
		WorkflowId:                     impact.WorkflowID.String(),
		Version:                        impact.Version,
		ProjectCount:                   impact.ProjectCount,
		LinkCount:                      impact.LinkCount,
		DefaultReplacementProjectCount: impact.DefaultReplacementProjectCount,
		TaskCount:                      impact.TaskCount,
		CurrentNodeCount:               impact.CurrentNodeCount,
		PendingApprovalCount:           impact.PendingApprovalCount,
		BlockedTaskCount:               impact.BlockedTaskCount,
	}
}

func (s *Service) workflowGraphValidationResultsForDefinition(def workflow.Definition, modes []pb.ValidationMode) ([]*pb.ModeValidationResult, error) {
	contexts := make([]workflow.ValidationContext, 0, len(modes))
	for _, mode := range modes {
		value, err := protoapi.WorkflowValidationMode.Decode(mode)
		if err != nil {
			return nil, err
		}
		contexts = append(contexts, workflow.ValidationContext(value))
	}
	results := workflowscript.EvaluateDefinition(def, contexts, s.roleResolver, nil)
	return workflowValidationResults(def.ID, contexts, results)
}

func workflowValidationResults(workflowID runtimeids.WorkflowID, contexts []workflow.ValidationContext, results map[workflow.ValidationContext]workflow.ValidationResult) ([]*pb.ModeValidationResult, error) {
	out := make([]*pb.ModeValidationResult, 0, len(contexts))
	for _, context := range contexts {
		mode, err := protoapi.WorkflowValidationMode.Encode(string(context))
		if err != nil {
			return nil, err
		}
		result, err := workflowValidationResponse(workflowID, results[context])
		if err != nil {
			return nil, err
		}
		out = append(out, &pb.ModeValidationResult{Mode: mode, Result: result})
	}
	return out, nil
}

func (s *Service) workflowGraphDraftDefinition(ctx context.Context, workflowID runtimeids.WorkflowID, metadata *pb.GraphMetadata, graph *pb.GraphDraft) (workflow.Definition, error) {
	current, _, err := s.store.GetDefinition(ctx, workflowID)
	if err != nil {
		return workflow.Definition{}, err
	}
	def, err := workflowDefinitionFromGraphDraft(workflowID, graph)
	if err != nil {
		return workflow.Definition{}, err
	}
	displayName := current.DisplayName
	if metadata != nil {
		displayName = metadata.Name
	}
	targetPolicy := current.ExecutionTargetPolicy
	if metadata != nil && metadata.ExecutionTargetPolicy != nil {
		targetPolicy, err = workflowExecutionTargetPolicyFromAPI(metadata.ExecutionTargetPolicy)
		if err != nil {
			return workflow.Definition{}, err
		}
	}
	def.DisplayName = displayName
	def.ExecutionTargetPolicy = targetPolicy
	return def, nil
}

func workflowDefinitionFromGraphDraft(workflowID runtimeids.WorkflowID, graph *pb.GraphDraft) (workflow.Definition, error) {
	def := workflow.Definition{ID: workflowID}
	groupMemberIDs := map[string][]workflow.NodeID{}
	groupIDByKey := make(map[workflow.ModelKey]string, len(graph.NodeGroups))
	for _, group := range graph.NodeGroups {
		groupIDByKey[workflow.ModelKey(strings.TrimSpace(group.Key))] = group.Id
		def.NodeGroups = append(def.NodeGroups, workflow.NodeGroup{
			WorkflowID:  workflowID,
			ID:          group.Id,
			Key:         workflow.ModelKey(group.Key),
			DisplayName: group.DisplayName,
		})
	}
	for _, node := range graph.Nodes {
		groupID := optionalStringValue(node.GroupId)
		if groupID == "" && strings.TrimSpace(node.GroupKey) != "" {
			groupKey := workflow.ModelKey(strings.TrimSpace(node.GroupKey))
			var ok bool
			groupID, ok = groupIDByKey[groupKey]
			if !ok {
				return workflow.Definition{}, fmt.Errorf(
					"workflow node group key %q is not in the saved graph",
					groupKey,
				)
			}
		}
		if groupID != "" {
			groupMemberIDs[groupID] = append(groupMemberIDs[groupID], workflow.NodeID(node.Id))
		}
		kind, err := protoapi.WorkflowNodeKind.Decode(node.Kind)
		if err != nil {
			return workflow.Definition{}, err
		}
		completion := ""
		if node.CompletionMode != nil {
			completion, err = protoapi.WorkflowCompletionMode.Decode(*node.CompletionMode)
			if err != nil {
				return workflow.Definition{}, err
			}
		}
		workflowNode, err := workflow.NewNode(
			workflow.NodeIdentity{
				WorkflowID:  workflowID,
				ID:          workflow.NodeID(node.Id),
				Key:         workflow.ModelKey(node.Key),
				DisplayName: node.DisplayName,
				GroupID:     textutil.OptionalExactString(groupID),
			},
			workflow.NodeKind(kind),
			workflow.NodeFields{
				SubagentRole:   node.GetSubagentRole(),
				CompletionMode: completion,
				ScriptPath: func() workflow.OptionalScriptPath {
					if scriptPath, ok := workflow.PresentScriptPath(optionalStringValue(node.ScriptPath)); ok {
						return scriptPath
					}
					return workflow.AbsentScriptPath()
				}(),
				JoinInputProviders: joinInputProviders(node.JoinInputProviders),
			},
		)
		if err != nil {
			return workflow.Definition{}, err
		}
		def.Nodes = append(def.Nodes, workflowNode)
	}
	for index := range def.NodeGroups {
		def.NodeGroups[index].MemberNodeIDs = groupMemberIDs[def.NodeGroups[index].ID]
	}
	for _, group := range graph.TransitionGroups {
		def.TransitionGroups = append(def.TransitionGroups, workflow.TransitionGroup{
			WorkflowID:   workflowID,
			ID:           workflow.TransitionGroupID(group.Id),
			SourceNodeID: workflow.NodeID(group.SourceNodeId),
			TransitionID: workflow.TransitionID(group.TransitionId),
			DisplayName:  group.DisplayName,
			Description:  group.Description,
		})
	}
	for _, edge := range graph.Edges {
		assignee, assigneeErr := protoapi.WorkflowAssigneeSelection.Decode(edge.AssigneeSelection)
		thinking, thinkingErr := protoapi.WorkflowThinkingSelection.Decode(edge.ThinkingSelection)
		contextMode, contextErr := protoapi.WorkflowContextMode.Decode(edge.ContextMode)
		source, sourceErr := protoapi.WorkflowContextSourceKind.Decode(edge.ContextSource.GetKind())
		parameters, parameterErr := domainParameters(edge.Parameters)
		if err := errors.Join(assigneeErr, thinkingErr, contextErr, sourceErr, parameterErr); err != nil {
			return workflow.Definition{}, err
		}
		def.Edges = append(def.Edges, workflow.Edge{
			WorkflowID:        workflowID,
			ID:                workflow.EdgeID(edge.Id),
			Key:               workflow.ModelKey(edge.Key),
			TransitionGroupID: workflow.TransitionGroupID(edge.TransitionGroupId),
			TargetNodeID:      workflow.NodeID(edge.TargetNodeId),
			AssigneeSelection: workflow.AssigneeSelection(assignee),
			ThinkingSelection: workflow.ThinkingSelection(thinking),
			ContextMode:       workflow.ContextMode(contextMode),
			ContextSource:     workflow.CanonicalContextSource(workflow.ContextSource{Kind: workflow.ContextSourceKind(source), NodeKey: workflow.ModelKey(edge.ContextSource.GetNodeKey())}),
			RequiresApproval:  edge.RequiresApproval,
			PromptTemplate:    edge.PromptTemplate,
			Parameters:        parameters,
		})
	}
	return def, nil
}

func workflowValidationResponse(workflowID runtimeids.WorkflowID, result workflow.ValidationResult) (*pb.ValidateResponse, error) {
	diagnostics, err := workflowview.ValidationErrors(&workflowID, result.Errors)
	if err != nil {
		return nil, err
	}
	return &pb.ValidateResponse{Valid: !result.HasBlockingErrors(), Errors: diagnostics}, nil
}

func workflowGraphStoreSaveRequest(workflowID runtimeids.WorkflowID, expectedVersion int64, metadata *pb.GraphMetadata, graph *pb.GraphDraft, confirmation *pb.GraphSaveConfirmation) (workflowstore.WorkflowGraphSaveRequest, error) {
	definition, err := workflowDefinitionFromGraphDraft(workflowID, graph)
	if err != nil {
		return workflowstore.WorkflowGraphSaveRequest{}, err
	}
	req := workflowstore.NewWorkflowGraphSaveRequest(definition, expectedVersion)
	if metadata != nil {
		req.Metadata = &workflowstore.WorkflowGraphSaveMetadata{Name: metadata.Name, Description: metadata.Description}
		if metadata.ExecutionTargetPolicy != nil {
			policy, err := workflowExecutionTargetPolicyFromAPI(metadata.ExecutionTargetPolicy)
			if err != nil {
				return workflowstore.WorkflowGraphSaveRequest{}, err
			}
			req.Metadata.ExecutionTargetPolicy = &policy
		}
	}
	if confirmation != nil {
		req.Confirmed = true
		req.ExpectedRemovedNodeGroupCount = confirmation.ExpectedRemovedNodeGroupCount
		req.ExpectedRemovedNodeCount = confirmation.ExpectedRemovedNodeCount
		req.ExpectedRemovedTransitionGroupCount = confirmation.ExpectedRemovedTransitionGroupCount
		req.ExpectedRemovedEdgeCount = confirmation.ExpectedRemovedEdgeCount
		req.ExpectedNodeTaskReferenceCount = confirmation.ExpectedNodeTaskReferenceCount
		req.ExpectedEdgeTaskReferenceCount = confirmation.ExpectedEdgeTaskReferenceCount
	}
	return req, nil
}

func workflowExecutionTargetPolicyFromAPI(policy *pb.ExecutionTargetConfiguration) (workflow.ExecutionTargetPolicy, error) {
	mode, err := protoapi.WorkflowExecutionTargetMode.Decode(policy.Mode)
	if err != nil {
		return workflow.ExecutionTargetPolicy{}, err
	}
	var customRef *string
	if policy.CustomRef != nil {
		value := *policy.CustomRef
		customRef = &value
	}
	return workflow.ExecutionTargetPolicy{
		Mode:      workflow.ExecutionTargetMode(mode),
		CustomRef: customRef,
	}.Canonical(), nil
}

func workflowGraphSaveResponse(result workflowstore.WorkflowGraphSaveResult) (*pb.GraphSaveSuccess, error) {
	validationResults, err := workflowValidationResults(result.Definition.ID,
		[]workflow.ValidationContext{workflow.ValidationContextDraft, workflow.ValidationContextExecution}, result.ValidationResults)
	if err != nil {
		return nil, err
	}
	impact, err := workflowGraphSaveImpact(result)
	if err != nil {
		return nil, err
	}
	blockers, err := workflowGraphSaveBlockers(result.Blockers)
	if err != nil {
		return nil, err
	}
	return &pb.GraphSaveSuccess{
		Saved:                result.Saved,
		Changed:              result.Changed,
		CurrentVersion:       result.Version,
		ValidationResults:    validationResults,
		Impact:               impact,
		Blockers:             blockers,
		CanSave:              result.CanSave,
		ConfirmationRequired: result.ConfirmationRequired,
	}, nil
}

func workflowGraphSaveImpact(result workflowstore.WorkflowGraphSaveResult) (*pb.GraphSaveImpact, error) {
	removed, err := workflowGraphEntityReferences(result.Impact.RemovedEntities)
	if err != nil {
		return nil, err
	}
	return &pb.GraphSaveImpact{
		RemovedNodeGroupCount:             result.Impact.RemovedNodeGroupCount,
		RemovedNodeCount:                  result.Impact.RemovedNodeCount,
		RemovedTransitionGroupCount:       result.Impact.RemovedTransitionGroupCount,
		RemovedEdgeCount:                  result.Impact.RemovedEdgeCount,
		RemovedEntities:                   removed,
		NodeTaskReferenceCount:            result.Impact.NodeTaskReferenceCount,
		EdgeTaskReferenceCount:            result.Impact.EdgeTaskReferenceCount,
		ActiveCurrentNodeCount:            result.EditPolicyImpact.ActiveCurrentNodeCount,
		PendingApprovalCount:              result.EditPolicyImpact.PendingApprovalCount,
		StartNodeChangeCount:              result.EditPolicyImpact.StartNodeChangeCount,
		LastTerminalChangeCount:           result.EditPolicyImpact.LastTerminalChangeCount,
		TaskReferencedNodeKindChangeCount: result.EditPolicyImpact.TaskReferencedNodeKindChangeCount,
	}, nil
}

func workflowGraphSaveBlockers(blockers []workflowstore.WorkflowGraphSaveBlocker) ([]*pb.GraphSaveBlocker, error) {
	out := make([]*pb.GraphSaveBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		affected, err := workflowGraphEntityReferences(blocker.AffectedEntities)
		if err != nil {
			return nil, err
		}
		out = append(out, &pb.GraphSaveBlocker{
			Code:             blocker.Code,
			Message:          blocker.Message,
			Count:            blocker.Count,
			AffectedEntities: affected,
		})
	}
	return out, nil
}

func workflowGraphEntityReferences(references []workflowstore.WorkflowGraphEntityReference) ([]*pb.GraphEntityReference, error) {
	out := make([]*pb.GraphEntityReference, 0, len(references))
	for _, reference := range references {
		entityType, err := protoapi.WorkflowGraphEntityType.Encode(string(reference.EntityType))
		if err != nil {
			return nil, err
		}
		out = append(out, &pb.GraphEntityReference{EntityType: entityType, EntityId: reference.EntityID})
	}
	return out, nil
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func joinInputProviders(in []*pb.DraftJoinInputProvider) []workflow.JoinInputProvider {
	out := make([]workflow.JoinInputProvider, 0, len(in))
	for _, provider := range in {
		out = append(out, workflow.JoinInputProvider{InputName: provider.InputName, ProviderEdgeID: workflow.EdgeID(provider.ProviderEdgeId)})
	}
	return out
}

func domainParameters(in []*pb.Parameter) ([]workflow.Parameter, error) {
	out := make([]workflow.Parameter, 0, len(in))
	for _, parameter := range in {
		purpose, err := protoapi.WorkflowParameterPurpose.Decode(parameter.Purpose)
		if err != nil {
			return nil, err
		}
		out = append(out, workflow.Parameter{Key: parameter.Key, Description: parameter.Description, Purpose: workflow.ParameterPurpose(purpose)})
	}
	return out, nil
}
