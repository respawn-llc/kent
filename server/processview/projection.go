package processview

import (
	"strings"

	shelltool "core/server/tools/shell"
	"core/shared/protoapi"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ProcessFromSnapshot(snapshot shelltool.Snapshot) (*processpb.BackgroundProcess, error) {
	var exitCode *int32
	if snapshot.ExitCode != nil {
		value, err := protoapi.Int32(*snapshot.ExitCode, "process exit code")
		if err != nil {
			return nil, err
		}
		exitCode = &value
	}
	result := &processpb.BackgroundProcess{
		Id:                      snapshot.ID,
		OwnerSessionId:          snapshot.OwnerSessionID,
		OwnerRunId:              textutil.OptionalExactString(snapshot.OwnerRunID),
		OwnerStepId:             textutil.OptionalExactString(snapshot.OwnerStepID),
		State:                   snapshot.State,
		Command:                 snapshot.Command,
		Workdir:                 snapshot.Workdir,
		StartedAt:               timestamppb.New(snapshot.StartedAt),
		ExitCode:                exitCode,
		LogPath:                 snapshot.LogPath,
		RecentOutput:            strings.ToValidUTF8(snapshot.RecentOutput, "\uFFFD"),
		OutputAvailable:         snapshot.OutputAvailable,
		OutputRetainedFromBytes: snapshot.OutputRetainedFromBytes,
		OutputRetainedToBytes:   snapshot.OutputRetainedToBytes,
		Running:                 snapshot.Running,
		StdinOpen:               snapshot.StdinOpen,
		Backgrounded:            snapshot.Backgrounded,
		KillRequested:           snapshot.KillRequested,
		LastUpdatedAt:           timestamppb.New(snapshot.LastUpdatedAt),
	}
	if !snapshot.FinishedAt.IsZero() {
		result.FinishedAt = timestamppb.New(snapshot.FinishedAt)
	}
	return result, protoapi.Validate(result)
}
