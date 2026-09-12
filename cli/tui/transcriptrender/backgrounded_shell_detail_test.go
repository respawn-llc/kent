package transcriptrender

import (
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/toolspec"
	"google.golang.org/protobuf/proto"
)

func TestBackgroundedShellDetailExpansionShowsFullCommandAndCommittedOutput(t *testing.T) {
	const (
		command = "printf first-line\nprintf full-command-line"
		output  = "server supplied output"
	)
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolName: proto.String(string(string(toolspec.ToolExecCommand))),
			Text:     output,
			Presentation: &transcriptpb.ToolPresentation{
				Presentation:      transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_SHELL,
				RenderBehavior:    transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_SHELL,
				IsShell:           true,
				Command:           stringPtr(command),
				CompactText:       stringPtr("printf first-line"),
				MovedToBackground: true,
			},
		}},
	}
	presentation := RenderDetailPresentation(row, 120, "dark")

	if !presentation.Expandable {
		t.Fatal("backgrounded shell with full command and committed output is not expandable")
	}
	if got := len(presentation.Collapsed); got != 1 {
		t.Fatalf("backgrounded shell collapsed rows = %d, want one", got)
	}
	if got := len(presentation.Expanded); got <= len(presentation.Collapsed) {
		t.Fatalf("backgrounded shell expansion did not reveal additional rows: collapsed=%d expanded=%d", len(presentation.Collapsed), got)
	}
	if got := len(RenderCommittedRow(row, 120, "dark", ModeOngoing).Lines); got != len(presentation.Collapsed) {
		t.Fatalf("backgrounded shell ongoing rows = %d, want compact row count %d", got, len(presentation.Collapsed))
	}
}
