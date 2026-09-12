package transcriptrender

import (
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
)

func TestThinkingUpdateIsDetailOnlyAndNonExpandable(t *testing.T) {
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:         transcriptpb.NoticeReason_NOTICE_REASON_THINKING_UPDATE,
			Severity:       transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			ThinkingEffort: textutil.Value("high"),
		}},
	}
	if rendered := RenderCommittedRow(row, 80, "dark", ModeOngoing); len(rendered.Lines) != 0 {
		t.Fatal("Thinking update must be hidden in ongoing mode")
	}
	for _, width := range []int{12, 80} {
		detail := RenderDetailPresentation(row, width, "dark")
		if len(detail.Collapsed) == 0 || detail.Expandable {
			t.Fatalf("Thinking update must remain visible and non-expandable at width %d", width)
		}
	}
}
