package app

import (
	"context"
	"errors"
	"io"
	"strings"

	"core/cli/app/internal/runtimeattach"
	"core/cli/app/internal/worktreeui"
	tuiinput "core/cli/tui/input"
	"core/shared/apicontract"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/worktreecontract"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

const (
	worktreeOverlayHeaderLines = 3
	worktreeOverlayFooterLines = 1
	worktreeOverlayRowLines    = 3
)

type uiWorktreeOverlayPhase string

const (
	uiWorktreeOverlayPhaseList          uiWorktreeOverlayPhase = "list"
	uiWorktreeOverlayPhaseCreate        uiWorktreeOverlayPhase = "create"
	uiWorktreeOverlayPhaseDeleteConfirm uiWorktreeOverlayPhase = "delete_confirm"
)

type uiWorktreeOpenIntent struct {
	OpenCreate         bool
	OpenDelete         bool
	DeleteTarget       uiWorktreeDeleteIntentTarget
	PreferDeleteBranch bool
}

type uiWorktreeDeleteIntentTargetKind uint8

const (
	uiWorktreeDeleteIntentTargetCurrent uiWorktreeDeleteIntentTargetKind = iota
	uiWorktreeDeleteIntentTargetSelector
	uiWorktreeDeleteIntentTargetIdentity
)

type uiWorktreeDeleteIntentTarget struct {
	kind     uiWorktreeDeleteIntentTargetKind
	selector string
	identity worktreeui.SelectionIdentity
}

type uiWorktreeCreateField = worktreeui.Field

const (
	uiWorktreeCreateFieldBranchTarget = worktreeui.FieldBranchTarget
	uiWorktreeCreateFieldBaseRef      = worktreeui.FieldBaseRef
	uiWorktreeCreateFieldActions      = worktreeui.FieldActions
)

type uiWorktreeCreateAction = worktreeui.CreateFormAction

const (
	uiWorktreeCreateActionCreate = worktreeui.CreateFormActionCreate
	uiWorktreeCreateActionCancel = worktreeui.CreateFormActionCancel
)

type uiWorktreeCreateDialogState struct {
	baseRef          tuiinput.Editor
	branchTarget     tuiinput.Editor
	focus            uiWorktreeCreateField
	action           uiWorktreeCreateAction
	errorText        string
	baseRefErrorText string
	submitting       bool
	resolving        bool
	submitPending    bool
	resolveToken     uint64
	resolution       *worktreepb.CreateTargetResolution
	setupProgress    *uiWorktreeSetupProgressState
	setupEvent       *worktreepb.SetupEvent
}

type uiWorktreeSetupProgressState struct {
	cancel context.CancelFunc
}

type uiWorktreeDeleteAction = worktreeui.DeleteAction

const (
	uiWorktreeDeleteActionCancel       = worktreeui.DeleteActionCancel
	uiWorktreeDeleteActionDelete       = worktreeui.DeleteActionDelete
	uiWorktreeDeleteActionDeleteBranch = worktreeui.DeleteActionDeleteBranch
)

type uiWorktreeDeleteTargetAuthority uint8

const (
	uiWorktreeDeleteTargetAuthorityListed uiWorktreeDeleteTargetAuthority = iota
	uiWorktreeDeleteTargetAuthorityResolvedSelector
)

type uiWorktreeDeleteDialogState struct {
	target             worktreeui.Item
	targetAuthority    uiWorktreeDeleteTargetAuthority
	selectedAction     uiWorktreeDeleteAction
	preferDeleteBranch bool
	errorText          string
	submitting         bool
	forceFolderRemoval bool
}

type uiWorktreeOverlayState struct {
	open                          bool
	listPending                   bool
	deleteTargetResolutionPending bool
	phase                         uiWorktreeOverlayPhase
	selection                     int
	target                        *worktreepb.SessionExecutionTarget
	entries                       []worktreeui.Item
	errorText                     string
	mutationToken                 uint64
	switchToken                   uint64
	switchPending                 bool
	queuedTransition              *runtimeinput.PendingWorkWorktreeTransition
	selectedIdentity              worktreeui.SelectionIdentity
	intent                        uiWorktreeOpenIntent
	create                        uiWorktreeCreateDialogState
	deleteConfirm                 uiWorktreeDeleteDialogState
	inputCursor                   uiInputFieldCursor
}

type worktreeListDoneMsg struct {
	token     uint64
	workspace *worktreepb.WorkspaceListSuccess
	err       error
}

type worktreeDeleteTargetResolvedMsg struct {
	generation         uint64
	preview            *worktreepb.DeletePreviewSuccess
	preferDeleteBranch bool
	err                error
}

type worktreeCreateDoneMsg struct {
	workspace      *worktreepb.TransitionWorkspace
	token          uint64
	resp           *worktreepb.CreateSuccess
	err            error
	session        *chatpb.ExistingSessionTarget
	createdSession bool
}

type worktreeSetupEventMsg struct {
	token  uint64
	event  *worktreepb.SetupEvent
	err    error
	events <-chan worktreeSetupEventMsg
}

type worktreeSwitchDoneMsg struct {
	token      uint64
	sessionID  runtimeids.SessionID
	transition runtimeinput.PendingWorkWorktreeTransition
	ack        *worktreepb.ScheduledAcknowledgement
	err        error
}

type worktreeDeleteDoneMsg struct {
	token  uint64
	target string
	resp   *worktreepb.DeleteSuccess
	err    error
}

func newWorktreeCreateDialog(suggestedBranch string) uiWorktreeCreateDialogState {
	dialog := uiWorktreeCreateDialogState{
		baseRef:      newSingleLineEditor(strings.TrimSpace("HEAD")),
		branchTarget: newSingleLineEditor(strings.TrimSpace(suggestedBranch)),
		focus:        uiWorktreeCreateFieldBranchTarget,
		action:       uiWorktreeCreateActionCreate,
	}
	dialog.syncFocus()
	return dialog
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

func (s uiWorktreeOverlayState) isLoading() bool {
	return s.listPending || s.deleteTargetResolutionPending
}

func (m *uiModel) openWorktreeOverlay(intent uiWorktreeOpenIntent) {
	if m == nil {
		return
	}
	m.invalidateWorktreeListRequest()
	m.invalidateWorktreeDeleteTargetResolution()
	m.worktrees.open = true
	m.worktrees.phase = uiWorktreeOverlayPhaseList
	m.worktrees.listPending = true
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
	if m.worktrees.create.setupProgress != nil && m.worktrees.create.setupProgress.cancel != nil {
		m.worktrees.create.setupProgress.cancel()
	}
	m.invalidateWorktreeListRequest()
	m.invalidateWorktreeDeleteTargetResolution()
	m.worktrees = uiWorktreeOverlayState{}
	m.restorePrimaryInputMode()
}

func (m *uiModel) requestWorktreeListCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	m.worktreeListGeneration = nextNonZeroToken(m.worktreeListGeneration)
	token := m.worktreeListGeneration
	m.worktrees.listPending = true
	m.worktrees.errorText = ""
	service := m.worktreeMutationService()
	return func() tea.Msg {
		resp, err := service.ListWorkspace()
		return worktreeListDoneMsg{token: token, workspace: resp, err: err}
	}
}

func (m *uiModel) openCreateWorktreeDialog() tea.Cmd {
	if m == nil {
		return nil
	}
	m.invalidateWorktreeDeleteTargetResolution()
	m.worktrees.phase = uiWorktreeOverlayPhaseCreate
	m.worktrees.errorText = ""
	m.worktrees.create = newWorktreeCreateDialog(m.suggestedWorktreeBranchFromEntries())
	return m.scheduleWorktreeCreateTargetResolution()
}

func (m *uiModel) openDeleteWorktreeDialog(
	target worktreeui.Item,
	preferDeleteBranch bool,
	targetAuthority uiWorktreeDeleteTargetAuthority,
) {
	if m == nil {
		return
	}
	m.invalidateWorktreeDeleteTargetResolution()
	m.worktrees.phase = uiWorktreeOverlayPhaseDeleteConfirm
	m.worktrees.errorText = ""
	m.worktrees.deleteConfirm = uiWorktreeDeleteDialogState{
		target:             target,
		targetAuthority:    targetAuthority,
		preferDeleteBranch: preferDeleteBranch,
	}
	m.worktrees.deleteConfirm.clampSelection()
}

func (m *uiModel) closeWorktreeDialog() {
	if m == nil {
		return
	}
	if m.worktrees.create.setupProgress != nil && m.worktrees.create.setupProgress.cancel != nil {
		m.worktrees.create.setupProgress.cancel()
	}
	m.worktrees.phase = uiWorktreeOverlayPhaseList
	m.worktrees.create = uiWorktreeCreateDialogState{}
	m.worktrees.deleteConfirm = uiWorktreeDeleteDialogState{}
	m.worktrees.errorText = ""
}

func (m *uiModel) applyWorktreeEntries(worktrees []*worktreepb.ListEntry) error {
	entries, err := worktreeui.ProjectItems(worktrees)
	if err != nil {
		m.worktrees.entries = nil
		return err
	}
	m.worktrees.entries = entries
	current := m.currentExecutionTarget()
	for i := range m.worktrees.entries {
		item := &m.worktrees.entries[i]
		item.IsCurrent = item.IsMainWorkspace && current.GetWorktree() == nil
		if current.GetWorktree() != nil && item.WorktreeID != nil {
			item.IsCurrent = *item.WorktreeID == current.Worktree.Id
		}
	}
	if target := m.newChatTarget(); target != nil {
		for i := range m.worktrees.entries {
			item := &m.worktrees.entries[i]
			item.IsCurrent = item.IsMainWorkspace && target.WorktreeSelector == nil
			if target.WorktreeSelector != nil {
				selector, err := worktreeui.StableMutationSelector(*item)
				if err != nil {
					return err
				}
				item.IsCurrent = selector == *target.WorktreeSelector
			}
		}
	}
	if err := m.restoreWorktreeSelection(); err != nil {
		return err
	}
	m.clampWorktreeSelection()
	if m.worktrees.phase == uiWorktreeOverlayPhaseDeleteConfirm {
		targetIdentity, err := worktreeui.SelectionIdentityForItem(m.worktrees.deleteConfirm.target)
		if err != nil {
			return err
		}
		item, idx, ok, err := worktreeui.FindByIdentity(m.worktrees.entries, targetIdentity)
		if err != nil {
			return err
		}
		if ok {
			if m.worktrees.deleteConfirm.targetAuthority == uiWorktreeDeleteTargetAuthorityResolvedSelector {
				target := m.worktrees.deleteConfirm.target
				target.IsCurrent = item.IsCurrent
				m.worktrees.entries[idx] = target
				m.worktrees.deleteConfirm.target = target
			} else {
				m.worktrees.deleteConfirm.target = item
			}
			m.worktrees.deleteConfirm.clampSelection()
			return nil
		}
		if m.worktrees.deleteConfirm.targetAuthority == uiWorktreeDeleteTargetAuthorityResolvedSelector {
			return nil
		}
		m.closeWorktreeDialog()
	}
	return nil
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
	if intent.DeleteTarget.kind == uiWorktreeDeleteIntentTargetSelector {
		return m.worktreeDeleteTargetResolveCmd(intent.DeleteTarget.selector, intent.PreferDeleteBranch)
	}
	var target worktreeui.Item
	var found bool
	if intent.DeleteTarget.kind == uiWorktreeDeleteIntentTargetIdentity {
		var err error
		target, _, found, err = worktreeui.FindByIdentity(m.worktrees.entries, intent.DeleteTarget.identity)
		if err != nil {
			m.worktrees.errorText = err.Error()
			return nil
		}
	} else if intent.DeleteTarget.kind == uiWorktreeDeleteIntentTargetCurrent {
		for _, item := range m.worktrees.entries {
			if item.IsCurrent {
				target, found = item, true
				break
			}
		}
	}
	if !found {
		m.worktrees.errorText = worktreecontract.ErrWorktreeNotFound.Error()
		return nil
	}
	selector, err := worktreeui.StableMutationSelector(target)
	if err != nil {
		m.worktrees.errorText = err.Error()
		return nil
	}
	return m.worktreeDeleteTargetResolveCmd(selector, intent.PreferDeleteBranch)
}

func (m *uiModel) worktreeDeleteTargetResolveCmd(selector string, preferDeleteBranch bool) tea.Cmd {
	if m == nil {
		return nil
	}
	m.deleteTargetResolutionGeneration = nextNonZeroToken(m.deleteTargetResolutionGeneration)
	generation := m.deleteTargetResolutionGeneration
	m.worktrees.deleteTargetResolutionPending = true
	m.worktrees.errorText = ""
	service := m.worktreeMutationService()
	return func() tea.Msg {
		preview, err := service.PreviewDelete(selector)
		return worktreeDeleteTargetResolvedMsg{generation: generation, preview: preview, preferDeleteBranch: preferDeleteBranch, err: err}
	}
}

func (m *uiModel) invalidateWorktreeDeleteTargetResolution() {
	if m == nil {
		return
	}
	m.deleteTargetResolutionGeneration = nextNonZeroToken(m.deleteTargetResolutionGeneration)
	m.worktrees.deleteTargetResolutionPending = false
}

func (m *uiModel) invalidateWorktreeListRequest() {
	if m == nil {
		return
	}
	m.worktreeListGeneration = nextNonZeroToken(m.worktreeListGeneration)
	m.worktrees.listPending = false
}

func (m *uiModel) suggestedWorktreeBranchFromEntries() string {
	if m == nil {
		return ""
	}
	if name := m.suggestedWorktreeSessionName(); name != nil {
		if sessionBranch := worktreecontract.SanitizeBranchSuggestion(*name); sessionBranch != nil {
			return *sessionBranch
		}
	}
	return ""
}

func (m *uiModel) worktreeCreateCmd(req *worktreepb.CreateRequest) tea.Cmd {
	if m == nil {
		return nil
	}
	if m.newSessionTarget() != nil && !m.newSessionConnectionReady() {
		return nil
	}
	var target *chatpb.NewChatTarget
	if m.newChatTarget() != nil {
		target = proto.Clone(m.newChatTarget()).(*chatpb.NewChatTarget)
		m.pendingNewChatActions++
	}
	draft := m.mainEditor.Text()
	createSession := m.createSession
	m.worktrees.mutationToken++
	token := m.worktrees.mutationToken
	if _, err := worktreecontract.ParseSetupOperationID(req.SetupOperationId); err != nil {
		req.SetupOperationId = worktreecontract.NewSetupOperationID().String()
	}
	m.worktrees.create.errorText = ""
	m.worktrees.create.baseRefErrorText = ""
	m.worktrees.create.submitting = true
	m.worktrees.create.setupEvent = nil
	setupCtx, cancel := context.WithCancel(context.Background())
	m.worktrees.create.setupProgress = &uiWorktreeSetupProgressState{cancel: cancel}
	setupReady := make(chan error, 1)
	setupEvents := make(chan worktreeSetupEventMsg, 8)
	service := m.worktreeMutationService()
	worktreeClient := service.Client
	subscribeCmd := func() tea.Msg {
		go subscribeWorktreeSetupEvents(setupCtx, worktreeClient, token, req.SetupOperationId, setupReady, setupEvents)
		return nil
	}
	createCmd := func() tea.Msg {
		result := worktreeCreateDoneMsg{token: token, createdSession: target != nil}
		if err := <-setupReady; err != nil {
			result.err = err
			return result
		}
		if target != nil {
			result.workspace = &worktreepb.TransitionWorkspace{WorkspaceId: service.WorkspaceID, WorkspaceRoot: service.WorkspaceRoot}
			if result.err = protoapi.Validate(result.workspace); result.err != nil {
				return result
			}
			result.session, result.err = createSession(context.Background(), target, draft)
			if result.err != nil {
				return result
			}
			id, err := runtimeids.ParseSessionID(result.session.SessionId)
			if err != nil {
				result.err = err
				return result
			}
			service.SessionTarget = protoapi.SessionSelectorToProto(id)
		}
		result.resp, result.err = service.Create(req)
		return result
	}
	return tea.Batch(subscribeCmd, createCmd, worktreeSetupEventCmd(setupEvents))
}

func subscribeWorktreeSetupEvents(ctx context.Context, worktreeClient apicontract.WorktreeService, token uint64, setupOperationID string, ready chan<- error, events chan worktreeSetupEventMsg) {
	defer close(events)
	if worktreeClient == nil {
		ready <- worktreeui.ErrClientUnavailable
		return
	}
	subscription, err := worktreeClient.SubscribeWorktreeSetup(ctx, &worktreepb.SetupSubscribeRequest{SetupOperationId: setupOperationID})
	if err != nil {
		ready <- err
		return
	}
	defer func() { _ = subscription.Close() }()
	ready <- nil
	for {
		event, err := subscription.Next(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				return
			}
			events <- worktreeSetupEventMsg{token: token, err: err, events: events}
			return
		}
		events <- worktreeSetupEventMsg{token: token, event: event, events: events}
		if event.GetCompleted() != nil || event.GetFailed() != nil {
			return
		}
	}
}

