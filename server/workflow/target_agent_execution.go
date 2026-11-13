package workflow

import "strings"

func (p transitionSelectionPolicy) materialize(fallbackRole string, retainedSession *AgentExecutionSelection, request TransitionSelectionMaterializationRequest) (AgentExecutionSelection, error) {
	if retainedSession != nil {
		selection := retainedSession.Clone()
		selection.Origin = AssigneeOriginRetainedSession
		role := TargetAgentRole{Identity: selection.Assignee}
		if p.retainedRole != nil {
			role = *p.retainedRole
		}
		if p.thinkingOverride || p.retainedRole != nil {
			thinking, err := p.thinking.selectThinking(role, p.thinkingOverride, request.SubmittedThinking, request.ThinkingDescription)
			if err != nil {
				return AgentExecutionSelection{}, err
			}
			selection.Thinking = thinking
		}
		if err := selection.Validate(); err != nil {
			return AgentExecutionSelection{}, err
		}
		return selection, nil
	}

	var role TargetAgentRole
	var err error
	if p.assigneeOverride {
		role, err = p.assignee.selectRole(request.SubmittedRole)
	} else {
		role, err = configuredTargetAgentSelection(p.configuredRole, fallbackRole)
	}
	if err != nil {
		return AgentExecutionSelection{}, err
	}

	thinking, err := p.thinking.selectThinking(role, p.thinkingOverride, request.SubmittedThinking, request.ThinkingDescription)
	if err != nil {
		return AgentExecutionSelection{}, err
	}

	origin := AssigneeOriginConfiguredFallback
	if p.assigneeOverride {
		origin = AssigneeOriginTransitionSelected
	}
	return NewAgentExecutionSelection(role.Identity, thinking, origin)
}

func materializeThinkingValue(raw string) (*ThinkingValue, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := NewThinkingValue(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}
