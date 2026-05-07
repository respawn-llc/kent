package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"builder/cli/tui"
	tuiinput "builder/cli/tui/input"
	"builder/shared/clientui"
	"builder/shared/serverapi"
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

type projectBindingPickerResult struct {
	CreateNew bool
	Project   *clientui.ProjectSummary
	Canceled  bool
}

type projectWorkspacePickerResult struct {
	Workspace *clientui.ProjectWorkspaceSummary
	Canceled  bool
}

type projectPickerOptions struct {
	AllowCreate    bool
	HeaderMarkdown string
	HeaderFallback string
	NoticeText     string
	GroupLabel     string
}

type projectBindingVisibleRow struct {
	index       int
	showPreview bool
	showGroup   bool
}

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
		if row.showGroup && !groupRendered {
			out.WriteString("\n")
			out.WriteString(lipgloss.NewStyle().Foreground(uiPalette(m.theme).foreground).Bold(true).Render(m.options.GroupLabel))
			out.WriteString("\n\n")
			groupRendered = true
		}
		out.WriteString(m.renderRow(row.index, row.showPreview))
	}
	return out.String()
}

func (m *projectBindingPickerModel) itemCount() int {
	count := len(m.projects)
	if m.options.AllowCreate {
		count++
	}
	return count
}

func (m *projectBindingPickerModel) visibleLineBudget() int {
	rows := m.height - 4
	if rows < 1 {
		return 1
	}
	return rows
}

func (m *projectBindingPickerModel) moveCursor(delta int) {
	if m.itemCount() == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= m.itemCount() {
		m.cursor = m.itemCount() - 1
	}
	m.ensureCursorVisible()
}

func (m *projectBindingPickerModel) ensureCursorVisible() {
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
	maxOffset := m.itemCount() - 1
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

func (m *projectBindingPickerModel) visibleRowsFromOffset(offset int) []projectBindingVisibleRow {
	budget := m.visibleLineBudget()
	visible := make([]projectBindingVisibleRow, 0, m.itemCount())
	groupRendered := false
	for i := offset; i < m.itemCount(); i++ {
		separator := 0
		if len(visible) > 0 {
			separator = 1
		}
		groupLines := 0
		showGroup := false
		if m.shouldShowGroupHeader(i, groupRendered) {
			groupLines = 3
			showGroup = true
		}
		available := budget - separator - groupLines
		if available < 1 {
			break
		}
		showPreview := m.hasPreview(i) && available >= 2
		rowLines := 1
		if showPreview {
			rowLines = 2
		}
		if rowLines > available {
			if len(visible) == 0 {
				return []projectBindingVisibleRow{{index: i, showPreview: false, showGroup: showGroup}}
			}
			break
		}
		visible = append(visible, projectBindingVisibleRow{index: i, showPreview: showPreview, showGroup: showGroup})
		budget -= separator + groupLines + rowLines
		if showGroup {
			groupRendered = true
		}
		if budget == 0 {
			break
		}
	}
	return visible
}

func (m *projectBindingPickerModel) rowVisibleFromOffset(offset, index int) bool {
	for _, row := range m.visibleRowsFromOffset(offset) {
		if row.index == index {
			return true
		}
	}
	return false
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
	title := projectBindingCreateLabel
	preview := ""
	var timestamp string
	if project, ok := m.projectForRow(index); ok {
		title = strings.TrimSpace(project.DisplayName)
		if title == "" {
			title = strings.TrimSpace(project.ProjectID)
		}
		preview = projectBindingPreviewPath(project.RootPath)
		timestamp = humanTime(project.UpdatedAt)
	}
	markerStyle := m.styles.marker
	rowStyle := m.styles.row
	marker := "◈"
	if selected {
		markerStyle = m.styles.markerSelected
		rowStyle = m.styles.rowSelected
	}
	left := markerStyle.Render(marker) + " " + rowStyle.Render(title)
	if timestamp == "" {
		if preview == "" || !showPreview {
			return left
		}
		previewWidth := m.width - 2
		if previewWidth < 1 {
			previewWidth = 1
		}
		previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(preview, previewWidth))
		return left + "\n" + previewLine
	}
	right := m.styles.timestamp.Render(timestamp)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	titleLine := left + strings.Repeat(" ", gap) + right
	if preview == "" || !showPreview {
		return titleLine
	}
	previewWidth := m.width - 2
	if previewWidth < 1 {
		previewWidth = 1
	}
	previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(preview, previewWidth))
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
	if m.options.AllowCreate {
		return 1
	}
	return 0
}

func (m *projectBindingPickerModel) isCreateRow(index int) bool {
	return m.options.AllowCreate && index == 0
}