func worktreeSetupEventCmd(events <-chan worktreeSetupEventMsg) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *uiModel) worktreeSwitchCmd(target worktreeui.Item) tea.Cmd {
	if m == nil {
		return nil
	}
	if m.newSessionTarget() != nil && target.IsMainWorkspace {
		return m.worktreeTransitionCommand(runtimeinput.PendingWorkWorktreeTransition{Transition: runtimeinput.PendingWorkWorktreeTransitionLeave})
	}
	selector, err := worktreeui.StableMutationSelector(target)
	if err != nil {
		m.worktrees.errorText = runtimeattach.FormatSubmissionError(err)
		return nil
	}
	if m.worktrees.switchPending {
		queued := runtimeinput.PendingWorkWorktreeTransition{
			Transition: runtimeinput.PendingWorkWorktreeTransitionEnter,
			Selector:   &selector,
		}
		m.worktrees.queuedTransition = &queued
		return nil
	}
	m.invalidateWorktreeDeleteTargetResolution()
	m.worktrees.errorText = ""
	return m.worktreeSwitchCommandForTarget(selector)
}

func (m *uiModel) takeQueuedWorktreeTransitionCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	queued := m.worktrees.queuedTransition
	m.worktrees.queuedTransition = nil
	if queued == nil {
		return nil
	}
	m.worktrees.switchPending = false
	return m.worktreeTransitionCommand(*queued)
}

