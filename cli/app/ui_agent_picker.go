package app

import (
	"errors"
	"strings"

	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	"core/shared/runtimeids"
	"core/shared/textutil"
	sharedtheme "core/shared/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type uiAgentPicker struct {
	selectedRole *string
	request      uiAgentPickerRequest
	pending      bool
	err          error
}

type uiAgentPickerRequest uint8

const (
	uiAgentPickerLoad uiAgentPickerRequest = iota
	uiAgentPickerApply
)

type agentPickerAppliedMsg struct {
	picker    *uiAgentPicker
	sessionID runtimeids.SessionID
	result    chatSettingsDoneMsg
}

func (m *uiModel) openAgentPicker() tea.Cmd {
	if m.newSessionTarget() == nil && m.sessionSettings != nil &&
		m.sessionSettings.AgentEditability == settingspb.Editability_EDITABILITY_WORKFLOW_LOCK {
		return m.showTransientStatusNotice(uiStatusNotice{Text: "The Workflow controls this Agent.", Kind: uiStatusNoticeError})
	}
	m.agentPicker = &uiAgentPicker{}
	m.focusCurrentAgent()
	m.setInputMode(uiInputModeAgent)
	return tea.Batch(m.activateSurface(uiSurfaceAgent), m.reconcileSpinnerTicking(false))
}

func (m *uiModel) agentChoices() []*settingspb.AgentChoice {
	if target := m.newSessionTarget(); target != nil {
		if ready, ok := target.settingsLoad.(uiNewSettingsReady); ok {
			choices := make([]*settingspb.AgentChoice, 0, len(ready.choices))
			for _, choice := range ready.choices {
				choices = append(choices, choice.Agent)
			}
			return choices
		}
		return nil
	}
	return m.sessionSettings.GetAgentChoices()
}

func (m *uiModel) agentSettings() *settingspb.Settings {
	if m.newSessionTarget() != nil {
		return m.projectedNewSessionSettings()
	}
	return m.sessionSettings
}

func (m *uiModel) focusCurrentAgent() {
	role := m.agentSettings().GetSelectedAgent().GetRole()
	for _, choice := range m.agentChoices() {
		if choice.Role == role {
			m.agentPicker.selectedRole = textutil.Value(role)
			return
		}
	}
}

func (m *uiModel) agentSelectionIndex() int {
	for index, choice := range m.agentChoices() {
		if m.agentPicker.selectedRole != nil && choice.Role == *m.agentPicker.selectedRole {
			return index
		}
	}
	return 0
}

func (m *uiModel) agentPickerLoading() bool {
	if m.agentPicker == nil {
		return false
	}
	if target := m.newSessionTarget(); target != nil {
		_, loading := target.settingsLoad.(uiNewSettingsLoading)
		return loading || target.settingsLoad == nil
	}
	return m.agentPicker.pending || (m.sessionSettings == nil && m.sessionObservationErr == nil)
}

func (m *uiModel) agentPickerError() error {
	if target := m.newSessionTarget(); target != nil {
		if failed, ok := target.settingsLoad.(uiNewSettingsFailed); ok {
			return failed.err
		}
		return nil
	}
	if m.agentPicker.request == uiAgentPickerLoad {
		return m.sessionObservationErr
	}
	return m.agentPicker.err
}

func (m *uiModel) retryAgentPicker() tea.Cmd {
	if m.newSessionTarget() != nil {
		return m.loadNewSessionSettingsCmd(nil)
	}
	if m.agentPicker.request == uiAgentPickerLoad {
		return tea.Batch(m.observeAdoptedSessionCmd(), m.reconcileSpinnerTicking(false))
	}
	return m.applyAgentSelection()
}

func (m *uiModel) reconcileNewChatAgentPicker(msg newSessionSettingsLoadedMsg) tea.Cmd {
	target := m.newSessionTarget()
	picker := m.agentPicker
	if target == nil || target.creation != msg.target || picker == nil {
		return nil
	}
	// Catalog readiness belongs to New Chat; apply completion belongs to its picker.
	if _, ready := target.settingsLoad.(uiNewSettingsReady); ready {
		if msg.picker == picker && picker.request == uiAgentPickerApply {
			return m.closeAgentPicker()
		}
		if picker.selectedRole == nil {
			m.focusCurrentAgent()
		}
	}
	return m.reconcileSpinnerTicking(false)
}

func (m *uiModel) applyAgentSelection() tea.Cmd {
	picker := m.agentPicker
	if picker.selectedRole == nil {
		return nil
	}
	role := *picker.selectedRole
	if role == m.agentSettings().GetSelectedAgent().GetRole() {
		return m.closeAgentPicker()
	}
	operation := &settingspb.MutationOperation{Operation: &settingspb.MutationOperation_AgentRole{AgentRole: role}}
	picker.request = uiAgentPickerApply
	picker.err = nil
	if m.newSessionTarget() != nil {
		return tea.Batch(m.mutateNewSessionSettings(operation), m.reconcileSpinnerTicking(false))
	}
	picker.pending = true
	client := m.selectedSessionSettingsClient()
	return tea.Batch(func() tea.Msg {
		response, err := client.MutateChatSettings(operation)
		return agentPickerAppliedMsg{picker: picker, sessionID: client.sessionID, result: chatSettingsDoneMsg{operation: operation, response: response, err: err}}
	}, m.reconcileSpinnerTicking(false))
}

