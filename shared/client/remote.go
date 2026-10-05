package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	serverpb "core/shared/protoapi/gen/kent/api/server"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/protocol"
	"core/shared/rpcwire"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/emptypb"
)

type InvalidResponseError struct {
	Operation string
	Cause     error
}

func (e *InvalidResponseError) Error() string {
	return fmt.Sprintf("validate %s response: %v", e.Operation, e.Cause)
}
func (e *InvalidResponseError) Unwrap() error { return e.Cause }
func invalidResponseError(operation string, cause error) error {
	return &InvalidResponseError{Operation: operation, Cause: cause}
}

func validateRuntimeLiveResponseSession(operation string, requestedSessionID string, responseSessionID string) error {
	if responseSessionID != requestedSessionID {
		return invalidResponseError(operation, fmt.Errorf(
			"response session ID %q does not match requested session %q",
			responseSessionID, requestedSessionID,
		))
	}
	return nil
}

type Remote struct {
	plan           remoteDialPlan
	transport      rpcwire.ClientTransport
	mu             sync.Mutex
	control        *remoteControlConn
	draftHandoff   *remoteSessionControl
	identity       protocol.ServerIdentity
	attachIntent   *remoteAttachmentIntent
	attachment     *remoteAttachment
	expectedRootID atomic.Value // string; empty disables root validation
	closed         atomic.Bool
}

func DialRemoteURL(ctx context.Context, rpcURL string) (*Remote, error) {
	return dialRemoteURL(ctx, rpcURL, nil)
}

func DialRemoteURLForProject(ctx context.Context, rpcURL string, projectID string) (*Remote, error) {
	intent, err := newRemoteDefaultProjectAttachmentIntent(projectID)
	if err != nil {
		return nil, err
	}
	return dialRemoteURL(ctx, rpcURL, intent)
}

func DialRemoteURLForProjectWorkspace(ctx context.Context, rpcURL string, projectID string, workspaceRoot string) (*Remote, error) {
	intent, err := newRemoteProjectWorkspaceRootAttachmentIntent(projectID, workspaceRoot)
	if err != nil {
		return nil, err
	}
	return dialRemoteURL(ctx, rpcURL, intent)
}

func DialRemoteURLForSession(ctx context.Context, rpcURL string, sessionID string) (*Remote, error) {
	intent, err := newRemoteSessionAttachmentIntent(sessionID)
	if err != nil {
		return nil, err
	}
	return dialRemoteURL(ctx, rpcURL, intent)
}

func DialConfiguredRemote(ctx context.Context, cfg config.Connection) (*Remote, error) {
	return dialConfiguredRemote(ctx, cfg, nil)
}

func DialConfiguredRemoteForProjectWorkspace(ctx context.Context, cfg config.Connection, projectID string, workspaceRoot string) (*Remote, error) {
	intent, err := newRemoteProjectWorkspaceRootAttachmentIntent(projectID, workspaceRoot)
	if err != nil {
		return nil, err
	}
	return dialConfiguredRemote(ctx, cfg, intent)
}

func DialConfiguredRemoteForProjectWorkspaceID(ctx context.Context, cfg config.Connection, projectID string, workspaceID string) (*Remote, error) {
	intent, err := newRemoteProjectWorkspaceIDAttachmentIntent(projectID, workspaceID)
	if err != nil {
		return nil, err
	}
	return dialConfiguredRemote(ctx, cfg, intent)
}

func DialConfiguredRemoteForSession(ctx context.Context, cfg config.Connection, sessionID string) (*Remote, error) {
	intent, err := newRemoteSessionAttachmentIntent(sessionID)
	if err != nil {
		return nil, err
	}
	return dialConfiguredRemote(ctx, cfg, intent)
}

func (c *Remote) Close() error {
	if c == nil {
		return nil
	}
	c.closed.Store(true)
	c.mu.Lock()
	control := c.control
	c.control = nil
	draftHandoff := c.draftHandoff
	c.draftHandoff = nil
	c.mu.Unlock()
	var draftHandoffErr error
	if draftHandoff != nil {
		draftHandoffErr = draftHandoff.remote.Close()
	}
	if control == nil {
		return draftHandoffErr
	}
	return errors.Join(control.Close(), draftHandoffErr)
}

