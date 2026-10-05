package clientui

import (
	"fmt"
	"strings"

	"core/shared/runtimeids"
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
	RequestID runtimeids.CompactionRequestID
	Admission runtimeinput.ManualCompactionAdmission
}

func (r RuntimeCompactRequest) Validate() error {
	if r.RequestID.IsZero() {
		return fmt.Errorf("compaction request id is required")
	}
	return r.Admission.Validate()
}
