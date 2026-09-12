package tui

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	"google.golang.org/protobuf/proto"
)

func TranscriptCommittedRowEqual(left, right *transcriptpb.CommittedRow) bool {
	return proto.Equal(left, right)
}
