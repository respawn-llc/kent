package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type uiConnectionRemovalPhase uint8

const (
	uiConnectionRemovalClosed uiConnectionRemovalPhase = iota
	uiConnectionRemovalLoading
	uiConnectionRemovalSelecting
	uiConnectionRemovalConfirming
	uiConnectionRemovalRemoving
	uiConnectionRemovalRefreshing
	uiConnectionRemovalCatalogFailed
	uiConnectionRemovalRefreshFailed
)

type uiConnectionRemovalRequest uint8

const (
	uiConnectionRemovalLoadCatalog uiConnectionRemovalRequest = iota + 1
	uiConnectionRemovalSubmit
	uiConnectionRemovalRefreshCatalog
)

type uiConnectionRemovalChoiceKind uint8

const (
	uiConnectionRemovalChooseConnection uiConnectionRemovalChoiceKind = iota + 1
	uiConnectionRemovalReturnToChat
	uiConnectionRemovalCancelConfirmation
	uiConnectionRemovalConfirmRemoval
	uiConnectionRemovalRetryCatalog
)

type uiConnectionRemovalChoice struct {
	kind         uiConnectionRemovalChoiceKind
	connectionID *config.ConnectionID
}

type uiConnectionRemovalPickerOption struct {
	title  string
	choice uiConnectionRemovalChoice
}

type uiConnectionRemovalState struct {
	generation  uint64
	phase       uiConnectionRemovalPhase
	definitions []config.ConnectionID
	selectedID  *config.ConnectionID
	picker      *startupPickerModel
	choices     map[string]uiConnectionRemovalChoice
}

type uiConnectionRemovalFeatureState struct {
	connectionManagement apicontract.ConnectionManagementService
	connectionRemoval    uiConnectionRemovalState
	pendingRemovalToken  *uint64
}

type uiConnectionRemovalRequestDoneMsg struct {
	token   uint64
	request uiConnectionRemovalRequest
	catalog *authpb.ConnectionCatalog
	err     error
}

func (s uiConnectionRemovalState) isOpen() bool {
	return s.phase != uiConnectionRemovalClosed
}

func (s uiConnectionRemovalState) isLoading() bool {
	return s.phase == uiConnectionRemovalLoading || s.phase == uiConnectionRemovalRefreshing
}

func (s uiConnectionRemovalState) isRemoving() bool {
	return s.phase == uiConnectionRemovalRemoving
}

func (c uiInputController) startConnectionRemovalFlowCmd() tea.Cmd {
	m := c.model
	if m.pendingRemovalToken != nil {
		return m.sendTransientStatusWithNoticeID(
			"Connection removal is still in progress",
			uiStatusNoticeInfo, transientStatusDuration, uiStatusNoticeReplace, "",
		)
	}
	state := &m.connectionRemoval
	state.generation = nextNonZeroToken(state.generation)
	state.phase = uiConnectionRemovalClosed
	state.definitions = nil
	state.selectedID = nil
	state.picker = nil
	state.choices = nil
	m.helpVisible = false
	m.setInputMode(uiInputModeConnectionRemoval)
	surfaceCmd := m.activateSurface(uiSurfaceConnectionRemoval)
	catalogCmd := m.requestConnectionRemovalCatalog(uiConnectionRemovalLoadCatalog)
	return tea.Batch(surfaceCmd, catalogCmd, m.reconcileSpinnerTicking(false))
}

func (m *uiModel) requestConnectionRemovalCatalog(request uiConnectionRemovalRequest) tea.Cmd {
	state := &m.connectionRemoval
	state.generation = nextNonZeroToken(state.generation)
	state.phase = uiConnectionRemovalLoading
	if request == uiConnectionRemovalRefreshCatalog {
		state.phase = uiConnectionRemovalRefreshing
	}
	token := state.generation
	client := m.connectionManagement
	return func() tea.Msg {
		if client == nil {
			return uiConnectionRemovalRequestDoneMsg{
				token: token, request: request, err: errors.New("connection management service is required"),
			}
		}
		catalog, err := client.GetConnections(context.Background(), &authpb.GetConnectionsRequest{})
		return uiConnectionRemovalRequestDoneMsg{token: token, request: request, catalog: catalog, err: err}
	}
}

