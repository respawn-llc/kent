package app

import (
	"time"

	"builder/cli/app/commands"
	"builder/cli/app/internal/runtimestate"
	"builder/cli/tui"
	"builder/shared/client"
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

type uiModel struct {
	uiRuntimeFeatureState
	uiInputFeatureState
	uiPresentationFeatureState
	uiConversationFeatureState
	uiSessionTransitionFeatureState
	uiStatusFeatureState
	uiTranscriptFeatureState
	uiNativeHistoryFeatureState
	uiKeyboardFeatureState
	uiRollbackFeatureState
	uiWorktreeFeatureState
}

type uiRuntimeFeatureState struct {
	engine clientui.RuntimeClient
	view   tui.Model

	processClient         clientui.ProcessClient
	processClientExplicit bool
	worktreeClient        client.WorktreeClient

	runtimeEvents               <-chan clientui.Event
	pendingRuntimeEvents        []clientui.Event
	askEvents                   <-chan askEvent
	pathReferenceEvents         <-chan uiPathReferenceSearchEvent
	runtimeConnectionEvents     <-chan runtimeConnectionStateChangedMsg
	runtimeLeaseRecoveryWarning <-chan runtimeLeaseRecoveryWarningMsg
	runtimeContextUsage         clientui.RuntimeContextUsage
	runtimeContextUsageSession  string
	logger                      uiLogger
}

type uiInputFeatureState struct {
	input                    string
	inputCursor              int // rune index; -1 means "track tail"
	inputKillBuffer          string
	mainInputDraftToken      uint64
	promptHistory            []string
	promptHistorySelection   int
	promptHistoryDraft       string
	promptHistoryDraftCursor int
	activity                 uiActivity
	runtimeLifecycle         runtimestate.RuntimeRunState
	reviewerEnabled          bool
	reviewerMode             string
	autoCompactionEnabled    bool
	conversationFreshness    clientui.ConversationFreshness
	runtimeControlToken      uint64
	runtimeControlTokens     map[runtimeControlOperation]uint64
	runtimeControlPending    map[runtimeControlOperation]runtimeControlPendingState

	// UI-side post-turn input queue. It may contain slash commands, shell
	// commands, and other client-only actions; server queues only runtime
	// injected user work.
	queued                                 []queuedInputItem
	compactionOrigin                       uiCompactionOrigin
	queuedRuntimeWorkCheckCompactionOrigin uiCompactionOrigin
	submitToken                            uint64
	activeSubmit                           activeSubmitState

	pendingInjected    []clientui.QueuedUserMessage
	injectedQueue      []injectedRuntimeQueueItem
	injectedQueueToken uint64
	lockedInjectText   string
	lockedInjectID     string
	inputSubmission    runtimestate.InputSubmissionLifecycle
	interruptLifecycle uiInterruptLifecycle

	modelName             string
	configuredModelName   string
	thinkingLevel         string
	fastModeAvailable     bool
	fastModeEnabled       bool
	modelContractLocked   bool
	spinnerFrame          int
	spinnerClock          frameAnimationClock
	spinnerTickDue        time.Time
	spinnerGeneration     uint64
	spinnerTickToken      uint64
	commandRegistry       *commands.Registry
	hasOtherSessions      bool
	hasOtherSessionsKnown bool
	authSlashCommandName  string
	authSlashCommandErr   string
	authSlashSessionOpen  bool
	authSlashLoading      bool
	authSlashToken        uint64
	authSlashGeneration   uint64
	authSlashResolved     uint64
	slashCommandFilter    string
	slashCommandFilterSet bool
	slashCommandSelection int
	pathReferenceSearch   uiPathReferenceSearch
	pathReference         uiPathReferenceState
}

type uiPresentationFeatureState struct {
	theme           string
	activeSurface   uiSurface
	altScreenActive bool
	terminalFocus   *terminalFocusState
	terminalCursor  *uiTerminalCursorState
	termWidth       int
	termHeight      int
	windowSizeKnown bool
	helpVisible     bool
	startupCmds     []tea.Cmd
	uiMainThread    uiMainThreadState
}

type uiConversationFeatureState struct {
	interaction       uiInteractionState
	ask               uiAskState
	initialTranscript []UITranscriptEntry
	startupSubmit     string
}

type uiSessionTransitionFeatureState struct {
	exitAction               UIAction
	nextSessionInitialPrompt string
	nextSessionInitialInput  string
	nextSessionID            string
	nextForkRollbackTargetID string
	nextParentSessionID      string
	sessionName              string
	sessionID                string
}

type uiStatusFeatureState struct {
	processList                 uiProcessListState
	reasoningStatusHeader       string
	turnQueueHook               turnQueueHook
	askNotificationHook         askNotificationHook
	statusConfig                uiStatusConfig
	statusCollector             uiStatusCollector
	statusRepository            uiStatusRepository
	status                      uiStatusOverlayState
	goal                        uiGoalOverlayState
	goalRuntimeToken            uint64
	statusGitBackgroundInFlight bool
	clipboardImagePaster        uiClipboardImagePaster
	clipboardTextCopier         uiClipboardTextCopier

	transientStatus         string
	transientStatusKind     uiStatusNoticeKind
	transientStatusNoticeID string
	transientStatusToken    uint64
	transientStatusQueue    []uiStatusNotice
	localNoticeSequence     uint64
	localEntryEcho          uiLocalEntryEchoState
	startupUpdateNotice     bool
	startupUpdateShown      bool
	debugKeys               bool
	debugMode               bool
	transcriptDiagnostics   bool
	tuiStrictIOMode         tuiStrictIOMode
	tuiStrictIOModeExplicit bool
}

type uiTranscriptFeatureState struct {
	sawAssistantDelta                bool
	lastCommittedAssistantStepID     string
	transcriptEntries                []tui.TranscriptEntry
	transcriptBaseOffset             int
	transcriptTotalEntries           int
	transcriptRevision               int64
	ongoingCommittedDelivery         ongoingCommittedDeliveryCursor
	deferredCommittedTail            []deferredProjectedTranscriptTail
	runtimeConnection                clientui.RuntimeConnectionLifecycle
	transcriptLiveDirty              bool
	reasoningLiveDirty               bool
	detailTranscript                 uiDetailTranscriptWindow
	runtimeMainViewToken             uint64
	runtimeMainViewBusy              bool
	runtimeMainViewActiveRequest     runtimeMainViewRefreshRequest
	runtimeMainViewPendingSet        bool
	runtimeMainViewPending           runtimeMainViewRefreshRequest
	runtimeTranscriptToken           uint64
	runtimeCommittedSuffixToken      uint64
	runtimeTranscriptRetry           uint64
	runtimeTranscriptBusy            bool
	runtimeTranscriptActiveRequest   runtimeTranscriptSyncRequest
	runtimeTranscriptPendingSet      bool
	runtimeTranscriptPending         runtimeTranscriptSyncRequest
	pendingQueuedDrainAfterHydration bool
	queuedDrainReadyAfterHydration   bool
	waitRuntimeEventAfterHydration   bool
}

type uiNativeHistoryFeatureState struct {
	nativeFlushedEntryCount            int
	nativeHistoryReplayed              bool
	nativeReplayWidth                  int
	nativeFormatterWidth               int
	nativeCommittedProjector           tui.CommittedOngoingProjector
	nativeProjection                   tui.TranscriptProjection
	nativeProjectionBaseOffset         int
	nativeRenderedProjection           tui.TranscriptProjection
	nativeRenderedBaseOffset           int
	nativeRenderedSnapshot             string
	nativeHistoryReplayPermit          nativeHistoryReplayPermit
	nativeFlushSequence                uint64
	nativeFlushedSequence              uint64
	nativePendingFlushes               map[uint64]nativeHistoryFlushMsg
	waitRuntimeEventAfterFlushSequence uint64
	nativeLiveRegionLines              int
	nativeLiveRegionPad                int
	nativeStreamingActive              bool
	nativeStreamingController          nativeAssistantStreamController
	nativeStreamingTail                []tui.TranscriptProjectionLine
	nativeStreamingStableFlushSequence uint64
	nativeStreamingText                string
	nativeStreamingStepID              string
	nativeStreamingCommitStart         int
	nativeStreamingCommitEnd           int
	nativeStreamingCommitRangeSet      bool
	nativeStreamingWidth               int
	nativeStreamingFlushedLineCount    int
	nativeStreamingDividerFlushed      bool
	nativeResizeReplayToken            uint64
	nativeResizeReplayAt               time.Time
}

type uiKeyboardFeatureState struct {
	lastEscAt              time.Time
	pendingCSIShiftEnterAt time.Time
	pendingCSIShiftEnter   bool
}

type uiRollbackFeatureState struct {
	rollback uiRollbackState
}

type uiWorktreeFeatureState struct {
	worktrees uiWorktreeOverlayState
}
