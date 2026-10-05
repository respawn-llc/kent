package transport

import (
	"context"
	"encoding/json"
	"fmt"

	rpccontract "core/shared/apicontract"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/protocol"
	"core/shared/rpcwire"
)

type gatewaySubscription[Event any] interface {
	Next(context.Context) (Event, error)
	Close() error
}

func (g *Gateway) serveSubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, req protocol.Request) {
	if err := req.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, err.Error()))
		return
	}
	if !state.handshakeDone {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, "handshake is required before other methods"))
		return
	}
	operation, route, ok := g.registration.LegacyOperation(req.Method)
	if !ok || operation.Options.Kind != sharedpb.OperationKind_OPERATION_KIND_SUBSCRIPTION {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method)))
		return
	}
	if err := g.requireCoreActive(); err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	route.Scope = routeScopePolicy(operation.Options.ScopePolicy)
	if _, resp, failed := g.preflightRouteRequest(ctx, state, route, req); failed {
		_ = sendResponse(ctx, conn, resp)
		return
	}
	gatewaySubscriptionHandlers[req.Method](g, conn, ctx, state, route, req)
}

func serveGatewaySubscription[Req interface{ Validate() error }, Event any, Wire any, Sub gatewaySubscription[Event]](
	conn rpcwire.Conn,
	ctx context.Context,
	route rpccontract.Route,
	req protocol.Request,
	subscribe func(context.Context, Req) (Sub, error),
	wire func(Event) Wire,
) {
	params, err := decodeParams[Req](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	sub, err := subscribe(ctx, params)
	if err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	defer func() { _ = sub.Close() }()
	if !sendResponse(ctx, conn, protocol.NewSuccessResponse(req.ID, protocol.SubscribeResponse{Stream: route.EventMethod})) {
		return
	}
	for {
		evt, err := sub.Next(ctx)
		if err != nil {
			if data, marshalErr := json.Marshal(streamCompleteParams(err)); marshalErr == nil {
				_ = conn.Send(ctx, rpcwire.FrameFromRequest(protocol.Request{JSONRPC: protocol.JSONRPCVersion, Method: route.CompleteMethod, Params: data}))
			}
			return
		}
		data, err := json.Marshal(wire(evt))
		if err == nil {
			err = conn.Send(ctx, rpcwire.FrameFromRequest(protocol.Request{JSONRPC: protocol.JSONRPCVersion, Method: route.EventMethod, Params: data}))
		}
		if err != nil {
			return
		}
	}
}
