package tui

import (
	"builder/shared/textutil"
	"builder/shared/toolspec"
	"builder/shared/transcript"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func detailDivider() string {
	return TranscriptDivider
}

func ongoingDividerGroup(role RenderIntent) string {
	normalized := normalizeOngoingDividerRole(role)
	if normalized.IsToolHeadline() {
		return "tool"
	}
	return normalized.String()
}

func transcriptRoleGroupsNeedSeparator(previousRole RenderIntent, currentRole RenderIntent) bool {
	return ongoingDividerGroup(previousRole) != ongoingDividerGroup(currentRole)
}

func normalizeOngoingDividerRole(role RenderIntent) RenderIntent {
	if role == RenderIntentAssistantCommentary {
		return RenderIntentAssistant
	}
	return role
}

func skipInOngoing(entry TranscriptEntry) bool {
	return !isVisibleInOngoing(entry)
}

func compactToolCallText(meta *transcript.ToolCallMeta, text string) string {
	return transcript.CompactToolCallText(meta, text)
}

func compactOngoingShellPreviewText(command string) string {
	normalized := textutil.NormalizeCRLF(command)
	if !strings.Contains(normalized, "\n") {
		return command
	}
	for _, line := range strings.Split(normalized, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return trimmed + "\n…"
	}
	return "…"
}

func shellPreviewShouldCollapse(command string) bool {
	return strings.Contains(textutil.NormalizeCRLF(command), "\n")
}

func compactReviewerStatusForOngoing(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	for _, line := range strings.Split(trimmed, "\n") {
		candidate := strings.TrimSpace(line)
		if candidate != "" {
			return candidate
		}
	}
	return trimmed
}

func compactReviewerSuggestionsForOngoing(text string) string {
	return strings.TrimSpace(text)
}

func askQuestionDisplay(meta *transcript.ToolCallMeta, text string) (string, []string, int) {
	_ = text
	question := ""
	suggestions := make([]string, 0)
	recommendedOptionIndex := 0
	if meta != nil {
		question = normalizeAskQuestionQuestion(meta.Question)
		if question == "" {
			question = normalizeAskQuestionQuestion(meta.Command)
		}
		recommendedOptionIndex = meta.RecommendedOptionIndex
		for _, suggestion := range meta.Suggestions {
			trimmed := normalizeAskQuestionSuggestion(suggestion)
			if trimmed == "" {
				continue
			}
			suggestions = append(suggestions, trimmed)
		}
	}
	if question == "" {
		question = "ask_question"
	}
	return question, suggestions, recommendedOptionIndex
}

func normalizeAskQuestionQuestion(question string) string {
	trimmed := strings.TrimSpace(question)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, "ask_question") {
		return ""
	}
	return trimmed
}

func normalizeAskQuestionSuggestion(suggestion string) string {
	return strings.TrimSpace(suggestion)
}

func (m Model) flattenAskQuestionEntry(role RenderIntent, question string, suggestions []string, recommendedOptionIndex int, answer string, includeSuggestions bool) []string {
	return m.flattenAskQuestionEntryWithSymbol(role, question, suggestions, recommendedOptionIndex, answer, includeSuggestions, "")
}

func (m Model) flattenAskQuestionEntryWithSymbol(role RenderIntent, question string, suggestions []string, recommendedOptionIndex int, answer string, includeSuggestions bool, symbolOverride string) []string {
	renderWidth := m.entryRenderWidth(role, symbolOverride)
	continuationPrefix := m.entryContinuationPrefix(role, symbolOverride)

	type askQuestionLine struct {
		text string
		kind string
	}

	lines := make([]askQuestionLine, 0, len(suggestions)+4)
	question = strings.TrimSpace(question)
	if question == "" {
		question = "ask question"
	}
	for _, line := range m.renderAskQuestionMarkdownLines(role, question, renderWidth) {
		lines = append(lines, askQuestionLine{text: line, kind: "question"})
	}
	if includeSuggestions {
		for index, suggestion := range suggestions {
			suggestion = normalizeAskQuestionSuggestion(suggestion)
			if suggestion == "" {
				continue
			}
			recommended := recommendedOptionIndex == index+1
			wrapped := splitLines(wrapTextForViewport(suggestion, max(1, renderWidth-2)))
			for idx, line := range wrapped {
				if idx == 0 {
					kind := "suggestion"
					if recommended {
						kind = "recommended_suggestion"
					}
					lines = append(lines, askQuestionLine{text: "- " + line, kind: kind})
					continue
				}
				kind := "suggestion"
				if recommended {
					kind = "recommended_suggestion"
				}
				lines = append(lines, askQuestionLine{text: "  " + line, kind: kind})
			}
		}
	}
	answer = strings.TrimSpace(answer)
	if answer != "" {
		for _, line := range splitLines(wrapTextForViewport(answer, renderWidth)) {
			lines = append(lines, askQuestionLine{text: line, kind: "answer"})
		}
	}
	if len(lines) == 0 {
		lines = append(lines, askQuestionLine{text: "", kind: "question"})
	}

	prefix := m.entryPrefix(role, symbolOverride)
	out := make([]string, 0, len(lines))
	for idx, line := range lines {
		display := line.text
		switch line.kind {
		case "suggestion":
			display = m.palette().preview.Faint(true).Render(display)
		case "recommended_suggestion":
			display = m.palette().model.Render(display)
		case "answer":
			if role == RenderIntentToolQuestionError {
				display = applyANSIStyleIntents(display, m.ansiIntentPalette(), ErrorForeground)
			} else {
				display = m.palette().user.Render(display)
			}
		}
		if idx == 0 {
			if prefix == "" {
				out = append(out, display)
				continue
			}
			out = append(out, prefix+display)
			continue
		}
		if strings.TrimSpace(display) == "" {
			out = append(out, "")
			continue
		}
		out = append(out, continuationPrefix+display)
	}
	return out
}

