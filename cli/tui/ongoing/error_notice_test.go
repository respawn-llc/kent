package ongoing

import (
	"reflect"
	"strings"
	"testing"

	"core/cli/tui/transcriptrender"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcript"
)

func TestErrorNoticesRenderCompletelyThroughNormalOngoingModes(t *testing.T) {
	diagnosticDetail := "runtime diagnostic first line\nruntime diagnostic second line"
	legacyText := "legacy error first line\nlegacy error second line"
	misleadingCompact := "wrong compact source"
	misleadingCondensed := "wrong condensed source"
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ERROR_FEEDBACK
	tests := []struct {
		name       string
		visibility transcriptpb.EntryVisibility
		notice     *transcriptpb.NoticeRow
		wantMode   transcriptrender.Mode
		wantText   string
	}{
		{
			name:       "runtime diagnostic ongoing",
			visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
			notice: &transcriptpb.NoticeRow{
				Reason:        transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
				Severity:      transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				CompactLabel:  &misleadingCompact,
				CondensedText: &misleadingCondensed,
				Diagnostic: &transcriptpb.Diagnostic{
					Code:   string(transcript.EntryRoleDeveloperErrorFeedback),
					Detail: diagnosticDetail,
				},
			},
			wantMode: transcriptrender.ModeOngoing,
			wantText: diagnosticDetail,
		},
		{
			name:       "legacy error ongoing collapsed",
			visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
			notice: &transcriptpb.NoticeRow{
				Reason:        transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
				Severity:      transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				MessageType:   &messageType,
				LegacyText:    &legacyText,
				CompactLabel:  &misleadingCompact,
				CondensedText: &misleadingCondensed,
			},
			wantMode: transcriptrender.ModeOngoingCollapsed,
			wantText: legacyText,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := &transcriptpb.CommittedRow{
				Visibility: test.visibility,
				Row:        &transcriptpb.CommittedRow_Notice{Notice: test.notice},
			}
			if got := ongoingRenderMode(row); got != test.wantMode {
				t.Fatalf("normal ongoing render mode = %d, want %d", got, test.wantMode)
			}
			structured := transcriptrender.RenderCommittedRow(row, 80, "dark", test.wantMode)
			gotText := make([]string, len(structured.Lines))
			for index, line := range structured.Lines {
				for _, span := range line.Spans {
					if role, ok := span.Style.Role(); ok && role == transcriptrender.StyleRoleError {
						gotText[index] += span.Text
					}
				}
				gotText[index] = strings.TrimSpace(gotText[index])
			}
			if want := strings.Split(test.wantText, "\n"); !reflect.DeepEqual(gotText, want) {
				t.Fatalf("structured ongoing error payload = %#v, want %#v", gotText, want)
			}
			if got, want := NewSurface().renderCommittedRow(row, 80, "dark"), encodeTranscriptLines(structured.Lines, "dark"); !reflect.DeepEqual(got, want) {
				t.Fatalf("ongoing surface changed structured renderer output: got=%q want=%q", got, want)
			}
		})
	}
}