func (c *Remote) Identity() protocol.ServerIdentity {
	if c == nil {
		return protocol.ServerIdentity{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.identity
}

// RequireRoot pins the persistence-root id that every (re)connect handshake must
// report. It validates the current identity immediately and returns an error on
// mismatch so an initial attach to the wrong instance is rejected; the pinned id
// then guards reconnects, where the dial plan may resolve a different server on
// the fallback TCP endpoint after the original socket disappears. An empty rootID
// disables root validation (default-root behavior is unchanged).
func (c *Remote) RequireRoot(rootID string) error {
	if c == nil {
		return errors.New("remote client is required")
	}
	c.expectedRootID.Store(rootID)
	c.mu.Lock()
	identity := c.identity
	c.mu.Unlock()
	return validateIdentityRoot(rootID, identity)
}

func (c *Remote) rootID() string {
	if c == nil {
		return ""
	}
	if value, ok := c.expectedRootID.Load().(string); ok {
		return value
	}
	return ""
}

func (c *Remote) GetReadiness(ctx context.Context, req *emptypb.Empty) (*serverpb.GetReadinessSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(serverpb.File_kent_api_server_server_proto, "ServerService", "GetReadiness"),
		req,
		&serverpb.GetReadinessResult{},
		func(failure *serverpb.GetReadinessError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) GetUpdateStatus(ctx context.Context, req *emptypb.Empty) (*serverpb.GetUpdateStatusSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(serverpb.File_kent_api_server_server_proto, "ServerService", "GetUpdateStatus"),
		req,
		&serverpb.GetUpdateStatusResult{},
		func(failure *serverpb.GetUpdateStatusError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) ProjectID() string {
	if binding, present := c.projectBinding(); present {
		return binding.ProjectID
	}
	return ""
}

func (c *Remote) ProjectBinding() (ProjectAttachment, bool) {
	return c.projectBinding()
}

func (c *Remote) projectBinding() (ProjectAttachment, bool) {
	if c == nil {
		return ProjectAttachment{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return remoteAttachmentProjectBinding(c.attachment)
}

func callUnscopedRPC[Req any, Resp any](c *Remote, ctx context.Context, method string, req Req) (Resp, error) {
	var resp Resp
	return resp, c.callUnscoped(ctx, method, req, &resp)
}

func callDedicatedRPC[Req any, Resp any](c *Remote, ctx context.Context, requestID string, method string, req Req) (Resp, error) {
	var resp Resp
	return resp, c.callDedicated(ctx, requestID, method, req, &resp)
}

func (c *Remote) GetBootstrapStatus(ctx context.Context, req *authpb.GetBootstrapStatusRequest) (*authpb.BootstrapStatus, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "GetBootstrapStatus"),
		req,
		&authpb.GetBootstrapStatusResult{},
		func(failure *authpb.GetBootstrapStatusError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure(), failure.GetConnectionFailure())
		})
}

func (c *Remote) GetConnections(ctx context.Context, req *authpb.GetConnectionsRequest) (*authpb.ConnectionCatalog, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "GetConnections"),
		req, &authpb.GetConnectionsResult{},
		func(failure *authpb.GetBootstrapStatusError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure(), failure.GetConnectionFailure())
		})
}

func (c *Remote) ConfigureConnection(ctx context.Context, req *authpb.ConfigureConnectionRequest) (*emptypb.Empty, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "ConfigureConnection"),
		req, &authpb.ConfigureConnectionResult{},
		func(failure *authpb.CompleteBootstrapError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure(), failure.GetConnectionFailure())
		})
}

func (c *Remote) CompleteBootstrap(ctx context.Context, req *authpb.CompleteBootstrapRequest) (*authpb.BootstrapCompletion, error) {
	resp, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "CompleteBootstrap"),
		req,
		&authpb.CompleteBootstrapResult{},
		func(failure *authpb.CompleteBootstrapError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure(), failure.GetConnectionFailure())
		})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Remote) GetStatus(ctx context.Context, req *authpb.GetStatusRequest) (*authpb.Status, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "GetStatus"),
		req,
		&authpb.GetStatusResult{},
		func(failure *authpb.GetStatusError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure(), nil)
		})
}