func (m *projectBindingPickerModel) projectForRow(index int) (clientui.ProjectSummary, bool) {
	if index < m.firstProjectRowIndex() {
		return clientui.ProjectSummary{}, false
	}
	projectIndex := index - m.firstProjectRowIndex()
	if projectIndex < 0 || projectIndex >= len(m.projects) {
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

func projectBindingPreviewPath(rootPath string) string {
	trimmedRoot := strings.TrimSpace(rootPath)
	if trimmedRoot == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return trimmedRoot
	}
	rel, err := filepath.Rel(home, trimmedRoot)
	if err != nil {
		return trimmedRoot
	}
	if rel == "." {
		return "~"
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join("~", rel)
	}
	return trimmedRoot
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
		out.WriteString(m.renderRow(row.index, row.showPreview))
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
	if m.itemCount() == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= m.itemCount() {
		m.cursor = m.itemCount() - 1
	}
	m.ensureCursorVisible()
}

func (m *projectWorkspacePickerModel) ensureCursorVisible() {
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
	maxOffset := m.itemCount() - 1
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

func (m *projectWorkspacePickerModel) visibleRowsFromOffset(offset int) []projectBindingVisibleRow {
	budget := m.visibleLineBudget()
	visible := make([]projectBindingVisibleRow, 0, m.itemCount())
	for i := offset; i < m.itemCount(); i++ {
		separator := 0
		if len(visible) > 0 {
			separator = 1
		}
		available := budget - separator
		if available < 1 {
			break
		}
		showPreview := m.hasPreview(i) && available >= 2
		rowLines := 1
		if showPreview {
			rowLines = 2
		}
		if rowLines > available {
			if len(visible) == 0 {
				return []projectBindingVisibleRow{{index: i, showPreview: false}}
			}
			break
		}
		visible = append(visible, projectBindingVisibleRow{index: i, showPreview: showPreview})
		budget -= separator + rowLines
		if budget == 0 {
			break
		}
	}
	return visible
}

func (m *projectWorkspacePickerModel) rowVisibleFromOffset(offset, index int) bool {
	for _, row := range m.visibleRowsFromOffset(offset) {
		if row.index == index {
			return true
		}
	}
	return false
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
	title := strings.TrimSpace(workspace.DisplayName)
	if title == "" {
		title = strings.TrimSpace(filepath.Base(workspace.RootPath))
	}
	preview := projectBindingPreviewPath(workspace.RootPath)
	timestamp := humanTime(workspace.UpdatedAt)
	markerStyle := m.styles.marker
	rowStyle := m.styles.row
	marker := "◈"
	if selected {
		markerStyle = m.styles.markerSelected
		rowStyle = m.styles.rowSelected
	}
	left := markerStyle.Render(marker) + " " + rowStyle.Render(title)
	if timestamp == "" {
		if preview == "" || !showPreview {
			return left
		}
		previewWidth := m.width - 2
		if previewWidth < 1 {
			previewWidth = 1
		}
		previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(preview, previewWidth))
		return left + "\n" + previewLine
	}
	right := m.styles.timestamp.Render(timestamp)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	titleLine := left + strings.Repeat(" ", gap) + right
	if preview == "" || !showPreview {
		return titleLine
	}
	previewWidth := m.width - 2
	if previewWidth < 1 {
		previewWidth = 1
	}
	previewLine := "  " + m.styles.preview.Render(truncateQueuedMessageLine(preview, previewWidth))
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

type projectNamePromptModel struct {
	width          int
	height         int
	theme          string
	headerMD       *glamour.TermRenderer
	input          tuiinput.Editor
	terminalCursor *uiTerminalCursorState
	error          string
	result         string
	canceled       bool
}

func newProjectNamePromptModel(defaultName string, theme string) *projectNamePromptModel {
	input := newSingleLineEditor(defaultName)
	return &projectNamePromptModel{
		width:    defaultPickerWidth,
		height:   defaultPickerHeight,
		theme:    theme,
		headerMD: newStartupMarkdownRenderer(theme),
		input:    input,
	}
}

func (m *projectNamePromptModel) Init() tea.Cmd { return nil }

func (m *projectNamePromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		if typed.Width > 0 {
			m.width = typed.Width
		}
		if typed.Height > 0 {
			m.height = typed.Height
		}
		return m, nil
	case tea.KeyMsg:
		switch typed.Type {
		case tea.KeyEnter:
			value := strings.TrimSpace(singleLineEditorValue(m.input))
			if value == "" {
				m.error = "project name is required"
				return m, nil
			}
			m.result = value
			return m, tea.Quit
		case tea.KeyEsc, tea.KeyCtrlC:
			m.canceled = true
			return m, tea.Quit
		}
	}
	return m, updateSingleLineEditorWithAppKeys(&m.input, msg)
}

func (m *projectNamePromptModel) View() string {
	var out strings.Builder
	out.WriteString(m.renderHeader())
	out.WriteString("\n\n")
	out.WriteString(tui.ApplyThemeDefaultForeground("Enter a project name. Press Enter to create the project.", m.theme))
	out.WriteString("\n\n")
	out.WriteString(renderStartupEditorField(m.width, m.height, m.theme, m.input, "› ", m.terminalCursor == nil, 0, ""))
	if trimmed := strings.TrimSpace(m.error); trimmed != "" {
		out.WriteString("\n\n")
		out.WriteString(lipgloss.NewStyle().Foreground(statusRedColor()).Bold(true).Render(truncateQueuedMessageLine(trimmed, m.width)))
	}
	m.updateTerminalCursor()
	return out.String()
}

func (m *projectNamePromptModel) updateTerminalCursor() {
	if m.terminalCursor == nil {
		return
	}
	headerLines := strings.Split(m.renderHeader(), "\n")
	cursor := renderSingleLineEditor(max(1, m.width), inputContentLineLimit(m.height), m.input, "› ", true, 0, "").Cursor
	if !cursor.Visible {
		m.terminalCursor.Clear()
		return
	}
	m.terminalCursor.Set(uiTerminalCursorPlacement{
		Visible:   true,
		CursorRow: len(headerLines) + 4 + cursor.Row,
		CursorCol: cursor.Col,
		AnchorRow: max(0, m.height-1),
		AltScreen: true,
	})
}

func (m *projectNamePromptModel) renderHeader() string {
	if m.headerMD != nil {
		rendered, err := m.headerMD.Render(projectNamePromptHeaderMarkdown)
		if err == nil {
			return tui.ApplyThemeDefaultForeground(trimRenderedHeaderInset(rendered), m.theme)
		}
	}
	return lipgloss.NewStyle().Foreground(uiPalette(m.theme).primary).Bold(true).Render(projectNamePromptHeaderFallback)
}

func runProjectNamePrompt(defaultName string, theme string) (string, error) {
	model := newProjectNamePromptModel(defaultName, theme)
	terminalCursor := newUITerminalCursorState()
	model.terminalCursor = terminalCursor
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(newUITerminalCursorWriter(os.Stdout, terminalCursor)))
	finalModel, err := program.Run()
	if err != nil {
		return "", err
	}
	finalized, ok := finalModel.(*projectNamePromptModel)
	if !ok {
		return "", fmt.Errorf("unexpected project name prompt model type %T", finalModel)
	}
	if finalized.canceled {
		return "", errors.New("startup canceled by user")
	}
	return strings.TrimSpace(finalized.result), nil
}

