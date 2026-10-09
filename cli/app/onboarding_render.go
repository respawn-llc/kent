package app

import (
	"fmt"
	"strings"

	"core/shared/config"
	sharedtheme "core/shared/theme"

	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
)

type onboardingStyles struct {
	title          lipgloss.Style
	body           lipgloss.Style
	helper         lipgloss.Style
	footer         lipgloss.Style
	option         lipgloss.Style
	optionSelected lipgloss.Style
	number         lipgloss.Style
	numberSelected lipgloss.Style
	description    lipgloss.Style
	warning        lipgloss.Style
	errorText      lipgloss.Style
	checkbox       lipgloss.Style
	checkboxOn     lipgloss.Style
	spinner        lipgloss.Style
	inputText      lipgloss.Style
	inputBorder    lipgloss.Style
	group          lipgloss.Style
	valueNeutral   lipgloss.Style
	valueOn        lipgloss.Style
	valueOff       lipgloss.Style
}

func newOnboardingStyles(theme string) onboardingStyles {
	palette := uiPalette(theme)
	return onboardingStyles{
		title:          lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		body:           lipgloss.NewStyle().Foreground(palette.foreground),
		helper:         lipgloss.NewStyle().Foreground(palette.muted).Faint(true),
		footer:         lipgloss.NewStyle().Foreground(palette.muted).Faint(true),
		option:         lipgloss.NewStyle().Foreground(palette.foreground),
		optionSelected: lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		number:         lipgloss.NewStyle().Foreground(palette.muted),
		numberSelected: lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		description:    lipgloss.NewStyle().Foreground(palette.muted).Faint(true),
		warning:        lipgloss.NewStyle().Foreground(sharedtheme.DefaultPalette().Status.Error.Adaptive()).Bold(true),
		errorText:      lipgloss.NewStyle().Foreground(sharedtheme.DefaultPalette().Status.Error.Adaptive()).Bold(true),
		checkbox:       lipgloss.NewStyle().Foreground(palette.muted),
		checkboxOn:     lipgloss.NewStyle().Foreground(palette.secondary).Bold(true),
		spinner:        lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		inputText:      lipgloss.NewStyle().Foreground(palette.foreground),
		inputBorder:    lipgloss.NewStyle().Foreground(palette.primary),
		group:          lipgloss.NewStyle().Foreground(palette.foreground).Bold(true),
		valueNeutral:   lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		valueOn:        lipgloss.NewStyle().Foreground(palette.secondary).Bold(true),
		valueOff:       lipgloss.NewStyle().Foreground(sharedtheme.DefaultPalette().Status.Error.Adaptive()).Bold(true),
	}
}

type onboardingRenderedContent struct {
	lines     []string
	cursorRow int
	cursorCol int
}

func (m *onboardingModel) View() string {
	if m.finalizing || m.currentScreen.Kind == onboardingScreenLoading {
		return m.renderLoadingView()
	}
	headerLines := m.renderHeaderLines(max(1, m.width))
	footerLines := m.renderFooterLines(max(1, m.width))
	content := m.buildContent(max(1, m.width))
	contentHeight := m.contentHeight()
	maxOffset := len(content.lines) - contentHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	end := m.offset + contentHeight
	if end > len(content.lines) {
		end = len(content.lines)
	}
	visible := content.lines
	if len(content.lines) > 0 {
		visible = content.lines[m.offset:end]
	}
	viewLines := append(headerLines, "")
	contentStartRow := len(viewLines)
	viewLines = append(viewLines, visible...)
	if filler := m.contentHeight() - len(visible); filler > 0 {
		viewLines = append(viewLines, make([]string, filler)...)
	}
	if len(footerLines) > 0 {
		viewLines = append(viewLines, "")
		viewLines = append(viewLines, footerLines...)
	}
	m.updateTerminalCursor(contentStartRow, content, visible)
	return strings.Join(viewLines, "\n")
}

func (m *onboardingModel) updateTerminalCursor(contentStartRow int, content onboardingRenderedContent, visible []string) {
	if m.terminalCursor == nil {
		return
	}
	if m.currentScreen.Kind != onboardingScreenInput || content.cursorRow < 0 {
		m.terminalCursor.Clear()
		return
	}
	visibleCursorRow := content.cursorRow - m.offset
	if visibleCursorRow < 0 || visibleCursorRow >= len(visible) {
		m.terminalCursor.Clear()
		return
	}
	m.terminalCursor.Set(uiTerminalCursorPlacement{
		Visible:   true,
		CursorRow: contentStartRow + visibleCursorRow,
		CursorCol: content.cursorCol,
		AnchorRow: max(0, m.height-1),
		AltScreen: true,
	})
}

