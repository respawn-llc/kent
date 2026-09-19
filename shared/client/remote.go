package client

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	noAuthAck      atomic.Bool
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

func DialConfiguredRemote(ctx context.Context, cfg config.App) (*Remote, error) {
	return dialConfiguredRemote(ctx, cfg, nil)
}

func DialConfiguredRemoteForProjectWorkspace(ctx context.Context, cfg config.App, projectID string, workspaceRoot string) (*Remote, error) {
	intent, err := newRemoteProjectWorkspaceRootAttachmentIntent(projectID, workspaceRoot)
	if err != nil {
		return nil, err
	}
	return dialConfiguredRemote(ctx, cfg, intent)
}

func DialConfiguredRemoteForProjectWorkspaceID(ctx context.Context, cfg config.App, projectID string, workspaceID string) (*Remote, error) {
	intent, err := newRemoteProjectWorkspaceIDAttachmentIntent(projectID, workspaceID)
	if err != nil {
		return nil, err
	}
	return dialConfiguredRemote(ctx, cfg, intent)
}

func DialConfiguredRemoteForSession(ctx context.Context, cfg config.App, sessionID string) (*Remote, error) {
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

func (c *Remote) EnableNoAuthBootstrapAcknowledgement(ctx context.Context) error {
	if c == nil {
		return errors.New("remote client is required")
	}
	resp, err := c.AcknowledgeNoAuth(ctx, &emptypb.Empty{})
	if err != nil {
		c.noAuthAck.Store(false)
		return err
	}
	if resp.NoAuthSelected {
		c.noAuthAck.Store(true)
		return nil
	}
	c.noAuthAck.Store(false)
	if resp.AuthReady {
		return nil
	}
	return serverapi.ErrServerAuthRequired
}

func (c *Remote) DisableNoAuthBootstrapAcknowledgement() {
	if c != nil {
		c.noAuthAck.Store(false)
	}
}

func (c *Remote) NoAuthBootstrapAcknowledgementEnabled() bool {
	return c != nil && c.noAuthAck.Load()
}

func (c *Remote) acknowledgeNoAuthOnConn(ctx context.Context, conn rpcwire.Conn) error {
	if c == nil || !c.noAuthAck.Load() {
		return nil
	}
	result := &authpb.AcknowledgeNoAuthResult{}
	if err := callBinaryRPC(
		ctx,
		conn,
		"auth-acknowledge-no-auth",
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "AcknowledgeNoAuth"),
		&emptypb.Empty{},
		result,
	); err != nil {
		if errors.Is(err, serverapi.ErrServerAuthRequired) {
			c.noAuthAck.Store(false)
		}
		return err
	}
	if result.GetError() != nil {
		err := authGeneratedError(result.GetError().Code, result.GetError().GetInternalFailure())
		if errors.Is(err, serverapi.ErrServerAuthRequired) {
			c.noAuthAck.Store(false)
		}
		return err
	}
	resp := result.GetSuccess()
	if resp.NoAuthSelected {
		return nil
	}
	c.noAuthAck.Store(false)
	if resp.AuthReady {
		return nil
	}
	return serverapi.ErrServerAuthRequired
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

func (c *Remote) GetBootstrapStatus(ctx context.Context, req *emptypb.Empty) (*authpb.BootstrapStatus, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "GetBootstrapStatus"),
		req,
		&authpb.GetBootstrapStatusResult{},
		func(failure *authpb.GetBootstrapStatusError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) CompleteBootstrap(ctx context.Context, req *authpb.CompleteBootstrapRequest) (*authpb.BootstrapCompletion, error) {
	resp, err := callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "CompleteBootstrap"),
		req,
		&authpb.CompleteBootstrapResult{},
		func(failure *authpb.CompleteBootstrapError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure())
		})
	if err != nil {
		return nil, err
	}
	if resp.GetNoAuthSelected() {
		c.noAuthAck.Store(true)
	} else if resp.GetAuthReady() {
		c.noAuthAck.Store(false)
	}
	return resp, nil
}

