package transport

import (
	"context"
	"errors"

	"core/shared/apicontract"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func registerSessionViewGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	sessionService := sessionpb.File_kent_api_session_session_proto.Services().ByName("ReadService")
	transcriptService := transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("ReadService")
	return errors.Join(
		registerSessionViewUnary(bindings, sessionService, "GetPromptHistory",
			func() *sessionpb.PromptHistoryRequest { return &sessionpb.PromptHistoryRequest{} },
			apicontract.SessionViewService.GetPromptHistory),
		registerSessionViewUnary(bindings, sessionService, "GetMainView",
			func() *sessionpb.MainViewRequest { return &sessionpb.MainViewRequest{} },
			apicontract.SessionViewService.GetSessionMainView),
		registerSessionViewUnary(bindings, transcriptService, "GetPage",
			func() *transcriptpb.PageRequest { return &transcriptpb.PageRequest{} },
			apicontract.SessionViewService.GetSessionTranscriptPage),
		registerSessionViewUnary(bindings, transcriptService, "GetLatestFinalAnswer",
			func() *transcriptpb.LatestFinalAnswerRequest { return &transcriptpb.LatestFinalAnswerRequest{} },
			apicontract.SessionViewService.GetLatestCommittedAssistantFinalAnswer),
	)
}

func registerSessionViewUnary[
	Request interface {
		proto.Message
		GetSessionId() string
	},
	Success proto.Message,
](
	bindings map[string]gatewayBinaryBinding,
	service protoreflect.ServiceDescriptor,
	method protoreflect.Name,
	newRequest func() Request,
	invoke func(apicontract.SessionViewService, context.Context, Request) (Success, error),
) error {
	return registerGatewayBinaryUnary(
		bindings, service, method, gatewayBinaryCoreActiveOrdinary, newRequest,
		func(request Request) (routeScopeParams, error) {
			return routeScopeParams{sessionID: request.GetSessionId()}, nil
		},
		func(g *Gateway, ctx context.Context, _ *connectionState, request Request) (Success, error) {
			client := g.deps.SessionViewClient()
			if client == nil {
				var zero Success
				return zero, errors.New("session view client is required")
			}
			return invoke(client, ctx, request)
		},
		func(_ *Gateway, _ *connectionState, _ Request, err error) proto.Message {
			return binaryInternalFailure(err)
		},
	)
}
