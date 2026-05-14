package app

import (
	"context"
	"strings"

	"builder/cli/app/internal/worktreecreateform"
	"builder/cli/app/internal/worktreedelete"
	"builder/cli/app/internal/worktreemutation"
	"builder/cli/app/internal/worktreeselection"
	"builder/cli/app/internal/worktreeview"
	tuiinput "builder/cli/tui/input"
	"builder/shared/clientui"
	"builder/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	worktreeOverlayHeaderLines = 3
	worktreeOverlayFooterLines = 1
	worktreeOverlayRowLines    = 3
	worktreeCreateRowID        = worktreeselection.CreateRowID
)

type uiWorktreeOverlayPhase string

const (
	uiWorktreeOverlayPhaseList          uiWorktreeOverlayPhase = "list"
	uiWorktreeOverlayPhaseCreate        uiWorktreeOverlayPhase = "create"
	uiWorktreeOverlayPhaseDeleteConfirm uiWorktreeOverlayPhase = "delete_confirm"
)

type uiWorktreeOpenIntent struct {
	OpenCreate          bool
	OpenDelete          bool
	ConfirmDeleteTarget string
	PreferDeleteBranch  bool
}

type uiWorktreeCreateField = worktreecreateform.Field

const (
	uiWorktreeCreateFieldBranchTarget = worktreecreateform.FieldBranchTarget
	uiWorktreeCreateFieldBaseRef      = worktreecreateform.FieldBaseRef
	uiWorktreeCreateFieldActions      = worktreecreateform.FieldActions
)

type uiWorktreeCreateAction = worktreecreateform.Action

const (
	uiWorktreeCreateActionCreate = worktreecreateform.ActionCreate
	uiWorktreeCreateActionCancel = worktreecreateform.ActionCancel
)

type uiWorktreeCreateDialogState struct {
	baseRef       tuiinput.Editor
	branchTarget  tuiinput.Editor
	focus         uiWorktreeCreateField
	action        uiWorktreeCreateAction
	errorText     string
	submitting    bool
	resolving     bool
	submitPending bool
	resolveToken  uint64
	resolution    serverapi.WorktreeCreateTargetResolution
}

type uiWorktreeDeleteAction = worktreedelete.Action

const (
	uiWorktreeDeleteActionCancel       = worktreedelete.ActionCancel
	uiWorktreeDeleteActionDelete       = worktreedelete.ActionDelete
	uiWorktreeDeleteActionDeleteBranch = worktreedelete.ActionDeleteBranch
)

type uiWorktreeDeleteDialogState struct {
	target             serverapi.WorktreeView
	selectedAction     uiWorktreeDeleteAction
	preferDeleteBranch bool
	errorText          string
	submitting         bool
}

type uiWorktreeOverlayState struct {
	open          bool
	loading       bool
	phase         uiWorktreeOverlayPhase
	selection     int
	target        clientui.SessionExecutionTarget
	entries       []serverapi.WorktreeView
	errorText     string
	refreshToken  uint64
	mutationToken uint64
	switchPending bool
	selectedID    string
	intent        uiWorktreeOpenIntent
	create        uiWorktreeCreateDialogState
	deleteConfirm uiWorktreeDeleteDialogState
	inputCursor   uiInputFieldCursor
}

type worktreeListDoneMsg struct {
	token uint64
	resp  serverapi.WorktreeListResponse
	err   error
}

type worktreeCreateDoneMsg struct {
	token uint64
	resp  serverapi.WorktreeCreateResponse
	err   error
}

type worktreeSwitchDoneMsg struct {
	token uint64
	resp  serverapi.WorktreeSwitchResponse
	err   error
}

type worktreeDeleteDoneMsg struct {
	token uint64
	resp  serverapi.WorktreeDeleteResponse
	err   error
}

func newWorktreeDialogEditor(value string) tuiinput.Editor {
	return newSingleLineEditor(strings.TrimSpace(value))
}

func newWorktreeCreateDialog(suggestedBranch string) uiWorktreeCreateDialogState {
	dialog := uiWorktreeCreateDialogState{
		baseRef:      newWorktreeDialogEditor("HEAD"),
		branchTarget: newWorktreeDialogEditor(strings.TrimSpace(suggestedBranch)),
		focus:        uiWorktreeCreateFieldBranchTarget,
		action:       uiWorktreeCreateActionCreate,
	}
	dialog.syncFocus()
	return dialog
}

