package transport

import (
	"context"
	"errors"

	"core/shared/apicontract"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func registerSessionLifecycleGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	return errors.Join(
		registerSessionLifecycleUnary(bindings, "GetInitialInput",
			func() *sessionlaunchpb.SessionInitialInputRequest {
				return &sessionlaunchpb.SessionInitialInputRequest{}
			},
			apicontract.SessionLifecycleService.GetInitialInput),
		registerSessionLifecycleUnary(bindings, "PersistInputDraft",
			func() *sessionlaunchpb.SessionPersistInputDraftRequest {
				return &sessionlaunchpb.SessionPersistInputDraftRequest{}
			},
			apicontract.SessionLifecycleService.PersistInputDraft),
		registerSessionLifecycleUnary(bindings, "RetargetWorkspace",
			func() *sessionlaunchpb.SessionRetargetWorkspaceRequest {
				return &sessionlaunchpb.SessionRetargetWorkspaceRequest{}
			},
			apicontract.SessionLifecycleService.RetargetSessionWorkspace),
	)
}

func registerSessionLifecycleUnary[Request interface {
	proto.Message
	GetSessionId() string
}, Success proto.Message](
	bindings map[string]gatewayBinaryBinding,
	name protoreflect.Name,
	newRequest func() Request,
	invoke func(apicontract.SessionLifecycleService, context.Context, Request) (Success, error),
) error {
	return registerGatewayBinaryUnary(bindings,
		sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionLifecycleService"),
		name, gatewayBinaryCoreActiveOrdinary, newRequest,
		func(request Request) (routeScopeParams, error) {
			return routeScopeParams{sessionID: request.GetSessionId()}, nil
		},
		func(g *Gateway, ctx context.Context, _ *connectionState, request Request) (Success, error) {
			return invoke(g.deps.SessionLifecycleClient(), ctx, request)
		},
		func(_ *Gateway, _ *connectionState, _ Request, err error) proto.Message {
			var retarget *serverapi.SessionRetargetError
			if errors.As(err, &retarget) {
				details, conversionErr := protoapi.SessionRetargetErrorToProto(retarget)
				if conversionErr != nil {
					return binaryInternalFailure(conversionErr)
				}
				return details
			}
			return binaryAuthFailure(err)
		})
}
