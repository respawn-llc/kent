package client

import (
	"context"
	"errors"

	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type WorkflowClient interface {
	CreateWorkflow(ctx context.Context, req serverapi.WorkflowCreateRequest) (serverapi.WorkflowCreateResponse, error)
	CreateAndLinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowCreateAndLinkProjectRequest) (serverapi.WorkflowCreateAndLinkProjectResponse, error)
	UpdateWorkflow(ctx context.Context, req serverapi.WorkflowUpdateRequest) (serverapi.WorkflowGetResponse, error)
	ListWorkflows(ctx context.Context, req serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error)
	GetWorkflow(ctx context.Context, req serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error)
	AddWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupAddRequest) (serverapi.WorkflowNodeGroupResponse, error)
	UpdateWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupUpdateRequest) (serverapi.WorkflowNodeGroupResponse, error)
	DeleteWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupDeleteRequest) error
	AddWorkflowNode(ctx context.Context, req serverapi.WorkflowNodeAddRequest) (serverapi.WorkflowNodeAddResponse, error)
	UpdateWorkflowNode(ctx context.Context, req serverapi.WorkflowNodeUpdateRequest) (serverapi.WorkflowNodeUpdateResponse, error)
	AddWorkflowTransitionGroup(ctx context.Context, req serverapi.WorkflowTransitionGroupAddRequest) (serverapi.WorkflowTransitionGroupAddResponse, error)
	UpdateWorkflowTransitionGroup(ctx context.Context, req serverapi.WorkflowTransitionGroupUpdateRequest) (serverapi.WorkflowTransitionGroupUpdateResponse, error)
	AddWorkflowEdge(ctx context.Context, req serverapi.WorkflowEdgeAddRequest) (serverapi.WorkflowEdgeAddResponse, error)
	UpdateWorkflowEdge(ctx context.Context, req serverapi.WorkflowEdgeUpdateRequest) (serverapi.WorkflowEdgeUpdateResponse, error)
	LinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowLinkProjectRequest) (serverapi.WorkflowLinkProjectResponse, error)
	ListProjectWorkflowLinks(ctx context.Context, req serverapi.WorkflowListProjectLinksRequest) (serverapi.WorkflowListProjectLinksResponse, error)
	SetDefaultProjectWorkflowLink(ctx context.Context, req serverapi.WorkflowSetDefaultProjectLinkRequest) (serverapi.WorkflowSetDefaultProjectLinkResponse, error)
	UnlinkWorkflowFromProject(ctx context.Context, req serverapi.WorkflowUnlinkProjectRequest) (serverapi.WorkflowUnlinkProjectResponse, error)
	PreviewWorkflowDelete(ctx context.Context, req serverapi.WorkflowDeletePreviewRequest) (serverapi.WorkflowDeletePreviewResponse, error)
	DeleteWorkflow(ctx context.Context, req serverapi.WorkflowDeleteRequest) (serverapi.WorkflowDeleteResponse, error)
	ValidateWorkflow(ctx context.Context, req serverapi.WorkflowValidateRequest) (serverapi.WorkflowValidateResponse, error)
	CreateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCreateRequest) (serverapi.WorkflowTaskCreateResponse, error)
	UpdateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskUpdateRequest) (serverapi.WorkflowTaskUpdateResponse, error)
	StartWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error)
	InterruptWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskInterruptRequest) (serverapi.WorkflowTaskInterruptResponse, error)
	ResumeWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskResumeRequest) (serverapi.WorkflowTaskResumeResponse, error)
	ApproveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskApproveRequest) (serverapi.WorkflowTaskApproveResponse, error)
	MoveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskMoveRequest) (serverapi.WorkflowTaskMoveResponse, error)
	CancelWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCancelRequest) error
	ListWorkflowAttention(ctx context.Context, req serverapi.WorkflowAttentionListRequest) (serverapi.WorkflowAttentionListResponse, error)
	ListWorkflowTaskAttention(ctx context.Context, req serverapi.WorkflowTaskAttentionListRequest) (serverapi.WorkflowTaskAttentionListResponse, error)
	AnswerWorkflowTaskQuestion(ctx context.Context, req serverapi.WorkflowTaskQuestionAnswerRequest) error
	AddWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error)
	ListWorkflowTaskComments(ctx context.Context, req serverapi.WorkflowTaskCommentListRequest) (serverapi.WorkflowTaskCommentListResponse, error)
	ReplaceWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentReplaceRequest) error
	DeleteWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentDeleteRequest) error
	ListWorkflowTaskActivity(ctx context.Context, req serverapi.WorkflowTaskActivityListRequest) (serverapi.WorkflowTaskActivityListResponse, error)
	GetWorkflowTaskTeleportTarget(ctx context.Context, req serverapi.WorkflowTaskTeleportTargetRequest) (serverapi.WorkflowTaskTeleportTargetResponse, error)
	SubscribeWorkflowProject(ctx context.Context, req serverapi.WorkflowProjectSubscribeRequest) (serverapi.WorkflowProjectSubscription, error)
	GetWorkflowBoard(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error)
	ListWorkflowBoardNodeCards(ctx context.Context, req serverapi.WorkflowBoardNodeCardsListRequest) (serverapi.WorkflowBoardNodeCardsListResponse, error)
	GetWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error)
}

