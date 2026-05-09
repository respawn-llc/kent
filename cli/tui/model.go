package tui

import (
	"builder/shared/clientui"
	"builder/shared/transcript"
	"fmt"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type Mode string

const (
	ModeOngoing Mode = "ongoing"
	ModeDetail  Mode = "detail"

	DefaultPreviewLines = 8
	TranscriptDivider   = "────────────────────────"
	detailItemSeparator = ""
)

var patchCountTokenPattern = regexp.MustCompile(`([+-]\d+)\b`)

type TranscriptEntry struct {
	Visibility        transcript.EntryVisibility
	RollbackTargetID  string
	Transient         bool
	Committed         bool
	Role              TranscriptRole
	Text              string
	OngoingText       string
	Phase             clientui.MessagePhase
	MessageType       clientui.MessageType
	SourcePath        string
	CompactLabel      string
	ToolResultSummary string
	ToolCallID        string
	ToolCall          *transcript.ToolCallMeta
}

type VisibleLineKind uint8

const (
	VisibleLineContent VisibleLineKind = iota
	VisibleLineDivider
)

type StreamingReasoningEntry struct {
	Key  string
	Role TranscriptRole
	Text string
}

type ToggleModeMsg struct {
	SkipDetailWarmup bool
}

type SetModeMsg struct {
	Mode             Mode
	SkipDetailWarmup bool
}

type ScrollOngoingMsg struct {
	Delta int
}

type SetViewportLinesMsg struct {
	Lines int
}

type SetViewportSizeMsg struct {
	Lines int
	Width int
}

type AppendTranscriptMsg struct {
	Visibility        transcript.EntryVisibility
	Transient         bool
	Committed         bool
	Role              TranscriptRole
	Text              string
	OngoingText       string
	Phase             clientui.MessagePhase
	MessageType       clientui.MessageType
	SourcePath        string
	CompactLabel      string
	ToolResultSummary string
	ToolCallID        string
	ToolCall          *transcript.ToolCallMeta
}

type SetConversationMsg struct {
	BaseOffset   int
	TotalEntries int
	Entries      []TranscriptEntry
	Ongoing      string
	OngoingError string
}

type SetSelectedTranscriptEntryMsg struct {
	EntryIndex            int
	Active                bool
	RefreshDetailSnapshot bool
}

type FocusTranscriptEntryMsg struct {
	EntryIndex int
	Center     bool
	Bottom     bool
}

type SetOngoingScrollMsg struct {
	Scroll int
}

type StreamAssistantMsg struct {
	Delta string
}

type ClearOngoingAssistantMsg struct{}

type UpsertStreamingReasoningMsg struct {
	Key  string
	Role string
	Text string
}

type ClearStreamingReasoningMsg struct{}

type CommitAssistantMsg struct{}

type SetOngoingErrorMsg struct {
	Err error
}

type ClearOngoingErrorMsg struct{}

type Option func(*Model)

type RenderDiagnosticSeverity string

const (
	RenderDiagnosticSeverityInfo  RenderDiagnosticSeverity = "info"
	RenderDiagnosticSeverityWarn  RenderDiagnosticSeverity = "warn"
	RenderDiagnosticSeverityError RenderDiagnosticSeverity = "error"
	RenderDiagnosticSeverityFatal RenderDiagnosticSeverity = "fatal"
)

type RenderDiagnostic struct {
	Component string
	Message   string
	Err       error
	Severity  RenderDiagnosticSeverity
}

type RenderDiagnosticHandler func(RenderDiagnostic)

func WithPreviewLines(lines int) Option {
	return func(m *Model) {
		if lines > 0 {
			m.viewportLines = lines
		}
	}
}

func WithTheme(theme string) Option {
	return func(m *Model) {
		m.theme = normalizeTheme(theme)
	}
}

func WithRenderDiagnosticHandler(handler RenderDiagnosticHandler) Option {
	return func(m *Model) {
		m.renderDiagnosticHandler = handler
	}
}

func WithCompactDetail() Option {
	return func(m *Model) {
		m.compactDetail = true
	}
}

type Model struct {
	mode Mode

	compactDetail               bool
	viewportLines               int
	viewportWidth               int
	ongoingScroll               int
	detailScroll                int
	snapOngoingOnViewportResize bool
	toolSymbolGap               int

	transcript             []TranscriptEntry
	transcriptBaseOffset   int
	transcriptTotalEntries int
	ongoing                string
	streamingReasoning     []StreamingReasoningEntry

	selectedTranscriptEntry  int
	selectedTranscriptActive bool
	detailSelectedEntry      int
	detailSelectedActive     bool
	detailExpandedEntries    map[int]struct{}

	detailSnapshot          string
	detailLines             []string
	detailLineKinds         []VisibleLineKind
	detailLineEntryIndices  []int
	detailEntryLineRanges   []lineRange
	detailBlockLineRanges   []lineRange
	detailEntryRangeOffset  int
	detailBlocks            []detailBlockSpec
	detailBlockLines        [][]string
	detailTotalLineCount    int
	detailMetricsResolved   bool
	detailBottomAnchor      bool
	detailBottomOffset      int
	detailDirty             bool
	detailStale             bool
	detailRebuildCount      int
	ongoingSnapshot         string
	ongoingLineCache        []string
	ongoingLineKinds        []VisibleLineKind
	ongoingBaseLines        []string
	ongoingBaseLineKinds    []VisibleLineKind
	ongoingBaseLastGroup    string
	ongoingStreamingLines   []string
	ongoingStreamingKinds   []VisibleLineKind
	ongoingStreamingDivider bool
	ongoingBaseDirty        bool
	ongoingDirty            bool
	ongoingError            string
	theme                   string
	md                      *markdownRenderer
	code                    *codeRenderer
	renderDiagnosticHandler RenderDiagnosticHandler
}

func (m Model) DetailScroll() int {
	if m.detailBottomAnchor && !m.detailMetricsResolved {
		return m.detailBottomOffset
	}
	return m.detailScroll
}

func (m Model) DetailMaxScroll() int {
	return m.maxDetailScroll()
}

func (m Model) DetailMetricsResolved() bool {
	return !m.detailDirty && !m.detailBottomAnchor
}

func (m Model) DetailRebuildCount() int {
	return m.detailRebuildCount
}

func (m Model) DetailSelectedEntry() (int, bool) {
	selectedEntry, ok := m.resolveDetailSelection()
	if !ok {
		return 0, false
	}
	return selectedEntry, true
}

func (m Model) DetailSelectedExpansionAction() (string, bool) {
	state, ok := m.detailSelectedExpansionState()
	if !ok {
		return "", false
	}
	if state.expanded {
		return "collapse", true
	}
	return "expand", true
}

func (m Model) TranscriptBaseOffset() int {
	return m.transcriptBaseOffset
}

func (m Model) TranscriptTotalEntries() int {
	if m.transcriptTotalEntries > 0 {
		return m.transcriptTotalEntries
	}
	return m.transcriptBaseOffset + len(m.transcript)
}

func (m Model) LoadedTranscriptEntryCount() int {
	return len(m.transcript)
}

func (m Model) LoadedTranscriptEntries() []TranscriptEntry {
	if len(m.transcript) == 0 {
		return nil
	}
	entries := make([]TranscriptEntry, 0, len(m.transcript))
	for _, entry := range m.transcript {
		copyEntry := entry
		copyEntry.ToolCall = cloneToolCallMeta(entry.ToolCall)
		entries = append(entries, copyEntry)
	}
	return entries
}

func (m Model) DetailVisibleEntryRange() (int, int, bool) {
	first := -1
	last := -1
	for _, entryIndex := range m.detailLineEntryIndices {
		if entryIndex < 0 {
			continue
		}
		if first < 0 {
			first = entryIndex
		}
		last = entryIndex
	}
	if first < 0 || last < 0 {
		return 0, 0, false
	}
	return first, last, true
}

func (m Model) absoluteTranscriptIndex(localIndex int) int {
	if localIndex < 0 {
		return -1
	}
	return m.transcriptBaseOffset + localIndex
}

func (m Model) localTranscriptIndex(absoluteIndex int) (int, bool) {
	local := absoluteIndex - m.transcriptBaseOffset
	if local < 0 || local >= len(m.transcript) {
		return 0, false
	}
	return local, true
}

type ongoingBlock struct {
	role       RenderIntent
	lines      []string
	entryIndex int
	entryEnd   int
}

type lineRange struct {
	Start int
	End   int
}

type detailBlockSpec struct {
	role       RenderIntent
	entryIndex int
	entryEnd   int
	selectable bool
	expanded   bool
	expandable bool
	render     func(Model, string) []string
}

func NewModel(opts ...Option) Model {
	m := Model{
		mode:             ModeOngoing,
		viewportLines:    DefaultPreviewLines,
		viewportWidth:    120,
		toolSymbolGap:    1,
		theme:            normalizeTheme(""),
		ongoingBaseDirty: true,
		ongoingDirty:     true,
		detailDirty:      true,
	}
	for _, opt := range opts {
		opt(&m)
	}
	m.md = newMarkdownRenderer(m.theme, m.reportRenderDiagnostic)
	m.code = newCodeRenderer(m.theme)
	return m
}

func (m Model) reportRenderDiagnostic(diag RenderDiagnostic) {
	if strings.TrimSpace(diag.Message) == "" && diag.Err != nil {
		diag.Message = diag.Err.Error()
	}
	if strings.TrimSpace(diag.Component) == "" {
		diag.Component = "render"
	}
	if strings.TrimSpace(string(diag.Severity)) == "" {
		diag.Severity = RenderDiagnosticSeverityWarn
	}
	if m.renderDiagnosticHandler != nil {
		m.renderDiagnosticHandler(diag)
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.reduce(msg)
	return m, nil
}

func (m Model) View() string {
	if m.mode == ModeDetail {
		return m.renderDetailSnapshot()
	}
	return m.renderOngoing()
}

func (m *Model) VisibleLineKinds() []VisibleLineKind {
	if m == nil {
		return nil
	}
	if m.mode == ModeDetail {
		if m.detailDirty {
			m.rebuildDetailSnapshot()
		}
		return visibleKindsForViewport(m.detailLineKinds, m.viewportLines)
	}
	if m.ongoingDirty {
		m.rebuildOngoingSnapshot()
	}
	return m.visibleOngoingLineKinds()
}

func visibleKindsForViewport(kinds []VisibleLineKind, viewportLines int) []VisibleLineKind {
	if viewportLines <= 0 {
		return nil
	}
	if len(kinds) == 0 {
		return append(make([]VisibleLineKind, 0, viewportLines), VisibleLineContent)
	}
	out := append([]VisibleLineKind(nil), kinds...)
	for len(out) < viewportLines {
		out = append(out, VisibleLineContent)
	}
	if len(out) > viewportLines {
		out = out[:viewportLines]
	}
	return out
}

func sliceVisibleLineKinds(kinds []VisibleLineKind, scroll, maxScroll, viewportLines int) []VisibleLineKind {
	if viewportLines <= 0 {
		return nil
	}
	if len(kinds) == 0 {
		return append(make([]VisibleLineKind, 0, viewportLines), VisibleLineContent)
	}
	start := clamp(scroll, 0, maxScroll)
	end := start + viewportLines
	if end > len(kinds) {
		end = len(kinds)
	}
	out := append([]VisibleLineKind(nil), kinds[start:end]...)
	for len(out) < viewportLines {
		out = append(out, VisibleLineContent)
	}
	return out
}

func (m Model) Mode() Mode {
	return m.mode
}

func (m Model) OngoingScroll() int {
	return m.ongoingScroll
}

func (m Model) OngoingSnapshot() string {
	if m.ongoingDirty {
		return m.renderFlatOngoingTranscript()
	}
	if m.ongoingSnapshot != "" {
		return m.ongoingSnapshot
	}
	return strings.Join(m.ongoingLines(), "\n")
}

func (m Model) OngoingCommittedSnapshot() string {
	return m.renderFlatCommittedOngoingTranscript()
}

func (m Model) OngoingStreamingText() string {
	return m.ongoing
}

func (m Model) OngoingErrorText() string {
	return m.ongoingError
}

func FormatOngoingError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "error"
	}
	return fmt.Sprintf("error: %s", msg)
}

