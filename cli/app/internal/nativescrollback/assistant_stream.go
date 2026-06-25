package nativescrollback

import (
	"strings"
	"unicode"

	"core/cli/tui"
)

type AssistantStreamInput struct {
	Source string
	Theme  string
	Width  int
}

type AssistantStreamUpdate struct {
	Stable          []tui.TranscriptProjectionLine
	Live            []tui.TranscriptProjectionLine
	StableLineCount int
	NeedsReplay     bool
	Done            bool
}

type AssistantStreamState struct {
	StepID                string
	Source                string
	Theme                 string
	Width                 int
	NeedsReplay           bool
	ScheduledStableLines  int
	AckedStableLines      int
	CommitRangeSet        bool
	CommitStartEntryCount int
	CommitEndEntryCount   int
}

type AssistantCommitEntry struct {
	Role string
	Text string
}

type AssistantCommitCandidate struct {
	StepID          string
	StartEntryCount int
	Entries         []AssistantCommitEntry
}

type AssistantCommitBinding struct {
	StartEntryCount int
	EndEntryCount   int
}

type assistantStableFlush struct {
	sequence        Sequence
	stableLineCount int
}

type assistantStreamLedger struct {
	theme  string
	width  int
	source string

	rendered                 []tui.TranscriptProjectionLine
	scheduledStableLineCount int
	ackedStableLineCount     int
	needsReplay              bool
	stableFlushes            []assistantStableFlush
	stepID                   string
	commitStartEntryCount    int
	commitEndEntryCount      int
	commitRangeSet           bool
}

func (l *Ledger) ApplyAssistantStreamSource(input AssistantStreamInput) AssistantStreamUpdate {
	if l == nil {
		return AssistantStreamUpdate{}
	}
	return l.assistant.applySource(input)
}

func (l *Ledger) FinalizeAssistantStreamSource(input AssistantStreamInput) AssistantStreamUpdate {
	if l == nil {
		return AssistantStreamUpdate{}
	}
	return l.assistant.finalizeSource(input)
}

func (l *Ledger) BindAssistantStableFlush(sequence Sequence, stableLineCount int) {
	if l == nil || sequence == 0 {
		return
	}
	l.assistant.bindStableFlush(sequence, stableLineCount)
}

func (l *Ledger) AssistantStreamLiveLines() []tui.TranscriptProjectionLine {
	if l == nil {
		return nil
	}
	return l.assistant.liveLines()
}

func (l *Ledger) AssistantStreamLiveLinesFor(input AssistantStreamInput) []tui.TranscriptProjectionLine {
	if l == nil {
		return nil
	}
	return l.assistant.liveLinesFor(input)
}

func (l *Ledger) ResetAssistantStream() {
	if l == nil {
		return
	}
	l.assistant = assistantStreamLedger{}
}

func (l *Ledger) ResetAssistantStreamRenderingState() {
	if l == nil {
		return
	}
	l.assistant.resetRenderingState()
}

func (l *Ledger) SetAssistantStreamStepID(stepID string) {
	if l == nil {
		return
	}
	l.assistant.setStepID(stepID)
}

func (l *Ledger) ObserveAssistantCommitCandidate(candidate AssistantCommitCandidate) (AssistantCommitBinding, bool) {
	if l == nil {
		return AssistantCommitBinding{}, false
	}
	return l.assistant.observeCommitCandidate(candidate)
}

func (l *Ledger) AssistantStreamState() AssistantStreamState {
	if l == nil {
		return AssistantStreamState{}
	}
	return l.assistant.state()
}

func (l *Ledger) AssistantSuffixCanFinalizeText(text string) bool {
	if l == nil {
		return false
	}
	return l.assistant.suffixCanFinalizeText(text)
}

func (s *assistantStreamLedger) applySource(input AssistantStreamInput) AssistantStreamUpdate {
	s.configure(input.Theme, input.Width)
	source := input.Source
	if strings.TrimSpace(source) == "" {
		return AssistantStreamUpdate{}
	}
	if s.source != "" && !strings.HasPrefix(source, s.source) {
		s.needsReplay = s.scheduledStableLineCount > 0
		s.scheduledStableLineCount = 0
		s.ackedStableLineCount = 0
		s.stableFlushes = nil
	}
	s.source = source
	s.rendered = tui.RenderAssistantMarkdownProjection(s.source, s.theme, s.width)
	return s.updateForStableTarget(s.stableTarget(false), false)
}

