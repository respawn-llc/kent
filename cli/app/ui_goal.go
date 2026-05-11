package app

import (
	"strings"

	"builder/cli/app/commands"
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

const noGoalHint = "No goal to manage yet. First, start a goal with /goal <objective>"

func (c uiInputController) handleGoalCommand(mode commands.GoalMode, objective string) (tea.Model, tea.Cmd) {
	m := c.model
	switch mode {
	case commands.GoalModeShow, "":
		return m, c.startGoalFlowCmd()
	case commands.GoalModeSet:
		current, err := m.showRuntimeGoal()
		if err != nil {
			detailErr := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(detailErr, c.showErrorStatus(detailErr))
		}
		if goalIsActive(current) {
			m.openGoalConfirmOverlay("replace", current, objective, nil)
			if overlayCmd := m.pushGoalOverlayIfNeeded(); overlayCmd != nil {
				return m, overlayCmd
			}
			return m, nil
		}
		_, err = m.setRuntimeGoal(objective)
		if err != nil {
			detailErr := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(detailErr, c.showErrorStatus(detailErr))
		}
		return m, c.appendGoalFeedback("Goal set")
	case commands.GoalModePause:
		_, err := m.pauseRuntimeGoal()
		if err != nil {
			detailErr := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(detailErr, c.showErrorStatus(detailErr))
		}
		return m, c.appendGoalFeedback("Goal paused")
	case commands.GoalModeResume:
		_, err := m.resumeRuntimeGoal()
		if err != nil {
			detailErr := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(detailErr, c.showErrorStatus(detailErr))
		}
		return m, c.appendGoalFeedback("Goal resumed")
	case commands.GoalModeClear:
		current, err := m.showRuntimeGoal()
		if err != nil {
			detailErr := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(detailErr, c.showErrorStatus(detailErr))
		}
		if goalRequiresClearConfirmation(current) {
			m.openGoalConfirmOverlay("clear", current, "", nil)
			if overlayCmd := m.pushGoalOverlayIfNeeded(); overlayCmd != nil {
				return m, overlayCmd
			}
			return m, nil
		}
		_, err = m.clearRuntimeGoal()
		if err != nil {
			detailErr := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(detailErr, c.showErrorStatus(detailErr))
		}
		return m, c.appendGoalFeedback("Goal cleared")
	default:
		errText := "Usage: /goal [show|pause|resume|clear|<objective>]"
		return m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}
}

func goalIsActive(goal *clientui.RuntimeGoal) bool {
	return goal != nil && goal.Status == clientui.RuntimeGoalStatusActive
}

func goalRequiresClearConfirmation(goal *clientui.RuntimeGoal) bool {
	return goalIsActive(goal) && !goal.Suspended
}

func (c uiInputController) appendGoalFeedback(text string) tea.Cmd {
	return nil
}

func (c uiInputController) startGoalFlowCmd() tea.Cmd {
	m := c.model
	goal, err := m.showRuntimeGoal()
	m.openGoalOverlay(goal, err)
	if overlayCmd := m.pushGoalOverlayIfNeeded(); overlayCmd != nil {
		return overlayCmd
	}
	return nil
}

func (c uiInputController) stopGoalFlowCmd() tea.Cmd {
	m := c.model
	overlayCmd := m.popGoalOverlayIfNeeded()
	m.closeGoalOverlay()
	return overlayCmd
}

func (c uiInputController) handleGoalOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	if strings.TrimSpace(m.goal.confirmMode) != "" {
		return c.handleGoalConfirmKey(msg)
	}
	switch strings.ToLower(msg.String()) {
	case "ctrl+c":
		if m.isBusy() {
			c.interruptBusyRuntime()
			return m, nil
		}
		m.exitAction = UIActionExit
		if overlayCmd := m.popGoalOverlayIfNeeded(); overlayCmd != nil {
			m.closeGoalOverlay()
			return m, tea.Sequence(overlayCmd, tea.Quit)
		}
		return m, tea.Quit
	case "esc", "q":
		return m, c.stopGoalFlowCmd()
	case "up":
		m.moveGoalScroll(-1)
	case "down":
		m.moveGoalScroll(1)
	case "pgup":
		m.moveGoalScrollPage(-1)
	case "pgdown":
		m.moveGoalScrollPage(1)
	case "home":
		m.goal.scroll = 0
	case "end":
		m.goal.scroll = 1 << 30
	}
	return m, nil
}

