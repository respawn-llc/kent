package app

import (
	"strings"
	"unicode"

	"builder/cli/tui"
)

type nativeAssistantStreamController struct {
	theme  string
	width  int
	source string

	rendered                []tui.TranscriptProjectionLine
	enqueuedStableLineCount int
	invalidatedByResize     bool
}

type nativeAssistantStreamUpdate struct {
	stable      []tui.TranscriptProjectionLine
	tail        []tui.TranscriptProjectionLine
	needsReplay bool
	done        bool
}

func newNativeAssistantStreamController(theme string, width int) nativeAssistantStreamController {
	return nativeAssistantStreamController{
		theme: theme,
		width: normalizedNativeStreamWidth(width),
	}
}

func (c *nativeAssistantStreamController) ApplySource(source string, theme string, width int) nativeAssistantStreamUpdate {
	c.Configure(theme, width)
	if strings.TrimSpace(source) == "" {
		return nativeAssistantStreamUpdate{}
	}
	if c.source != "" && !strings.HasPrefix(source, c.source) {
		c.invalidatedByResize = c.enqueuedStableLineCount > 0
		c.enqueuedStableLineCount = 0
	}
	c.source = source
	c.rendered = tui.RenderAssistantMarkdownProjection(c.source, c.theme, c.width)
	return c.updateForStableTarget(c.stableTarget(false), false)
}

func (c *nativeAssistantStreamController) Append(delta string) nativeAssistantStreamUpdate {
	if delta == "" && c.source == "" {
		return nativeAssistantStreamUpdate{}
	}
	c.source += delta
	c.rendered = tui.RenderAssistantMarkdownProjection(c.source, c.theme, c.width)
	return c.updateForStableTarget(c.stableTarget(false), false)
}

func (c *nativeAssistantStreamController) Finalize() nativeAssistantStreamUpdate {
	c.rendered = tui.RenderAssistantMarkdownProjection(c.source, c.theme, c.width)
	update := c.updateForStableTarget(len(c.rendered), true)
	*c = newNativeAssistantStreamController(c.theme, c.width)
	return update
}

func (c *nativeAssistantStreamController) Configure(theme string, width int) {
	width = normalizedNativeStreamWidth(width)
	if theme == "" {
		theme = "dark"
	}
	if width == c.width && theme == c.theme {
		return
	}
	hadStable := c.enqueuedStableLineCount > 0
	c.width = width
	c.theme = theme
	if c.source != "" {
		c.invalidatedByResize = c.invalidatedByResize || hadStable
		c.rendered = tui.RenderAssistantMarkdownProjection(c.source, c.theme, c.width)
	}
}

func (c *nativeAssistantStreamController) updateForStableTarget(stableTarget int, done bool) nativeAssistantStreamUpdate {
	if c.invalidatedByResize && !done {
		stableTarget = c.enqueuedStableLineCount
	}
	stableTarget = clampNativeStreamLineCount(stableTarget, c.enqueuedStableLineCount, len(c.rendered))
	stable := cloneNativeStreamProjectionLines(c.rendered[c.enqueuedStableLineCount:stableTarget])
	c.enqueuedStableLineCount = stableTarget
	tail := cloneNativeStreamProjectionLines(c.rendered[c.enqueuedStableLineCount:])
	if c.invalidatedByResize {
		tail = cloneNativeStreamProjectionLines(c.rendered)
	}
	if done {
		tail = nil
	}
	return nativeAssistantStreamUpdate{
		stable:      stable,
		tail:        tail,
		needsReplay: c.invalidatedByResize,
		done:        done,
	}
}

func (c nativeAssistantStreamController) stableTarget(final bool) int {
	if final {
		return len(c.rendered)
	}
	completeSource := completeNativeStreamSource(c.source)
	target := len(tui.RenderAssistantMarkdownProjection(completeSource, c.theme, c.width))
	if holdbackStart, ok := activeNativeStreamMarkdownHoldbackStart(c.source); ok {
		holdbackTarget := len(tui.RenderAssistantMarkdownProjection(c.source[:holdbackStart], c.theme, c.width))
		if holdbackTarget < target {
			target = holdbackTarget
		}
	}
	if tableStart, ok := activeNativeStreamTableStart(c.source); ok {
		tableTarget := len(tui.RenderAssistantMarkdownProjection(c.source[:tableStart], c.theme, c.width))
		if tableTarget < target {
			target = tableTarget
		}
	}
	return target
}

