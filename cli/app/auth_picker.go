package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"core/cli/app/internal/authui"
	"core/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
)

var runStartupPickerFlow = runStartupPicker

type startupPickerOption struct {
	ID    string
	Title string
}

type startupPickerNoticeKind string

const (
	startupPickerNoticeNeutral startupPickerNoticeKind = "neutral"
	startupPickerNoticeError   startupPickerNoticeKind = "error"
)

type startupPickerNotice struct {
	Text       string
	Kind       startupPickerNoticeKind
	Diagnostic error
}

type startupPickerResult struct {
	ChoiceID string
	Canceled bool
}

type startupPickerStyles struct {
	headerFallback lipgloss.Style
	row            lipgloss.Style
	rowSelected    lipgloss.Style
	marker         lipgloss.Style
	markerSelected lipgloss.Style
}

type startupPickerModel struct {
	headerMarkdown string
	headerFallback string
	items          []startupPickerOption
	cursor         int
	scrollOffset   int
	width          int
	height         int
	theme          string
	styles         startupPickerStyles
	headerMD       *startupMarkdownRenderer
	notice         startupPickerNotice
	result         startupPickerResult
}

type startupPickerRowRange struct {
	start        int
	endExclusive int
}

func newStartupPickerModel(headerMarkdown, headerFallback, theme string, notice startupPickerNotice, items []startupPickerOption) *startupPickerModel {
	m := &startupPickerModel{
		headerMarkdown: headerMarkdown,
		headerFallback: headerFallback,
		items:          append([]startupPickerOption(nil), items...),
		width:          defaultPickerWidth,
		height:         defaultPickerHeight,
		theme:          theme,
		styles:         newStartupPickerStyles(theme),
		notice:         notice,
	}
	m.headerMD = newStartupMarkdownRendererWithWordWrap(theme)
	return m
}

func (m *startupPickerModel) Init() tea.Cmd {
	return nil
}

func (m *startupPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.WindowSizeMsg:
		if key.Width > 0 {
			m.width = key.Width
		}
		if key.Height > 0 {
			m.height = key.Height
		}
		m.clampScrollOffset()
		return m, nil
	case tea.KeyMsg:
		switch key.Type {
		case tea.KeyUp:
			m.moveCursor(-1)
		case tea.KeyDown:
			m.moveCursor(1)
		case tea.KeyPgUp:
			m.scrollBy(-max(1, m.height-1))
		case tea.KeyPgDown:
			m.scrollBy(max(1, m.height-1))
		case tea.KeyHome:
			m.scrollOffset = 0
		case tea.KeyEnd:
			m.scrollOffset = m.maxScrollOffset()
		case tea.KeyRunes:
			filtered, _ := stripMouseSGRRunes(key.Runes)
			if len(filtered) == 1 {
				switch filtered[0] {
				case 'k':
					m.moveCursor(-1)
				case 'j':
					m.moveCursor(1)
				case 'q':
					m.result = startupPickerResult{Canceled: true}
					return m, tea.Quit
				}
			}
		case tea.KeyEnter:
			if len(m.items) == 0 || m.cursor < 0 || m.cursor >= len(m.items) {
				return m, nil
			}
			if !m.cursorVisible() {
				m.ensureCursorVisible()
				return m, nil
			}
			m.result = startupPickerResult{ChoiceID: m.items[m.cursor].ID}
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.result = startupPickerResult{Canceled: true}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *startupPickerModel) View() string {
	lines, _ := m.contentLines()
	start := min(m.scrollOffset, max(0, len(lines)-max(1, m.height)))
	end := min(len(lines), start+max(1, m.height))
	return strings.Join(lines[start:end], "\n")
}

func (m *startupPickerModel) moveCursor(delta int) {
	if len(m.items) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
	m.ensureCursorVisible()
}

func (m *startupPickerModel) ensureCursorVisible() {
	lines, rows := m.contentLines()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return
	}
	row := rows[m.cursor]
	height := max(1, m.height)
	if row.start < m.scrollOffset {
		m.scrollOffset = row.start
	}
	if row.endExclusive > m.scrollOffset+height {
		m.scrollOffset = min(row.start, row.endExclusive-height)
	}
	m.scrollOffset = min(max(0, m.scrollOffset), max(0, len(lines)-height))
}

func (m *startupPickerModel) renderHeader() string {
	if m.headerMD != nil {
		rendered := m.headerMD.Render(m.headerMarkdown, m.contentWidth())
		return tui.ApplyThemeStyleIntents(strings.TrimRight(rendered, "\n"), m.theme, tui.ThemeForeground)
	}
	return m.styles.headerFallback.Render(m.headerFallback)
}

func (m *startupPickerModel) renderNotice() string {
	text := strings.TrimSpace(m.notice.Text)
	if text == "" {
		return ""
	}
	inset := m.headerInset()
	rendered := renderStartupPickerNotice(m.notice, m.contentWidth()-lipgloss.Width(inset))
	if rendered == "" {
		return ""
	}
	lines := strings.Split(rendered, "\n")
	for index := range lines {
		lines[index] = inset + lines[index]
	}
	return strings.Join(lines, "\n")
}