func (m Model) toggleMode(skipDetailWarmup bool) Model {
	target := ModeDetail
	if m.mode == ModeDetail {
		target = ModeOngoing
	}
	return m.transitionMode(target, skipDetailWarmup)
}

func (m Model) transitionMode(target Mode, skipDetailWarmup bool) Model {
	if target == "" || target == m.mode {
		return m
	}
	switch target {
	case ModeDetail:
		m.mode = ModeDetail
		m.snapOngoingOnViewportResize = false
		m.detailBottomAnchor = true
		m.detailBottomOffset = 0
		if skipDetailWarmup {
			return m
		}
		if !skipDetailWarmup && (m.detailDirty || m.detailStale || len(m.detailLines) == 0) {
			m.rebuildDetailSnapshot()
			m.detailStale = false
		}
		m.refreshDetailViewport()
		if m.compactDetail {
			m.focusBottomVisibleDetailEntry()
		}
	case ModeOngoing:
		m.mode = ModeOngoing
		// Ongoing mode is the live tail view, so exiting detail always snaps to
		// the latest visible transcript content.
		m.ongoingScroll = m.maxOngoingScroll()
		// App-level layout shrinks the viewport when returning to ongoing. Re-snap
		// on the next viewport resize so we stay on the true latest tail.
		m.snapOngoingOnViewportResize = true
	}
	return m
}

