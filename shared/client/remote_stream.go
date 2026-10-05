package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	rpccontract "core/shared/apicontract"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/protocol"
	"core/shared/rpcwire"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/emptypb"
)

type remoteSubscription[Event any] struct {
	conn rpcwire.Conn
	next func(context.Context, rpcwire.Conn) (Event, error)
	once sync.Once
}

func (c *Remote) SubscribeAttentionNotifications(ctx context.Context, req *emptypb.Empty) (rpccontract.AttentionNotificationSubscription, error) {
	method := taskpb.File_kent_api_workflow_task_attention_proto.Services().ByName("AttentionNotificationService").Methods().ByName("Subscribe")
	return subscribeGeneratedBinary(c, ctx, method, req, &taskpb.AttentionNotificationStartResult{},
		func(failure *taskpb.AttentionNotificationStartError) error {
			return generatedOperationFailure(failure.Code)
		},
		func() *taskpb.AttentionNotificationEvent { return &taskpb.AttentionNotificationEvent{} },
		func() *sharedpb.StreamCompletion { return &sharedpb.StreamCompletion{} }, binaryStreamCompletionError, nil)
}

func (c *Remote) SubscribeWorkflowProject(ctx context.Context, req *pb.ProjectSubscribeRequest) (rpccontract.WorkflowEventSubscription, error) {
	return subscribeGeneratedBinary(c, ctx, workflowMethod("ProjectSubscriptionService", "Subscribe"), req,
		&pb.ProjectSubscriptionStartResult{}, func(failure *pb.ProjectSubscriptionStartError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		},
		func() *pb.ProjectEvent { return &pb.ProjectEvent{} },
		func() *sharedpb.StreamCompletion { return &sharedpb.StreamCompletion{} }, binaryStreamCompletionError, nil)
}

func (c *Remote) SubscribeWorkflow(ctx context.Context, req *pb.WorkflowSubscribeRequest) (rpccontract.WorkflowEventSubscription, error) {
	return subscribeGeneratedBinary(c, ctx, workflowMethod("WorkflowSubscriptionService", "Subscribe"), req,
		&pb.WorkflowSubscriptionStartResult{}, func(failure *pb.WorkflowSubscriptionStartError) error {
			return workflowEntityGeneratedError(failure.Code, failure.GetWorkflowNotFound())
		},
		func() *pb.ProjectEvent { return &pb.ProjectEvent{} },
		func() *sharedpb.StreamCompletion { return &sharedpb.StreamCompletion{} }, binaryStreamCompletionError, nil)
}

func (c *Remote) subscribeRPC(ctx context.Context, method string, requestID string, req any, sessionID string, attachSession bool) (rpcwire.Conn, rpccontract.Route, error) {
	route := mustRemoteRoute(method)
	var additionalAttachmentIntent *remoteAttachmentIntent
	if attachSession {
		var err error
		additionalAttachmentIntent, err = c.subscriptionAttachment(sessionID)
		if err != nil {
			return nil, rpccontract.Route{}, err
		}
	}
	conn, cleanup, err := c.openRPCConnWithAdditionalAttachment(ctx, additionalAttachmentIntent)
	if err != nil {
		return nil, rpccontract.Route{}, err
	}
	var ack protocol.SubscribeResponse
	if err := callRPC(ctx, conn, requestID, method, req, &ack); err != nil {
		cleanup()
		return nil, rpccontract.Route{}, err
	}
	return conn, route, nil
}

func (c *Remote) subscriptionAttachment(sessionID string) (*remoteAttachmentIntent, error) {
	c.mu.Lock()
	attachmentIntent := c.attachIntent
	c.mu.Unlock()
	attachedSessionID, attachedToSession := attachmentIntent.sessionID()
	if attachedToSession {
		if attachedSessionID != strings.TrimSpace(sessionID) {
			return nil, fmt.Errorf("remote is attached to session %q, cannot subscribe to session %q", attachedSessionID, strings.TrimSpace(sessionID))
		}
		return nil, nil
	}
	return newRemoteSessionAttachmentIntent(sessionID)
}

func newRemoteSubscription[Wire any, Event any](conn rpcwire.Conn, route rpccontract.Route, event func(Wire) Event) *remoteSubscription[Event] {
	return newRemoteSubscriptionWithError(conn, route, func(wire Wire) (Event, error) {
		return event(wire), nil
	})
}

func newRemoteSubscriptionWithError[Wire any, Event any](conn rpcwire.Conn, route rpccontract.Route, event func(Wire) (Event, error)) *remoteSubscription[Event] {
	return &remoteSubscription[Event]{
		conn: conn,
		next: func(ctx context.Context, conn rpcwire.Conn) (Event, error) {
			return nextJSONSubscriptionEvent(ctx, conn, route, event)
		},
	}
}

func mustRemoteRoute(method string) rpccontract.Route {
	route, ok := rpccontract.RouteByMethod(method)
	if !ok {
		panic(fmt.Sprintf("remote route %q is missing route contract", method))
	}
	return route
}

func nextJSONSubscriptionEvent[Wire any, Event any](
	ctx context.Context,
	conn rpcwire.Conn,
	route rpccontract.Route,
	event func(Wire) (Event, error),
) (Event, error) {
	frame, err := receiveFrame(ctx, conn)
	if err != nil {
		var zero Event
		return zero, serverapi.NormalizeStreamError(err)
	}
	message, err := frame.DecodeRequest()
	if err != nil {
		var zero Event
		return zero, errors.Join(serverapi.ErrStreamFailed, err)
	}
	switch message.Method {
	case route.EventMethod:
		var params Wire
		if err := json.Unmarshal(message.Params, &params); err != nil {
			var zero Event
			return zero, errors.Join(serverapi.ErrStreamFailed, err)
		}
		decoded, err := event(params)
		if err != nil {
			var zero Event
			return zero, errors.Join(serverapi.ErrStreamFailed, err)
		}
		return decoded, nil
	case route.CompleteMethod:
		var params protocol.StreamCompleteParams
		if err := json.Unmarshal(message.Params, &params); err != nil {
			var zero Event
			return zero, errors.Join(serverapi.ErrStreamFailed, err)
		}
		_ = conn.Close()
		var zero Event
		if params.Code == 0 && strings.TrimSpace(params.Message) == "" {
			return zero, io.EOF
		}
		terminalErr := protocolError(&protocol.ResponseError{Code: params.Code, Message: params.Message})
		if reason := strings.TrimSpace(params.TranscriptCloseReason); reason != "" {
			return zero, serverapi.NewTranscriptStreamError(serverapi.TranscriptCloseReason(reason), terminalErr)
		}
		return zero, terminalErr
	default:
		var zero Event
		return zero, errors.Join(serverapi.ErrStreamFailed, fmt.Errorf("unexpected notification method %q", message.Method))
	}
}

func (s *remoteSubscription[Event]) Next(ctx context.Context) (Event, error) {
	event, err := s.next(ctx, s.conn)
	if errors.Is(err, io.EOF) {
		_ = s.Close()
	}
	return event, err
}

func (s *remoteSubscription[Event]) Close() error {
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
