package transcriptrender

import (
	"reflect"
	"strings"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func TestTypedReviewerFeedbackRendersCountCollapsedAndMarkdownExpanded(t *testing.T) {
	stepID, err := runtimeids.ParseStepID(uuid.NewString())
	if err != nil {
		t.Fatalf("parse step id: %v", err)
	}
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_ReviewerFeedback{ReviewerFeedback: &transcriptpb.ReviewerFeedbackRow{
			Id:              runtimeids.NewReviewerFeedbackID().String(),
			StepId:          stepID.String(),
			Suggestions:     []string{"  **first**  ", "second\nline"},
			SuggestionCount: 2,
		}},
	}
	collapsed := RenderCommittedRow(row, 80, "dark", ModeOngoingCollapsed)
	expanded := RenderCommittedRow(row, 80, "dark", ModeDetailExpanded)
	if len(collapsed.Lines) == 0 || len(expanded.Lines) <= len(collapsed.Lines) {
		t.Fatalf("typed feedback presentation did not expand: collapsed=%+v expanded=%+v", collapsed, expanded)
	}
	if expanded.Group != GroupReviewerFeedback {
		t.Fatalf("typed feedback group = %q", expanded.Group)
	}
	if reflect.DeepEqual(collapsed.Lines, RenderCommittedRow(row, 80, "dark", ModeDetailExpanded).Lines) {
		t.Fatal("collapsed feedback unexpectedly rendered the full source")
	}
	countChanged := proto.Clone(row).(*transcriptpb.CommittedRow)
	countChanged.GetReviewerFeedback().SuggestionCount++
	if reflect.DeepEqual(collapsed.Lines, RenderCommittedRow(countChanged, 80, "dark", ModeOngoingCollapsed).Lines) {
		t.Fatal("collapsed feedback ignored the structured suggestion count")
	}
	sourceChanged := proto.Clone(row).(*transcriptpb.CommittedRow)
	sourceChanged.GetReviewerFeedback().Suggestions[0] = "different source"
	if reflect.DeepEqual(expanded.Lines, RenderCommittedRow(sourceChanged, 80, "dark", ModeDetailExpanded).Lines) {
		t.Fatal("expanded feedback ignored the persisted suggestion source")
	}
}

func TestTypedReviewerErrorRendersExpandedDiagnostic(t *testing.T) {
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_ReviewerError{ReviewerError: &transcriptpb.ReviewerErrorRow{
			Id:     runtimeids.NewReviewerErrorID().String(),
			StepId: mustReviewerStepID(t).String(),
			Detail: "raw failure detail",
		}},
	}
	rendered := RenderCommittedRow(row, 80, "dark", ModeOngoing)
	if len(rendered.Lines) == 0 || rendered.Group != GroupReviewerError {
		t.Fatalf("typed Reviewer error presentation = %+v", rendered)
	}
	var renderedDetail string
	for _, span := range rendered.Lines[0].Spans {
		renderedDetail += span.Text
	}
	if strings.TrimSpace(renderedDetail) != strings.TrimSpace(row.GetReviewerError().Detail) {
		t.Fatalf("Reviewer error detail was not preserved: got %q", renderedDetail)
	}
}

func mustReviewerStepID(t *testing.T) runtimeids.StepID {
	t.Helper()
	stepID, err := runtimeids.ParseStepID(uuid.NewString())
	if err != nil {
		t.Fatalf("parse step id: %v", err)
	}
	return stepID
}
