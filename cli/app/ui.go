package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"builder/cli/app/commands"
	"builder/cli/tui"
	"builder/server/processview"
	"builder/server/session"
	shelltool "builder/server/tools/shell"
	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/serverapi"
	"builder/shared/theme"
	"builder/shared/transcriptdiag"

	tea "github.com/charmbracelet/bubbletea"
)

type submitDoneMsg struct {
	token         uint64
	message       string
	submittedText string
	silentFinal   bool
	err           error
}

func newSubmitDoneMsg(token uint64, message string, submittedText string, err error) submitDoneMsg {
	return submitDoneMsg{
		token:         token,
		message:       message,
		submittedText: submittedText,
		silentFinal:   isNoopFinalText(message),
		err:           err,
	}
}

type promptHistoryPersistErrMsg struct {
	err error
}

type compactDoneMsg struct {
	err error
}

type activeSubmitState struct {
	token    uint64
	stepID   string
	text     string
	queuedID string
	flushed  bool
}

type spinnerTickMsg struct {
	token uint64
	at    time.Time
}

type processListRefreshTickMsg struct{}

type openProcessLogsDoneMsg struct {
	err error
}

type clearTransientStatusMsg struct {
	token uint64
}

type startupUpdateNoticeMsg struct {
	version string
}

type nativeResizeReplayMsg struct {
	token uint64
}

type nativeHistoryFlushMsg struct {
	Text       string
	AllowBlank bool
	Sequence   uint64
}

type runtimeEventMsg struct {
	event clientui.Event
}

type runtimeEventBatchMsg struct {
	events []clientui.Event
	carry  *clientui.Event
}

type uiModelProbeMessage interface {
	probeUIModel(*uiModel)
}

type runtimeConnectionStateChangedMsg struct {
	err error
}

type runtimeLeaseRecoveryWarningMsg struct {
	text       string
	visibility clientui.EntryVisibility
}

type runtimeMainViewRefreshedMsg struct {
	token uint64
	view  clientui.RuntimeMainView
	err   error
}

type runtimeTranscriptRefreshedMsg struct {
	token         uint64
	req           clientui.TranscriptPageRequest
	syncCause     runtimeTranscriptSyncCause
	transcript    clientui.TranscriptPage
	recoveryCause clientui.TranscriptRecoveryCause
	err           error
}

type runtimeCommittedTranscriptSuffixRefreshedMsg struct {
	token  uint64
	req    clientui.CommittedTranscriptSuffixRequest
	suffix clientui.CommittedTranscriptSuffix
	err    error
}

type nativeResizeTranscriptSuffixRefreshedMsg struct {
	token  uint64
	suffix clientui.CommittedTranscriptSuffix
	err    error
}

type runtimeTranscriptRetryMsg struct {
	syncCause     runtimeTranscriptSyncCause
	token         uint64
	recoveryCause clientui.TranscriptRecoveryCause
}

type runtimeTranscriptSyncCause string

const (
	runtimeTranscriptSyncCauseBootstrap               runtimeTranscriptSyncCause = "bootstrap"
	runtimeTranscriptSyncCauseCommittedConversation   runtimeTranscriptSyncCause = "committed_conversation_updated"
	runtimeTranscriptSyncCauseCommittedGap            runtimeTranscriptSyncCause = "committed_gap"
	runtimeTranscriptSyncCauseQueuedDrain             runtimeTranscriptSyncCause = "queued_drain"
	runtimeTranscriptSyncCauseDirtyFollowUp           runtimeTranscriptSyncCause = "dirty_follow_up"
	runtimeTranscriptSyncCauseContinuityRecovery      runtimeTranscriptSyncCause = "continuity_recovery"
	runtimeTranscriptSyncCauseManualTranscriptRefresh runtimeTranscriptSyncCause = "manual_transcript_refresh"
)

type detailTranscriptLoadMsg struct{}

type renderDiagnosticMsg struct {
	diagnostic tui.RenderDiagnostic
}

type deferredProjectedTranscriptTail struct {
	rangeStart int
	rangeEnd   int
	revision   int64
	entries    []clientui.ChatEntry
	pending    []string
}

type nativeHistoryReplayPermit uint8

const (
	nativeHistoryReplayPermitNone nativeHistoryReplayPermit = iota
	nativeHistoryReplayPermitContinuityRecovery
	nativeHistoryReplayPermitAuthoritativeHydrate
	nativeHistoryReplayPermitModeRestore
)

type runLoggerDiagnosticMsg struct {
	diagnostic runLoggerDiagnostic
}