func completeNativeStreamSource(source string) string {
	idx := strings.LastIndex(source, "\n")
	if idx < 0 {
		return ""
	}
	return source[:idx+1]
}

func activeNativeStreamMarkdownHoldbackStart(source string) (int, bool) {
	lines := completeNativeStreamLines(source)
	definitions := nativeStreamReferenceDefinitions(lines)
	holdStart := -1
	inFence := false
	lastNonBlank := -1
	for idx, sourceLine := range lines {
		line := strings.TrimSpace(sourceLine.text)
		if isNativeStreamFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if line == "" {
			continue
		}
		lastNonBlank = idx
		if nativeStreamReferenceDefinitionLabel(line) != "" {
			continue
		}
		for _, label := range nativeStreamReferenceLinkLabels(sourceLine.text) {
			if _, ok := definitions[label]; !ok {
				holdStart = minNativeStreamHoldbackStart(holdStart, sourceLine.start)
			}
		}
	}
	if lastNonBlank >= 0 && lastNonBlank == len(lines)-1 {
		line := strings.TrimSpace(lines[lastNonBlank].text)
		if nativeStreamCanBecomeSetextHeading(line) {
			holdStart = minNativeStreamHoldbackStart(holdStart, lines[lastNonBlank].start)
		}
	}
	if holdStart >= 0 {
		return holdStart, true
	}
	return 0, false
}

func nativeStreamReferenceDefinitions(lines []nativeStreamSourceLine) map[string]struct{} {
	definitions := make(map[string]struct{})
	inFence := false
	for _, sourceLine := range lines {
		line := strings.TrimSpace(sourceLine.text)
		if isNativeStreamFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if label := nativeStreamReferenceDefinitionLabel(line); label != "" {
			definitions[label] = struct{}{}
		}
	}
	return definitions
}

func nativeStreamReferenceDefinitionLabel(line string) string {
	if strings.HasPrefix(line, "[^") {
		return ""
	}
	closeIdx := strings.Index(line, "]:")
	if !strings.HasPrefix(line, "[") || closeIdx <= 1 {
		return ""
	}
	label := strings.TrimSpace(line[1:closeIdx])
	if label == "" || strings.Contains(label, "]") {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(label)), " ")
}

func nativeStreamReferenceLinkLabels(line string) []string {
	line = stripNativeStreamCodeSpans(line)
	labels := make([]string, 0, 1)
	for idx := 0; idx < len(line); idx++ {
		if line[idx] != '[' || (idx > 0 && line[idx-1] == '!') {
			continue
		}
		firstEnd := strings.IndexByte(line[idx+1:], ']')
		if firstEnd < 0 {
			continue
		}
		firstEnd += idx + 1
		firstLabel := strings.TrimSpace(line[idx+1 : firstEnd])
		if firstLabel == "" || strings.HasPrefix(firstLabel, "^") {
			idx = firstEnd
			continue
		}
		next := firstEnd + 1
		if next < len(line) && line[next] == '(' {
			idx = firstEnd
			continue
		}
		if next < len(line) && line[next] == '[' {
			secondEnd := strings.IndexByte(line[next+1:], ']')
			if secondEnd < 0 {
				idx = firstEnd
				continue
			}
			secondEnd += next + 1
			secondLabel := strings.TrimSpace(line[next+1 : secondEnd])
			if secondLabel == "" {
				secondLabel = firstLabel
			}
			if secondLabel != "" && !strings.HasPrefix(secondLabel, "^") {
				labels = append(labels, strings.Join(strings.Fields(strings.ToLower(secondLabel)), " "))
			}
			idx = secondEnd
			continue
		}
		if isNativeStreamTaskMarker(line, idx, firstEnd) {
			idx = firstEnd
			continue
		}
		labels = append(labels, strings.Join(strings.Fields(strings.ToLower(firstLabel)), " "))
		idx = firstEnd
	}
	return labels
}