func (c *Remote) AcknowledgeNoAuth(ctx context.Context, req *emptypb.Empty) (*authpb.NoAuthAcknowledgement, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "AcknowledgeNoAuth"),
		req,
		&authpb.AcknowledgeNoAuthResult{},
		func(failure *authpb.AcknowledgeNoAuthError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) GetStatus(ctx context.Context, req *authpb.GetStatusRequest) (*authpb.Status, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(authpb.File_kent_api_auth_auth_proto, "AuthService", "GetStatus"),
		req,
		&authpb.GetStatusResult{},
		func(failure *authpb.GetStatusError) error {
			return authGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) CreateWorkflow(ctx context.Context, req *workflowpb.CreateRequest) (*workflowpb.CreateSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Create"), req, &workflowpb.CreateResult{},
		func(failure *workflowpb.CreateError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) CreateAndLinkWorkflowToProject(ctx context.Context, req *workflowpb.CreateAndLinkProjectRequest) (*workflowpb.CreateAndLinkProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "CreateAndLinkProject"), req, &workflowpb.CreateAndLinkProjectResult{},
		func(failure *workflowpb.CreateAndLinkProjectError) error {
			return projectNotFoundGeneratedError(failure.Code, failure.GetProjectNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) UpdateWorkflow(ctx context.Context, req *workflowpb.UpdateRequest) (*workflowpb.GetSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Update"), req, &workflowpb.UpdateResult{},
		func(failure *workflowpb.UpdateError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) ListWorkflows(ctx context.Context, req *workflowpb.ListRequest) (*workflowpb.ListSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "List"), req, &workflowpb.ListResult{},
		func(failure *workflowpb.ListError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) GetWorkflow(ctx context.Context, req *workflowpb.GetRequest) (*workflowpb.GetSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Get"), req, &workflowpb.GetResult{},
		func(failure *workflowpb.GetError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) LinkWorkflowToProject(ctx context.Context, req *workflowpb.LinkProjectRequest) (*workflowpb.LinkProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "Link"), req, &workflowpb.LinkProjectResult{},
		func(failure *workflowpb.ProjectLinkError) error {
			if failure.GetProjectNotFound() != nil {
				return projectNotFoundError(failure.GetProjectNotFound())
			}
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) ListProjectWorkflowLinks(ctx context.Context, req *workflowpb.ListProjectLinksRequest) (*workflowpb.ListProjectLinksSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "List"), req, &workflowpb.ListProjectLinksResult{},
		func(failure *workflowpb.ProjectLinksListError) error {
			return projectNotFoundGeneratedError(failure.Code, failure.GetProjectNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) SetDefaultProjectWorkflowLink(ctx context.Context, req *workflowpb.SetDefaultProjectLinkRequest) (*workflowpb.SetDefaultProjectLinkSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "SetDefault"), req, &workflowpb.SetDefaultProjectLinkResult{},
		func(failure *workflowpb.SetDefaultProjectLinkError) error {
			if failure.GetProjectNotFound() != nil {
				return projectNotFoundError(failure.GetProjectNotFound())
			}
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) UnlinkWorkflowFromProject(ctx context.Context, req *workflowpb.UnlinkProjectRequest) (*workflowpb.UnlinkProjectSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("ProjectLinkService", "Unlink"), req, &workflowpb.UnlinkProjectResult{},
		func(failure *workflowpb.UnlinkProjectError) error {
			if detail := failure.GetReplacementDefaultInvalid(); detail != nil {
				return fmt.Errorf("replacement default workflow link is invalid for link %q", detail.LinkId)
			}
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) PreviewWorkflowDelete(ctx context.Context, req *workflowpb.DeletePreviewRequest) (*workflowpb.DeletePreviewSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "DeletePreview"), req, &workflowpb.DeletePreviewResult{},
		func(failure *workflowpb.DeletePreviewError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) DeleteWorkflow(ctx context.Context, req *workflowpb.DeleteRequest) (*workflowpb.DeleteSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Delete"), req, &workflowpb.DeleteResult{},
		func(failure *workflowpb.DeleteError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) ValidateWorkflow(ctx context.Context, req *workflowpb.ValidateRequest) (*workflowpb.ValidateResponse, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "Validate"), req, &workflowpb.ValidateResult{},
		func(failure *workflowpb.ValidateError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) ValidateWorkflowScriptPath(ctx context.Context, req *workflowpb.ScriptPathValidateRequest) (*workflowpb.ValidateResponse, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowDefinitionService", "ValidateScriptPath"), req, &workflowpb.ValidateScriptPathResult{},
		func(failure *workflowpb.ValidateScriptPathError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) ValidateWorkflowGraphDraft(ctx context.Context, req *workflowpb.GraphValidateDraftRequest) (*workflowpb.GraphValidateDraftSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "ValidateDraft"), req, &workflowpb.GraphValidateDraftResult{},
		func(failure *workflowpb.GraphValidateDraftError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) DeriveWorkflowGraphWiring(ctx context.Context, req *workflowpb.GraphDeriveWiringRequest) (*workflowpb.GraphDeriveWiringSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "DeriveWiring"), req, &workflowpb.GraphDeriveWiringResult{},
		func(failure *workflowpb.GraphDeriveWiringError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) PreviewWorkflowGraphSave(ctx context.Context, req *workflowpb.GraphSavePreviewRequest) (*workflowpb.GraphSavePreviewSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "SavePreview"), req, &workflowpb.GraphSavePreviewResult{},
		func(failure *workflowpb.GraphSavePreviewError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) SaveWorkflowGraph(ctx context.Context, req *workflowpb.GraphSaveRequest) (*workflowpb.GraphSaveSuccess, error) {
	return callGeneratedBinary(c, ctx, workflowMethod("WorkflowGraphService", "Save"), req, &workflowpb.GraphSaveResult{},
		func(failure *workflowpb.GraphSaveError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound(), failure.GetInternalFailure())
		})
}

func (c *Remote) CreateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCreateRequest) (serverapi.WorkflowTaskCreateResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowTaskCreateRequest, serverapi.WorkflowTaskCreateResponse](c, ctx, protocol.MethodWorkflowTaskCreate, req)
}

func (c *Remote) AddWorkflowTaskDependency(ctx context.Context, req serverapi.WorkflowTaskDependencyAddRequest) (serverapi.WorkflowTaskDependencyAddResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskDependencyAddRequest, serverapi.WorkflowTaskDependencyAddResponse](c, ctx, protocol.MethodWorkflowTaskDependencyAdd, req)
	return validateWorkflowResponse("add workflow task dependency", response, err)
}

func (c *Remote) RemoveWorkflowTaskDependency(ctx context.Context, req serverapi.WorkflowTaskDependencyRemoveRequest) (serverapi.WorkflowTaskDependencyRemoveResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskDependencyRemoveRequest, serverapi.WorkflowTaskDependencyRemoveResponse](c, ctx, protocol.MethodWorkflowTaskDependencyRemove, req)
	return validateWorkflowResponse("remove workflow task dependency", response, err)
}

func (c *Remote) ListWorkflowTaskDependencies(ctx context.Context, req serverapi.WorkflowTaskDependencyListRequest) (serverapi.WorkflowTaskDependencyListResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskDependencyListRequest, serverapi.WorkflowTaskDependencyListResponse](c, ctx, protocol.MethodWorkflowTaskDependencyList, req)
	return validateWorkflowResponse("list workflow task dependencies", response, err)
}

func (c *Remote) UpdateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskUpdateRequest) (serverapi.WorkflowTaskUpdateResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowTaskUpdateRequest, serverapi.WorkflowTaskUpdateResponse](c, ctx, protocol.MethodWorkflowTaskUpdate, req)
}

func (c *Remote) StartWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskStartRequest, serverapi.WorkflowTaskStartResponse](c, ctx, protocol.MethodWorkflowTaskStart, req)
	return validateWorkflowResponse("start workflow task", response, err)
}

