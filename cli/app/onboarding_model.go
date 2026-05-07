package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tuiinput "builder/cli/tui/input"
	"builder/server/llm"
	"builder/shared/config"
	"builder/shared/theme"
	"builder/shared/toolspec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
)

type onboardingFinalizeDoneMsg struct {
	result onboardingResult
	err    error
}

type onboardingSpinnerTickMsg struct {
	at time.Time
}

const onboardingToggleAllOptionID = "__toggle_all__"

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
	group          lipgloss.Style
	valueNeutral   lipgloss.Style
	valueOn        lipgloss.Style
	valueOff       lipgloss.Style
}

type onboardingModel struct {
	workflow         onboardingWorkflow
	state            onboardingFlowState
	result           onboardingResult
	globalRoot       string
	workspaceRoot    string
	width            int
	height           int
	styles           onboardingStyles
	spinnerClock     frameAnimationClock
	spinnerFrame     int
	input            tuiinput.Editor
	inputMask        rune
	inputPlaceholder string
	terminalCursor   *uiTerminalCursorState
	currentScreen    onboardingScreen
	stepIndex        int
	cursor           int
	offset           int
	selection        map[string]bool
	errorText        string
	finalizing       bool
	finalizingLabel  string
	canceled         bool
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
		warning:        lipgloss.NewStyle().Foreground(statusRedColor()).Bold(true),
		errorText:      lipgloss.NewStyle().Foreground(statusRedColor()).Bold(true),
		checkbox:       lipgloss.NewStyle().Foreground(palette.muted),
		checkboxOn:     lipgloss.NewStyle().Foreground(palette.secondary).Bold(true),
		spinner:        lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		inputText:      lipgloss.NewStyle().Foreground(palette.foreground),
		group:          lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		valueNeutral:   lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		valueOn:        lipgloss.NewStyle().Foreground(palette.secondary).Bold(true),
		valueOff:       lipgloss.NewStyle().Foreground(statusRedColor()).Bold(true),
	}
}

func newOnboardingModel(globalRoot string, state onboardingFlowState) *onboardingModel {
	return newOnboardingModelForWorkspace(globalRoot, "", state)
}

func newOnboardingModelForWorkspace(globalRoot string, workspaceRoot string, state onboardingFlowState) *onboardingModel {
	input := newSingleLineEditor("")
	m := &onboardingModel{
		workflow:      newOnboardingWorkflow(&state),
		state:         state,
		globalRoot:    globalRoot,
		workspaceRoot: workspaceRoot,
		width:         defaultPickerWidth,
		height:        defaultPickerHeight,
		styles:        newOnboardingStyles(state.theme),
		input:         input,
	}
	m.spinnerClock.Start(uiAnimationNow())
	m.syncScreen(true)
	return m
}

func tickOnboardingSpinner(delay time.Duration) tea.Cmd {
	if delay <= 0 {
		delay = spinnerTickInterval
	}
	return tea.Tick(delay, func(now time.Time) tea.Msg {
		return onboardingSpinnerTickMsg{at: now}
	})
}

func (m *onboardingModel) shouldAnimateSpinner() bool {
	return m.finalizing || m.currentScreen.Kind == onboardingScreenLoading
}

func (m *onboardingModel) activeTheme() string {
	if m.currentScreen.ThemePreview && m.currentScreen.Kind == onboardingScreenChoice && m.cursor >= 0 && m.cursor < len(m.currentScreen.Options) {
		if optionTheme := strings.TrimSpace(m.currentScreen.Options[m.cursor].ID); optionTheme == "light" || optionTheme == "dark" {
			return optionTheme
		}
	}
	return theme.Resolve(m.state.settings.Theme)
}

func (m *onboardingModel) applyActiveThemeStyles() {
	activeTheme := m.activeTheme()
	m.styles = newOnboardingStyles(activeTheme)
}

func (m *onboardingModel) Init() tea.Cmd {
	m.state.imports.pending = true
	return tea.Batch(tickOnboardingSpinner(m.spinnerClock.NextDelay(uiAnimationNow(), spinnerTickInterval)), func() tea.Msg {
		return onboardingImportDiscoveryDoneMsg{discovery: discoverOnboardingImportsForWorkspace(m.globalRoot, m.workspaceRoot)}
	})
}

