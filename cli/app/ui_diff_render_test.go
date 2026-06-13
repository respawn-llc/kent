package app

import (
	"core/cli/tui"
	"core/server/llm"
	"core/server/runtime"
	"core/server/tools"
	"core/shared/toolspec"
	"core/shared/transcript"
	patchformat "core/shared/transcript/patchformat"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

func TestDetailDiffBackgroundCoversSyntaxHighlightedCodeInAppView(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.termWidth = 80
	m.termHeight = 18
	m.syncViewport()

	detail := "Edited:\n./main.go\n+package main\n-func removed() {}"
	m = updateUIModel(t, m, tui.AppendTranscriptMsg{
		Role: "tool_call",
		Text: detail,
		ToolCall: &transcript.ToolCallMeta{
			ToolName:    "patch",
			PatchDetail: detail,
			PatchRender: &patchformat.RenderedPatch{DetailLines: []patchformat.RenderedLine{
				{Kind: patchformat.RenderedLineKindHeader, Text: "Edited:", FileIndex: -1},
				{Kind: patchformat.RenderedLineKindFile, Text: "./main.go", FileIndex: 0, Path: "main.go"},
				{Kind: patchformat.RenderedLineKindDiff, Text: "+package main", FileIndex: 0},
				{Kind: patchformat.RenderedLineKindDiff, Text: "-func removed() {}", FileIndex: 0},
			}},
			RenderHint: &transcript.ToolRenderHint{Kind: transcript.ToolRenderKindDiff},
		},
	})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	view := m.View()
	const addBg = "\x1b[48;2;31;42;34m"
	var addLine string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), "+package main") {
			addLine = line
			break
		}
	}
	if addLine == "" {
		t.Fatalf("expected detail diff line in app view, got %q", view)
	}
	if !strings.Contains(addLine, addBg+"  ") {
		t.Fatalf("expected app view to preserve diff background on detail indentation, got %q", addLine)
	}
	packageIdx := strings.Index(addLine, "package")
	if packageIdx < 0 {
		t.Fatalf("expected syntax-highlighted package token in app view, got %q", addLine)
	}
	bgIdx := strings.LastIndex(addLine[:packageIdx], addBg)
	if bgIdx < 0 {
		t.Fatalf("expected app view to apply diff background before syntax-highlighted token, got %q", addLine)
	}
	if strings.Contains(addLine[bgIdx:packageIdx], "\x1b[0") {
		t.Fatalf("expected no reset between diff background and first syntax-highlighted token, got %q", addLine)
	}
	if got := ansi.Strip(addLine); !strings.Contains(got, "+package main") {
		t.Fatalf("expected app view text preserved, got %q", got)
	}
}

func TestCustomPatchToolCallRendersSummaryOngoingAndHighlightedDiffDetail(t *testing.T) {
	const viewportWidth = 80
	patchText := "*** Begin Patch\n*** Update File: cli/app/ui_status.go\n@@\n type uiStatusAuthInfo struct {\n-\tSummary string\n+\tSummary string\n+\tReady bool\n }\n*** End Patch\n"

	m := newProjectedStaticUIModel()
	m.termWidth = viewportWidth
	m.termHeight = 18
	m.syncViewport()

	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(projectRuntimeEvent(runtime.Event{
		Kind:   runtime.EventToolCallStarted,
		StepID: "step-1",
		ToolCall: &llm.ToolCall{
			ID:          "call_patch_custom",
			Name:        string(toolspec.ToolPatch),
			Custom:      true,
			CustomInput: patchText,
		},
	}), true).cmd
	_ = m.runtimeAdapter().applyProjectedRuntimeEvent(projectRuntimeEvent(runtime.Event{
		Kind:   runtime.EventToolCallCompleted,
		StepID: "step-1",
		ToolResult: &tools.Result{
			CallID: "call_patch_custom",
			Name:   toolspec.ToolPatch,
			Output: []byte("{}"),
		},
	}), true).cmd

	ongoing := stripANSIAndTrimRight(m.view.OngoingSnapshot())
	if !strings.Contains(ongoing, "⇄ ./cli/app/ui_status.go -1 +2") || strings.Contains(ongoing, "Edited:") {
		t.Fatalf("expected custom patch summary in ongoing transcript, got %q", ongoing)
	}
	if strings.Contains(ongoing, "*** Begin Patch") || strings.Contains(ongoing, "+\tReady bool") {
		t.Fatalf("expected ongoing transcript to hide raw custom patch body, got %q", ongoing)
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	detailView := m.View()
	detailPlain := ansi.Strip(detailView)
	if !strings.Contains(detailPlain, "cli/app/ui_status.go") || !strings.Contains(detailPlain, "+    Ready bool") || !strings.Contains(detailPlain, "-    Summary string") {
		t.Fatalf("expected custom patch diff in detail transcript, got %q", detailPlain)
	}

	var addedLine string
	for _, line := range strings.Split(detailView, "\n") {
		if strings.Contains(ansi.Strip(line), "+    Ready bool") {
			addedLine = line
			break
		}
	}
	if addedLine == "" {
		t.Fatalf("expected highlighted added line in detail transcript, got %q", detailView)
	}
	const addBg = "\x1b[48;2;31;42;34m"
	if !strings.Contains(addedLine, addBg+"  ") {
		t.Fatalf("expected diff background highlight on custom patch detail line, got %q", addedLine)
	}
	readyIdx := strings.Index(addedLine, "Ready")
	if readyIdx < 0 {
		t.Fatalf("expected syntax-highlighted Ready token in custom patch detail line, got %q", addedLine)
	}
	bgIdx := strings.LastIndex(addedLine[:readyIdx], addBg)
	if bgIdx < 0 {
		t.Fatalf("expected diff background before syntax-highlighted custom patch token, got %q", addedLine)
	}
	if strings.Contains(addedLine[bgIdx:readyIdx], "\x1b[0") {
		t.Fatalf("expected no reset between diff background and first syntax-highlighted custom patch token, got %q", addedLine)
	}
	tailBgIdx := strings.LastIndex(addedLine, addBg)
	if tailBgIdx < 0 || !strings.Contains(addedLine[tailBgIdx:], strings.Repeat(" ", 16)) {
		t.Fatalf("expected diff background to continue through padded tail, got %q", addedLine)
	}
	if got := runewidth.StringWidth(ansi.Strip(addedLine)); got < viewportWidth {
		t.Fatalf("expected diff background line to span at least viewport width %d, got %d for %q", viewportWidth, got, addedLine)
	}
}