func (c uiInputController) handleGoalConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	switch strings.ToLower(msg.String()) {
	case "ctrl+c":
		if m.isBusy() {
			c.interruptBusyRuntime()
			return m, nil
		}
		m.exitAction = UIActionExit
		if overlayCmd := m.popGoalOverlayIfNeeded(); overlayCmd != nil {
			m.closeGoalOverlay()
			return m, tea.Sequence(overlayCmd, tea.Quit)
		}
		return m, tea.Quit
	case "esc", "q", "n":
		return m, c.stopGoalFlowCmd()
	case "tab", "left", "right", "up", "down":
		m.toggleGoalConfirmSelection()
		return m, nil
	case "enter", "y":
		if strings.ToLower(msg.String()) == "y" {
			m.goal.confirmSelection = goalConfirmSelectionConfirm
		}
		if m.goal.confirmSelection != goalConfirmSelectionConfirm {
			return m, c.stopGoalFlowCmd()
		}
		mode := strings.TrimSpace(m.goal.confirmMode)
		objective := m.goal.pendingObjective
		switch mode {
		case "replace":
			_, err := m.setRuntimeGoal(objective)
			if err != nil {
				m.goal.error = err.Error()
				return m, nil
			}
			overlayCmd := c.stopGoalFlowCmd()
			return m, sequenceCmds(overlayCmd, c.appendGoalFeedback("Goal set"))
		case "clear":
			if _, err := m.clearRuntimeGoal(); err != nil {
				m.goal.error = err.Error()
				return m, nil
			}
			overlayCmd := c.stopGoalFlowCmd()
			return m, sequenceCmds(overlayCmd, c.appendGoalFeedback("Goal cleared"))
		}
	}
	return m, nil
}

func (m *uiModel) openGoalOverlay(goal *clientui.RuntimeGoal, err error) {
	m.goal.open = true
	m.goal.scroll = 0
	m.goal.goal = cloneRuntimeGoal(goal)
	m.goal.error = ""
	if err != nil {
		m.goal.error = err.Error()
	}
	m.setInputMode(uiInputModeGoal)
}

func (m *uiModel) openGoalConfirmOverlay(mode string, goal *clientui.RuntimeGoal, pendingObjective string, err error) {
	m.openGoalOverlay(goal, err)
	m.goal.confirmMode = strings.TrimSpace(mode)
	m.goal.confirmSelection = goalConfirmSelectionCancel
	m.goal.pendingObjective = strings.TrimSpace(pendingObjective)
}

func (m *uiModel) closeGoalOverlay() {
	m.goal = uiGoalOverlayState{}
	m.restorePrimaryInputMode()
}

func (m *uiModel) pushGoalOverlayIfNeeded() tea.Cmd {
	return m.activateSurface(uiSurfaceGoal)
}

func (m *uiModel) popGoalOverlayIfNeeded() tea.Cmd {
	return m.restoreTranscriptSurface()
}

func (m *uiModel) moveGoalScroll(delta int) {
	m.goal.scroll += delta
	if m.goal.scroll < 0 {
		m.goal.scroll = 0
	}
}

func (m *uiModel) moveGoalScrollPage(deltaPages int) {
	height := m.termHeight
	if height < 1 {
		height = 1
	}
	m.moveGoalScroll(deltaPages * max(1, height-4))
}

const (
	goalConfirmSelectionCancel = iota
	goalConfirmSelectionConfirm
)

type goalOverlayLineBuilder struct {
	model *uiModel
	width int
	lines []string
}

func newGoalOverlayLineBuilder(model *uiModel, width int) *goalOverlayLineBuilder {
	return &goalOverlayLineBuilder{model: model, width: width, lines: make([]string, 0, 24)}
}

func (b *goalOverlayLineBuilder) appendWrapped(text string, lineStyle lipgloss.Style) {
	wrapped := wrapLine(strings.TrimRight(text, " \t"), b.width)
	if len(wrapped) == 0 {
		b.lines = append(b.lines, padRight("", b.width))
		return
	}
	for _, line := range wrapped {
		b.lines = append(b.lines, padANSIRight(lineStyle.Render(line), b.width))
	}
}

func (b *goalOverlayLineBuilder) appendMarkdown(text string) {
	renderer := b.goalMarkdownRenderer()
	if renderer == nil {
		b.appendWrapped(text, lipgloss.Style{})
		return
	}
	rendered, err := renderer.Render(text)
	if err != nil {
		b.appendWrapped(text, lipgloss.Style{})
		return
	}
	for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
		b.lines = append(b.lines, padANSIRight(line, b.width))
	}
}

func (b *goalOverlayLineBuilder) goalMarkdownRenderer() *glamour.TermRenderer {
	if b.model == nil {
		return nil
	}
	theme := strings.TrimSpace(b.model.theme)
	width := b.width
	if width < 1 {
		width = 1
	}
	if b.model.goal.markdownRenderer != nil && b.model.goal.markdownTheme == theme && b.model.goal.markdownWidth == width {
		return b.model.goal.markdownRenderer
	}
	renderer := newStartupMarkdownRendererWithWordWrap(theme, width)
	b.model.goal.markdownTheme = theme
	b.model.goal.markdownWidth = width
	b.model.goal.markdownRenderer = renderer
	return renderer
}