func (m *uiModel) removeSelectedConnectionCmd() tea.Cmd {
	state := &m.connectionRemoval
	if state.selectedID == nil {
		panic("connection removal confirmation is missing its selected connection")
	}
	state.phase = uiConnectionRemovalRemoving
	state.generation = nextNonZeroToken(state.generation)
	token := state.generation
	m.pendingRemovalToken = &token
	id := *state.selectedID
	client := m.connectionManagement
	return func() tea.Msg {
		if client == nil {
			return uiConnectionRemovalRequestDoneMsg{
				token: token, request: uiConnectionRemovalSubmit, err: errors.New("connection management service is required"),
			}
		}
		_, err := client.ConfigureConnection(context.Background(), &authpb.ConfigureConnectionRequest{
			Change: &authpb.ConfigureConnectionRequest_RemoveConnectionId{RemoveConnectionId: string(id)},
		})
		return uiConnectionRemovalRequestDoneMsg{token: token, request: uiConnectionRemovalSubmit, err: err}
	}
}

func (m *uiModel) reduceConnectionRemovalMessage(msg tea.Msg) uiFeatureUpdateResult {
	done, ok := msg.(uiConnectionRemovalRequestDoneMsg)
	if !ok {
		return uiFeatureUpdateResult{}
	}
	state := &m.connectionRemoval
	if done.request == uiConnectionRemovalSubmit {
		if m.pendingRemovalToken == nil ||
			*m.pendingRemovalToken != done.token {
			return handledUIFeatureUpdate(m, nil)
		}
		m.pendingRemovalToken = nil
		if !state.isOpen() || state.phase != uiConnectionRemovalRemoving || done.token != state.generation {
			if done.err == nil {
				return handledUIFeatureUpdate(m, nil)
			}
			message := connectionOperationErrorText(done.err)
			return handledUIFeatureUpdate(m, tea.Batch(
				m.appendLocalEntryWithNoticeID("error", message, ""),
				m.sendTransientStatusWithNoticeID(message, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, ""),
			))
		}
	} else if !state.isOpen() || done.token != state.generation {
		return handledUIFeatureUpdate(m, nil)
	}

	switch done.request {
	case uiConnectionRemovalLoadCatalog:
		if state.phase != uiConnectionRemovalLoading {
			return handledUIFeatureUpdate(m, nil)
		}
		if err := m.applyConnectionRemovalCatalog(done); err != nil {
			m.showConnectionRemovalCatalogFailure(err)
			return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
		}
		m.showConnectionRemovalList(nil)
	case uiConnectionRemovalSubmit:
		if state.phase != uiConnectionRemovalRemoving {
			return handledUIFeatureUpdate(m, nil)
		}
		if done.err != nil {
			state.phase = uiConnectionRemovalConfirming
			state.picker.result = startupPickerResult{}
			return handledUIFeatureUpdate(m, tea.Batch(
				m.sendTransientStatusWithNoticeID(connectionOperationErrorText(done.err), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, ""),
				m.reconcileSpinnerTicking(false),
			))
		}
		state.selectedID = nil
		return handledUIFeatureUpdate(m, tea.Batch(
			m.requestConnectionRemovalCatalog(uiConnectionRemovalRefreshCatalog),
			m.reconcileSpinnerTicking(false),
		))
	case uiConnectionRemovalRefreshCatalog:
		if state.phase != uiConnectionRemovalRefreshing {
			return handledUIFeatureUpdate(m, nil)
		}
		if err := m.applyConnectionRemovalCatalog(done); err != nil {
			m.showConnectionRemovalRefreshFailure(err)
			return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
		}
		if len(state.definitions) == 0 {
			closeCmd := m.closeConnectionRemovalFlow()
			m.exitAction = UIActionLogout
			return handledUIFeatureUpdate(m, sequenceCmds(closeCmd, tea.Quit))
		}
		m.showConnectionRemovalList(nil)
	default:
		panic(fmt.Sprintf("unknown connection removal request %d", done.request))
	}
	m.layout().syncViewport()
	return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
}

func (m *uiModel) applyConnectionRemovalCatalog(done uiConnectionRemovalRequestDoneMsg) error {
	if done.err != nil {
		if done.request == uiConnectionRemovalRefreshCatalog {
			return fmt.Errorf("load refreshed connection list: %w", done.err)
		}
		return fmt.Errorf("load connection list: %w", done.err)
	}
	if done.catalog == nil {
		return errors.New("server returned no connection catalog")
	}
	definitions := make([]config.ConnectionID, 0, len(done.catalog.Connections))
	for _, value := range done.catalog.Connections {
		id, _, err := protoapi.ConnectionFromProto(value)
		if err != nil {
			return fmt.Errorf("validate connection definition: %w", err)
		}
		definitions = append(definitions, id)
	}
	m.connectionRemoval.definitions = definitions
	return nil
}

