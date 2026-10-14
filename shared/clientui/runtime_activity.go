package clientui

import (
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
)

type RuntimeSubmitRequest struct {
	Input runtimeinput.Input
}

type RuntimeShellRequest struct {
	Command string
}

type RuntimeCompactRequest struct {
	RequestID runtimeids.CompactionRequestID
	Admission runtimeinput.ManualCompactionAdmission
}
