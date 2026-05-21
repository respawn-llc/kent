package app

import (
	"strings"
	"time"

	"builder/cli/tui"
	"builder/shared/clientui"
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

// Active submit is the in-flight turn only. uiModel.queued stores future work;
// never mirror active submit there or it can run again after completion.
type activeSubmitState struct {
	token              uint64
	stepID             string
	text               string
	queuedID           string
	restoreOnInterrupt bool
	flushed            bool
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
	Text             string
	AllowBlank       bool
	ClearBelowBefore bool
	Sequence         uint64
}

type nativeStreamingStableFlushAckMsg struct {
	Sequence uint64
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
	NoticeID string
}

type uiStatusNoticeDelivery uint8

const (
	uiStatusNoticeReplace uiStatusNoticeDelivery = iota
	uiStatusNoticeQueue
)
