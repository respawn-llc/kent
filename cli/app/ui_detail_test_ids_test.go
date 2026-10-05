package app

import (
	"core/cli/tui"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"google.golang.org/protobuf/proto"
	"hash/fnv"
	"slices"
	"testing"
)

const (
	detailTestSessionID            = "58e121b5-30f7-4d0f-a1fa-fb3e6695e39c"
	detailTestReplacementSessionID = "2fd85f0b-70fe-4d0d-8dfc-946d895502f8"
	detailTestStaleSessionID       = "d09bb9c6-5a8a-4634-b39c-79b0e2132fa7"
)

func detailTestUserRow(text string) *transcriptpb.CommittedRow {
	return &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    detailTestLocator("user:" + text), Row: &transcriptpb.CommittedRow_User{User: &transcriptpb.UserRow{Text: text}}}
}

func detailTestAssistantRow(text string) *transcriptpb.CommittedRow {
	return &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    detailTestLocator("assistant:" + text),
		Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
			StepId: ongoingTestStepID().String(),
			Phase:  transcriptpb.AssistantPhase_ASSISTANT_PHASE_FINAL,
			Text:   text,
		}},
	}
}

func detailTestToolRow(tool *transcriptpb.ToolRow) *transcriptpb.CommittedRow {
	return &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Locator:    detailTestLocator("tool:" + string(tool.GetToolCallId()) + ":" + tool.Text), Row: &transcriptpb.CommittedRow_Tool{Tool: tool}}
}

func detailTestLocator(value string) *transcriptpb.CommittedRowLocator {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	sequence := int64(hash.Sum64() & (uint64(1<<53) - 1))
	if sequence == 0 {
		sequence = 1
	}
	return &transcriptpb.CommittedRowLocator{EventSequence: sequence, RowOrdinal: 1}
}

func detailTestRowsEqual(left, right []*transcriptpb.CommittedRow) bool {
	return slices.EqualFunc(left, right, tui.TranscriptCommittedRowEqual)
}

func TestDetailTranscriptRowEqualityIncludesLocator(t *testing.T) {
	left := detailTestUserRow("same content")
	right := proto.CloneOf(left)
	right.Locator.RowOrdinal++

	if !tui.TranscriptCommittedRowEqual(left, left) {
		t.Fatal("a transcript row was not equal to itself")
	}
	if tui.TranscriptCommittedRowEqual(left, right) {
		t.Fatalf("rows with different locators were treated as equal: left=%+v right=%+v", left.Locator, right.Locator)
	}
}
