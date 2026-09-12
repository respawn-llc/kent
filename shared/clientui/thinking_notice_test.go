package clientui_test

import (
	"testing"

	"core/shared/clientui"
	"core/shared/textutil"
)

func TestThinkingNoticeRequiresItsOwnEffort(t *testing.T) {
	for _, tc := range []struct {
		name   string
		notice clientui.TranscriptNoticeRow
		valid  bool
	}{
		{name: "high", notice: clientui.TranscriptNoticeRow{Reason: clientui.TranscriptNoticeThinkingUpdate, Severity: clientui.TranscriptNoticeInfo, ThinkingEffort: textutil.Value("high")}, valid: true},
		{name: "provider level", notice: clientui.TranscriptNoticeRow{Reason: clientui.TranscriptNoticeThinkingUpdate, Severity: clientui.TranscriptNoticeInfo, ThinkingEffort: textutil.Value("provider-effort")}, valid: true},
		{name: "absent", notice: clientui.TranscriptNoticeRow{Reason: clientui.TranscriptNoticeThinkingUpdate, Severity: clientui.TranscriptNoticeInfo}},
		{name: "empty", notice: clientui.TranscriptNoticeRow{Reason: clientui.TranscriptNoticeThinkingUpdate, Severity: clientui.TranscriptNoticeInfo, ThinkingEffort: textutil.Value("")}},
		{name: "wrong reason", notice: clientui.TranscriptNoticeRow{Reason: clientui.TranscriptNoticeCompaction, Severity: clientui.TranscriptNoticeInfo, ThinkingEffort: textutil.Value("high")}},
		{name: "mixed facts", notice: clientui.TranscriptNoticeRow{Reason: clientui.TranscriptNoticeThinkingUpdate, Severity: clientui.TranscriptNoticeInfo, ThinkingEffort: textutil.Value("high"), Compaction: &clientui.TranscriptCompactionNotice{Count: textutil.Value(1)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.notice.Validate(); (err == nil) != tc.valid {
				t.Fatalf("valid=%t, error=%v", tc.valid, err)
			}
		})
	}
}