func (c *Remote) CreateWorkflow(ctx context.Context, req *workflowpb.CreateRequest) (*workflowpb.CreateSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Create"), req, &workflowpb.CreateResult{},
		func(failure *workflowpb.CreateError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) CreateAndLinkWorkflowToProject(ctx context.Context, req *workflowpb.CreateAndLinkProjectRequest) (*workflowpb.CreateAndLinkProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "CreateAndLinkProject"), req, &workflowpb.CreateAndLinkProjectResult{},
		func(failure *workflowpb.CreateAndLinkProjectError) error {
			return projectNotFoundGeneratedError(failure.Code, failure.GetProjectNotFound())
		})
}

func (c *Remote) UpdateWorkflow(ctx context.Context, req *workflowpb.UpdateRequest) (*workflowpb.GetSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Update"), req, &workflowpb.UpdateResult{},
		func(failure *workflowpb.UpdateError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) ListWorkflows(ctx context.Context, req *workflowpb.ListRequest) (*workflowpb.ListSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "List"), req, &workflowpb.ListResult{},
		func(failure *workflowpb.ListError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) GetWorkflow(ctx context.Context, req *workflowpb.GetRequest) (*workflowpb.GetSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Get"), req, &workflowpb.GetResult{},
		func(failure *workflowpb.GetError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) LinkWorkflowToProject(ctx context.Context, req *workflowpb.LinkProjectRequest) (*workflowpb.LinkProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "Link"), req, &workflowpb.LinkProjectResult{},
		func(failure *workflowpb.ProjectLinkError) error {
			if failure.GetProjectNotFound() != nil {
				return projectNotFoundError(failure.GetProjectNotFound())
			}
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) ListProjectWorkflowLinks(ctx context.Context, req *workflowpb.ListProjectLinksRequest) (*workflowpb.ListProjectLinksSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "List"), req, &workflowpb.ListProjectLinksResult{},
		func(failure *workflowpb.ProjectLinksListError) error {
			return projectNotFoundGeneratedError(failure.Code, failure.GetProjectNotFound())
		})
}

func (c *Remote) SetDefaultProjectWorkflowLink(ctx context.Context, req *workflowpb.SetDefaultProjectLinkRequest) (*workflowpb.SetDefaultProjectLinkSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "SetDefault"), req, &workflowpb.SetDefaultProjectLinkResult{},
		func(failure *workflowpb.SetDefaultProjectLinkError) error {
			if failure.GetProjectNotFound() != nil {
				return projectNotFoundError(failure.GetProjectNotFound())
			}
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) UnlinkWorkflowFromProject(ctx context.Context, req *workflowpb.UnlinkProjectRequest) (*workflowpb.UnlinkProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "Unlink"), req, &workflowpb.UnlinkProjectResult{},
		func(failure *workflowpb.UnlinkProjectError) error {
			if detail := failure.GetReplacementDefaultInvalid(); detail != nil {
				return fmt.Errorf("replacement default workflow link is invalid for link %q", detail.LinkId)
			}
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) PreviewWorkflowDelete(ctx context.Context, req *workflowpb.DeletePreviewRequest) (*workflowpb.DeletePreviewSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "DeletePreview"), req, &workflowpb.DeletePreviewResult{},
		func(failure *workflowpb.DeletePreviewError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) DeleteWorkflow(ctx context.Context, req *workflowpb.DeleteRequest) (*workflowpb.DeleteSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Delete"), req, &workflowpb.DeleteResult{},
		func(failure *workflowpb.DeleteError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) ValidateWorkflow(ctx context.Context, req *workflowpb.ValidateRequest) (*workflowpb.ValidateResponse, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Validate"), req, &workflowpb.ValidateResult{},
		func(failure *workflowpb.ValidateError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) ValidateWorkflowScriptPath(ctx context.Context, req *workflowpb.ScriptPathValidateRequest) (*workflowpb.ValidateResponse, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "ValidateScriptPath"), req, &workflowpb.ValidateScriptPathResult{},
		func(failure *workflowpb.ValidateScriptPathError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) ValidateWorkflowGraphDraft(ctx context.Context, req *workflowpb.GraphValidateDraftRequest) (*workflowpb.GraphValidateDraftSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "ValidateDraft"), req, &workflowpb.GraphValidateDraftResult{},
		func(failure *workflowpb.GraphValidateDraftError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) DeriveWorkflowGraphWiring(ctx context.Context, req *workflowpb.GraphDeriveWiringRequest) (*workflowpb.GraphDeriveWiringSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "DeriveWiring"), req, &workflowpb.GraphDeriveWiringResult{},
		func(failure *workflowpb.GraphDeriveWiringError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) PreviewWorkflowGraphSave(ctx context.Context, req *workflowpb.GraphSavePreviewRequest) (*workflowpb.GraphSavePreviewSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "SavePreview"), req, &workflowpb.GraphSavePreviewResult{},
		func(failure *workflowpb.GraphSavePreviewError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) SaveWorkflowGraph(ctx context.Context, req *workflowpb.GraphSaveRequest) (*workflowpb.GraphSaveSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "Save"), req, &workflowpb.GraphSaveResult{},
		func(failure *workflowpb.GraphSaveError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		})
}

