package app

import worktreepb "core/shared/protoapi/gen/kent/api/worktree"

import transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"context"
	"time"

	"core/cli/app/commands"
	"core/cli/tui"
	tuiinput "core/cli/tui/input"
	"core/cli/tui/ongoing"
	"core/cli/tui/transcriptrender"
	"core/shared/apicontract"
	"core/shared/clientui"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"

	tea "github.com/charmbracelet/bubbletea"
)

type uiModel struct {
	drafts             *uiDraftPersistence
	presentationTarget uiPresentationTarget
	eventDispatcher    *uiEventDispatcher
	uiRuntimeFeatureState
	uiInputFeatureState
	uiPresentationFeatureState
	uiConversationFeatureState
	uiSessionTransitionFeatureState
	uiStatusFeatureState
	uiTranscriptFeatureState
	uiKeyboardFeatureState
	uiRollbackFeatureState
	uiWorktreeFeatureState
}

type uiRuntimeFeatureState struct {
	chatMutations      apicontract.ChatMutationService
	chatSettings       apicontract.ChatSettingsService
	sessionControls    apicontract.RuntimeControlService
	createSession      func(context.Context, *chatpb.NewChatTarget, string) (*chatpb.ExistingSessionTarget, error)
	observeSession     func(context.Context, runtimeids.SessionID) (sessionLaunchPlan, *runtimeLaunchPlan, error)
	sessionObservation *runtimeLaunchPlan
	observationContext context.Context
	observationScope   context.Context
	cancelObservation  context.CancelFunc
	engine             clientui.RuntimeClient
	view               tui.Model

	processClient         clientui.ProcessClient
	processClientExplicit bool
	worktreeClient        apicontract.WorktreeService

	pathReferenceEvents        <-chan uiPathReferenceSearchEvent
	runtimeConnectionEvents    chan runtimeConnectionStateChangedMsg
	runtimeContextUsage        *runtimepb.ContextUsage
	runtimeContextUsageSession string
	runtimeActivityProjection  *runtimepb.Activity
	missingPromptRecovery      *missingPromptRecoveryScope
	logger                     uiLogger
}

type missingPromptRecoveryScope struct {
	sessionID string
	stepID    runtimeids.StepID
}

type uiInputFeatureState struct {
	pendingNewChatActions  int
	mainEditor             tuiinput.Editor
	mainInputDraftToken    uint64
	promptHistory          []string
	promptHistoryLoading   bool
	promptHistorySelection *int
	promptHistoryDraft     *tuiinput.EditorSnapshot
	activity               uiActivity
	runtimeLifecycle       uiRuntimeLifecycle
	reviewerEnabled        bool
	reviewerMode           string
	autoCompactionEnabled  bool
	questionsEnabled       bool
	conversationFreshness  runtimepb.ConversationFreshness
	localConversationTurn  bool
	runtimeControlToken    uint64
	runtimeControlTokens   map[runtimeControlOperation]uint64
	runtimeControlPending  map[runtimeControlOperation]runtimeControlPendingState

	// UI-side post-turn input queue. It may contain slash commands, shell
	// commands, and other client-only actions; server queues only runtime
	// injected user work.
	queued       []queuedInputItem
	submitToken  uint64
	activeSubmit activeSubmitState

	injectedQueue               []*injectedRuntimeQueueItem
	unownedQueuedTerminalStates map[string]*transcriptpb.QueuedMessageState
	pendingInputSubmissionOrder uint64
	interruptLifecycle          uiInterruptLifecycle
	currentStepID               string
	interruptStepID             string

	modelName                 string
	configuredModelName       *string
	thinkingLevel             string
	fastModeAvailable         bool
	fastModeEnabled           bool
	modelContractLocked       bool
	spinnerFrame              int
	spinnerClock              frameAnimationClock
	spinnerTickDue            time.Time
	spinnerGeneration         uint64
	spinnerTickToken          uint64
	commandRegistry           *commands.Registry
	promptCatalog             apicontract.PromptCommandCatalogService
	promptCatalogEntries      []commands.PromptCommandCatalogEntry
	finalAnswerOperation      *uiFinalAnswerOperation
	finalAnswerOperationToken uint64
	slashCommandFilter        string
	slashCommandFilterSet     bool
	slashCommandSelection     int
	pathReferenceSearch       uiPathReferenceSearch
	pathReference             uiPathReferenceState
}

