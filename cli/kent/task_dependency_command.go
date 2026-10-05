package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

type taskDependencyMutationKind string

const (
	taskDependencyMutationAdd    taskDependencyMutationKind = "add"
	taskDependencyMutationRemove taskDependencyMutationKind = "remove"
)

func taskDependencySubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return dispatchCommandGroup(args, stdout, stderr, commandGroup{
		path:  "task dep",
		usage: taskDependencyUsage,
		routes: map[string]commandHandler{
			"add":    taskDependencyAddSubcommand,
			"remove": taskDependencyRemoveSubcommand,
			"list":   taskDependencyListSubcommand,
		},
	})
}

func taskDependencyAddSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return taskDependencyMutationSubcommand(taskDependencyMutationAdd, args, stdout, stderr)
}

func taskDependencyRemoveSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	return taskDependencyMutationSubcommand(taskDependencyMutationRemove, args, stdout, stderr)
}

func taskDependencyMutationSubcommand(kind taskDependencyMutationKind, args []string, stdout io.Writer, stderr io.Writer) int {
	var usage commandUsage
	switch kind {
	case taskDependencyMutationAdd:
		usage = taskDependencyAddUsage
	case taskDependencyMutationRemove:
		usage = taskDependencyRemoveUsage
	default:
		panic(fmt.Sprintf("unsupported task dependency mutation %q", kind))
	}
	fs := newCommandFlagSet(config.Command+" task dep "+string(kind), stderr, usage)
	blockerRef := fs.String("blocker", "", "Blocker Task ID or project-scoped Short ID")
	blockedRef := fs.String("blocked", "", "Blocked Task ID or project-scoped Short ID")
	projectRef := fs.String("project", ".", "project ID or attached workspace path used to resolve Short IDs")
	jsonOut := fs.Bool("json", false, "write the typed dependency outcome as JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintf(stderr, "task dep %s does not accept positional arguments\n", kind)
		return 2
	}
	if strings.TrimSpace(*blockerRef) == "" || strings.TrimSpace(*blockedRef) == "" {
		fmt.Fprintf(stderr, "task dep %s requires --blocker <task> and --blocked <task>\n", kind)
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.Connection, remote *client.Remote) int {
		blockerTaskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, remote, *projectRef, *blockerRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		blockedTaskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, remote, *projectRef, *blockedRef)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		defer cancel()
		var response *taskpb.DependencyMutationSuccess
		switch kind {
		case taskDependencyMutationAdd:
			result, err := remote.AddWorkflowTaskDependency(ctx, &taskpb.DependencyAddRequest{
				BlockerTaskId: blockerTaskID,
				BlockedTaskId: blockedTaskID,
			})
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if err := protoapi.Validate(result); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			response = result
		case taskDependencyMutationRemove:
			result, err := remote.RemoveWorkflowTaskDependency(ctx, &taskpb.DependencyRemoveRequest{
				BlockerTaskId: blockerTaskID,
				BlockedTaskId: blockedTaskID,
			})
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if err := protoapi.Validate(result); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			response = result
		default:
			panic(fmt.Sprintf("unsupported task dependency mutation %q", kind))
		}
		if *jsonOut {
			output, err := taskDependencyMutationOutput(response)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return writeCommandJSON(stdout, stderr, output)
		}
		fmt.Fprintln(stdout, "done")
		return 0
	})
}

func taskDependencyListSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" task dep list", stderr, taskDependencyListUsage)
	projectRef := fs.String("project", ".", "project ID or attached workspace path used to resolve a Short ID")
	directionRaw := fs.String("direction", "", "relationship direction: blocks or blocked-by")
	jsonOut := fs.Bool("json", false, "write the complete dependency directions as JSON")
	positionals, flagArgs := takeLeadingPositionals(args, 1)
	if ok, exitCode := parseCommandFlags(fs, flagArgs); !ok {
		return exitCode
	}
	positionals = append(positionals, fs.Args()...)
	if len(positionals) != 1 {
		fmt.Fprintln(stderr, "task dep list requires <short-id-or-task-id>")
		return 2
	}
	if flagExplicit(fs, "direction") && strings.TrimSpace(*directionRaw) == "" {
		fmt.Fprintln(stderr, "--direction must not be blank")
		return 2
	}
	direction, err := parseTaskDependencyDirection(*directionRaw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return runWorkflowCommandSession(stderr, func(cfg config.Connection, remote *client.Remote) int {
		taskID, err := resolveWorkflowTaskID(context.Background(), cfg, remote, remote, *projectRef, positionals[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		ctx, cancel := context.WithTimeout(context.Background(), workflowCommandTimeout)
		defer cancel()
		response, err := remote.ListWorkflowTaskDependencies(ctx, &taskpb.DependencyListRequest{
			TaskId:    taskID,
			Direction: direction,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := protoapi.Validate(response); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if *jsonOut {
			output, err := taskDependencyListOutput(response)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return writeCommandJSON(stdout, stderr, output)
		}
		if err := writeTaskDependencyDirections(stdout, response.Directions); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	})
}

func parseTaskDependencyDirection(raw string) (*taskpb.DependencyDirection, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return nil, nil
	case "blocks":
		value := taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS
		return &value, nil
	case "blocked-by":
		value := taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY
		return &value, nil
	default:
		return nil, errors.New("--direction must be blocks or blocked-by")
	}
}

type taskDependencyDirection interface {
	GetDirection() taskpb.DependencyDirection
	GetTotalCount() int32
	GetItems() []*taskpb.DependencyItem
}

func writeTaskDependencyDirections[T taskDependencyDirection](stdout io.Writer, directions []T) error {
	for _, direction := range taskDependencyDirectionsForRender(directions) {
		if direction.GetDirection() == taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS {
			fmt.Fprintf(stdout, "Blocks %d tasks:\n", direction.GetTotalCount())
		} else {
			fmt.Fprintln(stdout, "Blocked by:")
		}
		for _, item := range direction.GetItems() {
			status, err := taskStatusText(item.Status)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%s: %s (%s)\n", item.ShortId, item.Title, status)
		}
	}
	return nil
}

func taskDependencyDirectionsForRender[T taskDependencyDirection](directions []T) []T {
	ordered := make([]T, 0, len(directions))
	for _, wanted := range []taskpb.DependencyDirection{
		taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS,
		taskpb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY,
	} {
		for _, direction := range directions {
			if direction.GetDirection() == wanted && len(direction.GetItems()) > 0 {
				ordered = append(ordered, direction)
			}
		}
	}
	return ordered
}
