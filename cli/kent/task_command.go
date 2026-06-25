package main

import (
	"fmt"
	"io"

	"core/shared/config"
)

func taskSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := newCommandFlagSet(config.Command+" task", stderr, taskUsage)
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "create":
		return taskCreateSubcommand(args[1:], stdout, stderr)
	case "edit":
		return taskEditSubcommand(args[1:], stdout, stderr)
	case "start":
		return taskStartSubcommand(args[1:], stdout, stderr)
	case "list":
		return taskListSubcommand(args[1:], stdout, stderr)
	case "show":
		return taskShowSubcommand(args[1:], stdout, stderr)
	case "cancel":
		return taskCancelSubcommand(args[1:], stdout, stderr)
	case "delete":
		return taskDeleteSubcommand(args[1:], stdout, stderr)
	case "approve":
		return taskApproveSubcommand(args[1:], stdout, stderr)
	case "move":
		return taskMoveSubcommand(args[1:], stdout, stderr)
	case "complete":
		return taskCompleteSubcommand(args[1:], stdout, stderr)
	case "resume":
		return taskResumeSubcommand(args[1:], stdout, stderr)
	case "comment", "comments":
		return taskCommentSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown task command: %s\n\n", args[0])
		fs := newCommandFlagSet(config.Command+" task", stderr, taskUsage)
		taskUsage.write(fs)
		return 2
	}
}