type loopbackWorkflowClient struct {
	service servicecontract.WorkflowService
}

func NewLoopbackWorkflowClient(service servicecontract.WorkflowService) WorkflowClient {
	return &loopbackWorkflowClient{service: service}
}

func (c *loopbackWorkflowClient) CreateWorkflow(ctx context.Context, req serverapi.WorkflowCreateRequest) (serverapi.WorkflowCreateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowCreateResponse{}, errors.New("workflow service is required")
	}
	return c.service.CreateWorkflow(ctx, req)
}

func (c *loopbackWorkflowClient) CreateAndLinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowCreateAndLinkProjectRequest) (serverapi.WorkflowCreateAndLinkProjectResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowCreateAndLinkProjectResponse{}, errors.New("workflow service is required")
	}
	return c.service.CreateAndLinkWorkflowToProject(ctx, req)
}

func (c *loopbackWorkflowClient) UpdateWorkflow(ctx context.Context, req serverapi.WorkflowUpdateRequest) (serverapi.WorkflowGetResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowGetResponse{}, errors.New("workflow service is required")
	}
	return c.service.UpdateWorkflow(ctx, req)
}

func (c *loopbackWorkflowClient) ListWorkflows(ctx context.Context, req serverapi.WorkflowListRequest) (serverapi.WorkflowListResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowListResponse{}, errors.New("workflow service is required")
	}
	return c.service.ListWorkflows(ctx, req)
}

func (c *loopbackWorkflowClient) GetWorkflow(ctx context.Context, req serverapi.WorkflowGetRequest) (serverapi.WorkflowGetResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowGetResponse{}, errors.New("workflow service is required")
	}
	return c.service.GetWorkflow(ctx, req)
}

func (c *loopbackWorkflowClient) AddWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupAddRequest) (serverapi.WorkflowNodeGroupResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowNodeGroupResponse{}, errors.New("workflow service is required")
	}
	return c.service.AddWorkflowNodeGroup(ctx, req)
}

func (c *loopbackWorkflowClient) UpdateWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupUpdateRequest) (serverapi.WorkflowNodeGroupResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowNodeGroupResponse{}, errors.New("workflow service is required")
	}
	return c.service.UpdateWorkflowNodeGroup(ctx, req)
}

func (c *loopbackWorkflowClient) DeleteWorkflowNodeGroup(ctx context.Context, req serverapi.WorkflowNodeGroupDeleteRequest) error {
	if c == nil || c.service == nil {
		return errors.New("workflow service is required")
	}
	return c.service.DeleteWorkflowNodeGroup(ctx, req)
}

func (c *loopbackWorkflowClient) AddWorkflowNode(ctx context.Context, req serverapi.WorkflowNodeAddRequest) (serverapi.WorkflowNodeAddResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowNodeAddResponse{}, errors.New("workflow service is required")
	}
	return c.service.AddWorkflowNode(ctx, req)
}

func (c *loopbackWorkflowClient) UpdateWorkflowNode(ctx context.Context, req serverapi.WorkflowNodeUpdateRequest) (serverapi.WorkflowNodeUpdateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowNodeUpdateResponse{}, errors.New("workflow service is required")
	}
	return c.service.UpdateWorkflowNode(ctx, req)
}

func (c *loopbackWorkflowClient) AddWorkflowTransitionGroup(ctx context.Context, req serverapi.WorkflowTransitionGroupAddRequest) (serverapi.WorkflowTransitionGroupAddResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTransitionGroupAddResponse{}, errors.New("workflow service is required")
	}
	return c.service.AddWorkflowTransitionGroup(ctx, req)
}

