package clientui

import (
	"core/shared/runtimeinput"
)

type RuntimeSubmitRequest struct {
	Input runtimeinput.Input
}

type RuntimeShellRequest struct {
	Command string
}

type RuntimeCompactRequest struct {
	Admission runtimeinput.ManualCompactionAdmission
}

type RuntimeCompactResult struct {
	Diagnostic error
}
