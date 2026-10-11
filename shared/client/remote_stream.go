package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	rpccontract "core/shared/apicontract"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/rpcwire"
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
