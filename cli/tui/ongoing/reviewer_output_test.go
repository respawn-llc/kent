package ongoing

import (
	"strings"
	"testing"

	"core/cli/tui/transcriptrender"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
)

func TestVerboseReviewerSuggestionsRenderFullyInOngoingMode(t *testing.T) {
	suggestions := []string{
		"Preserve the first complete supervisor suggestion across narrow terminal widths.",
		"Preserve the second complete supervisor suggestion without an ellipsis.",
	}
	stepID, err := runtimeids.ParseStepID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("parse step id: %v", err)
	}
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_ReviewerFeedback{ReviewerFeedback: &transcriptpb.ReviewerFeedbackRow{
			Id:              runtimeids.NewReviewerFeedbackID().String(),
			StepId:          stepID.String(),
			Suggestions:     suggestions,
			SuggestionCount: int32(len(suggestions)),
		}},
	}

	if got := ongoingRenderMode(row); got != transcriptrender.ModeOngoingFull {
		t.Fatalf("reviewer suggestions render mode = %d, want full ongoing mode", got)
	}
	rendered := transcriptrender.RenderCommittedRowWithLinkPresentation(
		row,
		24,
		"dark",
		ongoingRenderMode(row),
		transcriptrender.MarkdownLinkLabelOnly,
	)
	var renderedLines []string
	for _, line := range rendered.Lines {
		var text string
		for _, span := range line.Spans {
			text += span.Text
		}
		renderedLines = append(renderedLines, text)
	}
	text := strings.Join(renderedLines, "")
	compactText := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, text)
	for _, suggestion := range suggestions {
		compactSuggestion := strings.ReplaceAll(suggestion, " ", "")
		if !strings.Contains(compactText, compactSuggestion) {
			t.Fatalf("verbose reviewer suggestions omitted content: %q", text)
		}
	}
	if strings.Contains(text, "…") {
		t.Fatalf("verbose reviewer suggestions were ellipsized: %q", text)
	}
}

func TestAgentSteerRenderFullyInOngoingMode(t *testing.T) {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_AGENT_STEER
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			MessageType: &messageType,
			Diagnostic:  &transcriptpb.Diagnostic{Detail: "first\nsecond"},
		}},
	}
	if got := ongoingRenderMode(row); got != transcriptrender.ModeOngoingFull {
		t.Fatalf("agent steer render mode = %d, want full ongoing mode", got)
	}
	rendered := transcriptrender.RenderCommittedRowWithLinkPresentation(
		row,
		80,
		"dark",
		ongoingRenderMode(row),
		transcriptrender.MarkdownLinkLabelOnly,
	)
	if got, want := len(rendered.Lines), 2; got != want {
		t.Fatalf("agent steer ongoing rows = %d, want %d", got, want)
	}
}

func reviewerNoticeRow(visibility transcriptpb.EntryVisibility, code, detail string) *transcriptpb.CommittedRow {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_REVIEWER_FEEDBACK
	return &transcriptpb.CommittedRow{
		Visibility: visibility,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:      transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
			Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			MessageType: &messageType,
			Diagnostic: &transcriptpb.Diagnostic{
				Code:   string(code),
				Detail: detail,
			},
		}},
	}
}
