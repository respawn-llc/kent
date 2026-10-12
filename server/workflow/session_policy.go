package workflow

import "fmt"

type AssigneeSessionPolicy string

const (
	AssigneeSessionPolicyEstablishTarget AssigneeSessionPolicy = "establish_target"
	AssigneeSessionPolicyPreserve        AssigneeSessionPolicy = "preserve"
)

type AssigneeSessionPolicyRequest struct {
	ContextMode           ContextMode
	ContextSource         ContextSource
	TargetSessionResolved bool
	FanoutBranch          bool
}

func (request AssigneeSessionPolicyRequest) CopiesSourceSession() bool {
	source := CanonicalContextSource(request.ContextSource)
	return request.FanoutBranch && request.TargetSessionResolved && request.ContextMode != ContextModeNewSession &&
		(source.Kind == ContextSourceImmediateSource || source.Kind == ContextSourceSelectedNode)
}

func ResolveAssigneeSessionPolicy(request AssigneeSessionPolicyRequest) (AssigneeSessionPolicy, error) {
	source := CanonicalContextSource(request.ContextSource)
	switch source.Kind {
	case ContextSourceImmediateSource,
		ContextSourceSelectedNode,
		ContextSourcePreviousTarget,
		ContextSourcePreviousTargetOrNew:
	default:
		return "", fmt.Errorf("assignee session policy does not support context source %q", source.Kind)
	}
	switch request.ContextMode {
	case ContextModeNewSession, ContextModeCompactAndContinueSession:
		return AssigneeSessionPolicyEstablishTarget, nil
	case ContextModeContinueSession:
		if request.CopiesSourceSession() {
			return AssigneeSessionPolicyEstablishTarget, nil
		}
		if source.Kind == ContextSourcePreviousTargetOrNew {
			if request.TargetSessionResolved {
				return AssigneeSessionPolicyPreserve, nil
			}
			return AssigneeSessionPolicyEstablishTarget, nil
		}
		return AssigneeSessionPolicyPreserve, nil
	default:
		return "", fmt.Errorf("assignee session policy does not support context mode %q", request.ContextMode)
	}
}