func (m *onboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		if typed.Width > 0 {
			m.width = typed.Width
		}
		if typed.Height > 0 {
			m.height = typed.Height
		}
		m.ensureCursorVisible()
		return m, nil
	case onboardingImportDiscoveryDoneMsg:
		m.state.imports = typed.discovery
		m.state.imports.pending = false
		if m.state.imports.skipSkills {
			m.state.skillImport = onboardingImportSelection{Mode: onboardingImportModeNone}
		}
		if m.state.imports.skipCommands {
			m.state.commandImport = onboardingImportSelection{Mode: onboardingImportModeNone}
		}
		m.syncScreen(false)
		return m, nil
	case onboardingFinalizeDoneMsg:
		m.finalizing = false
		if typed.err != nil {
			m.errorText = typed.err.Error()
			m.syncScreen(false)
			return m, nil
		}
		m.result = typed.result
		return m, tea.Quit
	case onboardingSpinnerTickMsg:
		m.spinnerFrame = m.spinnerClock.Frame(typed.at, len(pendingToolSpinner.Frames), spinnerTickInterval)
		if !m.shouldAnimateSpinner() {
			return m, nil
		}
		return m, tickOnboardingSpinner(m.spinnerClock.NextDelay(typed.at, spinnerTickInterval))
	case tea.KeyMsg:
		if m.shouldAnimateSpinner() {
			switch typed.Type {
			case tea.KeyCtrlC, tea.KeyEsc:
				m.canceled = true
				return m, tea.Quit
			}
			return m, nil
		}
		switch typed.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyUp:
			if m.currentScreen.Kind == onboardingScreenInput {
				return m, m.scrollContent(-1)
			}
			if cmd := m.moveCursor(-1); cmd != nil {
				return m, m.scrollContent(-1)
			}
			return m, nil
		case tea.KeyDown:
			if m.currentScreen.Kind == onboardingScreenInput {
				return m, m.scrollContent(1)
			}
			if cmd := m.moveCursor(1); cmd != nil {
				return m, m.scrollContent(1)
			}
			return m, nil
		case tea.KeyLeft:
			return m.goBack()
		case tea.KeyRight:
			return m.submitCurrentScreen()
		case tea.KeyEnter:
			return m.submitCurrentScreen()
		case tea.KeySpace:
			if m.currentScreen.Kind == onboardingScreenMulti {
				return m, m.toggleCurrentSelection()
			}
		case tea.KeyBackspace, tea.KeyCtrlH:
			if m.currentScreen.Kind == onboardingScreenMulti {
				return m, m.toggleCurrentSelection()
			}
		case tea.KeyRunes:
			filtered, _ := stripMouseSGRRunes(typed.Runes)
			if len(filtered) == 1 {
				if (filtered[0] == 'a' || filtered[0] == 'A') && m.currentScreen.Kind == onboardingScreenMulti && screenHasToggleAllOption(m.currentScreen) {
					return m, m.toggleAllSelections()
				}
				if filtered[0] == 'j' && m.currentScreen.Kind != onboardingScreenInput {
					if cmd := m.moveCursor(1); cmd != nil {
						return m, m.scrollContent(1)
					}
					return m, nil
				}
				if filtered[0] == 'k' && m.currentScreen.Kind != onboardingScreenInput {
					if cmd := m.moveCursor(-1); cmd != nil {
						return m, m.scrollContent(-1)
					}
					return m, nil
				}
				if filtered[0] >= '1' && filtered[0] <= '9' && m.currentScreen.Kind != onboardingScreenInput {
					index := int(filtered[0] - '1')
					if index < len(m.currentScreen.Options) {
						m.cursor = index
						m.ensureCursorVisible()
						if m.currentScreen.Kind == onboardingScreenChoice {
							return m.submitCurrentScreen()
						}
						if m.currentScreen.Kind == onboardingScreenMulti {
							return m, m.toggleCurrentSelection()
						}
					}
				}
			}
		}
	}
	if m.currentScreen.Kind == onboardingScreenInput {
		return m, updateSingleLineEditorWithAppKeys(&m.input, msg)
	}
	return m, nil
}