func (c *Remote) InterruptWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskInterruptRequest) (serverapi.WorkflowTaskInterruptResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowTaskInterruptRequest, serverapi.WorkflowTaskInterruptResponse](c, ctx, protocol.MethodWorkflowTaskInterrupt, req)
}

func (c *Remote) ResumeWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskResumeRequest) (serverapi.WorkflowTaskResumeResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskResumeRequest, serverapi.WorkflowTaskResumeResponse](c, ctx, protocol.MethodWorkflowTaskResume, req)
	return validateWorkflowResponse("resume workflow task", response, err)
}

func (c *Remote) ApproveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskApproveRequest) (serverapi.WorkflowTaskApproveResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskApproveRequest, serverapi.WorkflowTaskApproveResponse](c, ctx, protocol.MethodWorkflowTaskApprove, req)
	return validateWorkflowResponse("approve workflow task", response, err)
}

func (c *Remote) PreviewWorkflowTaskMove(ctx context.Context, req serverapi.WorkflowTaskMovePreviewRequest) (serverapi.WorkflowTaskMovePreviewResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskMovePreviewRequest, serverapi.WorkflowTaskMovePreviewResponse](c, ctx, protocol.MethodWorkflowTaskMovePreview, req)
	return validateWorkflowResponse("preview workflow task move", response, err)
}

