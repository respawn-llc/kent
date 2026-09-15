package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"core/cli/tui"
	"core/prompts"
	"core/shared/client"
	"core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/serverapi"
	"core/shared/sessionenv"
	"core/shared/textutil"
)

type goalRuntimeUnavailablePresentationError struct {
	SessionID string
}

func (e goalRuntimeUnavailablePresentationError) Error() string {
	return fmt.Sprintf(
		"No session is currently running under that goal, and updating it without an active run can make the model confused. Please start session %s.",
		e.SessionID,
	)
}

type goalCommandRemote interface {
	ShowGoal(context.Context, *runtimepb.GoalShowRequest) (*runtimepb.GoalShowSuccess, error)
	SetGoal(context.Context, *runtimepb.GoalSetRequest) (*runtimepb.GoalSetSuccess, error)
	PauseGoal(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)
	ResumeGoal(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)
	CompleteGoal(context.Context, *runtimepb.GoalMutationRequest) (*runtimepb.GoalMutationSuccess, error)
	ClearGoal(context.Context, *runtimepb.GoalClearRequest) (*runtimepb.GoalMutationSuccess, error)
	Close() error
}

var goalCommandRemoteOpener = openGoalCommandRemote

func withGoalCommandRemote(stderr io.Writer, run func(context.Context, goalCommandRemote) int) int {
	ctx, cancel := context.WithTimeout(context.Background(), client.GoalRequestTimeout)
	defer cancel()
	remote, err := goalCommandRemoteOpener(ctx)
	if err != nil {
		fmt.Fprintln(stderr, client.PresentGoalRequestError(err))
		return 1
	}
	defer func() { _ = remote.Close() }()
	return run(ctx, remote)
}

func goalSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fs := newCommandFlagSet(config.Command+" goal", stderr, goalUsage)
		fs.Usage()
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	action := strings.TrimSpace(args[0])
	switch action {
	case "show":
		return goalShowSubcommand(args[1:], stdout, stderr)
	case "set":
		return goalSetSubcommand(args[1:], stdout, stderr)
	case "pause":
		return goalStatusSubcommand("pause", args[1:], stdout, stderr)
	case "resume":
		return goalStatusSubcommand("resume", args[1:], stdout, stderr)
	case "complete":
		return goalCompleteSubcommand(args[1:], stdout, stderr)
	case "clear":
		return goalClearSubcommand(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown goal command: %s\n\n", action)
		fs := newCommandFlagSet(config.Command+" goal", stderr, goalUsage)
		goalUsage.write(fs)
		return 2
	}
}

func goalShowSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" goal show", stderr, goalShowUsage)
	sessionFlag := fs.String("session", "", "session to inspect; required outside Kent shell commands")
	jsonOut := fs.Bool("json", false, "write the complete goal response as JSON")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "goal show does not accept positional arguments")
		return 2
	}
	target, _, err := resolveGoalCommandSession(*sessionFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return withGoalCommandRemote(stderr, func(ctx context.Context, remote goalCommandRemote) int {
		resp, err := remote.ShowGoal(ctx, &runtimepb.GoalShowRequest{SessionId: target})
		if err != nil {
			fmt.Fprintln(stderr, client.PresentGoalRequestError(err))
			return 1
		}
		if *jsonOut {
			return writeGoalShowJSON(stdout, stderr, resp)
		}
		return writeGoalShowText(stdout, stderr, resp.Goal)
	})
}

func goalSetSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" goal set", stderr, goalSetUsage)
	sessionFlag := fs.String("session", "", "session to update; required outside Kent shell commands")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	target, agent, err := resolveGoalCommandSession(*sessionFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	objective := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if objective == "" {
		fmt.Fprintln(stderr, "goal set requires an objective")
		return 2
	}
	actor := "user"
	var runID, stepID *string
	if agent {
		actor = "agent"
		run, step := sessionenv.LookupRunStepID(os.LookupEnv)
		runID, stepID = textutil.OptionalTrimmedString(run), textutil.OptionalTrimmedString(step)
	}
	return withGoalCommandRemote(stderr, func(ctx context.Context, remote goalCommandRemote) int {
		resp, err := remote.SetGoal(ctx, &runtimepb.GoalSetRequest{
			Target: &chatpb.ChatTarget{
				Target: &chatpb.ChatTarget_Session{
					Session: &chatpb.ExistingSessionTarget{SessionId: target},
				},
			},
			Objective:       objective,
			Actor:           actor,
			ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_PRESERVE_RUNTIME_STATE,
			RunId:           runID,
			StepId:          stepID,
		})
		if err != nil {
			fmt.Fprintln(stderr, goalMutationCommandError(target, err))
			return 1
		}
		return writeGoalSetResult(stdout, stderr, resp, target)
	})
}

