package transport

import (
	"context"
	"errors"
	"fmt"

	"core/shared/apicontract"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func registerWorkflowStreamsGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	file := pb.File_kent_api_workflow_definition_workflow_definition_proto
	return errors.Join(
		registerWorkflowBinarySubscription(bindings, file.Services().ByName("WorkflowSubscriptionService"),
			func() *pb.WorkflowSubscribeRequest { return &pb.WorkflowSubscribeRequest{} },
			apicontract.WorkflowService.SubscribeWorkflow, binaryWorkflowEntityFailure[*pb.WorkflowSubscribeRequest]),
		registerWorkflowBinarySubscription(bindings, file.Services().ByName("ProjectSubscriptionService"),
			func() *pb.ProjectSubscribeRequest { return &pb.ProjectSubscribeRequest{} },
			apicontract.WorkflowService.SubscribeWorkflowProject, binaryWorkflowCreateFailure[*pb.ProjectSubscribeRequest]),
	)
}

func registerWorkflowBinarySubscription[Request proto.Message](
	bindings map[string]gatewayBinaryBinding, service protoreflect.ServiceDescriptor,
	newRequest func() Request,
	subscribe func(apicontract.WorkflowService, context.Context, Request) (apicontract.WorkflowEventSubscription, error),
	failure func(Request, error) proto.Message,
) error {
	method := service.Methods().ByName("Subscribe")
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
		subscribe: func(g *Gateway, ctx context.Context, _ *connectionState, message proto.Message) (gatewayBinarySubscriber, error) {
			request, ok := message.(Request)
			if !ok {
				return nil, fmt.Errorf("%s request type is invalid", associated.Subscribe.Name)
			}
			sub, err := subscribe(g.deps.WorkflowClient(), ctx, request)
			if err != nil {
				return nil, err
			}
			return gatewayBinaryStreamSubscriber[*pb.ProjectEvent]{sub}, nil
		},
		failure: func(_ *Gateway, _ *connectionState, message proto.Message, err error) proto.Message {
			if details, ok := binaryServerNotReadyDetails(err); ok {
				return gatewayBinaryFailureResult(method, details)
			}
			return gatewayBinaryFailureResult(method, failure(message.(Request), err))
		},
		start: start, complete: binaryStreamCompletion,
	}
	return nil
}
