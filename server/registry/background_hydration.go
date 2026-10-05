package registry

import (
	"core/server/runtimeview"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func (r *RuntimeRegistry) backgroundActivitiesForSession(sessionID string) ([]*transcriptpb.BackgroundActivity, error) {
	if r == nil || r.backgroundProcessSnapshots == nil {
		return nil, nil
	}
	return runtimeview.TranscriptBackgroundActivitiesFromProcessSnapshots(sessionID, r.backgroundProcessSnapshots())
}
