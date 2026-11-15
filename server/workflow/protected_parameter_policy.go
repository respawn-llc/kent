package workflow

type ProtectedParameterConsumptionPolicy string

const (
	ProtectedParameterConsumptionRequiredValidate ProtectedParameterConsumptionPolicy = "required_validate"
	ProtectedParameterConsumptionIgnoreAuthorized ProtectedParameterConsumptionPolicy = "ignore_authorized"
	ProtectedParameterConsumptionRejectUnknown    ProtectedParameterConsumptionPolicy = "reject_unknown"
)

type ProtectedParameterConsumption struct {
	Assignee ProtectedParameterConsumptionPolicy
	Thinking ProtectedParameterConsumptionPolicy
}

// ForParameter also covers ordinary Parameters so all value-flow consumers use
// the same required/ignored/rejected decision.
func (c ProtectedParameterConsumption) ForParameter(parameter Parameter) ProtectedParameterConsumptionPolicy {
	switch CanonicalParameterPurpose(parameter.Purpose) {
	case ParameterPurposeTargetAssignee:
		return c.Assignee
	case ParameterPurposeTargetThinking:
		return c.Thinking
	default:
		return ProtectedParameterConsumptionRequiredValidate
	}
}