func (m *uiModel) applyAgentPickerResult(msg agentPickerAppliedMsg) tea.Cmd {
	if m.currentRuntimeSessionID() != msg.sessionID.String() {
		return nil
	}
	cmd := m.applyChatSettingsDone(msg.result)
	if m.agentPicker != msg.picker {
		return cmd
	}
	m.agentPicker.pending = false
	if msg.result.err != nil {
		m.agentPicker.err = msg.result.err
		return cmd
	}
	if rejected := msg.result.response.Result.GetRejected(); rejected != nil {
		m.agentPicker.err = errors.New(chatSettingsRejectionNotices[rejected.Reason])
		return cmd
	}
	return tea.Batch(cmd, m.closeAgentPicker())
}

func (m *uiModel) closeAgentPicker() tea.Cmd {
	m.agentPicker = nil
	m.restorePrimaryInputMode()
	return tea.Batch(m.restoreTranscriptSurface(), m.reconcileSpinnerTicking(false))
}

func (m *uiModel) agentRowsPerPage() int {
	return max(1, (m.layout().effectiveHeight()-4)/2)
}

func (c uiInputController) handleAgentPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	switch msg.String() {
	case "ctrl+c":
		return c.handleRuntimeCtrlC(m.closeAgentPicker)
	}
	if m.agentPicker.request == uiAgentPickerApply && m.agentPickerLoading() {
		return m, nil
	}
	switch msg.String() {
	case "esc", "q":
		return m, m.closeAgentPicker()
	}
	if m.agentPickerLoading() {
		return m, nil
	}
	if m.agentPickerError() != nil {
		switch msg.String() {
		case "r", "enter":
			return m, m.retryAgentPicker()
		default:
			return m, nil
		}
	}
	selection := m.agentSelectionIndex()
	choices := m.agentChoices()
	switch msg.String() {
	case "enter":
		return m, m.applyAgentSelection()
	case "up", "k":
		selection--
	case "down", "j":
		selection++
	case "pgup":
		selection -= m.agentRowsPerPage()
	case "pgdown":
		selection += m.agentRowsPerPage()
	case "home":
		selection = 0
	case "end":
		selection = len(choices) - 1
	}
	if len(choices) > 0 {
		m.agentPicker.selectedRole = textutil.Value(choices[min(max(0, selection), len(choices)-1)].Role)
	}
	return m, nil
}

func (l uiViewLayout) renderAgentPicker(width, height int, style uiStyles) []string {
	m := l.model
	if m.agentPickerLoading() {
		return l.renderCenteredSpinner(width, height, style)
	}
	lines := []string{style.brand.Render(truncateQueuedMessageLine("Agent", width))}
	if height >= 5 || m.agentPickerError() != nil {
		lines = append(lines, "")
	}
	if err := m.agentPickerError(); err != nil {
		lines[1] = statusNoticeStyle(m.theme, uiStatusNoticeError).Render(truncateQueuedMessageLine(err.Error(), width))
	}
	choices := m.agentChoices()
	footerLines := 0
	if height >= 4 {
		footerLines = 1
	}
	available := max(0, height-len(lines)-footerLines)
	count := max(1, available/2)
	selection := m.agentSelectionIndex()
	start := max(0, min(selection-count+1, len(choices)-count))
	selectedAgent := m.agentSettings().GetSelectedAgent()
	palette := uiPalette(m.theme)
	for index := start; index < min(len(choices), start+count); index++ {
		choice := choices[index]
		row := lipgloss.NewStyle().Foreground(palette.foreground)
		rail := sharedtheme.SelectionRailBlank
		if m.agentPicker.selectedRole != nil && choice.Role == *m.agentPicker.selectedRole {
			row = row.Background(palette.modeBg)
			rail = sharedtheme.SelectionRailGlyph
		}
		title := choice.Role
		model, thinking := choice.GetModel(), choice.GetThinking()
		if choice.Role == selectedAgent.GetRole() {
			title += " (current)"
			model, thinking = selectedAgent.Model, selectedAgent.Thinking
		}
		lines = append(lines,
			row.Bold(true).Render(padANSIRight(truncateQueuedMessageLine(rail+" "+title, width), width)),
			row.Foreground(palette.muted).Render(padANSIRight(truncateQueuedMessageLine("  "+strings.TrimSpace(model+" "+thinking), width), width)),
		)
	}
	for len(lines) < height-footerLines {
		lines = append(lines, "")
	}
	footer := "Enter apply | Esc/q cancel | ↑/↓ j/k PgUp/PgDn Home/End"
	if m.agentPickerError() != nil {
		footer = "r/Enter retry | Esc/q cancel"
	}
	if footerLines > 0 {
		lines = append(lines, style.meta.Render(truncateQueuedMessageLine(footer, width)))
	}
	return l.renderChatContentLines(lines[:min(len(lines), height)], width, style)
}
