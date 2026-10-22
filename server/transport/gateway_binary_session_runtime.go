package transport

import (
	"context"
	"errors"

	"core/server/sessionruntime"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func registerSessionRuntimeGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionRuntimeService")
	return errors.Join(
		registerGatewayBinaryUnary(bindings, service, "Activate", gatewayBinaryCoreActiveOrdinary,
			func() *sessionlaunchpb.SessionRuntimeActivateRequest {
				return &sessionlaunchpb.SessionRuntimeActivateRequest{}
			},
			func(request *sessionlaunchpb.SessionRuntimeActivateRequest) (routeScopeParams, error) {
				return routeScopeParams{sessionSelector: request.SessionId}, nil
			},
			func(g *Gateway, ctx context.Context, _ *connectionState, request *sessionlaunchpb.SessionRuntimeActivateRequest) (*emptypb.Empty, error) {
				native, err := protoapi.SessionRuntimeActivateFromProto(request)
				if err != nil {
					return nil, err
				}
				if err := g.deps.SessionRuntimeClient().ActivateSessionRuntime(ctx, native); err != nil {
					return nil, err
				}
				return &emptypb.Empty{}, nil
			},
			func(_ *Gateway, _ *connectionState, _ *sessionlaunchpb.SessionRuntimeActivateRequest, err error) proto.Message {
				if errors.Is(err, sessionruntime.ErrAuthorityClosed) {
					err = errors.Join(serverapi.ErrRuntimeUnavailable, err)
				}
				return binaryRuntimeLifetimeFailure(err)
			}),
	)
}

func binaryRuntimeLifetimeFailure(err error) proto.Message {
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return &sessionlaunchpb.SessionRuntimeUnavailableDetails{SessionTarget: protoapi.SessionTargetErrorContext(err)}
	}
	return binaryAuthReadinessFailure(err)
}
