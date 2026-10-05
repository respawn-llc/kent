package protoapi

import (
	"fmt"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcript"
)

func AppendVisibilityFromProto(value *transcriptpb.AppendVisibility) (transcript.EntryVisibility, error) {
	if value == nil {
		return transcript.EntryVisibilityAuto, nil
	}
	switch *value {
	case transcriptpb.AppendVisibility_APPEND_VISIBILITY_AUTO:
		return transcript.EntryVisibilityAuto, nil
	case transcriptpb.AppendVisibility_APPEND_VISIBILITY_ONGOING:
		return transcript.EntryVisibilityOngoing, nil
	case transcriptpb.AppendVisibility_APPEND_VISIBILITY_ONGOING_COLLAPSED:
		return transcript.EntryVisibilityOngoingCollapsed, nil
	case transcriptpb.AppendVisibility_APPEND_VISIBILITY_DETAIL:
		return transcript.EntryVisibilityDetail, nil
	case transcriptpb.AppendVisibility_APPEND_VISIBILITY_HIDDEN:
		return transcript.EntryVisibilityHidden, nil
	default:
		return "", fmt.Errorf("invalid append visibility %v", *value)
	}
}