func (m *onboardingModel) contentHeight() int {
	headerLines := m.renderHeaderLines(max(1, m.width))
	footerLines := m.renderFooterLines(max(1, m.width))
	height := m.height - len(headerLines) - len(footerLines) - 2
	if height < 1 {
		return 1
	}
	return height
}

func (m *onboardingModel) renderHeaderLines(width int) []string {
	var lines []string
	if m.currentScreen.ID == onboardingStepTheme {
		lines = append(lines, wrapANSIText(renderStartupBanner(startupBannerANSI), width)...)
		lines = append(lines, "")
	}
	return append(lines, wrapANSIText(m.styles.title.Render(m.currentScreen.Title), width)...)
}

func (m *onboardingModel) buildContent(width int) onboardingRenderedContent {
	lines := make([]string, 0, 32)
	cursorRow := -1
	cursorCol := 0
	appendBlank := func() {
		if len(lines) == 0 || lines[len(lines)-1] == "" {
			return
		}
		lines = append(lines, "")
	}
	appendWrapped := func(text string, style lipgloss.Style) {
		for _, line := range wrapStyledParagraphs(text, width, style) {
			lines = append(lines, line)
		}
	}
	body := strings.TrimSpace(m.currentScreen.Body)
	if body != "" {
		appendWrapped(body, m.styles.body)
	}
	if m.currentScreen.ThemePreview {
		appendBlank()
		for _, line := range m.renderThemePreview(width) {
			lines = append(lines, line)
		}
	}
	if m.currentScreen.ID == "review" {
		appendBlank()
		for _, line := range m.renderReviewSummary(width) {
			lines = append(lines, line)
		}
	}
	if text := strings.TrimSpace(m.currentScreen.ErrorText); text != "" {
		appendBlank()
		appendWrapped(text, m.styles.errorText)
	}
	switch m.currentScreen.Kind {
	case onboardingScreenChoice, onboardingScreenMulti:
		appendBlank()
		for index, option := range m.currentScreen.Options {
			if option.Group != "" && (index == 0 || m.currentScreen.Options[index-1].Group != option.Group) {
				if len(lines) > 0 && lines[len(lines)-1] != "" {
					lines = append(lines, "")
				}
				appendWrapped(option.Group, m.styles.group)
			}
			if index == m.cursor {
				cursorRow = len(lines)
			}
			prefix := fmt.Sprintf("%d. ", index+1)
			if m.currentScreen.Kind == onboardingScreenMulti {
				prefix += "[ ] "
				if m.selection[option.ID] {
					prefix = fmt.Sprintf("%d. [x] ", index+1)
				}
			}
			style := m.styles.option
			if index == m.cursor {
				style = m.styles.optionSelected
			}
			optionLine := style.Render(prefix + option.Title)
			if option.ID == m.currentScreen.DefaultOptionID {
				optionLine += m.styles.description.Render("  • recommended")
			}
			if warning := strings.TrimSpace(option.Warning); warning != "" {
				optionLine += m.styles.warning.Render("  " + warning)
			}
			for _, line := range wrapANSIText(optionLine, width) {
				lines = append(lines, line)
			}
			if desc := strings.TrimSpace(option.Description); desc != "" {
				for _, line := range wrapANSIText(m.styles.description.Render("  "+desc), width) {
					lines = append(lines, line)
				}
			}
		}
	case onboardingScreenInput:
		appendBlank()
		field := m.input
		field.MaxLines = inputContentLineLimit(m.contentHeight())
		renderedInput := field.Render(width)
		renderedInput = renderFramedInputField(width, renderedInput, m.styles.inputText, m.styles.inputBorder, m.terminalCursor == nil)
		if renderedInput.Cursor.Visible {
			cursorRow = len(lines) + renderedInput.Cursor.Row
			cursorCol = renderedInput.Cursor.Col
		} else {
			cursorRow = len(lines)
			cursorCol = 0
		}
		lines = append(lines, renderedInput.Lines...)
	}
	if helper := strings.TrimSpace(m.currentScreen.Helper); helper != "" {
		appendBlank()
		appendWrapped(helper, m.styles.helper)
	}
	return onboardingRenderedContent{lines: lines, cursorRow: cursorRow, cursorCol: cursorCol}
}

