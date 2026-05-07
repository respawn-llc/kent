package app

import (
	"errors"
	"fmt"
	"strings"

	"builder/cli/tui"
	"builder/server/auth"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
)

const (
	authPickerHeaderMarkdown         = "**Sign in to Builder**"
	authConflictPickerHeaderMarkdown = "**Choose Auth Source**"
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
	Text string
	Kind startupPickerNoticeKind
}

type startupPickerResult struct {
	ChoiceID string
	Canceled bool
}

type startupPickerStyles struct {
	headerFallback lipgloss.Style
	notice         lipgloss.Style
	noticeError    lipgloss.Style
	row            lipgloss.Style
	rowSelected    lipgloss.Style
	marker         lipgloss.Style
	markerSelected lipgloss.Style
}

type startupPickerModel struct {
	banner         string
	headerMarkdown string
	headerFallback string
	items          []startupPickerOption
	cursor         int
	offset         int
	width          int
	height         int
	theme          string
	styles         startupPickerStyles
	headerMD       *glamour.TermRenderer
	notice         startupPickerNotice
	result         startupPickerResult
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
	m.headerMD = newStartupMarkdownRenderer(theme)
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
		m.ensureCursorVisible()
		return m, nil
	case tea.KeyMsg:
		switch key.Type {
		case tea.KeyUp:
			m.moveCursor(-1)
		case tea.KeyDown:
			m.moveCursor(1)
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
	var out strings.Builder
	if banner := m.renderBanner(); banner != "" {
		out.WriteString(banner)
		out.WriteString("\n\n")
	}
	out.WriteString(m.renderHeader())
	if notice := m.renderNotice(); strings.TrimSpace(notice) != "" {
		out.WriteString("\n\n")
		out.WriteString(notice)
	}
	out.WriteString("\n\n")
	visible := m.visibleRowsFromOffset(m.offset)
	for idx, row := range visible {
		if idx > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(m.renderRow(row.index))
	}
	return out.String()
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
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	for m.offset < m.cursor && !m.rowVisibleFromOffset(m.offset, m.cursor) {
		m.offset++
	}
	if m.offset < 0 {
		m.offset = 0
	}
	for m.offset > 0 && m.rowVisibleFromOffset(m.offset-1, m.cursor) {
		m.offset--
	}
	maxOffset := len(m.items) - 1
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

func (m *startupPickerModel) renderHeader() string {
	if m.headerMD != nil {
		rendered, err := m.headerMD.Render(m.headerMarkdown)
		if err == nil {
			return tui.ApplyThemeDefaultForeground(strings.TrimRight(rendered, "\n"), m.theme)
		}
	}
	return m.styles.headerFallback.Render(m.headerFallback)
}

func (m *startupPickerModel) renderBanner() string {
	return renderStartupBanner(m.banner)
}

func (m *startupPickerModel) renderNotice() string {
	text := strings.TrimSpace(m.notice.Text)
	if text == "" {
		return ""
	}
	style := m.styles.notice
	if m.notice.Kind == startupPickerNoticeError {
		style = m.styles.noticeError
	}
	return style.Render(m.headerInset() + truncateQueuedMessageLine(text, m.contentWidth()))
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
	return marker + " " + titleStyle.Render(truncateQueuedMessageLine(item.Title, contentWidth))
}

func (m *startupPickerModel) contentWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m *startupPickerModel) staticLineCount() int {
	lines := 2
	if bannerLines := startupBannerLineCount(m.banner); bannerLines > 0 {
		lines += bannerLines + 2
	}
	if strings.TrimSpace(m.notice.Text) != "" {
		lines += 2
	}
	return lines
}

func (m *startupPickerModel) visibleLineBudget() int {
	rows := m.height - m.staticLineCount()
	if rows < 1 {
		return 1
	}
	return rows
}

type startupPickerVisibleRow struct {
	index int
}

func (m *startupPickerModel) visibleRowsFromOffset(offset int) []startupPickerVisibleRow {
	budget := m.visibleLineBudget()
	if budget <= 0 {
		return nil
	}
	visible := make([]startupPickerVisibleRow, 0, len(m.items))
	for i := offset; i < len(m.items); i++ {
		separator := 0
		if len(visible) > 0 {
			separator = 1
		}
		available := budget - separator
		if available < 1 {
			break
		}
		rowLines := 1
		if rowLines > available {
			if len(visible) == 0 {
				return []startupPickerVisibleRow{{index: i}}
			}
			break
		}
		visible = append(visible, startupPickerVisibleRow{index: i})
		budget -= separator + rowLines
		if budget == 0 {
			break
		}
	}
	return visible
}

func (m *startupPickerModel) rowVisibleFromOffset(offset, index int) bool {
	for _, row := range m.visibleRowsFromOffset(offset) {
		if row.index == index {
			return true
		}
	}
	return false
}

func newStartupPickerStyles(theme string) startupPickerStyles {
	palette := uiPalette(theme)
	return startupPickerStyles{
		headerFallback: lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		notice:         lipgloss.NewStyle().Foreground(palette.foreground),
		noticeError:    lipgloss.NewStyle().Foreground(statusRedColor()).Bold(true),
		row:            lipgloss.NewStyle().Foreground(palette.foreground),
		rowSelected:    lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
		marker:         lipgloss.NewStyle().Foreground(palette.muted),
		markerSelected: lipgloss.NewStyle().Foreground(palette.primary).Bold(true),
	}
}

func runStartupPicker(model *startupPickerModel) (startupPickerResult, error) {
	program := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := program.Run()
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
	authMethodChoiceSkip         authMethodChoice = "skip"
	authMethodChoiceEnvAPIKey    authMethodChoice = "env_api_key"
	authMethodChoiceBrowserAuto  authMethodChoice = "oauth_browser"
	authMethodChoiceBrowserPaste authMethodChoice = "oauth_browser_paste"
	authMethodChoiceDevice       authMethodChoice = "oauth_device"
)

type authMethodPickerResult struct {
	Choice   authMethodChoice
	Canceled bool
}

func authMethodOptions(includeEnvAPIKey bool, allowSkip bool) []startupPickerOption {
	items := make([]startupPickerOption, 0, 5)
	if includeEnvAPIKey {
		items = append(items, startupPickerOption{
			ID:    string(authMethodChoiceEnvAPIKey),
			Title: "Use existing OPENAI_API_KEY from now on",
		})
	}
	items = append(items,
		startupPickerOption{
			ID:    string(authMethodChoiceBrowserAuto),
			Title: "Open browser and finish automatically",
		},
		startupPickerOption{
			ID:    string(authMethodChoiceBrowserPaste),
			Title: "Open browser and paste the callback manually",
		},
		startupPickerOption{
			ID:    string(authMethodChoiceDevice),
			Title: "Use a device code in any browser",
		},
	)
	if allowSkip {
		items = append(items, startupPickerOption{
			ID:    string(authMethodChoiceSkip),
			Title: "Continue without Builder auth",
		})
	}
	return items
}

func newAuthMethodPickerModel(theme string, notice startupPickerNotice, includeEnvAPIKey bool, allowSkip bool) *startupPickerModel {
	model := newStartupPickerModel(authPickerHeaderMarkdown, "Sign in to Builder", theme, notice, authMethodOptions(includeEnvAPIKey, allowSkip))
	model.banner = builderStartupBannerANSI
	return model
}

func authMethodPickerNoticeForRequest(req authInteraction) startupPickerNotice {
	if req.FlowErr != nil {
		if errors.Is(req.FlowErr, auth.ErrDeviceCodeUnsupported) {
			return startupPickerNotice{Text: "Device-code sign-in is not enabled for this issuer. Choose another method.", Kind: startupPickerNoticeError}
		}
		return startupPickerNotice{Text: "Sign-in failed: " + req.FlowErr.Error(), Kind: startupPickerNoticeError}
	}
	if req.StartupErr != nil && !errors.Is(req.StartupErr, auth.ErrAuthNotConfigured) {
		return startupPickerNotice{Text: "Saved sign-in needs attention: " + req.StartupErr.Error(), Kind: startupPickerNoticeError}
	}
	if strings.TrimSpace(req.Gate.Reason) != "" && req.Gate.Reason != auth.ErrAuthNotConfigured.Error() {
		return startupPickerNotice{Text: "Saved sign-in needs attention: " + req.Gate.Reason, Kind: startupPickerNoticeError}
	}
	if req.HasEnvAPIKey {
		return startupPickerNotice{Text: "Choose how Builder should sign in. OPENAI_API_KEY is available for this launch.", Kind: startupPickerNoticeNeutral}
	}
	return startupPickerNotice{Text: "Choose how to authenticate.", Kind: startupPickerNoticeNeutral}
}

func authMethodDisplayTitle(choice authMethodChoice) string {
	for _, item := range authMethodOptions(true, true) {
		if item.ID == string(choice) {
			return item.Title
		}
	}
	return string(choice)
}

func runAuthMethodPicker(req authInteraction) (authMethodPickerResult, error) {
	model := newAuthMethodPickerModel(req.Theme, authMethodPickerNoticeForRequest(req), req.HasEnvAPIKey, !req.AuthRequired)
	picked, err := runStartupPickerFlow(model)
	if err != nil {
		return authMethodPickerResult{}, err
	}
	return authMethodPickerResult{Choice: authMethodChoice(picked.ChoiceID), Canceled: picked.Canceled}, nil
}

type authConflictChoice string

const (
	authConflictChoiceEnvAPIKey authConflictChoice = "env_api_key"
	authConflictChoiceSavedAuth authConflictChoice = "saved_auth"
)

type authConflictPickerResult struct {
	Choice   authConflictChoice
	Canceled bool
}

func authConflictOptions() []startupPickerOption {
	return []startupPickerOption{
		{
			ID:    string(authConflictChoiceEnvAPIKey),
			Title: "Use existing OPENAI_API_KEY from now on",
		},
		{
			ID:    string(authConflictChoiceSavedAuth),
			Title: "Keep using saved subscription sign-in",
		},
	}
}

func runAuthConflictPicker(req authInteraction) (authConflictPickerResult, error) {
	model := newStartupPickerModel(
		authConflictPickerHeaderMarkdown,
		"Choose auth source",
		req.Theme,
		startupPickerNotice{Text: "Builder found both saved subscription auth and OPENAI_API_KEY. Choose which auth source should win from now on.", Kind: startupPickerNoticeNeutral},
		authConflictOptions(),
	)
	picked, err := runStartupPickerFlow(model)
	if err != nil {
		return authConflictPickerResult{}, err
	}
	return authConflictPickerResult{Choice: authConflictChoice(picked.ChoiceID), Canceled: picked.Canceled}, nil
}
