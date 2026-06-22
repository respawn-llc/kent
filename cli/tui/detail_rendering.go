package tui

import (
	"core/shared/transcript"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	detailTreeMiddle = "│"
	detailTreeLast   = "└"
)

func (m Model) detailEntryExpanded(entryIndex int) bool {
	if !m.compactDetail {
		return true
	}
	if m.detailExpandedEntries == nil {
		return false
	}
	_, ok := m.detailExpandedEntries[entryIndex]
	return ok
}

func (m Model) detailWithTreeGuideWithSymbol(role RenderIntent, lines []string, expanded bool, symbolOverride string) []string {
	if !m.compactDetail {
		return lines
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	out := append([]string(nil), lines...)
	if !expanded {
		out[0] = m.truncateDetailLine(out[0])
	}
	for idx := 1; idx < len(out); idx++ {
		out[idx] = m.detailTreeGuideLine(role, out[idx], idx == len(out)-1, expanded, symbolOverride)
	}
	return out
}

func (m Model) ongoingToolWithTreeGuideWithSymbol(role RenderIntent, lines []string, symbolOverride string) []string {
	if !role.IsToolHeadline() || len(lines) < 2 {
		return lines
	}
	out := append([]string(nil), lines...)
	for idx := 1; idx < len(out); idx++ {
		out[idx] = m.detailTreeGuideLine(role, out[idx], idx == len(out)-1, true, symbolOverride)
	}
	return out
}

func (m Model) detailTreeGuideLine(role RenderIntent, line string, last bool, expanded bool, symbolOverride string) string {
	connector := detailTreeMiddle
	if last {
		connector = detailTreeLast
	}
	styledConnector := m.palette().preview.Faint(true).Render(connector)
	formatLine := func(value string) string {
		if expanded {
			return value
		}
		return m.truncateDetailLine(value)
	}
	prefixWidth := lipgloss.Width(m.entryPrefix(role, symbolOverride))
	if prefixWidth <= 0 {
		return formatLine(styledConnector + " " + strings.TrimLeft(line, " "))
	}
	plainPrefix := strings.Repeat(" ", prefixWidth)
	replacement := styledConnector + strings.Repeat(" ", max(0, prefixWidth-1))
	if strings.HasPrefix(line, plainPrefix) {
		return formatLine(replacement + strings.TrimPrefix(line, plainPrefix))
	}
	if strings.TrimSpace(line) == "" {
		return formatLine(styledConnector)
	}
	return formatLine(replacement + strings.TrimLeft(line, " "))
}

func (m Model) truncateDetailLine(line string) string {
	width := m.viewportWidth
	if width < 1 {
		width = 1
	}
	for overflow := lipgloss.Width(line) - width; overflow > 0; overflow = lipgloss.Width(line) - width {
		trimmed := removeExtraSpacesFromLongestRunLongerThan(line, overflow, 1)
		if trimmed == line {
			break
		}
		line = trimmed
	}
	return truncateRenderedLineToWidthWithEllipsis(line, width, false)
}

func removeExtraSpacesFromLongestRunLongerThan(line string, count int, minLen int) string {
	if count <= 0 || line == "" {
		return line
	}
	bestStart := -1
	bestLen := 0
	for idx := 0; idx < len(line); {
		if line[idx] != ' ' {
			idx++
			continue
		}
		start := idx
		for idx < len(line) && line[idx] == ' ' {
			idx++
		}
		if length := idx - start; length > minLen && length > bestLen {
			bestStart = start
			bestLen = length
		}
	}
	if bestStart < 0 {
		return line
	}
	remove := min(count, bestLen-1)
	return line[:bestStart] + line[bestStart+remove:]
}

func (m Model) detailCollapsedStandardLinesWithSymbol(entry TranscriptEntry, role RenderIntent, text string, symbolOverride string) []string {
	if label := strings.TrimSpace(entry.CompactLabel); label != "" {
		if role == RenderIntentGoalFeedback {
			return m.detailWithTreeGuideWithSymbol(role, m.flattenPlainEntryWithIntents(role, label, PrimaryForeground, symbolOverride), false, symbolOverride)
		}
		return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, label, false, nil, symbolOverride), false, symbolOverride)
	}
	if role == RenderIntentReviewerSuggestions {
		if label := reviewerSuggestionsCollapsedLabel(entry); label != "" {
			return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, label, false, nil, symbolOverride), false, symbolOverride)
		}
		if label := strings.TrimSpace(entry.CondensedText); label != "" && strings.Contains(label, "\n") {
			return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, label, false, nil, symbolOverride), true, symbolOverride)
		}
		return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, "Supervisor suggestions", false, nil, symbolOverride), false, symbolOverride)
	}
	if label := strings.TrimSpace(entry.CondensedText); label != "" {
		if role == RenderIntentGoalFeedback {
			return m.detailWithTreeGuideWithSymbol(role, m.flattenPlainEntryWithIntents(role, label, PrimaryForeground, symbolOverride), false, symbolOverride)
		}
		return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, label, false, nil, symbolOverride), false, symbolOverride)
	}
	if isThreeLinePreviewRole(role) {
		return m.detailWithTreeGuideWithSymbol(role, firstNRenderedLines(m.flattenEntryWithMetaAndSymbol(role, text, false, nil, symbolOverride), 3), false, symbolOverride)
	}
	if label := m.knownDetailLabel(entry, role); label != "" {
		return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, label, false, nil, symbolOverride), false, symbolOverride)
	}
	return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, m.firstDetailPreviewLine(text, defaultDetailLabelForRole(role)), false, nil, symbolOverride), false, symbolOverride)
}