type clipboardImagePasteDoneMsg struct {
	Target         uiClipboardPasteTarget
	MainDraftToken uint64
	AskToken       uint64
	Path           string
	Err            error
}

type clipboardTextCopyDoneMsg struct {
	Err error
}

type askEvent struct {
	req              clientui.PendingPromptEvent
	reply            chan askReply
	cancel           func()
	resolvedPromptID string
}

func (e askEvent) promptID() string {
	if strings.TrimSpace(e.resolvedPromptID) != "" {
		return strings.TrimSpace(e.resolvedPromptID)
	}
	return strings.TrimSpace(e.req.PromptID)
}

func (e askEvent) isResolution() bool {
	return strings.TrimSpace(e.resolvedPromptID) != ""
}

func (e askEvent) cancelPending() {
	if e.cancel != nil {
		e.cancel()
	}
}

type askReply struct {
	response clientui.PromptAnswer
	err      error
}

type askEventMsg struct {
	event askEvent
}

type uiStatusNoticeKind uint8

const (
	uiStatusNoticeNeutral uiStatusNoticeKind = iota
	uiStatusNoticeSuccess
	uiStatusNoticeError
	uiStatusNoticeUpdateAvailable
)

type uiStatusNotice struct {
	Text     string
	Kind     uiStatusNoticeKind
	Duration time.Duration
}

type uiStatusNoticeDelivery uint8

const (
	uiStatusNoticeReplace uiStatusNoticeDelivery = iota
	uiStatusNoticeQueue
)

type uiLogger interface {
	Logf(format string, args ...any)
}

type UIOption func(*uiModel)

type UIAction string

type UITranscriptEntry struct {
	Role string
	Text string
}

type UITransition struct {
	Action                   UIAction
	InitialPrompt            string
	InitialInput             string
	TargetSessionID          string
	ForkUserMessageIndex     int
	ForkTranscriptEntryIndex int
	ParentSessionID          string
}

const (
	UIActionNone         UIAction = "none"
	UIActionExit         UIAction = "exit"
	UIActionNewSession   UIAction = "new_session"
	UIActionResume       UIAction = "resume"
	UIActionLogout       UIAction = "logout"
	UIActionForkRollback UIAction = "fork_rollback"
	UIActionOpenSession  UIAction = "open_session"
)

var nativeResizeReplayDebounce = time.Second
var nativeResizeReplayNow = time.Now

func WithUILogger(logger uiLogger) UIOption {
	return func(m *uiModel) {
		m.logger = logger
		if logger != nil {
			if configurable, ok := m.engine.(interface{ SetTranscriptDiagnosticLogger(func(string)) }); ok {
				configurable.SetTranscriptDiagnosticLogger(func(line string) {
					logger.Logf("%s", strings.TrimSpace(line))
				})
			}
		}
	}
}

func WithUITranscriptDiagnostics(enabled bool) UIOption {
	return func(m *uiModel) {
		m.transcriptDiagnostics = enabled
		m.updateTranscriptDiagnosticsMode()
	}
}

func WithUIDebug(enabled bool) UIOption {
	return func(m *uiModel) {
		m.debugMode = enabled
		m.updateTranscriptDiagnosticsMode()
	}
}

func WithUITerminalCursorState(state *uiTerminalCursorState) UIOption {
	return func(m *uiModel) {
		m.terminalCursor = state
	}
}

func WithUIModelName(model string) UIOption {
	return func(m *uiModel) {
		m.modelName = strings.TrimSpace(model)
	}
}

func WithUIConfiguredModelName(model string) UIOption {
	return func(m *uiModel) {
		m.configuredModelName = strings.TrimSpace(model)
	}
}

func WithUIThinkingLevel(thinkingLevel string) UIOption {
	return func(m *uiModel) {
		m.thinkingLevel = strings.TrimSpace(thinkingLevel)
	}
}

func WithUIFastModeAvailable(available bool) UIOption {
	return func(m *uiModel) {
		m.fastModeAvailable = available
	}
}

func WithUIFastModeEnabled(enabled bool) UIOption {
	return func(m *uiModel) {
		m.fastModeEnabled = enabled
	}
}

func WithUIConversationFreshness(freshness session.ConversationFreshness) UIOption {
	return func(m *uiModel) {
		m.conversationFreshness = mapConversationFreshness(freshness)
	}
}

func WithUIModelContractLocked(locked bool) UIOption {
	return func(m *uiModel) {
		m.modelContractLocked = locked
	}
}

