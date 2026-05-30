package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"builder/cli/app/internal/projectbinding"
	"builder/cli/app/internal/projectpicker"
	"builder/cli/tui"
	"builder/shared/clientui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	ansi "github.com/charmbracelet/x/ansi"
)

const (
	projectBindingPickerHeaderMarkdown   = "**Bind Workspace**"
	projectBindingPickerHeaderFallback   = "Bind Workspace"
	projectBindingPickerNoticeText       = "Unknown directory opened, how do you want Builder to treat it?"
	projectBindingCreateLabel            = "Create a new project and attach this workspace"
	projectBindingExistingLabel          = "Attach to existing project:"
	serverProjectPickerHeaderMarkdown    = "**Open Server Project**"
	serverProjectPickerHeaderFallback    = "Open Server Project"
	serverProjectPickerNoticeText        = "Couldn't find the path the client requested - looks like the client & server might be in different locations. Open an existing registered project workspace, or run `builder project create` in the server location."
	serverProjectExistingLabel           = "Available server projects:"
	projectWorkspacePickerHeaderMarkdown = "**Select Workspace**"
	projectWorkspacePickerHeaderFallback = "Select Workspace"
	projectWorkspacePickerNoticeText     = "Choose the server workspace to open."
	projectNamePromptHeaderMarkdown      = "**Name New Project**"
	projectNamePromptHeaderFallback      = "Name New Project"
)

var runProjectBindingPickerFlow = runProjectBindingPicker
var runServerProjectPickerFlow = runServerProjectPicker
var runProjectWorkspacePickerFlow = runProjectWorkspacePicker
var runProjectNamePromptFlow = runProjectNamePrompt

type projectBindingPickerResult = projectbinding.ProjectPickerResult

type projectWorkspacePickerResult = projectbinding.WorkspacePickerResult

type projectPickerOptions struct {
	AllowCreate    bool
	HeaderMarkdown string
	HeaderFallback string
	NoticeText     string
	GroupLabel     string
}

type projectBindingVisibleRow = projectpicker.VisibleRow

type projectBindingPickerModel struct {
	projects []clientui.ProjectSummary
	options  projectPickerOptions
	cursor   int
	offset   int
	width    int
	height   int
	theme    string
	styles   sessionPickerStyles
	headerMD *glamour.TermRenderer
	result   projectBindingPickerResult
}

func newProjectBindingPickerModel(projects []clientui.ProjectSummary, theme string, options projectPickerOptions) *projectBindingPickerModel {
	items := append([]clientui.ProjectSummary(nil), projects...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return &projectBindingPickerModel{
		projects: items,
		options:  options,
		width:    defaultPickerWidth,
		height:   defaultPickerHeight,
		theme:    theme,
		styles:   newSessionPickerStyles(theme),
		headerMD: newStartupMarkdownRenderer(theme),
	}
}

func (m *projectBindingPickerModel) Init() tea.Cmd { return nil }

func (m *projectBindingPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case tea.MouseMsg:
		switch key.Button {
		case tea.MouseButtonWheelUp:
			m.moveCursor(-1)
		case tea.MouseButtonWheelDown:
			m.moveCursor(1)
		}
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
					m.result = projectBindingPickerResult{Canceled: true}
					return m, tea.Quit
				}
			}
		case tea.KeyEnter:
			if m.isCreateRow(m.cursor) {
				m.result = projectBindingPickerResult{CreateNew: true}
				return m, tea.Quit
			}
			picked, ok := m.projectForRow(m.cursor)
			if !ok {
				return m, nil
			}
			m.result = projectBindingPickerResult{Project: &picked}
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.result = projectBindingPickerResult{Canceled: true}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *projectBindingPickerModel) View() string {
	var out strings.Builder
	out.WriteString(m.renderHeader())
	out.WriteString("\n\n")
	out.WriteString(tui.ApplyThemeDefaultForeground(truncateQueuedMessageLine(m.options.NoticeText, m.width), m.theme))
	out.WriteString("\n\n")
	visible := m.visibleRowsFromOffset(m.offset)
	groupRendered := false
	for idx, row := range visible {
		if idx > 0 {
			out.WriteByte('\n')
		}
		if row.ShowGroup && !groupRendered {
			out.WriteString("\n")
			out.WriteString(lipgloss.NewStyle().Foreground(uiPalette(m.theme).foreground).Bold(true).Render(m.options.GroupLabel))
			out.WriteString("\n\n")
			groupRendered = true
		}
		out.WriteString(m.renderRow(row.Index, row.ShowPreview))
	}
	return out.String()
}

