package app

import (
	"builder/cli/tui"
	"builder/shared/clientui"

	"github.com/charmbracelet/glamour"
)

type uiInputMode string

const (
	uiInputModeMain              uiInputMode = "main"
	uiInputModeAsk               uiInputMode = "ask"
	uiInputModeStatus            uiInputMode = "status"
	uiInputModeGoal              uiInputMode = "goal"
	uiInputModeWorktree          uiInputMode = "worktree"
	uiInputModeRollbackSelection uiInputMode = "rollback_selection"
	uiInputModeRollbackEdit      uiInputMode = "rollback_edit"
	uiInputModeProcessList       uiInputMode = "process_list"
)

type uiInteractionState struct {
	Mode uiInputMode
}

type uiAskState struct {
	current      *askEvent
	currentToken uint64
	queue        []askEvent
	cursor       int
	freeform     bool
	freeformMode askFreeformMode
	input        string
	inputCursor  int
	inputKill    string
}

type uiProcessListState struct {
	open      bool
	selection int
	entries   []clientui.BackgroundProcess
}

type uiRollbackPhase string

const (
	uiRollbackPhaseInactive  uiRollbackPhase = "inactive"
	uiRollbackPhaseSelection uiRollbackPhase = "selection"
	uiRollbackPhaseEditing   uiRollbackPhase = "editing"
)

type uiRollbackState struct {
	phase                     uiRollbackPhase
	suppressedAlternateScroll bool
	restoreTranscriptMode     tui.Mode
	candidates                []rollbackCandidate
	selection                 int
	selectedTranscriptEntry   int
	selectedTargetID          string
	pendingSelectionAnchor    int
	pendingSelectionDelta     int
	restoreOngoingScroll      int
	restoreScrollActive       bool
}

type uiStatusOverlayState struct {
	open            bool
	loading         bool
	scroll          int
	snapshot        uiStatusSnapshot
	error           string
	refreshToken    uint64
	pendingSections map[uiStatusSection]bool
	sectionWarnings map[uiStatusSection]string
}

type uiGoalOverlayState struct {
	open             bool
	scroll           int
	goal             *clientui.RuntimeGoal
	confirmMode      string
	confirmSelection int
	pendingObjective string
	error            string
	markdownTheme    string
	markdownWidth    int
	markdownRenderer *glamour.TermRenderer
}

func (s uiAskState) hasCurrent() bool {
	return s.current != nil
}

func (s uiProcessListState) isOpen() bool {
	return s.open
}

func (s uiRollbackState) isSelecting() bool {
	return s.phase == uiRollbackPhaseSelection
}

func (s uiRollbackState) isEditing() bool {
	return s.phase == uiRollbackPhaseEditing
}

func (s uiRollbackState) isActive() bool {
	return s.phase != uiRollbackPhaseInactive
}

func (s uiStatusOverlayState) isOpen() bool {
	return s.open
}

func (s uiGoalOverlayState) isOpen() bool {
	return s.open
}

type uiInputModeState struct {
	Mode           uiInputMode
	InputLocked    bool
	Busy           bool
	ShowsMainInput bool
	ShowsAskInput  bool
}

func (m *uiModel) inputMode() uiInputMode {
	if m == nil || m.interaction.Mode == "" {
		return uiInputModeMain
	}
	return m.interaction.Mode
}

func (m *uiModel) setInputMode(mode uiInputMode) {
	if m == nil {
		return
	}
	if mode == "" {
		mode = uiInputModeMain
	}
	m.interaction.Mode = mode
}

func (m *uiModel) restorePrimaryInputMode() {
	if m == nil {
		return
	}
	if m.ask.hasCurrent() && (m.view.Mode() == "" || m.view.Mode() == tui.ModeOngoing) {
		m.setInputMode(uiInputModeAsk)
		return
	}
	m.setInputMode(uiInputModeMain)
}

func (m *uiModel) inputModeState() uiInputModeState {
	mode := m.inputMode()
	return uiInputModeState{
		Mode:           mode,
		InputLocked:    m != nil && m.isInputLocked(),
		Busy:           m != nil && m.busy,
		ShowsMainInput: mode.showsMainInput(),
		ShowsAskInput:  mode.showsAskInput(),
	}
}

func (mode uiInputMode) showsMainInput() bool {
	return mode == uiInputModeMain || mode == uiInputModeRollbackEdit
}

func (mode uiInputMode) showsAskInput() bool {
	return mode == uiInputModeAsk
}

func (mode uiInputMode) suppressesMainInput() bool {
	return !mode.showsMainInput() && !mode.showsAskInput()
}
