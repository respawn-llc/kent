package transcriptrender

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"testing"
)

func TestAgentSteerNoticeUsesFullOngoingAndDetailExpansion(t *testing.T) {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_AGENT_STEER
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			MessageType:  &messageType,
			CompactLabel: stringPtr("compact"),
			Diagnostic:   &transcriptpb.Diagnostic{Detail: "full"},
		}},
	}
	ongoing := RenderCommittedRow(row, 80, "dark", ModeOngoing)
	if PlainLines(ongoing.Lines)[0] == "compact" {
		t.Fatal("ongoing agent steer used collapsed content")
	}
	collapsed := RenderCommittedRow(row, 80, "dark", ModeDetailCollapsed)
	if PlainLines(collapsed.Lines)[0] == "full" {
		t.Fatal("collapsed detail agent steer used full content")
	}
	expanded := RenderCommittedRow(row, 80, "dark", ModeDetailExpanded)
	if PlainLines(expanded.Lines)[0] == "compact" {
		t.Fatal("expanded detail agent steer used compact content")
	}
}

func stringPtr(value string) *string {
	return &value
}
