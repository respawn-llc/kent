package transport

import (
	"context"
	"errors"

	"core/shared/apicontract"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func registerPromptGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	services := promptpb.File_kent_api_prompt_prompt_proto.Services()
	return errors.Join(
		registerPromptUnary(bindings, services.ByName("QuestionService"), "ListPending", GatewayDependencies.AskViewClient,
			func() *promptpb.ListPendingRequest { return &promptpb.ListPendingRequest{} },
			apicontract.AskViewService.ListPendingAsksBySession),
		registerPromptUnary(bindings, services.ByName("ApprovalService"), "ListPending", GatewayDependencies.ApprovalViewClient,
			func() *promptpb.ListPendingRequest { return &promptpb.ListPendingRequest{} },
			apicontract.ApprovalViewService.ListPendingApprovalsBySession),
		registerPromptUnary(bindings, services.ByName("AnswerService"), "AnswerBatch", GatewayDependencies.PromptControlClient,
			func() *promptpb.AnswerBatchRequest { return &promptpb.AnswerBatchRequest{} },
			apicontract.PromptControlService.AnswerPromptBatch),
	)
}

func registerPromptUnary[Client any, Request interface {
	proto.Message
	GetSessionId() string
}, Success proto.Message](
	bindings map[string]gatewayBinaryBinding,
	service protoreflect.ServiceDescriptor,
	method protoreflect.Name,
	client func(GatewayDependencies) Client,
	request func() Request,
	invoke func(Client, context.Context, Request) (Success, error),
) error {
	return registerGatewayBinaryUnary(bindings, service, method, gatewayBinaryCoreActiveOrdinary,
		request, func(request Request) (routeScopeParams, error) {
			return routeScopeParams{sessionID: request.GetSessionId()}, nil
		},
		func(g *Gateway, ctx context.Context, _ *connectionState, request Request) (Success, error) {
			return invoke(client(g.deps), ctx, request)
		},
		func(_ *Gateway, _ *connectionState, _ Request, err error) proto.Message {
			return binaryAuthFailure(err)
		})
}
