package transport

import (
	"context"
	"fmt"
	"sync/atomic"

	"builder/shared/protocol"
	"builder/shared/rpccontract"
	"builder/shared/rpcwire"
	"builder/shared/serverapi"
)

func (g *Gateway) serveRunPrompt(conn rpcwire.Conn, ctx context.Context, state *connectionState, route rpccontract.Route, req protocol.Request) bool {
	if err := req.Validate(); err != nil {
		return sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, err.Error()))
	}
	if !state.handshakeDone {
		return sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidRequest, "handshake is required before other methods"))
	}
	if err := newRoutePolicyExecutor(g).requireAuth(ctx, req.Method); err != nil {
		return sendResponse(ctx, conn, responseForError(req.ID, err))
	}
	decoded, preflightResp, failed := g.preflightRouteRequest(ctx, state, route, req)
	if failed {
		return sendResponse(ctx, conn, preflightResp)
	}
	params, ok := decoded.(serverapi.RunPromptRequest)
	if !ok {
		return sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInternalError, "run prompt route contract mismatch"))
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var progressBroken atomic.Bool
	progress := serverapi.RunPromptProgressFunc(func(update serverapi.RunPromptProgress) {
		if progressBroken.Load() {
			return
		}
		if err := sendNotification(runCtx, conn, route.EventMethod, update); err != nil {
			if progressBroken.CompareAndSwap(false, true) {
				cancel()
			}
		}
	})
	runClient, err := g.runPromptClientForState(runCtx, state)
	if err != nil {
		return sendResponse(ctx, conn, responseForError(req.ID, err))
	}
	resp, err := runClient.RunPrompt(runCtx, params, progress)
	if err != nil {
		return sendResponse(ctx, conn, responseForError(req.ID, err))
	}
	return sendResponse(ctx, conn, protocol.NewSuccessResponse(req.ID, resp))
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
	if err := newRoutePolicyExecutor(g).requireAuth(ctx, req.Method); err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	handler, ok := gatewaySubscriptionHandlers[req.Method]
	if !ok {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method)))
		return
	}
	route, ok := rpccontract.RouteByMethod(req.Method)
	if !ok {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method)))
		return
	}
	if _, resp, failed := g.preflightRouteRequest(ctx, state, route, req); failed {
		_ = sendResponse(ctx, conn, resp)
		return
	}
	handler(g, conn, ctx, state, route, req)
}

func (g *Gateway) serveSessionActivitySubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, route rpccontract.Route, req protocol.Request) {
	params, err := decodeParams[serverapi.SessionActivitySubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	sub, err := g.deps.SessionActivityClient().SubscribeSessionActivity(ctx, params)
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
			_ = sendNotification(ctx, conn, route.CompleteMethod, streamCompleteParams(err))
			return
		}
		if err := sendNotification(ctx, conn, route.EventMethod, protocol.SessionActivityEventParams{Event: evt}); err != nil {
			return
		}
	}
}

func (g *Gateway) serveProcessOutputSubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, route rpccontract.Route, req protocol.Request) {
	params, err := decodeParams[serverapi.ProcessOutputSubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	sub, err := g.deps.ProcessOutputClient().SubscribeProcessOutput(ctx, params)
	if err != nil {
		_ = sendResponse(ctx, conn, responseForError(req.ID, err))
		return
	}
	defer func() { _ = sub.Close() }()
	if !sendResponse(ctx, conn, protocol.NewSuccessResponse(req.ID, protocol.SubscribeResponse{Stream: route.EventMethod})) {
		return
	}
	for {
		chunk, err := sub.Next(ctx)
		if err != nil {
			_ = sendNotification(ctx, conn, route.CompleteMethod, streamCompleteParams(err))
			return
		}
		if err := sendNotification(ctx, conn, route.EventMethod, protocol.ProcessOutputEventParams{Chunk: chunk}); err != nil {
			return
		}
	}
}

func (g *Gateway) servePromptActivitySubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, route rpccontract.Route, req protocol.Request) {
	params, err := decodeParams[serverapi.PromptActivitySubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	sub, err := g.deps.PromptActivityClient().SubscribePromptActivity(ctx, params)
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
			_ = sendNotification(ctx, conn, route.CompleteMethod, streamCompleteParams(err))
			return
		}
		if err := sendNotification(ctx, conn, route.EventMethod, protocol.PromptActivityEventParams{Event: evt}); err != nil {
			return
		}
	}
}

func (g *Gateway) serveWorkflowProjectSubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, route rpccontract.Route, req protocol.Request) {
	params, err := decodeParams[serverapi.WorkflowProjectSubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	sub, err := g.deps.WorkflowClient().SubscribeWorkflowProject(ctx, params)
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
			_ = sendNotification(ctx, conn, route.CompleteMethod, streamCompleteParams(err))
			return
		}
		params := protocol.WorkflowProjectEventParams{Event: protocol.WorkflowProjectEvent{ProjectID: evt.ProjectID, WorkflowID: evt.WorkflowID, Resource: evt.Resource, Action: evt.Action, ChangedIDs: evt.ChangedIDs, OccurredAtUnixMs: evt.OccurredAtUnixMs}}
		if err := sendNotification(ctx, conn, route.EventMethod, params); err != nil {
			return
		}
	}
}

func (g *Gateway) serveWorkflowSubscription(conn rpcwire.Conn, ctx context.Context, state *connectionState, route rpccontract.Route, req protocol.Request) {
	params, err := decodeParams[serverapi.WorkflowSubscribeRequest](req.Params)
	if err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	if err := params.Validate(); err != nil {
		_ = sendResponse(ctx, conn, protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams, err.Error()))
		return
	}
	sub, err := g.deps.WorkflowClient().SubscribeWorkflow(ctx, params)
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
			_ = sendNotification(ctx, conn, route.CompleteMethod, streamCompleteParams(err))
			return
		}
		params := protocol.WorkflowProjectEventParams{Event: protocol.WorkflowProjectEvent{ProjectID: evt.ProjectID, WorkflowID: evt.WorkflowID, Resource: evt.Resource, Action: evt.Action, ChangedIDs: evt.ChangedIDs, OccurredAtUnixMs: evt.OccurredAtUnixMs}}
		if err := sendNotification(ctx, conn, route.EventMethod, params); err != nil {
			return
		}
	}
}
