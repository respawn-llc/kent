package app

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *uiModel) refreshProcessEntries() {
	selectedID := ""
	if m.processList.selection >= 0 && m.processList.selection < len(m.processList.entries) {
		selectedID = m.processList.entries[m.processList.selection].ID
	}
	if m.processClient == nil {
		m.processList.entries = nil
		m.processList.selection = 0
		return
	}
	m.processList.entries = m.listProcesses()
	if len(m.processList.entries) == 0 {
		m.processList.selection = 0
		return
	}
	if selectedID != "" {
		for idx, entry := range m.processList.entries {
			if entry.ID == selectedID {
				m.processList.selection = idx
				return
			}
		}
	}
	if m.processList.selection < 0 {
		m.processList.selection = 0
	}
	if m.processList.selection >= len(m.processList.entries) {
		m.processList.selection = len(m.processList.entries) - 1
	}
}

func (m *uiModel) refreshProcessEntriesIfOpen() {
	if m == nil || !m.processList.isOpen() {
		return
	}
	m.refreshProcessEntries()
}

func (m *uiModel) openProcessList() {
	m.processList.open = true
	m.setInputMode(uiInputModeProcessList)
	m.refreshProcessEntries()
}

func (m *uiModel) closeProcessList() {
	m.processList.open = false
	m.refreshProcessEntries()
	m.restorePrimaryInputMode()
}

func (m *uiModel) pushProcessOverlayIfNeeded() tea.Cmd {
	return m.activateSurface(uiSurfaceProcessList)
}

func (m *uiModel) popProcessOverlayIfNeeded() tea.Cmd {
	return m.restoreTranscriptSurface()
}

func (m *uiModel) moveProcessSelection(delta int) {
	if len(m.processList.entries) == 0 {
		m.processList.selection = 0
		return
	}
	m.processList.selection += delta
	if m.processList.selection < 0 {
		m.processList.selection = 0
	}
	if m.processList.selection >= len(m.processList.entries) {
		m.processList.selection = len(m.processList.entries) - 1
	}
}

func (m *uiModel) moveProcessSelectionPage(deltaPages int) {
	rowsPerPage := m.processListRowsPerPage()
	m.moveProcessSelection(deltaPages * rowsPerPage)
}

func (m *uiModel) processListRowsPerPage() int {
	available := m.termHeight - 1 - processListHeaderLines - processListFooterLines // status line + header + footer
	if available < processListEntryLines {
		return 1
	}
	rows := available / processListEntryLines
	if rows < 1 {
		return 1
	}
	return rows
}

func (m *uiModel) selectFirstProcess() {
	if len(m.processList.entries) == 0 {
		m.processList.selection = 0
		return
	}
	m.processList.selection = 0
}

func (m *uiModel) selectLastProcess() {
	if len(m.processList.entries) == 0 {
		m.processList.selection = 0
		return
	}
	m.processList.selection = len(m.processList.entries) - 1
}

func (m *uiModel) selectedProcess() (clientui.BackgroundProcess, bool) {
	if len(m.processList.entries) == 0 || m.processList.selection < 0 || m.processList.selection >= len(m.processList.entries) {
		return clientui.BackgroundProcess{}, false
	}
	return m.processList.entries[m.processList.selection], true
}

func (m *uiModel) processListHasRunningEntries() bool {
	if m == nil || !m.processList.isOpen() {
		return false
	}
	for _, entry := range m.processList.entries {
		if entry.Running || strings.TrimSpace(entry.State) == "starting" || strings.TrimSpace(entry.State) == "running" {
			return true
		}
	}
	return false
}

func (c uiInputController) handleProcessListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	switch strings.ToLower(msg.String()) {
	case "ctrl+c":
		m.exitAction = UIActionExit
		if overlayCmd := m.popProcessOverlayIfNeeded(); overlayCmd != nil {
			m.closeProcessList()
			return m, tea.Sequence(overlayCmd, tea.Quit)
		}
		return m, tea.Quit
	case "esc", "q":
		return m, c.stopProcessListFlowCmd()
	case "up":
		m.moveProcessSelection(-1)
		return m, nil
	case "down":
		m.moveProcessSelection(1)
		return m, nil
	case "pgup":
		m.moveProcessSelectionPage(-1)
		return m, nil
	case "pgdown":
		m.moveProcessSelectionPage(1)
		return m, nil
	case "home":
		m.selectFirstProcess()
		return m, nil
	case "end":
		m.selectLastProcess()
		return m, nil
	case "r":
		m.refreshProcessEntries()
		return m, c.showTransientStatus(fmt.Sprintf("refreshed %d processes", len(m.processList.entries)))
	case "enter":
		return c.runProcessListAction("inline")
	case "k":
		return c.runProcessListAction("kill")
	case "i":
		return c.runProcessListAction("inline")
	case "o":
		return c.runProcessListAction("logs")
	default:
		return m, nil
	}
}