func (m *onboardingModel) renderFooterLines(width int) []string {
	movement := "↑/↓ pick or scroll"
	if m.currentScreen.Kind == onboardingScreenInput {
		movement = "←/→/↑/↓ move cursor"
	}
	hints := []string{
		movement,
		sharedtheme.KeyTabGlyph + " next",
		sharedtheme.KeyShiftGlyph + " + " + sharedtheme.KeyTabGlyph + " back",
		sharedtheme.KeyEnterGlyph + " confirm",
	}
	if m.currentScreen.Kind != onboardingScreenInput {
		hints = append(hints, sharedtheme.KeySpaceGlyph+" toggle")
	}
	if m.currentScreen.Kind == onboardingScreenMulti && screenHasToggleAllOption(m.currentScreen) {
		hints = append(hints, "a toggle all")
	}
	hints = append(hints, sharedtheme.KeyEscapeGlyph+" cancel")
	return wrapStyledParagraphs(strings.Join(hints, " | "), width, m.styles.footer)
}

type onboardingThemePreviewStyles struct {
	status lipgloss.Style
	input  lipgloss.Style
	help   lipgloss.Style
}

func onboardingThemePreviewStyleSet(theme string, width int) onboardingThemePreviewStyles {
	palette := uiPalette(theme)
	innerWidth := max(12, width-2)
	return onboardingThemePreviewStyles{
		status: lipgloss.NewStyle().Foreground(palette.foreground).Background(palette.background).Padding(0, 1).Width(innerWidth),
		input:  lipgloss.NewStyle().Foreground(palette.foreground).Background(palette.inputBg).Padding(0, 1).Width(innerWidth),
		help:   lipgloss.NewStyle().Foreground(palette.muted).Background(palette.background).Padding(0, 1).Width(innerWidth).Faint(true),
	}
}

func (m *onboardingModel) renderThemePreview(width int) []string {
	theme := m.activeTheme()
	palette := uiPalette(theme)
	previewStyles := onboardingThemePreviewStyleSet(theme, width)
	heading := lipgloss.NewStyle().Foreground(palette.primary).Bold(true).Render("Preview")
	modelLabel := strings.TrimSpace(m.state.selections.model.value)
	if modelLabel == "" {
		modelLabel = config.DefaultModel()
	}
	statusLine := lipgloss.NewStyle().Foreground(palette.primary).Bold(true).Render(config.Command) +
		lipgloss.NewStyle().Foreground(palette.muted).Render(" | ") +
		lipgloss.NewStyle().Foreground(palette.foreground).Render("ready") +
		lipgloss.NewStyle().Foreground(palette.muted).Render(" | ") +
		lipgloss.NewStyle().Foreground(palette.foreground).Render(modelLabel) +
		lipgloss.NewStyle().Foreground(palette.muted).Render(" | ") +
		lipgloss.NewStyle().Foreground(palette.secondary).Bold(true).Render(theme)
	inputLine := lipgloss.NewStyle().Foreground(palette.primary).Bold(true).Render("> ") + lipgloss.NewStyle().Foreground(palette.foreground).Render("Explain this failing test")
	helpLine := lipgloss.NewStyle().Foreground(palette.muted).Render("status line and input preview")
	return []string{
		heading,
		previewStyles.status.Render(statusLine),
		previewStyles.input.Render(inputLine),
		previewStyles.help.Render(helpLine),
	}
}

