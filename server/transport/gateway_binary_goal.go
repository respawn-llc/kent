package transport

import (
	"context"
	"errors"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
)

func registerGoalGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService")
	return registerGatewayBinaryUnary(
		bindings,
		service,
		"Set",
		gatewayBinaryCoreActiveOrdinary,
		func() *runtimepb.GoalSetRequest { return &runtimepb.GoalSetRequest{} },
		goalSetTargetScope,
		func(g *Gateway, ctx context.Context, _ *connectionState, request *runtimepb.GoalSetRequest) (*runtimepb.GoalSetSuccess, error) {
			client := g.deps.ChatMutationClient()
			if client == nil {
				return nil, errors.New("Chat mutation service is required")
			}
			return client.SetGoal(ctx, request)
		},
		binaryGoalSetFailure,
	)
}

func goalSetTargetScope(request *runtimepb.GoalSetRequest) (routeScopeParams, error) {
	target, err := protoapi.ChatTargetFromRequest(request)
	if err != nil {
		return routeScopeParams{}, err
	}
	return routeScopeParams{chatTarget: target}, nil
}

func binaryGoalSetFailure(
	_ *Gateway,
	_ *connectionState,
	request *runtimepb.GoalSetRequest,
	err error,
) proto.Message {
	var sessionID runtimeids.SessionID
	if request != nil {
		if requested := request.Target.GetSession(); requested != nil {
			sessionID, _ = runtimeids.ParseSessionID(requested.SessionId)
		}
	}
	return protoapi.GoalSetErrorFromError(err, sessionID)
}
