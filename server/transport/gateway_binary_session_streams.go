package transport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"core/shared/protoapi"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func registerSessionStreamsGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	return errors.Join(
		registerSessionBinarySubscription(bindings,
			transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("StreamService"), "Subscribe",
			func() *transcriptpb.SubscribeRequest { return &transcriptpb.SubscribeRequest{} },
			func(g *Gateway, ctx context.Context, request *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
				return g.deps.SessionTranscriptClient().SubscribeSessionTranscript(ctx, request)
			}),
		registerSessionBinarySubscription(bindings,
			sessionpb.File_kent_api_session_session_proto.Services().ByName("QuestionHistoryService"), "Subscribe",
			func() *sessionpb.QuestionHistorySubscribeRequest { return &sessionpb.QuestionHistorySubscribeRequest{} },
			func(g *Gateway, ctx context.Context, request *sessionpb.QuestionHistorySubscribeRequest) (serverapi.QuestionHistorySubscription, error) {
				return g.deps.SessionViewClient().SubscribeQuestionHistory(ctx, request)
			}),
		registerSessionBinarySubscription(bindings,
			promptpb.File_kent_api_prompt_prompt_proto.Services().ByName("FollowUpService"), "Watch",
			func() *promptpb.FollowUpWatchRequest { return &promptpb.FollowUpWatchRequest{} },
			func(g *Gateway, ctx context.Context, request *promptpb.FollowUpWatchRequest) (serverapi.PromptFollowUpSubscription, error) {
				return g.deps.PromptControlClient().SubscribeFollowUp(ctx, request)
			}),
		registerSessionBinarySubscription(bindings,
			attentionpb.File_kent_api_attention_attention_proto.Services().ByName("SessionService"), "Subscribe",
			func() *attentionpb.SubscribeRequest { return &attentionpb.SubscribeRequest{} },
			func(g *Gateway, ctx context.Context, request *attentionpb.SubscribeRequest) (serverapi.SessionAttentionNotificationSubscription, error) {
				return g.deps.AttentionNotificationClient().SubscribeSessionAttentionNotifications(ctx, request)
			}),
		registerSessionBinarySubscription(bindings,
			runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService"), "Observe",
			func() *runtimepb.GoalObserveRequest { return &runtimepb.GoalObserveRequest{} },
			func(g *Gateway, ctx context.Context, request *runtimepb.GoalObserveRequest) (serverapi.GoalObservationSubscription, error) {
				return g.deps.GoalObservationClient().SubscribeGoalObservation(ctx, request)
			}),
	)
}

func registerSessionBinarySubscription[
	Request interface {
		proto.Message
		GetSessionId() string
	},
	Event proto.Message, Sub gatewaySubscription[Event],
](
	bindings map[string]gatewayBinaryBinding, service protoreflect.ServiceDescriptor, methodName protoreflect.Name,
	newRequest func() Request, subscribe func(*Gateway, context.Context, Request) (Sub, error),
) error {
	if service == nil {
		return errors.New("generated subscription service is required")
	}
	method := service.Methods().ByName(methodName)
	associated, err := protoapi.ResolveSubscriptionOperations(method)
	if err != nil {
		return err
	}
	start, err := protoapi.SuccessResult(method, &emptypb.Empty{})
	if err != nil {
		return err
	}
	bindings[associated.Subscribe.Name] = gatewayBinaryBinding{
		operation: associated.Subscribe, associated: &associated, policy: gatewayBinaryCoreActiveOrdinary,
		request: func() proto.Message { return newRequest() },
		scope: func(message proto.Message) (routeScopeParams, error) {
			request, ok := message.(Request)
			if !ok {
				return routeScopeParams{}, fmt.Errorf("%s request type is invalid", associated.Subscribe.Name)
			}
			return routeScopeParams{sessionID: request.GetSessionId()}, nil
		},
		subscribe: func(g *Gateway, ctx context.Context, _ *connectionState, message proto.Message) (gatewayBinarySubscriber, error) {
			request, ok := message.(Request)
			if !ok {
				return nil, fmt.Errorf("%s request type is invalid", associated.Subscribe.Name)
			}
			sub, err := subscribe(g, ctx, request)
			if err != nil {
				return nil, err
			}
			return sessionGatewayBinarySubscriber[Event]{sub}, nil
		},
		failure: func(_ *Gateway, _ *connectionState, _ proto.Message, err error) proto.Message {
			if details, ok := binaryServerNotReadyDetails(err); ok {
				return gatewayBinaryFailureResult(method, details)
			}
			return gatewayBinaryFailureResult(method, binaryAuthFailure(err))
		},
		start: start, complete: binarySessionStreamCompletion,
	}
	return nil
}

type sessionGatewayBinarySubscriber[Event proto.Message] struct {
	gatewaySubscription[Event]
}

func (s sessionGatewayBinarySubscriber[Event]) Next(ctx context.Context) (proto.Message, error) {
	return s.gatewaySubscription.Next(ctx)
}

func binarySessionStreamCompletion(err error) proto.Message {
	completion := &sharedpb.StreamCompletion{}
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return completion
	}
	code, message := protocolError(err)
	completion.Code = proto.Int32(int32(code))
	completion.Message = proto.String(message)
	if reason, ok := serverapi.TranscriptCloseReasonOf(err); ok {
		switch reason {
		case serverapi.TranscriptCloseReasonSubscriberOverflow:
			completion.TranscriptCloseReason = sharedpb.TranscriptCloseReason_TRANSCRIPT_CLOSE_REASON_SUBSCRIBER_OVERFLOW.Enum()
		case serverapi.TranscriptCloseReasonContractViolation:
			completion.TranscriptCloseReason = sharedpb.TranscriptCloseReason_TRANSCRIPT_CLOSE_REASON_CONTRACT_VIOLATION.Enum()
		}
	}
	return completion
}
