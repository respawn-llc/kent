package transcriptrender

import (
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestPatchHyperlinks(t *testing.T) {
	status := "ok"
	statusRow := patchRow("/worktree/dir/file.go", "./dir/file.go")
	statusRow.GetTool().ResultSummary = &status
	success := RenderCommittedRow(statusRow, 80, "dark", ModeOngoing).Lines[0]
	for _, span := range success.Spans {
		if span.Text == status {
			t.Fatal("successful patch row displayed result suffix")
		}
	}
	status = "failed"
	statusRow.GetTool().IsError = true
	last := lastSpan(RenderCommittedRow(statusRow, 80, "dark", ModeOngoing).Lines[0])
	if last.Text != status || last.Hyperlink != nil {
		t.Fatal("failed patch row omitted failure status")
	}
	assertPatchLink(t, RenderCommittedRow(patchRow("/worktree/dir/file.go", "./dir/file.go"), 80, "dark", ModeOngoing).Lines, "./dir/file.go", "file:///worktree/dir/file.go")
	assertPatchLink(t, RenderCommittedRow(patchRow("/worktree/dir/file.go", "./dir/file.go"), 12, "dark", ModeDetailExpanded).Lines, "/worktree/dir/file.go", "file:///worktree/dir/file.go")
	moved := patchRow("/worktree/new.go", "./new.go")
	file := moved.GetTool().Presentation.PatchPresentation.GetChanges().Files[0]
	file.Operations[0].Operation = &transcriptpb.PatchFileOperation_Move{
		Move: &transcriptpb.PatchMoveOperation{
			Source: &transcriptpb.PatchPath{
				Absolute: "/worktree/old.go",
				Relative: "./old.go",
			},
			Groups: file.Operations[0].GetUpdate().Groups,
		},
	}
	assertPatchLink(t, RenderCommittedRow(moved, 80, "dark", ModeOngoing).Lines, "./new.go", "file:///worktree/new.go")
	relative := patchRow("dir/file.go", "dir/file.go")
	for _, mode := range []Mode{ModeOngoing, ModeDetailExpanded} {
		if text, _ := patchLink(RenderCommittedRow(relative, 80, "dark", mode).Lines); text != "" {
			t.Fatalf("relative path linked as %q", text)
		}
	}
}

func patchRow(absolute, relative string) *transcriptpb.CommittedRow {
	removed := int32(1)
	return &transcriptpb.CommittedRow{
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolName: proto.String(string("patch")),
			Presentation: &transcriptpb.ToolPresentation{
				Presentation:   transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
				RenderBehavior: transcriptpb.ToolPresentationKind_TOOL_PRESENTATION_KIND_DEFAULT,
				PatchPresentation: &transcriptpb.PatchPresentation{
					Presentation: &transcriptpb.PatchPresentation_Changes{Changes: &transcriptpb.PatchChanges{
						Files: []*transcriptpb.PatchFileChange{{
							Path: &transcriptpb.PatchPath{
								Absolute: absolute,
								Relative: relative,
							},
							Added: 1, Removed: &removed,
							Operations: []*transcriptpb.PatchFileOperation{{
								Operation: &transcriptpb.PatchFileOperation_Update{Update: &transcriptpb.PatchUpdateOperation{
									Groups: []*transcriptpb.PatchChangeGroup{{Lines: []*transcriptpb.PatchChangedLine{
										{Kind: transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_REMOVED, Content: "old"},
										{Kind: transcriptpb.PatchChangedLineKind_PATCH_CHANGED_LINE_KIND_ADDED, Content: "new"},
									}}},
								}},
							}},
						}},
					}},
				},
			},
		}},
	}
}
func patchLink(lines []Line) (text, url string) {
	for _, line := range lines {
		for _, span := range line.Spans {
			if span.Hyperlink != nil {
				text += span.Text
				url = span.Hyperlink.URL
			}
		}
	}
	return text, url
}
func lastSpan(line Line) Span { return line.Spans[len(line.Spans)-1] }
func assertPatchLink(t *testing.T, lines []Line, text, url string) {
	got, gotURL := patchLink(lines)
	if got != text || gotURL != url {
		t.Fatalf("patch link = (%q, %q), want (%q, %q)", got, gotURL, text, url)
	}
}
func TestTruncateLineLeavesEllipsisOutsidePatchHyperlink(t *testing.T) {
	line := Line{Spans: []Span{{Text: "/worktree/long-file.go", Style: SemanticStyle(StyleRoleToolPatch), Hyperlink: &Hyperlink{URL: "file:///worktree/long-file.go"}}}}
	truncated := TruncateLine(line, 10, false)
	if truncated.Spans[0].Hyperlink == nil || truncated.Spans[len(truncated.Spans)-1].Text != "…" || truncated.Spans[len(truncated.Spans)-1].Hyperlink != nil {
		t.Fatalf("truncated path hyperlink boundary = %+v", truncated.Spans)
	}
}