func (s *assistantStreamLedger) finalizeSource(input AssistantStreamInput) AssistantStreamUpdate {
	s.configure(input.Theme, input.Width)
	source := input.Source
	if strings.TrimSpace(source) != "" {
		rendered := tui.RenderAssistantMarkdownProjection(source, s.theme, s.width)
		if source != s.source && s.scheduledPrefixDivergesFrom(rendered) {
			s.source = source
			s.rendered = rendered
			s.needsReplay = true
			return s.updateForStableTarget(len(s.rendered), true)
		}
		s.source = source
		s.rendered = rendered
	}
	if strings.TrimSpace(s.source) == "" {
		return AssistantStreamUpdate{}
	}
	if len(s.rendered) == 0 {
		s.rendered = tui.RenderAssistantMarkdownProjection(s.source, s.theme, s.width)
	}
	return s.updateForStableTarget(len(s.rendered), true)
}

func (s assistantStreamLedger) scheduledPrefixDivergesFrom(rendered []tui.TranscriptProjectionLine) bool {
	count := s.scheduledStableLineCount
	if count <= 0 {
		return false
	}
	if count > len(s.rendered) || count > len(rendered) {
		return true
	}
	for idx := 0; idx < count; idx++ {
		if s.rendered[idx].Kind != rendered[idx].Kind || s.rendered[idx].Text != rendered[idx].Text {
			return true
		}
	}
	return false
}

func (s *assistantStreamLedger) bindStableFlush(sequence Sequence, stableLineCount int) {
	if stableLineCount <= s.scheduledStableLineCount {
		return
	}
	renderedLen := len(s.rendered)
	if stableLineCount > renderedLen {
		stableLineCount = renderedLen
	}
	if stableLineCount < s.scheduledStableLineCount {
		stableLineCount = s.scheduledStableLineCount
	}
	s.scheduledStableLineCount = stableLineCount
	s.stableFlushes = append(s.stableFlushes, assistantStableFlush{
		sequence:        sequence,
		stableLineCount: stableLineCount,
	})
}

func (s *assistantStreamLedger) setStepID(stepID string) {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" || stepID == s.stepID {
		return
	}
	s.stepID = stepID
	s.commitRangeSet = false
	s.commitStartEntryCount = 0
	s.commitEndEntryCount = 0
}

func (s *assistantStreamLedger) resetRenderingState() {
	source := s.source
	theme := s.theme
	width := s.width
	stepID := s.stepID
	commitRangeSet := s.commitRangeSet
	commitStartEntryCount := s.commitStartEntryCount
	commitEndEntryCount := s.commitEndEntryCount
	rendered := []tui.TranscriptProjectionLine(nil)
	if strings.TrimSpace(source) != "" {
		rendered = tui.RenderAssistantMarkdownProjection(source, theme, width)
	}
	*s = assistantStreamLedger{
		theme:                 theme,
		width:                 width,
		source:                source,
		rendered:              rendered,
		stepID:                stepID,
		commitRangeSet:        commitRangeSet,
		commitStartEntryCount: commitStartEntryCount,
		commitEndEntryCount:   commitEndEntryCount,
	}
}

func (s *assistantStreamLedger) observeCommitCandidate(candidate AssistantCommitCandidate) (AssistantCommitBinding, bool) {
	streamStepID := strings.TrimSpace(s.stepID)
	candidateStepID := strings.TrimSpace(candidate.StepID)
	if streamStepID != "" && streamStepID != candidateStepID {
		return AssistantCommitBinding{}, false
	}
	assistantStart := -1
	assistantEnd := -1
	for idx, entry := range candidate.Entries {
		if tui.TranscriptRoleFromWire(entry.Role) != tui.TranscriptRoleAssistant {
			continue
		}
		if assistantStart >= 0 {
			s.commitRangeSet = false
			return AssistantCommitBinding{}, false
		}
		assistantStart = candidate.StartEntryCount + idx
		assistantEnd = assistantStart + 1
	}
	if assistantStart < 0 {
		return AssistantCommitBinding{}, false
	}
	s.commitStartEntryCount = assistantStart
	s.commitEndEntryCount = assistantEnd
	s.commitRangeSet = true
	return AssistantCommitBinding{StartEntryCount: assistantStart, EndEntryCount: assistantEnd}, true
}

func (s assistantStreamLedger) state() AssistantStreamState {
	return AssistantStreamState{
		StepID:                s.stepID,
		Source:                s.source,
		Theme:                 s.theme,
		Width:                 s.width,
		NeedsReplay:           s.needsReplay,
		ScheduledStableLines:  s.scheduledStableLineCount,
		AckedStableLines:      s.ackedStableLineCount,
		CommitRangeSet:        s.commitRangeSet,
		CommitStartEntryCount: s.commitStartEntryCount,
		CommitEndEntryCount:   s.commitEndEntryCount,
	}
}

func (s assistantStreamLedger) suffixCanFinalizeText(text string) bool {
	if strings.TrimSpace(s.stepID) != "" {
		return false
	}
	source := strings.TrimSpace(s.source)
	if source == "" {
		return false
	}
	return source == strings.TrimSpace(text)
}

