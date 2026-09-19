package transcriptrender

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"strings"
	"testing"
)

func TestUserShellCommandOnlyRevealsRawOutputInExpandedDetail(t *testing.T) {
	command := "echo '**literal**'"
	output := "**literal** <result>raw output</result>"
	fullText := strings.Join([]string{t.Name(), command, output}, "\n")
	messageType := transcriptpb.NoticeMessageType_NOTICE_MESSAGE_TYPE_USER_SHELL_COMMAND
	row := &transcriptpb.CommittedRow{
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Row: &transcriptpb.CommittedRow_Notice{Notice: &transcriptpb.NoticeRow{
			MessageType:   &messageType,
			CompactLabel:  &command,
			CondensedText: &command,
			Reason:        transcriptpb.NoticeReason_NOTICE_REASON_RUNTIME_DIAGNOSTIC,
			Diagnostic:    &transcriptpb.Diagnostic{Code: "user_shell_command", Detail: fullText},
		}},
	}
	for _, mode := range []Mode{ModeOngoing, ModeOngoingCollapsed, ModeOngoingFull, ModeOngoingStable, ModeDetailCollapsed} {
		text := strings.Join(PlainLines(RenderCommittedRow(row, 120, "dark", mode).Lines), "\n")
		if !strings.Contains(text, command) || strings.Contains(text, output) {
			t.Fatalf("mode=%d must show only the shell command: %q", mode, text)
		}
	}
	detail := RenderDetailPresentation(row, 120, "dark")
	if !detail.Expandable {
		t.Fatal("shell command output must be expandable")
	}
	text := strings.Join(PlainLines(detail.Expanded), "\n")
	for _, line := range strings.Split(fullText, "\n") {
		if !strings.Contains(text, line) {
			t.Fatalf("expanded output did not preserve raw text %q: %q", line, text)
		}
	}
}