func ensureInteractiveProjectBinding(ctx context.Context, server embeddedServer) (embeddedServer, error) {
	if server == nil || server.ProjectViewClient() == nil {
		return nil, errors.New("project view client is required")
	}
	workspaceRoot := strings.TrimSpace(server.Config().WorkspaceRoot)
	if workspaceRoot == "" {
		return nil, errors.New("workspace root is required")
	}
	plan, err := server.ProjectViewClient().PlanWorkspaceBinding(ctx, serverapi.ProjectBindingPlanRequest{Path: workspaceRoot, Mode: serverapi.ProjectBindingPlanModeInteractive})
	if err != nil {
		return nil, err
	}
	if canonicalRoot := strings.TrimSpace(plan.CanonicalRoot); canonicalRoot != "" {
		workspaceRoot = canonicalRoot
	}
	switch plan.Kind {
	case serverapi.ProjectBindingPlanKindBound:
		if plan.Binding == nil {
			return nil, errors.New("resolved project binding is required")
		}
		projectID := strings.TrimSpace(plan.Binding.ProjectID)
		if projectID == "" {
			return nil, errors.New("resolved project id is required")
		}
		bound, bindErr := server.BindProjectWorkspace(ctx, projectID, strings.TrimSpace(plan.Binding.WorkspaceID))
		if bindErr != nil {
			return nil, formatProjectBindingStartupError(workspaceRoot, projectID, bindErr)
		}
		return bound, nil
	case serverapi.ProjectBindingPlanKindServerWorkspaceSelection:
		return ensureInteractiveServerBrowsingBinding(ctx, server, plan.Projects)
	case serverapi.ProjectBindingPlanKindLocalUnbound:
		return ensureInteractiveLocalPathBinding(ctx, server, workspaceRoot, plan.Projects)
	default:
		return nil, fmt.Errorf("unsupported interactive project binding plan %q", plan.Kind)
	}
}

