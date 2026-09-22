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
	protocol.MethodWorkflowTaskDependencyAdd:    gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskDependencyAddRequest, serverapi.WorkflowTaskDependencyAddResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.AddWorkflowTaskDependency),
	protocol.MethodWorkflowTaskDependencyRemove: gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskDependencyRemoveRequest, serverapi.WorkflowTaskDependencyRemoveResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.RemoveWorkflowTaskDependency),
	protocol.MethodWorkflowTaskDependencyList:   gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskDependencyListRequest, serverapi.WorkflowTaskDependencyListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskDependencies),
	protocol.MethodWorkflowAttentionList:        gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowAttentionListRequest, serverapi.WorkflowAttentionListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowAttention),
	protocol.MethodWorkflowTaskAttentionList:    gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskAttentionListRequest, serverapi.WorkflowTaskAttentionListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskAttention),
	protocol.MethodWorkflowTaskCommentAdd:       gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskCommentAddRequest, serverapi.WorkflowTaskCommentAddResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.AddWorkflowTaskComment),
	protocol.MethodWorkflowTaskCommentList:      gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskCommentListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskComments),
	protocol.MethodWorkflowTaskCommentReplace:   gatewayClientCallNoResponse[apicontract.WorkflowService, serverapi.WorkflowTaskCommentReplaceRequest](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ReplaceWorkflowTaskComment),
	protocol.MethodWorkflowTaskCommentDelete:    gatewayClientCallNoResponse[apicontract.WorkflowService, serverapi.WorkflowTaskCommentDeleteRequest](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.DeleteWorkflowTaskComment),
	protocol.MethodWorkflowTaskActivityList:     gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskActivityListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskActivity),
	protocol.MethodWorkflowTaskSessionList:      gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskOffsetPageRequest, serverapi.WorkflowTaskSessionListResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ListWorkflowTaskSessions),
	protocol.MethodWorkflowTaskObserve:          gatewayClientCall[apicontract.WorkflowService, serverapi.WorkflowTaskObservationRequest, serverapi.WorkflowTaskObservationResponse](GatewayDependencies.WorkflowClient, apicontract.WorkflowService.ObserveWorkflowTask),
}
