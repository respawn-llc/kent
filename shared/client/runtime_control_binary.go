package client

import (
	"context"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type runtimeControlFailure interface {
	GetCode() string
}

func runtimeControlGeneratedError(failure runtimeControlFailure) error {
	if value, ok := failure.(interface {
		GetRuntimeUnavailable() *runtimepb.RuntimeUnavailableDetails
	}); ok && value.GetRuntimeUnavailable() != nil {
		return serverapi.ErrRuntimeUnavailable
	}
	if value, ok := failure.(interface {
		GetRuntimeCommandNotAccepted() *runtimepb.RuntimeCommandNotAcceptedDetails
	}); ok && value.GetRuntimeCommandNotAccepted() != nil {
		return protoapi.RuntimeCommandNotAcceptedFromProto(value.GetRuntimeCommandNotAccepted())
	}
	if value, ok := failure.(interface {
		GetPendingWorkNotPending() *runtimepb.PendingWorkNotPendingDetails
	}); ok && value.GetPendingWorkNotPending() != nil {
		id, err := runtimeids.ParseQueueItemID(value.GetPendingWorkNotPending().ItemId)
		if err != nil {
			return err
		}
		return &serverapi.PendingWorkNotPendingError{ItemID: id}
	}
	if value, ok := failure.(interface {
		GetPromptCatalogRead() *promptcommandpb.CatalogReadDetails
	}); ok && value.GetPromptCatalogRead() != nil {
		return protoapi.PromptCommandErrorFromProto(value.GetPromptCatalogRead())
	}
	if value, ok := failure.(interface {
		GetPromptCommandNotFound() *promptcommandpb.CommandNotFoundDetails
	}); ok && value.GetPromptCommandNotFound() != nil {
		return protoapi.PromptCommandErrorFromProto(value.GetPromptCommandNotFound())
	}
	if value, ok := failure.(interface {
		GetPromptCommandRead() *promptcommandpb.CommandReadDetails
	}); ok && value.GetPromptCommandRead() != nil {
		return protoapi.PromptCommandErrorFromProto(value.GetPromptCommandRead())
	}
	switch failure.GetCode() {
	case "manual_compaction_too_soon":
		return serverapi.ErrManualCompactionTooSoon
	case "manual_compaction_disabled":
		return serverapi.ErrManualCompactionDisabled
	case "manual_compaction_active":
		return serverapi.ErrManualCompactionActive
	default:
		return generatedOperationFailure(failure.GetCode())
	}
}

func (c *Remote) SetSessionName(ctx context.Context, request *runtimepb.SetSessionNameRequest) error {
	_, err := callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "SettingsService", "SetSessionName"), request, &runtimepb.SetSessionNameResult{},
		func(failure *runtimepb.SetSessionNameError) error { return runtimeControlGeneratedError(failure) })
	return err
}

func (c *Remote) AppendCommittedEntry(ctx context.Context, request *transcriptpb.AppendCommittedEntryRequest) error {
	_, err := callGeneratedBinary(c, ctx, bootstrapMethod(transcriptpb.File_kent_api_transcript_transcript_proto, "AppendService", "AppendCommittedEntry"), request, &transcriptpb.AppendCommittedEntryResult{},
		func(failure *transcriptpb.AppendCommittedEntryError) error {
			return runtimeControlGeneratedError(failure)
		})
	return err
}

func (c *Remote) ShouldCompactBeforeUserMessage(ctx context.Context, request *runtimepb.ShouldCompactRequest) (*runtimepb.ShouldCompactSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "TurnService", "ShouldCompactBeforeUserMessage"), request, &runtimepb.ShouldCompactResult{},
		func(failure *runtimepb.ShouldCompactError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) SubmitUserTurn(ctx context.Context, request *runtimepb.SubmitUserTurnRequest) (*runtimepb.SubmitUserTurnSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "TurnService", "SubmitUserTurn"), request, &runtimepb.SubmitUserTurnResult{},
		func(failure *runtimepb.SubmitUserTurnError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) SubmitUserShellCommand(ctx context.Context, request *runtimepb.ShellCommandRequest) error {
	_, err := callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "TurnService", "SubmitUserShellCommand"), request, &runtimepb.SubmitUserShellCommandResult{},
		func(failure *runtimepb.SubmitUserShellCommandError) error {
			return runtimeControlGeneratedError(failure)
		})
	return err
}

func (c *Remote) CompactContext(ctx context.Context, request *runtimepb.CompactContextRequest) error {
	_, err := callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "TurnService", "CompactContext"), request, &runtimepb.CompactContextResult{},
		func(failure *runtimepb.CompactContextError) error { return runtimeControlGeneratedError(failure) })
	return err
}

func (c *Remote) Interrupt(ctx context.Context, request *runtimepb.InterruptRequest) (*runtimepb.ReadModelUpdate, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "TurnService", "Interrupt"), request, &runtimepb.InterruptResult{},
		func(failure *runtimepb.InterruptError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) ListPendingWork(ctx context.Context, request *runtimepb.ListPendingWorkRequest) (*runtimepb.ListPendingWorkSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "TurnService", "ListPendingWork"), request, &runtimepb.ListPendingWorkResult{},
		func(failure *runtimepb.ListPendingWorkError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) RemovePendingWork(ctx context.Context, request *runtimepb.RemovePendingWorkRequest) (*runtimepb.RemovePendingWorkSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "TurnService", "RemovePendingWork"), request, &runtimepb.RemovePendingWorkResult{},
		func(failure *runtimepb.RemovePendingWorkError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) RecordPromptHistory(ctx context.Context, request *promptpb.RecordHistoryRequest) error {
	_, err := callGeneratedBinary(c, ctx, bootstrapMethod(promptpb.File_kent_api_prompt_prompt_proto, "HistoryService", "Record"), request, &promptpb.RecordHistoryResult{},
		func(failure *promptpb.RecordHistoryError) error { return runtimeControlGeneratedError(failure) })
	return err
}

func (c *Remote) ShowGoal(ctx context.Context, request *runtimepb.GoalShowRequest) (*runtimepb.GoalShowSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Show"), request, &runtimepb.GoalShowResult{},
		func(failure *runtimepb.GoalShowError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) SetGoal(ctx context.Context, request *runtimepb.GoalSetRequest) (*runtimepb.GoalMutationSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Set"), request, &runtimepb.GoalSetResult{},
		func(failure *runtimepb.GoalSetError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) PauseGoal(ctx context.Context, request *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Pause"), request, &runtimepb.GoalPauseResult{},
		func(failure *runtimepb.GoalPauseError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) ResumeGoal(ctx context.Context, request *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Resume"), request, &runtimepb.GoalResumeResult{},
		func(failure *runtimepb.GoalResumeError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) CompleteGoal(ctx context.Context, request *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Complete"), request, &runtimepb.GoalCompleteResult{},
		func(failure *runtimepb.GoalCompleteError) error { return runtimeControlGeneratedError(failure) })
}

func (c *Remote) ClearGoal(ctx context.Context, request *runtimepb.GoalClearRequest) (*runtimepb.GoalMutationSuccess, error) {
	return callGeneratedBinary(c, ctx, bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Clear"), request, &runtimepb.GoalClearResult{},
		func(failure *runtimepb.GoalClearError) error { return runtimeControlGeneratedError(failure) })
}