func WithUITheme(theme string) UIOption {
	return func(m *uiModel) {
		m.theme = strings.TrimSpace(theme)
		m.view = tui.NewModel(
			tui.WithTheme(theme),
			tui.WithCompactDetail(),
			tui.WithRenderDiagnosticHandler(m.handleRenderDiagnostic),
		)
	}
}

func WithUIInitialTranscript(entries []UITranscriptEntry) UIOption {
	return func(m *uiModel) {
		m.initialTranscript = append([]UITranscriptEntry(nil), entries...)
	}
}

func WithUICommandRegistry(registry *commands.Registry) UIOption {
	return func(m *uiModel) {
		if registry == nil {
			return
		}
		m.commandRegistry = registry
	}
}

func WithUIHasOtherSessions(known bool, available bool) UIOption {
	return func(m *uiModel) {
		m.hasOtherSessionsKnown = known
		m.hasOtherSessions = available
	}
}

func WithUIStartupSubmit(text string) UIOption {
	return func(m *uiModel) {
		m.startupSubmit = text
	}
}

func WithUIInitialInput(text string) UIOption {
	return func(m *uiModel) {
		if text == "" || m.input != "" {
			return
		}
		m.replaceMainInput(text, -1)
	}
}

func WithUISessionName(name string) UIOption {
	return func(m *uiModel) {
		m.sessionName = strings.TrimSpace(name)
	}
}

func WithUISessionID(sessionID string) UIOption {
	return func(m *uiModel) {
		m.sessionID = strings.TrimSpace(sessionID)
	}
}

func WithUIBackgroundManager(manager *shelltool.Manager) UIOption {
	return func(m *uiModel) {
		if manager == nil || m.processClientExplicit {
			return
		}
		processes := processview.NewService(manager)
		m.processClient = newUIProcessClientWithReads(
			client.NewLoopbackProcessViewClient(processes),
			client.NewLoopbackProcessControlClient(processes),
		)
	}
}

func WithUIProcessClient(client clientui.ProcessClient) UIOption {
	return func(m *uiModel) {
		m.processClient = client
		m.processClientExplicit = true
	}
}

func WithUIWorktreeClient(client client.WorktreeClient) UIOption {
	return func(m *uiModel) {
		m.worktreeClient = client
	}
}

func WithUITurnQueueHook(hook turnQueueHook) UIOption {
	return func(m *uiModel) {
		m.turnQueueHook = hook
	}
}

func WithUITerminalFocusState(state *terminalFocusState) UIOption {
	return func(m *uiModel) {
		if state != nil {
			m.terminalFocus = state
		}
	}
}

func WithUIPromptHistory(history []string) UIOption {
	return func(m *uiModel) {
		m.loadPromptHistory(history)
	}
}

func WithUIStartupUpdateNotice(enabled bool) UIOption {
	return func(m *uiModel) {
		m.startupUpdateNotice = enabled
	}
}

func WithUIClipboardImagePaster(paster uiClipboardImagePaster) UIOption {
	return func(m *uiModel) {
		m.clipboardImagePaster = paster
	}
}

func WithUIClipboardTextCopier(copier uiClipboardTextCopier) UIOption {
	return func(m *uiModel) {
		m.clipboardTextCopier = copier
	}
}

func (m *uiModel) isInputLocked() bool {
	return m.inputSubmitLocked
}

func (m *uiModel) clearReviewerState() {
	m.reviewerRunning = false
	m.reviewerBlocking = false
}

func (m *uiModel) invalidateNativeResizeReplay() {
	m.nativeResizeReplayToken++
}

type rollbackCandidate struct {
	TranscriptIndex int
	Text            string
}

func newUIModelDefaults(runtimeClient clientui.RuntimeClient, runtimeEvents <-chan clientui.Event, askEvents <-chan askEvent) *uiModel {
	return &uiModel{
		uiRuntimeFeatureState:           newUIRuntimeFeatureState(runtimeClient, runtimeEvents, askEvents),
		uiInputFeatureState:             newUIInputFeatureState(),
		uiPresentationFeatureState:      newUIPresentationFeatureState(),
		uiConversationFeatureState:      newUIConversationFeatureState(),
		uiSessionTransitionFeatureState: newUISessionTransitionFeatureState(),
		uiStatusFeatureState:            newUIStatusFeatureState(),
		uiRollbackFeatureState:          newUIRollbackFeatureState(),
	}
}