func (m *projectBindingPickerModel) itemCount() int {
	return projectpicker.ItemCount(len(m.projects), m.options.AllowCreate)
}

func (m *projectBindingPickerModel) visibleLineBudget() int {
	rows := m.height - 4
	if rows < 1 {
		return 1
	}
	return rows
}

func (m *projectBindingPickerModel) moveCursor(delta int) {
	m.cursor = projectpicker.MoveCursor(m.cursor, delta, m.itemCount())
	m.ensureCursorVisible()
}

func (m *projectBindingPickerModel) ensureCursorVisible() {
	m.offset = projectpicker.EnsureCursorVisible(m.cursor, m.offset, m.itemCount(), m.visibleRowsFromOffset)
}

func (m *projectBindingPickerModel) visibleRowsFromOffset(offset int) []projectBindingVisibleRow {
	return projectpicker.VisibleRows(projectpicker.VisibleRowsRequest{
		Offset:     offset,
		ItemCount:  m.itemCount(),
		LineBudget: m.visibleLineBudget(),
		HasPreview: m.hasPreview,
		ShowGroup:  m.shouldShowGroupHeader,
	})
}

func (m *projectBindingPickerModel) renderHeader() string {
	if m.headerMD != nil {
		rendered, err := m.headerMD.Render(m.options.HeaderMarkdown)
		if err == nil {
			return tui.ApplyThemeDefaultForeground(trimRenderedHeaderInset(rendered), m.theme)
		}
	}
	return m.styles.headerFallback.Render(m.options.HeaderFallback)
}

func (m *projectBindingPickerModel) renderRow(index int, showPreview bool) string {
	selected := index == m.cursor
	row := projectpicker.RowText{Title: projectBindingCreateLabel}
	if project, ok := m.projectForRow(index); ok {
		row = projectpicker.ProjectRowText(project.DisplayName, project.ProjectID, project.RootPath, humanTime(project.UpdatedAt), projectBindingHomeDir())
	}
	markerStyle := m.styles.marker
	rowStyle := m.styles.row
	marker := "◈"
	if selected {
		markerStyle = m.styles.markerSelected
		rowStyle = m.styles.rowSelected
	}
	left := markerStyle.Render(marker) + " " + rowStyle.Render(row.Title)
	if row.Timestamp == "" {
		if row.Preview == "" || !showPreview {
			return left
		}
		previewWidth := m.width - 2
		if previewWidth < 1 {
			previewWidth = 1
		}
		previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(row.Preview, previewWidth))
		return left + "\n" + previewLine
	}
	right := m.styles.timestamp.Render(row.Timestamp)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	titleLine := left + strings.Repeat(" ", gap) + right
	if row.Preview == "" || !showPreview {
		return titleLine
	}
	previewWidth := m.width - 2
	if previewWidth < 1 {
		previewWidth = 1
	}
	previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(row.Preview, previewWidth))
	return titleLine + "\n" + previewLine
}

func (m *projectBindingPickerModel) hasPreview(index int) bool {
	project, ok := m.projectForRow(index)
	if !ok {
		return false
	}
	return strings.TrimSpace(project.RootPath) != ""
}

func (m *projectBindingPickerModel) firstProjectRowIndex() int {
	return projectpicker.FirstProjectRowIndex(m.options.AllowCreate)
}

func (m *projectBindingPickerModel) isCreateRow(index int) bool {
	return m.options.AllowCreate && index == 0
}

func (m *projectBindingPickerModel) projectForRow(index int) (clientui.ProjectSummary, bool) {
	projectIndex, ok := projectpicker.ProjectIndexForRow(index, len(m.projects), m.options.AllowCreate)
	if !ok {
		return clientui.ProjectSummary{}, false
	}
	return m.projects[projectIndex], true
}

func (m *projectBindingPickerModel) shouldShowGroupHeader(index int, groupRendered bool) bool {
	if groupRendered || strings.TrimSpace(m.options.GroupLabel) == "" || len(m.projects) == 0 {
		return false
	}
	return index == m.firstProjectRowIndex()
}

func projectBindingHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return home
}

