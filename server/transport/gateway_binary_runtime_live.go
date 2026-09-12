package transport

import (
	"errors"

	"core/shared/apicontract"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func registerRuntimeLiveGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	service := runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("LiveService")
	return errors.Join(
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeLiveControlClient, service, "Steer",
			func() *runtimepb.LiveSteerRequest { return &runtimepb.LiveSteerRequest{} },
			apicontract.RuntimeLiveControlService.LiveSteer,
			func(_ *runtimepb.LiveSteerRequest, err error) proto.Message {
				if errors.Is(err, serverapi.ErrRuntimeNoActiveRun) {
					return &runtimepb.LiveSteerError{Code: "no_active_run", Detail: &runtimepb.LiveSteerError_NoActiveRun{NoActiveRun: &emptypb.Empty{}}}
				}
				return nil
			}),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeLiveControlClient, service, "Stop",
			func() *runtimepb.LiveStopRequest { return &runtimepb.LiveStopRequest{} },
			apicontract.RuntimeLiveControlService.LiveStop),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeLiveControlClient, service, "Wait",
			func() *runtimepb.LiveWaitRequest { return &runtimepb.LiveWaitRequest{} },
			apicontract.RuntimeLiveControlService.LiveWait,
			func(_ *runtimepb.LiveWaitRequest, err error) proto.Message {
				switch {
				case errors.Is(err, serverapi.ErrRuntimeNoActiveRun):
					return &runtimepb.LiveWaitError{Code: "no_active_run", Detail: &runtimepb.LiveWaitError_NoActiveRun{NoActiveRun: &emptypb.Empty{}}}
				case errors.Is(err, serverapi.ErrRuntimeNoFinalAnswer):
					return &runtimepb.LiveWaitError{Code: "no_final_answer", Detail: &runtimepb.LiveWaitError_NoFinalAnswer{NoFinalAnswer: &emptypb.Empty{}}}
				}
				return nil
			}),
		registerRuntimeControlUnary(bindings, GatewayDependencies.RuntimeLiveControlClient, promptpb.File_kent_api_prompt_prompt_proto.Services().ByName("LiveWatchService"), "Watch",
			func() *promptpb.LiveWatchRequest { return &promptpb.LiveWatchRequest{} },
			apicontract.RuntimeLiveControlService.LiveWatch,
			func(_ *promptpb.LiveWatchRequest, err error) proto.Message {
				switch {
				case errors.Is(err, serverapi.ErrRuntimeNoActiveRun):
					return &promptpb.LiveWatchError{Code: "no_active_run", Detail: &promptpb.LiveWatchError_NoActiveRun{NoActiveRun: &emptypb.Empty{}}}
				case errors.Is(err, serverapi.ErrStreamFailed):
					return &promptpb.LiveWatchError{Code: "stream_failed", Detail: &promptpb.LiveWatchError_StreamFailed{StreamFailed: binaryInternalFailure(err)}}
				default:
					// Both stream failure and internal failure carry the same detail message.
					// Select the generated error union explicitly.
					if !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
						return &promptpb.LiveWatchError{Code: "internal_failure", Detail: &promptpb.LiveWatchError_InternalFailure{InternalFailure: binaryInternalFailure(err)}}
					}
				}
				return nil
			}),
	)
}
