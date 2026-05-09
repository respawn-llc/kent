package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type authSuccessScreenData struct {
	Theme string
	Title string
}

type authSuccessScreenModel struct {
	data   authSuccessScreenData
	width  int
	height int
	styles authSuccessScreenStyles
	ready  bool
}

type authSuccessScreenStyles struct {
	hint lipgloss.Style
}

func newAuthSuccessScreenModel(data authSuccessScreenData) *authSuccessScreenModel {
	return &authSuccessScreenModel{
		data:   data,
		width:  defaultPickerWidth,
		height: defaultPickerHeight,
		styles: newAuthSuccessScreenStyles(data.Theme),
	}
}

func newAuthSuccessScreenStyles(theme string) authSuccessScreenStyles {
	palette := uiPalette(theme)
	return authSuccessScreenStyles{
		hint: lipgloss.NewStyle().Foreground(palette.foreground),
	}
}

func (m *authSuccessScreenModel) Init() tea.Cmd {
	return nil
}

func (m *authSuccessScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.WindowSizeMsg:
		if key.Width > 0 {
			m.width = key.Width
		}
		if key.Height > 0 {
			m.height = key.Height
		}
		m.ready = true
		return m, nil
	case tea.KeyMsg:
		return m, tea.Quit
	}
	return m, nil
}

func (m *authSuccessScreenModel) View() string {
	body := strings.Join([]string{
		renderStartupPlainTitle(authSuccessScreenTitle(m.data.Title), m.data.Theme),
		"",
		m.styles.hint.Render("Press any key to continue"),
	}, "\n")
	width := m.width
	height := m.height
	if width < 1 {
		width = defaultPickerWidth
	}
	if height < 1 {
		height = defaultPickerHeight
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, body)
}

func authSuccessScreenTitle(title string) string {
	if title := strings.TrimSpace(title); title != "" {
		return title
	}
	return "Auth success"
}

var runAuthSuccessScreen = func(data authSuccessScreenData) error {
	model := newAuthSuccessScreenModel(data)
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err := program.Run()
	return err
}
