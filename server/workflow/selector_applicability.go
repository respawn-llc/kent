package workflow

type SelectorApplicabilityReason string

const (
	SelectorApplicabilityEligible                 SelectorApplicabilityReason = "eligible"
	SelectorApplicabilityTopology                 SelectorApplicabilityReason = "topology"
	SelectorApplicabilityContextSource            SelectorApplicabilityReason = "context_source"
	SelectorApplicabilityNoCallableRoles          SelectorApplicabilityReason = "no_callable_roles"
	SelectorApplicabilityNoThinkingSupport        SelectorApplicabilityReason = "no_thinking_support"
	SelectorApplicabilityUnavailableConfiguration SelectorApplicabilityReason = "unavailable_configuration"
	SelectorApplicabilitySoleCallableRole         SelectorApplicabilityReason = "sole_callable_role"
	SelectorApplicabilityNoThinkingLevels         SelectorApplicabilityReason = "no_thinking_levels"
	SelectorApplicabilitySoleThinkingLevel        SelectorApplicabilityReason = "sole_thinking_level"
)

type SelectorApplicability struct {
	Available        bool
	ParameterVisible bool
	Reason           SelectorApplicabilityReason
}

type EdgeSelectorApplicability struct {
	Assignee SelectorApplicability
	Thinking SelectorApplicability
}

func ResolveEdgeSelectorApplicability(
	edge Edge,
	source NodeKind,
	target NodeKind,
	fanOut bool,
	catalog TargetAgentCatalog,
	targetRole string,
) EdgeSelectorApplicability {
	return resolveTransitionSelectionPolicy(TransitionParameterContractRequest{
		Edge:       edge,
		SourceKind: source,
		TargetKind: target,
		TargetRole: targetRole,
		FanOut:     fanOut,
		Catalog:    catalog,
	}).applicability
}
