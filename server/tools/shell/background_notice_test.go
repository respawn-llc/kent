package shell

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"core/server/tools/shell/postprocess"
)

func writeBackgroundNoticeLog(t *testing.T, content []byte) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "1000.log")
	if err := os.WriteFile(logPath, content, 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return logPath
}

func completedBackgroundSnapshot(logPath string, exitCode *int) Snapshot {
	return Snapshot{ID: "1000", State: "completed", LogPath: logPath, ExitCode: exitCode}
}

func TestBackgroundEventCompletionOutputApplicability(t *testing.T) {
	exitCode := 0
	snapshot := Snapshot{ID: "1000", State: "completed", ExitCode: &exitCode}

	backgrounded := newBackgroundedEvent(snapshot)
	if backgrounded.completion != nil {
		t.Fatal("background transition must not carry completion output")
	}

	finalized := newFinalizedBackgroundEvent(EventCompleted, snapshot, "", nil, false)
	if finalized.completion == nil {
		t.Fatal("completed event must carry completion output")
	}
	if finalized.completion.source != completionOutputFinalized {
		t.Fatalf("finalized event source = %d", finalized.completion.source)
	}

	fallback := newFallbackBackgroundEvent(EventKilled, snapshot, "preview", nil, 1, false)
	if fallback.completion == nil {
		t.Fatal("killed event must carry completion output")
	}
	if fallback.completion.source != completionOutputFallback {
		t.Fatalf("fallback event source = %d", fallback.completion.source)
	}

	_, err := SummarizeBackgroundEvent(Event{Type: EventCompleted, Snapshot: snapshot}, BackgroundNoticeOptions{})
	var invalid *InvalidBackgroundEventError
	if !errors.As(err, &invalid) {
		t.Fatalf("terminal event without completion output error = %v, want typed invalid event error", err)
	}
	if invalid.EventType != EventCompleted || invalid.ProcessID != snapshot.ID || invalid.State != snapshot.State {
		t.Fatalf("invalid event error = %+v", invalid)
	}
}

func TestBackgroundNoticeFinalizedOutputUsesCompletionContent(t *testing.T) {
	lines := strings.Repeat("line\n", 100)
	logPath := writeBackgroundNoticeLog(t, []byte(lines))
	exitCode := 0
	event := newFinalizedBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), "semantic replacement", nil, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{MaxChars: 80, SuccessOutputMode: BackgroundOutputDefault})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	preview, removed := summary.RuntimePreview()
	if preview != "semantic replacement" || removed != 0 {
		t.Fatalf("finalized completion preview = %q, removed = %d", preview, removed)
	}
	if !strings.Contains(summary.DetailText, "semantic replacement") || strings.Contains(summary.DetailText, logPath) {
		t.Fatalf("finalized notice did not use completion content: %q", summary.DetailText)
	}
	if summary.Truncated {
		t.Fatal("semantic replacement must not inherit retained-log truncation")
	}

	concise, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{MaxChars: 80, SuccessOutputMode: BackgroundOutputConcise})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent concise: %v", err)
	}
	if strings.Contains(concise.DetailText, "semantic replacement") || !strings.Contains(concise.DetailText, logPath) {
		t.Fatalf("concise finalized notice did not replace command output with its path: %q", concise.DetailText)
	}
}

func TestBackgroundNoticeUsesSharedCompletedShellPresentation(t *testing.T) {
	logPath := writeBackgroundNoticeLog(t, []byte("failed payload\n"))
	exitCode := 7
	event := newFinalizedBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), "failed payload", nil, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: BackgroundOutputDefault})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	if !strings.Contains(summary.DetailText, "Shell 1000 exited with code 7") {
		t.Fatalf("completion detail does not identify the shell's numeric exit status: %q", summary.DetailText)
	}
	if strings.Count(summary.DetailText, "failed payload") != 1 {
		t.Fatalf("completion detail does not deliver command output once: %q", summary.DetailText)
	}
	if strings.Contains(summary.DetailText, "Background shell") || strings.Contains(summary.DetailText, "Exit code:") {
		t.Fatalf("completion detail repeats lifecycle or exit facts: %q", summary.DetailText)
	}
	if strings.Contains(summary.DetailText, logPath) {
		t.Fatalf("completion detail exposes a path for full output: %q", summary.DetailText)
	}
}

