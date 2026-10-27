package runtime

import (
	"time"

	"core/server/llm"
	"core/server/sessionstore"
	"core/shared/config"
	"core/shared/runtimeids"
)

type TranscriptHydrationSnapshot struct {
	ConnectionReplacement *config.ConnectionReplacement
	ActiveAssistants      []AssistantStreamSnapshot
	ActiveThinkingStatus  *TranscriptThinkingStatusState
	ActiveReasoningTraces []TranscriptReasoningTraceState
	InFlightTools         []TranscriptLiveToolStart
	ActiveCompaction      *TranscriptCompactionState
	ContextUsage          *ContextUsage
	Goal                  *session.GoalState
	GoalSuspended         bool
}

type TranscriptThinkingStatusState struct {
	StepID string
	Text   string
}

type TranscriptReasoningTraceIdentity struct {
	Provider *llm.ReasoningItemIdentity
	Kent     *runtimeids.ReasoningTraceID
}

type TranscriptReasoningTraceState struct {
	StepID           string
	Source           llm.ReasoningSourceCoordinate
	Identity         TranscriptReasoningTraceIdentity
	ProviderMetadata *llm.ReasoningItemIdentity
	Text             string
	startedAt        time.Time
}

type TranscriptCompactionState struct {
	StepID string
	Mode   string
	Count  int
}

func (e *Engine) WithTranscriptHydrationSnapshot(fn func(TranscriptHydrationSnapshot) error) error {
	if e == nil {
		if fn == nil {
			return nil
		}
		return fn(TranscriptHydrationSnapshot{})
	}
	e.outputMutationMu.Lock()
	defer e.outputMutationMu.Unlock()
	if fn == nil {
		return nil
	}
	e.ensureOrchestrationCollaborators()
	return fn(e.transcriptHydrationSegmentLocked())
}

func (e *Engine) transcriptHydrationSegmentLocked() TranscriptHydrationSnapshot {
	if e == nil {
		return TranscriptHydrationSnapshot{}
	}
	streaming, _ := e.transcriptRuntimeState().StreamingSnapshot()
	thinkingStatus, reasoningTraces := e.transcriptRuntimeState().ReasoningSnapshot()
	usage := e.ContextUsage()
	return TranscriptHydrationSnapshot{
		ConnectionReplacement: e.transcriptRuntimeState().ConnectionReplacement(),
		ActiveAssistants:      streaming,
		ActiveThinkingStatus:  thinkingStatus,
		ActiveReasoningTraces: reasoningTraces,
		InFlightTools:         e.transcriptRuntimeState().LiveToolSnapshot(),
		ActiveCompaction:      e.compactionRuntimeState().ActiveSnapshot(),
		ContextUsage:          &usage,
		Goal:                  e.Goal(),
		GoalSuspended:         e.GoalLoopSuspended(),
	}
}