func (c *loopbackWorkflowClient) UpdateWorkflowTransitionGroup(ctx context.Context, req serverapi.WorkflowTransitionGroupUpdateRequest) (serverapi.WorkflowTransitionGroupUpdateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTransitionGroupUpdateResponse{}, errors.New("workflow service is required")
	}
	return c.service.UpdateWorkflowTransitionGroup(ctx, req)
}

func (c *loopbackWorkflowClient) AddWorkflowEdge(ctx context.Context, req serverapi.WorkflowEdgeAddRequest) (serverapi.WorkflowEdgeAddResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowEdgeAddResponse{}, errors.New("workflow service is required")
	}
	return c.service.AddWorkflowEdge(ctx, req)
}

func (c *loopbackWorkflowClient) UpdateWorkflowEdge(ctx context.Context, req serverapi.WorkflowEdgeUpdateRequest) (serverapi.WorkflowEdgeUpdateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowEdgeUpdateResponse{}, errors.New("workflow service is required")
	}
	return c.service.UpdateWorkflowEdge(ctx, req)
}

func (c *loopbackWorkflowClient) LinkWorkflowToProject(ctx context.Context, req serverapi.WorkflowLinkProjectRequest) (serverapi.WorkflowLinkProjectResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowLinkProjectResponse{}, errors.New("workflow service is required")
	}
	return c.service.LinkWorkflowToProject(ctx, req)
}

func (c *loopbackWorkflowClient) ListProjectWorkflowLinks(ctx context.Context, req serverapi.WorkflowListProjectLinksRequest) (serverapi.WorkflowListProjectLinksResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowListProjectLinksResponse{}, errors.New("workflow service is required")
	}
	return c.service.ListProjectWorkflowLinks(ctx, req)
}

func (c *loopbackWorkflowClient) SetDefaultProjectWorkflowLink(ctx context.Context, req serverapi.WorkflowSetDefaultProjectLinkRequest) (serverapi.WorkflowSetDefaultProjectLinkResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowSetDefaultProjectLinkResponse{}, errors.New("workflow service is required")
	}
	return c.service.SetDefaultProjectWorkflowLink(ctx, req)
}

func (c *loopbackWorkflowClient) UnlinkWorkflowFromProject(ctx context.Context, req serverapi.WorkflowUnlinkProjectRequest) (serverapi.WorkflowUnlinkProjectResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowUnlinkProjectResponse{}, errors.New("workflow service is required")
	}
	return c.service.UnlinkWorkflowFromProject(ctx, req)
}

func (c *loopbackWorkflowClient) PreviewWorkflowDelete(ctx context.Context, req serverapi.WorkflowDeletePreviewRequest) (serverapi.WorkflowDeletePreviewResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowDeletePreviewResponse{}, errors.New("workflow service is required")
	}
	return c.service.PreviewWorkflowDelete(ctx, req)
}

func (c *loopbackWorkflowClient) DeleteWorkflow(ctx context.Context, req serverapi.WorkflowDeleteRequest) (serverapi.WorkflowDeleteResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowDeleteResponse{}, errors.New("workflow service is required")
	}
	return c.service.DeleteWorkflow(ctx, req)
}

func (c *loopbackWorkflowClient) ValidateWorkflow(ctx context.Context, req serverapi.WorkflowValidateRequest) (serverapi.WorkflowValidateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowValidateResponse{}, errors.New("workflow service is required")
	}
	return c.service.ValidateWorkflow(ctx, req)
}

func (c *loopbackWorkflowClient) CreateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCreateRequest) (serverapi.WorkflowTaskCreateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskCreateResponse{}, errors.New("workflow service is required")
	}
	return c.service.CreateWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) UpdateWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskUpdateRequest) (serverapi.WorkflowTaskUpdateResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskUpdateResponse{}, errors.New("workflow service is required")
	}
	return c.service.UpdateWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) StartWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskStartRequest) (serverapi.WorkflowTaskStartResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskStartResponse{}, errors.New("workflow service is required")
	}
	return c.service.StartWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) InterruptWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskInterruptRequest) (serverapi.WorkflowTaskInterruptResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskInterruptResponse{}, errors.New("workflow service is required")
	}
	return c.service.InterruptWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) ResumeWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskResumeRequest) (serverapi.WorkflowTaskResumeResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskResumeResponse{}, errors.New("workflow service is required")
	}
	return c.service.ResumeWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) ApproveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskApproveRequest) (serverapi.WorkflowTaskApproveResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskApproveResponse{}, errors.New("workflow service is required")
	}
	return c.service.ApproveWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) MoveWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskMoveRequest) (serverapi.WorkflowTaskMoveResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskMoveResponse{}, errors.New("workflow service is required")
	}
	return c.service.MoveWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) CancelWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskCancelRequest) error {
	if c == nil || c.service == nil {
		return errors.New("workflow service is required")
	}
	return c.service.CancelWorkflowTask(ctx, req)
}

