package app

import (
	"fmt"
	"testing"

	"core/cli/tui"
)

func benchmarkNativeReplayEntries(count int) []tui.TranscriptEntry {
	entries := make([]tui.TranscriptEntry, 0, count)
	for index := 0; index < count; index++ {
		entries = append(entries, tui.TranscriptEntry{
			Role: "assistant",
			Text: fmt.Sprintf("message %d\n```go\nfmt.Println(\"%d\")\n```\n- item a\n- item b", index, index),
		})
	}
	return entries
}

func BenchmarkRenderNativeScrollbackSnapshot(b *testing.B) {
	entries := benchmarkNativeReplayEntries(1200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = renderStyledNativeProjectionLines(tui.ProjectCommittedOngoingTranscript(entries, "dark", 120).Lines(tui.TranscriptDivider), "dark", 120)
	}
}
