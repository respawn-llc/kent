package client

import (
	"context"

	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func subscribeSessionBinary[
	Request interface {
		proto.Message
		GetSessionId() string
	},
	Success any, Failure comparableProtoMessage,
	Result generatedUnaryResult[Success, Failure], Event proto.Message,
](
	c *Remote, ctx context.Context, method protoreflect.MethodDescriptor, request Request,
	result Result, decodeFailure func(Failure) error, newEvent func() Event,
) (*remoteSubscription[Event], error) {
	attachment, err := c.subscriptionAttachment(request.GetSessionId())
	if err != nil {
		return nil, err
	}
	return subscribeGeneratedBinary(c, ctx, method, request, result, decodeFailure, newEvent,
		func() *sharedpb.StreamCompletion { return &sharedpb.StreamCompletion{} }, binaryStreamCompletionError, attachment)
}

func (c *Remote) SubscribeSessionTranscript(ctx context.Context, request *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
	handoff, installHandoff, err := c.prepareDraftHandoff(ctx, request.GetSessionId())
	if err != nil {
		return nil, err
	}
	subscriptionRemote := c
	if handoff != nil {
		subscriptionRemote = handoff.remote
	}
	subscription, err := subscribeSessionBinary(subscriptionRemote, ctx,
		bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "StreamService", "Subscribe"),
		request, &transcriptpb.SubscribeResult{},
		func(failure *transcriptpb.SubscribeError) error {
			return generatedOperationFailure(failure.Code)
		}, func() *transcriptpb.Message { return &transcriptpb.Message{} })
	if err != nil {
		if installHandoff {
			_ = handoff.remote.Close()
		}
		return nil, err
	}
	if installHandoff {
		if err := c.installDraftHandoff(handoff); err != nil {
			_ = subscription.Close()
			return nil, err
		}
	}
	return subscription, nil
}

func (c *Remote) SubscribeQuestionHistory(ctx context.Context, request *sessionpb.QuestionHistorySubscribeRequest) (serverapi.QuestionHistorySubscription, error) {
	return subscribeSessionBinary(c, ctx,
		bootstrapMethod(sessionpb.File_kent_api_session_session_proto, "QuestionHistoryService", "Subscribe"),
		request, &sessionpb.QuestionHistorySubscribeResult{},
		func(failure *sessionpb.QuestionHistorySubscribeError) error {
			return generatedOperationFailure(failure.Code)
		}, func() *sessionpb.QuestionHistoryEvent { return &sessionpb.QuestionHistoryEvent{} })
}

func (c *Remote) SubscribeFollowUp(ctx context.Context, request *promptpb.FollowUpWatchRequest) (serverapi.PromptFollowUpSubscription, error) {
	return subscribeSessionBinary(c, ctx,
		bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "FollowUpService", "Watch"),
		request, &promptpb.FollowUpStartResult{},
		func(failure *promptpb.FollowUpWatchError) error {
			return generatedOperationFailure(failure.Code)
		}, func() *promptpb.FollowUpEvent { return &promptpb.FollowUpEvent{} })
}

func (c *Remote) SubscribeSessionAttentionNotifications(ctx context.Context, request *attentionpb.SubscribeRequest) (serverapi.SessionAttentionNotificationSubscription, error) {
	return subscribeSessionBinary(c, ctx,
		bootstrapMethod(attentionpb.File_kent_api_attention_attention_proto, "SessionService", "Subscribe"),
		request, &attentionpb.StartResult{},
		func(failure *attentionpb.StartError) error {
			return generatedOperationFailure(failure.Code)
		}, func() *attentionpb.NotificationEvent { return &attentionpb.NotificationEvent{} })
}

func (c *Remote) SubscribeGoalObservation(ctx context.Context, request *runtimepb.GoalObserveRequest) (serverapi.GoalObservationSubscription, error) {
	return subscribeSessionBinary(c, ctx,
		bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Observe"),
		request, &runtimepb.GoalObserveResult{},
		func(failure *runtimepb.GoalObserveError) error {
			return generatedOperationFailure(failure.Code)
		}, func() *runtimepb.GoalObservation { return &runtimepb.GoalObservation{} })
}
