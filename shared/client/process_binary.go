package client

import (
	"context"

	processpb "core/shared/protoapi/gen/kent/api/process"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (c *Remote) ListProcesses(ctx context.Context, request *processpb.ListRequest) (*processpb.ListSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(processpb.File_kent_api_process_process_proto, "ViewService", "List"),
		request, &processpb.ListResult{}, func(failure *processpb.ListError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) GetProcess(ctx context.Context, request *processpb.GetRequest) (*processpb.GetSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(processpb.File_kent_api_process_process_proto, "ViewService", "Get"),
		request, &processpb.GetResult{}, func(failure *processpb.GetError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) KillProcess(ctx context.Context, request *processpb.KillRequest) (*emptypb.Empty, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(processpb.File_kent_api_process_process_proto, "ControlService", "Kill"),
		request, &processpb.KillResult{}, func(failure *processpb.KillError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}

func (c *Remote) GetInlineOutput(ctx context.Context, request *processpb.InlineOutputRequest) (*processpb.InlineOutputSuccess, error) {
	return callGeneratedBinary(c, ctx,
		bootstrapMethod(processpb.File_kent_api_process_process_proto, "ControlService", "InlineOutput"),
		request, &processpb.InlineOutputResult{}, func(failure *processpb.InlineOutputError) error {
			return projectInternalGeneratedError(failure.Code, failure.GetInternalFailure())
		})
}
