package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"builder/shared/clientui"
	"builder/shared/protocol"
	"builder/shared/rpccontract"
	"builder/shared/rpcwire"
	"builder/shared/serverapi"
)

type remoteSessionActivitySubscription struct {
	conn  rpcwire.Conn
	route rpccontract.Route
	once  sync.Once
}

type remotePromptActivitySubscription struct {
	conn  rpcwire.Conn
	route rpccontract.Route
	once  sync.Once
}

type remoteProcessOutputSubscription struct {
	conn  rpcwire.Conn
	route rpccontract.Route
	once  sync.Once
}

type remoteWorkflowProjectSubscription struct {
	conn  rpcwire.Conn
	route rpccontract.Route
	once  sync.Once
}

func (c *Remote) SubscribePromptActivity(ctx context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
	route := mustRemoteRoute(protocol.MethodPromptSubscribeActivity)
	conn, cleanup, err := c.openSessionRPCConn(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	var ack protocol.SubscribeResponse
	if err := callRPC(ctx, conn, "subscribe-prompt-activity", protocol.MethodPromptSubscribeActivity, req, &ack); err != nil {
		cleanup()
		return nil, err
	}
	return &remotePromptActivitySubscription{conn: conn, route: route}, nil
}

func (c *Remote) RunPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error) {
	route := mustRemoteRoute(protocol.MethodRunPrompt)
	conn, cleanup, err := c.openRPCConn(ctx)
	if err != nil {
		return serverapi.RunPromptResponse{}, err
	}
	defer cleanup()
	params, err := json.Marshal(req)
	if err != nil {
		return serverapi.RunPromptResponse{}, err
	}
	const requestID = "run-prompt"
	request := protocol.Request{JSONRPC: protocol.JSONRPCVersion, ID: requestID, Method: protocol.MethodRunPrompt, Params: params}
	if err := conn.Send(ctx, rpcwire.FrameFromRequest(request)); err != nil {
		return serverapi.RunPromptResponse{}, err
	}
	for {
		frame, err := receiveFrame(ctx, conn)
		if err != nil {
			return serverapi.RunPromptResponse{}, err
		}
		if frame.Method == route.EventMethod {
			if progress != nil {
				var update serverapi.RunPromptProgress
				if err := json.Unmarshal(frame.Params, &update); err != nil {
					return serverapi.RunPromptResponse{}, err
				}
				progress.PublishRunPromptProgress(update)
			}
			continue
		}
		if frame.ID != requestID {
			return serverapi.RunPromptResponse{}, fmt.Errorf("unexpected rpc frame id %q", frame.ID)
		}
		resp := frame.Response()
		if resp.Error != nil {
			return serverapi.RunPromptResponse{}, protocolError(resp.Error)
		}
		var result serverapi.RunPromptResponse
		if len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				return serverapi.RunPromptResponse{}, err
			}
		}
		return result, nil
	}
}

