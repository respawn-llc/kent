package registry

import (
	"core/server/runtimeview"
	shelltool "core/server/tools/shell"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func (r *RuntimeRegistry) backgroundActivitiesForSession(sessionID string) ([]*transcriptpb.BackgroundActivity, error) {
	if r == nil || r.backgroundProcessSnapshots == nil {
		return nil, nil
	}
	return transcriptBackgroundActivitiesFromProcessSnapshots(sessionID, r.backgroundProcessSnapshots())
}

func transcriptBackgroundActivitiesFromProcessSnapshots(
	sessionID string,
	snapshots []shelltool.Snapshot,
) ([]*transcriptpb.BackgroundActivity, error) {
	return runtimeview.TranscriptBackgroundActivitiesFromProcessSnapshots(sessionID, snapshots)
}