type uiPresentationFeatureState struct {
	theme                       string
	tuiNativeProgressBar        bool
	terminalOutput              *uiTerminalOutput
	nativeProgress              uiNativeProgressState
	markdownLinks               transcriptrender.MarkdownLinkPresentation
	activeSurface               uiSurface
	altScreenActive             bool
	terminalFocus               *terminalFocusState
	terminalCursor              *uiTerminalCursorState
	rendererOutputGate          *uiRendererOutputGateState
	ongoingSurface              *ongoing.Surface
	ongoingTranscript           *ongoingTranscriptController
	requestOngoingOpen          func()
	ongoingWidthToken           uint64
	pendingOngoingScratchReset  *ongoing.RehydrateReason
	pendingOngoingWidthReset    bool
	pendingOngoingResizeRepaint bool
	terminalGeometry            terminalGeometry
	ownershipReconciler         *ongoingOwnershipReconciler
	pendingOwnershipCmd         tea.Cmd
	helpVisible                 bool
	startupCmds                 []tea.Cmd
	uiMainThread                uiMainThreadState
}

type uiConversationFeatureState struct {
	interaction                        uiInteractionState
	ask                                uiAskState
	questionProjector                  questionProjector
	promptAnswers                      *transcriptPromptAnswerer
	promptAttention                    promptAttentionSink
	startupSubmit                      string
	startupSubmitPromptHistoryRecorded bool
}

type uiSessionTransitionFeatureState struct {
	exitAction                              UIAction
	nextSessionInitialPrompt                string
	nextSessionInitialPromptHistoryRecorded bool
	nextSessionInitialInput                 *string
	nextSessionID                           string
	nextForkRollbackTargetID                string
	nextPreviousSessionID                   *runtimeids.SessionID
	sessionExecutionTarget                  *worktreepb.SessionExecutionTarget
	sessionRetargeted                       bool
	sessionName                             *string
	sessionCategory                         *sessioncontract.SessionCategory
	sessionShortID                          *runtimeids.SessionShortID
	stopAttention                           func()
	forcedLocalExit                         bool
}

type uiStatusFeatureState struct {
	processList                 uiProcessListState
	reasoningStatusHeader       string
	turnQueueHook               turnQueueHook
	statusConfig                uiStatusConfig
	statusCollector             uiStatusCollector
	statusRepository            uiStatusRepository
	status                      uiStatusOverlayState
	goal                        uiGoalOverlayState
	goalRuntimeToken            uint64
	goalRuntimeMutationSerial   uint64
	goalRuntimePending          goalRuntimePendingState
	statusGitBackgroundInFlight bool
	clipboardPaster             uiClipboardPaster
	clipboardTextCopier         uiClipboardTextCopier

	transientStatus              string
	transientStatusKind          uiStatusNoticeKind
	transientStatusNoticeID      string
	transientStatusDetailRequest *uiPendingDetailTranscriptRequest
	transientStatusToken         uint64
	transientStatusQueue         []uiStatusNotice
	localNoticeSequence          uint64
	debugKeys                    bool
	debugMode                    bool
}

type uiTranscriptFeatureState struct {
	runtimeConnection                            clientui.RuntimeConnectionLifecycle
	pendingWorkRefresh                           pendingWorkRefreshOwner
	runtimeMainViewToken                         uint64
	runtimeMainViewBusy                          bool
	runtimeMainViewPendingSet                    bool
	runtimeMainViewPendingInterruptedSubmitToken *uint64
	detailTranscript                             uiDetailTranscriptWindow
	pendingDetailTranscript                      *uiPendingDetailTranscriptRequest
}

type uiKeyboardFeatureState struct {
	lastEscAt              time.Time
	pendingCSIShiftEnterAt time.Time
	pendingCSIShiftEnter   bool
	keyboardReporting      *uiTerminalKeyboardReportingOwner
}

type uiRollbackFeatureState struct {
	rollback uiRollbackState
}

type uiWorktreeFeatureState struct {
	worktrees                        uiWorktreeOverlayState
	worktreeListGeneration           uint64
	deleteTargetResolutionGeneration uint64
}
