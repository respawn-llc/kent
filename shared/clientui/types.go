package clientui

import (
	"builder/shared/cachewarn"
	patchformat "builder/shared/transcript/patchformat"
	"time"
)

type EventKind string

type TranscriptRecoveryCause string

const (
	EventConversationUpdated EventKind = "conversation_updated"
	EventStreamGap           EventKind = "stream_gap"
	EventAssistantDelta      EventKind = "assistant_delta"
	EventAssistantDeltaReset EventKind = "assistant_delta_reset"
	EventOngoingErrorUpdated EventKind = "ongoing_error_updated"
	EventReasoningDelta      EventKind = "reasoning_delta"
	EventReasoningDeltaReset EventKind = "reasoning_delta_reset"
	EventAssistantMessage    EventKind = "assistant_message"
	EventModelResponse       EventKind = "model_response_received"
	EventUserMessageFlushed  EventKind = "user_message_flushed"
	EventToolCallStarted     EventKind = "tool_call_started"
	EventToolCallCompleted   EventKind = "tool_call_completed"
	EventReviewerStarted     EventKind = "reviewer_started"
	EventReviewerCompleted   EventKind = "reviewer_completed"
	EventInFlightClearFailed EventKind = "in_flight_clear_failed"
	EventCompactionStarted   EventKind = "context_compaction_started"
	EventCompactionCompleted EventKind = "context_compaction_completed"
	EventCompactionFailed    EventKind = "context_compaction_failed"
	EventCacheWarning        EventKind = "cache_warning"
	EventLocalEntryAdded     EventKind = "local_entry_added"
	EventRunStateChanged     EventKind = "run_state_changed"
	EventBackgroundUpdated   EventKind = "background_updated"

	TranscriptRecoveryCauseNone         TranscriptRecoveryCause = ""
	TranscriptRecoveryCauseStreamGap    TranscriptRecoveryCause = "stream_gap"
	TranscriptRecoveryCauseHydrateRetry TranscriptRecoveryCause = "hydrate_retry"
)

type Event struct {
	Sequence                     uint64
	Kind                         EventKind
	StepID                       string
	RecoveryCause                TranscriptRecoveryCause
	CommittedTranscriptChanged   bool
	TranscriptRevision           int64
	CommittedEntryCount          int
	CommittedEntryStart          int
	CommittedEntryStartSet       bool
	Error                        string
	AssistantDelta               string
	ReasoningDelta               *ReasoningDelta
	UserMessage                  string
	UserMessageBatch             []string
	UserMessageBatchQueueItemIDs []string
	TranscriptEntries            []ChatEntry
	Compaction                   *CompactionStatus
	CacheWarning                 *cachewarn.Warning
	CacheWarningVisibility       EntryVisibility
	RunState                     *RunState
	ContextUsage                 *RuntimeContextUsage
	Background                   *BackgroundShellEvent
}

type CompactionStatus struct {
	Mode  string
	Count int
	Error string
}

type ReasoningDelta struct {
	Key  string
	Role string
	Text string
}

type RunState struct {
	Busy       bool
	RunID      string
	Status     RunStatus
	GoalLoop   bool
	StartedAt  time.Time
	FinishedAt time.Time
}

type BackgroundShellEvent struct {
	Type              string
	ID                string
	State             string
	Command           string
	Workdir           string
	LogPath           string
	NoticeText        string
	CompactText       string
	Preview           string
	Removed           int
	ExitCode          *int
	UserRequestedKill bool
	NoticeSuppressed  bool
}

type ChatEntry struct {
	Visibility        EntryVisibility
	RollbackTargetID  string
	Role              string
	Text              string
	OngoingText       string
	Phase             string
	MessageType       string
	SourcePath        string
	CompactLabel      string
	ToolResultSummary string
	ToolCallID        string
	NoticeID          string
	ToolCall          *ToolCallMeta
}

const ChatEntryPhaseFinalAnswer = string(MessagePhaseFinal)

type ChatSnapshot struct {
	Entries      []ChatEntry
	Ongoing      string
	OngoingError string
}

type TranscriptWindow string

const (
	TranscriptWindowDefault     TranscriptWindow = ""
	TranscriptWindowOngoingTail TranscriptWindow = "ongoing_tail"
)

type TranscriptPageRequest struct {
	Offset                   int
	Limit                    int
	Page                     int
	PageSize                 int
	Window                   TranscriptWindow
	KnownRevision            int64
	KnownCommittedEntryCount int
}

type TranscriptPage struct {
	SessionID             string
	SessionName           string
	ConversationFreshness ConversationFreshness
	Revision              int64
	TotalEntries          int
	Offset                int
	NextOffset            int
	HasMore               bool
	Entries               []ChatEntry
	Ongoing               string
	OngoingError          string
}

const (
	DefaultCommittedTranscriptSuffixLimit = 250
	MaxCommittedTranscriptSuffixLimit     = 500
)

type CommittedTranscriptSuffixRequest struct {
	AfterEntryCount int
	Limit           int
}

type CommittedTranscriptSuffix struct {
	SessionID             string
	SessionName           string
	ConversationFreshness ConversationFreshness
	Revision              int64
	CommittedEntryCount   int
	StartEntryCount       int
	NextEntryCount        int
	HasMore               bool
	Entries               []ChatEntry
}

func NormalizeCommittedTranscriptSuffixRequest(req CommittedTranscriptSuffixRequest) CommittedTranscriptSuffixRequest {
	if req.AfterEntryCount < 0 {
		req.AfterEntryCount = 0
	}
	if req.Limit <= 0 {
		req.Limit = DefaultCommittedTranscriptSuffixLimit
	}
	if req.Limit > MaxCommittedTranscriptSuffixLimit {
		req.Limit = MaxCommittedTranscriptSuffixLimit
	}
	return req
}

type ToolPresentationKind string
type ToolCallRenderBehavior string
type ToolRenderKind string
type ToolShellDialect string

const (
	ToolPresentationDefault     ToolPresentationKind = "default"
	ToolPresentationShell       ToolPresentationKind = "shell"
	ToolPresentationAskQuestion ToolPresentationKind = "ask_question"

	ToolCallRenderBehaviorDefault     ToolCallRenderBehavior = "default"
	ToolCallRenderBehaviorShell       ToolCallRenderBehavior = "shell"
	ToolCallRenderBehaviorAskQuestion ToolCallRenderBehavior = "ask_question"

	ToolRenderKindShell  ToolRenderKind = "shell"
	ToolRenderKindDiff   ToolRenderKind = "diff"
	ToolRenderKindSource ToolRenderKind = "source"
	ToolRenderKindPlain  ToolRenderKind = "plain"

	ToolShellDialectPosix          ToolShellDialect = "posix"
	ToolShellDialectPowerShell     ToolShellDialect = "powershell"
	ToolShellDialectWindowsCommand ToolShellDialect = "windows_command"
)

type ToolRenderHint struct {
	Kind         ToolRenderKind
	Path         string
	ResultOnly   bool
	ShellDialect ToolShellDialect
}

type ToolCallMeta struct {
	ToolName               string
	Presentation           ToolPresentationKind
	RenderBehavior         ToolCallRenderBehavior
	IsShell                bool
	UserInitiated          bool
	Command                string
	CompactText            string
	InlineMeta             string
	TimeoutLabel           string
	PatchSummary           string
	PatchDetail            string
	PatchRender            *patchformat.RenderedPatch
	RenderHint             *ToolRenderHint
	Question               string
	Suggestions            []string
	RecommendedOptionIndex int
	OmitSuccessfulResult   bool
}