func newUIRuntimeFeatureState(runtimeClient clientui.RuntimeClient, runtimeEvents <-chan clientui.Event, askEvents <-chan askEvent) uiRuntimeFeatureState {
	return uiRuntimeFeatureState{
		engine:        runtimeClient,
		view:          tui.NewModel(tui.WithCompactDetail()),
		runtimeEvents: runtimeEvents,
		askEvents:     askEvents,
	}
}

func newUIInputFeatureState() uiInputFeatureState {
	return uiInputFeatureState{
		activity:                 uiActivityIdle,
		inputCursor:              -1,
		mainInputDraftToken:      1,
		promptHistorySelection:   -1,
		promptHistoryDraftCursor: -1,
		commandRegistry:          commands.NewDefaultRegistry(),
		reviewerMode:             "off",
		autoCompactionEnabled:    true,
		conversationFreshness:    clientui.ConversationFreshnessFresh,
	}
}

func newUIPresentationFeatureState() uiPresentationFeatureState {
	return uiPresentationFeatureState{
		theme:         theme.Auto,
		terminalFocus: newTerminalFocusState(),
	}
}

func newUIConversationFeatureState() uiConversationFeatureState {
	return uiConversationFeatureState{
		interaction: uiInteractionState{Mode: uiInputModeMain},
		ask:         uiAskState{inputCursor: -1},
	}
}

func newUISessionTransitionFeatureState() uiSessionTransitionFeatureState {
	return uiSessionTransitionFeatureState{
		exitAction:                   UIActionNone,
		nextForkTranscriptEntryIndex: -1,
	}
}

func newUIStatusFeatureState() uiStatusFeatureState {
	return uiStatusFeatureState{
		statusRepository:      newMemoryUIStatusRepository(),
		clipboardImagePaster:  newSystemClipboardImagePaster(),
		clipboardTextCopier:   newSystemClipboardTextCopier(),
		debugKeys:             envFlagEnabled("BUILDER_DEBUG_KEYS"),
		debugMode:             envFlagEnabled("BUILDER_DEBUG"),
		transcriptDiagnostics: envFlagEnabled("BUILDER_TRANSCRIPT_DIAGNOSTICS"),
	}
}

func newUIRollbackFeatureState() uiRollbackFeatureState {
	return uiRollbackFeatureState{
		rollback: uiRollbackState{phase: uiRollbackPhaseInactive, selectedTranscriptEntry: -1},
	}
}

func NewProjectedUIModel(runtimeClient clientui.RuntimeClient, runtimeEvents <-chan clientui.Event, askEvents <-chan askEvent, opts ...UIOption) tea.Model {
	m := newUIModelDefaults(runtimeClient, runtimeEvents, askEvents)
	for _, opt := range opts {
		opt(m)
	}
	if m.pathReferenceSearch == nil {
		m.pathReferenceSearch = newUIPathReferenceSearch()
		m.pathReferenceEvents = m.pathReferenceSearch.Events()
	}
	m.updateTranscriptDiagnosticsMode()
	m.refreshAutocompleteFromInput()
	if configurable, ok := m.engine.(interface{ SetConnectionStateObserver(func(error)) }); ok {
		runtimeConnectionEvents := make(chan runtimeConnectionStateChangedMsg, 1)
		m.runtimeConnectionEvents = runtimeConnectionEvents
		configurable.SetConnectionStateObserver(func(err error) {
			enqueueRuntimeConnectionStateChange(runtimeConnectionEvents, err)
		})
	}
	if configurable, ok := m.engine.(interface {
		SetLeaseRecoveryWarningObserver(func(string, clientui.EntryVisibility))
	}); ok {
		runtimeLeaseRecoveryWarning := make(chan runtimeLeaseRecoveryWarningMsg, 1)
		m.runtimeLeaseRecoveryWarning = runtimeLeaseRecoveryWarning
		configurable.SetLeaseRecoveryWarningObserver(func(text string, visibility clientui.EntryVisibility) {
			enqueueRuntimeLeaseRecoveryWarning(runtimeLeaseRecoveryWarning, text, visibility)
		})
	}
	mainView := m.runtimeMainView()
	status := mainView.Status
	m.applyRuntimeMainViewState(mainView)
	m.refreshAuthSlashCommandState()
	if !m.hasRuntimeClient() {
		m.reviewerEnabled = strings.TrimSpace(m.reviewerMode) != "" && strings.TrimSpace(m.reviewerMode) != "off"
	}
	m.refreshProcessEntries()
	var startupNativeHistoryCmd tea.Cmd
	if m.hasRuntimeClient() {
		seedView := mainView.Session
		_ = m.runtimeAdapter().applyProjectedSessionMetadata(seedView)
		_ = m.runtimeAdapter().applyProjectedTranscriptPage(m.startupRuntimeTranscript())
		startupNativeHistoryCmd = m.requestRuntimeBootstrapTranscriptSync()
		m.runtimeTranscriptBusy = false
	} else {
		for _, entry := range m.initialTranscript {
			if strings.TrimSpace(entry.Text) == "" {
				continue
			}
			role := tui.TranscriptRoleFromWire(entry.Role)
			m.transcriptEntries = append(m.transcriptEntries, tui.TranscriptEntry{Role: role, Text: entry.Text})
			m.forwardToView(tui.AppendTranscriptMsg{Role: role, Text: entry.Text})
		}
		m.transcriptBaseOffset = 0
		m.transcriptTotalEntries = len(m.transcriptEntries)
		m.seedPromptHistoryFromTranscriptEntries(m.transcriptEntries)
		m.refreshRollbackCandidates()
		startupNativeHistoryCmd = m.syncNativeHistoryFromTranscript()
	}
	if startupNativeHistoryCmd != nil {
		m.startupCmds = append(m.startupCmds, startupNativeHistoryCmd)
	}
	if gitStartupCmd := m.statusLineGitStartupCmd(); gitStartupCmd != nil {
		m.statusGitBackgroundInFlight = true
		m.startupCmds = append(m.startupCmds, gitStartupCmd)
	}
	if m.pathReferenceSearch != nil && strings.TrimSpace(m.statusConfig.WorkspaceRoot) != "" {
		m.startupCmds = append(m.startupCmds, func() tea.Msg {
			m.pathReferenceSearch.StartPrewarm(strings.TrimSpace(m.statusConfig.WorkspaceRoot))
			return nil
		})
	}
	if m.startupUpdateNotice && m.hasRuntimeClient() {
		m.startupCmds = append(m.startupCmds, m.startupUpdateNoticeCmd(status.Update))
	}
	m.syncViewport()
	return m
}

