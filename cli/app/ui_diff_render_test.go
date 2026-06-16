package app

import (
	"core/server/llm"
	"core/server/runtime"
	"core/server/tools"
	"core/shared/toolspec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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
}