func (m *onboardingModel) submitCurrentScreen() (tea.Model, tea.Cmd) {
	step := m.currentStep()
	if step == nil {
		return m, nil
	}
	m.errorText = ""
	var err error
	switch m.currentScreen.Kind {
	case onboardingScreenChoice:
		if m.cursor < 0 || m.cursor >= len(m.currentScreen.Options) {
			return m, nil
		}
		err = step.ApplyChoice(&m.state, m.currentScreen.Options[m.cursor].ID)
	case onboardingScreenInput:
		err = step.ApplyInput(&m.state, strings.TrimSpace(singleLineEditorValue(m.input)))
	case onboardingScreenMulti:
		err = step.ApplyMultiSelect(&m.state, cloneSelection(m.selection))
	}
	if err != nil {
		m.errorText = err.Error()
		m.currentScreen.ErrorText = m.errorText
		return m, nil
	}
	m.currentScreen.ErrorText = ""
	switch m.state.pendingAction {
	case onboardingPendingActionRestart:
		m.state.pendingAction = onboardingPendingActionNone
		m.stepIndex = 0
		m.syncScreen(true)
		return m, nil
	case onboardingPendingActionWriteDefaults:
		m.state.pendingAction = onboardingPendingActionNone
		m.finalizing = true
		m.finalizingLabel = "Writing default configuration..."
		return m, m.finalizeCmd(true)
	case onboardingPendingActionWriteCustom:
		m.state.pendingAction = onboardingPendingActionNone
		m.finalizing = true
		m.finalizingLabel = "Saving first-time setup..."
		return m, m.finalizeCmd(false)
	default:
		m.stepIndex++
		m.syncScreen(true)
		return m, nil
	}
}

func (m *onboardingModel) finalizeCmd(writeDefaults bool) tea.Cmd {
	state := m.state
	globalRoot := m.globalRoot
	return func() tea.Msg {
		if writeDefaults {
			path, _, err := config.WriteDefaultSettingsFileWithTheme(state.settings.Theme)
			return onboardingFinalizeDoneMsg{result: onboardingResult{Completed: err == nil, CreatedDefaultConfig: err == nil, SettingsPath: path}, err: err}
		}
		rollback, err := executeOnboardingImports(globalRoot, state)
		if err != nil {
			return onboardingFinalizeDoneMsg{err: err}
		}
		path, err := config.WriteSettingsFileForOnboardingWithOptions(state.settings, config.OnboardingWriteOptions{
			PreservedDefaults: onboardingPreservedDefaults(state),
		})
		if err != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				err = errors.Join(err, rollbackErr)
			}
		}
		return onboardingFinalizeDoneMsg{result: onboardingResult{Completed: err == nil, SettingsPath: path}, err: err}
	}
}

func onboardingPreservedDefaults(state onboardingFlowState) map[string]bool {
	preserved := map[string]bool{}
	if state.reviewerCustomModel {
		preserved["reviewer.model"] = true
	}
	if state.reviewerCustomThinking {
		preserved["reviewer.thinking_level"] = true
	}
	if len(preserved) == 0 {
		return nil
	}
	return preserved
}

func (m *onboardingModel) currentStep() onboardingStepDefinition {
	steps := m.workflow.visibleSteps(&m.state)
	if len(steps) == 0 {
		return nil
	}
	if m.stepIndex >= len(steps) {
		m.stepIndex = len(steps) - 1
	}
	if m.stepIndex < 0 {
		m.stepIndex = 0
	}
	return steps[m.stepIndex]
}