func (m Model) scrollOngoing(delta int) Model {
	m.ongoingScroll = clamp(m.ongoingScroll+delta, 0, m.maxOngoingScroll())
	return m
}

func (m Model) scrollDetail(delta int) Model {
	if m.moveDetailSelectionTowardCenterAtScrollEdge(delta) {
		return m
	}
	if moved := m.scrollDetailLine(delta); moved {
		m.focusCenterVisibleDetailEntry()
		return m
	}
	m.moveDetailSelectionWithinViewport(delta)
	return m
}

func (m *Model) scrollDetailLine(delta int) bool {
	if m.detailBottomAnchor && !m.detailMetricsResolved {
		before := m.detailBottomOffset
		nextOffset := m.detailBottomOffset - delta
		if nextOffset < 0 {
			nextOffset = 0
		}
		m.detailBottomOffset = nextOffset
		m.refreshDetailViewport()
		return m.detailBottomOffset != before
	}
	m.ensureDetailScrollResolved()
	before := m.detailScroll
	m.detailScroll = clamp(m.detailScroll+delta, 0, m.maxDetailScroll())
	m.refreshDetailViewport()
	return m.detailScroll != before
}

func (m *Model) ensureDetailSelection() {
	if m == nil {
		return
	}
	if m.detailDirty {
		m.rebuildDetailSnapshot()
	}
	if m.detailSelectedActive && m.detailBlockIndexForEntry(m.detailSelectedEntry) >= 0 {
		return
	}
	for idx := len(m.detailBlocks) - 1; idx >= 0; idx-- {
		if !m.detailBlocks[idx].selectable {
			continue
		}
		m.detailSelectedEntry = m.detailBlocks[idx].entryIndex
		m.detailSelectedActive = true
		return
	}
	m.detailSelectedEntry = -1
	m.detailSelectedActive = false
}

func (m *Model) focusCenterVisibleDetailEntry() {
	m.focusVisibleDetailEntry(m.viewportLines / 2)
}

func (m *Model) focusBottomVisibleDetailEntry() {
	m.focusVisibleDetailEntry(len(m.detailLineEntryIndices) - 1)
}