func (m Model) detailStandardExpandable(entry TranscriptEntry, role RenderIntent, text string) bool {
	if !m.compactDetail || m.detailRoleRendersFullWhenCollapsed(role) {
		return false
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return false
	}
	if label := strings.TrimSpace(entry.CompactLabel); label != "" {
		return label != trimmedText
	}
	if role == RenderIntentReviewerSuggestions {
		if label := reviewerSuggestionsCollapsedLabel(entry); label != "" {
			return label != trimmedText
		}
		if label := strings.TrimSpace(entry.CondensedText); label != "" && strings.Contains(label, "\n") {
			return false
		}
		return trimmedText != "Supervisor suggestions"
	}
	if label := strings.TrimSpace(entry.CondensedText); label != "" {
		return label != trimmedText
	}
	if isThreeLinePreviewRole(role) {
		return m.detailRenderedContentLineCount(role, text) > 3
	}
	if label := m.knownDetailLabel(entry, role); label != "" {
		return label != trimmedText
	}
	preview := m.firstDetailPreviewLine(text, defaultDetailLabelForRole(role))
	return preview != trimmedText || m.detailRenderedContentLineCount(role, text) > 1
}

func reviewerSuggestionsCollapsedLabel(entry TranscriptEntry) string {
	if label := strings.TrimSpace(entry.CondensedText); label != "" && !strings.Contains(label, "\n") {
		return label
	}
	return ""
}

func (m Model) detailCollapsedToolLinesWithSymbol(role RenderIntent, entry TranscriptEntry, resultSummary string, symbolOverride string) []string {
	compact := m.toolCallDisplayText(entry, role, transcriptBlockOptions{mode: transcriptBlockModeOngoing})
	if strings.TrimSpace(compact) == "" {
		compact = "Tool call"
	}
	if summary := strings.TrimSpace(resultSummary); summary != "" {
		if role.IsShellPreview() {
			compact = attachShellSummaryToFirstLine(compact, summary)
		} else {
			lines := m.flattenEntryWithMetaAndSymbol(role, compact, true, entry.ToolCall, symbolOverride)
			if role.IsToolErrorHeadline() {
				summaryLines := m.flattenToolErrorText(role, summary, strings.Repeat(" ", max(0, lipgloss.Width(m.entryPrefix(role, symbolOverride)))))
				return m.detailWithTreeGuideWithSymbol(role, append(lines, summaryLines...), false, symbolOverride)
			}
			compact += "\n" + summary
		}
	}
	if role.IsToolErrorHeadline() {
		return m.detailWithTreeGuideWithSymbol(role, m.flattenToolErrorText(role, compact, symbolOverride), false, symbolOverride)
	}
	return m.detailWithTreeGuideWithSymbol(role, m.flattenEntryWithMetaAndSymbol(role, compact, true, entry.ToolCall, symbolOverride), false, symbolOverride)
}

