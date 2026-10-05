package tui

import (
	"strings"
	"testing"

	"core/internal/testharness/pty"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"google.golang.org/protobuf/proto"
)

func TestDetailPatchHyperlinkClosesBeforeUnselectedAndSelectedPadding(t *testing.T) {
	removed := int32(0)
	row := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_DETAIL,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolName: proto.String(string("patch")),
			Presentation: &transcriptpb.ToolPresentation{
				Presentation:   transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
				RenderBehavior: transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
				PatchPresentation: &transcriptpb.PatchPresentation{
					Presentation: &transcriptpb.PatchPresentation_Changes{Changes: &transcriptpb.PatchChanges{
						Files: []*transcriptpb.PatchFileChange{{
							Path: &transcriptpb.PatchPath{
								Absolute: "/worktree/dir/file.go",
								Relative: "dir/file.go",
							},
							Removed: &removed,
							Operations: []*transcriptpb.PatchFileOperation{{
								Operation: &transcriptpb.PatchFileOperation_Update{Update: &transcriptpb.PatchUpdateOperation{}},
							}},
						}},
					}},
				},
			},
		}},
	}
	for _, selected := range []bool{false, true} {
		model := NewModel()
		model.expanded = map[int]struct{}{0: {}}
		model.detailProjection.replaceSnapshot([]*transcriptpb.CommittedRow{row}, model.detailContentWidth(), model.theme, model.expanded)
		if selected {
			model.setSelectedDetailIndex(0)
		}
		lines := model.detailProjectedLines()
		if selected {
			lines = model.detailVisibleProjectedLines()
		}
		trace := pty.TraceTerminalHyperlinks(t, strings.Join(renderDetailProjectedLines(lines, model.theme), "\n"))
		if got := trace.LinkedText("file:///worktree/dir/file.go"); got != "/worktree/dir/file.go" {
			t.Fatalf("linked detail path = %q, want represented path", got)
		}
	}
}
