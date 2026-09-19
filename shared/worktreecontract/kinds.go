package worktreecontract

type DirtyStateKind string
type SetupFailureKind string
type SetupRequirement string
type TransitionKind string

const (
	DirtyStateClean                     DirtyStateKind   = "clean"
	DirtyStateDirty                     DirtyStateKind   = "dirty"
	DirtyStateUnknown                   DirtyStateKind   = "unknown"
	SetupRequirementRequired            SetupRequirement = "required"
	SetupRequirementAlreadyCompleted    SetupRequirement = "already_completed"
	SetupFailureProcessExit             SetupFailureKind = "process_exit"
	SetupFailureTimeout                 SetupFailureKind = "timeout"
	SetupFailureTargetPreparation       SetupFailureKind = "target_preparation"
	SetupFailureInterruptionPersistence SetupFailureKind = "interruption_persistence"
	SetupFailureCanceled                SetupFailureKind = "canceled"
	SetupFailureControllerShutdown      SetupFailureKind = "controller_shutdown"
	SetupFailureOperational             SetupFailureKind = "operational"
	TransitionDelete                    TransitionKind   = "delete"
)

func IsRetryReadySetupFailure(kind SetupFailureKind) bool {
	return kind == SetupFailureProcessExit || kind == SetupFailureTimeout || kind == SetupFailureTargetPreparation || kind == SetupFailureOperational
}
func IsNonRetryableSetupFailure(kind SetupFailureKind) bool {
	return kind == SetupFailureInterruptionPersistence || kind == SetupFailureCanceled || kind == SetupFailureControllerShutdown
}
func HasFixedRetryReadiness(kind SetupFailureKind) bool { return kind != SetupFailureOperational }
func IsValidSetupRequirement(requirement SetupRequirement) bool {
	return requirement == SetupRequirementRequired || requirement == SetupRequirementAlreadyCompleted
}
