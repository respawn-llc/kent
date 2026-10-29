package runtimeinput

type InputAdmissionDisposition string

const (
	InputAdmissionStartedTurn  InputAdmissionDisposition = "started_turn"
	InputAdmissionStartedShell InputAdmissionDisposition = "started_shell"
	InputAdmissionPendingSteer InputAdmissionDisposition = "pending_steer"
	InputAdmissionPendingQueue InputAdmissionDisposition = "pending_queue"
)