func TestBackgroundNoticeTruncatedCompletionExposesPathOnce(t *testing.T) {
	longOutput := "start-" + strings.Repeat("x", 200) + "-end"
	logPath := writeBackgroundNoticeLog(t, []byte(longOutput+"\n"))
	exitCode := 5
	event := newFinalizedBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), longOutput, nil, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{MaxChars: 80, SuccessOutputMode: BackgroundOutputDefault})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	preview, removed := summary.RuntimePreview()
	if preview == "" || removed <= 0 || !summary.Truncated {
		t.Fatalf("truncated completion preview = %q, removed = %d, truncated = %t", preview, removed, summary.Truncated)
	}
	if strings.Count(summary.DetailText, logPath) != 1 {
		t.Fatalf("truncated completion must expose its path once: %q", summary.DetailText)
	}
	if !strings.Contains(summary.DetailText, "Shell 1000 exited with code 5") {
		t.Fatalf("truncated completion does not identify its shell and exit status: %q", summary.DetailText)
	}
	if strings.Contains(summary.DetailText, "Output file (") {
		t.Fatalf("truncated completion retained a redundant line-count header: %q", summary.DetailText)
	}
}

func TestBackgroundNoticeConciseSuccessUsesPathInsteadOfOutput(t *testing.T) {
	logPath := writeBackgroundNoticeLog(t, []byte("successful payload\n"))
	exitCode := 0
	event := newFinalizedBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), "successful payload", nil, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: BackgroundOutputConcise})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	if !strings.Contains(summary.DetailText, logPath) {
		t.Fatalf("concise completion did not expose the output-file path: %q", summary.DetailText)
	}
	if strings.Contains(summary.DetailText, "successful payload") || strings.Contains(summary.DetailText, "no output") {
		t.Fatalf("concise completion did not omit nonempty output honestly: %q", summary.DetailText)
	}
	if strings.Contains(summary.DetailText, "Background shell") || strings.Contains(summary.DetailText, "Exit code:") {
		t.Fatalf("concise completion repeats lifecycle or exit facts: %q", summary.DetailText)
	}
}

func TestBackgroundNoticeConciseFailureKeepsOutputAndWarning(t *testing.T) {
	logPath := writeBackgroundNoticeLog(t, []byte("failure payload\n"))
	warning, err := postprocess.NewWarning("recoverable warning")
	if err != nil {
		t.Fatalf("NewWarning: %v", err)
	}
	exitCode := 7
	event := newFinalizedBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), "failure payload", warning, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: BackgroundOutputConcise})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	if !strings.Contains(summary.DetailText, "failure payload") || !strings.Contains(summary.DetailText, warning.Text()) {
		t.Fatalf("concise failure hid command output or its warning: %q", summary.DetailText)
	}
	if !strings.Contains(summary.DetailText, "Shell 1000 exited with code 7") {
		t.Fatalf("concise failure does not identify its shell and exit status: %q", summary.DetailText)
	}
}

func TestBackgroundNoticeWarningOnlyOutputRemainsVisible(t *testing.T) {
	logPath := writeBackgroundNoticeLog(t, nil)
	warning, err := postprocess.NewWarning("recoverable warning")
	if err != nil {
		t.Fatalf("NewWarning: %v", err)
	}
	exitCode := 0
	event := newFinalizedBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), "", warning, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: BackgroundOutputConcise})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	if !strings.Contains(summary.DetailText, warning.Text()) ||
		strings.Contains(summary.DetailText, "and no output") ||
		strings.Contains(summary.DetailText, logPath) {
		t.Fatalf("warning-only completion was hidden or claimed command-log content: %q", summary.DetailText)
	}
}

func TestBackgroundNoticeWhitespaceLogRendersNoOutputCompletion(t *testing.T) {
	logPath := writeBackgroundNoticeLog(t, []byte("\n"))
	exitCode := 0
	event := newFinalizedBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), "", nil, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: BackgroundOutputDefault})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	if !strings.Contains(summary.DetailText, "Shell 1000 exited with code 0 and no output") {
		t.Fatalf("whitespace-only completion did not render an explicit empty result: %q", summary.DetailText)
	}
}