func goalStatusSubcommand(action string, args []string, stdout io.Writer, stderr io.Writer) int {
	usage := goalPauseUsage
	if action == "resume" {
		usage = goalResumeUsage
	}
	fs := newCommandFlagSet(config.Command+" goal "+action, stderr, usage)
	sessionFlag := fs.String("session", "", "session to update; required outside Kent shell commands")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintf(stderr, "goal %s does not accept positional arguments\n", action)
		return 2
	}
	target, agent, err := resolveGoalCommandSession(*sessionFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if agent {
		fmt.Fprintln(stderr, prompts.RenderGoalAgentCommandDeniedPrompt())
		return 1
	}
	req := &runtimepb.GoalMutationRequest{SessionId: target, Actor: "user"}
	return withGoalCommandRemote(stderr, func(ctx context.Context, remote goalCommandRemote) int {
		var (
			resp    *runtimepb.GoalMutationSuccess
			callErr error
		)
		if action == "pause" {
			resp, callErr = remote.PauseGoal(ctx, req)
		} else {
			resp, callErr = remote.ResumeGoal(ctx, req)
		}
		if callErr != nil {
			fmt.Fprintln(stderr, goalMutationCommandError(target, callErr))
			return 1
		}
		return writeGoalMutationResult(stdout, stderr, resp)
	})
}

func goalCompleteSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" goal complete", stderr, goalCompleteUsage)
	sessionFlag := fs.String("session", "", "session to update; required outside Kent shell commands")
	confirmed := fs.Bool("confirm", false, "allow an agent to mark the goal complete")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "goal complete does not accept positional arguments")
		return 2
	}
	target, agent, err := resolveGoalCommandSession(*sessionFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return withGoalCommandRemote(stderr, func(ctx context.Context, remote goalCommandRemote) int {
		current, err := remote.ShowGoal(ctx, &runtimepb.GoalShowRequest{SessionId: target})
		if err != nil {
			fmt.Fprintln(stderr, client.PresentGoalRequestError(err))
			return 1
		}
		if goalAlreadyComplete(current.Goal) {
			fmt.Fprintln(stdout, prompts.RenderGoalAlreadyCompletePrompt(current.Goal.Objective))
			return 0
		}
		if agent && !*confirmed {
			objective := ""
			if current.Goal != nil {
				objective = current.Goal.Objective
			}
			fmt.Fprintln(stderr, prompts.RenderGoalCompleteConfirmRequiredPrompt(objective))
			return 1
		}
		actor := "user"
		var runID, stepID *string
		if agent {
			actor = "agent"
			run, step := sessionenv.LookupRunStepID(os.LookupEnv)
			runID, stepID = textutil.OptionalTrimmedString(run), textutil.OptionalTrimmedString(step)
		}
		response, err := remote.CompleteGoal(ctx, &runtimepb.GoalMutationRequest{SessionId: target, Actor: actor, RunId: runID, StepId: stepID})
		if err != nil {
			fmt.Fprintln(stderr, goalMutationCommandError(target, err))
			return 1
		}
		if err := protoapi.Validate(response); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if response.Kind != runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL ||
			response.Goal == nil ||
			response.Goal.Status != runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_COMPLETE {
			fmt.Fprintln(stderr, "Goal completion response did not contain an authoritative completed Goal")
			return 1
		}
		fmt.Fprintln(stdout, "Goal marked as completed, changes will come into effect in a few seconds. After that you may end your turn normally.")
		return 0
	})
}

func goalAlreadyComplete(goal *runtimepb.Goal) bool {
	return goal != nil && goal.Status == runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_COMPLETE
}

func goalClearSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" goal clear", stderr, goalClearUsage)
	sessionFlag := fs.String("session", "", "session to update; required outside Kent shell commands")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "goal clear does not accept positional arguments")
		return 2
	}
	target, agent, err := resolveGoalCommandSession(*sessionFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if agent {
		fmt.Fprintln(stderr, prompts.RenderGoalAgentCommandDeniedPrompt())
		return 1
	}
	return withGoalCommandRemote(stderr, func(ctx context.Context, remote goalCommandRemote) int {
		resp, err := remote.ClearGoal(ctx, &runtimepb.GoalClearRequest{SessionId: target, Actor: "user"})
		if err != nil {
			fmt.Fprintln(stderr, goalMutationCommandError(target, err))
			return 1
		}
		return writeGoalMutationResult(stdout, stderr, resp)
	})
}