func (c *Remote) SubscribeSessionActivity(ctx context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
	route := mustRemoteRoute(protocol.MethodSessionSubscribeActivity)
	conn, cleanup, err := c.openSessionRPCConn(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	var ack protocol.SubscribeResponse
	if err := callRPC(ctx, conn, "subscribe-session-activity", protocol.MethodSessionSubscribeActivity, req, &ack); err != nil {
		cleanup()
		return nil, err
	}
	return &remoteSessionActivitySubscription{conn: conn, route: route}, nil
}

func (c *Remote) SubscribeProcessOutput(ctx context.Context, req serverapi.ProcessOutputSubscribeRequest) (serverapi.ProcessOutputSubscription, error) {
	route := mustRemoteRoute(protocol.MethodProcessSubscribeOutput)
	conn, cleanup, err := c.openRPCConn(ctx)
	if err != nil {
		return nil, err
	}
	var ack protocol.SubscribeResponse
	if err := callRPC(ctx, conn, "subscribe-process-output", protocol.MethodProcessSubscribeOutput, req, &ack); err != nil {
		cleanup()
		return nil, err
	}
	return &remoteProcessOutputSubscription{conn: conn, route: route}, nil
}

func (c *Remote) SubscribeWorkflowProject(ctx context.Context, req serverapi.WorkflowProjectSubscribeRequest) (serverapi.WorkflowProjectSubscription, error) {
	route := mustRemoteRoute(protocol.MethodWorkflowSubscribeProject)
	conn, cleanup, err := c.openRPCConn(ctx)
	if err != nil {
		return nil, err
	}
	var ack protocol.SubscribeResponse
	if err := callRPC(ctx, conn, "subscribe-workflow-project", protocol.MethodWorkflowSubscribeProject, req, &ack); err != nil {
		cleanup()
		return nil, err
	}
	return &remoteWorkflowProjectSubscription{conn: conn, route: route}, nil
}

func (c *Remote) SubscribeWorkflow(ctx context.Context, req serverapi.WorkflowSubscribeRequest) (serverapi.WorkflowSubscription, error) {
	route := mustRemoteRoute(protocol.MethodWorkflowSubscribe)
	conn, cleanup, err := c.openRPCConn(ctx)
	if err != nil {
		return nil, err
	}
	var ack protocol.SubscribeResponse
	if err := callRPC(ctx, conn, "subscribe-workflow", protocol.MethodWorkflowSubscribe, req, &ack); err != nil {
		cleanup()
		return nil, err
	}
	return &remoteWorkflowProjectSubscription{conn: conn, route: route}, nil
}

func (c *Remote) openSessionRPCConn(ctx context.Context, sessionID string) (rpcwire.Conn, func(), error) {
	conn, cleanup, err := c.openRPCConn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := callRPC(ctx, conn, "attach-session", protocol.MethodAttachSession, protocol.AttachSessionRequest{SessionID: sessionID}, nil); err != nil {
		cleanup()
		return nil, nil, err
	}
	return conn, cleanup, nil
}

func mustRemoteRoute(method string) rpccontract.Route {
	route, ok := rpccontract.RouteByMethod(method)
	if !ok {
		panic(fmt.Sprintf("remote route %q is missing route contract", method))
	}
	return route
}

func (s *remoteSessionActivitySubscription) Next(ctx context.Context) (clientui.Event, error) {
	frame, err := receiveFrame(ctx, s.conn)
	if err != nil {
		return clientui.Event{}, serverapi.NormalizeStreamError(err)
	}
	switch frame.Method {
	case s.route.EventMethod:
		var params protocol.SessionActivityEventParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return clientui.Event{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		return params.Event, nil
	case s.route.CompleteMethod:
		var params protocol.StreamCompleteParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return clientui.Event{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		_ = s.Close()
		return clientui.Event{}, streamCompleteError(params)
	default:
		return clientui.Event{}, errors.Join(serverapi.ErrStreamFailed, fmt.Errorf("unexpected notification method %q", frame.Method))
	}
}

func (s *remoteSessionActivitySubscription) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
	return nil
}

func (s *remotePromptActivitySubscription) Next(ctx context.Context) (clientui.PendingPromptEvent, error) {
	frame, err := receiveFrame(ctx, s.conn)
	if err != nil {
		return clientui.PendingPromptEvent{}, serverapi.NormalizeStreamError(err)
	}
	switch frame.Method {
	case s.route.EventMethod:
		var params protocol.PromptActivityEventParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return clientui.PendingPromptEvent{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		return params.Event, nil
	case s.route.CompleteMethod:
		var params protocol.StreamCompleteParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return clientui.PendingPromptEvent{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		_ = s.Close()
		return clientui.PendingPromptEvent{}, streamCompleteError(params)
	default:
		return clientui.PendingPromptEvent{}, errors.Join(serverapi.ErrStreamFailed, fmt.Errorf("unexpected notification method %q", frame.Method))
	}
}

func (s *remotePromptActivitySubscription) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
	return nil
}

func (s *remoteProcessOutputSubscription) Next(ctx context.Context) (clientui.ProcessOutputChunk, error) {
	frame, err := receiveFrame(ctx, s.conn)
	if err != nil {
		return clientui.ProcessOutputChunk{}, serverapi.NormalizeStreamError(err)
	}
	switch frame.Method {
	case s.route.EventMethod:
		var params protocol.ProcessOutputEventParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return clientui.ProcessOutputChunk{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		return params.Chunk, nil
	case s.route.CompleteMethod:
		var params protocol.StreamCompleteParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return clientui.ProcessOutputChunk{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		_ = s.Close()
		return clientui.ProcessOutputChunk{}, streamCompleteError(params)
	default:
		return clientui.ProcessOutputChunk{}, errors.Join(serverapi.ErrStreamFailed, fmt.Errorf("unexpected notification method %q", frame.Method))
	}
}

func (s *remoteProcessOutputSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
	return nil
}

func (s *remoteWorkflowProjectSubscription) Next(ctx context.Context) (serverapi.WorkflowProjectEvent, error) {
	frame, err := receiveFrame(ctx, s.conn)
	if err != nil {
		return serverapi.WorkflowProjectEvent{}, serverapi.NormalizeStreamError(err)
	}
	switch frame.Method {
	case s.route.EventMethod:
		var params protocol.WorkflowProjectEventParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return serverapi.WorkflowProjectEvent{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		return serverapi.WorkflowProjectEvent{ProjectID: params.Event.ProjectID, WorkflowID: params.Event.WorkflowID, Resource: params.Event.Resource, Action: params.Event.Action, ChangedIDs: params.Event.ChangedIDs, OccurredAtUnixMs: params.Event.OccurredAtUnixMs}, nil
	case s.route.CompleteMethod:
		var params protocol.StreamCompleteParams
		if err := json.Unmarshal(frame.Params, &params); err != nil {
			return serverapi.WorkflowProjectEvent{}, errors.Join(serverapi.ErrStreamFailed, err)
		}
		_ = s.Close()
		return serverapi.WorkflowProjectEvent{}, streamCompleteError(params)
	default:
		return serverapi.WorkflowProjectEvent{}, errors.Join(serverapi.ErrStreamFailed, fmt.Errorf("unexpected notification method %q", frame.Method))
	}
}

func (s *remoteWorkflowProjectSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
	return nil
}