func (c *loopbackWorkflowClient) ListWorkflowAttention(ctx context.Context, req serverapi.WorkflowAttentionListRequest) (serverapi.WorkflowAttentionListResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowAttentionListResponse{}, errors.New("workflow service is required")
	}
	return c.service.ListWorkflowAttention(ctx, req)
}

func (c *loopbackWorkflowClient) ListWorkflowTaskAttention(ctx context.Context, req serverapi.WorkflowTaskAttentionListRequest) (serverapi.WorkflowTaskAttentionListResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskAttentionListResponse{}, errors.New("workflow service is required")
	}
	return c.service.ListWorkflowTaskAttention(ctx, req)
}

func (c *loopbackWorkflowClient) AnswerWorkflowTaskQuestion(ctx context.Context, req serverapi.WorkflowTaskQuestionAnswerRequest) error {
	if c == nil || c.service == nil {
		return errors.New("workflow service is required")
	}
	return c.service.AnswerWorkflowTaskQuestion(ctx, req)
}

func (c *loopbackWorkflowClient) AddWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentAddRequest) (serverapi.WorkflowTaskCommentAddResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskCommentAddResponse{}, errors.New("workflow service is required")
	}
	return c.service.AddWorkflowTaskComment(ctx, req)
}

func (c *loopbackWorkflowClient) ListWorkflowTaskComments(ctx context.Context, req serverapi.WorkflowTaskCommentListRequest) (serverapi.WorkflowTaskCommentListResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskCommentListResponse{}, errors.New("workflow service is required")
	}
	return c.service.ListWorkflowTaskComments(ctx, req)
}

func (c *loopbackWorkflowClient) ReplaceWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentReplaceRequest) error {
	if c == nil || c.service == nil {
		return errors.New("workflow service is required")
	}
	return c.service.ReplaceWorkflowTaskComment(ctx, req)
}

func (c *loopbackWorkflowClient) DeleteWorkflowTaskComment(ctx context.Context, req serverapi.WorkflowTaskCommentDeleteRequest) error {
	if c == nil || c.service == nil {
		return errors.New("workflow service is required")
	}
	return c.service.DeleteWorkflowTaskComment(ctx, req)
}

func (c *loopbackWorkflowClient) ListWorkflowTaskActivity(ctx context.Context, req serverapi.WorkflowTaskActivityListRequest) (serverapi.WorkflowTaskActivityListResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskActivityListResponse{}, errors.New("workflow service is required")
	}
	return c.service.ListWorkflowTaskActivity(ctx, req)
}

func (c *loopbackWorkflowClient) GetWorkflowTaskTeleportTarget(ctx context.Context, req serverapi.WorkflowTaskTeleportTargetRequest) (serverapi.WorkflowTaskTeleportTargetResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskTeleportTargetResponse{}, errors.New("workflow service is required")
	}
	return c.service.GetWorkflowTaskTeleportTarget(ctx, req)
}

func (c *loopbackWorkflowClient) SubscribeWorkflowProject(ctx context.Context, req serverapi.WorkflowProjectSubscribeRequest) (serverapi.WorkflowProjectSubscription, error) {
	if c == nil || c.service == nil {
		return nil, errors.New("workflow service is required")
	}
	return c.service.SubscribeWorkflowProject(ctx, req)
}

func (c *loopbackWorkflowClient) GetWorkflowBoard(ctx context.Context, req serverapi.WorkflowBoardRequest) (serverapi.WorkflowBoardResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowBoardResponse{}, errors.New("workflow service is required")
	}
	return c.service.GetWorkflowBoard(ctx, req)
}

func (c *loopbackWorkflowClient) ListWorkflowBoardNodeCards(ctx context.Context, req serverapi.WorkflowBoardNodeCardsListRequest) (serverapi.WorkflowBoardNodeCardsListResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowBoardNodeCardsListResponse{}, errors.New("workflow service is required")
	}
	return c.service.ListWorkflowBoardNodeCards(ctx, req)
}

func (c *loopbackWorkflowClient) GetWorkflowTask(ctx context.Context, req serverapi.WorkflowTaskGetRequest) (serverapi.WorkflowTaskGetResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.WorkflowTaskGetResponse{}, errors.New("workflow service is required")
	}
	return c.service.GetWorkflowTask(ctx, req)
}