func ensureInteractiveLocalPathBinding(ctx context.Context, server embeddedServer, workspaceRoot string, projects []clientui.ProjectSummary) (embeddedServer, error) {
	cfg := server.Config()
	picked, err := runProjectBindingPickerFlow(projects, cfg.Settings.Theme)
	if err != nil {
		return nil, err
	}
	if picked.Canceled {
		return nil, errors.New("startup canceled by user")
	}
	if picked.CreateNew {
		projectName, err := runProjectNamePromptFlow(filepath.Base(filepath.Clean(workspaceRoot)), cfg.Settings.Theme)
		if err != nil {
			return nil, err
		}
		created, err := server.ProjectViewClient().CreateProject(ctx, serverapi.ProjectCreateRequest{DisplayName: projectName, WorkspaceRoot: workspaceRoot})
		if err != nil {
			return nil, formatProjectBindingMutationError(workspaceRoot, "", err)
		}
		bound, bindErr := server.BindProjectWorkspace(ctx, created.Binding.ProjectID, created.Binding.WorkspaceID)
		if bindErr != nil {
			return nil, formatProjectBindingStartupError(workspaceRoot, created.Binding.ProjectID, bindErr)
		}
		return bound, nil
	}
	if picked.Project == nil {
		return nil, errors.New("no project selected")
	}
	attached, err := server.ProjectViewClient().AttachWorkspaceToProject(ctx, serverapi.ProjectAttachWorkspaceRequest{ProjectID: picked.Project.ProjectID, WorkspaceRoot: workspaceRoot})
	if err != nil {
		return nil, formatProjectBindingMutationError(workspaceRoot, picked.Project.ProjectID, err)
	}
	bound, bindErr := server.BindProjectWorkspace(ctx, attached.Binding.ProjectID, attached.Binding.WorkspaceID)
	if bindErr != nil {
		return nil, formatProjectBindingStartupError(workspaceRoot, attached.Binding.ProjectID, bindErr)
	}
	return bound, nil
}

func ensureInteractiveServerBrowsingBinding(ctx context.Context, server embeddedServer, projects []clientui.ProjectSummary) (embeddedServer, error) {
	if len(projects) == 0 {
		return nil, errors.New("server has no registered projects. Create one with `builder project create --path <server-path> --name <project-name>` or attach an existing workspace with `builder attach --project <project-id> <server-path>`")
	}
	cfg := server.Config()
	picked, err := runServerProjectPickerFlow(projects, cfg.Settings.Theme)
	if err != nil {
		return nil, err
	}
	if picked.Canceled {
		return nil, errors.New("startup canceled by user")
	}
	if picked.Project == nil {
		return nil, errors.New("no project selected")
	}
	workspace, err := selectProjectWorkspaceForStartup(ctx, server, picked.Project.ProjectID)
	if err != nil {
		return nil, err
	}
	bound, bindErr := server.BindProjectWorkspace(ctx, picked.Project.ProjectID, workspace.WorkspaceID)
	if bindErr != nil {
		return nil, formatProjectBindingStartupError(workspace.RootPath, picked.Project.ProjectID, bindErr)
	}
	return bound, nil
}

func selectProjectWorkspaceForStartup(ctx context.Context, server embeddedServer, projectID string) (clientui.ProjectWorkspaceSummary, error) {
	overview, err := server.ProjectViewClient().GetProjectOverview(ctx, serverapi.ProjectGetOverviewRequest{ProjectID: projectID})
	if err != nil {
		return clientui.ProjectWorkspaceSummary{}, err
	}
	if len(overview.Overview.Workspaces) == 0 {
		return clientui.ProjectWorkspaceSummary{}, fmt.Errorf("project %q has no attached workspaces", strings.TrimSpace(projectID))
	}
	if len(overview.Overview.Workspaces) == 1 {
		return overview.Overview.Workspaces[0], nil
	}
	picked, err := runProjectWorkspacePickerFlow(overview.Overview.Workspaces, server.Config().Settings.Theme)
	if err != nil {
		return clientui.ProjectWorkspaceSummary{}, err
	}
	if picked.Canceled {
		return clientui.ProjectWorkspaceSummary{}, errors.New("startup canceled by user")
	}
	if picked.Workspace == nil {
		return clientui.ProjectWorkspaceSummary{}, errors.New("no workspace selected")
	}
	return *picked.Workspace, nil
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

func renderStartupEditorField(width int, height int, theme string, input tuiinput.Editor, prefix string, renderCursor bool, mask rune, placeholder string) string {
	contentWidth := width
	if contentWidth < 1 {
		contentWidth = 1
	}
	lineStyle := lipgloss.NewStyle().Foreground(uiPalette(theme).foreground)
	borderStyle := lipgloss.NewStyle().Foreground(uiPalette(theme).primary)
	return strings.Join(renderSingleLineEditorFramedSoftCursorLines(contentWidth, inputContentLineLimit(height), input, prefix, renderCursor, lineStyle, borderStyle, mask, placeholder), "\n")
}