func (b *goalOverlayLineBuilder) appendGap() {
	if len(b.lines) > 0 {
		b.lines = append(b.lines, padRight("", b.width))
	}
}

func (m *uiModel) toggleGoalConfirmSelection() {
	if m.goal.confirmSelection == goalConfirmSelectionConfirm {
		m.goal.confirmSelection = goalConfirmSelectionCancel
		return
	}
	m.goal.confirmSelection = goalConfirmSelectionConfirm
}

func (l uiViewLayout) renderGoalOverlay(width, height int, _ uiStyles) []string {
	if width < 1 || height < 1 {
		return []string{padRight("", width)}
	}
	content := l.goalOverlayContentLines(width)
	maxScroll := max(0, len(content)-height)
	if l.model.goal.scroll > maxScroll {
		l.model.goal.scroll = maxScroll
	}
	if l.model.goal.scroll < 0 {
		l.model.goal.scroll = 0
	}
	start := l.model.goal.scroll
	end := min(len(content), start+height)
	visible := append([]string(nil), content[start:end]...)
	for len(visible) < height {
		visible = append(visible, padRight("", width))
	}
	return visible
}

func (l uiViewLayout) goalOverlayContentLines(width int) []string {
	m := l.model
	palette := uiPalette(m.theme)
	titleStyle := lipgloss.NewStyle().Foreground(palette.primary).Bold(true)
	boldStyle := lipgloss.NewStyle().Bold(true)
	subtleStyle := lipgloss.NewStyle().Foreground(palette.muted).Faint(true)
	warningStyle := lipgloss.NewStyle().Foreground(statusAmberColor()).Bold(true)
	builder := newGoalOverlayLineBuilder(m, width)

	builder.appendWrapped("Goal", titleStyle)
	if strings.TrimSpace(m.goal.confirmMode) != "" {
		return l.goalConfirmContentLines(width, titleStyle, boldStyle, subtleStyle, warningStyle)
	}
	if strings.TrimSpace(m.goal.error) != "" {
		builder.appendGap()
		builder.appendWrapped("Could not load goal: "+m.goal.error, warningStyle)
		return builder.lines
	}
	if m.goal.goal == nil {
		builder.appendGap()
		builder.appendWrapped(noGoalHint, subtleStyle)
		return builder.lines
	}
	goal := m.goal.goal
	builder.appendGap()
	builder.appendWrapped("Status: "+strings.TrimSpace(string(goal.Status)), boldStyle)
	if strings.TrimSpace(goal.ID) != "" {
		builder.appendWrapped("ID: "+strings.TrimSpace(goal.ID), subtleStyle)
	}
	builder.appendGap()
	builder.appendWrapped("Objective", titleStyle)
	builder.appendMarkdown(goal.Objective)
	builder.appendGap()
	builder.appendWrapped("Esc/q closes. /goal pause, /goal resume, /goal clear manage lifecycle.", subtleStyle)
	return builder.lines
}

func (l uiViewLayout) goalConfirmContentLines(width int, titleStyle, boldStyle, subtleStyle, warningStyle lipgloss.Style) []string {
	m := l.model
	builder := newGoalOverlayLineBuilder(m, width)

	builder.appendWrapped("Goal", titleStyle)
	builder.appendGap()
	if strings.TrimSpace(m.goal.error) != "" {
		builder.appendWrapped("Could not update goal: "+m.goal.error, warningStyle)
		builder.appendGap()
	}
	switch strings.TrimSpace(m.goal.confirmMode) {
	case "replace":
		builder.appendWrapped("Replace active goal?", boldStyle)
		if m.goal.goal != nil {
			builder.appendWrapped("Current: "+m.goal.goal.Objective, lipgloss.Style{})
		}
		builder.appendWrapped("New: "+m.goal.pendingObjective, lipgloss.Style{})
	case "clear":
		builder.appendWrapped("Clear active goal?", boldStyle)
		if m.goal.goal != nil {
			builder.appendWrapped(m.goal.goal.Objective, lipgloss.Style{})
		}
	default:
		builder.appendWrapped("Confirm goal action?", boldStyle)
	}
	builder.appendGap()
	cancel := "Cancel"
	confirm := "Confirm"
	if m.goal.confirmSelection == goalConfirmSelectionCancel {
		cancel = "> " + cancel
		confirm = "  " + confirm
	} else {
		cancel = "  " + cancel
		confirm = "> " + confirm
	}
	builder.appendWrapped(cancel+"    "+confirm, subtleStyle)
	builder.appendWrapped("Tab/arrows toggle. Enter selects. Esc cancels.", subtleStyle)
	return builder.lines
}