func (c *Remote) CreateWorkflowTask(ctx context.Context, req *taskpb.CreateRequest) (*taskpb.CreateSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Create")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.CreateResult{},
		func(failure *taskpb.CreateError) error {
			if failure.GetLabel() != nil {
				return &WorkflowLabelError{Detail: failure.GetLabel()}
			}
			return &TaskCreateError{Failure: failure}
		})
}

func (c *Remote) AddWorkflowTaskDependency(ctx context.Context, req *taskpb.DependencyAddRequest) (*taskpb.DependencyMutationSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskDependencyService").Methods().ByName("Add")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.DependencyAddResult{}, taskDependencyGeneratedError[*taskpb.DependencyAddError])
}

func (c *Remote) RemoveWorkflowTaskDependency(ctx context.Context, req *taskpb.DependencyRemoveRequest) (*taskpb.DependencyMutationSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskDependencyService").Methods().ByName("Remove")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.DependencyRemoveResult{}, taskDependencyGeneratedError[*taskpb.DependencyRemoveError])
}

func (c *Remote) ListWorkflowTaskDependencies(ctx context.Context, req *taskpb.DependencyListRequest) (*taskpb.DependencyListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskDependencyService").Methods().ByName("List")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.DependencyListResult{}, taskEntityGeneratedError[*taskpb.DependencyListError])
}

func (c *Remote) UpdateWorkflowTask(ctx context.Context, req *taskpb.UpdateRequest) (*taskpb.UpdateSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Update")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.UpdateResult{}, taskEntityGeneratedError[*taskpb.UpdateError])
}

func (c *Remote) StartWorkflowTask(ctx context.Context, req *taskpb.StartRequest) (*taskpb.StartSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Start")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.StartResult{},
		func(failure *taskpb.StartError) error {
			if conflict := failure.GetStartConflict(); conflict != nil {
				return &serverapi.WorkflowTaskStartConflictError{TaskID: conflict.TaskId, Reason: serverapi.WorkflowTaskStartConflictAlreadyStarted}
			}
			if branch := failure.GetInitialBranch(); branch != nil {
				return taskInitialBranchGeneratedError(branch)
			}
			return taskExecutionGeneratedError(failure)
		})
}

func (c *Remote) InterruptWorkflowTask(ctx context.Context, req *taskpb.InterruptRequest) (*emptypb.Empty, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Interrupt")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.InterruptResult{}, taskMutationGeneratedError[*taskpb.InterruptError])
}

func (c *Remote) ResumeWorkflowTask(ctx context.Context, req *taskpb.ResumeRequest) (*taskpb.ResumeSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Resume")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.ResumeResult{},
		func(failure *taskpb.ResumeError) error {
			if detail := failure.GetContextSelectionRequired(); detail != nil {
				return &serverapi.WorkflowTaskContextSelectionRequiredError{TaskID: detail.TaskId}
			}
			if branch := failure.GetInitialBranch(); branch != nil {
				return taskInitialBranchGeneratedError(branch)
			}
			return taskExecutionGeneratedError(failure)
		})
}

func (c *Remote) ApproveWorkflowTask(ctx context.Context, req *taskpb.ApproveRequest) (*taskpb.ApproveSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Approve")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.ApproveResult{},
		func(failure *taskpb.ApproveError) error {
			if detail := failure.GetContextSelectionRequired(); detail != nil {
				return &serverapi.WorkflowTaskContextSelectionRequiredError{TaskID: detail.TaskId}
			}
			return taskExecutionGeneratedError(failure)
		})
}

