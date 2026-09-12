package transcriptrender

import (
	"strings"
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/proto"
)

func TestLongPatchPathPreservesFilenameAndCounts(t *testing.T) {
	path := strings.Repeat("long-directory/", 12) + "workflow.json"
	row := patchRow("/worktree/"+path, "./"+path)
	status := "Mismatch between file and model-supplied content"
	row.GetTool().IsError = true
	row.GetTool().ResultSummary = &status
	for _, width := range []int{24, 40, 80} {
		for _, mode := range []Mode{ModeOngoing, ModeOngoingCollapsed} {
			line := RenderCommittedRow(row, width, "dark", mode).Lines[0]
			text, url := patchLink([]Line{line})
			if !strings.HasSuffix(text, "workflow.json") || url != "file:///worktree/"+path {
				t.Fatalf("lost filename or link at width %d: %q %q", width, text, url)
			}
			for _, role := range []StyleRole{StyleRoleToolError, StyleRoleToolSuccess} {
				var found bool
				for _, span := range line.Spans {
					if spanRole, ok := span.Style.Role(); ok && spanRole == role && span.Text != "" {
						found = true
					}
				}
				if !found {
					t.Fatalf("lost change count at width %d: %q", width, line.Plain())
				}
			}
			if lipgloss.Width(line.Plain()) > width {
				t.Fatalf("row overflow at width %d: %q", width, line.Plain())
			}
			t.Logf("%d columns: %s", width, line.Plain())
		}
	}
}

func TestPatchFailurePreservesInputBeforeStatus(t *testing.T) {
	row := patchRow("/worktree/dir/file.go", "./dir/file.go")
	status := strings.Repeat("failure ", 30)
	row.GetTool().IsError = true
	row.GetTool().ResultSummary = &status
	for _, mode := range []Mode{ModeOngoing, ModeOngoingCollapsed} {
		input := RenderCommittedRow(patchRow("/worktree/dir/file.go", "./dir/file.go"), 40, "dark", mode).Lines[0]
		failed := RenderCommittedRow(row, 40, "dark", mode).Lines[0]
		if !strings.HasPrefix(failed.Plain(), input.Plain()) {
			t.Fatalf("failure displaced patch input: %q; input %q", failed.Plain(), input.Plain())
		}
		assertPatchLink(t, []Line{failed}, "./dir/file.go", "file:///worktree/dir/file.go")
	}
}

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
