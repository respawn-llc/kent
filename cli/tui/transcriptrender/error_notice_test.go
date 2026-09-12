package transcriptrender

import (
	"reflect"
	"strings"
	"testing"
	"unicode"

	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcript"
	"google.golang.org/protobuf/proto"
)

func TestErrorNoticeClassifiesAsError(t *testing.T) {
	row := &transcriptpb.NoticeRow{
		Reason:   transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
		Severity: transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
		Diagnostic: &transcriptpb.Diagnostic{
			Code:   string(transcript.EntryRoleDeveloperErrorFeedback),
			Detail: "failure",
		},
	}
	if got := noticeStyleRole(row); got != StyleRoleError {
		t.Fatalf("error notice role = %v, want %v", got, StyleRoleError)
	}
}

func TestRuntimeDiagnosticErrorUsesCompleteTypedDetailInEveryMode(t *testing.T) {
	detail := "diagnostic \x1b[31mred\tvalue\x00\nsecond diagnostic \x07line remains complete"
	want := []string{
		"diagnostic [31mred value",
		"second diagnostic line remains complete",
	}
	misleadingCompact := "wrong compact source"
	misleadingCondensed := "wrong condensed source"
	row := errorNoticeRow(&transcriptpb.NoticeRow{
		Reason:        transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
		Severity:      transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
		CompactLabel:  &misleadingCompact,
		CondensedText: &misleadingCondensed,
		Diagnostic: &transcriptpb.Diagnostic{
			Code:   string(transcript.EntryRoleDeveloperErrorFeedback),
			Detail: detail,
		},
	})
	if err := protoapi.Validate(row.GetNotice()); err != nil {
		t.Fatalf("runtime diagnostic row is invalid: %v", err)
	}

	for _, mode := range allTranscriptModes() {
		t.Run(transcriptModeName(mode), func(t *testing.T) {
			rendered := RenderCommittedRow(row, 120, "dark", mode)
			assertCompleteErrorContent(
				t,
				rendered,
				want,
			)
		})
	}
}

func TestLegacyUntypedErrorUsesCompleteLegacyTextInEveryMode(t *testing.T) {
	legacy := "legacy alpha\tbeta\x00\nsecond legacy \x07line remains complete"
	want := []string{
		"legacy alpha beta",
		"second legacy line remains complete",
	}
	misleadingCompact := "wrong compact source"
	misleadingCondensed := "wrong condensed source"
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ERROR_FEEDBACK
	row := errorNoticeRow(&transcriptpb.NoticeRow{
		Reason:        transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
		Severity:      transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
		MessageType:   &messageType,
		LegacyText:    &legacy,
		CompactLabel:  &misleadingCompact,
		CondensedText: &misleadingCondensed,
	})
	if err := protoapi.Validate(row.GetNotice()); err != nil {
		t.Fatalf("legacy error row is invalid: %v", err)
	}

	for _, mode := range allTranscriptModes() {
		t.Run(transcriptModeName(mode), func(t *testing.T) {
			rendered := RenderCommittedRow(row, 120, "dark", mode)
			assertCompleteErrorContent(
				t,
				rendered,
				want,
			)
		})
	}
}

func TestErrorNoticeReasonSelectsItsTypedContentSource(t *testing.T) {
	cacheWarning := &transcriptpb.CacheWarning{
		Scope:      string(transcript.CacheWarningScopeConversation),
		Reason:     string(transcript.CacheWarningReasonNonPostfix),
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
	}
	compactionDetail := "typed compaction detail"
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SUMMARY
	repair := &transcriptpb.ToolOutputRepair{
		Kind:  string(transcript.ToolOutputRepairFreshResource),
		Count: 2,
	}
	diagnosticDetail := "typed runtime diagnostic"
	legacyText := "typed legacy text"
	metadataType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKTREE_MODE
	metadataNotice := &transcriptpb.NoticeRow{
		Reason:      transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
		Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
		MessageType: &metadataType,
		Worktree: &transcriptpb.WorktreeContext{
			Branch:        stringPtr("feature/error-source"),
			WorktreePath:  "/workspace/feature",
			WorkspaceRoot: "/workspace",
			EffectiveCwd:  "/workspace/feature",
		},
	}
	metadataText, metadataTextPresent := worktreeNoticeText(metadataNotice, ModeDetailExpanded)
	if !metadataTextPresent {
		t.Fatal("typed Worktree message metadata has no formatter")
	}

	tests := []struct {
		name   string
		notice *transcriptpb.NoticeRow
		want   string
	}{
		{
			name: "cache warning",
			notice: &transcriptpb.NoticeRow{
				Reason:       transcriptpb.NoticeReason_NOTICE_REASON_CACHE_WARNING,
				Severity:     transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				CacheWarning: cacheWarning,
			},
			want: cacheWarningNoticeText(cacheWarning),
		},
		{
			name: "compaction detail",
			notice: &transcriptpb.NoticeRow{
				Reason:      transcriptpb.NoticeReason_NOTICE_REASON_COMPACTION,
				Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				MessageType: &messageType,
				Compaction:  &transcriptpb.CompactionNotice{Detail: &compactionDetail},
			},
			want: compactionDetail,
		},
		{
			name: "compaction formatter",
			notice: &transcriptpb.NoticeRow{
				Reason:      transcriptpb.NoticeReason_NOTICE_REASON_COMPACTION,
				Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				MessageType: &messageType,
				Compaction:  &transcriptpb.CompactionNotice{},
			},
			want: compactionNoticeText(nil),
		},
		{
			name: "tool output repair",
			notice: &transcriptpb.NoticeRow{
				Reason:           transcriptpb.NoticeReason_NOTICE_REASON_TOOL_OUTPUT_REPAIR,
				Severity:         transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				ToolOutputRepair: repair,
			},
			want: toolOutputRepairNoticeText(repair),
		},
		{
			name: "runtime diagnostic",
			notice: &transcriptpb.NoticeRow{
				Reason:   transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
				Severity: transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				Diagnostic: &transcriptpb.Diagnostic{
					Code:   string(transcript.EntryRoleDeveloperErrorFeedback),
					Detail: diagnosticDetail,
				},
			},
			want: diagnosticDetail,
		},
		{
			name: "legacy text",
			notice: &transcriptpb.NoticeRow{
				Reason:     transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
				Severity:   transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
				LegacyText: &legacyText,
			},
			want: legacyText,
		},
		{
			name:   "typed message metadata",
			notice: metadataNotice,
			want:   metadataText,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := protoapi.Validate(test.notice); err != nil {
				t.Fatalf("notice is invalid: %v", err)
			}
			rendered := RenderCommittedRow(errorNoticeRow(test.notice), 120, "dark", ModeOngoingCollapsed)
			assertCompleteErrorContent(t, rendered, []string{test.want})
		})
	}
}