func (m *uiModel) handleRenderDiagnostic(diag tui.RenderDiagnostic) {
	m.startupCmds = append(m.startupCmds, func() tea.Msg {
		return renderDiagnosticMsg{diagnostic: diag}
	})
}

func (m *uiModel) handleRunLoggerDiagnostic(diag runLoggerDiagnostic) {
	m.startupCmds = append(m.startupCmds, func() tea.Msg {
		return runLoggerDiagnosticMsg{diagnostic: diag}
	})
}

func (m *uiModel) applyRenderDiagnostic(diag tui.RenderDiagnostic) tea.Cmd {
	message := strings.TrimSpace(diag.Message)
	if message == "" {
		return nil
	}
	severity := strings.TrimSpace(string(diag.Severity))
	if severity == "" {
		severity = string(tui.RenderDiagnosticSeverityWarn)
	}
	m.logf("render.diagnostic severity=%s component=%s message=%q", severity, strings.TrimSpace(diag.Component), message)
	if diag.Err != nil {
		m.logf("render.diagnostic.err component=%s err=%q", strings.TrimSpace(diag.Component), diag.Err.Error())
	}
	kind := uiStatusNoticeNeutral
	switch diag.Severity {
	case tui.RenderDiagnosticSeverityError, tui.RenderDiagnosticSeverityFatal:
		kind = uiStatusNoticeError
	default:
		kind = uiStatusNoticeNeutral
	}
	return m.setTransientStatusWithKind(message, kind)
}

func (m *uiModel) applyRunLoggerDiagnostic(diag runLoggerDiagnostic) tea.Cmd {
	message := strings.TrimSpace(diag.Message)
	if message == "" {
		message = "run logger diagnostic"
	}
	m.logf("run_logger.diagnostic kind=%s message=%q", strings.TrimSpace(diag.Kind), message)
	if diag.Err != nil {
		m.logf("run_logger.diagnostic.err kind=%s err=%q", strings.TrimSpace(diag.Kind), diag.Err.Error())
	}
	return m.setTransientStatusWithKind(message, uiStatusNoticeError)
}