func (c uiInputController) runProcessListAction(action string) (tea.Model, tea.Cmd) {
	m := c.model
	selected, ok := m.selectedProcess()
	if !ok {
		return m, c.showErrorStatus("no background process selected")
	}
	return c.runProcessAction(action, selected.ID)
}

func (c uiInputController) runProcessAction(action, id string) (tea.Model, tea.Cmd) {
	m := c.model
	if m.processClient == nil {
		return m, c.showErrorStatus("background process client is unavailable")
	}
	action = strings.ToLower(strings.TrimSpace(action))
	id = strings.TrimSpace(id)
	if id == "" {
		return m, c.startProcessListFlowCmd()
	}
	switch action {
	case "kill":
		if err := m.processClient.KillProcess(id); err != nil {
			return m, c.showErrorStatus(err.Error())
		}
		m.refreshProcessEntries()
		return m, c.showTransientStatus(fmt.Sprintf("sent terminate signal to %s", id))
	case "inline":
		preview, _, err := m.processClient.InlineOutput(id, 12_000)
		if err != nil {
			return m, c.showErrorStatus(err.Error())
		}
		preview = strings.TrimSpace(preview)
		if preview == "" {
			preview = "<no output yet>"
		}
		c.releaseLockedInjectedInput(true)
		m.appendProcessOutputToInput(id, preview)
		return m, tea.Batch(c.stopProcessListFlowCmd(), c.showTransientStatus("Pasted shell transcript"))
	case "logs":
		path, err := processLogPath(m.listProcesses(), id)
		if err != nil {
			return m, c.showErrorStatus(err.Error())
		}
		if err := openDefault(path); err == nil {
			return m, tea.Batch(c.stopProcessListFlowCmd(), c.showTransientStatus("Opened logs"))
		}
		editorCmd, err := editorCommand(path)
		if err != nil {
			return m, c.showErrorStatus(err.Error())
		}
		return m, tea.Batch(
			c.stopProcessListFlowCmd(),
			c.showTransientStatus("Opened logs"),
			tea.ExecProcess(editorCmd, func(runErr error) tea.Msg {
				return openProcessLogsDoneMsg{err: runErr}
			}),
		)
	default:
		return m, c.showErrorStatus(fmt.Sprintf("unknown /ps action %q", action))
	}
}

func (m *uiModel) appendProcessOutputToInput(id, output string) {
	payload := fmt.Sprintf("Output of bg shell %s:\n%s\n", id, output)
	if strings.TrimSpace(m.input) == "" {
		m.replaceMainInput(payload, -1)
		return
	}
	m.moveCursorEnd()
	prefix := "\n"
	if strings.HasSuffix(m.input, "\n") {
		prefix = ""
	}
	m.insertInputRunes([]rune(prefix + payload))
}

func processLogPath(entries []clientui.BackgroundProcess, id string) (string, error) {
	for _, entry := range entries {
		if entry.ID == id {
			if strings.TrimSpace(entry.LogPath) == "" {
				return "", fmt.Errorf("process %s has no log file", id)
			}
			return entry.LogPath, nil
		}
	}
	return "", fmt.Errorf("unknown session_id %s", id)
}

func editorCommand(path string) (*exec.Cmd, error) {
	editor := strings.TrimSpace(os.Getenv("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("EDITOR"))
	}
	if editor == "" {
		return nil, fmt.Errorf("open logs failed and EDITOR/VISUAL is not set")
	}
	shellPath := strings.TrimSpace(os.Getenv("SHELL"))
	if shellPath == "" {
		shellPath = "/bin/sh"
	}
	cmd := exec.Command(shellPath, "-lc", `eval "$BUILDER_EDITOR \"$1\""`, "builder-editor", path)
	cmd.Env = append(os.Environ(), "BUILDER_EDITOR="+editor)
	return cmd, nil
}

var openDefault = func(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("open is not supported on %s", runtime.GOOS)
	}
	return cmd.Start()
}
