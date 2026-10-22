package ongoing

import (
	"io"
	"strings"

	"core/cli/tui/transcriptrender"

	xansi "github.com/charmbracelet/x/ansi"
)

type terminalRowRange struct {
	start int
	end   int
}

// previousMutableBandHeightAtBottom is the part of the previous band that
// still occupies the target bottom edge. On expansion terminal reflow owns the
// previous screen, while the logical band height remains unchanged so new rows
// never become mutable-band padding.
func (s *Surface) previousMutableBandHeightAtBottom(target Size) int {
	if s.lastPaintedSize == nil || s.retainedBandHeight <= 0 {
		return 0
	}
	if target.Height >= s.lastPaintedSize.Height {
		return min(s.retainedBandHeight, target.Height)
	}
	previous := s.visiblePreviousMutableBand(target)
	if previous == nil {
		return 0
	}
	return previous.end - previous.start + 1
}

func (s *Surface) visiblePreviousMutableBand(target Size) *terminalRowRange {
	if s.lastPaintedSize == nil || s.retainedBandHeight <= 0 {
		return nil
	}
	start := s.lastPaintedSize.Height - s.retainedBandHeight + 1
	end := min(s.lastPaintedSize.Height, target.Height)
	if start > end {
		return nil
	}
	return &terminalRowRange{start: start, end: end}
}

func (s *Surface) terminalHeightExpanded(target Size) bool {
	return s.lastPaintedSize != nil && target.Height > s.lastPaintedSize.Height
}

func (s *Surface) validateRenderFrame(frame FrameInput, operation string) {
	if frame.Size.Width <= 0 || frame.Size.Height <= 0 {
		panicOngoingDeveloperError(operation, "invalid terminal geometry", map[string]any{
			"width":  frame.Size.Width,
			"height": frame.Size.Height,
		})
	}
	if frame.Cursor.Visible {
		if frame.Cursor.Row <= 0 || frame.Cursor.Row > frame.Size.Height || frame.Cursor.Column <= 0 || frame.Cursor.Column > frame.Size.Width {
			panicOngoingDeveloperError(operation, "invalid cursor", map[string]any{
				"row":    frame.Cursor.Row,
				"column": frame.Cursor.Column,
				"width":  frame.Size.Width,
				"height": frame.Size.Height,
			})
		}
	}
}

func (s *Surface) writeFrameTransaction(frame FrameInput, immutableRows []string) (Result, error) {
	liveLayout, cursor := s.allocateLiveBand(frame)
	liveLayout = composeInputOverlay(frame, liveLayout)
	frame.Cursor = cursor
	liveLines := liveBandLineTexts(liveLayout)
	for row, line := range liveLines {
		if width := xansi.StringWidth(line); width > frame.Size.Width {
			panicOngoingDeveloperError("write_frame_transaction", "live row exceeds terminal width", map[string]any{"row": row, "width": width, "terminal_width": frame.Size.Width})
		}
	}
	frame.Cursor = cursorForVisibleLiveBand(frame.Cursor, frame.Size.Height, liveLayout)
	previousHeight := s.previousMutableBandHeightAtBottom(frame.Size)
	contentHeight := len(liveLines)
	releasableHeight := max(0, previousHeight-contentHeight)
	releasedHeight := min(len(immutableRows), releasableHeight)
	nextHeight := max(contentHeight, previousHeight-releasedHeight)
	validateFrameTransactionGeometry(frame, previousHeight, contentHeight, nextHeight, len(immutableRows))
	eraseHeight := max(previousHeight, nextHeight)
	var transaction strings.Builder
	transaction.WriteString(resetScrollRegionAndOriginMode())
	transaction.WriteString(semanticOutputSequence())
	if s.terminalHeightExpanded(frame.Size) {
		previous := s.visiblePreviousMutableBand(frame.Size)
		if s.terminalResize.usesWidthRehydration() && previous != nil {
			writeMutableRowsErase(&transaction, previous.start, previous.end)
		}
		if s.terminalResize.bottomAnchorsVerticalExpansion() && s.immutableScrollbackProduced() {
			writeImmutableRegionScrollForTerminalExpansion(
				&transaction,
				s.lastPaintedSize.Height,
				frame.Size.Height,
				previousHeight,
			)
		}
	}
	if nextHeight > previousHeight && s.immutableScrollbackProduced() {
		writeImmutableRegionScrollForLiveBandGrowth(&transaction, frame.Size.Height, previousHeight, nextHeight)
	}
	writeMutableBandErase(&transaction, frame.Size.Height, eraseHeight)
	writeImmutableRowsAboveMutableBand(
		&transaction,
		frame.Size.Height,
		previousHeight,
		nextHeight,
		immutableRows,
	)
	writeRetainedMutableBand(
		&transaction,
		frame.Size.Height,
		nextHeight,
		liveLines,
		s.terminalResize == TerminalResizeSemanticPrompt,
	)
	writeCursor(&transaction, frame.Cursor)
	if _, err := io.WriteString(s.writer, transaction.String()); err != nil {
		return Result{}, err
	}
	s.retainedBandHeight = nextHeight
	paintedSize := frame.Size
	s.lastPaintedSize = &paintedSize
	return Result{}, nil
}