func (s uiWorktreeOverlayState) isOpen() bool {
	return s.open
}

func (s uiWorktreeOverlayState) visibleErrorText() string {
	if !s.open {
		return ""
	}
	switch s.phase {
	case uiWorktreeOverlayPhaseCreate:
		return strings.TrimSpace(s.create.errorText)
	case uiWorktreeOverlayPhaseDeleteConfirm:
		return strings.TrimSpace(s.deleteConfirm.errorText)
	default:
		return strings.TrimSpace(s.errorText)
	}
}

func (m *uiModel) openWorktreeOverlay(intent uiWorktreeOpenIntent) {
	if m == nil {
		return
	}
	m.worktrees.open = true
	m.worktrees.phase = uiWorktreeOverlayPhaseList
	m.worktrees.loading = true
	m.worktrees.errorText = ""
	m.worktrees.intent = intent
	m.worktrees.create = uiWorktreeCreateDialogState{}
	m.worktrees.deleteConfirm = uiWorktreeDeleteDialogState{}
	m.setInputMode(uiInputModeWorktree)
	if len(m.worktrees.entries) == 0 {
		m.worktrees.selection = 0
	}
}

func (m *uiModel) closeWorktreeOverlay() {
	if m == nil {
		return
	}
	if m.worktrees.switchPending {
		return
	}
	m.worktrees = uiWorktreeOverlayState{}
	m.restorePrimaryInputMode()
}

func (m *uiModel) pushWorktreeOverlayIfNeeded() tea.Cmd {
	return m.activateSurface(uiSurfaceWorktree)
}

func (m *uiModel) popWorktreeOverlayIfNeeded() tea.Cmd {
	return m.restoreTranscriptSurface()
}

func (m *uiModel) requestWorktreeListCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	m.worktrees.refreshToken++
	token := m.worktrees.refreshToken
	includeDirtyCount := m.worktrees.intent.OpenDelete || m.worktrees.phase == uiWorktreeOverlayPhaseDeleteConfirm
	m.worktrees.loading = true
	m.worktrees.errorText = ""
	return func() tea.Msg {
		resp, err := m.listWorktreesForCurrentSession(includeDirtyCount)
		return worktreeListDoneMsg{token: token, resp: resp, err: err}
	}
}

func (m *uiModel) openCreateWorktreeDialog() tea.Cmd {
	if m == nil {
		return nil
	}
	m.worktrees.phase = uiWorktreeOverlayPhaseCreate
	m.worktrees.errorText = ""
	m.worktrees.create = newWorktreeCreateDialog(m.suggestedWorktreeBranchFromEntries())
	return m.scheduleWorktreeCreateTargetResolution()
}

func (m *uiModel) openDeleteWorktreeDialog(target serverapi.WorktreeView, preferDeleteBranch bool) {
	if m == nil {
		return
	}
	m.worktrees.phase = uiWorktreeOverlayPhaseDeleteConfirm
	m.worktrees.errorText = ""
	m.worktrees.deleteConfirm = uiWorktreeDeleteDialogState{target: target, preferDeleteBranch: preferDeleteBranch}
	m.worktrees.deleteConfirm.clampSelection()
}

func (m *uiModel) closeWorktreeDialog() {
	if m == nil {
		return
	}
	m.worktrees.phase = uiWorktreeOverlayPhaseList
	m.worktrees.create = uiWorktreeCreateDialogState{}
	m.worktrees.deleteConfirm = uiWorktreeDeleteDialogState{}
	m.worktrees.errorText = ""
}

func (m *uiModel) applyWorktreeListResponse(resp serverapi.WorktreeListResponse) {
	if m == nil {
		return
	}
	m.recordWorktreeSelection()
	m.worktrees.target = resp.Target
	m.worktrees.entries = append([]serverapi.WorktreeView(nil), resp.Worktrees...)
	m.restoreWorktreeSelection()
	m.clampWorktreeSelection()
	if m.worktrees.phase == uiWorktreeOverlayPhaseDeleteConfirm {
		targetID := strings.TrimSpace(m.worktrees.deleteConfirm.target.WorktreeID)
		if targetID == "" {
			m.closeWorktreeDialog()
			return
		}
		for _, item := range m.worktrees.entries {
			if strings.TrimSpace(item.WorktreeID) == targetID {
				m.worktrees.deleteConfirm.target = item
				m.worktrees.deleteConfirm.clampSelection()
				return
			}
		}
		m.closeWorktreeDialog()
	}
}