func (s *assistantStreamLedger) ackStableFlush(sequence Sequence) {
	if sequence == 0 || len(s.stableFlushes) == 0 {
		return
	}
	remaining := s.stableFlushes[:0]
	for _, flush := range s.stableFlushes {
		if flush.sequence <= sequence {
			if flush.stableLineCount > s.ackedStableLineCount {
				s.ackedStableLineCount = flush.stableLineCount
			}
			continue
		}
		remaining = append(remaining, flush)
	}
	s.stableFlushes = remaining
}

func (s assistantStreamLedger) liveLines() []tui.TranscriptProjectionLine {
	if len(s.rendered) == 0 {
		return nil
	}
	if s.needsReplay {
		return cloneAssistantStreamProjectionLines(s.rendered)
	}
	start := s.ackedStableLineCount
	if start < 0 {
		start = 0
	}
	if start > len(s.rendered) {
		start = len(s.rendered)
	}
	return cloneAssistantStreamProjectionLines(s.rendered[start:])
}

func (s assistantStreamLedger) liveLinesFor(input AssistantStreamInput) []tui.TranscriptProjectionLine {
	source := input.Source
	if strings.TrimSpace(source) == "" {
		return nil
	}
	theme := input.Theme
	if theme == "" {
		theme = "dark"
	}
	width := normalizedAssistantStreamWidth(input.Width)
	if source == s.source && theme == s.theme && width == s.width {
		return s.liveLines()
	}
	if source == s.source && (s.scheduledStableLineCount > 0 || s.ackedStableLineCount > 0 || s.needsReplay) {
		return s.liveLines()
	}
	return cloneAssistantStreamProjectionLines(tui.RenderAssistantMarkdownProjection(source, theme, width))
}

func (s *assistantStreamLedger) configure(theme string, width int) {
	width = normalizedAssistantStreamWidth(width)
	if theme == "" {
		theme = "dark"
	}
	if width == s.width && theme == s.theme {
		return
	}
	hadStable := s.scheduledStableLineCount > 0
	s.width = width
	s.theme = theme
	if s.source != "" {
		s.needsReplay = s.needsReplay || hadStable
		s.rendered = tui.RenderAssistantMarkdownProjection(s.source, s.theme, s.width)
	}
}

func (s *assistantStreamLedger) updateForStableTarget(stableTarget int, done bool) AssistantStreamUpdate {
	renderedLen := len(s.rendered)
	if s.needsReplay && !done {
		stableTarget = s.scheduledStableLineCount
	}
	committed := min(s.scheduledStableLineCount, renderedLen)
	stableTarget = clampAssistantStreamLineCount(stableTarget, committed, renderedLen)
	stable := cloneAssistantStreamProjectionLines(s.rendered[committed:stableTarget])
	if done {
		return AssistantStreamUpdate{
			Stable:          stable,
			Live:            nil,
			StableLineCount: stableTarget,
			NeedsReplay:     s.needsReplay,
			Done:            true,
		}
	}
	return AssistantStreamUpdate{
		Stable:          stable,
		Live:            s.liveLines(),
		StableLineCount: stableTarget,
		NeedsReplay:     s.needsReplay,
	}
}

func (s assistantStreamLedger) stableTarget(final bool) int {
	if final {
		return len(s.rendered)
	}
	completeSource := completeAssistantStreamSource(s.source)
	target := len(tui.RenderAssistantMarkdownProjection(completeSource, s.theme, s.width))
	if holdbackStart, ok := activeAssistantStreamMarkdownHoldbackStart(s.source); ok {
		holdbackTarget := len(tui.RenderAssistantMarkdownProjection(s.source[:holdbackStart], s.theme, s.width))
		if holdbackTarget < target {
			target = holdbackTarget
		}
	}
	if tableStart, ok := activeAssistantStreamTableStart(s.source); ok {
		tableTarget := len(tui.RenderAssistantMarkdownProjection(s.source[:tableStart], s.theme, s.width))
		if tableTarget < target {
			target = tableTarget
		}
	}
	return target
}

func completeAssistantStreamSource(source string) string {
	idx := strings.LastIndex(source, "\n")
	if idx < 0 {
		return ""
	}
	return source[:idx+1]
}

