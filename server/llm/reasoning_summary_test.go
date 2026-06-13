package llm

import (
	"testing"

	"core/shared/textutil"
)

func TestNormalizeReasoningSummaryTextPreservesBoldMarkers(t *testing.T) {
	text := normalizeReasoningSummaryLines(textutil.SplitLinesCRLF("**Preparing patch**\n\nI am exploring options.\n**Running checks**"))
	if text != "**Preparing patch**\n\nI am exploring options.\n**Running checks**" {
		t.Fatalf("unexpected normalized text: %q", text)
	}
}

func TestReasoningSummaryDeltaFromTextDoesNotInferStatus(t *testing.T) {
	delta := reasoningSummaryDeltaFromText("rs_1:summary:0", "reasoning", "**First status**\n\n`literal` details\n\n**Second status**\nMore details")
	if delta.Text != "**First status**\n\n`literal` details\n\n**Second status**\nMore details" {
		t.Fatalf("unexpected delta text: %q", delta.Text)
	}
}

func TestNormalizeReasoningEntriesKeepsBoldOnlyReasoningEntries(t *testing.T) {
	got := normalizeReasoningEntries([]ReasoningEntry{{Role: "reasoning", Text: "**Preparing patch**"}})
	if len(got) != 1 || got[0].Text != "**Preparing patch**" {
		t.Fatalf("expected bold-only reasoning entry preserved, got %+v", got)
	}
}
