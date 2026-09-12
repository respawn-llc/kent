package transcriptrender

import (
	"core/shared/clientui"
	"core/shared/transcript"
	patchformat "core/shared/transcript/patchformat"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLongPatchPathPreservesFilenameAndCounts(t *testing.T) {
	path := strings.Repeat("long-directory/", 12) + "workflow.json"
	presentation := patchformat.Render("*** Begin Patch\n*** Update File: "+path+"\n-old\n+new\n*** End Patch\n", "/worktree")
	row := patchRow(presentation)
	status := "Mismatch between file and model-supplied content"
	row.Tool.IsError = true
	row.Tool.ResultSummary = &status
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
	presentation := patchformat.Render("*** Begin Patch\n*** Update File: dir/file.go\n-old\n+new\n*** End Patch\n", "/worktree")
	row := patchRow(presentation)
	status := strings.Repeat("failure ", 30)
	row.Tool.IsError = true
	row.Tool.ResultSummary = &status
	for _, mode := range []Mode{ModeOngoing, ModeOngoingCollapsed} {
		input := RenderCommittedRow(patchRow(presentation), 40, "dark", mode).Lines[0]
		failed := RenderCommittedRow(row, 40, "dark", mode).Lines[0]
		if !strings.HasPrefix(failed.Plain(), input.Plain()) {
			t.Fatalf("failure displaced patch input: %q; input %q", failed.Plain(), input.Plain())
		}
		assertPatchLink(t, []Line{failed}, "./dir/file.go", "file:///worktree/dir/file.go")
	}
}

func TestPatchHyperlinks(t *testing.T) {
	patch := "*** Begin Patch\n*** Update File: dir/file.go\n-old\n+new\n*** End Patch\n"
	rendered := patchformat.Render(patch, "/worktree")
	status := "ok"
	statusRow := patchRow(rendered)
	statusRow.Tool.ResultSummary = &status
	success := RenderCommittedRow(statusRow, 80, "dark", ModeOngoing).Lines[0]
	for _, span := range success.Spans {
		if span.Text == status {
			t.Fatal("successful patch row displayed result suffix")
		}
	}
	status = "failed"
	statusRow.Tool.IsError = true
	last := lastSpan(RenderCommittedRow(statusRow, 80, "dark", ModeOngoing).Lines[0])
	if last.Text != status || last.Hyperlink != nil {
		t.Fatal("failed patch row omitted failure status")
	}
	assertPatchLink(t, RenderCommittedRow(patchRow(rendered), 80, "dark", ModeOngoing).Lines, "./dir/file.go", "file:///worktree/dir/file.go")
	assertPatchLink(t, RenderCommittedRow(patchRow(rendered), 12, "dark", ModeDetailExpanded).Lines, "/worktree/dir/file.go", "file:///worktree/dir/file.go")
	moved := patchformat.Render("*** Begin Patch\n*** Update File: old.go\n*** Move to: new.go\n-old\n+new\n*** End Patch\n", "/worktree")
	assertPatchLink(t, RenderCommittedRow(patchRow(moved), 80, "dark", ModeOngoing).Lines, "./new.go", "file:///worktree/new.go")
	relative := patchformat.Render(patch, "")
	for _, mode := range []Mode{ModeOngoing, ModeDetailExpanded} {
		if text, _ := patchLink(RenderCommittedRow(patchRow(relative), 80, "dark", mode).Lines); text != "" {
			t.Fatalf("relative path linked as %q", text)
		}
	}
	if text, _ := patchLink(RenderCommittedRow(patchRow(relative), 80, "dark", ModeOngoing).Lines); text != "" {
		t.Fatalf("relative patch path linked as %q", text)
	}
}
func patchRow(presentation patchformat.Presentation) clientui.TranscriptCommittedRow {
	return clientui.TranscriptCommittedRow{Kind: clientui.TranscriptRowTool, Tool: &clientui.TranscriptToolRow{ToolName: "patch", Presentation: &transcript.ToolCallMeta{ToolName: "patch", PatchPresentation: &presentation}}}
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
