package transport

import (
	"context"
	"errors"

	"core/shared/apicontract"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"

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
			return binaryAuthFailure(err)
		})
}
