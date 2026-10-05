package clientui_test

import (
	"testing"

	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
)

const reviewerTestStepID = "22222222-2222-4222-8222-222222222222"

func TestReviewerRowsValidateTheirTypedPayloads(t *testing.T) {
	feedback := &transcriptpb.ReviewerFeedbackRow{
		Id:              runtimeids.NewReviewerFeedbackID().String(),
		StepId:          reviewerTestStepID,
		Suggestions:     []string{"  **first**\n\n- item  ", "second"},
		SuggestionCount: 2,
	}
	feedbackRow := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 1, RowOrdinal: 1},
		Row:        &transcriptpb.CommittedRow_ReviewerFeedback{ReviewerFeedback: feedback},
	}
	if err := protoapi.Validate(feedbackRow); err != nil {
		t.Fatalf("validate Reviewer feedback row: %v", err)
	}

	errorRow := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 2, RowOrdinal: 1},
		Row: &transcriptpb.CommittedRow_ReviewerError{ReviewerError: &transcriptpb.ReviewerErrorRow{
			Id:     runtimeids.NewReviewerErrorID().String(),
			StepId: reviewerTestStepID,
			Detail: "raw failure detail",
		}},
	}
	if err := protoapi.Validate(errorRow); err != nil {
		t.Fatalf("validate Reviewer error row: %v", err)
	}
}

func TestReviewerRowsRejectMissingOrInconsistentFacts(t *testing.T) {
	validFeedback := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 3, RowOrdinal: 1},
		Row: &transcriptpb.CommittedRow_ReviewerFeedback{ReviewerFeedback: &transcriptpb.ReviewerFeedbackRow{
			Id:              runtimeids.NewReviewerFeedbackID().String(),
			StepId:          reviewerTestStepID,
			Suggestions:     []string{"suggestion"},
			SuggestionCount: 1,
		}},
	}
	cloneValidFeedback := func() *transcriptpb.CommittedRow {
		return proto.Clone(validFeedback).(*transcriptpb.CommittedRow)
	}
	tests := []*transcriptpb.CommittedRow{
		{Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED, Integrity: transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID, Locator: &transcriptpb.CommittedRowLocator{EventSequence: 5, RowOrdinal: 1}},
		func() *transcriptpb.CommittedRow {
			row := cloneValidFeedback()
			row.GetReviewerFeedback().Id = ""
			return row
		}(),
		func() *transcriptpb.CommittedRow {
			row := cloneValidFeedback()
			row.GetReviewerFeedback().Suggestions = nil
			return row
		}(),
		func() *transcriptpb.CommittedRow {
			row := cloneValidFeedback()
			row.GetReviewerFeedback().Suggestions = []string{" \t "}
			return row
		}(),
		func() *transcriptpb.CommittedRow {
			row := cloneValidFeedback()
			row.GetReviewerFeedback().SuggestionCount = 2
			return row
		}(),
		{
			Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
			Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
			Locator:    &transcriptpb.CommittedRowLocator{EventSequence: 4, RowOrdinal: 1},
			Row: &transcriptpb.CommittedRow_ReviewerError{ReviewerError: &transcriptpb.ReviewerErrorRow{
				Id:     runtimeids.NewReviewerErrorID().String(),
				StepId: reviewerTestStepID,
			}},
		},
	}
	for _, row := range tests {
		if err := protoapi.Validate(row); err == nil {
			t.Fatalf("accepted invalid Reviewer row: %#v", row)
		}
	}
}
