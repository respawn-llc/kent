package client

import (
	"context"
	"errors"
	"fmt"
	"io"

	"core/shared/protoapi"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/protocol"
	"core/shared/rpcwire"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func subscribeWorktreeSetupBinary(
	c *Remote,
	ctx context.Context,
	method protoreflect.MethodDescriptor,
	request *worktreepb.SetupSubscribeRequest,
) (*remoteSubscription[*worktreepb.SetupEvent], error) {
	return subscribeGeneratedBinary(c, ctx, method, request, &worktreepb.SetupStartResult{},
		worktreeError[*worktreepb.SetupStartError], func() *worktreepb.SetupEvent { return &worktreepb.SetupEvent{} },
		func() *worktreepb.SetupCompletion { return &worktreepb.SetupCompletion{} },
		func(completion *worktreepb.SetupCompletion) error {
			if completion.Code == nil {
				return io.EOF
			}
			return protocolError(&protocol.ResponseError{Code: int(completion.GetCode()), Message: completion.GetDiagnostic()})
		}, nil)
}

func subscribeGeneratedBinary[
	Request proto.Message, Success any, Failure comparableProtoMessage,
	Result generatedUnaryResult[Success, Failure], Event proto.Message, Completion proto.Message,
](
	c *Remote, ctx context.Context, method protoreflect.MethodDescriptor, request Request,
	startResult Result, decodeFailure func(Failure) error,
	newEvent func() Event, newCompletion func() Completion, completionError func(Completion) error,
	attachment *remoteAttachmentIntent,
) (*remoteSubscription[Event], error) {
	operations, err := protoapi.ResolveSubscriptionOperations(method)
	if err != nil {
		return nil, err
	}
	conn, cleanup, err := c.openRPCConnWithAdditionalAttachment(ctx, attachment)
	if err != nil {
		return nil, err
	}
	if err := callBinaryRPC(ctx, conn, operations.Subscribe.Name, method, request, startResult); err != nil {
		cleanup()
		return nil, errors.Join(serverapi.ErrStreamFailed, err)
	}
	if _, err := decodeGeneratedResult(method, startResult, decodeFailure); err != nil {
		cleanup()
		return nil, err
	}
	return &remoteSubscription[Event]{
		conn: conn,
		next: func(ctx context.Context, conn rpcwire.Conn) (Event, error) {
			return nextBinarySubscriptionEvent(ctx, conn, operations, newEvent, newCompletion, completionError)
		},
	}, nil
}

func nextBinarySubscriptionEvent[Event proto.Message, Completion proto.Message](
	ctx context.Context,
	conn rpcwire.Conn,
	operations protoapi.SubscriptionOperations,
	newEvent func() Event, newCompletion func() Completion, completionError func(Completion) error,
) (Event, error) {
	var zero Event
	frame, err := receiveFrame(ctx, conn)
	if err != nil {
		return zero, serverapi.NormalizeStreamError(err)
	}
	if frame.Kind != rpcwire.FrameBinary {
		return zero, errors.Join(serverapi.ErrStreamFailed,
			fmt.Errorf("operation %s received a JSON frame", operations.Subscribe.Name))
	}
	envelope, err := protoapi.DecodeEnvelope(frame.Payload)
	if err != nil {
		return zero, errors.Join(serverapi.ErrStreamFailed, err)
	}
	notification := envelope.GetNotificationEvent()
	if notification == nil || notification.Payload == nil {
		return zero, errors.Join(serverapi.ErrStreamFailed,
			fmt.Errorf("operation %s received an unexpected envelope", operations.Subscribe.Name))
	}
	switch notification.Operation {
	case operations.Event.Name:
		event := newEvent()
		if err := protoapi.Decode(notification.Payload, event); err != nil {
			return zero, errors.Join(serverapi.ErrStreamFailed, err)
		}
		return event, nil
	case operations.Completion.Name:
		completion := newCompletion()
		if err := protoapi.Decode(notification.Payload, completion); err != nil {
			return zero, errors.Join(serverapi.ErrStreamFailed, err)
		}
		_ = conn.Close()
		return zero, completionError(completion)
	default:
		return zero, errors.Join(serverapi.ErrStreamFailed, fmt.Errorf(
			"operation %s received unexpected notification %s", operations.Subscribe.Name, notification.Operation))
	}
}

func binaryStreamCompletionError(completion *sharedpb.StreamCompletion) error {
	if completion.Code == nil {
		return io.EOF
	}
	err := protocolError(&protocol.ResponseError{Code: int(completion.GetCode()), Message: completion.GetMessage()})
	if completion.TranscriptCloseReason != nil {
		var reason serverapi.TranscriptCloseReason
		switch completion.GetTranscriptCloseReason() {
		case sharedpb.TranscriptCloseReason_TRANSCRIPT_CLOSE_REASON_SUBSCRIBER_OVERFLOW:
			reason = serverapi.TranscriptCloseReasonSubscriberOverflow
		case sharedpb.TranscriptCloseReason_TRANSCRIPT_CLOSE_REASON_CONTRACT_VIOLATION:
			reason = serverapi.TranscriptCloseReasonContractViolation
		default:
			return errors.Join(serverapi.ErrStreamFailed, errors.New("invalid transcript close reason"))
		}
		return serverapi.NewTranscriptStreamError(reason, err)
	}
	return err
}