func (m Model) detailToolCallExpandable(role RenderIntent, entry TranscriptEntry, resultSummary string, combined string, meta *transcript.ToolCallMeta, resultText string) bool {
	if !m.compactDetail || m.detailRoleRendersFullWhenCollapsed(role) {
		return false
	}
	if strings.TrimSpace(resultText) != "" || m.detailRenderedContentLineCount(role, combined) > 1 {
		return true
	}
	if meta != nil && meta.PatchRender != nil {
		return strings.TrimSpace(meta.PatchDetail) != ""
	}
	compact := m.toolCallDisplayText(entry, role, transcriptBlockOptions{mode: transcriptBlockModeOngoing})
	return strings.TrimSpace(compact) != strings.TrimSpace(combined)
}

func (m Model) detailRenderedContentLineCount(role RenderIntent, text string) int {
	renderWidth := m.entryRenderWidth(role, "")
	lines := splitLines(wrapTextForViewport(transcriptDisplayText(role, text), renderWidth))
	if len(lines) == 0 {
		return 1
	}
	return len(lines)
}

func attachShellSummaryToFirstLine(text string, summary string) string {
	lines := splitLines(text)
	if len(lines) == 0 {
		return summary
	}
	if len(lines) > 1 {
		if hiddenSuffix := strings.TrimSpace(strings.Join(lines[1:], " ")); hiddenSuffix != "" {
			lines[0] = strings.TrimSpace(lines[0]) + " " + hiddenSuffix
		}
		lines = lines[:1]
	}
	command, meta := transcript.SplitInlineMeta(lines[0])
	if meta == "" {
		lines[0] = command + transcript.InlineMetaSeparator + summary
	} else {
		lines[0] = command + transcript.InlineMetaSeparator + meta + " · " + summary
	}
	return strings.Join(lines, "\n")
}

func (m Model) knownDetailLabel(entry TranscriptEntry, role RenderIntent) string {
	messageType := strings.TrimSpace(string(entry.MessageType))
	if messageType != "" && role == RenderIntentDeveloperContext {
		return "Developer context: " + messageType
	}
	return ""
}

func (m Model) detailRoleRendersFullWhenCollapsed(role RenderIntent) bool {
	switch role {
	case RenderIntentError, RenderIntentDeveloperErrorFeedback:
		return true
	default:
		return false
	}
}

func (m Model) firstDetailPreviewLine(text string, fallback string) string {
	for _, line := range splitLines(text) {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

func isThreeLinePreviewRole(role RenderIntent) bool {
	switch role {
	case RenderIntentUser, RenderIntentAssistant, RenderIntentAssistantCommentary:
		return true
	default:
		return false
	}
}

func firstNRenderedLines(lines []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	if len(lines) <= limit {
		return lines
	}
	return append([]string(nil), lines[:limit]...)
}

func defaultDetailLabelForRole(role RenderIntent) string {
	switch role {
	case RenderIntentSystem:
		return "System notice"
	case RenderIntentWarning:
		return "Warning"
	case RenderIntentCacheWarning:
		return "Cache warning"
	case RenderIntentReviewerStatus:
		return "Reviewer status"
	case RenderIntentReviewerSuggestions:
		return "Reviewer suggestions"
	case RenderIntentThinking, RenderIntentReasoning:
		return "Reasoning summary"
	case RenderIntentThinkingTrace:
		return "Reasoning trace"
	case RenderIntentDeveloperContext:
		return "Developer context"
	case RenderIntentDeveloperFeedback:
		return "Developer feedback"
	case RenderIntentManualCompactionCarryover:
		return "Last user message preserved for compaction"
	case RenderIntentCompactionSummary:
		return "Context compacted"
	case RenderIntentInterruption:
		return "You interrupted"
	case RenderIntentTool, RenderIntentToolSuccess, RenderIntentToolError:
		return "Tool output"
	default:
		if role == "" {
			return "Unknown entry"
		}
		return "Unknown entry: " + role.String()
	}
}
