package transcriptrender

import (
	"testing"

	"core/shared/clientui"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/transcript"
)

func TestSessionRebindNoticeIsCompactAndExpandsToFullReminder(t *testing.T) {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_SESSION_REBIND
	fullReminder := t.Name()
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:      transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
			Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			MessageType: &messageType,
			Diagnostic: &transcriptpb.Diagnostic{
				Code:   string(transcript.EntryRoleDeveloperContext),
				Detail: fullReminder,
			},
		}},
	}
	if _, got := noticeRoleAndText(row.GetNotice(), row.Visibility, ModeDetailCollapsed); got != clientui.SessionRebindCompactLabel {
		t.Fatalf("collapsed Session rebind notice = %q", got)
	}
	if _, got := noticeRoleAndText(row.GetNotice(), row.Visibility, ModeDetailExpanded); got != row.GetNotice().Diagnostic.Detail {
		t.Fatalf("expanded Session rebind notice = %q", got)
	}
}