func (m *uiModel) handleRuntimeEventBatch(events []clientui.Event) (*uiModel, tea.Cmd) {
	flushSequenceBefore := m.nativeFlushSequence
	m.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.runtime_batch", map[string]string{
		"session_id":             strings.TrimSpace(m.sessionID),
		"mode":                   string(m.view.Mode()),
		"event_count":            strconv.Itoa(len(events)),
		"pending_runtime_events": strconv.Itoa(len(m.pendingRuntimeEvents)),
		"wait_after_flush":       strconv.FormatUint(m.waitRuntimeEventAfterFlushSequence, 10),
		"wait_after_hydration":   strconv.FormatBool(m.waitRuntimeEventAfterHydration),
	}))
	result := m.runtimeAdapter().applyProjectedRuntimeEventsBatch(events)
	cmd := result.cmd
	cmd = tea.Batch(cmd, m.rearmSpinnerTicking())
	if !result.awaitsHydration {
		cmd = sequenceCmds(cmd, m.flushQueuedInputsAfterHydration())
		cmd = sequenceCmds(cmd, m.inputController().resumeQueuedInputsAfterIdleRuntime())
	}
	m.syncViewport()
	if !result.transcriptMutated {
		cmd = sequenceCmds(cmd, m.syncNativeStreamingScrollback())
	}
	if result.awaitsHydration {
		m.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.runtime_batch_pause", map[string]string{
			"session_id":             strings.TrimSpace(m.sessionID),
			"mode":                   string(m.view.Mode()),
			"pending_runtime_events": strconv.Itoa(len(m.pendingRuntimeEvents)),
			"native_flush_sequence":  strconv.FormatUint(m.nativeFlushSequence, 10),
		}))
		m.waitRuntimeEventAfterHydration = true
	}
	if m.nativeFlushSequence != flushSequenceBefore {
		m.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.runtime_batch_wait_flush", map[string]string{
			"session_id":             strings.TrimSpace(m.sessionID),
			"mode":                   string(m.view.Mode()),
			"pending_runtime_events": strconv.Itoa(len(m.pendingRuntimeEvents)),
			"native_flush_sequence":  strconv.FormatUint(m.nativeFlushSequence, 10),
		}))
		m.waitRuntimeEventAfterFlushSequence = m.nativeFlushSequence
		if result.awaitsHydration {
			return m, cmd
		}
		return m, cmd
	}
	if result.awaitsHydration {
		return m, cmd
	}
	return m, tea.Batch(m.waitRuntimeEventCmd(), cmd)
}

func (m *uiModel) waitRuntimeEventCmd() tea.Cmd {
	if m == nil {
		return nil
	}
	if m.waitRuntimeEventAfterFlushSequence != 0 || m.waitRuntimeEventAfterHydration {
		m.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.wait_runtime_event_blocked", map[string]string{
			"session_id":             strings.TrimSpace(m.sessionID),
			"mode":                   string(m.view.Mode()),
			"pending_runtime_events": strconv.Itoa(len(m.pendingRuntimeEvents)),
			"wait_after_flush":       strconv.FormatUint(m.waitRuntimeEventAfterFlushSequence, 10),
			"wait_after_hydration":   strconv.FormatBool(m.waitRuntimeEventAfterHydration),
		}))
		return nil
	}
	if len(m.pendingRuntimeEvents) == 0 {
		return waitRuntimeEvent(m.runtimeEvents)
	}
	evt := m.pendingRuntimeEvents[0]
	m.pendingRuntimeEvents = append([]clientui.Event(nil), m.pendingRuntimeEvents[1:]...)
	m.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.wait_runtime_event_resume_pending", map[string]string{
		"session_id":             strings.TrimSpace(m.sessionID),
		"mode":                   string(m.view.Mode()),
		"kind":                   string(evt.Kind),
		"pending_runtime_events": strconv.Itoa(len(m.pendingRuntimeEvents)),
	}))
	return func() tea.Msg {
		return runtimeEventBatchMsg{events: []clientui.Event{evt}}
	}
}

func (m *uiModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.waitRuntimeEventCmd(),
		waitAskEvent(m.askEvents),
		waitPathReferenceSearchEvent(m.pathReferenceEvents),
		tea.SetWindowTitle(m.windowTitle()),
		tea.WindowSize(),
	}
	if m.runtimeConnectionEvents != nil {
		cmds = append(cmds, waitRuntimeConnectionStateChange(m.runtimeConnectionEvents))
	}
	if m.runtimeLeaseRecoveryWarning != nil {
		cmds = append(cmds, waitRuntimeLeaseRecoveryWarning(m.runtimeLeaseRecoveryWarning))
	}
	cmds = append([]tea.Cmd{tea.ClearScreen}, cmds...)
	if startupText := strings.TrimSpace(m.startupSubmit); startupText != "" {
		cmds = append(cmds, m.inputController().startSubmissionWithPromptHistory(startupText))
	}
	if len(m.startupCmds) > 0 {
		cmds = append(cmds, m.startupCmds...)
		m.startupCmds = nil
	}
	return tea.Batch(cmds...)
}