func (c *Remote) PreviewWorkflowTaskMove(ctx context.Context, req *taskpb.MovePreviewRequest) (*taskpb.MovePreviewSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("PreviewMove")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.MovePreviewResult{}, taskEntityGeneratedError[*taskpb.MovePreviewError])
}

func (c *Remote) MoveWorkflowTask(ctx context.Context, req *taskpb.MoveRequest) (*taskpb.MoveSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Move")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.MoveResult{},
		func(failure *taskpb.MoveError) error {
			if branch := failure.GetInitialBranch(); branch != nil {
				return taskInitialBranchGeneratedError(branch)
			}
			return taskExecutionGeneratedError(failure)
		})
}

func (c *Remote) CompleteWorkflowTask(ctx context.Context, req *taskpb.CompleteRequest) (*taskpb.CompleteSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Complete")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.CompleteResult{},
		func(failure *taskpb.CompleteError) error {
			switch failure.GetCode() {
			case "task_not_found":
				return serverapi.ErrWorkflowTaskNotFound
			case "completion_target_not_found":
				return serverapi.ErrWorkflowTaskCompleteTargetNotFound
			case "completion_selector_ambiguous":
				return serverapi.ErrWorkflowTaskCompleteSelectorAmbiguous
			case "execution_target_resolution":
				return taskExecutionResolutionGeneratedError(failure.GetExecutionTargetResolution())
			case "locked_execution_target":
				return taskLockedTargetGeneratedError(failure.GetLockedExecutionTarget())
			case "initial_branch":
				return taskInitialBranchGeneratedError(failure.GetInitialBranch())
			default:
				return worktreeError(failure)
			}
		})
}

func (c *Remote) DeleteWorkflowTask(ctx context.Context, req *taskpb.DeleteRequest) (*emptypb.Empty, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService").Methods().ByName("Delete")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.DeleteResult{},
		func(failure *taskpb.DeleteError) error {
			if failure.GetTaskNotFound() != nil {
				return serverapi.ErrWorkflowTaskNotFound
			}
			return worktreeError(failure)
		})
}

func (c *Remote) ListWorkflowAttention(ctx context.Context, req *taskpb.AttentionListRequest) (*taskpb.AttentionListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_attention_proto.Services().ByName("AttentionReadService").Methods().ByName("List")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.AttentionListResult{}, func(failure *taskpb.AttentionListError) error {
		return generatedOperationFailure(failure.Code)
	})
}

func (c *Remote) ListWorkflowTaskAttention(ctx context.Context, req *taskpb.TaskAttentionListRequest) (*taskpb.TaskAttentionListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_attention_proto.Services().ByName("AttentionReadService").Methods().ByName("ListTask")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.TaskAttentionListResult{}, func(failure *taskpb.TaskAttentionListError) error {
		return generatedOperationFailure(failure.Code)
	})
}

func (c *Remote) AddWorkflowTaskComment(ctx context.Context, req *taskpb.CommentAddRequest) (*taskpb.CommentAddSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskCommentService").Methods().ByName("Add")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.CommentAddResult{}, taskEntityGeneratedError[*taskpb.CommentAddError])
}

func (c *Remote) ListWorkflowTaskComments(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.CommentListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskCommentService").Methods().ByName("List")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.CommentListResult{}, taskEntityGeneratedError[*taskpb.CommentListError])
}

func (c *Remote) ReplaceWorkflowTaskComment(ctx context.Context, req *taskpb.CommentReplaceRequest) (*emptypb.Empty, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskCommentService").Methods().ByName("Replace")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.CommentReplaceResult{}, taskEntityGeneratedError[*taskpb.CommentReplaceError])
}

func (c *Remote) DeleteWorkflowTaskComment(ctx context.Context, req *taskpb.CommentDeleteRequest) (*emptypb.Empty, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskCommentService").Methods().ByName("Delete")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.CommentDeleteResult{}, taskEntityGeneratedError[*taskpb.CommentDeleteError])
}

func (c *Remote) ListWorkflowTaskActivity(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.ActivityListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskActivityService").Methods().ByName("List")
	response, err := callGeneratedBinary(c, ctx, method, req, &taskpb.ActivityListResult{}, taskEntityGeneratedError[*taskpb.ActivityListError])
	if err != nil {
		return nil, err
	}
	for _, item := range response.Items {
		if item.TaskId != req.TaskId {
			return nil, fmt.Errorf("Task activity returned an item for another Task")
		}
	}
	return response, nil
}

