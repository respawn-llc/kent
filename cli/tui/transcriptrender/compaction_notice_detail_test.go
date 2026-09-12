package transcriptrender

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"testing"
)

func TestExpandedCompactionNoticesUseNormalTextRole(t *testing.T) {
	for _, test := range []struct {
		name        string
		messageType transcriptpb.NoticeMessageType
		compactRole StyleRole
	}{
		{
			name:        "summary",
			messageType: transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SUMMARY,
			compactRole: StyleRoleNoticeSecondary,
		},
		{
			name:        "reminder",
			messageType: transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SOON_REMINDER,
			compactRole: StyleRoleWarning,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			notice := &transcriptpb.NoticeRow{MessageType: &test.messageType}
			if test.messageType == transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SUMMARY {
				detail := "provider compaction detail"
				notice.Reason = transcriptpb.NoticeReason_NOTICE_REASON_COMPACTION
				notice.Severity = transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO
				notice.Compaction = &transcriptpb.CompactionNotice{Detail: &detail}
			}
			row := &transcriptpb.CommittedRow{
				Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
				Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
				Row:        &transcriptpb.CommittedRow_Notice{Notice: notice},
			}

			if got, _ := noticeRoleAndText(row.GetNotice(), row.Visibility, ModeDetailCollapsed); got != test.compactRole {
				t.Fatalf("compact role = %v, want %v", got, test.compactRole)
			}
			if got, _ := noticeRoleAndText(row.GetNotice(), row.Visibility, ModeDetailExpanded); got != StyleRoleNotice {
				t.Fatalf("expanded role = %v, want normal notice role %v", got, StyleRoleNotice)
			}
		})
	}
}

func TestCompactionNoticeWithoutDetailIsNotExpandable(t *testing.T) {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_COMPACTION_SUMMARY
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:      transcriptpb.NoticeReason_NOTICE_REASON_COMPACTION,
			Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			MessageType: &messageType,
			Compaction:  &transcriptpb.CompactionNotice{},
		}},
	}

	presentation := RenderDetailPresentation(row, 80, "dark")
	if presentation.Expandable {
		t.Fatal("compaction notice is expandable without additional detail")
	}
}