func runProjectBindingPicker(projects []clientui.ProjectSummary, theme string) (projectBindingPickerResult, error) {
	return runConfiguredProjectPicker(projects, theme, projectPickerOptions{
		AllowCreate:    true,
		HeaderMarkdown: projectBindingPickerHeaderMarkdown,
		HeaderFallback: projectBindingPickerHeaderFallback,
		NoticeText:     projectBindingPickerNoticeText,
		GroupLabel:     projectBindingExistingLabel,
	})
}

func runServerProjectPicker(projects []clientui.ProjectSummary, theme string) (projectBindingPickerResult, error) {
	return runConfiguredProjectPicker(projects, theme, projectPickerOptions{
		AllowCreate:    false,
		HeaderMarkdown: serverProjectPickerHeaderMarkdown,
		HeaderFallback: serverProjectPickerHeaderFallback,
		NoticeText:     serverProjectPickerNoticeText,
		GroupLabel:     serverProjectExistingLabel,
	})
}

func runConfiguredProjectPicker(projects []clientui.ProjectSummary, theme string, options projectPickerOptions) (projectBindingPickerResult, error) {
	model := newProjectBindingPickerModel(projects, theme, options)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	finalModel, err := program.Run()
	if err != nil {
		return projectBindingPickerResult{}, err
	}
	picked, ok := finalModel.(*projectBindingPickerModel)
	if !ok {
		return projectBindingPickerResult{}, fmt.Errorf("unexpected binding picker model type %T", finalModel)
	}
	return picked.result, nil
}

type projectWorkspacePickerModel struct {
	workspaces []clientui.ProjectWorkspaceSummary
	cursor     int
	offset     int
	width      int
	height     int
	theme      string
	styles     sessionPickerStyles
	headerMD   *glamour.TermRenderer
	result     projectWorkspacePickerResult
}

func newProjectWorkspacePickerModel(workspaces []clientui.ProjectWorkspaceSummary, theme string) *projectWorkspacePickerModel {
	items := append([]clientui.ProjectWorkspaceSummary(nil), workspaces...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsPrimary != items[j].IsPrimary {
			return items[i].IsPrimary
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return &projectWorkspacePickerModel{
		workspaces: items,
		width:      defaultPickerWidth,
		height:     defaultPickerHeight,
		theme:      theme,
		styles:     newSessionPickerStyles(theme),
		headerMD:   newStartupMarkdownRenderer(theme),
	}
}

func (m *projectWorkspacePickerModel) Init() tea.Cmd { return nil }

func (m *projectWorkspacePickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case tea.MouseMsg:
		switch key.Button {
		case tea.MouseButtonWheelUp:
			m.moveCursor(-1)
		case tea.MouseButtonWheelDown:
			m.moveCursor(1)
		}
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
					m.result = projectWorkspacePickerResult{Canceled: true}
					return m, tea.Quit
				}
			}
		case tea.KeyEnter:
			if len(m.workspaces) == 0 {
				return m, nil
			}
			picked := m.workspaces[m.cursor]
			m.result = projectWorkspacePickerResult{Workspace: &picked}
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.result = projectWorkspacePickerResult{Canceled: true}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *projectWorkspacePickerModel) View() string {
	var out strings.Builder
	out.WriteString(m.renderHeader())
	out.WriteString("\n\n")
	out.WriteString(tui.ApplyThemeDefaultForeground(truncateQueuedMessageLine(projectWorkspacePickerNoticeText, m.width), m.theme))
	out.WriteString("\n\n")
	for idx, row := range m.visibleRowsFromOffset(m.offset) {
		if idx > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(m.renderRow(row.Index, row.ShowPreview))
	}
	return out.String()
}

func (m *projectWorkspacePickerModel) itemCount() int { return len(m.workspaces) }

func (m *projectWorkspacePickerModel) visibleLineBudget() int {
	rows := m.height - 4
	if rows < 1 {
		return 1
	}
	return rows
}

func (m *projectWorkspacePickerModel) moveCursor(delta int) {
	m.cursor = projectpicker.MoveCursor(m.cursor, delta, m.itemCount())
	m.ensureCursorVisible()
}

func (m *projectWorkspacePickerModel) ensureCursorVisible() {
	m.offset = projectpicker.EnsureCursorVisible(m.cursor, m.offset, m.itemCount(), m.visibleRowsFromOffset)
}

