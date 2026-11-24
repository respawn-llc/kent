package transport

import (
	"context"
	"core/shared/protoapi"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
	"errors"
	"google.golang.org/protobuf/proto"
)

func registerTaskImportGatewayBinaryBinding(bindings map[string]gatewayBinaryBinding) error {
	method := taskimportpb.File_kent_api_task_import_task_import_proto.Services().ByName("TaskImportService").Methods().ByName("Import")
	operation, err := protoapi.OperationFromDescriptor(method)
	if err != nil {
		return err
	}
	progress, err := protoapi.ResolveProgressOperation(method)
	if err != nil {
		return err
	}
	bindings[operation.Name] = gatewayBinaryBinding{
		operation: operation, policy: gatewayBinaryCoreActiveOrdinary, progressEvent: &progress,
		request: func() proto.Message { return &taskimportpb.Request{} },
		failure: func(_ *Gateway, _ *connectionState, _ proto.Message, err error) proto.Message {
			var failure *serverapi.TaskImportError
			if errors.As(err, &failure) {
				return gatewayBinaryFailureResult(method, failure.Detail)
			}
			if details, ok := binaryServerNotReadyDetails(err); ok {
				return gatewayBinaryFailureResult(method, details)
			}
			return gatewayBinaryFailureResult(method, binaryAuthReadinessFailure(err))
		},
		invoke: func(g *Gateway, ctx context.Context, _ *connectionState, message proto.Message, emit func(proto.Message) error) (proto.Message, error) {
			var deliveryErr error
			sink := serverapi.TaskImportProgressFunc(func(event *taskimportpb.ProgressEvent) {
				if deliveryErr == nil {
					deliveryErr = emit(event)
				}
			})
			result, err := g.deps.TaskImportClient().ImportTasks(ctx, message.(*taskimportpb.Request), sink)
			if err = errors.Join(err, deliveryErr); err != nil {
				return nil, err
			}
			return protoapi.SuccessResult(method, result)
		},
	}
	return nil
}