func activeAssistantStreamMarkdownHoldbackStart(source string) (int, bool) {
	lines := completeAssistantStreamLines(source)
	definitions := assistantStreamReferenceDefinitions(lines)
	holdStart := -1
	inFence := false
	lastNonBlank := -1
	for idx, sourceLine := range lines {
		line := strings.TrimSpace(sourceLine.text)
		if isAssistantStreamFenceLine(line) {
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
		if assistantStreamReferenceDefinitionLabel(line) != "" {
			continue
		}
		for _, label := range assistantStreamReferenceLinkLabels(sourceLine.text) {
			if _, ok := definitions[label]; !ok {
				holdStart = minAssistantStreamHoldbackStart(holdStart, sourceLine.start)
			}
		}
	}
	if lastNonBlank >= 0 && lastNonBlank == len(lines)-1 {
		line := strings.TrimSpace(lines[lastNonBlank].text)
		if assistantStreamCanBecomeSetextHeading(line) {
			holdStart = minAssistantStreamHoldbackStart(holdStart, lines[lastNonBlank].start)
		}
	}
	if holdStart >= 0 {
		return holdStart, true
	}
	return 0, false
}

func assistantStreamReferenceDefinitions(lines []assistantStreamSourceLine) map[string]struct{} {
	definitions := make(map[string]struct{})
	inFence := false
	for _, sourceLine := range lines {
		line := strings.TrimSpace(sourceLine.text)
		if isAssistantStreamFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if label := assistantStreamReferenceDefinitionLabel(line); label != "" {
			definitions[label] = struct{}{}
		}
	}
	return definitions
}

func assistantStreamReferenceDefinitionLabel(line string) string {
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

func assistantStreamReferenceLinkLabels(line string) []string {
	line = stripAssistantStreamCodeSpans(line)
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
		if isAssistantStreamTaskMarker(line, idx, firstEnd) {
			idx = firstEnd
			continue
		}
		labels = append(labels, strings.Join(strings.Fields(strings.ToLower(firstLabel)), " "))
		idx = firstEnd
	}
	return labels
}

func stripAssistantStreamCodeSpans(line string) string {
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

func isAssistantStreamTaskMarker(line string, openIdx int, closeIdx int) bool {
	label := strings.TrimSpace(line[openIdx+1 : closeIdx])
	if label != "" && !strings.EqualFold(label, "x") {
		return false
	}
	prefix := strings.TrimSpace(line[:openIdx])
	return strings.HasSuffix(prefix, "-") || strings.HasSuffix(prefix, "*") || strings.HasSuffix(prefix, "+")
}

func assistantStreamCanBecomeSetextHeading(line string) bool {
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ">") {
		return false
	}
	if isAssistantStreamFenceLine(line) || isAssistantStreamTableDelimiterLine(line) || isAssistantStreamThematicBreakLine(line) {
		return false
	}
	if isAssistantStreamListMarkerLine(line) {
		return false
	}
	return true
}

func isAssistantStreamListMarkerLine(line string) bool {
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

func isAssistantStreamThematicBreakLine(line string) bool {
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

func minAssistantStreamHoldbackStart(current int, candidate int) int {
	if current < 0 || candidate < current {
		return candidate
	}
	return current
}

func activeAssistantStreamTableStart(source string) (int, bool) {
	lines := completeAssistantStreamLines(source)
	inFence := false
	tableStart := -1
	for idx := 0; idx < len(lines); idx++ {
		line := strings.TrimSpace(lines[idx].text)
		if isAssistantStreamFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if tableStart >= 0 {
			if isAssistantStreamTableLine(line) {
				continue
			}
			tableStart = -1
			continue
		}
		if idx+1 < len(lines) && isAssistantStreamTableHeaderLine(line) && isAssistantStreamTableDelimiterLine(strings.TrimSpace(lines[idx+1].text)) {
			tableStart = lines[idx].start
			idx++
		}
	}
	if tableStart >= 0 {
		return tableStart, true
	}
	return 0, false
}

type assistantStreamSourceLine struct {
	text  string
	start int
}

func completeAssistantStreamLines(source string) []assistantStreamSourceLine {
	lines := make([]assistantStreamSourceLine, 0, strings.Count(source, "\n"))
	start := 0
	for idx, r := range source {
		if r != '\n' {
			continue
		}
		lines = append(lines, assistantStreamSourceLine{text: source[start:idx], start: start})
		start = idx + 1
	}
	return lines
}

func isAssistantStreamFenceLine(line string) bool {
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

func isAssistantStreamTableHeaderLine(line string) bool {
	return strings.Contains(line, "|") && strings.Trim(line, "| ") != ""
}

func isAssistantStreamTableLine(line string) bool {
	return strings.Contains(line, "|") && strings.TrimSpace(line) != ""
}

func isAssistantStreamTableDelimiterLine(line string) bool {
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

func normalizedAssistantStreamWidth(width int) int {
	if width <= 0 {
		return 120
	}
	return width
}

func clampAssistantStreamLineCount(value int, minValue int, maxValue int) int {
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

func cloneAssistantStreamProjectionLines(lines []tui.TranscriptProjectionLine) []tui.TranscriptProjectionLine {
	if len(lines) == 0 {
		return nil
	}
	return append([]tui.TranscriptProjectionLine(nil), lines...)
}