func (c *Remote) ListWorkflowTaskSessions(ctx context.Context, req *taskpb.TaskOffsetPageRequest) (*taskpb.SessionListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskSessionService").Methods().ByName("List")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.SessionListResult{}, taskEntityGeneratedError[*taskpb.SessionListError])
}

func (c *Remote) ListWorkflowTasks(ctx context.Context, req *taskpb.ListRequest) (*taskpb.ListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskReadService").Methods().ByName("List")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.ListResult{},
		func(failure *taskpb.ListError) error {
			if failure.GetScopeError() != nil {
				return &TaskListError{Failure: failure}
			}
			return taskReadGeneratedError(failure)
		})
}

func (c *Remote) GetWorkflowProjectTaskGroupCounts(ctx context.Context, req *taskpb.ProjectTaskGroupCountsRequest) (*taskpb.ProjectTaskGroupCountsSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskReadService").Methods().ByName("GetProjectGroupCounts")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.ProjectTaskGroupCountsResult{},
		func(failure *taskpb.ProjectTaskGroupCountsError) error {
			return projectNotFoundGeneratedError(failure.Code, failure.GetProjectNotFound())
		})
}

func (c *Remote) SearchWorkflowTasks(ctx context.Context, req *taskpb.SearchRequest) (*taskpb.SearchSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskReadService").Methods().ByName("Search")
	response, err := callGeneratedBinary(c, ctx, method, req, &taskpb.SearchResult{},
		func(failure *taskpb.SearchError) error {
			if failure.GetNormalizedTooShort() != nil {
				return &serverapi.TaskSearchError{Reason: serverapi.TaskSearchErrorReasonNormalizedTooShort}
			}
			return generatedOperationFailure(failure.Code)
		})
	if err != nil {
		return nil, err
	}
	if response.Mode != req.Mode {
		return nil, fmt.Errorf(
			"search workflow tasks returned mode %q for request mode %q",
			response.Mode,
			req.Mode,
		)
	}
	return response, nil
}

func (c *Remote) GetWorkflowBoard(ctx context.Context, req *taskpb.BoardGetRequest) (*taskpb.BoardGetSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("BoardReadService").Methods().ByName("Get")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.BoardGetResult{}, taskReadGeneratedError[*taskpb.BoardGetError])
}

func (c *Remote) ListWorkflowBoardNodeCards(ctx context.Context, req *taskpb.BoardNodeCardsListRequest) (*taskpb.BoardNodeCardsListSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("BoardReadService").Methods().ByName("ListNodeCards")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.BoardNodeCardsListResult{}, taskReadGeneratedError[*taskpb.BoardNodeCardsListError])
}

func (c *Remote) GetWorkflowTask(ctx context.Context, req *taskpb.GetRequest) (*taskpb.GetSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskReadService").Methods().ByName("Get")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.GetResult{}, taskEntityGeneratedError[*taskpb.GetError])
}

func (c *Remote) ObserveWorkflowTask(ctx context.Context, req *taskpb.ObserveRequest) (*taskpb.ObserveSuccess, error) {
	method := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskObservationService").Methods().ByName("Observe")
	return callGeneratedBinary(c, ctx, method, req, &taskpb.ObserveResult{}, taskEntityGeneratedError[*taskpb.ObserveError])
}

func (c *Remote) ReadChatSettings(
	ctx context.Context,
	req *chatsettingspb.ReadRequest,
) (*chatsettingspb.ReadSuccess, error) {
	response, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(chatsettingspb.File_kent_api_chat_settings_chat_settings_proto, "ChatSettingsService", "Read"),
		req, &chatsettingspb.ReadResult{}, protoapi.ChatSettingsErrorFromProto)
	if err != nil {
		return nil, err
	}
	switch target := req.Target.(type) {
	case *chatsettingspb.ReadRequest_NewChat:
		if response.GetNewChat() == nil {
			return nil, invalidResponseError("Chat settings", errors.New("New Chat catalog is required"))
		}
	case *chatsettingspb.ReadRequest_Session:
		if response.GetSession() == nil || response.GetSession().Session.SessionId != target.Session.SessionId {
			return nil, invalidResponseError("Chat settings", errors.New("response must contain the target Session"))
		}
	}
	return response, nil
}