func (c *Remote) MoveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskMoveRequest) (serverapi.WorkflowTaskMoveResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskMoveRequest, serverapi.WorkflowTaskMoveResponse](c, ctx, protocol.MethodWorkflowTaskMove, req)
	return validateWorkflowResponse("move workflow task", response, err)
}

func (c *Remote) CompleteWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCompleteRequest) (serverapi.WorkflowTaskCompleteResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowTaskCompleteRequest, serverapi.WorkflowTaskCompleteResponse](c, ctx, protocol.MethodWorkflowTaskComplete, req)
}

func (c *Remote) DeleteWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskDeleteRequest) error {
	return c.callUnscoped(ctx, protocol.MethodWorkflowTaskDelete, req, &struct{}{})
}

func (c *Remote) ListWorkflowAttention(ctx context.Context, req serverapi.WorkflowAttentionListRequest) (serverapi.WorkflowAttentionListResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowAttentionListRequest, serverapi.WorkflowAttentionListResponse](c, ctx, protocol.MethodWorkflowAttentionList, req)
	return validateWorkflowResponse("list workflow attention", response, err)
}

func (c *Remote) ListWorkflowTaskAttention(ctx context.Context, req serverapi.WorkflowTaskAttentionListRequest) (serverapi.WorkflowTaskAttentionListResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskAttentionListRequest, serverapi.WorkflowTaskAttentionListResponse](c, ctx, protocol.MethodWorkflowTaskAttentionList, req)
	return validateWorkflowTaskBoundResponse("list workflow task attention", strings.TrimSpace(req.TaskID), response, err)
}

func (c *Remote) AddWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowTaskCommentAddRequest, serverapi.WorkflowTaskCommentAddResponse](c, ctx, protocol.MethodWorkflowTaskCommentAdd, req)
}

func (c *Remote) ListWorkflowTaskComments(ctx context.Context, req serverapi.WorkflowTaskOffsetPageRequest) (serverapi.WorkflowTaskCommentListResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskCommentListResponse](c, ctx, protocol.MethodWorkflowTaskCommentList, req)
}

func (c *Remote) ReplaceWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentReplaceRequest) error {
	return c.callUnscoped(ctx, protocol.MethodWorkflowTaskCommentReplace, req, &struct{}{})
}

func (c *Remote) DeleteWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentDeleteRequest) error {
	return c.callUnscoped(ctx, protocol.MethodWorkflowTaskCommentDelete, req, &struct{}{})
}

func (c *Remote) ListWorkflowTaskActivity(ctx context.Context, req serverapi.WorkflowTaskOffsetPageRequest) (serverapi.WorkflowTaskActivityListResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskActivityListResponse](c, ctx, protocol.MethodWorkflowTaskActivityList, req)
	return validateWorkflowTaskBoundResponse("list workflow task activity", strings.TrimSpace(req.TaskID), response, err)
}

