package workflow

import (
	"fmt"
	"maps"
)

// ResumePromptParameters projects current prompt inputs without reconstructing
// historical Transition values. Existing materialized inputs remain unchanged.
func ResumePromptParameters(
	request TransitionParameterContractRequest,
	effectiveRole TargetAgentRole,
	currentValues map[string]string,
) (map[string]string, error) {
	references, err := ExtractPromptTemplateReferences(request.Edge.PromptTemplate)
	if err != nil {
		return nil, err
	}
	consumption := resumeProtectedParameterConsumption(request, effectiveRole)
	values := maps.Clone(currentValues)
	for _, reference := range references.Params {
		for _, parameter := range request.Edge.Parameters {
			if parameter.Key != reference.Name || CanonicalParameterPurpose(parameter.Purpose) == ParameterPurposeOrdinary {
				continue
			}
			if _, present := values[parameter.Key]; present {
				continue
			}
			if consumption.ForParameter(parameter) != ProtectedParameterConsumptionRequiredValidate {
				return nil, fmt.Errorf("Resume prompt Parameter %q is not exposed for the effective Agent", parameter.Key)
			}
			if values == nil {
				values = make(map[string]string)
			}
			switch CanonicalParameterPurpose(parameter.Purpose) {
			case ParameterPurposeTargetAssignee:
				values[parameter.Key] = effectiveRole.Identity
			case ParameterPurposeTargetThinking:
				thinking := effectiveRole.ConfiguredThinking
				if thinking == "" {
					thinking = "model default"
				}
				values[parameter.Key] = thinking
			}
		}
	}
	return values, nil
}