func (m *uiModel) worktreeDeleteCmd(target worktreeui.Item, deleteBranch bool) tea.Cmd {
	if m == nil {
		return nil
	}
	m.worktrees.mutationToken++
	token := m.worktrees.mutationToken
	m.worktrees.deleteConfirm.errorText = ""
	m.worktrees.deleteConfirm.submitting = true
	service := m.worktreeMutationService()
	selector, selectorErr := worktreeui.StableMutationSelector(target)
	cleanupPolicy := worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_AUTO_IF_KENT_CREATED
	if deleteBranch {
		cleanupPolicy = worktreepb.BranchCleanupMode_WORKTREE_BRANCH_CLEANUP_MODE_DELETE_SAFE
	}
	return func() tea.Msg {
		if selectorErr != nil {
			return worktreeDeleteDoneMsg{token: token, target: worktreeui.DisplayName(target), err: selectorErr}
		}
		resp, err := service.Delete(selector, m.worktrees.deleteConfirm.forceFolderRemoval, cleanupPolicy)
		return worktreeDeleteDoneMsg{token: token, target: worktreeui.DisplayName(target), resp: resp, err: err}
	}
}

type uiWorkspaceContext struct {
	projectID   string
	workspaceID string
	root        string
}

func (m *uiModel) worktreeMutationService() worktreeui.Service {
	if m == nil {
		return worktreeui.Service{}
	}
	service := worktreeui.Service{
		Client:        m.worktreeClient,
		ProjectID:     m.workspaceContext.projectID,
		WorkspaceID:   m.workspaceContext.workspaceID,
		WorkspaceRoot: m.workspaceContext.root,
	}
	if m.newSessionTarget() == nil {
		service.SessionTarget = &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: m.currentRuntimeSessionID()}}
	}
	return service
}
