package worktreeviewport

func RowsPerPage(termHeight int, headerLines int, footerLines int, rowLines int) int {
	if rowLines <= 0 {
		return 1
	}
	available := termHeight - 1 - headerLines - footerLines
	if available < rowLines {
		return 1
	}
	rows := available / rowLines
	if rows < 1 {
		return 1
	}
	return rows
}

func OverlayStartRow(selection int, rowCount int, contentHeight int, rowLines int) int {
	if selection < 0 || rowCount <= 0 || contentHeight <= 0 || rowLines <= 0 {
		return 0
	}
	visibleRows := contentHeight / rowLines
	if visibleRows < 1 {
		visibleRows = 1
	}
	startRow := 0
	if selection >= visibleRows {
		startRow = selection - visibleRows + 1
	}
	if startRow >= rowCount {
		startRow = rowCount - 1
	}
	if startRow < 0 {
		startRow = 0
	}
	return startRow * rowLines
}

func DialogVisibleStart(totalLines int, viewportHeight int, focusedStart int, focusedEnd int) int {
	if viewportHeight <= 0 || totalLines <= viewportHeight {
		return 0
	}
	if focusedStart < 0 {
		focusedStart = 0
	}
	if focusedEnd < focusedStart {
		focusedEnd = focusedStart
	}
	start := focusedStart
	maxStart := totalLines - viewportHeight
	if focusedEnd-focusedStart+1 >= viewportHeight {
		if start > maxStart {
			start = maxStart
		}
		if start < 0 {
			start = 0
		}
		return start
	}
	if start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	return start
}
