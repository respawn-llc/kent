package serverapi

import (
	"fmt"
	"strings"
)

type SubagentLaunchDenialKind string

const (
	SubagentLaunchDenialInvalidTarget SubagentLaunchDenialKind = "invalid_target"
	SubagentLaunchDenialTargetMissing SubagentLaunchDenialKind = "target_missing"
	SubagentLaunchDenialNotCallable   SubagentLaunchDenialKind = "not_callable"
	SubagentLaunchDenialCallerMissing SubagentLaunchDenialKind = "caller_missing"
	SubagentLaunchDenialParentMissing SubagentLaunchDenialKind = "parent_missing"
)

// SubagentLaunchDeniedError is the structured cross-process contract for a
// server-owned model-originated launch denial.
type SubagentLaunchDeniedError struct {
	Kind           SubagentLaunchDenialKind `json:"kind"`
	Target         *string                  `json:"target,omitempty"`
	AvailableRoles []string                 `json:"available_roles,omitempty"`
}

func (e *SubagentLaunchDeniedError) Error() string {
	if e == nil {
		return "subagent launch denied"
	}
	return fmt.Sprintf("subagent launch denied: %s", strings.TrimSpace(string(e.Kind)))
}
