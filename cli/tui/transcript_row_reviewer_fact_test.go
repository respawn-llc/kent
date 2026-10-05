package tui

import (
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func TestTranscriptCommittedRowEqualDetectsReviewerFactMutations(t *testing.T) {
	stepID, err := runtimeids.ParseStepID(uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	base := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Row: &transcriptpb.CommittedRow_ReviewerFeedback{ReviewerFeedback: &transcriptpb.ReviewerFeedbackRow{
			Id:              runtimeids.NewReviewerFeedbackID().String(),
			StepId:          stepID.String(),
			Suggestions:     []string{"one", "two"},
			SuggestionCount: 2,
		}},
	}
	feedbackMutations := []struct {
		name string
		edit func(*transcriptpb.ReviewerFeedbackRow)
	}{
		{"id", func(row *transcriptpb.ReviewerFeedbackRow) { row.Id = runtimeids.NewReviewerFeedbackID().String() }},
		{"step", func(row *transcriptpb.ReviewerFeedbackRow) {
			row.StepId = uuid.NewString()
		}},
		{"suggestions", func(row *transcriptpb.ReviewerFeedbackRow) { row.Suggestions[1] = "changed" }},
		{"count", func(row *transcriptpb.ReviewerFeedbackRow) { row.SuggestionCount++ }},
	}
	for _, mutation := range feedbackMutations {
		t.Run("feedback "+mutation.name, func(t *testing.T) {
			changed := proto.Clone(base).(*transcriptpb.CommittedRow)
			mutation.edit(changed.GetReviewerFeedback())
			if TranscriptCommittedRowEqual(base, changed) {
				t.Fatalf("Reviewer feedback %s mutation was considered equal", mutation.name)
			}
		})
	}

	errorBase := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Row: &transcriptpb.CommittedRow_ReviewerError{ReviewerError: &transcriptpb.ReviewerErrorRow{
			Id:     runtimeids.NewReviewerErrorID().String(),
			StepId: stepID.String(),
			Detail: "detail",
		}},
	}
	errorMutations := []struct {
		name string
		edit func(*transcriptpb.ReviewerErrorRow)
	}{
		{"id", func(row *transcriptpb.ReviewerErrorRow) { row.Id = runtimeids.NewReviewerErrorID().String() }},
		{"step", func(row *transcriptpb.ReviewerErrorRow) {
			row.StepId = uuid.NewString()
		}},
		{"detail", func(row *transcriptpb.ReviewerErrorRow) { row.Detail = "changed" }},
	}
	for _, mutation := range errorMutations {
		t.Run("error "+mutation.name, func(t *testing.T) {
			changed := proto.Clone(errorBase).(*transcriptpb.CommittedRow)
			mutation.edit(changed.GetReviewerError())
			if TranscriptCommittedRowEqual(errorBase, changed) {
				t.Fatalf("Reviewer error %s mutation was considered equal", mutation.name)
			}
		})
	}
}
