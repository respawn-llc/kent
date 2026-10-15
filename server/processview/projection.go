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
	var exitCode *int64
	if snapshot.ExitCode != nil {
		value := int64(*snapshot.ExitCode)
		exitCode = &value
	}
	result := &processpb.BackgroundProcess{
		Id:             snapshot.ID,
		OwnerSessionId: snapshot.OwnerSessionID,
		OwnerStepId:    textutil.OptionalExactString(snapshot.OwnerStepID),
		State:          snapshot.State,
		Command:        snapshot.Command,
		Workdir:        snapshot.Workdir,
		StartedAt:      timestamppb.New(snapshot.StartedAt),
		ExitCode:       exitCode,
		LogPath:        snapshot.LogPath,
		RecentOutput:   strings.ToValidUTF8(snapshot.RecentOutput, "\uFFFD"),
		Running:        snapshot.Running,
		StdinOpen:      snapshot.StdinOpen,
		Backgrounded:   snapshot.Backgrounded,
		KillRequested:  snapshot.KillRequested,
		LastUpdatedAt:  timestamppb.New(snapshot.LastUpdatedAt),
	}
	if !snapshot.FinishedAt.IsZero() {
		result.FinishedAt = timestamppb.New(snapshot.FinishedAt)
	}
	return result, protoapi.Validate(result)
}
