package transport

import (
	"context"

	"core/shared/apicontract"
	"core/shared/protocol"
	"core/shared/serverapi"
)

func gatewayClientCall[C any, Req any, Resp any](getClient func(GatewayDependencies) C, call func(C, context.Context, Req) (Resp, error)) gatewayUnaryHandler {
	return func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request, prepared any) protocol.Response {
		return handlePrepared(req.ID, prepared, func(params Req) (Resp, error) {
			return call(getClient(g.deps), ctx, params)
		})
	}
}

func gatewayClientCallNoResponse[C any, Req any](getClient func(GatewayDependencies) C, call func(C, context.Context, Req) error) gatewayUnaryHandler {
	return func(g *Gateway, ctx context.Context, state *connectionState, req protocol.Request, prepared any) protocol.Response {
		return handlePrepared(req.ID, prepared, func(params Req) (struct{}, error) {
			return struct{}{}, call(getClient(g.deps), ctx, params)
		})
	}
}

func runtimePendingWorkClient(deps GatewayDependencies) apicontract.RuntimePendingWorkService {
	client, ok := deps.RuntimeControlClient().(apicontract.RuntimePendingWorkService)
	if !ok {
		panic("Runtime Pending Work service is unavailable")
	}
	return client
}

var gatewayUnaryHandlerEntries = map[string]gatewayUnaryHandler{
	protocol.MethodWorkflowCreate:                 gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowCreateRequest, serverapi.WorkflowCreateResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.CreateWorkflow),
	protocol.MethodWorkflowCreateAndLinkProject:   gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowCreateAndLinkProjectRequest, serverapi.WorkflowCreateAndLinkProjectResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.CreateAndLinkWorkflowToProject),
	protocol.MethodWorkflowUpdate:                 gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowUpdateRequest, serverapi.WorkflowGetResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.UpdateWorkflow),
	protocol.MethodWorkflowList:                   gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowListRequest, serverapi.WorkflowListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflows),
	protocol.MethodWorkflowGet:                    gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowGetRequest, serverapi.WorkflowGetResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.GetWorkflow),
	protocol.MethodWorkflowLinkProject:            gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowLinkProjectRequest, serverapi.WorkflowLinkProjectResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.LinkWorkflowToProject),
	protocol.MethodWorkflowListProjectLinks:       gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowListProjectLinksRequest, serverapi.WorkflowListProjectLinksResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListProjectWorkflowLinks),
	protocol.MethodWorkflowSetDefaultProjectLink:  gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowSetDefaultProjectLinkRequest, serverapi.WorkflowSetDefaultProjectLinkResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.SetDefaultProjectWorkflowLink),
	protocol.MethodWorkflowUnlinkProject:          gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowUnlinkProjectRequest, serverapi.WorkflowUnlinkProjectResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.UnlinkWorkflowFromProject),
	protocol.MethodWorkflowDeletePreview:          gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowDeletePreviewRequest, serverapi.WorkflowDeletePreviewResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.PreviewWorkflowDelete),
	protocol.MethodWorkflowDelete:                 gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowDeleteRequest, serverapi.WorkflowDeleteResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.DeleteWorkflow),
	protocol.MethodWorkflowValidate:               gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowValidateRequest, serverapi.WorkflowValidateResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ValidateWorkflow),
	protocol.MethodWorkflowScriptPathValidate:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowScriptPathValidateRequest, serverapi.WorkflowValidateResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ValidateWorkflowScriptPath),
	protocol.MethodWorkflowGraphValidateDraft:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowGraphValidateDraftRequest, serverapi.WorkflowGraphValidateDraftResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ValidateWorkflowGraphDraft),
	protocol.MethodWorkflowGraphDeriveWiring:      gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowGraphDeriveWiringRequest, serverapi.WorkflowGraphDeriveWiringResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.DeriveWorkflowGraphWiring),
	protocol.MethodWorkflowGraphSavePreview:       gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowGraphSavePreviewRequest, serverapi.WorkflowGraphSavePreviewResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.PreviewWorkflowGraphSave),
	protocol.MethodWorkflowGraphSave:              gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowGraphSaveRequest, serverapi.WorkflowGraphSaveResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.SaveWorkflowGraph),
	protocol.MethodWorkflowProjectLabelCreate:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowProjectLabelCreateRequest, serverapi.WorkflowProjectLabelCreateResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.CreateWorkflowProjectLabel),
	protocol.MethodWorkflowProjectLabelList:       gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowProjectLabelCatalogRequest, serverapi.WorkflowProjectLabelCatalogResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowProjectLabels),
	protocol.MethodWorkflowProjectLabelRename:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowProjectLabelRenameRequest, serverapi.WorkflowProjectLabelRenameResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.RenameWorkflowProjectLabel),
	protocol.MethodWorkflowProjectLabelDelete:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowProjectLabelDeleteRequest, serverapi.WorkflowProjectLabelDeleteResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.DeleteWorkflowProjectLabel),
	protocol.MethodWorkflowProjectLabelReorder:    gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowProjectLabelReorderRequest, serverapi.WorkflowProjectLabelReorderResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ReorderWorkflowProjectLabels),
	protocol.MethodWorkflowTaskLabelsGet:          gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskLabelsGetRequest, serverapi.WorkflowTaskLabelsGetResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.GetWorkflowTaskLabels),
	protocol.MethodWorkflowTaskLabelsUpdate:       gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskLabelsUpdateRequest, serverapi.WorkflowTaskLabelsUpdateResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.UpdateWorkflowTaskLabels),
	protocol.MethodWorkflowTaskCreate:             gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskCreateRequest, serverapi.WorkflowTaskCreateResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.CreateWorkflowTask),
	protocol.MethodWorkflowTaskDependencyAdd:      gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskDependencyAddRequest, serverapi.WorkflowTaskDependencyAddResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.AddWorkflowTaskDependency),
	protocol.MethodWorkflowTaskDependencyRemove:   gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskDependencyRemoveRequest, serverapi.WorkflowTaskDependencyRemoveResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.RemoveWorkflowTaskDependency),
	protocol.MethodWorkflowTaskDependencyList:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskDependencyListRequest, serverapi.WorkflowTaskDependencyListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskDependencies),
	protocol.MethodWorkflowTaskUpdate:             gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskUpdateRequest, serverapi.WorkflowTaskUpdateResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.UpdateWorkflowTask),
	protocol.MethodWorkflowTaskStart:              gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskStartRequest, serverapi.WorkflowTaskStartResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.StartWorkflowTask),
	protocol.MethodWorkflowTaskInterrupt:          gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskInterruptRequest, serverapi.WorkflowTaskInterruptResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.InterruptWorkflowTask),
	protocol.MethodWorkflowTaskResume:             gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskResumeRequest, serverapi.WorkflowTaskResumeResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ResumeWorkflowTask),
	protocol.MethodWorkflowTaskApprove:            gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskApproveRequest, serverapi.WorkflowTaskApproveResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ApproveWorkflowTask),
	protocol.MethodWorkflowTaskMovePreview:        gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskMovePreviewRequest, serverapi.WorkflowTaskMovePreviewResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.PreviewWorkflowTaskMove),
	protocol.MethodWorkflowTaskMove:               gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskMoveRequest, serverapi.WorkflowTaskMoveResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.MoveWorkflowTask),
	protocol.MethodWorkflowTaskComplete:           gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskCompleteRequest, serverapi.WorkflowTaskCompleteResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.CompleteWorkflowTask),
	protocol.MethodWorkflowTaskDelete:             gatewayClientCallNoResponse[apicontract.WorkflowService, serverapi.WorkflowTaskDeleteRequest](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.DeleteWorkflowTask),
	protocol.MethodWorkflowAttentionList:          gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowAttentionListRequest, serverapi.WorkflowAttentionListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowAttention),
	protocol.MethodWorkflowTaskAttentionList:      gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskAttentionListRequest, serverapi.WorkflowTaskAttentionListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskAttention),
	protocol.MethodWorkflowTaskCommentAdd:         gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskCommentAddRequest, serverapi.WorkflowTaskCommentAddResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.AddWorkflowTaskComment),
	protocol.MethodWorkflowTaskCommentList:        gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskCommentListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskComments),
	protocol.MethodWorkflowTaskCommentReplace:     gatewayClientCallNoResponse[apicontract.WorkflowService, serverapi.WorkflowTaskCommentReplaceRequest](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ReplaceWorkflowTaskComment),
	protocol.MethodWorkflowTaskCommentDelete:      gatewayClientCallNoResponse[apicontract.WorkflowService, serverapi.WorkflowTaskCommentDeleteRequest](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.DeleteWorkflowTaskComment),
	protocol.MethodWorkflowTaskActivityList:       gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskActivityListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskActivity),
	protocol.MethodWorkflowTaskSessionList:        gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskSessionListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskSessions),
	protocol.MethodWorkflowTaskList:               gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskListRequest, serverapi.WorkflowTaskListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTasks),
	protocol.MethodWorkflowProjectTaskGroupCounts: gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowProjectTaskGroupCountsRequest, serverapi.WorkflowProjectTaskGroupCountsResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.GetWorkflowProjectTaskGroupCounts),
	protocol.MethodWorkflowTaskSearch:             gatewayClientCall[apicontract.WorkflowService, serverapi.TaskSearchRequest, serverapi.TaskSearchResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.SearchWorkflowTasks),
	protocol.MethodWorkflowBoardGet:               gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowBoardRequest, serverapi.WorkflowBoardResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.GetWorkflowBoard),
	protocol.MethodWorkflowBoardNodeCardsList:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowBoardNodeCardsListRequest, serverapi.WorkflowBoardNodeCardsListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowBoardNodeCards),
	protocol.MethodWorkflowTaskGet:                gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskGetRequest, serverapi.WorkflowTaskGetResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.GetWorkflowTask),
	protocol.MethodWorkflowTaskObserve:            gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskObservationRequest, serverapi.WorkflowTaskObservationResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ObserveWorkflowTask),
}
