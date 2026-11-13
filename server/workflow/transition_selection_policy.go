package workflow

// transitionSelectionPolicy resolves catalog and topology decisions once.
// Editor availability does not depend on whether a selector is enabled or a
// saved Session will be reused. Runtime thinking uses that Session's role.
type transitionSelectionPolicy struct {
	applicability    EdgeSelectorApplicability
	consumption      ProtectedParameterConsumption
	assignee         assigneeSelectionPolicy
	thinking         thinkingSelectionPolicy
	configuredRole   *TargetAgentRole
	retainedRole     *TargetAgentRole
	assigneeOverride bool
	thinkingOverride bool
}

type assigneeSelectionPolicy struct {
	roles         []TargetAgentRole
	applicability SelectorApplicability
}

func resolveAssigneeSelectionPolicy(roles []TargetAgentRole) assigneeSelectionPolicy {
	policy := assigneeSelectionPolicy{roles: canonicalExplicitCallableRoles(roles)}
	switch len(policy.roles) {
	case 0:
		policy.applicability = SelectorApplicability{Reason: SelectorApplicabilityNoCallableRoles}
	case 1:
		policy.applicability = SelectorApplicability{Available: true, Reason: SelectorApplicabilitySoleCallableRole}
	default:
		policy.applicability = SelectorApplicability{Available: true, ParameterVisible: true, Reason: SelectorApplicabilityEligible}
	}
	return policy
}

type thinkingSelectionPolicy struct {
	capability    ThinkingCapability
	applicability SelectorApplicability
}

func resolveThinkingSelectionPolicy(roles []TargetAgentRole) thinkingSelectionPolicy {
	policy := thinkingSelectionPolicy{capability: UnionTargetAgentThinkingCapabilities(roles)}
	switch {
	case len(roles) == 0:
		policy.applicability = SelectorApplicability{Reason: SelectorApplicabilityUnavailableConfiguration}
	case !policy.capability.ReasoningCapable:
		policy.applicability = SelectorApplicability{Reason: SelectorApplicabilityNoThinkingSupport}
	case policy.capability.Finite && len(policy.capability.Levels) == 0:
		policy.applicability = SelectorApplicability{Available: true, Reason: SelectorApplicabilityNoThinkingLevels}
	case policy.capability.Finite && len(policy.capability.Levels) == 1:
		policy.applicability = SelectorApplicability{Available: true, Reason: SelectorApplicabilitySoleThinkingLevel}
	default:
		policy.applicability = SelectorApplicability{Available: true, ParameterVisible: true, Reason: SelectorApplicabilityEligible}
	}
	return policy
}

func resolveTransitionSelectionPolicy(request TransitionParameterContractRequest) transitionSelectionPolicy {
	policy := transitionSelectionPolicy{
		assignee: resolveAssigneeSelectionPolicy(explicitCallableRoles(request.Catalog)),
	}
	policy.configuredRole = resolveConfiguredTargetRole(request.Catalog, request.TargetRole)
	var roles []TargetAgentRole
	if CanonicalAssigneeSelection(request.Edge.AssigneeSelection) == AssigneeSelectionPreviousNode {
		roles = policy.assignee.roles
	} else if policy.configuredRole != nil {
		roles = []TargetAgentRole{*policy.configuredRole}
	}
	policy.thinking = resolveThinkingSelectionPolicy(roles)
	policy.applicability = EdgeSelectorApplicability{
		Assignee: policy.assignee.applicability,
		Thinking: policy.thinking.applicability,
	}
	eligible := !request.FanOut &&
		(request.SourceKind == NodeKindAgent || request.SourceKind == NodeKindScript) &&
		request.TargetKind == NodeKindAgent
	assigneeContext := request.Edge.ContextMode != ContextModeContinueSession ||
		CanonicalContextSource(request.Edge.ContextSource).Kind == ContextSourcePreviousTargetOrNew
	if !eligible {
		policy.applicability = EdgeSelectorApplicability{
			Assignee: SelectorApplicability{Reason: SelectorApplicabilityTopology},
			Thinking: SelectorApplicability{Reason: SelectorApplicabilityTopology},
		}
	} else if !assigneeContext {
		policy.applicability.Assignee = SelectorApplicability{Reason: SelectorApplicabilityContextSource}
	}
	if retained := request.RetainedSession; retained != nil {
		policy.retainedRole = resolveConfiguredTargetRole(request.Catalog, retained.Assignee)
		role := TargetAgentRole{Identity: retained.Assignee}
		if policy.retainedRole != nil {
			role = *policy.retainedRole
		}
		policy.thinking = resolveThinkingSelectionPolicy([]TargetAgentRole{role})
	}
	policy.assigneeOverride = eligible && assigneeContext &&
		CanonicalAssigneeSelection(request.Edge.AssigneeSelection) == AssigneeSelectionPreviousNode
	policy.thinkingOverride = eligible &&
		CanonicalThinkingSelection(request.Edge.ThinkingSelection) == ThinkingSelectionPreviousNode
	policy.resolveConsumption(request.RetainedSession != nil)
	return policy
}

// Resume uses the actual saved model's capabilities, not an edited fallback
// role or the union of models that could be chosen on a new entry.
func resumeProtectedParameterConsumption(request TransitionParameterContractRequest, effectiveRole TargetAgentRole) ProtectedParameterConsumption {
	policy := resolveTransitionSelectionPolicy(request)
	policy.thinking = resolveThinkingSelectionPolicy([]TargetAgentRole{effectiveRole})
	policy.resolveConsumption(request.RetainedSession != nil)
	return policy.consumption
}

func (policy *transitionSelectionPolicy) resolveConsumption(retainedSession bool) {
	policy.consumption = ProtectedParameterConsumption{
		Assignee: selectorConsumption(policy.assigneeOverride, policy.assignee.applicability),
		Thinking: selectorConsumption(policy.thinkingOverride, policy.thinking.applicability),
	}
	if policy.assigneeOverride && retainedSession {
		policy.consumption.Assignee = ProtectedParameterConsumptionIgnoreAuthorized
	}
}

func resolveConfiguredTargetRole(catalog TargetAgentCatalog, name string) *TargetAgentRole {
	if catalog != nil {
		if role, ok := catalog.ResolveConfiguredRole(name); ok {
			role = canonicalTargetAgentRole(role)
			return &role
		}
	}
	return nil
}

func selectorConsumption(enabled bool, availability SelectorApplicability) ProtectedParameterConsumptionPolicy {
	if !enabled || !availability.Available {
		return ProtectedParameterConsumptionRejectUnknown
	}
	if availability.ParameterVisible {
		return ProtectedParameterConsumptionRequiredValidate
	}
	return ProtectedParameterConsumptionIgnoreAuthorized
}