func composeInputOverlay(frame FrameInput, layout []liveBandLine) []liveBandLine {
	if frame.Overlay == nil || len(layout) == 0 {
		return layout
	}
	for inputRow, line := range layout {
		if line.sectionKind != FrameSectionInput {
			continue
		}
		available := frame.Size.Height - (len(layout) - inputRow)
		height := min(frame.Overlay.PreferredHeight, available)
		if height < frame.Overlay.MinimumHeight {
			return layout
		}
		projected := frame.Overlay.Project(height)
		rows := frameLines(FrameInput{Theme: frame.Theme, Sections: []FrameSection{{
			Lines: projected.Lines, StyledLines: projected.StyledLines,
		}}})
		if len(rows) > height {
			panicOngoingDeveloperError("compose_input_overlay", "projection exceeds allocation", map[string]any{"height": height, "rows": len(rows)})
		}
		growth := max(0, len(rows)-inputRow)
		if growth > 0 {
			layout = append(make([]liveBandLine, growth), layout...)
			inputRow += growth
		}
		copy(layout[inputRow-len(rows):inputRow], rows)
		return layout
	}
	return layout
}

func validateFrameTransactionGeometry(
	frame FrameInput,
	previousHeight int,
	contentHeight int,
	nextHeight int,
	immutableRowCount int,
) {
	valid := previousHeight >= 0 &&
		contentHeight >= 0 &&
		contentHeight <= nextHeight &&
		nextHeight <= frame.Size.Height
	if frame.Cursor.Visible {
		valid = valid &&
			frame.Cursor.Row > 0 &&
			frame.Cursor.Row <= frame.Size.Height &&
			frame.Cursor.Column > 0 &&
			frame.Cursor.Column <= frame.Size.Width
	}
	if valid {
		return
	}
	sectionLineCounts := make(map[FrameSectionKind]int, len(frame.Sections))
	for _, section := range frame.Sections {
		sectionLineCounts[section.Kind] += len(section.Lines) + len(section.StyledLines)
	}
	panicOngoingDeveloperError("write_frame_transaction", "invalid frame transaction geometry", map[string]any{
		"width":               frame.Size.Width,
		"height":              frame.Size.Height,
		"previous_height":     previousHeight,
		"content_height":      contentHeight,
		"next_height":         nextHeight,
		"immutable_row_count": immutableRowCount,
		"section_line_counts": sectionLineCounts,
		"cursor":              frame.Cursor,
	})
}

