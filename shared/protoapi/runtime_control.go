package protoapi

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcriptmodel"
)

var appendVisibilities = map[transcriptpb.AppendVisibility]transcript.EntryVisibility{
	transcriptpb.AppendVisibility_APPEND_VISIBILITY_AUTO:              transcript.EntryVisibilityAuto,
	transcriptpb.AppendVisibility_APPEND_VISIBILITY_ONGOING:           transcript.EntryVisibilityOngoing,
	transcriptpb.AppendVisibility_APPEND_VISIBILITY_ONGOING_COLLAPSED: transcript.EntryVisibilityOngoingCollapsed,
	transcriptpb.AppendVisibility_APPEND_VISIBILITY_DETAIL:            transcript.EntryVisibilityDetail,
	transcriptpb.AppendVisibility_APPEND_VISIBILITY_HIDDEN:            transcript.EntryVisibilityHidden,
}

func AppendVisibilityFromProto(value *transcriptpb.AppendVisibility) transcript.EntryVisibility {
	if value == nil {
		return transcript.EntryVisibilityAuto
	}
	return appendVisibilities[*value]
}