func stripNativeStreamCodeSpans(line string) string {
	var out strings.Builder
	inCode := false
	for _, r := range line {
		if r == '`' {
			inCode = !inCode
			out.WriteRune(' ')
			continue
		}
		if inCode {
			out.WriteRune(' ')
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func isNativeStreamTaskMarker(line string, openIdx int, closeIdx int) bool {
	label := strings.TrimSpace(line[openIdx+1 : closeIdx])
	if label != "" && !strings.EqualFold(label, "x") {
		return false
	}
	prefix := strings.TrimSpace(line[:openIdx])
	return strings.HasSuffix(prefix, "-") || strings.HasSuffix(prefix, "*") || strings.HasSuffix(prefix, "+")
}

func nativeStreamCanBecomeSetextHeading(line string) bool {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ">") {
		return false
	}
	if isNativeStreamFenceLine(line) || isNativeStreamTableDelimiterLine(line) || isNativeStreamThematicBreakLine(line) {
		return false
	}
	if isNativeStreamListMarkerLine(line) {
		return false
	}
	return true
}

func isNativeStreamListMarkerLine(line string) bool {
	trimmed := strings.TrimLeftFunc(line, unicode.IsSpace)
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ ") {
		return true
	}
	digitCount := 0
	for _, r := range trimmed {
		if !unicode.IsDigit(r) {
			break
		}
		digitCount++
	}
	if digitCount == 0 || digitCount >= len(trimmed) {
		return false
	}
	marker := trimmed[digitCount]
	return (marker == '.' || marker == ')') && digitCount+1 < len(trimmed) && unicode.IsSpace(rune(trimmed[digitCount+1]))
}

func isNativeStreamThematicBreakLine(line string) bool {
	if line == "" {
		return false
	}
	marker := rune(0)
	count := 0
	for _, r := range line {
		if unicode.IsSpace(r) {
			continue
		}
		if r != '-' && r != '_' && r != '*' {
			return false
		}
		if marker == 0 {
			marker = r
		}
		if r != marker {
			return false
		}
		count++
	}
	return count >= 3
}

func minNativeStreamHoldbackStart(current int, candidate int) int {
	if current < 0 || candidate < current {
		return candidate
	}
	return current
}

func activeNativeStreamTableStart(source string) (int, bool) {
	lines := completeNativeStreamLines(source)
	inFence := false
	tableStart := -1
	for idx := 0; idx < len(lines); idx++ {
		line := strings.TrimSpace(lines[idx].text)
		if isNativeStreamFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if tableStart >= 0 {
			if isNativeStreamTableLine(line) {
				continue
			}
			tableStart = -1
			continue
		}
		if idx+1 < len(lines) && isNativeStreamTableHeaderLine(line) && isNativeStreamTableDelimiterLine(strings.TrimSpace(lines[idx+1].text)) {
			tableStart = lines[idx].start
			idx++
		}
	}
	if tableStart >= 0 {
		return tableStart, true
	}
	return 0, false
}

type nativeStreamSourceLine struct {
	text  string
	start int
}

func completeNativeStreamLines(source string) []nativeStreamSourceLine {
	lines := make([]nativeStreamSourceLine, 0, strings.Count(source, "\n"))
	start := 0
	for idx, r := range source {
		if r != '\n' {
			continue
		}
		lines = append(lines, nativeStreamSourceLine{text: source[start:idx], start: start})
		start = idx + 1
	}
	return lines
}

func isNativeStreamFenceLine(line string) bool {
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

func isNativeStreamTableHeaderLine(line string) bool {
	return strings.Contains(line, "|") && strings.Trim(line, "| ") != ""
}

func isNativeStreamTableLine(line string) bool {
	return strings.Contains(line, "|") && strings.TrimSpace(line) != ""
}

func isNativeStreamTableDelimiterLine(line string) bool {
	cells := strings.Split(strings.Trim(line, "| "), "|")
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		cell = strings.Trim(cell, ":")
		if len(cell) < 3 || strings.Trim(cell, "-") != "" {
			return false
		}
	}
	return true
}

func normalizedNativeStreamWidth(width int) int {
	if width <= 0 {
		return 120
	}
	return width
}

func clampNativeStreamLineCount(value int, minValue int, maxValue int) int {
	if minValue > maxValue {
		minValue = maxValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func cloneNativeStreamProjectionLines(lines []tui.TranscriptProjectionLine) []tui.TranscriptProjectionLine {
	if len(lines) == 0 {
		return nil
	}
	return append([]tui.TranscriptProjectionLine(nil), lines...)
}