func (s *Surface) allocateLiveBand(frame FrameInput) ([]liveBandLine, Cursor) {
	lines := activeAssistantLinesWithLinkPresentation(
		s.activeAssistant,
		frameWidthOrDefault(frame),
		frame.Theme,
		s.markdownLinks,
	)
	heights := make([]int, len(frame.Sections))
	minimums := make([]int, len(frame.Sections))
	total, minimum := len(lines), min(1, len(lines))
	for index, section := range frame.Sections {
		preferred := len(section.Lines) + len(section.StyledLines)
		floor := preferred
		if section.Demand != nil {
			preferred, floor = section.Demand.PreferredHeight, section.Demand.MinimumHeight
		} else {
			switch section.Kind {
			case FrameSectionPendingTools:
				floor = min(1, preferred)
			}
		}
		if floor < 0 || preferred < floor {
			panicOngoingDeveloperError("allocate_live_band", "invalid section demand", map[string]any{"kind": section.Kind, "preferred": preferred, "minimum": floor})
		}
		heights[index], minimums[index] = preferred, floor
		total += preferred
		minimum += floor
	}
	if minimum > frame.Size.Height {
		return nil, Cursor{}
	}
	deficit := max(0, total-frame.Size.Height)
	commentaryReduction := min(deficit, len(lines)-min(1, len(lines)))
	lines = lines[commentaryReduction:]
	deficit -= commentaryReduction
	for _, kind := range []FrameSectionKind{FrameSectionQueuedOrSteered, FrameSectionPendingTools, FrameSectionInput} {
		for index, section := range frame.Sections {
			if section.Kind == kind {
				reduction := min(deficit, heights[index]-minimums[index])
				heights[index] -= reduction
				deficit -= reduction
			}
		}
	}
	layout := make([]liveBandLine, 0, min(total, frame.Size.Height))
	for _, line := range lines {
		layout = append(layout, liveBandLine{text: line})
	}
	cursor := frame.Cursor
	for index, section := range frame.Sections {
		if section.Demand != nil {
			projected := section.Demand.Project(heights[index])
			section.Lines, section.StyledLines = projected.Lines, projected.StyledLines
			if section.Kind == FrameSectionInput {
				cursor = projected.Cursor
				if cursor.Visible {
					cursor.Target = &CursorTarget{SectionKind: section.Kind, Row: cursor.Row}
				}
			}
		}
		rows := frameLines(FrameInput{Theme: frame.Theme, Sections: []FrameSection{section}})
		if section.Demand != nil && len(rows) > heights[index] {
			panicOngoingDeveloperError("allocate_live_band", "projection exceeds allocation", map[string]any{"kind": section.Kind, "height": heights[index], "rows": len(rows)})
		}
		if len(rows) > heights[index] {
			rows = rows[len(rows)-heights[index]:]
		}
		layout = append(layout, rows...)
	}
	return layout, cursor
}

func activeAssistantLinesWithLinkPresentation(
	state activeAssistantState,
	width int,
	themeName string,
	linkPresentation transcriptrender.MarkdownLinkPresentation,
) []string {
	if state.source == "" {
		return nil
	}
	projection := newMarkdownProjector(nil, themeName, linkPresentation).Project(markdownProjectionInput{
		Source:           state.source,
		Width:            width,
		PromotedBoundary: state.promotedSourceBoundary,
	})
	if projection.ProjectionFailure != nil {
		panicOngoingDeveloperError("assistant_tail_render", "markdown projection instability", map[string]any{
			"source_boundary":    projection.ProjectionFailure.SourceBoundary,
			"candidate_boundary": projection.ProjectionFailure.CandidateBoundary,
			"row_index":          projection.ProjectionFailure.RowIndex,
			"width":              projection.ProjectionFailure.Width,
		})
	}
	return projection.VolatileRows
}

func frameWidthOrDefault(frame FrameInput) int {
	if frame.Size.Width > 0 {
		return frame.Size.Width
	}
	return 80
}

func frameLines(frame FrameInput) []liveBandLine {
	var lines []liveBandLine
	for _, section := range frame.Sections {
		for index, line := range section.StyledLines {
			lines = append(lines, liveBandLine{
				text:        encodeTranscriptLine(line, frame.Theme),
				sectionKind: section.Kind,
				sectionRow:  index + 1,
			})
		}
		for index, line := range section.Lines {
			lines = append(lines, liveBandLine{
				text:        line,
				sectionKind: section.Kind,
				sectionRow:  len(section.StyledLines) + index + 1,
			})
		}
	}
	return lines
}

func liveBandLineTexts(layout []liveBandLine) []string {
	if len(layout) == 0 {
		return nil
	}
	lines := make([]string, 0, len(layout))
	for _, line := range layout {
		lines = append(lines, line.text)
	}
	return lines
}

func cursorForVisibleLiveBand(cursor Cursor, terminalHeight int, layout []liveBandLine) Cursor {
	if !cursor.Visible || cursor.Target == nil {
		return cursor
	}
	for index, line := range layout {
		if line.sectionKind == cursor.Target.SectionKind && line.sectionRow == cursor.Target.Row {
			cursor.Row = terminalHeight - len(layout) + index + 1
			return cursor
		}
	}
	cursor.Visible = false
	return cursor
}
