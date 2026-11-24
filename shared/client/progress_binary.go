package client

import (
	"context"
	"core/shared/protoapi"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/rpcwire"
	"fmt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func (c *Remote) callProgress(ctx context.Context, correlation string, method protoreflect.MethodDescriptor, request, result proto.Message, newEvent func() proto.Message, publish func(proto.Message)) error {
	progressOperation, err := protoapi.ResolveProgressOperation(method)
	if err != nil {
		return err
	}
	frame, operation, err := binaryCallFrame(correlation, method, request, result)
	if err != nil {
		return err
	}
	conn, cleanup, err := c.openRPCConn(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := conn.Send(ctx, frame); err != nil {
		return err
	}
	for {
		frame, err := receiveFrame(ctx, conn)
		if err != nil {
			return err
		}
		if frame.Kind != rpcwire.FrameBinary {
			return fmt.Errorf("%s received a non-binary frame", operation.Name)
		}
		envelope, err := protoapi.DecodeEnvelope(frame.Payload)
		if err != nil {
			return err
		}
		switch selected := envelope.GetFrame().(type) {
		case *sharedpb.Envelope_NotificationEvent:
			if selected.NotificationEvent.Operation != progressOperation.Name {
				return fmt.Errorf("unexpected progress operation %q", selected.NotificationEvent.Operation)
			}
			event := newEvent()
			if err := protoapi.Decode(selected.NotificationEvent.Payload, event); err != nil {
				return err
			}
			publish(event)
		case *sharedpb.Envelope_Result, *sharedpb.Envelope_TransportFailure:
			return decodeBinaryResponse(operation, correlation, &remoteBinaryResponse{result: envelope.GetResult(), failure: envelope.GetTransportFailure()}, result)
		default:
			return fmt.Errorf("%s received an unexpected frame %T", operation.Name, selected)
		}
	}
}
