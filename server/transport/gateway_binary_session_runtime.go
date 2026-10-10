package transport

import (
	"context"
	"errors"

	"core/server/sessionruntime"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
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
			func(g *Gateway, ctx context.Context, state *connectionState, request *sessionlaunchpb.SessionRuntimeActivateRequest) (*sessionlaunchpb.SessionRuntimeActivateSuccess, error) {
				native, err := protoapi.SessionRuntimeActivateFromProto(request)
				if err != nil {
					return nil, err
				}
				native.OwnerID = state.runtimeOwnerID
				attachment, err := g.deps.SessionRuntimeClient().ActivateSessionRuntime(ctx, native)
				if err != nil {
					return nil, err
				}
				if err := attachment.Validate(); err != nil {
					return nil, err
				}
				state.recordOwnedRuntime(attachment)
				return &sessionlaunchpb.SessionRuntimeActivateSuccess{Attachment: protoapi.SessionRuntimeAttachmentToProto(attachment)}, nil
			},
			func(g *Gateway, _ *connectionState, request *sessionlaunchpb.SessionRuntimeActivateRequest, err error) proto.Message {
				if errors.Is(err, sessionruntime.ErrAuthorityClosed) {
					err = errors.Join(serverapi.ErrRuntimeUnavailable, err)
				}
				if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
					return binaryAuthFailure(err)
				}
				id, lookupErr := g.deps.MetadataStore().ResolveSessionTarget(context.Background(), request.GetSessionId())
				if lookupErr != nil {
					return binaryAuthFailure(errors.Join(err, lookupErr))
				}
				return binaryRuntimeLifetimeFailure(id.String(), err)
			}),
		registerGatewayBinaryUnary(bindings, service, "Release", gatewayBinaryCoreActiveOrdinary,
			func() *sessionlaunchpb.SessionRuntimeReleaseRequest {
				return &sessionlaunchpb.SessionRuntimeReleaseRequest{}
			},
			func(request *sessionlaunchpb.SessionRuntimeReleaseRequest) (routeScopeParams, error) {
				return routeScopeParams{sessionID: request.Attachment.SessionId}, nil
			},
			func(g *Gateway, ctx context.Context, state *connectionState, request *sessionlaunchpb.SessionRuntimeReleaseRequest) (*sessionlaunchpb.SessionRuntimeReleaseSuccess, error) {
				native, err := protoapi.SessionRuntimeReleaseFromProto(request)
				if err != nil {
					return nil, err
				}
				native.OwnerID = state.runtimeOwnerID
				response, err := g.deps.SessionRuntimeClient().ReleaseSessionRuntime(ctx, native)
				if err == nil && (response.Released || native.DropOwner) {
					state.removeOwnedRuntime(native.Attachment)
				}
				return response, err
			},
			func(_ *Gateway, _ *connectionState, request *sessionlaunchpb.SessionRuntimeReleaseRequest, err error) proto.Message {
				return binaryRuntimeLifetimeFailure(request.GetAttachment().GetSessionId(), err)
			}),
	)
}

func binaryRuntimeLifetimeFailure(sessionID string, err error) proto.Message {
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return &sessionlaunchpb.SessionRuntimeUnavailableDetails{SessionId: sessionID}
	}
	return binaryAuthReadinessFailure(err)
}
