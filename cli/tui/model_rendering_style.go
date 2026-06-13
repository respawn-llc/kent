package tui

import (
	"core/shared/theme"
	"core/shared/transcript"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type toolResultIndex struct {
	results map[string][]int
	cursors map[string]int
}

func buildToolResultIndex(entries []TranscriptEntry) toolResultIndex {
	index := toolResultIndex{
		results: make(map[string][]int),
		cursors: make(map[string]int),
	}
	for idx, entry := range entries {
		if !TranscriptRoleFromWire(string(entry.Role)).IsToolResult() {
			continue
		}
		callID := strings.TrimSpace(entry.ToolCallID)
		if callID == "" {
			continue
		}
		index.results[callID] = append(index.results[callID], idx)
	}
	return index
}

func (index toolResultIndex) findMatchingToolResultIndex(entries []TranscriptEntry, callIdx int, consumed map[int]struct{}) int {
	if callIdx < 0 || callIdx >= len(entries) {
		return -1
	}
	callID := strings.TrimSpace(entries[callIdx].ToolCallID)
	nextIdx := callIdx + 1
	if nextIdx < len(entries) {
		if _, used := consumed[nextIdx]; !used && TranscriptRoleFromWire(string(entries[nextIdx].Role)).IsToolResult() {
			nextCallID := strings.TrimSpace(entries[nextIdx].ToolCallID)
			if callID == nextCallID {
				return nextIdx
			}
		}
	}
	if callID == "" {
		return -1
	}
	results := index.results[callID]
	for cursor := index.cursors[callID]; cursor < len(results); cursor++ {
		resultIdx := results[cursor]
		if resultIdx <= callIdx {
			index.cursors[callID] = cursor + 1
			continue
		}
		if _, used := consumed[resultIdx]; used {
			index.cursors[callID] = cursor + 1
			continue
		}
		index.cursors[callID] = cursor
		return resultIdx
	}
	index.cursors[callID] = len(results)
	return -1
}

func (m Model) roleSymbol(role RenderIntent) string {
	prefix := rolePrefix(role)
	if prefix == "" {
		return ""
	}
	p := m.palette()
	if style := transcriptMessageStyleForIntent(role); style != transcriptMessageStyleNone {
		return renderRoleSymbol(prefix, roleSymbolStyle(role, p))
	}
	switch role {
	case RenderIntentTool, RenderIntentToolSuccess, RenderIntentToolError, RenderIntentToolShell, RenderIntentToolShellSuccess, RenderIntentToolShellError, RenderIntentToolPatch, RenderIntentToolPatchSuccess, RenderIntentToolPatchError, RenderIntentToolQuestion, RenderIntentToolQuestionError, RenderIntentToolWebSearch, RenderIntentToolWebSearchSuccess, RenderIntentToolWebSearchError:
		return renderRoleSymbol(prefix, roleSymbolStyle(role, p))
	case RenderIntentDeveloperContext, RenderIntentDeveloperFeedback, RenderIntentInterruption, RenderIntentGoalFeedback:
		return renderRoleSymbol(prefix, roleSymbolStyle(role, p))
	default:
		if TranscriptRole(role).IsCompaction() {
			return renderRoleSymbol(prefix, roleSymbolStyle(role, p))
		}
		return prefix
	}
}

type roleSymbolColorStyle struct {
	color rgbColor
	faint bool
}

func roleSymbolStyle(role RenderIntent, p palette) roleSymbolColorStyle {
	if TranscriptRole(role).IsCompaction() {
		return roleSymbolColorStyle{color: p.compactionColor}
	}
	switch transcriptMessageStyleForIntent(role) {
	case transcriptMessageStyleSuccess:
		return roleSymbolColorStyle{color: p.successColor}
	case transcriptMessageStyleWarning:
		return roleSymbolColorStyle{color: p.warningColor}
	case transcriptMessageStyleError:
		return roleSymbolColorStyle{color: p.errorColor}
	}
	switch role {
	case RenderIntentToolSuccess, RenderIntentToolShellSuccess, RenderIntentToolPatchSuccess, RenderIntentToolWebSearchSuccess:
		return roleSymbolColorStyle{color: p.toolSuccessColor}
	case RenderIntentToolError, RenderIntentToolShellError, RenderIntentToolPatchError, RenderIntentToolWebSearchError:
		return roleSymbolColorStyle{color: p.toolErrorColor}
	case RenderIntentToolQuestion:
		return roleSymbolColorStyle{color: p.userColor}
	case RenderIntentToolQuestionError, RenderIntentDeveloperFeedback, RenderIntentInterruption:
		return roleSymbolColorStyle{color: p.errorColor}
	case RenderIntentDeveloperContext:
		return roleSymbolColorStyle{color: p.foregroundColor}
	case RenderIntentGoalFeedback:
		return roleSymbolColorStyle{color: p.primaryColor}
	case RenderIntentTool, RenderIntentToolShell, RenderIntentToolPatch, RenderIntentToolWebSearch:
		return roleSymbolColorStyle{color: p.toolColor}
	default:
		return roleSymbolColorStyle{color: p.foregroundColor}
	}
}

func renderRoleSymbol(prefix string, style roleSymbolColorStyle) string {
	transform := ansiStyleTransform{DefaultForeground: &style.color, ForceFaint: style.faint}
	return styleEscape(transform, false) + prefix + "\x1b[0m"
}

func (m Model) shellWarningSymbolOverride(role RenderIntent, meta *transcript.ToolCallMeta) string {
	if !usesWarningShellSymbol(role, meta) {
		return ""
	}
	prefix := rolePrefix(role)
	if prefix == "" {
		return ""
	}
	symbol := renderRoleSymbol(prefix, roleSymbolColorStyle{color: m.palette().warningColor})
	if role.IsToolHeadline() && m.toolSymbolGap > 1 {
		return symbol + strings.Repeat(" ", m.toolSymbolGap)
	}
	return symbol + " "
}

func usesWarningShellSymbol(role RenderIntent, meta *transcript.ToolCallMeta) bool {
	if !role.IsShellPreview() || meta == nil {
		return false
	}
	normalized := transcript.NormalizeToolCallMeta(*meta)
	return normalized.RawOutputRequested || normalized.OutputTruncated
}

func rolePrefix(role RenderIntent) string {
	if TranscriptRole(role).IsCompaction() {
		return "@"
	}
	if symbol := transcriptMessageStyleSymbol(transcriptMessageStyleForIntent(role)); symbol != "" {
		return symbol
	}
	switch role {
	case RenderIntentUser:
		return "❯"
	case RenderIntentAssistant, RenderIntentAssistantCommentary:
		return "❮"
	case RenderIntentTool, RenderIntentToolSuccess, RenderIntentToolError:
		return "•"
	case RenderIntentToolWebSearch, RenderIntentToolWebSearchSuccess, RenderIntentToolWebSearchError:
		return "@"
	case RenderIntentToolShell, RenderIntentToolShellSuccess, RenderIntentToolShellError:
		return "$"
	case RenderIntentToolPatch, RenderIntentToolPatchSuccess, RenderIntentToolPatchError:
		return "⇄"
	case RenderIntentToolQuestion, RenderIntentToolQuestionError:
		return "?"
	case RenderIntentDeveloperContext:
		return "ℹ"
	case RenderIntentDeveloperFeedback:
		return "!"
	case RenderIntentInterruption:
		return "!"
	case RenderIntentGoalFeedback:
		return "ℹ"
	default:
		return ""
	}
}

func styleForRole(role RenderIntent, p palette) lipgloss.Style {
	if TranscriptRole(role).IsCompaction() {
		return p.compaction
	}
	switch transcriptMessageStyleForIntent(role) {
	case transcriptMessageStyleSuccess:
		return p.success
	case transcriptMessageStyleWarning:
		return p.warning
	case transcriptMessageStyleError:
		return p.error
	}
	switch role {
	case RenderIntentUser:
		return p.user
	case RenderIntentAssistant, RenderIntentAssistantCommentary:
		return p.model
	case RenderIntentTool:
		return p.tool
	case RenderIntentToolSuccess:
		return p.toolSuccess
	case RenderIntentToolError:
		return p.toolError
	case RenderIntentToolWebSearch:
		return p.tool
	case RenderIntentToolWebSearchSuccess:
		return p.toolSuccess
	case RenderIntentToolWebSearchError:
		return p.toolError
	case RenderIntentToolShell:
		return p.tool
	case RenderIntentToolShellSuccess:
		return p.toolSuccess
	case RenderIntentToolShellError:
		return p.toolError
	case RenderIntentToolPatch:
		return p.tool
	case RenderIntentToolPatchSuccess:
		return p.toolSuccess
	case RenderIntentToolPatchError:
		return p.toolError
	case RenderIntentToolQuestion:
		return p.user
	case RenderIntentToolQuestionError:
		return p.toolError
	case RenderIntentSystem:
		return p.system
	case RenderIntentDeveloperContext:
		return p.preview
	case RenderIntentDeveloperFeedback:
		return p.warning
	case RenderIntentInterruption:
		return p.error
	case RenderIntentGoalFeedback:
		return p.primary
	case RenderIntentReasoning, RenderIntentThinkingTrace:
		return p.system
	default:
		return p.preview
	}
}

type palette struct {
	foregroundColor          rgbColor
	selectionForegroundColor rgbColor
	selectionBackgroundColor rgbColor
	primaryColor             rgbColor
	preview                  lipgloss.Style
	previewColor             rgbColor
	userColor                rgbColor
	modelColor               rgbColor
	toolColor                rgbColor
	toolSuccessColor         rgbColor
	toolErrorColor           rgbColor
	successColor             rgbColor
	warningColor             rgbColor
	errorColor               rgbColor
	compactionColor          rgbColor
	user                     lipgloss.Style
	model                    lipgloss.Style
	tool                     lipgloss.Style
	toolSuccess              lipgloss.Style
	toolError                lipgloss.Style
	primary                  lipgloss.Style
	system                   lipgloss.Style
	error                    lipgloss.Style
	warning                  lipgloss.Style
	success                  lipgloss.Style
	compaction               lipgloss.Style
	selection                lipgloss.Style

	diffAddBackground    string
	diffRemoveBackground string
}

func (m Model) palette() palette {
	tokens := theme.ResolvePalette(m.theme)
	return palette{
		foregroundColor:          rgbColorFromHex(tokens.Transcript.Foreground.TrueColor),
		selectionForegroundColor: rgbColorFromHex(tokens.Transcript.SelectionForeground.TrueColor),
		selectionBackgroundColor: rgbColorFromHex(tokens.Transcript.SelectionBackground.TrueColor),
		primaryColor:             rgbColorFromHex(tokens.App.Primary.TrueColor),
		preview:                  lipgloss.NewStyle().Foreground(tokens.Transcript.Subdued.Lipgloss()),
		previewColor:             rgbColorFromHex(tokens.Transcript.Subdued.TrueColor),
		userColor:                rgbColorFromHex(tokens.Transcript.User.TrueColor),
		modelColor:               rgbColorFromHex(tokens.Transcript.Assistant.TrueColor),
		toolColor:                rgbColorFromHex(tokens.Transcript.Tool.TrueColor),
		toolSuccessColor:         rgbColorFromHex(tokens.Transcript.ToolSuccess.TrueColor),
		toolErrorColor:           rgbColorFromHex(tokens.Transcript.ToolError.TrueColor),
		successColor:             rgbColorFromHex(tokens.Transcript.Success.TrueColor),
		warningColor:             rgbColorFromHex(tokens.Transcript.Warning.TrueColor),
		errorColor:               rgbColorFromHex(tokens.Transcript.Error.TrueColor),
		compactionColor:          rgbColorFromHex(tokens.Transcript.Compaction.TrueColor),
		user:                     lipgloss.NewStyle().Foreground(tokens.Transcript.User.Lipgloss()),
		model:                    lipgloss.NewStyle().Foreground(tokens.Transcript.Assistant.Lipgloss()),
		tool:                     lipgloss.NewStyle().Foreground(tokens.Transcript.Tool.Lipgloss()),
		toolSuccess:              lipgloss.NewStyle().Foreground(tokens.Transcript.ToolSuccess.Lipgloss()),
		toolError:                lipgloss.NewStyle().Foreground(tokens.Transcript.ToolError.Lipgloss()),
		primary:                  lipgloss.NewStyle().Foreground(tokens.App.Primary.Lipgloss()),
		system:                   lipgloss.NewStyle().Foreground(tokens.Transcript.System.Lipgloss()).Faint(true),
		error:                    lipgloss.NewStyle().Foreground(tokens.Transcript.Error.Lipgloss()),
		warning:                  lipgloss.NewStyle().Foreground(tokens.Transcript.Warning.Lipgloss()),
		success:                  lipgloss.NewStyle().Foreground(tokens.Transcript.Success.Lipgloss()),
		compaction:               lipgloss.NewStyle().Foreground(tokens.Transcript.Compaction.Lipgloss()),
		selection: lipgloss.NewStyle().
			Background(tokens.Transcript.SelectionBackground.Lipgloss()).
			Foreground(tokens.Transcript.SelectionForeground.Lipgloss()),

		diffAddBackground:    tokens.Transcript.DiffAddBackground.TrueColor,
		diffRemoveBackground: tokens.Transcript.DiffRemoveBackground.TrueColor,
	}
}

func rgbColorFromHex(hex string) rgbColor {
	r, g, b, ok := parseHexColor(hex)
	if !ok {
		return rgbColor{}
	}
	return rgbColor{r: r, g: g, b: b}
}

func (m Model) ansiIntentPalette() ansiIntentPalette {
	colors := m.palette()
	return ansiIntentPalette{
		ThemeForeground:   colors.foregroundColor,
		SubduedForeground: colors.previewColor,
		PrimaryForeground: colors.primaryColor,
		SuccessForeground: colors.successColor,
		WarningForeground: colors.warningColor,
		ErrorForeground:   colors.errorColor,
	}
}

func splitLines(v string) []string {
	if v == "" {
		return []string{""}
	}
	return strings.Split(v, "\n")
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func cloneToolCallMeta(in *transcript.ToolCallMeta) *transcript.ToolCallMeta {
	if in == nil {
		return nil
	}
	out := *in
	out = transcript.NormalizeToolCallMeta(out)
	if in.RenderHint != nil {
		hint := *in.RenderHint
		out.RenderHint = &hint
	}
	if len(in.Suggestions) > 0 {
		out.Suggestions = append([]string(nil), in.Suggestions...)
	}
	return &out
}