func TestMetadataOnlyLegacyErrorWithoutTypedFormatterFailsValidation(t *testing.T) {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ERROR_FEEDBACK
	condensed := "condensed preview is not error content"
	compact := "compact label is not error content"
	sourcePath := "/preview/source/path"
	notice := &transcriptpb.NoticeRow{
		Reason:        transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
		Severity:      transcriptpb.NoticeSeverity_NOTICE_SEVERITY_ERROR,
		MessageType:   &messageType,
		CondensedText: &condensed,
		CompactLabel:  &compact,
		SourcePath:    &sourcePath,
	}
	if err := protoapi.Validate(notice); err == nil {
		t.Fatal("metadata-only legacy error without a typed formatter passed validation")
	}
}

func TestNonErrorNoticeAndToolRowsRetainCompactOneLineLayout(t *testing.T) {
	legacy := "ordinary notice first line\nordinary notice second line"
	notice := &transcriptpb.NoticeRow{
		Reason:     transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
		Severity:   transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
		LegacyText: &legacy,
	}
	rendered := RenderCommittedRow(errorNoticeRow(notice), 80, "dark", ModeOngoingCollapsed)
	if got := len(rendered.Lines); got != 1 {
		t.Fatalf("ordinary notice lines = %d, want compact single line", got)
	}
	tool := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolName: proto.String(string("ordinary_tool")),
			Text:     "ordinary tool first line\nordinary tool second line",
		}},
	}
	rendered = RenderCommittedRow(tool, 80, "dark", ModeOngoingCollapsed)
	if got := len(rendered.Lines); got != 1 {
		t.Fatalf("ordinary tool lines = %d, want compact single line", got)
	}
}

func errorNoticeRow(notice *transcriptpb.NoticeRow) *transcriptpb.CommittedRow {
	return &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row:        &transcriptpb.CommittedRow_Notice{Notice: notice},
	}
}

func TestProviderModelMismatchNoticeRendersInDetailModesAndWraps(t *testing.T) {
	row := errorNoticeRow(&transcriptpb.NoticeRow{
		Reason:   transcriptpb.NoticeReason_NOTICE_REASON_PROVIDER_MODEL_MISMATCH,
		Severity: transcriptpb.NoticeSeverity_NOTICE_SEVERITY_WARNING,
		ProviderModelMismatch: &transcriptpb.ProviderModelMismatch{
			RequestedModel: "session-contract-model",
			ServedModel:    "served-model",
		},
	})
	if len(RenderCommittedRow(row, 120, "dark", ModeDetailExpanded).Lines) == 0 {
		t.Fatal("expanded provider-model warning rendered no lines")
	}
	if lines := RenderCommittedRow(row, 28, "dark", ModeDetailCollapsed).Lines; len(lines) < 2 {
		t.Fatalf("narrow provider-model warning lines = %d, want wrapping", len(lines))
	}
}

func allTranscriptModes() []Mode {
	return []Mode{
		ModeOngoing,
		ModeOngoingCollapsed,
		ModeDetailCollapsed,
		ModeOngoingFull,
		ModeOngoingStable,
		ModeDetailExpanded,
	}
}

func transcriptModeName(mode Mode) string {
	switch mode {
	case ModeOngoing:
		return "ongoing"
	case ModeOngoingCollapsed:
		return "ongoing_collapsed"
	case ModeOngoingFull:
		return "ongoing_full"
	case ModeOngoingStable:
		return "ongoing_stable"
	case ModeDetailCollapsed:
		return "detail_collapsed"
	case ModeDetailExpanded:
		return "detail_expanded"
	default:
		return "unknown"
	}
}

func assertCompleteErrorContent(t *testing.T, rendered Row, want []string) {
	t.Helper()
	got := make([]string, 0, len(rendered.Lines))
	for _, line := range rendered.Lines {
		var content strings.Builder
		for _, span := range line.Spans {
			if role, ok := span.Style.Role(); ok && role == StyleRoleError {
				content.WriteString(span.Text)
			}
		}
		projected := strings.TrimSpace(content.String())
		for _, value := range projected {
			if unicode.IsControl(value) {
				t.Fatalf("rendered error payload contains terminal control %U", value)
			}
		}
		got = append(got, projected)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rendered semantic error payload = %#v, want %#v", got, want)
	}
}
