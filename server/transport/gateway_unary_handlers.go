package transport

import (
	"context"

	"core/shared/apicontract"
	"core/shared/protocol"
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

var gatewayUnaryHandlerEntries = map[string]gatewayUnaryHandler{}