func (m *onboardingModel) renderReviewSummary(width int) []string {
	lines := make([]string, 0, 10)
	appendRow := func(label, value string, style lipgloss.Style) {
		row := m.styles.body.Render("- "+label+": ") + style.Render(value)
		lines = append(lines, wrapANSIText(row, width)...)
	}
	themeValue := m.state.selections.themeValue()
	themeSummary := sharedtheme.Auto + " (" + sharedtheme.Resolve(themeValue) + ")"
	if sharedtheme.IsExplicit(themeValue) {
		themeSummary = sharedtheme.Resolve(themeValue)
	}
	appendRow("Theme", themeSummary, m.styles.valueNeutral)
	appendRow("Model", m.state.selections.model.value, m.styles.valueNeutral)
	modelFact := modelFactFor(&m.state, m.state.selections.model.value)
	if modelFact.ContextWindowTokens != nil && *modelFact.ContextWindowTokens > 0 {
		contextValue := formatTokenWindow(m.state.selections.contextWindowTokens(modelFact))
		if m.state.selections.contextWindow.kind == onboardingContextDefault {
			contextValue = "default (" + formatTokenWindow(int(*modelFact.ContextWindowTokens)) + ")"
		}
		appendRow("Context window", contextValue, m.styles.valueNeutral)
	}
	thinking := strings.TrimSpace(m.state.selections.thinkingValue())
	if thinking == "" {
		appendRow("Thinking", "off", m.styles.valueOff)
	} else {
		appendRow("Thinking", thinking, m.styles.valueNeutral)
	}
	verbosity := m.state.selections.verbosity.value
	if strings.TrimSpace(verbosity) == "" {
		verbosity = "off"
	}
	verbosityStyle := m.styles.valueNeutral
	if verbosity == "off" {
		verbosityStyle = m.styles.valueOff
	}
	appendRow("Verbosity", verbosity, verbosityStyle)
	if m.state.selections.askQuestion {
		appendRow("Questions", "on", m.styles.valueOn)
	} else {
		appendRow("Questions", "off", m.styles.valueOff)
	}
	reviewer := string(m.state.selections.supervisor.frequency)
	reviewerStyle := m.styles.valueOn
	if reviewer == "off" {
		reviewerStyle = m.styles.valueOff
	}
	appendRow("Supervisor", reviewer, reviewerStyle)
	if reviewerEnabled(&m.state) {
		appendRow("Supervisor model", m.state.selections.reviewerModelValue(), m.styles.valueNeutral)
		reviewerThinking := strings.TrimSpace(m.state.selections.reviewerThinkingValue())
		reviewerThinkingStyle := m.styles.valueNeutral
		if reviewerThinking == "" {
			reviewerThinking = "off"
			reviewerThinkingStyle = m.styles.valueOff
		}
		appendRow("Supervisor thinking", reviewerThinking, reviewerThinkingStyle)
	}
	compactionStyle := m.styles.valueNeutral
	if m.state.selections.compaction == onboardingCompactionManualOnly {
		compactionStyle = m.styles.valueOff
	}
	appendRow("Compaction", string(m.state.selections.compactionValue()), compactionStyle)
	if summary := skillImportSummary(&m.state); summary != "" {
		appendRow("Skills import", summary, m.styles.valueNeutral)
	}
	if enabled, disabled := selectedSkillCounts(&m.state); enabled > 0 || disabled > 0 {
		appendRow("Enabled skills", fmt.Sprintf("%d enabled, %d disabled", enabled, disabled), m.styles.valueNeutral)
	}
	appendRow("Slash commands import", commandImportSummary(&m.state), m.styles.valueNeutral)
	return lines
}

func wrapStyledParagraphs(text string, width int, style lipgloss.Style) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	paragraphs := strings.Split(trimmed, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, wrapANSIText(style.Render(paragraph), width)...)
	}
	return lines
}

func wrapANSIText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	wrapped := ansi.Wordwrap(strings.TrimRight(text, "\n"), width, " ,.;-+|")
	if strings.TrimSpace(ansi.Strip(wrapped)) == "" {
		return []string{text}
	}
	return strings.Split(strings.TrimRight(wrapped, "\n"), "\n")
}

func (m *onboardingModel) renderLoadingView() string {
	title := m.currentScreen.Title
	if m.finalizing {
		title = "First-time setup"
	}
	loadingText := m.currentScreen.LoadingText
	if m.finalizingLabel != "" {
		loadingText = m.finalizingLabel
	}
	spinner := pendingToolSpinnerFrame(m.spinnerFrame)
	if loadingText != "" {
		spinner += " " + loadingText
	}
	content := m.styles.spinner.Render(spinner)
	if title != "" {
		content = m.styles.title.Render(title) + "\n\n" + content
	}
	if m.currentScreen.ErrorText != "" {
		content += "\n\n" + m.styles.errorText.Render(m.currentScreen.ErrorText)
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func renderedLineCount(text string) int {
	trimmed := ansi.Strip(strings.TrimRight(text, "\n"))
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}
