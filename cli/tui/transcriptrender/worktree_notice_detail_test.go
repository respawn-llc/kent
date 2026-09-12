package transcriptrender

import (
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"core/shared/transcript"
)

func TestExpandedWorktreeNoticeShowsFullReminderVerbatim(t *testing.T) {
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_WORKTREE_MODE
	const fullReminder = "  full worktree reminder\nwith exact spacing  "
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			Reason:      transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
			Severity:    transcriptpb.NoticeSeverity_NOTICE_SEVERITY_INFO,
			MessageType: &messageType,
			Worktree: &transcriptpb.WorktreeContext{
				Branch:        textutil.Value("feature/transcript"),
				WorktreePath:  "/tmp/worktree",
				WorkspaceRoot: "/tmp/workspace",
				EffectiveCwd:  "/tmp/worktree/pkg",
			},
			Diagnostic: &transcriptpb.Diagnostic{
				Code:   string(transcript.EntryRoleDeveloperContext),
				Detail: fullReminder,
			},
		}},
	}

	if _, got := noticeRoleAndText(row.GetNotice(), row.Visibility, ModeDetailExpanded); got != fullReminder {
		t.Fatalf("expanded worktree reminder = %q, want full reminder preserved verbatim", got)
	}
	if !RenderDetailPresentation(row, 80, "dark").Expandable {
		t.Fatal("worktree reminder with full content is not expandable")
	}
}
