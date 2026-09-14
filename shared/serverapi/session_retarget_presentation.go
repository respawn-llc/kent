package serverapi

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type presentedSessionRetargetError struct {
	Message string
	Cause   error
}

func (e *presentedSessionRetargetError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *presentedSessionRetargetError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func PresentSessionRetargetFailure(commandName string, cause error) error {
	if cause == nil {
		return nil
	}
	var retargetErr *SessionRetargetError
	if !errors.As(cause, &retargetErr) {
		return cause
	}
	return &presentedSessionRetargetError{
		Message: SessionRetargetFailureText(commandName, cause),
		Cause:   cause,
	}
}

func SessionRetargetFailureText(commandName string, cause error) string {
	if cause == nil {
		return ""
	}
	var retargetErr *SessionRetargetError
	if !errors.As(cause, &retargetErr) {
		return cause.Error()
	}
	details := fmt.Sprintf(
		"Session %s in source Project %q (%s); target path %q.",
		retargetErr.SessionID,
		retargetErr.SourceProject.Name,
		retargetErr.SourceProject.ID,
		retargetErr.TargetRoot,
	)
	if retargetErr.Reason == SessionRetargetWorkflowOwned && len(retargetErr.WorkflowTaskIDs) > 0 {
		taskIDs := append([]string(nil), retargetErr.WorkflowTaskIDs...)
		sort.Strings(taskIDs)
		details += fmt.Sprintf(" Owning Workflow Task IDs: %s.", strings.Join(taskIDs, ", "))
	}
	guidance := ""
	switch retargetErr.Reason {
	case SessionRetargetTargetProjectRequired:
		guidance = sessionRetargetTargetProjectRequiredGuidance(commandName, retargetErr)
	case SessionRetargetTargetProjectConflict:
		guidance = sessionRetargetTargetProjectConflictGuidance(commandName, retargetErr)
	}
	if guidance == "" {
		return cause.Error() + "\n" + details
	}
	return cause.Error() + "\n" + details + "\n" + guidance
}

func sessionRetargetTargetProjectRequiredGuidance(
	commandName string,
	retargetErr *SessionRetargetError,
) string {
	var guidance strings.Builder
	guidance.WriteString("Choose an attached Project for the target path because it is not attached to the source Project:\n")
	for _, candidate := range retargetErr.SortedCandidateProjects() {
		_, _ = fmt.Fprintf(
			&guidance,
			"Select Project %q (%s): %s\n",
			candidate.Name,
			candidate.ID,
			ShellCommand(commandName, "rebind", "--project", candidate.ID, retargetErr.SessionID, retargetErr.TargetRoot),
		)
	}
	_, _ = fmt.Fprintf(
		&guidance,
		"To keep the source Project, attach the path first: %s\nThen rebind: %s",
		ShellCommand(commandName, "attach", "--project", retargetErr.SourceProject.ID, retargetErr.TargetRoot),
		ShellCommand(commandName, "rebind", retargetErr.SessionID, retargetErr.TargetRoot),
	)
	return guidance.String()
}

func sessionRetargetTargetProjectConflictGuidance(
	commandName string,
	retargetErr *SessionRetargetError,
) string {
	var guidance strings.Builder
	guidance.WriteString("Use one of the Projects already attached to the target path:\n")
	for _, candidate := range retargetErr.SortedCandidateProjects() {
		_, _ = fmt.Fprintf(
			&guidance,
			"Use existing Project %q (%s): %s\n",
			candidate.Name,
			candidate.ID,
			ShellCommand(commandName, "rebind", "--project", candidate.ID, retargetErr.SessionID, retargetErr.TargetRoot),
		)
	}
	return strings.TrimSuffix(guidance.String(), "\n")
}

func ShellCommand(args ...string) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`!*?[]{}();&|<>") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
