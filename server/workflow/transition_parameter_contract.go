package workflow

import (
	"fmt"
	"strings"
)

// TransitionParameterContractRequest contains the immutable facts needed to
// prepare one Edge's model-facing completion parameters.
type TransitionParameterContractRequest struct {
	Edge                         Edge
	SourceKind                   NodeKind
	TargetKind                   NodeKind
	TargetRole                   string
	RetainedSession              *AgentExecutionSelection
	FanOut                       bool
	Catalog                      TargetAgentCatalog
	RequireExecutionDescriptions bool
	Materialization              *TransitionSelectionMaterializationRequest
}

type TransitionSelectionMaterializationRequest struct {
	SubmittedRole       string
	SubmittedThinking   string
	ThinkingDescription string
}

type TransitionParameterContract struct {
	Parameters      []Parameter
	KnownParameters []Parameter
	Consumption     ProtectedParameterConsumption
}

type TransitionSelectionPlan struct {
	Applicability      EdgeSelectorApplicability
	Contract           TransitionParameterContract
	ExecutionSelection *AgentExecutionSelection
}

func PlanTransitionSelection(request TransitionParameterContractRequest) (TransitionSelectionPlan, error) {
	policy := resolveTransitionSelectionPolicy(request)
	plan := TransitionSelectionPlan{Applicability: policy.applicability}
	contract, err := policy.parameterContract(request)
	if err != nil {
		return plan, err
	}
	plan.Contract = contract
	if request.Materialization != nil {
		selection, err := policy.materialize(request.TargetRole, request.RetainedSession, *request.Materialization)
		if err != nil {
			return plan, err
		}
		plan.ExecutionSelection = &selection
	}
	return plan, nil
}

func PlanTransitionParameterContract(request TransitionParameterContractRequest) (TransitionParameterContract, error) {
	plan, err := PlanTransitionSelection(request)
	if err != nil {
		return TransitionParameterContract{}, err
	}
	return plan.Contract, nil
}

// PlanTransitionProtectedParameterConsumption derives the protected-parameter
// consumption policy without validating the Edge's ordinary parameter shape.
func PlanTransitionProtectedParameterConsumption(request TransitionParameterContractRequest) ProtectedParameterConsumption {
	return resolveTransitionSelectionPolicy(request).consumption
}

func (p transitionSelectionPolicy) parameterContract(request TransitionParameterContractRequest) (TransitionParameterContract, error) {
	planned := TransitionParameterContract{
		Consumption: p.consumption,
	}
	for _, parameter := range request.Edge.Parameters {
		purpose := CanonicalParameterPurpose(parameter.Purpose)
		if !validParameterPurpose(purpose) {
			return TransitionParameterContract{}, fmt.Errorf("transition parameter %q has invalid purpose %q", parameter.Key, purpose)
		}
		switch p.consumption.ForParameter(parameter) {
		case ProtectedParameterConsumptionRejectUnknown:
			continue
		case ProtectedParameterConsumptionIgnoreAuthorized:
			planned.KnownParameters = append(planned.KnownParameters, parameter)
			continue
		}
		planned.KnownParameters = append(planned.KnownParameters, parameter)
		switch purpose {
		case ParameterPurposeTargetAssignee:
			parameter.Description = transitionParameterDescription(
				parameter.Description,
				DefaultTargetAgentRoleDescription(p.assignee.roles),
			)
		case ParameterPurposeTargetThinking:
			parameter.Description = transitionParameterDescription(
				parameter.Description,
				DefaultTargetAgentThinkingDescription(p.thinking.capability),
			)
			if request.RequireExecutionDescriptions && !p.thinking.capability.Finite && strings.TrimSpace(parameter.Description) == "" {
				return TransitionParameterContract{}, TargetAgentSelectionError{
					Code: TargetAgentSelectionErrorThinkingDescriptionRequired,
				}
			}
		}
		planned.Parameters = append(planned.Parameters, parameter)
	}
	return planned, nil
}

func explicitCallableRoles(catalog TargetAgentCatalog) []TargetAgentRole {
	if catalog == nil {
		return nil
	}
	return catalog.ExplicitCallableRoles()
}

func transitionParameterDescription(authored, derived string) string {
	if strings.TrimSpace(authored) != "" {
		return strings.TrimSpace(authored)
	}
	return strings.TrimSpace(derived)
}

func validParameterPurpose(purpose ParameterPurpose) bool {
	switch purpose {
	case ParameterPurposeOrdinary, ParameterPurposeTargetAssignee, ParameterPurposeTargetThinking:
		return true
	default:
		return false
	}
}
