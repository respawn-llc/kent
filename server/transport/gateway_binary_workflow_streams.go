package transport

import (
	"context"
	"errors"

	"core/shared/apicontract"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func registerWorkflowStreamsGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	file := pb.File_kent_api_workflow_definition_workflow_definition_proto
	return errors.Join(
		registerGatewayBinarySubscription(bindings, file.Services().ByName("WorkflowSubscriptionService"), "Subscribe",
			func() *pb.WorkflowSubscribeRequest { return &pb.WorkflowSubscribeRequest{} }, nil,
			func(g *Gateway, ctx context.Context, request *pb.WorkflowSubscribeRequest) (apicontract.WorkflowEventSubscription, error) {
				return g.deps.WorkflowClient().SubscribeWorkflow(ctx, request)
			}, binaryWorkflowEntityFailure[*pb.WorkflowSubscribeRequest]),
		registerGatewayBinarySubscription(bindings, file.Services().ByName("ProjectSubscriptionService"), "Subscribe",
			func() *pb.ProjectSubscribeRequest { return &pb.ProjectSubscribeRequest{} }, nil,
			func(g *Gateway, ctx context.Context, request *pb.ProjectSubscribeRequest) (apicontract.WorkflowEventSubscription, error) {
				return g.deps.WorkflowClient().SubscribeWorkflowProject(ctx, request)
			}, binaryWorkflowCreateFailure[*pb.ProjectSubscribeRequest]),
	)
}

func registerAttentionNotificationGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := taskpb.File_kent_api_workflow_task_attention_proto.Services().ByName("AttentionNotificationService")
	return registerGatewayBinarySubscription(bindings, service, "Subscribe",
		func() *emptypb.Empty { return &emptypb.Empty{} }, nil,
		func(g *Gateway, ctx context.Context, request *emptypb.Empty) (apicontract.AttentionNotificationSubscription, error) {
			return g.deps.AttentionNotificationClient().SubscribeAttentionNotifications(ctx, request)
		}, func(_ *emptypb.Empty, err error) proto.Message { return binaryAuthFailure(err) })
}