func (m *Model) focusVisibleDetailEntry(anchor int) {
	if m == nil || !m.compactDetail {
		return
	}
	if m.detailDirty {
		m.rebuildDetailSnapshot()
	}
	if len(m.detailLineEntryIndices) == 0 {
		m.ensureDetailSelection()
		return
	}
	if anchor >= len(m.detailLineEntryIndices) {
		anchor = len(m.detailLineEntryIndices) - 1
	}
	if anchor < 0 {
		m.ensureDetailSelection()
		return
	}
	bestEntry := -1
	bestDistance := len(m.detailLineEntryIndices) + 1
	for lineIndex, entryIndex := range m.detailLineEntryIndices {
		if entryIndex < 0 || m.detailBlockIndexForEntry(entryIndex) < 0 {
			continue
		}
		distance := detailLineDistance(lineIndex, anchor)
		if distance >= bestDistance {
			continue
		}
		bestEntry = entryIndex
		bestDistance = distance
	}
	if bestEntry < 0 {
		m.ensureDetailSelection()
		return
	}
	previousEntry := m.detailSelectedEntry
	previousActive := m.detailSelectedActive
	m.detailSelectedEntry = bestEntry
	m.detailSelectedActive = true
	if previousEntry != m.detailSelectedEntry || previousActive != m.detailSelectedActive {
		m.refreshDetailViewport()
	}
}

func (m *Model) moveDetailSelectionWithinViewport(delta int) bool {
	if m == nil || !m.compactDetail {
		return false
	}
	if len(m.visibleSelectableDetailEntries()) == 0 {
		m.ensureDetailSelection()
		return false
	}
	first, last, ok := m.visibleDetailEntryLineRange(m.detailSelectedEntry)
	if !m.detailSelectedActive || !ok {
		m.focusCenterVisibleDetailEntry()
		return false
	}
	startLine := first - 1
	if delta > 0 {
		startLine = last + 1
	}
	return m.selectVisibleDetailEntryInLineDirection(startLine, delta)
}

func (m *Model) moveDetailSelectionTowardCenterAtScrollEdge(delta int) bool {
	if m == nil || !m.compactDetail || (delta != -1 && delta != 1) {
		return false
	}
	if m.detailDirty {
		m.rebuildDetailSnapshot()
	}
	first, last, ok := m.visibleDetailEntryLineRange(m.detailSelectedEntry)
	if !m.detailSelectedActive || !ok {
		return false
	}
	centerEntry := m.centerVisibleSelectableDetailEntry()
	centerFirst, centerLast, ok := m.visibleDetailEntryLineRange(centerEntry)
	if !ok {
		return false
	}
	centerLine := (centerFirst + centerLast) / 2
	if delta < 0 && first <= centerLine {
		return false
	}
	if delta > 0 && last >= centerLine {
		return false
	}
	startLine := first - 1
	if delta > 0 {
		startLine = last + 1
	}
	return m.selectVisibleDetailEntryInLineDirection(startLine, delta)
}

func (m *Model) selectVisibleDetailEntry(entryIndex int) bool {
	if m == nil || entryIndex < 0 {
		return false
	}
	if detailVisibleEntryIndex(m.visibleSelectableDetailEntries(), entryIndex) < 0 {
		return false
	}
	previousEntry := m.detailSelectedEntry
	previousActive := m.detailSelectedActive
	m.detailSelectedEntry = entryIndex
	m.detailSelectedActive = true
	if previousEntry != m.detailSelectedEntry || previousActive != m.detailSelectedActive {
		m.refreshDetailViewport()
	}
	return true
}

func (m *Model) selectVisibleDetailEntryInLineDirection(startLine int, delta int) bool {
	if m == nil || delta == 0 {
		return false
	}
	for lineIndex := startLine; lineIndex >= 0 && lineIndex < len(m.detailLineEntryIndices); lineIndex += delta {
		entryIndex := m.detailLineEntryIndices[lineIndex]
		if entryIndex < 0 || entryIndex == m.detailSelectedEntry || m.detailBlockIndexForEntry(entryIndex) < 0 {
			continue
		}
		return m.selectVisibleDetailEntry(entryIndex)
	}
	return false
}

func (m Model) visibleDetailEntryLineRange(entryIndex int) (int, int, bool) {
	if entryIndex < 0 {
		return -1, -1, false
	}
	first := -1
	last := -1
	for lineIndex, owner := range m.detailLineEntryIndices {
		if owner != entryIndex {
			continue
		}
		if first < 0 {
			first = lineIndex
		}
		last = lineIndex
	}
	return first, last, first >= 0
}

func (m *Model) visibleSelectableDetailEntries() []int {
	if m == nil {
		return nil
	}
	if m.detailDirty {
		m.rebuildDetailSnapshot()
	}
	entries := make([]int, 0, len(m.detailLineEntryIndices))
	seen := make(map[int]struct{}, len(m.detailLineEntryIndices))
	for _, entryIndex := range m.detailLineEntryIndices {
		if entryIndex < 0 || m.detailBlockIndexForEntry(entryIndex) < 0 {
			continue
		}
		if _, ok := seen[entryIndex]; ok {
			continue
		}
		seen[entryIndex] = struct{}{}
		entries = append(entries, entryIndex)
	}
	return entries
}

func (m *Model) centerVisibleSelectableDetailEntry() int {
	if m == nil {
		return -1
	}
	if m.detailDirty {
		m.rebuildDetailSnapshot()
	}
	if len(m.detailLineEntryIndices) == 0 {
		return -1
	}
	anchor := m.viewportLines / 2
	if anchor >= len(m.detailLineEntryIndices) {
		anchor = len(m.detailLineEntryIndices) - 1
	}
	if anchor < 0 {
		return -1
	}
	bestEntry := -1
	bestDistance := len(m.detailLineEntryIndices) + 1
	for lineIndex, entryIndex := range m.detailLineEntryIndices {
		if entryIndex < 0 || m.detailBlockIndexForEntry(entryIndex) < 0 {
			continue
		}
		distance := detailLineDistance(lineIndex, anchor)
		if distance >= bestDistance {
			continue
		}
		bestEntry = entryIndex
		bestDistance = distance
	}
	return bestEntry
}

