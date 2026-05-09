package tui

import (
	"strings"
)

type PendingSpinnerFrameFunc func(entry TranscriptEntry, entryIndex int) string

func RenderPendingToolSnapshot(entries []TranscriptEntry, theme string, width int, spinner string) string {
	return renderPendingToolSnapshotProjection(entries, theme, width, uniformPendingSpinnerFrame(spinner)).Render(TranscriptDivider)
}

func RenderPendingOngoingSnapshot(entries []TranscriptEntry, theme string, width int, spinner string) string {
	return renderPendingOngoingSnapshotProjection(entries, theme, width, uniformPendingSpinnerFrame(spinner)).Render(TranscriptDivider)
}

func RenderPendingToolSnapshotLines(entries []TranscriptEntry, theme string, width int, spinner string) []TranscriptProjectionLine {
	return renderPendingToolSnapshotProjection(entries, theme, width, uniformPendingSpinnerFrame(spinner)).Lines(TranscriptDivider)
}

func RenderPendingOngoingSnapshotLines(entries []TranscriptEntry, theme string, width int, spinner string) []TranscriptProjectionLine {
	return renderPendingOngoingSnapshotProjection(entries, theme, width, uniformPendingSpinnerFrame(spinner)).Lines(TranscriptDivider)
}

func RenderPendingToolSnapshotWithSpinnerFrames(entries []TranscriptEntry, theme string, width int, spinnerForEntry PendingSpinnerFrameFunc) string {
	return renderPendingToolSnapshotProjection(entries, theme, width, spinnerForEntry).Render(TranscriptDivider)
}

func RenderPendingToolSnapshotLinesWithSpinnerFrames(entries []TranscriptEntry, theme string, width int, spinnerForEntry PendingSpinnerFrameFunc) []TranscriptProjectionLine {
	return renderPendingToolSnapshotProjection(entries, theme, width, spinnerForEntry).Lines(TranscriptDivider)
}

func RenderPendingOngoingSnapshotWithSpinnerFrames(entries []TranscriptEntry, theme string, width int, spinnerForEntry PendingSpinnerFrameFunc) string {
	return renderPendingOngoingSnapshotProjection(entries, theme, width, spinnerForEntry).Render(TranscriptDivider)
}

func RenderPendingOngoingSnapshotLinesWithSpinnerFrames(entries []TranscriptEntry, theme string, width int, spinnerForEntry PendingSpinnerFrameFunc) []TranscriptProjectionLine {
	return renderPendingOngoingSnapshotProjection(entries, theme, width, spinnerForEntry).Lines(TranscriptDivider)
}

func uniformPendingSpinnerFrame(spinner string) PendingSpinnerFrameFunc {
	return func(TranscriptEntry, int) string {
		return spinner
	}
}

func renderPendingToolSnapshotProjection(entries []TranscriptEntry, theme string, width int, spinnerForEntry PendingSpinnerFrameFunc) TranscriptProjection {
	pending := PendingToolEntries(entries)
	if len(pending) == 0 {
		return TranscriptProjection{}
	}
	return renderPendingOngoingSnapshotProjection(pending, theme, width, spinnerForEntry)
}

func renderPendingOngoingSnapshotProjection(entries []TranscriptEntry, theme string, width int, spinnerForEntry PendingSpinnerFrameFunc) TranscriptProjection {
	if len(entries) == 0 {
		return TranscriptProjection{}
	}
	if width <= 0 {
		width = 120
	}
	model := transcriptProjectionRenderer(theme, width, 0)
	model.toolSymbolGap = 2
	model.transcriptInput.Entries = append([]TranscriptEntry(nil), entries...)
	blocks := model.buildOngoingBlocks(false)
	blocks = model.applyPendingSpinner(blocks, entries, spinnerForEntry)
	if len(blocks) == 0 {
		return TranscriptProjection{}
	}
	return projectionFromOngoingBlocks(blocks)
}

func (m Model) applyPendingSpinner(blocks []ongoingBlock, entries []TranscriptEntry, spinnerForEntry PendingSpinnerFrameFunc) []ongoingBlock {
	if spinnerForEntry == nil {
		return blocks
	}
	consumedResults := make(map[int]struct{})
	resultIndex := buildToolResultIndex(entries)
	out := make([]ongoingBlock, 0, len(blocks))
	for _, block := range blocks {
		if !m.shouldRenderPendingSpinner(block, entries, consumedResults, resultIndex) {
			out = append(out, block)
			continue
		}
		spinner := spinnerForEntry(entries[block.entryIndex], block.entryIndex)
		if strings.TrimSpace(spinner) == "" {
			out = append(out, block)
			continue
		}
		spinnerSymbol := styleForRole(block.role, m.palette()).Render(spinner) + " "
		rebuilt, ok := m.renderPendingSpinnerBlock(block, entries, spinnerSymbol)
		if !ok {
			out = append(out, block)
			continue
		}
		out = append(out, rebuilt)
	}
	return out
}

func (m Model) renderPendingSpinnerBlock(block ongoingBlock, entries []TranscriptEntry, spinnerSymbol string) (ongoingBlock, bool) {
	if block.entryIndex < 0 || block.entryIndex >= len(entries) {
		return ongoingBlock{}, false
	}
	entry := entries[block.entryIndex]
	if roleFromEntry(entry) != TranscriptRoleToolCall {
		return ongoingBlock{}, false
	}
	lines := block.lines
	if isAskQuestionToolCall(entry.ToolCall) {
		question, suggestions, recommendedOptionIndex := askQuestionDisplay(entry.ToolCall, entry.Text)
		lines = m.flattenAskQuestionEntryWithSymbol(block.role, question, suggestions, recommendedOptionIndex, "", false, spinnerSymbol)
	} else {
		combined := m.toolCallDisplayText(entry, block.role, transcriptBlockOptions{mode: transcriptBlockModeOngoing})
		lines = m.flattenEntryWithMetaAndSymbol(block.role, combined, true, entry.ToolCall, spinnerSymbol)
	}
	lines = m.ongoingToolWithTreeGuideWithSymbol(block.role, lines, spinnerSymbol)
	return ongoingBlock{role: block.role, lines: lines, entryIndex: block.entryIndex, entryEnd: block.entryEnd}, true
}

func (m Model) shouldRenderPendingSpinner(block ongoingBlock, entries []TranscriptEntry, consumedResults map[int]struct{}, resultIndex toolResultIndex) bool {
	if !isToolHeadlineRole(block.role) || len(block.lines) == 0 {
		return false
	}
	if block.entryIndex < 0 || block.entryIndex >= len(entries) {
		return false
	}
	entry := entries[block.entryIndex]
	if roleFromEntry(entry) != TranscriptRoleToolCall {
		return false
	}
	resultIdx := resultIndex.findMatchingToolResultIndex(entries, block.entryIndex, consumedResults)
	if resultIdx < 0 {
		return true
	}
	consumedResults[resultIdx] = struct{}{}
	return false
}