func (m *uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if probe, ok := msg.(uiModelProbeMessage); ok {
		probe.probeUIModel(m)
		return m, nil
	}
	switch msg.(type) {
	case tea.FocusMsg:
		m.terminalFocus.MarkFocused()
		return m, nil
	case tea.BlurMsg:
		m.terminalFocus.MarkBlurred()
		return m, nil
	}
	if result := m.reduceFeatureMessage(msg); result.handled {
		return result.bubbleTea()
	}

	if _, ok := msg.(tea.MouseMsg); ok && m.rollback.isActive() {
		m.syncViewport()
		return m, nil
	}
	m.forwardToView(msg)
	m.syncViewport()
	return m, m.maybeRequestDetailTranscriptPage()
}

func (m *uiModel) TerminalFocused() bool {
	if m == nil {
		return false
	}
	return m.terminalFocus.FocusedForAttention()
}

func (m *uiModel) TerminalFocusKnown() bool {
	if m == nil {
		return false
	}
	return m.terminalFocus.Known()
}

func (m *uiModel) setDebugKeyTransientStatus(raw tea.Msg, normalized tea.KeyMsg, source string) {
	rawString := ""
	if stringer, ok := raw.(fmt.Stringer); ok {
		rawString = stringer.String()
	}
	m.transientStatusToken++
	m.transientStatus = fmt.Sprintf("key src=%s raw=%q norm=%q type=%d", source, rawString, normalized.String(), normalized.Type)
	m.transientStatusKind = uiStatusNoticeNeutral
}