func (m *uiModel) showConnectionRemovalList(preselect *config.ConnectionID) {
	state := &m.connectionRemoval
	state.phase = uiConnectionRemovalSelecting
	options := make([]uiConnectionRemovalPickerOption, 0, len(state.definitions)+1)
	selectedIndex := 0
	if len(state.definitions) == 0 {
		options = append(options, uiConnectionRemovalPickerOption{
			title: "Return to chat", choice: uiConnectionRemovalChoice{kind: uiConnectionRemovalReturnToChat},
		})
		m.setConnectionRemovalPicker(
			"**Provider connections**",
			"Provider connections",
			startupPickerNotice{},
			options,
		)
		return
	}
	for _, id := range state.definitions {
		if preselect != nil && id == *preselect {
			selectedIndex = len(options)
		}
		options = append(options, uiConnectionRemovalPickerOption{
			title: string(id),
			choice: uiConnectionRemovalChoice{
				kind: uiConnectionRemovalChooseConnection, connectionID: &id,
			},
		})
	}
	m.setConnectionRemovalPicker("**Provider connections**", "Provider connections", startupPickerNotice{}, options)
	if selectedIndex < len(state.picker.items) {
		state.picker.cursor = selectedIndex
		state.picker.ensureCursorVisible()
	}
}

func (m *uiModel) showConnectionRemovalConfirmation(id config.ConnectionID) {
	state := &m.connectionRemoval
	selectedID := id
	state.selectedID = &selectedID
	state.phase = uiConnectionRemovalConfirming
	m.setConnectionRemovalPicker(
		"**Remove connection "+string(id)+"?**",
		"Remove connection",
		startupPickerNotice{
			Text: "Removes this connection and its saved credentials server-wide.",
		},
		[]uiConnectionRemovalPickerOption{
			{title: "No", choice: uiConnectionRemovalChoice{kind: uiConnectionRemovalCancelConfirmation}},
			{title: "Yes", choice: uiConnectionRemovalChoice{kind: uiConnectionRemovalConfirmRemoval}},
		},
	)
}

func (m *uiModel) showConnectionRemovalCatalogFailure(err error) {
	m.connectionRemoval.phase = uiConnectionRemovalCatalogFailed
	m.setConnectionRemovalPicker(
		"**Provider connections unavailable**",
		"Provider connections unavailable",
		startupPickerNotice{Text: err.Error(), Kind: startupPickerNoticeError},
		[]uiConnectionRemovalPickerOption{
			{title: "Return to chat", choice: uiConnectionRemovalChoice{kind: uiConnectionRemovalReturnToChat}},
			{title: "Retry list", choice: uiConnectionRemovalChoice{kind: uiConnectionRemovalRetryCatalog}},
		},
	)
}

func (m *uiModel) showConnectionRemovalRefreshFailure(err error) {
	m.connectionRemoval.phase = uiConnectionRemovalRefreshFailed
	m.setConnectionRemovalPicker(
		"**Connection removed**",
		"Connection removed",
		startupPickerNotice{
			Text: err.Error(),
			Kind: startupPickerNoticeError,
		},
		[]uiConnectionRemovalPickerOption{
			{title: "Return to chat", choice: uiConnectionRemovalChoice{kind: uiConnectionRemovalReturnToChat}},
			{title: "Retry list", choice: uiConnectionRemovalChoice{kind: uiConnectionRemovalRetryCatalog}},
		},
	)
}

func (m *uiModel) setConnectionRemovalPicker(
	headerMarkdown string,
	headerFallback string,
	notice startupPickerNotice,
	options []uiConnectionRemovalPickerOption,
) {
	items := make([]startupPickerOption, 0, len(options))
	choices := make(map[string]uiConnectionRemovalChoice, len(options))
	for index, option := range options {
		id := strconv.Itoa(index)
		items = append(items, startupPickerOption{ID: id, Title: option.title})
		choices[id] = option.choice
	}
	picker := newStartupPickerModel(headerMarkdown, headerFallback, m.theme, notice, items)
	m.connectionRemoval.picker = picker
	m.connectionRemoval.choices = choices
	m.updateConnectionRemovalPickerSize()
}

func (m *uiModel) updateConnectionRemovalPickerSize() {
	if m.connectionRemoval.picker == nil {
		return
	}
	next, _ := m.connectionRemoval.picker.Update(tea.WindowSizeMsg{
		Width:  m.layout().effectiveWidth(),
		Height: m.layout().calcChatLines(),
	})
	picker, ok := next.(*startupPickerModel)
	if !ok {
		panic("connection removal picker returned an unexpected model")
	}
	m.connectionRemoval.picker = picker
}

