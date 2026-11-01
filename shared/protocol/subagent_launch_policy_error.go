package protocol

import (
	"fmt"

	"core/shared/runtimeids"
	"core/shared/textutil"
)

type SubagentLaunchPolicyErrorKind string

const (
	SubagentLaunchPolicyMaxDepthExceeded SubagentLaunchPolicyErrorKind = "max_depth_exceeded"
	SubagentLaunchPolicyLineageCorrupt   SubagentLaunchPolicyErrorKind = "lineage_corrupt"
)

type SubagentLaunchPolicyError struct {
	Kind              SubagentLaunchPolicyErrorKind `json:"kind"`
	AttemptedDepth    *int                          `json:"attempted_depth,omitempty"`
	MaxDepth          *int                          `json:"max_depth,omitempty"`
	RepeatedSessionID *runtimeids.SessionID         `json:"repeated_session_id,omitempty"`
	VisitedSessionIDs []runtimeids.SessionID        `json:"visited_session_ids,omitempty"`
}

func NewMaxDepthExceededSubagentLaunchPolicyError(attemptedDepth int, maxDepth int) *SubagentLaunchPolicyError {
	return &SubagentLaunchPolicyError{
		Kind:           SubagentLaunchPolicyMaxDepthExceeded,
		AttemptedDepth: textutil.Value(attemptedDepth),
		MaxDepth:       textutil.Value(maxDepth),
	}
}

func NewLineageCorruptSubagentLaunchPolicyError(repeated runtimeids.SessionID, visited []runtimeids.SessionID) *SubagentLaunchPolicyError {
	copiedRepeated := repeated
	return &SubagentLaunchPolicyError{
		Kind:              SubagentLaunchPolicyLineageCorrupt,
		RepeatedSessionID: &copiedRepeated,
		VisitedSessionIDs: append([]runtimeids.SessionID(nil), visited...),
	}
}

func (e *SubagentLaunchPolicyError) Error() string {
	if e == nil {
		return "subagent launch rejected"
	}
	switch e.Kind {
	case SubagentLaunchPolicyMaxDepthExceeded:
		if e.AttemptedDepth != nil && e.MaxDepth != nil {
			return fmt.Sprintf(
				"subagent launch rejected at depth %d (maximum %d): You are already a subagent, so you shouldn't spawn more subagents to prevent overloading the machine and infinite recursion. Do not attempt to use subagents anymore and complete the task on your own",
				*e.AttemptedDepth,
				*e.MaxDepth,
			)
		}
	case SubagentLaunchPolicyLineageCorrupt:
		return "subagent launch rejected: parent-agent lineage is corrupt"
	}
	return "subagent launch rejected"
}
