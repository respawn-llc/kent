package clientui

import "strings"

type RuntimeRunState struct {
	Busy             bool
	Compacting       bool
	ReviewerRunning  bool
	ReviewerBlocking bool
	GoalLoop         bool
}

type RuntimeConversationState struct {
	Freshness ConversationFreshness
}

type RuntimeReasoningState struct {
	StatusHeader string
}

type PendingInputState struct {
	Input             string
	PendingInjected   []QueuedUserMessage
	LockedInjectText  string
	LockedInjectID    string
	InputSubmitLocked bool
}

type BackgroundNoticeKind uint8

const (
	BackgroundNoticeSuccess BackgroundNoticeKind = iota + 1
	BackgroundNoticeError
)

type BackgroundNotice struct {
	Message string
	Kind    BackgroundNoticeKind
}

type RuntimeTranscriptSyncReason string

const (
	RuntimeTranscriptSyncNone                RuntimeTranscriptSyncReason = ""
	RuntimeTranscriptSyncStreamGap           RuntimeTranscriptSyncReason = "stream_gap"
	RuntimeTranscriptSyncCommittedAdvance    RuntimeTranscriptSyncReason = "committed_advance"
	RuntimeTranscriptSyncRecovery            RuntimeTranscriptSyncReason = "recovery"
	RuntimeTranscriptSyncOngoingErrorUpdated RuntimeTranscriptSyncReason = "ongoing_error_updated"
)

type RuntimeTranscriptSyncCommand struct {
	Reason        RuntimeTranscriptSyncReason
	RecoveryCause TranscriptRecoveryCause
}

func (c RuntimeTranscriptSyncCommand) IsSet() bool {
	return c.Reason != RuntimeTranscriptSyncNone
}

type RuntimeAssistantStreamCommandKind uint8

const (
	RuntimeAssistantStreamAppend RuntimeAssistantStreamCommandKind = iota + 1
	RuntimeAssistantStreamClear
)

type RuntimeAssistantStreamCommand struct {
	Kind   RuntimeAssistantStreamCommandKind
	Delta  string
	StepID string
}

type RuntimeTranscriptReduction struct {
	Sync                  RuntimeTranscriptSyncCommand
	AssistantStream       []RuntimeAssistantStreamCommand
	SyntheticOngoingEntry *ChatEntry
}

type RuntimeActivityCommand uint8

const (
	RuntimeActivityUnchanged RuntimeActivityCommand = iota
	RuntimeActivityRunning
	RuntimeActivityIdle
)

type RuntimeRunStateReduction struct {
	State    RuntimeRunState
	Activity RuntimeActivityCommand
}

type RuntimeDraftInputCommandKind uint8

const (
	RuntimePendingInputKeepDraft RuntimeDraftInputCommandKind = iota
	RuntimePendingInputClearDraft
)

type RuntimePromptHistoryCommand struct {
	Text string
}

type RuntimePendingInputReduction struct {
	State                PendingInputState
	DraftCommand         RuntimeDraftInputCommandKind
	PromptHistoryCommand *RuntimePromptHistoryCommand
}

type RuntimeReasoningStreamCommandKind uint8

const (
	RuntimeReasoningStreamUpsert RuntimeReasoningStreamCommandKind = iota + 1
	RuntimeReasoningStreamClear
)

type RuntimeReasoningStreamCommand struct {
	Kind  RuntimeReasoningStreamCommandKind
	Delta *ReasoningDelta
}

type RuntimeReasoningReduction struct {
	State  RuntimeReasoningState
	Stream []RuntimeReasoningStreamCommand
}

type RuntimeBackgroundProcessCommand uint8

const (
	RuntimeBackgroundProcessUnchanged RuntimeBackgroundProcessCommand = iota
	RuntimeBackgroundProcessRefresh
)

type RuntimeNoticeReduction struct {
	BackgroundNotice *BackgroundNotice
}

type RuntimeBackgroundProcessReduction struct {
	Command RuntimeBackgroundProcessCommand
}

type RuntimeConversationReduction struct {
	State RuntimeConversationState
}

type RuntimeEventReduction struct {
	Transcript          RuntimeTranscriptReduction
	RunState            RuntimeRunStateReduction
	Conversation        RuntimeConversationReduction
	PendingInput        RuntimePendingInputReduction
	Reasoning           RuntimeReasoningReduction
	BackgroundProcesses RuntimeBackgroundProcessReduction
	Notices             RuntimeNoticeReduction
}