func (m *uiModel) applyWorktreeIntent() tea.Cmd {
	if m == nil {
		return nil
	}
	intent := m.worktrees.intent
	m.worktrees.intent = uiWorktreeOpenIntent{}
	if intent.OpenCreate {
		return m.openCreateWorktreeDialog()
	}
	if !intent.OpenDelete {
		return nil
	}
	target, err := resolveWorktreeDeletionTargetFromEntries(m.worktrees.entries, intent.ConfirmDeleteTarget)
	if err != nil {
		m.worktrees.errorText = formatSubmissionError(err)
		return nil
	}
	m.recordWorktreeSelection()
	for idx, item := range m.worktrees.entries {
		if strings.TrimSpace(item.WorktreeID) == strings.TrimSpace(target.WorktreeID) {
			m.worktrees.selection = idx + 1
			break
		}
	}
	m.openDeleteWorktreeDialog(target, intent.PreferDeleteBranch)
	return nil
}

func resolveWorktreeDeletionTargetFromEntries(entries []serverapi.WorktreeView, token string) (serverapi.WorktreeView, error) {
	return worktreeview.ResolveDeletionTarget(entries, token)
}

func resolveWorktreeTokenFromEntries(entries []serverapi.WorktreeView, token string) (serverapi.WorktreeView, error) {
	return worktreeview.ResolveToken(entries, token)
}

func (m *uiModel) suggestedWorktreeBranchFromEntries() string {
	if m == nil {
		return ""
	}
	if sessionBranch := sanitizeWorktreeBranchSuggestion(m.suggestedWorktreeSessionName()); sessionBranch != "" {
		return sessionBranch
	}
	return ""
}

func (m *uiModel) worktreeCreateCmd(req serverapi.WorktreeCreateRequest) tea.Cmd {
	if m == nil {
		return nil
	}
	m.worktrees.mutationToken++
	token := m.worktrees.mutationToken
	m.worktrees.create.errorText = ""
	m.worktrees.create.submitting = true
	return func() tea.Msg {
		resp, err := m.worktreeMutationService().Create(req)
		return worktreeCreateDoneMsg{token: token, resp: resp, err: err}
	}
}

func (m *uiModel) worktreeSwitchCmd(target serverapi.WorktreeView) tea.Cmd {
	if m == nil {
		return nil
	}
	m.worktrees.mutationToken++
	m.worktrees.switchPending = true
	token := m.worktrees.mutationToken
	m.worktrees.errorText = ""
	return func() tea.Msg {
		resp, err := m.worktreeMutationService().Switch(target.WorktreeID)
		return worktreeSwitchDoneMsg{token: token, resp: resp, err: err}
	}
}

func (m *uiModel) worktreeDeleteCmd(target serverapi.WorktreeView, deleteBranch bool) tea.Cmd {
	if m == nil {
		return nil
	}
	m.worktrees.mutationToken++
	token := m.worktrees.mutationToken
	m.worktrees.deleteConfirm.errorText = ""
	m.worktrees.deleteConfirm.submitting = true
	return func() tea.Msg {
		resp, err := m.worktreeMutationService().Delete(target.WorktreeID, deleteBranch)
		return worktreeDeleteDoneMsg{token: token, resp: resp, err: err}
	}
}

func (m *uiModel) worktreeMutationService() worktreemutation.Service {
	if m == nil {
		return worktreemutation.Service{}
	}
	service := worktreemutation.Service{
		Client:    m.worktreeClient,
		SessionID: m.sessionID,
		ResolveContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), uiRuntimeControlTimeout)
		},
	}
	if client, ok := m.runtimeClient().(*sessionRuntimeClient); ok && client != nil {
		service.Runtime = worktreemutation.RuntimeControl{
			Context:        client.controlContext,
			CurrentLeaseID: client.controllerLeaseIDValue,
			RecoverLease:   client.recoverControllerLease,
			ReadOnly:       client.isReadOnly,
		}
	}
	return service
}
