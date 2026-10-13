package runtimeinput

type InputAdmissionDisposition string

const (
	InputAdmissionStartedTurn  InputAdmissionDisposition = "started_turn"
	InputAdmissionPendingSteer InputAdmissionDisposition = "pending_steer"
	InputAdmissionPendingQueue InputAdmissionDisposition = "pending_queue"
)