func (m Model) renderAskQuestionMarkdownLines(role RenderIntent, question string, renderWidth int) []string {
	if renderWidth < 1 {
		renderWidth = 1
	}
	if m.md != nil {
		if rendered, err := m.md.render(role, question, renderWidth); err == nil {
			lines := splitLines(rendered)
			if len(lines) > 0 {
				return lines
			}
		}
	}
	lines := splitLines(wrapTextForViewport(question, renderWidth))
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func toolCallDisplayText(meta *transcript.ToolCallMeta, text string) string {
	command := strings.TrimSpace(text)
	inlineMeta := ""
	if meta != nil && strings.TrimSpace(meta.Command) != "" {
		command = strings.TrimSpace(meta.Command)
	}
	if meta != nil && strings.TrimSpace(meta.PatchDetail) != "" && strings.TrimSpace(command) == "" {
		command = strings.TrimSpace(meta.PatchDetail)
	}
	if command == "" {
		command = transcript.CompactToolCallText(meta, text)
	}
	if meta != nil && meta.UsesShellRendering() && meta.UserInitiated {
		command = "User ran: " + command
	}
	if meta != nil {
		inlineMeta = strings.TrimSpace(meta.InlineMeta)
		if inlineMeta == "" {
			inlineMeta = strings.TrimSpace(meta.TimeoutLabel)
		}
	}
	if inlineMeta == "" {
		return command
	}
	return command + transcript.InlineMetaSeparator + inlineMeta
}

func isShellToolCall(meta *transcript.ToolCallMeta, text string) bool {
	_ = text
	return meta != nil && meta.UsesShellRendering()
}

func isPatchToolCall(meta *transcript.ToolCallMeta) bool {
	if meta == nil {
		return false
	}
	switch strings.TrimSpace(meta.ToolName) {
	case "patch", "edit":
		return true
	default:
		return meta.HasPatchDetail()
	}
}

func isPatchToolBlock(role RenderIntent, meta *transcript.ToolCallMeta) bool {
	if meta != nil && (isPatchToolCall(meta) || meta.PatchRender != nil) {
		return true
	}
	switch role {
	case RenderIntentToolPatch, RenderIntentToolPatchSuccess, RenderIntentToolPatchError:
		return true
	default:
		return false
	}
}

func isAskQuestionToolCall(meta *transcript.ToolCallMeta) bool {
	return meta != nil && meta.UsesAskQuestionRendering()
}

func isWebSearchToolCall(meta *transcript.ToolCallMeta) bool {
	return meta != nil && strings.TrimSpace(meta.ToolName) == string(toolspec.ToolWebSearch)
}

func isToolHeadlineRole(role RenderIntent) bool {
	return role.IsToolHeadline()
}

func isToolErrorHeadlineRole(role RenderIntent) bool {
	return role.IsToolErrorHeadline()
}

func isShellPreviewRole(role RenderIntent) bool {
	return role.IsShellPreview()
}

func splitToolInlineMeta(line string) (string, string) {
	return transcript.SplitInlineMeta(line)
}

func (m Model) renderToolHeadline(line string, width int) string {
	command, meta := splitToolInlineMeta(line)
	if meta == "" {
		return command
	}
	metaText := m.palette().preview.Faint(true).Render(meta)
	if command == "" {
		return metaText
	}
	space := width - lipgloss.Width(command) - lipgloss.Width(metaText)
	if space < 1 {
		space = 1
	}
	return command + strings.Repeat(" ", space) + metaText
}

func (m Model) tintToolDiffLine(line, kind string) string {
	if strings.TrimSpace(line) == "" {
		return line
	}
	if width := m.viewportWidth; width > 0 {
		lineWidth := lipgloss.Width(line)
		if lineWidth < width {
			line += strings.Repeat(" ", width-lineWidth)
		}
	}
	addBg, removeBg := m.diffLineBackgroundEscapes()
	if kind == "add" {
		return applyBackgroundTint(line, addBg)
	}
	if kind == "remove" {
		return applyBackgroundTint(line, removeBg)
	}
	return line
}

func (m Model) diffLineBackgroundEscapes() (string, string) {
	p := m.palette()
	return bgEscape(p.diffAddBackground), bgEscape(p.diffRemoveBackground)
}

func (m Model) styleToolLine(line string, isPatchBlock bool) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line
	}
	if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
		return m.palette().toolSuccess.Render("+") + line[1:]
	}
	if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
		return m.palette().toolError.Render("-") + line[1:]
	}
	successCountStyle := m.palette().toolSuccess
	errorCountStyle := m.palette().toolError
	if isPatchBlock {
		return patchCountTokenPattern.ReplaceAllStringFunc(line, func(token string) string {
			if strings.HasPrefix(token, "+") {
				return successCountStyle.Render(token)
			}
			if strings.HasPrefix(token, "-") {
				return errorCountStyle.Render(token)
			}
			return token
		})
	}
	if !strings.HasPrefix(trimmed, "./") {
		return line
	}
	return patchCountTokenPattern.ReplaceAllStringFunc(line, func(token string) string {
		if strings.HasPrefix(token, "+") {
			return successCountStyle.Render(token)
		}
		if strings.HasPrefix(token, "-") {
			return errorCountStyle.Render(token)
		}
		return token
	})
}
