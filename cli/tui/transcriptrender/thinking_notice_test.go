package transcriptrender

import (
	"testing"

	"core/shared/clientui"
	"core/shared/textutil"
	"core/shared/transcript"
)

func TestThinkingUpdateIsDetailOnlyAndNonExpandable(t *testing.T) {
	row := clientui.TranscriptCommittedRow{
		Visibility: transcript.EntryVisibilityDetail,
		Integrity:  transcript.RowIntegrityValid,
		Kind:       clientui.TranscriptRowNotice,
		Notice: &clientui.TranscriptNoticeRow{
			Reason:         clientui.TranscriptNoticeThinkingUpdate,
			Severity:       clientui.TranscriptNoticeInfo,
			ThinkingEffort: textutil.Value("high"),
		},
	}
	if err := row.Notice.Validate(); err != nil {
		t.Fatal(err)
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