func resolveGoalCommandSession(sessionFlag string) (sessionID string, agent bool, err error) {
	if envSessionID, ok := sessionenv.LookupSessionID(os.LookupEnv); ok {
		return envSessionID, true, nil
	}
	trimmed := strings.TrimSpace(sessionFlag)
	if trimmed == "" {
		return "", false, errors.New("goal command requires --session outside " + config.Product + " shell commands")
	}
	return trimmed, false, nil
}

func openGoalCommandRemote(ctx context.Context) (goalCommandRemote, error) {
	cfg, err := config.Load(".", config.LoadOptions{})
	if err != nil {
		return nil, err
	}
	remote, err := client.DialConfiguredRemote(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// When the operator selected an explicit non-default persistence root, only
	// operate on a server actually serving that root so goal commands never read
	// or mutate a different instance reachable on the same TCP endpoint.
	if err := remote.RequireRoot(config.ExplicitPersistenceRootID(cfg)); err != nil {
		_ = remote.Close()
		return nil, err
	}
	return remote, nil
}

func writeGoalShowText(stdout io.Writer, stderr io.Writer, goal *runtimepb.Goal) int {
	if goal == nil {
		fmt.Fprintln(stdout, "No goal")
		return 0
	}
	status, err := tui.GoalStatusLabel(goal.Status)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "Goal: %s\nStatus: %s\n", goal.Objective, status)
	return 0
}

func writeGoalShowJSON(stdout io.Writer, stderr io.Writer, response *runtimepb.GoalShowSuccess) int {
	if err := protoapi.Validate(response); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := struct {
		Goal *struct {
			ID        string    `json:"id"`
			Objective string    `json:"objective"`
			Status    string    `json:"status"`
			CreatedAt time.Time `json:"created_at"`
			UpdatedAt time.Time `json:"updated_at"`
		} `json:"goal,omitempty"`
		Availability string `json:"availability"`
	}{}
	switch response.Availability {
	case runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE:
		output.Availability = "available"
	case runtimepb.GoalAvailability_GOAL_AVAILABILITY_AGENT_CAPABILITY_MISSING:
		output.Availability = "agent_capability_missing"
	}
	if goal := response.Goal; goal != nil {
		status, err := tui.GoalStatusLabel(goal.Status)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		output.Goal = &struct {
			ID        string    `json:"id"`
			Objective string    `json:"objective"`
			Status    string    `json:"status"`
			CreatedAt time.Time `json:"created_at"`
			UpdatedAt time.Time `json:"updated_at"`
		}{
			ID: goal.Id, Objective: goal.Objective, Status: status,
			CreatedAt: goal.CreatedAt.AsTime(), UpdatedAt: goal.UpdatedAt.AsTime(),
		}
	}
	return writeCommandJSON(stdout, stderr, output)
}

func writeGoalMutationResult(stdout io.Writer, stderr io.Writer, result *runtimepb.GoalMutationSuccess) int {
	if err := protoapi.Validate(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch result.Kind {
	case runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL:
		return writeGoalShowText(stdout, stderr, result.Goal)
	case runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_CLEAR:
		fmt.Fprintln(stdout, "Goal cleared")
	}
	return 0
}

func writeGoalSetResult(stdout io.Writer, stderr io.Writer, result *runtimepb.GoalSetSuccess, requestedSessionID string) int {
	if err := protoapi.Validate(result); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if rejected := result.GetRejected(); rejected != nil {
		fmt.Fprintln(stderr, goalMutationCommandError(requestedSessionID, client.GoalSetErrorAsError(rejected)))
		return 1
	}
	if mutation := result.GetMutation(); mutation != nil {
		if code := writeGoalMutationResult(stdout, stderr, mutation); code != 0 {
			return code
		}
	} else {
		fmt.Fprintln(stderr, "Goal Set response did not contain a committed mutation")
		return 1
	}
	if diagnostic := result.GetDiagnostic(); diagnostic != nil {
		fmt.Fprintf(stderr, "Warning: %s\n", client.GoalSetErrorAsError(diagnostic))
	}
	return 0
}

func goalMutationCommandError(sessionID string, err error) error {
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return goalRuntimeUnavailablePresentationError{SessionID: strings.TrimSpace(sessionID)}
	}
	return client.PresentGoalRequestError(err)
}
