package transcriptrender

import (
	"core/shared/clientui"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"testing"
)

func TestTypedSystemNoticesUseMarkdown(t *testing.T) {
	for _, messageType := range []transcriptpb.NoticeMessageType{transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_AGENTS_MD, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_SKILLS, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_SUBAGENTS, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKSPACES, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ENVIRONMENT, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SUMMARY, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_HEADLESS_MODE, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_HEADLESS_MODE_EXIT, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_ACTIVE_GOAL_CONTINUATION, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKFLOW_MODE} {
		t.Run(string(messageType), func(t *testing.T) {
			if !noticeUsesMarkdown(systemNoticeRow(messageType).GetNotice()) {
				t.Fatalf("message type %q does not use Markdown", messageType)
			}
		})
	}
}

func TestWorkspacesNoticeCanExpandMarkdownDetails(t *testing.T) {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKSPACES
	const detail = "expanded detail"
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:        transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
			Severity:      transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			MessageType:   &messageType,
			CompactLabel:  stringPtr("server compact label"),
			CondensedText: stringPtr("server condensed text"),
			Diagnostic: &transcriptpb.Diagnostic{
				Code:   string(clientui.MessageTypeWorkspaces),
				Detail: detail,
			},
		}},
	}

	_, expanded := noticeRoleAndText(row.GetNotice(), row.Visibility, ModeDetailExpanded)
	if expanded != detail {
		t.Fatal("expanded Workspaces presentation did not preserve the notice detail")
	}
	if !noticeUsesMarkdown(row.GetNotice()) {
		t.Fatal("Workspaces reminder is not rendered as Markdown")
	}
	if !RenderDetailPresentation(row, 80, "dark").Expandable {
		t.Fatal("Workspaces reminder is not expandable")
	}
}

func TestExcludedSystemNoticesDoNotUseMarkdown(t *testing.T) {
	for _, messageType := range []transcriptpb.NoticeMessageType{transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_HANDOFF_FUTURE_MESSAGE, transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_PRESERVED_USER_MESSAGE} {
		t.Run(string(messageType), func(t *testing.T) {
			if noticeUsesMarkdown(systemNoticeRow(messageType).GetNotice()) {
				t.Fatalf("message type %q unexpectedly uses Markdown", messageType)
			}
		})
	}
}

func systemNoticeRow(messageType transcriptpb.NoticeMessageType) *transcriptpb.CommittedRow {
	return &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:      transcriptpb.NoticeReason_NOTICE_REASON_LEGACY_UNTYPED_NOTICE,
			Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			MessageType: &messageType,
		}},
	}
}
