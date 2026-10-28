package runtime

import (
	"time"

	"core/server/llm"
)

var generateRetryDelays = llm.DefaultGenerationRetrySchedule().Failures
var idleStallRetryDelays = llm.DefaultGenerationRetrySchedule().StreamStalls
var compactionRetryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

type modelRequestRetryPolicy uint8

const (
	modelRequestRetriesAllowed modelRequestRetryPolicy = iota + 1
	// Each provider dispatch is single-attempt. Compaction's specified
	// tool-call correction and overflow repairs may dispatch corrected requests.
	modelRequestSingleAttempt
)

func (p modelRequestRetryPolicy) delays(standard []time.Duration) []time.Duration {
	switch p {
	case modelRequestRetriesAllowed:
		return standard
	case modelRequestSingleAttempt:
		return nil
	default:
		panic("invalid model request retry policy")
	}
}
