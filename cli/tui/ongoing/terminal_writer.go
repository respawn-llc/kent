package ongoing

import (
	"fmt"
	"strings"
)

func resetScrollRegionAndOriginMode() string {
	return "\x1b[r\x1b[?6l"
}

func semanticOutputSequence() string {
	return "\x1b]133;C\x1b\\"
}

func redrawableSemanticPromptSequence() string {
	return "\x1b]133;A;redraw=1\x1b\\"
}

func writeMutableBandErase(builder *strings.Builder, terminalHeight, bandHeight int) {
	if bandHeight <= 0 {
		return
	}
	writeMutableRowsErase(builder, terminalHeight-bandHeight+1, terminalHeight)
}

func writeMutableRowsErase(builder *strings.Builder, startRow, endRow int) {
	for row := startRow; row <= endRow; row++ {
		fmt.Fprintf(builder, "\x1b[%d;1H", row)
		// Retire semantic prompt metadata before this row can rejoin the
		// immutable area; erasing cells alone does not clear that metadata.
		builder.WriteString(semanticOutputSequence())
		builder.WriteString("\x1b[2K")
	}
}

func writeImmutableRegionScrollForLiveBandGrowth(builder *strings.Builder, terminalHeight, previousBandHeight, nextBandHeight int) {
	delta := nextBandHeight - previousBandHeight
	if delta <= 0 {
		return
	}
	oldImmutableBottom := terminalHeight - previousBandHeight
	if oldImmutableBottom == 1 {
		// Equal top/bottom margins are invalid. Retire the sole immutable row
		// before the disposable mutable rows are erased and repainted.
		builder.WriteString(resetScrollRegionAndOriginMode())
		fmt.Fprintf(builder, "\x1b[%d;1H\r\n", terminalHeight)
		return
	}
	writeImmutableRegionScroll(builder, oldImmutableBottom, delta)
}

func writeImmutableRegionScrollForTerminalExpansion(
	builder *strings.Builder,
	previousTerminalHeight int,
	terminalHeight int,
	previousBandHeight int,
) {
	writeImmutableRegionScroll(
		builder,
		terminalHeight-previousBandHeight,
		terminalHeight-previousTerminalHeight,
	)
}

func writeImmutableRegionScroll(builder *strings.Builder, immutableBottom, rows int) {
	if immutableBottom < 2 || rows <= 0 {
		return
	}
	if rows > immutableBottom {
		rows = immutableBottom
	}
	fmt.Fprintf(builder, "\x1b[1;%dr\x1b[%d;1H", immutableBottom, immutableBottom)
	for range rows {
		builder.WriteString("\r\n")
	}
	builder.WriteString(resetScrollRegionAndOriginMode())
}

func writeRetainedMutableBand(
	builder *strings.Builder,
	terminalHeight int,
	retainedHeight int,
	lines []string,
	redrawableSemanticPrompt bool,
) {
	if retainedHeight <= 0 {
		return
	}
	retainedStartRow := terminalHeight - retainedHeight + 1
	fmt.Fprintf(builder, "\x1b[%d;1H", retainedStartRow)
	if redrawableSemanticPrompt {
		// At the left margin OSC 133 A marks the complete retained region without
		// advancing. Supporting terminals clear the region before resize reflow.
		builder.WriteString(redrawableSemanticPromptSequence())
	}
	startRow := terminalHeight - len(lines) + 1
	for index, line := range lines {
		row := startRow + index
		if index > 0 || row != retainedStartRow {
			fmt.Fprintf(builder, "\x1b[%d;1H", row)
		}
		builder.WriteString(line)
	}
}

func writeImmutableRowsAboveMutableBand(
	builder *strings.Builder,
	terminalHeight int,
	previousBandHeight int,
	bandHeight int,
	rows []string,
) {
	if len(rows) == 0 {
		return
	}
	bottom := terminalHeight - bandHeight
	if bottom <= 1 {
		builder.WriteString(resetScrollRegionAndOriginMode())
		if bottom == 1 && previousBandHeight < terminalHeight {
			fmt.Fprintf(builder, "\x1b[%d;1H\r\n", terminalHeight)
		}
		// The mutable area has already been erased. Start at the top without
		// a leading blank row; scrolling after the payload places every new
		// immutable row above the final band, including at full height.
		builder.WriteString("\x1b[1;1H")
		for index, row := range rows {
			if index > 0 {
				builder.WriteString("\r\n")
			}
			builder.WriteString(row)
		}
		for range terminalHeight - bottom {
			builder.WriteString("\r\n")
		}
		return
	}
	previousBottom := terminalHeight - min(previousBandHeight, terminalHeight)
	appendRow := bottom
	if previousBottom >= 1 && previousBottom < bottom {
		appendRow = previousBottom
	}
	fmt.Fprintf(builder, "\x1b[1;%dr\x1b[%d;1H", bottom, appendRow)
	for _, row := range rows {
		builder.WriteString("\r\n")
		builder.WriteString(row)
	}
	builder.WriteString(resetScrollRegionAndOriginMode())
}

func writeCursor(builder *strings.Builder, cursor Cursor) {
	if !cursor.Visible {
		builder.WriteString("\x1b[?25l")
		return
	}
	fmt.Fprintf(builder, "\x1b[%d;%dH\x1b[?25h", cursor.Row, cursor.Column)
}