func envFlagEnabled(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func statusHasAuthData(snapshot uiStatusSnapshot) bool {
	return snapshot.Auth.Visible || snapshot.Subscription.Applicable || strings.TrimSpace(snapshot.Subscription.Summary) != "" || len(snapshot.Subscription.Windows) > 0
}

func (m *uiModel) forwardToView(msg tea.Msg) {
	prevMode := m.view.Mode()
	next, _ := m.view.Update(msg)
	casted, ok := next.(tui.Model)
	if ok {
		m.view = casted
	}
	if prevMode != m.view.Mode() && m.surface().isTranscript() {
		m.activeSurface = surfaceForTranscriptMode(m.view.Mode())
	}
	if prevMode != m.view.Mode() && m.view.Mode() == tui.ModeDetail && m.hasRuntimeClient() {
		m.primeDetailTranscriptFromCurrentTail()
		page := m.detailTranscript.page()
		nextDetail, _ := m.view.Update(tui.SetConversationMsg{
			BaseOffset:   page.Offset,
			TotalEntries: page.TotalEntries,
			Entries:      transcriptEntriesFromPage(page),
			Ongoing:      page.Ongoing,
			OngoingError: page.OngoingError,
		})
		if castedDetail, ok := nextDetail.(tui.Model); ok {
			m.view = castedDetail
		}
	}
}

func (m *uiModel) Action() UIAction {
	return m.exitAction
}

func (m *uiModel) Close() {
	if m == nil || m.pathReferenceSearch == nil {
		return
	}
	m.pathReferenceSearch.Stop()
	m.pathReferenceSearch = nil
	m.pathReferenceEvents = nil
}

func (m *uiModel) Transition() UITransition {
	return UITransition{
		Action:                   m.exitAction,
		InitialPrompt:            m.nextSessionInitialPrompt,
		InitialInput:             m.nextSessionInitialInput,
		TargetSessionID:          strings.TrimSpace(m.nextSessionID),
		ForkUserMessageIndex:     m.nextForkUserMessageIndex,
		ForkTranscriptEntryIndex: m.nextForkTranscriptEntryIndex,
		ParentSessionID:          strings.TrimSpace(m.nextParentSessionID),
	}
}

func (m *uiModel) windowTitle() string {
	return sessionTitle(m.sessionName)
}

func (m *uiModel) logf(format string, args ...any) {
	if m.logger != nil {
		m.logger.Logf(format, args...)
	}
}

func (m *uiModel) logTranscriptDiag(line string) {
	if m == nil || !m.transcriptDiagnosticsEnabled() {
		return
	}
	m.logf("%s", strings.TrimSpace(line))
}

func (m *uiModel) transcriptDiagnosticsEnabled() bool {
	if m == nil {
		return false
	}
	return m.transcriptDiagnostics || transcriptdiag.EnabledForProcess(m.debugMode)
}

func (m *uiModel) updateTranscriptDiagnosticsMode() {
	if m == nil {
		return
	}
	if configurable, ok := m.engine.(interface{ SetTranscriptDiagnosticsEnabled(bool) }); ok {
		configurable.SetTranscriptDiagnosticsEnabled(m.transcriptDiagnosticsEnabled())
	}
}

func (m *uiModel) inputController() uiInputController {
	return uiInputController{model: m}
}

func worktreeDeleteSuccessStatus(resp serverapi.WorktreeDeleteResponse) string {
	status := "Deleted worktree " + worktreeDisplayName(resp.Worktree)
	if cleanup := strings.TrimSpace(resp.BranchCleanupMessage); cleanup != "" {
		status += ". " + cleanup
	}
	return status
}

func (m *uiModel) askController() uiAskController {
	return uiAskController{model: m}
}

func (m *uiModel) runtimeAdapter() uiRuntimeAdapter {
	return uiRuntimeAdapter{model: m}
}

func (m *uiModel) setTransientStatus(message string) tea.Cmd {
	return m.setTransientStatusWithKind(message, uiStatusNoticeNeutral)
}

func (m *uiModel) setTransientStatusWithKind(message string, kind uiStatusNoticeKind) tea.Cmd {
	return m.sendTransientStatus(message, kind, transientStatusDuration, uiStatusNoticeReplace)
}

func (m *uiModel) enqueueTransientStatus(message string, kind uiStatusNoticeKind) tea.Cmd {
	return m.sendTransientStatus(message, kind, transientStatusDuration, uiStatusNoticeQueue)
}

func (m *uiModel) enqueueTransientStatusWithDuration(message string, kind uiStatusNoticeKind, duration time.Duration) tea.Cmd {
	return m.sendTransientStatus(message, kind, duration, uiStatusNoticeQueue)
}

func (m *uiModel) sendTransientStatus(message string, kind uiStatusNoticeKind, duration time.Duration, delivery uiStatusNoticeDelivery) tea.Cmd {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	notice := uiStatusNotice{Text: strings.TrimSpace(message), Kind: kind, Duration: duration}
	if delivery == uiStatusNoticeQueue && strings.TrimSpace(m.transientStatus) != "" {
		if m.transientStatus == notice.Text && m.transientStatusKind == notice.Kind {
			return nil
		}
		if len(m.transientStatusQueue) > 0 {
			last := m.transientStatusQueue[len(m.transientStatusQueue)-1]
			if last == notice {
				return nil
			}
		}
		m.transientStatusQueue = append(m.transientStatusQueue, notice)
		return nil
	}
	return m.showTransientStatusNotice(notice)
}

func (m *uiModel) showTransientStatusNotice(notice uiStatusNotice) tea.Cmd {
	m.transientStatusToken++
	token := m.transientStatusToken
	m.transientStatus = strings.TrimSpace(notice.Text)
	m.transientStatusKind = notice.Kind
	if notice.Kind == uiStatusNoticeUpdateAvailable {
		m.startupUpdateShown = true
	}
	return scheduleTransientStatusClear(notice.Duration, token)
}

func (m *uiModel) advanceTransientStatusQueue() tea.Cmd {
	m.transientStatus = ""
	m.transientStatusKind = uiStatusNoticeNeutral
	if len(m.transientStatusQueue) == 0 {
		m.syncViewport()
		return nil
	}
	next := m.transientStatusQueue[0]
	m.transientStatusQueue = append([]uiStatusNotice(nil), m.transientStatusQueue[1:]...)
	cmd := m.showTransientStatusNotice(next)
	m.syncViewport()
	return cmd
}

func (m *uiModel) startupUpdateNoticeCmd(status clientui.UpdateStatus) tea.Cmd {
	if status.Available && strings.TrimSpace(status.LatestVersion) != "" {
		return func() tea.Msg {
			return startupUpdateNoticeMsg{version: status.LatestVersion}
		}
	}
	if status.Checked {
		return nil
	}
	client := m.runtimeClient()
	if client == nil {
		return nil
	}
	return func() tea.Msg {
		refreshed, err := client.RefreshMainView()
		if err != nil || !refreshed.Status.Update.Available || strings.TrimSpace(refreshed.Status.Update.LatestVersion) == "" {
			return nil
		}
		return startupUpdateNoticeMsg{version: refreshed.Status.Update.LatestVersion}
	}
}

func batchCmds(cmds ...tea.Cmd) tea.Cmd {
	filtered := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return tea.Batch(filtered...)
}

func (m *uiModel) layout() uiViewLayout {
	return uiViewLayout{model: m}
}