func detailLineDistance(left int, right int) int {
	distance := left - right
	if distance < 0 {
		return -distance
	}
	return distance
}

func detailVisibleEntryIndex(entries []int, entryIndex int) int {
	for idx, candidate := range entries {
		if candidate == entryIndex {
			return idx
		}
	}
	return -1
}

func (m Model) detailBlockIndexForEntry(entryIndex int) int {
	for idx, block := range m.detailBlocks {
		if block.selectable && block.entryIndex == entryIndex {
			return idx
		}
	}
	return -1
}

func (m Model) maxOngoingScroll() int {
	lineCount := m.ongoingRenderedLineCount()
	if lineCount <= m.viewportLines {
		return 0
	}
	return lineCount - m.viewportLines
}

func (m *Model) maxDetailScroll() int {
	if m.detailBottomAnchor && !m.detailMetricsResolved {
		return m.detailBottomOffset
	}
	m.ensureDetailMetricsResolved()
	if m.detailTotalLineCount <= m.viewportLines {
		return 0
	}
	return m.detailTotalLineCount - m.viewportLines
}

func (m Model) isOngoingAtBottom() bool {
	return m.ongoingScroll >= m.maxOngoingScroll()
}

func (m Model) renderOngoing() string {
	lineCount := m.ongoingRenderedLineCount()
	start := clamp(m.ongoingScroll, 0, m.maxOngoingScroll())
	end := start + m.viewportLines
	if end > lineCount {
		end = lineCount
	}

	out := make([]string, 0, m.viewportLines+1)
	for i := start; i < end; i++ {
		out = append(out, m.ongoingLineAt(i))
	}
	for len(out) < m.viewportLines {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

func (m Model) ongoingLines() []string {
	if m.ongoingDirty {
		return splitLines(m.renderFlatOngoingTranscript())
	}
	if len(m.ongoingLineCache) > 0 {
		return m.ongoingLineCache
	}
	lineCount := m.ongoingRenderedLineCount()
	lines := make([]string, 0, lineCount)
	for idx := 0; idx < lineCount; idx++ {
		lines = append(lines, m.ongoingLineAt(idx))
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func (m *Model) invalidateOngoingSnapshot() {
	m.ongoingDirty = true
	m.ongoingSnapshot = ""
	m.ongoingLineCache = nil
	m.ongoingLineKinds = nil
}

func (m *Model) invalidateOngoingBaseSnapshot() {
	m.ongoingBaseDirty = true
	m.invalidateOngoingSnapshot()
}

func (m *Model) rebuildOngoingSnapshot() {
	if m.ongoingBaseDirty {
		projection := m.OngoingProjection(false)
		lines := projection.Lines(detailDivider())
		m.ongoingBaseLines = m.ongoingBaseLines[:0]
		m.ongoingBaseLineKinds = m.ongoingBaseLineKinds[:0]
		m.ongoingBaseLastGroup = ""
		for _, line := range lines {
			m.ongoingBaseLines = append(m.ongoingBaseLines, line.Text)
			m.ongoingBaseLineKinds = append(m.ongoingBaseLineKinds, line.Kind)
		}
		if blockCount := len(projection.Blocks); blockCount > 0 {
			m.ongoingBaseLastGroup = projection.Blocks[blockCount-1].DividerGroup
		}
		m.ongoingBaseDirty = false
	}
	m.ongoingStreamingLines = m.ongoingStreamingLines[:0]
	m.ongoingStreamingKinds = m.ongoingStreamingKinds[:0]
	m.ongoingStreamingDivider = false
	if strings.TrimSpace(m.ongoing) != "" {
		m.ongoingStreamingLines = append(m.ongoingStreamingLines, m.flattenEntryPlain("assistant", m.ongoing)...)
		if len(m.ongoingStreamingLines) > 0 {
			m.ongoingStreamingKinds = make([]VisibleLineKind, len(m.ongoingStreamingLines))
			if len(m.ongoingBaseLines) > 0 && m.ongoingBaseLastGroup != ongoingDividerGroup("assistant") {
				m.ongoingStreamingDivider = true
			}
		}
	}
	if len(m.ongoingBaseLines) == 0 && !m.ongoingStreamingDivider && len(m.ongoingStreamingLines) == 0 {
		m.ongoingSnapshot = ""
		m.ongoingLineCache = []string{""}
		m.ongoingLineKinds = []VisibleLineKind{VisibleLineContent}
		m.ongoingDirty = false
		return
	}
	plain := make([]string, 0, len(m.ongoingBaseLines)+len(m.ongoingStreamingLines)+1)
	kinds := make([]VisibleLineKind, 0, len(m.ongoingBaseLineKinds)+len(m.ongoingStreamingKinds)+1)
	plain = append(plain, m.ongoingBaseLines...)
	kinds = append(kinds, m.ongoingBaseLineKinds...)
	if m.ongoingStreamingDivider {
		plain = append(plain, detailDivider())
		kinds = append(kinds, VisibleLineDivider)
	}
	plain = append(plain, m.ongoingStreamingLines...)
	kinds = append(kinds, m.ongoingStreamingKinds...)
	m.ongoingSnapshot = strings.Join(plain, "\n")
	m.ongoingLineCache = plain
	m.ongoingLineKinds = kinds
	m.ongoingDirty = false
}

func (m Model) ongoingRenderedLineCount() int {
	total := len(m.ongoingBaseLines) + len(m.ongoingStreamingLines)
	if m.ongoingStreamingDivider {
		total++
	}
	if total == 0 {
		return 1
	}
	return total
}

func (m Model) ongoingLineAt(index int) string {
	if index < 0 || index >= m.ongoingRenderedLineCount() {
		return ""
	}
	if len(m.ongoingBaseLines) == 0 && !m.ongoingStreamingDivider && len(m.ongoingStreamingLines) == 0 {
		return ""
	}
	if index < len(m.ongoingBaseLines) {
		return m.ongoingBaseLines[index]
	}
	index -= len(m.ongoingBaseLines)
	if m.ongoingStreamingDivider {
		if index == 0 {
			return detailDivider()
		}
		index--
	}
	if index >= 0 && index < len(m.ongoingStreamingLines) {
		return m.ongoingStreamingLines[index]
	}
	return ""
}

func (m Model) ongoingLineKindAt(index int) VisibleLineKind {
	if index < 0 || index >= m.ongoingRenderedLineCount() {
		return VisibleLineContent
	}
	if len(m.ongoingBaseLines) == 0 && !m.ongoingStreamingDivider && len(m.ongoingStreamingLines) == 0 {
		return VisibleLineContent
	}
	if index < len(m.ongoingBaseLineKinds) {
		return m.ongoingBaseLineKinds[index]
	}
	index -= len(m.ongoingBaseLineKinds)
	if m.ongoingStreamingDivider {
		if index == 0 {
			return VisibleLineDivider
		}
		index--
	}
	if index >= 0 && index < len(m.ongoingStreamingKinds) {
		return m.ongoingStreamingKinds[index]
	}
	return VisibleLineContent
}

func (m Model) visibleOngoingLineKinds() []VisibleLineKind {
	if m.viewportLines <= 0 {
		return nil
	}
	start := clamp(m.ongoingScroll, 0, m.maxOngoingScroll())
	end := start + m.viewportLines
	if total := m.ongoingRenderedLineCount(); end > total {
		end = total
	}
	out := make([]VisibleLineKind, 0, m.viewportLines)
	for idx := start; idx < end; idx++ {
		out = append(out, m.ongoingLineKindAt(idx))
	}
	for len(out) < m.viewportLines {
		out = append(out, VisibleLineContent)
	}
	return out
}

func (m Model) renderDetailSnapshot() string {
	if m.detailDirty {
		m.rebuildDetailSnapshot()
	}
	lines := m.detailLines
	if len(lines) == 0 {
		lines = []string{""}
	}

	out := make([]string, 0, m.viewportLines)
	selectedEntry, highlightSelected := m.resolveDetailSelection()
	firstSelectedLine := -1
	lastSelectedLine := -1
	if highlightSelected && m.compactDetail {
		for i, entryIndex := range m.detailLineEntryIndices {
			if entryIndex != selectedEntry {
				continue
			}
			if firstSelectedLine < 0 {
				firstSelectedLine = i
			}
			lastSelectedLine = i
		}
	}
	for i, line := range lines {
		selected := highlightSelected && i < len(m.detailLineEntryIndices) && m.detailLineEntryIndices[i] == selectedEntry
		if m.compactDetail && highlightSelected && m.shouldInsertDetailSelectionSpacerBefore(i, firstSelectedLine) {
			out = append(out, m.renderDetailSelectionSpacerLine())
		}
		if m.compactDetail && highlightSelected && m.shouldRenderDetailSelectionSpacer(i, firstSelectedLine, lastSelectedLine) {
			out = append(out, m.renderDetailSelectionSpacerLine())
			continue
		}
		line = m.renderDetailViewportLine(line, selected)
		out = append(out, line)
		if m.compactDetail && highlightSelected && m.shouldInsertDetailSelectionSpacerAfter(i, lastSelectedLine) {
			out = append(out, m.renderDetailSelectionSpacerLine())
		}
	}
	if len(out) > m.viewportLines {
		out = out[:m.viewportLines]
	}
	for len(out) < m.viewportLines {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

func (m Model) shouldRenderDetailSelectionSpacer(lineIndex int, firstSelectedLine int, lastSelectedLine int) bool {
	if firstSelectedLine < 0 || lastSelectedLine < 0 {
		return false
	}
	if lineIndex == firstSelectedLine-1 {
		return !m.shouldInsertDetailSelectionSpacerBefore(firstSelectedLine, firstSelectedLine)
	}
	if lineIndex == lastSelectedLine+1 {
		return !m.shouldInsertDetailSelectionSpacerAfter(lastSelectedLine, lastSelectedLine)
	}
	return false
}

func (m Model) shouldInsertDetailSelectionSpacerBefore(lineIndex int, firstSelectedLine int) bool {
	return lineIndex == firstSelectedLine && m.detailAtTopEdgeForSelectionSpacer() && m.detailViewportLineOwnsSelectableEntry(firstSelectedLine-1)
}

func (m Model) shouldInsertDetailSelectionSpacerAfter(lineIndex int, lastSelectedLine int) bool {
	return lineIndex == lastSelectedLine && m.detailAtBottomEdgeForSelectionSpacer() && m.detailViewportLineOwnsSelectableEntry(lastSelectedLine+1)
}

func (m Model) detailAtTopEdgeForSelectionSpacer() bool {
	if m.detailBottomAnchor && !m.detailMetricsResolved {
		return false
	}
	return m.detailScroll == 0
}

func (m Model) detailAtBottomEdgeForSelectionSpacer() bool {
	if m.detailBottomAnchor && !m.detailMetricsResolved {
		return false
	}
	return m.detailScroll == m.maxDetailScroll()
}

func (m Model) detailViewportLineOwnsSelectableEntry(lineIndex int) bool {
	if lineIndex < 0 || lineIndex >= len(m.detailLineEntryIndices) {
		return false
	}
	return m.detailBlockIndexForEntry(m.detailLineEntryIndices[lineIndex]) >= 0
}

type detailExpansionSymbolState struct {
	role     RenderIntent
	expanded bool
}

func (m Model) detailSelectedExpansionState() (detailExpansionSymbolState, bool) {
	if !m.compactDetail || m.mode != ModeDetail {
		return detailExpansionSymbolState{}, false
	}
	selectedEntry, ok := m.resolveDetailSelection()
	if !ok {
		return detailExpansionSymbolState{}, false
	}
	blockIndex := m.detailBlockIndexForEntry(selectedEntry)
	if blockIndex < 0 || blockIndex >= len(m.detailBlocks) {
		return detailExpansionSymbolState{}, false
	}
	block := m.detailBlocks[blockIndex]
	if !block.selectable || !block.expandable {
		return detailExpansionSymbolState{}, false
	}
	return detailExpansionSymbolState{role: block.role, expanded: block.expanded}, true
}

func (m Model) detailExpansionSymbolOverride(block detailBlockSpec) string {
	if !m.compactDetail || m.mode != ModeDetail || !block.selectable || !block.expandable {
		return ""
	}
	if selectedEntry, ok := m.selectedUserTranscriptEntry(); ok && selectedEntry == block.entryIndex {
		return renderRoleSymbol(">", m.detailExpansionSymbolStyle(block.role)) + " "
	}
	selectedEntry, ok := m.resolveDetailSelection()
	if !ok || selectedEntry != block.entryIndex {
		return ""
	}
	return m.detailExpansionSymbolPrefix(block.role, block.expanded)
}

func (m *Model) invalidateDetailSnapshot() {
	m.detailDirty = true
}

func (m *Model) rebuildDetailSnapshot() {
	m.detailRebuildCount++
	m.detailBlocks = m.buildDetailBlockSpecs(true)
	m.detailBlockLines = make([][]string, len(m.detailBlocks))
	m.detailEntryLineRanges = nil
	m.detailBlockLineRanges = nil
	m.detailEntryRangeOffset = m.transcriptBaseOffset
	m.detailTotalLineCount = 0
	m.detailMetricsResolved = false
	m.detailSnapshot = ""
	if len(m.detailBlocks) == 0 {
		m.detailSnapshot = ""
		m.detailLines = []string{""}
		m.detailLineKinds = []VisibleLineKind{VisibleLineContent}
		m.detailLineEntryIndices = []int{-1}
		m.detailMetricsResolved = true
		m.detailBottomAnchor = false
		m.detailBottomOffset = 0
		m.detailTotalLineCount = 1
		m.detailDirty = false
		return
	}
	m.detailDirty = false
	m.refreshDetailViewport()
}

func (m *Model) refreshDetailViewport() {
	if m == nil {
		return
	}
	if m.detailDirty {
		m.rebuildDetailSnapshot()
		return
	}
	if len(m.detailBlocks) == 0 {
		m.detailLines = []string{""}
		m.detailLineKinds = []VisibleLineKind{VisibleLineContent}
		m.detailLineEntryIndices = []int{-1}
		return
	}
	if m.detailBottomAnchor && !m.detailMetricsResolved {
		m.detailLines, m.detailLineKinds, m.detailLineEntryIndices, m.detailBottomOffset = m.detailViewportFromBottomOffset(m.detailBottomOffset)
		return
	}
	start := clamp(m.detailScroll, 0, m.maxDetailScroll())
	m.detailLines, m.detailLineKinds, m.detailLineEntryIndices = m.detailViewportFromScroll(start)
}

func (m *Model) ensureDetailScrollResolved() {
	if m == nil || (!m.detailBottomAnchor && m.detailMetricsResolved) {
		return
	}
	bottomOffset := m.detailBottomOffset
	m.ensureDetailMetricsResolved()
	if m.detailBottomAnchor {
		maxScroll := max(0, m.detailTotalLineCount-m.viewportLines)
		if bottomOffset > maxScroll {
			bottomOffset = maxScroll
		}
		m.detailScroll = maxScroll - bottomOffset
		m.detailBottomAnchor = false
		m.detailBottomOffset = 0
	}
}

func (m *Model) ensureDetailMetricsResolved() {
	if m == nil || m.detailMetricsResolved {
		return
	}
	ranges := make([]lineRange, len(m.transcript))
	for i := range ranges {
		ranges[i] = lineRange{Start: -1, End: -1}
	}
	blockRanges := make([]lineRange, len(m.detailBlocks))
	lineOffset := 0
	for idx, block := range m.detailBlocks {
		if idx > 0 && transcriptRoleGroupsNeedSeparator(m.detailBlocks[idx-1].role, block.role) {
			lineOffset++
		}
		blockLines := m.detailBlockLinesAt(idx)
		start := lineOffset
		end := start + len(blockLines) - 1
		blockRanges[idx] = lineRange{Start: start, End: end}
		localIndex := block.entryIndex - m.transcriptBaseOffset
		if localIndex >= 0 && localIndex < len(ranges) {
			if ranges[localIndex].Start < 0 {
				ranges[localIndex] = lineRange{Start: start, End: end}
			} else {
				ranges[localIndex] = lineRange{Start: ranges[localIndex].Start, End: end}
			}
		}
		lineOffset += len(blockLines)
	}
	if lineOffset == 0 {
		lineOffset = 1
	}
	m.detailEntryLineRanges = ranges
	m.detailBlockLineRanges = blockRanges
	m.detailEntryRangeOffset = m.transcriptBaseOffset
	m.detailTotalLineCount = lineOffset
	m.detailMetricsResolved = true
}

func (m *Model) detailBlockLinesAt(idx int) []string {
	if m == nil || idx < 0 || idx >= len(m.detailBlocks) {
		return []string{""}
	}
	block := m.detailBlocks[idx]
	symbolOverride := m.detailExpansionSymbolOverride(block)
	if symbolOverride == "" && len(m.detailBlockLines[idx]) > 0 {
		return m.detailBlockLines[idx]
	}
	lines := block.render(*m, symbolOverride)
	if len(lines) == 0 {
		lines = []string{""}
	}
	if symbolOverride != "" {
		return append([]string(nil), lines...)
	}
	m.detailBlockLines[idx] = append([]string(nil), lines...)
	return m.detailBlockLines[idx]
}

func (m *Model) detailViewportFromBottom() ([]string, []VisibleLineKind, []int) {
	lines, kinds, owners, _ := m.detailViewportFromBottomOffset(0)
	return lines, kinds, owners
}

func (m *Model) detailViewportFromBottomOffset(offset int) ([]string, []VisibleLineKind, []int, int) {
	lines := make([]string, 0, m.viewportLines)
	kinds := make([]VisibleLineKind, 0, m.viewportLines)
	owners := make([]int, 0, m.viewportLines)
	if offset < 0 {
		offset = 0
	}
	remainingSkip := offset
	totalLines := 0
	for idx := len(m.detailBlocks) - 1; idx >= 0 && len(lines) < m.viewportLines; idx-- {
		block := m.detailBlocks[idx]
		blockLines := m.detailBlockLinesAt(idx)
		totalLines += len(blockLines)
		for lineIdx := len(blockLines) - 1; lineIdx >= 0 && len(lines) < m.viewportLines; lineIdx-- {
			if remainingSkip > 0 {
				remainingSkip--
				continue
			}
			lines = append(lines, blockLines[lineIdx])
			kinds = append(kinds, VisibleLineContent)
			owners = append(owners, block.entryIndex)
		}
		if idx > 0 && transcriptRoleGroupsNeedSeparator(m.detailBlocks[idx-1].role, block.role) {
			totalLines++
		}
		if idx > 0 && transcriptRoleGroupsNeedSeparator(m.detailBlocks[idx-1].role, block.role) && len(lines) < m.viewportLines {
			if remainingSkip > 0 {
				remainingSkip--
				continue
			}
			lines = append(lines, detailItemSeparator)
			kinds = append(kinds, VisibleLineContent)
			owners = append(owners, -1)
		}
	}
	clampedOffset := offset - remainingSkip
	maxOffset := max(0, totalLines-m.viewportLines)
	if clampedOffset > maxOffset {
		clampedOffset = maxOffset
	}
	reverseStrings(lines)
	reverseVisibleKinds(kinds)
	reverseInts(owners)
	return lines, kinds, owners, clampedOffset
}

func (m *Model) detailViewportFromScroll(start int) ([]string, []VisibleLineKind, []int) {
	if m.viewportLines <= 0 {
		return nil, nil, nil
	}
	m.ensureDetailMetricsResolved()
	end := start + m.viewportLines
	lines := make([]string, 0, m.viewportLines)
	kinds := make([]VisibleLineKind, 0, m.viewportLines)
	owners := make([]int, 0, m.viewportLines)
	blockRanges := m.detailBlockLineRanges
	idx := firstDetailBlockAtOrAfterLine(blockRanges, start)
	if idx < 0 {
		return lines, kinds, owners
	}
	for ; idx < len(m.detailBlocks); idx++ {
		blockRange := blockRanges[idx]
		if blockRange.Start > end {
			break
		}
		block := m.detailBlocks[idx]
		if idx > 0 && transcriptRoleGroupsNeedSeparator(m.detailBlocks[idx-1].role, block.role) {
			separatorLine := blockRange.Start - 1
			if separatorLine >= start && separatorLine < end {
				lines = append(lines, detailItemSeparator)
				kinds = append(kinds, VisibleLineContent)
				owners = append(owners, -1)
			}
		}
		blockLines := m.detailBlockLinesAt(idx)
		blockStart := blockRange.Start
		blockEnd := blockRange.End + 1
		if blockEnd > start && blockStart < end {
			from := max(0, start-blockStart)
			to := min(len(blockLines), end-blockStart)
			for _, line := range blockLines[from:to] {
				lines = append(lines, line)
				kinds = append(kinds, VisibleLineContent)
				owners = append(owners, block.entryIndex)
			}
		}
		if blockEnd >= end {
			break
		}
	}
	return lines, kinds, owners
}

func firstDetailBlockAtOrAfterLine(ranges []lineRange, line int) int {
	idx := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].End >= line
	})
	if idx >= len(ranges) {
		return -1
	}
	return idx
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseVisibleKinds(values []VisibleLineKind) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseInts(values []int) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
