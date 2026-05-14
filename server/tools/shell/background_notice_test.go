package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeBackgroundEventDefaultDoesNotDuplicateShortLogAroundTruncationBoundary(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "1000.log")
	content := strings.Repeat("x", 543)
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	exitCode := 17
	summary := SummarizeBackgroundEvent(Event{
		Type: EventCompleted,
		Snapshot: Snapshot{
			ID:       "1000",
			State:    "completed",
			LogPath:  logPath,
			ExitCode: &exitCode,
		},
	}, BackgroundNoticeOptions{MaxChars: 80, SuccessOutputMode: BackgroundOutputDefault})

	if !summary.Truncated {
		t.Fatal("expected truncated summary")
	}
	if strings.Contains(summary.DetailText, "omitted -") {
		t.Fatalf("did not expect negative omitted bytes, got %q", summary.DetailText)
	}
	if strings.Count(summary.DetailText, content) > 0 {
		t.Fatalf("did not expect full content duplicated in summary, got %q", summary.DetailText)
	}
	headLen, tailLen := truncationSegmentLengths(len(content), 80)
	wantMax := headLen + tailLen + backgroundTruncationBannerLen(len(content)-headLen-tailLen)
	_, preview, ok := strings.Cut(summary.DetailText, "Output:\n")
	if !ok {
		t.Fatalf("expected output section in summary, got %q", summary.DetailText)
	}
	if got := len(preview); got > wantMax {
		t.Fatalf("expected bounded preview <= %d bytes, got %d", wantMax, got)
	}
	if len(preview) >= len(content) {
		t.Fatalf("expected truncated preview smaller than content, got preview=%d content=%d", len(preview), len(content))
	}
}

func TestSummarizeBackgroundEventPreservesRawAnsi(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "1000.log")
	content := "\x1b[31mred\x1b[0m\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	exitCode := 0
	summary := SummarizeBackgroundEvent(Event{
		Type: EventCompleted,
		Snapshot: Snapshot{
			ID:        "1000",
			State:     "completed",
			LogPath:   logPath,
			ExitCode:  &exitCode,
			RawOutput: true,
		},
	}, BackgroundNoticeOptions{MaxChars: 80, SuccessOutputMode: BackgroundOutputDefault})

	if !strings.Contains(summary.DetailText, content) {
		t.Fatalf("raw ANSI missing from summary: %q", summary.DetailText)
	}
}