func (m *projectWorkspacePickerModel) visibleRowsFromOffset(offset int) []projectBindingVisibleRow {
	return projectpicker.VisibleRows(projectpicker.VisibleRowsRequest{
		Offset:     offset,
		ItemCount:  m.itemCount(),
		LineBudget: m.visibleLineBudget(),
		HasPreview: m.hasPreview,
	})
}

func (m *projectWorkspacePickerModel) renderHeader() string {
	if m.headerMD != nil {
		rendered, err := m.headerMD.Render(projectWorkspacePickerHeaderMarkdown)
		if err == nil {
			return tui.ApplyThemeDefaultForeground(trimRenderedHeaderInset(rendered), m.theme)
		}
	}
	return m.styles.headerFallback.Render(projectWorkspacePickerHeaderFallback)
}

func (m *projectWorkspacePickerModel) renderRow(index int, showPreview bool) string {
	selected := index == m.cursor
	workspace := m.workspaces[index]
	row := projectpicker.WorkspaceRowText(workspace.DisplayName, workspace.RootPath, humanTime(workspace.UpdatedAt), projectBindingHomeDir())
	markerStyle := m.styles.marker
	rowStyle := m.styles.row
	marker := "◈"
	if selected {
		markerStyle = m.styles.markerSelected
		rowStyle = m.styles.rowSelected
	}
	left := markerStyle.Render(marker) + " " + rowStyle.Render(row.Title)
	if row.Timestamp == "" {
		if row.Preview == "" || !showPreview {
			return left
		}
		previewWidth := m.width - 2
		if previewWidth < 1 {
			previewWidth = 1
		}
		previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(row.Preview, previewWidth))
		return left + "\n" + previewLine
	}
	right := m.styles.timestamp.Render(row.Timestamp)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	titleLine := left + strings.Repeat(" ", gap) + right
	if row.Preview == "" || !showPreview {
		return titleLine
	}
	previewWidth := m.width - 2
	if previewWidth < 1 {
		previewWidth = 1
	}
	previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(row.Preview, previewWidth))
	return titleLine + "\n" + previewLine
}

func (m *projectWorkspacePickerModel) hasPreview(index int) bool {
	if index < 0 || index >= len(m.workspaces) {
		return false
	}
	return strings.TrimSpace(m.workspaces[index].RootPath) != ""
}

func runProjectWorkspacePicker(workspaces []clientui.ProjectWorkspaceSummary, theme string) (projectWorkspacePickerResult, error) {
	model := newProjectWorkspacePickerModel(workspaces, theme)
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	finalModel, err := program.Run()
	if err != nil {
		return projectWorkspacePickerResult{}, err
	}
	picked, ok := finalModel.(*projectWorkspacePickerModel)
	if !ok {
		return projectWorkspacePickerResult{}, fmt.Errorf("unexpected workspace picker model type %T", finalModel)
	}
	return picked.result, nil
}

func ensureInteractiveProjectBinding(ctx context.Context, server projectbinding.Server[interactiveSessionServer]) (interactiveSessionServer, error) {
	return projectbinding.EnsureInteractive[interactiveSessionServer](ctx, projectbinding.Request[interactiveSessionServer]{
		Server:            server,
		PickLocalProject:  runProjectBindingPickerFlow,
		PickServerProject: runServerProjectPickerFlow,
		PickWorkspace:     runProjectWorkspacePickerFlow,
		PromptProjectName: runProjectNamePromptFlow,
	})
}

func ensureInteractiveServerBrowsingBinding(ctx context.Context, server projectbinding.Server[interactiveSessionServer], projects []clientui.ProjectSummary) (interactiveSessionServer, error) {
	return projectbinding.EnsureServerBrowsing[interactiveSessionServer](ctx, projectbinding.Request[interactiveSessionServer]{
		Server:            server,
		PickServerProject: runServerProjectPickerFlow,
		PickWorkspace:     runProjectWorkspacePickerFlow,
	}, projects)
}

func headerInsetFromRenderedHeader(rendered string) string {
	for _, line := range strings.Split(ansi.Strip(rendered), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		trimmed := strings.TrimLeft(line, " ")
		return line[:len(line)-len(trimmed)]
	}
	return ""
}

func trimRenderedHeaderInset(rendered string) string {
	trimmed := strings.TrimRight(rendered, "\n")
	inset := headerInsetFromRenderedHeader(trimmed)
	if inset == "" {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, inset) {
			lines[i] = strings.TrimPrefix(line, inset)
		}
	}
	return strings.Join(lines, "\n")
}