func ReduceRuntimeEvent(
	runState RuntimeRunState,
	conversationState RuntimeConversationState,
	input PendingInputState,
	reasoningState RuntimeReasoningState,
	activityRunning bool,
	evt Event,
) RuntimeEventReduction {
	return RuntimeEventReduction{
		Transcript:          ReduceRuntimeTranscriptEvent(evt),
		RunState:            ReduceRuntimeRunStateEvent(runState, activityRunning, evt),
		Conversation:        ReduceRuntimeConversationEvent(conversationState, evt),
		PendingInput:        ReduceRuntimePendingInputEvent(input, evt),
		Reasoning:           ReduceRuntimeReasoningEvent(reasoningState, evt),
		BackgroundProcesses: ReduceRuntimeBackgroundProcessEvent(evt),
		Notices:             ReduceRuntimeNoticeEvent(evt),
	}
}

func ReduceRuntimeTranscriptEvent(evt Event) RuntimeTranscriptReduction {
	switch evt.Kind {
	case EventStreamGap:
		return RuntimeTranscriptReduction{Sync: RuntimeTranscriptSyncCommand{Reason: RuntimeTranscriptSyncStreamGap, RecoveryCause: evt.RecoveryCause}}
	case EventConversationUpdated:
		if evt.RecoveryCause != TranscriptRecoveryCauseNone {
			return RuntimeTranscriptReduction{Sync: RuntimeTranscriptSyncCommand{Reason: RuntimeTranscriptSyncRecovery, RecoveryCause: evt.RecoveryCause}}
		}
		if evt.CommittedTranscriptChanged {
			return RuntimeTranscriptReduction{Sync: RuntimeTranscriptSyncCommand{Reason: RuntimeTranscriptSyncCommittedAdvance}}
		}
	case EventOngoingErrorUpdated:
		return RuntimeTranscriptReduction{Sync: RuntimeTranscriptSyncCommand{Reason: RuntimeTranscriptSyncOngoingErrorUpdated}}
	case EventAssistantDelta:
		return RuntimeTranscriptReduction{AssistantStream: []RuntimeAssistantStreamCommand{{Kind: RuntimeAssistantStreamAppend, Delta: evt.AssistantDelta, StepID: evt.StepID}}}
	case EventAssistantDeltaReset:
		return RuntimeTranscriptReduction{AssistantStream: []RuntimeAssistantStreamCommand{{Kind: RuntimeAssistantStreamClear, StepID: evt.StepID}}}
	}
	return RuntimeTranscriptReduction{}
}

func ReduceRuntimeRunStateEvent(state RuntimeRunState, activityRunning bool, evt Event) RuntimeRunStateReduction {
	next := state
	reduction := RuntimeRunStateReduction{State: next}
	switch evt.Kind {
	case EventCompactionStarted:
		reduction.State.Compacting = true
	case EventCompactionCompleted, EventCompactionFailed:
		reduction.State.Compacting = false
	case EventReviewerStarted:
		reduction.State.ReviewerRunning = true
		reduction.State.ReviewerBlocking = true
	case EventReviewerCompleted:
		reduction.State.ReviewerRunning = false
		reduction.State.ReviewerBlocking = false
	case EventRunStateChanged:
		if evt.RunState == nil {
			return reduction
		}
		reduction.State.Busy = evt.RunState.Busy
		reduction.State.GoalLoop = evt.RunState.Busy && evt.RunState.GoalLoop
		if evt.RunState.Busy {
			reduction.Activity = RuntimeActivityRunning
			return reduction
		}
		if activityRunning {
			reduction.Activity = RuntimeActivityIdle
		}
	}
	return reduction
}

func ReduceRuntimeConversationEvent(state RuntimeConversationState, evt Event) RuntimeConversationReduction {
	if evt.Kind == EventUserMessageFlushed {
		return RuntimeConversationReduction{State: RuntimeConversationState{Freshness: ConversationFreshnessEstablished}}
	}
	return RuntimeConversationReduction{State: state}
}

func ReduceRuntimePendingInputEvent(input PendingInputState, evt Event) RuntimePendingInputReduction {
	next := clonePendingInputState(input)
	reduction := RuntimePendingInputReduction{
		State:        next,
		DraftCommand: RuntimePendingInputKeepDraft,
	}
	switch evt.Kind {
	case EventUserMessageFlushed:
		consumed := consumedQueuedUserMessages(reduction.State.PendingInjected, evt.UserMessageBatchQueueItemIDs)
		if len(consumed) > 0 {
			reduction.State.PendingInjected = append([]QueuedUserMessage(nil), reduction.State.PendingInjected[len(consumed):]...)
			reduction.PromptHistoryCommand = &RuntimePromptHistoryCommand{Text: evt.UserMessage}
		}
		if reduction.State.InputSubmitLocked && containsQueuedUserMessageID(consumed, reduction.State.LockedInjectID) {
			if reduction.State.Input == reduction.State.LockedInjectText {
				reduction.DraftCommand = RuntimePendingInputClearDraft
			}
			reduction.State.LockedInjectText = ""
			reduction.State.LockedInjectID = ""
			reduction.State.InputSubmitLocked = false
		}
	}
	return reduction
}

