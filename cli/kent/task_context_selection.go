package main

import (
	"errors"
	"fmt"
	"io"

	"core/shared/serverapi"
)

func writeWorkflowTaskContextSelectionError(stderr io.Writer, err error) bool {
	var restriction *serverapi.WorkflowTaskContextSelectionRequiredError
	if !errors.As(err, &restriction) {
		return false
	}
	fmt.Fprintf(stderr, "Task %s needs an explicit Move to select its context before it can continue. Move replaces all current Task positions and resets Join progress; existing chats, files, and history are preserved.\n", restriction.TaskID)
	return true
}