func (m *onboardingModel) syncScreen(resetViewport bool) {
	step := m.currentStep()
	if step == nil {
		return
	}
	screen := step.Build(&m.state)
	previousID := m.currentScreen.ID
	previousKind := m.currentScreen.Kind
	inputDraft := singleLineEditorValue(m.input)
	m.currentScreen = screen
	if resetViewport || previousID != screen.ID {
		m.offset = 0
	}
	if screen.Kind == onboardingScreenInput {
		if !resetViewport && previousID == screen.ID && previousKind == onboardingScreenInput {
			setSingleLineEditorValue(&m.input, inputDraft)
		} else {
			setSingleLineEditorValue(&m.input, screen.InputValue)
		}
		m.inputPlaceholder = screen.Placeholder
		if screen.SensitiveInput {
			m.inputMask = '*'
		} else {
			m.inputMask = 0
		}
	}
	if screen.Kind == onboardingScreenMulti {
		m.selection = cloneSelection(screen.Selection)
		if m.selection == nil {
			m.selection = map[string]bool{}
		}
		m.refreshToggleAllOption()
	}
	m.cursor = 0
	if screen.Kind == onboardingScreenChoice {
		for index, option := range screen.Options {
			if option.ID == screen.DefaultOptionID {
				m.cursor = index
				break
			}
		}
	}
	m.ensureCursorVisible()
	if m.errorText != "" && screen.ErrorText == "" {
		m.currentScreen.ErrorText = m.errorText
	}
	if screen.Kind != onboardingScreenLoading {
		m.finalizingLabel = ""
	}
	if screen.Kind != onboardingScreenMulti {
		m.selection = nil
	}
	if screen.Kind == onboardingScreenMulti {
		m.cursor = 0
	}
	if screen.Kind == onboardingScreenInput {
		m.cursor = 0
	}
	if screen.Kind == onboardingScreenChoice && screen.DefaultOptionID == "" && len(screen.Options) > 0 {
		m.cursor = 0
	}
	m.applyActiveThemeStyles()
	if !resetViewport && previousID == screen.ID && screen.Kind == onboardingScreenMulti {
		m.ensureCursorVisible()
	}
	if !resetViewport && previousID == screen.ID && screen.Kind == onboardingScreenChoice {
		m.ensureCursorVisible()
	}
	if previousID != screen.ID {
		m.errorText = ""
	}
	m.ensureCursorVisible()
}

func (m *onboardingModel) moveCursor(delta int) tea.Cmd {
	limit := len(m.currentScreen.Options)
	if limit == 0 {
		return onboardingBellCmd()
	}
	previous := m.cursor
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= limit {
		m.cursor = limit - 1
	}
	m.ensureCursorVisible()
	if previous == m.cursor {
		return onboardingBellCmd()
	}
	m.applyActiveThemeStyles()
	return nil
}

func (m *onboardingModel) scrollContent(delta int) tea.Cmd {
	content := m.buildContent(max(1, m.width))
	maxOffset := len(content.lines) - m.contentHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	previous := m.offset
	m.offset += delta
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if previous == m.offset {
		return onboardingBellCmd()
	}
	return nil
}

func (m *onboardingModel) goBack() (tea.Model, tea.Cmd) {
	if m.stepIndex <= 0 {
		return m, onboardingBellCmd()
	}
	m.stepIndex--
	m.syncScreen(true)
	return m, nil
}

func (m *onboardingModel) toggleCurrentSelection() tea.Cmd {
	if m.cursor < 0 || m.cursor >= len(m.currentScreen.Options) {
		return onboardingBellCmd()
	}
	id := m.currentScreen.Options[m.cursor].ID
	if id == onboardingToggleAllOptionID {
		return m.toggleAllSelections()
	}
	m.selection[id] = !m.selection[id]
	m.refreshToggleAllOption()
	return nil
}

func (m *onboardingModel) toggleAllSelections() tea.Cmd {
	if !screenHasToggleAllOption(m.currentScreen) {
		return onboardingBellCmd()
	}
	setEnabled := !allSelectableOptionsEnabled(m.currentScreen.Options, m.selection)
	for _, option := range m.currentScreen.Options {
		if option.ID == onboardingToggleAllOptionID {
			continue
		}
		m.selection[option.ID] = setEnabled
	}
	m.refreshToggleAllOption()
	return nil
}

func (m *onboardingModel) refreshToggleAllOption() {
	allEnabled := allSelectableOptionsEnabled(m.currentScreen.Options, m.selection)
	m.selection[onboardingToggleAllOptionID] = allEnabled
	for index, option := range m.currentScreen.Options {
		if option.ID != onboardingToggleAllOptionID {
			continue
		}
		m.currentScreen.Options[index].Title = toggleAllOptionTitle(m.currentScreen.Options, m.selection)
		return
	}
}

