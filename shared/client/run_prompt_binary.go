package client

import (
	"context"
	"fmt"

	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/rpcwire"
	"core/shared/serverapi"
)

func (c *Remote) RunPrompt(ctx context.Context, request serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (*runpromptpb.Success, error) {
	generated, err := protoapi.RunPromptRequestToProto(request)
	if err != nil {
		return nil, err
	}
	method := bootstrapMethod(runpromptpb.File_kent_api_run_prompt_run_prompt_proto, "RunService", "Prompt")
	progressOperation, err := protoapi.ResolveProgressOperation(method)
	if err != nil {
		return nil, err
	}
	result := &runpromptpb.Result{}
	const correlation = "run-prompt"
	frame, operation, err := binaryCallFrame(correlation, method, generated, result)
	if err != nil {
		return nil, err
	}
	conn, cleanup, err := c.openRPCConn(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	if err := conn.Send(ctx, frame); err != nil {
		return nil, err
	}
	for {
		frame, err := receiveFrame(ctx, conn)
		if err != nil {
			return nil, err
		}
		if frame.Kind != rpcwire.FrameBinary {
			return nil, fmt.Errorf("%s received a non-binary frame", operation.Name)
		}
		envelope, err := protoapi.DecodeEnvelope(frame.Payload)
		if err != nil {
			return nil, err
		}
		switch selected := envelope.GetFrame().(type) {
		case *sharedpb.Envelope_NotificationEvent:
			if selected.NotificationEvent.Operation != progressOperation.Name {
				return nil, fmt.Errorf("unexpected progress operation %q", selected.NotificationEvent.Operation)
			}
			event := &runpromptpb.ProgressEvent{}
			if err := protoapi.Decode(selected.NotificationEvent.Payload, event); err != nil {
				return nil, err
			}
			if progress != nil {
				progress.PublishRunPromptProgress(event)
			}
		case *sharedpb.Envelope_Result, *sharedpb.Envelope_TransportFailure:
			response := &remoteBinaryResponse{result: envelope.GetResult(), failure: envelope.GetTransportFailure()}
			if err := decodeBinaryResponse(operation, correlation, response, result); err != nil {
				return nil, err
			}
			return decodeGeneratedResult(method, result, func(failure *runpromptpb.Error) error {
				return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
			})
		default:
			return nil, fmt.Errorf("%s received an unexpected frame %T", operation.Name, selected)
		}
	}
}