func (c *Remote) MutateChatSettings(
	ctx context.Context,
	req *chatsettingspb.MutationRequest,
) (*chatsettingspb.MutationSuccess, error) {
	response, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(chatsettingspb.File_kent_api_chat_settings_chat_settings_proto, "ChatSettingsService", "Mutate"),
		req, &chatsettingspb.MutationResponse{}, protoapi.ChatSettingsErrorFromProto)
	if err != nil {
		return nil, err
	}
	if response.Session.SessionId != req.Session.SessionId {
		return nil, invalidResponseError("Chat settings mutation", errors.New("response must contain the target Session"))
	}
	return response, nil
}

func (c *Remote) ensureOpen() error {
	if c == nil {
		return errors.New("remote client is required")
	}
	if c.closed.Load() {
		return errors.New("remote client is closed")
	}
	return nil
}

func (c *Remote) callUnscoped(ctx context.Context, method string, params any, out any) error {
	control, err := c.ensureControl(ctx)
	if err != nil {
		return err
	}
	return control.call(ctx, method, params, out)
}

func (c *Remote) ensureControl(ctx context.Context) (*remoteControlConn, error) {
	if err := c.ensureOpen(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return nil, errors.New("remote client is closed")
	}
	if c.control != nil && !c.control.IsDone() {
		return c.control, nil
	}
	if c.control != nil {
		_ = c.control.Close()
		c.control = nil
	}
	conn, cleanup, state, err := c.openSetupRPCConnForAttachment(
		ctx,
		nil,
		c.attachIntent,
		c.attachment,
	)
	if err != nil {
		return nil, err
	}
	if c.closed.Load() {
		cleanup()
		return nil, errors.New("remote client is closed")
	}
	control := newRemoteControlConn(conn)
	c.control = control
	c.identity = state.identity
	c.attachment = state.attachment
	if state.attachment != nil && state.attachment.session != nil {
		c.attachIntent, err = newRemoteSessionReattachmentIntent(*state.attachment.session)
		if err != nil {
			_ = control.Close()
			c.control = nil
			return nil, err
		}
	}
	return control, nil
}

func dialRemoteURL(ctx context.Context, rpcURL string, intent *remoteAttachmentIntent) (*Remote, error) {
	endpoint, err := rpcwire.ParseWebSocketEndpoint(strings.TrimSpace(rpcURL))
	if err != nil {
		return nil, err
	}
	return dialRemoteWithTransport(ctx, remoteDialPlan{endpoints: []rpcwire.Endpoint{endpoint}}, rpcwire.NewWebSocketTransport(), intent)
}

func dialConfiguredRemote(ctx context.Context, cfg config.Connection, intent *remoteAttachmentIntent) (*Remote, error) {
	plan, err := configuredRemoteDialPlan(cfg)
	if err != nil {
		return nil, err
	}
	return dialRemoteWithTransport(ctx, plan, rpcwire.NewWebSocketTransport(), intent)
}

var _ apicontract.ProjectViewService = (*Remote)(nil)
var _ apicontract.AuthStatusService = (*Remote)(nil)
var _ apicontract.ChatContextService = (*Remote)(nil)
var _ apicontract.SessionLaunchService = (*Remote)(nil)
var _ apicontract.SessionViewService = (*Remote)(nil)
var _ apicontract.SessionLifecycleService = (*Remote)(nil)
var _ apicontract.SessionRuntimeService = (*Remote)(nil)
var _ apicontract.RuntimeControlService = (*Remote)(nil)
var _ apicontract.RuntimeLiveControlService = (*Remote)(nil)
var _ apicontract.ProcessViewService = (*Remote)(nil)
var _ apicontract.ProcessControlService = (*Remote)(nil)
var _ apicontract.SessionTranscriptService = (*Remote)(nil)
var _ apicontract.AttentionNotificationService = (*Remote)(nil)
var _ apicontract.RunPromptService = (*Remote)(nil)
var _ apicontract.AskViewService = (*Remote)(nil)
var _ apicontract.PromptControlService = (*Remote)(nil)
var _ apicontract.ApprovalViewService = (*Remote)(nil)