func (m *onboardingModel) ensureCursorVisible() {
	content := m.buildContent(max(1, m.width))
	maxOffset := len(content.lines) - m.contentHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if content.cursorRow < 0 {
		return
	}
	contentHeight := m.contentHeight()
	if content.cursorRow < m.offset {
		m.offset = content.cursorRow
	}
	if content.cursorRow >= m.offset+contentHeight {
		m.offset = content.cursorRow - contentHeight + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
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
	headerLines := wrapANSIText(m.styles.title.Render(m.currentScreen.Title), max(1, m.width))
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
	var b strings.Builder
	b.WriteString(strings.Join(headerLines, "\n"))
	b.WriteString("\n\n")
	if len(visible) > 0 {
		b.WriteString(strings.Join(visible, "\n"))
	}
	if filler := m.contentHeight() - len(visible); filler > 0 {
		b.WriteString(strings.Repeat("\n", filler))
	}
	if len(footerLines) > 0 {
		b.WriteString("\n\n")
		b.WriteString(strings.Join(footerLines, "\n"))
	}
	m.updateTerminalCursor(headerLines, content, visible)
	return b.String()
}

func (m *onboardingModel) updateTerminalCursor(headerLines []string, content onboardingRenderedContent, visible []string) {
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
		CursorRow: len(headerLines) + 2 + visibleCursorRow,
		CursorCol: content.cursorCol,
		AnchorRow: max(0, m.height-1),
		AltScreen: true,
	})
}

func (m *onboardingModel) contentHeight() int {
	headerLines := wrapANSIText(m.styles.title.Render(m.currentScreen.Title), max(1, m.width))
	footerLines := m.renderFooterLines(max(1, m.width))
	height := m.height - len(headerLines) - len(footerLines) - 2
	if height < 1 {
		return 1
	}
	return height
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
		renderedInput := renderSingleLineEditor(width, 0, m.input, "> ", true, m.inputMask, m.inputPlaceholder)
		if renderedInput.Cursor.Visible {
			cursorRow = len(lines) + renderedInput.Cursor.Row
			cursorCol = renderedInput.Cursor.Col
		} else {
			cursorRow = len(lines)
			cursorCol = 0
		}
		if m.terminalCursor == nil {
			lines = append(lines, renderEditableInputSoftCursorLines(width, renderedInput, m.styles.inputText)...)
		} else {
			for _, line := range renderedInput.Lines {
				lines = append(lines, m.styles.inputText.Render(line))
			}
		}
	}
	if helper := strings.TrimSpace(m.currentScreen.Helper); helper != "" {
		appendBlank()
		appendWrapped(helper, m.styles.helper)
	}
	return onboardingRenderedContent{lines: lines, cursorRow: cursorRow, cursorCol: cursorCol}
}

func (m *onboardingModel) renderFooterLines(width int) []string {
	help := "↑/↓ pick or scroll | <-/-> back or forward | enter confirm | space toggle | esc cancel"
	if m.currentScreen.Kind == onboardingScreenInput {
		help = "↑/↓ scroll | <-/-> back or forward | enter confirm | esc cancel"
	} else if m.currentScreen.Kind == onboardingScreenMulti && screenHasToggleAllOption(m.currentScreen) {
		help = "↑/↓ pick or scroll | <-/-> back or forward | enter confirm | space toggle | a toggle all | esc cancel"
	}
	return wrapStyledParagraphs(help, width, m.styles.footer)
}

func screenHasToggleAllOption(screen onboardingScreen) bool {
	for _, option := range screen.Options {
		if option.ID == onboardingToggleAllOptionID {
			return true
		}
	}
	return false
}

func toggleAllOptionTitle(options []onboardingOption, selection map[string]bool) string {
	if allSelectableOptionsEnabled(options, selection) {
		return "Disable all"
	}
	return "Enable all"
}

func allSelectableOptionsEnabled(options []onboardingOption, selection map[string]bool) bool {
	hasSelectable := false
	for _, option := range options {
		if option.ID == onboardingToggleAllOptionID {
			continue
		}
		hasSelectable = true
		if !selection[option.ID] {
			return false
		}
	}
	return hasSelectable
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
		status: lipgloss.NewStyle().Foreground(palette.foreground).Background(palette.chatBg).Padding(0, 1).Width(innerWidth),
		input:  lipgloss.NewStyle().Foreground(palette.foreground).Background(palette.inputBg).Padding(0, 1).Width(innerWidth),
		help:   lipgloss.NewStyle().Foreground(palette.muted).Background(palette.chatBg).Padding(0, 1).Width(innerWidth).Faint(true),
	}
}

