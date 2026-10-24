package transport

import (
	"context"
	"errors"

	speechpb "core/shared/protoapi/gen/kent/api/speech"
	"google.golang.org/protobuf/types/known/emptypb"
)

func registerSpeechGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := speechpb.File_kent_api_speech_speech_proto.Services().ByName("SpeechService")
	return registerBootstrapUnary(bindings, service, "GetAvailability", gatewayBinaryCoreActiveOrdinary,
		func() *emptypb.Empty { return &emptypb.Empty{} },
		func(g *Gateway, ctx context.Context, _ *connectionState, req *emptypb.Empty) (*speechpb.Availability, error) {
			client := g.deps.SpeechClient()
			if client == nil {
				return nil, errors.New("speech service is unavailable")
			}
			result, err := client.GetAvailability(ctx, req)
			if err != nil {
				return nil, err
			}
			if available := result.GetAvailable(); available != nil {
				authority, err := g.sessionReattachAuthority()
				if err != nil {
					return nil, err
				}
				available.UploadUrl = authority.speechUploadURL(g.identity.ProtocolVersion)
			}
			return result, nil
		}, binaryServerReadinessFailure)
}
