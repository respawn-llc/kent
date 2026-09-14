package transport

import (
	"context"
	"errors"

	"core/shared/apicontract"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func binaryRuntimeControlFailure(sessionID string, err error) proto.Message {
	var rejected *serverapi.RuntimeCommandNotAcceptedError
	var notPending *serverapi.PendingWorkNotPendingError
	var commandErr *serverapi.PromptCommandError
	if detail := binaryWorkflowContinuationFailure(err); detail != nil {
		return detail
	}
	switch {
	case errors.As(err, &rejected):
		return protoapi.RuntimeCommandNotAcceptedToProto(sessionID, rejected)
	case errors.As(err, &notPending):
		return &runtimepb.PendingWorkNotPendingDetails{ItemId: notPending.ItemID.String()}
	case errors.As(err, &commandErr):
		detail, conversionErr := protoapi.PromptCommandErrorToProto(commandErr)
		if conversionErr != nil {
			return binaryInternalFailure(errors.Join(err, conversionErr))
		}
		return detail
	case errors.Is(err, serverapi.ErrRuntimeUnavailable):
		return &runtimepb.RuntimeUnavailableDetails{SessionId: sessionID}
	case errors.Is(err, serverapi.ErrManualCompactionTooSoon):
		return &chatpb.ManualCompactionTooSoonDetails{}
	case errors.Is(err, serverapi.ErrManualCompactionDisabled):
		return &chatpb.ManualCompactionDisabledDetails{}
	case errors.Is(err, serverapi.ErrManualCompactionActive):
		return &chatpb.ManualCompactionActiveDetails{}
	default:
		return binaryAuthFailure(err)
	}
}

func registerRuntimeControlGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	return errors.Join(
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("SettingsService"), "SetSessionName",
			func() *runtimepb.SetSessionNameRequest { return &runtimepb.SetSessionNameRequest{} },
			func(client apicontract.RuntimeControlService, ctx context.Context, request *runtimepb.SetSessionNameRequest) (*emptypb.Empty, error) {
				if err := client.SetSessionName(ctx, request); err != nil {
					return nil, err
				}
				return &emptypb.Empty{}, nil
			}),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("AppendService"), "AppendCommittedEntry",
			func() *transcriptpb.AppendCommittedEntryRequest { return &transcriptpb.AppendCommittedEntryRequest{} },
			func(client apicontract.RuntimeControlService, ctx context.Context, request *transcriptpb.AppendCommittedEntryRequest) (*emptypb.Empty, error) {
				if err := client.AppendCommittedEntry(ctx, request); err != nil {
					return nil, err
				}
				return &emptypb.Empty{}, nil
			}),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService"), "ShouldCompactBeforeUserMessage",
			func() *runtimepb.ShouldCompactRequest { return &runtimepb.ShouldCompactRequest{} },
			apicontract.RuntimeControlService.ShouldCompactBeforeUserMessage),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService"), "SubmitUserTurn",
			func() *runtimepb.SubmitUserTurnRequest { return &runtimepb.SubmitUserTurnRequest{} },
			apicontract.RuntimeControlService.SubmitUserTurn),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService"), "SubmitUserShellCommand",
			func() *runtimepb.ShellCommandRequest { return &runtimepb.ShellCommandRequest{} },
			func(client apicontract.RuntimeControlService, ctx context.Context, request *runtimepb.ShellCommandRequest) (*emptypb.Empty, error) {
				if err := client.SubmitUserShellCommand(ctx, request); err != nil {
					return nil, err
				}
				return &emptypb.Empty{}, nil
			}),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService"), "CompactContext",
			func() *runtimepb.CompactContextRequest { return &runtimepb.CompactContextRequest{} },
			func(client apicontract.RuntimeControlService, ctx context.Context, request *runtimepb.CompactContextRequest) (*emptypb.Empty, error) {
				if err := client.CompactContext(ctx, request); err != nil {
					return nil, err
				}
				return &emptypb.Empty{}, nil
			}),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService"), "Interrupt",
			func() *runtimepb.InterruptRequest { return &runtimepb.InterruptRequest{} },
			apicontract.RuntimeControlService.Interrupt),
		registerRuntimeControlUnary(bindings, runtimePendingWorkClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService"), "ListPendingWork",
			func() *runtimepb.ListPendingWorkRequest { return &runtimepb.ListPendingWorkRequest{} },
			apicontract.RuntimePendingWorkService.ListPendingWork),
		registerRuntimeControlUnary(bindings, runtimePendingWorkClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("TurnService"), "RemovePendingWork",
			func() *runtimepb.RemovePendingWorkRequest { return &runtimepb.RemovePendingWorkRequest{} },
			apicontract.RuntimePendingWorkService.RemovePendingWork),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, promptpb.File_kent_api_prompt_prompt_proto.Services().ByName("HistoryService"), "Record",
			func() *promptpb.RecordHistoryRequest { return &promptpb.RecordHistoryRequest{} },
			func(client apicontract.RuntimeControlService, ctx context.Context, request *promptpb.RecordHistoryRequest) (*emptypb.Empty, error) {
				if err := client.RecordPromptHistory(ctx, request); err != nil {
					return nil, err
				}
				return &emptypb.Empty{}, nil
			}),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService"), "Show",
			func() *runtimepb.GoalShowRequest { return &runtimepb.GoalShowRequest{} },
			apicontract.RuntimeControlService.ShowGoal),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService"), "Pause",
			func() *runtimepb.GoalMutationRequest { return &runtimepb.GoalMutationRequest{} },
			apicontract.RuntimeControlService.PauseGoal),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService"), "Resume",
			func() *runtimepb.GoalMutationRequest { return &runtimepb.GoalMutationRequest{} },
			apicontract.RuntimeControlService.ResumeGoal),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService"), "Complete",
			func() *runtimepb.GoalMutationRequest { return &runtimepb.GoalMutationRequest{} },
			apicontract.RuntimeControlService.CompleteGoal),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeControlClient, runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService"), "Clear",
			func() *runtimepb.GoalClearRequest { return &runtimepb.GoalClearRequest{} },
			apicontract.RuntimeControlService.ClearGoal),
	)
}