func (c *Remote) ListWorkflowTaskSessions(ctx context.Context, req serverapi.WorkflowTaskOffsetPageRequest) (serverapi.WorkflowTaskSessionListResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskSessionListResponse](c, ctx, protocol.MethodWorkflowTaskSessionList, req)
}

func (c *Remote) ListWorkflowTasks(ctx context.Context, req serverapi.WorkflowTaskListRequest) (serverapi.WorkflowTaskListResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskListRequest, serverapi.WorkflowTaskListResponse](c, ctx, protocol.MethodWorkflowTaskList, req)
	return validateWorkflowResponse("list workflow tasks", response, err)
}

func (c *Remote) GetWorkflowProjectTaskGroupCounts(ctx context.Context, req serverapi.WorkflowProjectTaskGroupCountsRequest) (serverapi.WorkflowProjectTaskGroupCountsResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowProjectTaskGroupCountsRequest, serverapi.WorkflowProjectTaskGroupCountsResponse](c, ctx, protocol.MethodWorkflowProjectTaskGroupCounts, req)
	return validateWorkflowResponse("get workflow project task group counts", response, err)
}

func (c *Remote) SearchWorkflowTasks(ctx context.Context, req serverapi.TaskSearchRequest) (serverapi.TaskSearchResponse, error) {
	response, err := callDedicatedRPC[serverapi.TaskSearchRequest, serverapi.TaskSearchResponse](
		c,
		ctx,
		apicontract.TaskSearchDedicatedRequestID,
		protocol.MethodWorkflowTaskSearch,
		req,
	)
	response, err = validateWorkflowResponse("search workflow tasks", response, err)
	if err != nil {
		return response, err
	}
	if response.Mode != req.Mode {
		return serverapi.TaskSearchResponse{}, fmt.Errorf(
			"search workflow tasks returned mode %q for request mode %q",
			response.Mode,
			req.Mode,
		)
	}
	return response, nil
}

func (c *Remote) GetWorkflowBoard(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
	return callUnscopedRPC[serverapi.WorkflowBoardRequest, serverapi.WorkflowBoardResponse](c, ctx, protocol.MethodWorkflowBoardGet, req)
}

func (c *Remote) ListWorkflowBoardNodeCards(ctx context.Context, req serverapi.WorkflowBoardNodeCardsListRequest) (serverapi.WorkflowBoardNodeCardsListResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowBoardNodeCardsListRequest, serverapi.WorkflowBoardNodeCardsListResponse](c, ctx, protocol.MethodWorkflowBoardNodeCardsList, req)
	return validateWorkflowResponse("list workflow board node cards", response, err)
}

func (c *Remote) GetWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskGetRequest, serverapi.WorkflowTaskGetResponse](c, ctx, protocol.MethodWorkflowTaskGet, req)
	return validateWorkflowResponse("get workflow task", response, err)
}

func (c *Remote) ObserveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskObservationRequest) (serverapi.WorkflowTaskObservationResponse, error) {
	response, err := callUnscopedRPC[serverapi.WorkflowTaskObservationRequest, serverapi.WorkflowTaskObservationResponse](c, ctx, protocol.MethodWorkflowTaskObserve, req)
	if err = normalizeWorkflowTaskObservationRPCError(err); err != nil {
		return serverapi.WorkflowTaskObservationResponse{}, err
	}
	if err := response.Validate(); err != nil {
		return serverapi.WorkflowTaskObservationResponse{}, invalidResponseError("workflow task observation", err)
	}
	return response, nil
}

func normalizeWorkflowTaskObservationRPCError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: workflow task observation RPC stream closed: %v", serverapi.ErrStreamFailed, err)
	}
	return err
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

func dialConfiguredRemote(ctx context.Context, cfg config.App, intent *remoteAttachmentIntent) (*Remote, error) {
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