func (m *startupPickerModel) headerInset() string {
	for _, line := range strings.Split(ansi.Strip(m.renderHeader()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		return line[:len(line)-len(trimmed)]
	}
	return ""
}

func (m *startupPickerModel) renderRow(index int) string {
	if index < 0 || index >= len(m.items) {
		return ""
	}
	item := m.items[index]
	selected := index == m.cursor
	marker := m.styles.marker.Render("◈")
	titleStyle := m.styles.row
	if selected {
		marker = m.styles.markerSelected.Render("◈")
		titleStyle = m.styles.rowSelected
	}
	contentWidth := m.contentWidth() - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	titleLines := wrapLine(item.Title, contentWidth)
	if len(titleLines) == 0 {
		titleLines = []string{""}
	}
	lines := make([]string, 0, len(titleLines))
	lines = append(lines, marker+" "+titleStyle.Render(titleLines[0]))
	for _, line := range titleLines[1:] {
		lines = append(lines, "  "+titleStyle.Render(line))
	}
	return strings.Join(lines, "\n")
}

func (m *startupPickerModel) contentWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m *startupPickerModel) contentLines() ([]string, []startupPickerRowRange) {
	lines := strings.Split(m.renderHeader(), "\n")
	if notice := m.renderNotice(); notice != "" {
		lines = append(lines, "")
		lines = append(lines, strings.Split(notice, "\n")...)
	}
	lines = append(lines, "")
	rows := make([]startupPickerRowRange, len(m.items))
	for index := range m.items {
		if index > 0 {
			lines = append(lines, "")
		}
		rows[index].start = len(lines)
		lines = append(lines, strings.Split(m.renderRow(index), "\n")...)
		rows[index].endExclusive = len(lines)
	}
	return lines, rows
}

func (m *startupPickerModel) cursorVisible() bool {
	_, rows := m.contentLines()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return false
	}
	row := rows[m.cursor]
	height := max(1, m.height)
	if row.endExclusive-row.start > height {
		return row.start >= m.scrollOffset && row.start < m.scrollOffset+height
	}
	return row.start >= m.scrollOffset && row.endExclusive <= m.scrollOffset+height
}

func (m *startupPickerModel) scrollBy(delta int) {
	m.scrollOffset = min(max(0, m.scrollOffset+delta), m.maxScrollOffset())
}

func (m *startupPickerModel) maxScrollOffset() int {
	lines, _ := m.contentLines()
	return max(0, len(lines)-max(1, m.height))
}

func (m *startupPickerModel) clampScrollOffset() {
	m.scrollOffset = min(max(0, m.scrollOffset), m.maxScrollOffset())
}

func newStartupPickerStyles(theme string) startupPickerStyles {
	palette := uiPalette(theme)
	return startupPickerStyles{
		headerFallback: lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		row:            lipgloss.NewStyle().Foreground(palette.foreground),
		rowSelected:    lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		marker:         lipgloss.NewStyle().Foreground(palette.muted),
		markerSelected: lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
	}
}

func runStartupPicker(model *startupPickerModel) (startupPickerResult, error) {
	finalModel, err := runStartupAlternateScreen(context.Background(), model, os.Stdout)
	if err != nil {
		return startupPickerResult{}, err
	}
	picked, ok := finalModel.(*startupPickerModel)
	if !ok {
		return startupPickerResult{}, fmt.Errorf("unexpected startup picker model type %T", finalModel)
	}
	return picked.result, nil
}

type authMethodChoice string

const (
	authMethodChoiceBrowserAuto authMethodChoice = "oauth_browser"
	authMethodChoiceDevice      authMethodChoice = "oauth_device"
)

type authMethodPickerResult struct {
	Choice   authMethodChoice
	Canceled bool
}

func newAuthMethodPickerModel(req authInteraction) (*onboardingModel, *authMethodPickerResult, error) {
	theme, err := seedThemeSelection(req.Theme)
	if err != nil {
		return nil, nil, err
	}
	options := []onboardingOption{}
	for _, choice := range []struct {
		method authMethodChoice
		title  string
	}{
		{authMethodChoiceBrowserAuto, "Browser"},
		{authMethodChoiceDevice, "Device code"},
	} {
		if supportsBootstrapMode(req.Modes, choice.method) {
			options = append(options, onboardingOption{ID: string(choice.method), Title: choice.title})
		}
	}
	result := &authMethodPickerResult{}
	model := newOnboardingFormModel(onboardingFlowState{selections: onboardingSelections{theme: theme}}, onboardingWorkflow{
		steps: []onboardingStepDefinition{{id: connectionStepAuthMethod, build: func(*onboardingFlowState) onboardingScreen {
			return onboardingScreen{ID: connectionStepAuthMethod, Kind: onboardingScreenChoice, Title: "Choose a sign-in method",
				Options: options, ErrorText: authMethodPickerNoticeForRequest(req).Text}
		}, apply: func(state *onboardingFlowState, value string) error {
			result.Choice = authMethodChoice(value)
			state.pendingAction = onboardingPendingActionConnectionComplete
			return nil
		}}},
	})
	return model, result, nil
}

func authMethodPickerNoticeForRequest(req authInteraction) startupPickerNotice {
	notice := authui.AuthMethodPickerNotice(authui.AuthMethodPickerNoticeRequest{
		FlowErr: req.FlowErr,
	})
	kind := startupPickerNoticeNeutral
	if notice.Kind == authui.AuthNoticeError {
		kind = startupPickerNoticeError
	}
	return startupPickerNotice{Text: notice.Text, Kind: kind}
}

func runAuthMethodPicker(req authInteraction) (authMethodPickerResult, error) {
	model, result, err := newAuthMethodPickerModel(req)
	if err != nil {
		return authMethodPickerResult{}, err
	}
	_, err = runOnboardingProgram(context.Background(), model)
	if err != nil {
		return authMethodPickerResult{}, err
	}
	result.Canceled = model.canceled
	return *result, nil
}
