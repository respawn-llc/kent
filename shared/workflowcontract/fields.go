package workflowcontract

type ValidationErrorReason string

const (
	ValidationErrorReasonSessionSourceCannotOwnSession  ValidationErrorReason = "session_source_cannot_own_session"
	ValidationErrorReasonSessionTransitionMissing       ValidationErrorReason = "session_transition_missing"
	ValidationErrorReasonSessionTransitionNotGuaranteed ValidationErrorReason = "session_transition_not_guaranteed"
	ValidationErrorReasonSessionTransitionAmbiguous     ValidationErrorReason = "session_transition_ambiguous"
)

const (
	MaxOutputFieldNameChars          = 64
	MaxOutputFieldDescriptionChars   = 1000
	MaxParameterKeyChars             = MaxOutputFieldNameChars
	MaxParameterDescriptionChars     = MaxOutputFieldDescriptionChars
	RuntimePromptParameterCommentary = "commentary"
	RuntimePromptParameterSessionID  = "session_id"
)
