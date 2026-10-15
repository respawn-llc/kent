package clientui

import (
	"fmt"
	"strings"

	"core/shared/runtimeinput"
)

type RuntimeSubmitRequest struct {
	Input runtimeinput.Input
}

func (r RuntimeSubmitRequest) Validate() error {
	return r.Input.Validate()
}

type RuntimeShellRequest struct {
	Command string
}

func (r RuntimeShellRequest) Validate() error {
	if strings.TrimSpace(r.Command) == "" {
		return fmt.Errorf("shell command is required")
	}
	return nil
}

type RuntimeCompactRequest struct {
	Admission runtimeinput.ManualCompactionAdmission
}

type RuntimeCompactResult struct {
	Diagnostic error
}

func (r RuntimeCompactRequest) Validate() error {
	return r.Admission.Validate()
}
