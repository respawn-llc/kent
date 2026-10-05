package chatcontext

import (
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
)

type Policy struct {
	ContextWindowTokens      int64
	AutomaticThresholdTokens int64
	CompactionMode           contextpb.CompactionMode
}

type ProjectionInput struct {
	Policy                   Policy
	UsedTokens               int64
	AutoCompactionEnabled    bool
	CompletedCompactionCount int64
	CompactionRunning        bool
	ManualCompactEligible    bool
}

func Project(input ProjectionInput) *contextpb.Context {
	window := input.Policy.ContextWindowTokens
	used := max(input.UsedTokens, 0)
	threshold := min(max(input.Policy.AutomaticThresholdTokens, 0), window)
	count := max(input.CompletedCompactionCount, 0)
	mode := input.Policy.CompactionMode

	return &contextpb.Context{
		ContextWindowTokens:      window,
		UsedTokens:               used,
		RemainingTokens:          window - used,
		AutomaticThresholdTokens: threshold,
		AutoCompactionEnabled:    input.AutoCompactionEnabled,
		CompactionMode:           mode,
		CompletedCompactionCount: count,
		CompactionRunning:        input.CompactionRunning,
		ManualCompactAvailable: mode != contextpb.CompactionMode_COMPACTION_MODE_DISABLED &&
			!input.CompactionRunning &&
			input.ManualCompactEligible,
	}
}
