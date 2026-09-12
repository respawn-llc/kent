package transport

import (
	"context"
	"encoding/json"
	"fmt"

	rpccontract "core/shared/apicontract"
	"core/shared/clientui"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/protocol"
	"core/shared/rpcwire"
	"core/shared/serverapi"
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
	if err := newRoutePolicyExecutor(g).requireAuthenticationStage(
		ctx,
		state,
		operation.Options.AuthenticationStage,
	); err != nil {
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

func (g *Gateway) serveAttentionNotificationSubscription(conn rpcwire.Conn, ctx context.Context, _ *connectionState, route rpccontract.Route, req protocol.Request) {
	serveGatewaySubscription(conn, ctx, route, req, g.deps.AttentionNotificationClient().SubscribeAttentionNotifications, func(evt clientui.AttentionNotificationEvent) protocol.AttentionNotificationEventParams {
		return protocol.AttentionNotificationEventParams{Event: evt}
	})
}

func (g *Gateway) serveWorkflowProjectSubscription(conn rpcwire.Conn, ctx context.Context, _ *connectionState, route rpccontract.Route, req protocol.Request) {
	serveGatewaySubscription(conn, ctx, route, req, g.deps.WorkflowClient().SubscribeWorkflowProject, workflowProjectEventParams)
}

func (g *Gateway) serveWorkflowSubscription(conn rpcwire.Conn, ctx context.Context, _ *connectionState, route rpccontract.Route, req protocol.Request) {
	serveGatewaySubscription(conn, ctx, route, req, g.deps.WorkflowClient().SubscribeWorkflow, workflowProjectEventParams)
}

func workflowProjectEventParams(evt serverapi.WorkflowProjectEvent) protocol.WorkflowProjectEventParams {
	return protocol.WorkflowProjectEventParams{Event: protocol.WorkflowProjectEvent{
		ProjectID:        evt.ProjectID,
		WorkflowID:       evt.WorkflowID,
		Resource:         protocol.WorkflowProjectEventResource(evt.Resource),
		Action:           protocol.WorkflowProjectEventAction(evt.Action),
		PrimaryEntityID:  evt.PrimaryEntityID,
		RelatedIDs:       append([]string(nil), evt.RelatedIDs...),
		OccurredAtUnixMs: evt.OccurredAtUnixMs,
	}}
}
