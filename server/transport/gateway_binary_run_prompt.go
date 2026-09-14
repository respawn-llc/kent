package transport

import (
	"context"
	"errors"
	"sync"

	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

func registerRunPromptGatewayBinaryBinding(bindings map[string]gatewayBinaryBinding) error {
	method := runpromptpb.File_kent_api_run_prompt_run_prompt_proto.Services().ByName("RunService").Methods().ByName("Prompt")
	operation, err := protoapi.OperationFromDescriptor(method)
	if err != nil {
		return err
	}
	progressOperation, err := protoapi.ResolveProgressOperation(method)
	if err != nil {
		return err
	}
	bindings[operation.Name] = gatewayBinaryBinding{
		operation: operation, policy: gatewayBinaryCoreActiveOrdinary, progressEvent: &progressOperation,
		request: func() proto.Message { return &runpromptpb.Request{} },
		failure: func(_ *Gateway, _ *connectionState, _ proto.Message, err error) proto.Message {
			if details := binaryWorkflowContinuationFailure(err); details != nil {
				return gatewayBinaryFailureResult(method, details)
			}
			if details, ok := binaryServerNotReadyDetails(err); ok {
				return gatewayBinaryFailureResult(method, details)
			}
			return gatewayBinaryFailureResult(method, binaryAuthFailure(err))
		},
		invoke: func(g *Gateway, ctx context.Context, state *connectionState, message proto.Message, emit func(proto.Message) error) (proto.Message, error) {
			request, err := protoapi.RunPromptRequestFromProto(message.(*runpromptpb.Request))
			if err != nil {
				return nil, err
			}
			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			client, err := g.runPromptClientForState(runCtx, state)
			if err != nil {
				return nil, err
			}
			var mu sync.Mutex
			var progressErr error
			sink := serverapi.RunPromptProgressFunc(func(event *runpromptpb.ProgressEvent) {
				mu.Lock()
				defer mu.Unlock()
				if progressErr != nil {
					return
				}
				if err := emit(event); err != nil {
					progressErr = err
					cancel()
				}
			})
			result, runErr := client.RunPrompt(runCtx, request, sink)
			mu.Lock()
			err = errors.Join(runErr, progressErr)
			mu.Unlock()
			if err != nil {
				return nil, err
			}
			return protoapi.SuccessResult(method, result)
		},
	}
	return nil
}