func TestBackgroundPreviewOmitsTruncationForWhitespaceOnlyOutput(t *testing.T) {
	builder := newBackgroundPreviewBuilder(80, BackgroundOutputDefault, false)
	builder.WriteRaw([]byte(strings.Repeat(" ", 200)))
	if !builder.Truncated() {
		t.Fatal("expected whitespace-only preview source to be truncated")
	}
	if preview := builder.Preview(); preview != "" {
		t.Fatalf("whitespace-only preview must not surface generated truncation content: %q", preview)
	}
}

func TestBackgroundNoticeFallbackCarriesTruncationWithoutFinalizedState(t *testing.T) {
	exitCode := 1
	event := newFallbackBackgroundEvent(EventCompleted, Snapshot{
		ID:       "1000",
		State:    "completed",
		ExitCode: &exitCode,
	}, strings.Repeat("x", 200), nil, 1, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{MaxChars: 80, SuccessOutputMode: BackgroundOutputDefault})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	preview, removed := summary.RuntimePreview()
	if preview == "" || removed <= 0 || !summary.Truncated {
		t.Fatal("expected fallback truncation")
	}
}

func TestBackgroundNoticeConciseFallbackRetainsVisibleCompletion(t *testing.T) {
	logPath := writeBackgroundNoticeLog(t, []byte("output\n"))
	exitCode := 0
	event := newFallbackBackgroundEvent(EventCompleted, completedBackgroundSnapshot(logPath, &exitCode), "output", nil, 0, false)

	summary, err := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: BackgroundOutputConcise})
	if err != nil {
		t.Fatalf("SummarizeBackgroundEvent: %v", err)
	}
	if !strings.Contains(summary.DetailText, logPath) || strings.Contains(summary.DetailText, "output\n") ||
		strings.Contains(summary.DetailText, "no output") {
		t.Fatalf("concise fallback did not replace output with its path: %q", summary.DetailText)
	}
}

func TestModelVisibleFallbackPreviewsLimitCommandOutputLines(t *testing.T) {
	long := strings.Repeat("x", 1001)
	want := strings.Repeat("x", 975) + "… [26 characters omitted]"
	path := writeBackgroundNoticeLog(t, []byte(long))
	exitCode := 0
	snapshot := completedBackgroundSnapshot(path, &exitCode)
	event, err := (&Manager{}).fallbackBackgroundEvent(EventCompleted, snapshot, nil)
	if err != nil || fallbackPreview(t, event) != want {
		t.Fatalf("fallback event = %v", err)
	}
	rawSnapshot := snapshot
	rawSnapshot.RawOutput = true
	rawEvent, _ := (&Manager{}).fallbackBackgroundEvent(EventCompleted, rawSnapshot, nil)
	if got := fallbackPreview(t, rawEvent); got != long {
		t.Fatalf("raw fallback preview = %q", got)
	}
	cache := &terminalEventCache{eventType: EventCompleted, snapshot: Snapshot{ID: "1000", RecentOutput: long}}
	cached := cache.fallbackEvent(errors.New("cache unavailable"))
	if got := fallbackPreview(t, cached); got != want {
		t.Fatalf("cache fallback preview = %q", got)
	}
}

func fallbackPreview(t *testing.T, event Event) string {
	t.Helper()
	summary, _ := SummarizeBackgroundEvent(event, BackgroundNoticeOptions{SuccessOutputMode: BackgroundOutputVerbose})
	got, _ := summary.RuntimePreview()
	return got
}
func TestInvariantFailureBackgroundNoticeRetainsPublicNotice(t *testing.T) {
	exitCode := 17
	summary := InvariantFailureBackgroundNotice(Event{
		Type: EventCompleted,
		Snapshot: Snapshot{
			ID:       "1000",
			State:    "completed",
			ExitCode: &exitCode,
			LogPath:  "/tmp/1000.log",
		},
	}, errors.New("missing completion output"))

	if summary.DetailText == "" || summary.CondensedText == "" {
		t.Fatal("invariant failure notice must remain visible through the ordinary notice envelope")
	}
}