func (m *onboardingModel) renderThemePreview(width int) []string {
	theme := m.activeTheme()
	palette := uiPalette(theme)
	previewStyles := onboardingThemePreviewStyleSet(theme, width)
	heading := lipgloss.NewStyle().Foreground(palette.primary).Bold(true).Render("Preview")
	modelLabel := strings.TrimSpace(m.state.settings.Model)
	if modelLabel == "" {
		modelLabel = "gpt-5"
	}
	statusLine := lipgloss.NewStyle().Foreground(palette.primary).Bold(true).Render("builder") +
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
	appendRow("Theme", onboardingThemeSummary(m.state.settings.Theme), m.styles.valueNeutral)
	appendRow("Model", m.state.settings.Model, m.styles.valueNeutral)
	if meta, ok := llm.LookupModelMetadata(m.state.settings.Model); ok && meta.ContextWindowTokens > 0 {
		contextValue := formatTokenWindow(m.state.settings.ModelContextWindow)
		if m.state.settings.ModelContextWindow == meta.ContextWindowTokens {
			contextValue = "default (" + formatTokenWindow(meta.ContextWindowTokens) + ")"
		}
		appendRow("Context window", contextValue, m.styles.valueNeutral)
	}
	thinking := strings.TrimSpace(m.state.settings.ThinkingLevel)
	if thinking == "" {
		appendRow("Thinking", "off", m.styles.valueOff)
	} else {
		appendRow("Thinking", thinking, m.styles.valueNeutral)
	}
	verbosity := valueOrFallback(string(m.state.settings.ModelVerbosity), "off")
	verbosityStyle := m.styles.valueNeutral
	if verbosity == "off" {
		verbosityStyle = m.styles.valueOff
	}
	appendRow("Verbosity", verbosity, verbosityStyle)
	if m.state.settings.EnabledTools[toolspec.ToolAskQuestion] {
		appendRow("Questions", "on", m.styles.valueOn)
	} else {
		appendRow("Questions", "off", m.styles.valueOff)
	}
	reviewer := valueOrFallback(m.state.settings.Reviewer.Frequency, "off")
	reviewerStyle := m.styles.valueOn
	if reviewer == "off" {
		reviewerStyle = m.styles.valueOff
	}
	appendRow("Supervisor", reviewer, reviewerStyle)
	if reviewerEnabled(&m.state) {
		appendRow("Supervisor model", m.state.settings.Reviewer.Model, m.styles.valueNeutral)
		reviewerThinking := strings.TrimSpace(m.state.settings.Reviewer.ThinkingLevel)
		reviewerThinkingStyle := m.styles.valueNeutral
		if reviewerThinking == "" {
			reviewerThinking = "off"
			reviewerThinkingStyle = m.styles.valueOff
		}
		appendRow("Supervisor thinking", reviewerThinking, reviewerThinkingStyle)
	}
	compactionStyle := m.styles.valueNeutral
	if m.state.settings.CompactionMode == config.CompactionModeNone {
		compactionStyle = m.styles.valueOff
	}
	appendRow("Compaction", string(m.state.settings.CompactionMode), compactionStyle)
	if summary := skillImportSummary(&m.state); summary != "" {
		appendRow("Skills import", summary, m.styles.valueNeutral)
	}
	if enabled, disabled := selectedSkillCounts(&m.state); enabled > 0 || disabled > 0 {
		appendRow("Enabled skills", fmt.Sprintf("%d enabled, %d disabled", enabled, disabled), m.styles.valueNeutral)
	}
	if summary := commandImportSummary(&m.state); summary != "" {
		appendRow("Slash commands", summary, m.styles.valueNeutral)
	}
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

func onboardingBellCmd() tea.Cmd {
	return func() tea.Msg {
		fmt.Print("\a")
		return nil
	}
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
	content := strings.Join([]string{m.styles.title.Render(title), "", m.styles.spinner.Render(pendingToolSpinnerFrame(m.spinnerFrame) + " " + loadingText)}, "\n")
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
