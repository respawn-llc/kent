package transport

import (
	"context"
	"errors"

	"core/shared/apicontract"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func registerProcessGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	view := processpb.File_kent_api_process_process_proto.Services().ByName("ViewService")
	control := processpb.File_kent_api_process_process_proto.Services().ByName("ControlService")
	return errors.Join(
		registerProcessUnary(bindings, view, "List", GatewayDependencies.ProcessViewClient,
			func() *processpb.ListRequest { return &processpb.ListRequest{} },
			func(request *processpb.ListRequest) (routeScopeParams, error) {
				return routeScopeParams{projectID: request.ProjectId}, nil
			}, apicontract.ProcessViewService.ListProcesses),
		registerProcessUnary(bindings, view, "Get", GatewayDependencies.ProcessViewClient,
			func() *processpb.GetRequest { return &processpb.GetRequest{} },
			func(request *processpb.GetRequest) (routeScopeParams, error) {
				return routeScopeParams{processID: request.ProcessId}, nil
			}, apicontract.ProcessViewService.GetProcess),
		registerProcessUnary(bindings, control, "Kill", GatewayDependencies.ProcessControlClient,
			func() *processpb.KillRequest { return &processpb.KillRequest{} },
			nil, apicontract.ProcessControlService.KillProcess),
		registerProcessUnary(bindings, control, "InlineOutput", GatewayDependencies.ProcessControlClient,
			func() *processpb.InlineOutputRequest { return &processpb.InlineOutputRequest{} },
			func(request *processpb.InlineOutputRequest) (routeScopeParams, error) {
				return routeScopeParams{processID: request.ProcessId}, nil
			}, apicontract.ProcessControlService.GetInlineOutput),
	)
}

func registerProcessUnary[Client any, Request proto.Message, Success proto.Message](
	bindings map[string]gatewayBinaryBinding,
	service protoreflect.ServiceDescriptor,
	method protoreflect.Name,
	client func(GatewayDependencies) Client,
	request func() Request,
	scope func(Request) (routeScopeParams, error),
	invoke func(Client, context.Context, Request) (Success, error),
) error {
	return registerGatewayBinaryUnary(bindings, service, method, gatewayBinaryCoreActiveOrdinary,
		request, scope,
		func(g *Gateway, ctx context.Context, _ *connectionState, request Request) (Success, error) {
			return invoke(client(g.deps), ctx, request)
		},
		func(_ *Gateway, _ *connectionState, _ Request, err error) proto.Message {
			return binaryAuthFailure(err)
		})
}
