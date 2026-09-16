package subagentpolicy

import (
	"core/shared/config"
	"core/shared/serverapi"
)

func Authorize(settings config.Settings, caller *Caller, target Target) error {
	context := config.SubagentInvocationContextOrdinary
	if caller != nil && caller.Workflow {
		context = config.SubagentInvocationContextWorkflow
	}
	selector := target.Selector
	switch target.Kind {
	case TargetOmittedBase, TargetExplicitBase:
		selector = config.DefaultSubagentRole
	case TargetNamed:
	default:
		return denial(serverapi.SubagentLaunchDenialInvalidTarget, nil, nil)
	}
	lookup := config.LookupSubagentRole(settings, selector)
	if lookup.Status == config.SubagentRoleLookupInvalid {
		return denial(serverapi.SubagentLaunchDenialInvalidTarget, nil, nil)
	}
	if lookup.Status == config.SubagentRoleLookupMissing {
		return denial(serverapi.SubagentLaunchDenialTargetMissing, lookup.NormalizedSelector, available(settings, context))
	}
	if caller == nil {
		return nil
	}
	if !roleTargetAllowed(settings, context, lookup) {
		return denial(serverapi.SubagentLaunchDenialNotCallable, lookup.NormalizedSelector, available(settings, context))
	}
	return nil
}

func roleTargetAllowed(settings config.Settings, context config.SubagentInvocationContext, lookup config.SubagentRoleLookup) bool {
	if lookup.Status != config.SubagentRoleLookupPresent || !config.SubagentRoleCallable(lookup.Role) {
		return false
	}
	return context != config.SubagentInvocationContextWorkflow ||
		(settings.Workflow.Subagents && config.SubagentRoleWorkflowCallable(lookup.Role))
}