func ReduceRuntimeReasoningEvent(state RuntimeReasoningState, evt Event) RuntimeReasoningReduction {
	reduction := RuntimeReasoningReduction{State: state}
	switch evt.Kind {
	case EventReasoningDelta:
		delta := cloneReasoningDelta(evt.ReasoningDelta)
		reduction.Stream = append(reduction.Stream, RuntimeReasoningStreamCommand{Kind: RuntimeReasoningStreamUpsert, Delta: delta})
		if delta != nil {
			if nextHeader := ExtractReasoningStatusHeader(delta.Text); nextHeader != "" {
				reduction.State.StatusHeader = nextHeader
			}
		}
	case EventReasoningDeltaReset:
		reduction.Stream = append(reduction.Stream, RuntimeReasoningStreamCommand{Kind: RuntimeReasoningStreamClear})
	case EventRunStateChanged:
		if evt.RunState != nil && !evt.RunState.Busy {
			reduction.State.StatusHeader = ""
			reduction.Stream = append(reduction.Stream, RuntimeReasoningStreamCommand{Kind: RuntimeReasoningStreamClear})
		}
	}
	return reduction
}

func ReduceRuntimeBackgroundProcessEvent(evt Event) RuntimeBackgroundProcessReduction {
	if evt.Kind != EventBackgroundUpdated {
		return RuntimeBackgroundProcessReduction{}
	}
	return RuntimeBackgroundProcessReduction{Command: RuntimeBackgroundProcessRefresh}
}

func ReduceRuntimeNoticeEvent(evt Event) RuntimeNoticeReduction {
	if evt.Kind != EventBackgroundUpdated {
		return RuntimeNoticeReduction{}
	}
	notice := backgroundNoticeFromEvent(evt.Background)
	if notice == nil {
		return RuntimeNoticeReduction{}
	}
	return RuntimeNoticeReduction{BackgroundNotice: notice}
}

func ExtractReasoningStatusHeader(text string) string {
	trimmed := strings.TrimSpace(text)
	bytes := []byte(trimmed)
	for i := 0; i+1 < len(bytes); i++ {
		if bytes[i] != '*' || bytes[i+1] != '*' {
			continue
		}
		start := i + 2
		for j := start; j+1 < len(bytes); j++ {
			if bytes[j] != '*' || bytes[j+1] != '*' {
				continue
			}
			inner := strings.TrimSpace(trimmed[start:j])
			if inner == "" {
				return ""
			}
			return inner
		}
		return ""
	}
	return ""
}

func clonePendingInputState(input PendingInputState) PendingInputState {
	cloned := input
	if len(input.PendingInjected) > 0 {
		cloned.PendingInjected = append([]QueuedUserMessage(nil), input.PendingInjected...)
	}
	return cloned
}

func consumedQueuedUserMessages(pending []QueuedUserMessage, ids []string) []QueuedUserMessage {
	if len(pending) == 0 || len(ids) == 0 {
		return nil
	}
	consumed := make([]QueuedUserMessage, 0, len(ids))
	for index, id := range ids {
		if index >= len(pending) || pending[index].ID != id {
			return consumed
		}
		consumed = append(consumed, pending[index])
	}
	return consumed
}

func containsQueuedUserMessageID(messages []QueuedUserMessage, id string) bool {
	if id == "" {
		return false
	}
	for _, message := range messages {
		if message.ID == id {
			return true
		}
	}
	return false
}

func cloneReasoningDelta(delta *ReasoningDelta) *ReasoningDelta {
	if delta == nil {
		return nil
	}
	cloned := *delta
	return &cloned
}

func backgroundNoticeFromEvent(evt *BackgroundShellEvent) *BackgroundNotice {
	if evt == nil || evt.NoticeSuppressed {
		return nil
	}
	if evt.Type != "completed" && evt.Type != "killed" {
		return nil
	}
	message := strings.TrimSpace(evt.CompactText)
	if message == "" {
		message = "background shell " + evt.ID + " " + evt.State
	}
	notice := &BackgroundNotice{
		Message: message,
		Kind:    BackgroundNoticeSuccess,
	}
	if evt.Type == "killed" && !evt.UserRequestedKill {
		notice.Kind = BackgroundNoticeError
	}
	return notice
}