type runtimeControlRequest interface {
	proto.Message
	GetSessionId() string
}

func registerRuntimeControlUnary[Service any, Request runtimeControlRequest, Success proto.Message](
	bindings map[string]gatewayBinaryBinding, resolve func(GatewayDependencies) Service, service protoreflect.ServiceDescriptor, method protoreflect.Name,
	newRequest func() Request, invoke func(Service, context.Context, Request) (Success, error),
	failure ...func(Request, error) proto.Message,
) error {
	if len(failure) > 1 {
		return errors.New("runtime unary binding accepts one failure classifier")
	}
	return registerGatewayBinaryUnary(bindings, service, method, gatewayBinaryCoreActiveOrdinary, newRequest,
		func(request Request) (routeScopeParams, error) {
			return routeScopeParams{sessionID: request.GetSessionId()}, nil
		},
		func(g *Gateway, ctx context.Context, _ *connectionState, request Request) (Success, error) {
			client := resolve(g.deps)
			if any(client) == nil {
				var zero Success
				return zero, errors.New("runtime control client is required")
			}
			return invoke(client, ctx, request)
		},
		func(_ *Gateway, _ *connectionState, request Request, err error) proto.Message {
			if errors.Is(err, serverapi.ErrServerAuthRequired) {
				return binaryAuthFailure(err)
			}
			if len(failure) == 1 && failure[0] != nil {
				if detail := failure[0](request, err); detail != nil {
					return detail
				}
			}
			return binaryRuntimeControlFailure(request.GetSessionId(), err)
		})
}