func (c uiInputController) handleConnectionRemovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	state := &m.connectionRemoval
	if msg.Type == tea.KeyCtrlC {
		return c.handleRuntimeCtrlC(func() tea.Cmd {
			return m.closeConnectionRemovalFlow()
		})
	}
	if state.isLoading() || state.isRemoving() {
		if msg.Type == tea.KeyEsc {
			return m, nil
		}
		return m, nil
	}
	if state.picker == nil {
		panic("open connection removal flow is missing its picker")
	}
	next, _ := state.picker.Update(msg)
	picker, ok := next.(*startupPickerModel)
	if !ok {
		panic("connection removal picker returned an unexpected model")
	}
	state.picker = picker
	if picker.result.Canceled {
		switch state.phase {
		case uiConnectionRemovalConfirming:
			return m, m.returnToConnectionRemovalList()
		case uiConnectionRemovalSelecting, uiConnectionRemovalCatalogFailed, uiConnectionRemovalRefreshFailed:
			return m, m.closeConnectionRemovalFlow()
		default:
			panic(fmt.Sprintf("connection removal cannot be canceled in phase %d", state.phase))
		}
	}
	if picker.result.ChoiceID == "" {
		return m, nil
	}
	choice, present := state.choices[picker.result.ChoiceID]
	if !present {
		panic("connection removal picker selected an unknown option")
	}

	switch state.phase {
	case uiConnectionRemovalSelecting:
		switch choice.kind {
		case uiConnectionRemovalChooseConnection:
			if choice.connectionID == nil {
				panic("connection removal list option is missing its connection ID")
			}
			m.showConnectionRemovalConfirmation(*choice.connectionID)
		case uiConnectionRemovalReturnToChat:
			return m, m.closeConnectionRemovalFlow()
		default:
			panic(fmt.Sprintf("invalid connection removal list choice %d", choice.kind))
		}
	case uiConnectionRemovalConfirming:
		switch choice.kind {
		case uiConnectionRemovalCancelConfirmation:
			return m, m.returnToConnectionRemovalList()
		case uiConnectionRemovalConfirmRemoval:
			return m, tea.Batch(m.removeSelectedConnectionCmd(), m.reconcileSpinnerTicking(false))
		default:
			panic(fmt.Sprintf("invalid connection removal confirmation choice %d", choice.kind))
		}
	case uiConnectionRemovalCatalogFailed:
		switch choice.kind {
		case uiConnectionRemovalReturnToChat:
			return m, m.closeConnectionRemovalFlow()
		case uiConnectionRemovalRetryCatalog:
			return m, tea.Batch(m.requestConnectionRemovalCatalog(uiConnectionRemovalLoadCatalog), m.reconcileSpinnerTicking(false))
		default:
			panic(fmt.Sprintf("invalid catalog failure choice %d", choice.kind))
		}
	case uiConnectionRemovalRefreshFailed:
		switch choice.kind {
		case uiConnectionRemovalReturnToChat:
			return m, m.closeConnectionRemovalFlow()
		case uiConnectionRemovalRetryCatalog:
			return m, tea.Batch(m.requestConnectionRemovalCatalog(uiConnectionRemovalRefreshCatalog), m.reconcileSpinnerTicking(false))
		default:
			panic(fmt.Sprintf("invalid refresh failure choice %d", choice.kind))
		}
	default:
		panic(fmt.Sprintf("connection removal picker is unavailable in phase %d", state.phase))
	}
	return m, nil
}

func (m *uiModel) returnToConnectionRemovalList() tea.Cmd {
	selectedID := m.connectionRemoval.selectedID
	m.showConnectionRemovalList(selectedID)
	return m.reconcileSpinnerTicking(false)
}

func (m *uiModel) closeConnectionRemovalFlow() tea.Cmd {
	state := &m.connectionRemoval
	state.generation = nextNonZeroToken(state.generation)
	state.phase = uiConnectionRemovalClosed
	state.definitions = nil
	state.selectedID = nil
	state.picker = nil
	state.choices = nil
	m.restorePrimaryInputMode()
	return tea.Batch(m.restoreTranscriptSurface(), m.reconcileSpinnerTicking(false))
}

func (l uiViewLayout) renderConnectionRemoval(width, height int, style uiStyles) []string {
	state := l.model.connectionRemoval
	if state.isLoading() || state.isRemoving() {
		spinner := lipgloss.NewStyle().Foreground(uiPalette(l.model.theme).primary).Render(
			pendingToolSpinnerFrame(l.model.spinnerFrame),
		)
		placed := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, spinner)
		return l.renderChatContentLines(strings.Split(placed, "\n"), width, style)
	}
	if state.picker == nil {
		panic("connection removal surface is missing its picker")
	}
	return l.renderChatContentLines(strings.Split(state.picker.View(), "\n"), width, style)
}
