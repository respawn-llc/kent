package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"core/shared/client"
	"core/shared/clientui"
	"core/shared/serverapi"
	"core/shared/sessionenv"
)

type goalTimeoutRemote struct {
	goalCommandRemote
	failRead bool
}

func (r goalTimeoutRemote) ShowGoal(context.Context, serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if r.failRead {
		return serverapi.RuntimeGoalShowResponse{}, context.DeadlineExceeded
	}
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func (goalTimeoutRemote) SetGoal(context.Context, serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalSetResponse, error) {
	return serverapi.RuntimeGoalSetResponse{}, context.DeadlineExceeded
}

func (goalTimeoutRemote) PauseGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalMutationResponse, error) {
	return serverapi.RuntimeGoalMutationResponse{}, context.DeadlineExceeded
}

func (goalTimeoutRemote) ResumeGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalMutationResponse, error) {
	return serverapi.RuntimeGoalMutationResponse{}, context.DeadlineExceeded
}

func (goalTimeoutRemote) CompleteGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalMutationResponse, error) {
	return serverapi.RuntimeGoalMutationResponse{}, context.DeadlineExceeded
}

func (goalTimeoutRemote) ClearGoal(context.Context, serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalMutationResponse, error) {
	return serverapi.RuntimeGoalMutationResponse{}, context.DeadlineExceeded
}

func (goalTimeoutRemote) Close() error { return nil }

func TestGoalCommandsPresentTimeoutWithoutReportingSuccess(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	for _, phase := range []string{"connect", "read", "mutation"} {
		for _, action := range []string{"show", "set", "pause", "resume", "complete", "clear"} {
			if phase == "read" && action != "show" && action != "complete" || phase == "mutation" && action == "show" {
				continue
			}
			t.Run(phase+"/"+action, func(t *testing.T) {
				previous := goalCommandRemoteOpener
				t.Cleanup(func() { goalCommandRemoteOpener = previous })
				goalCommandRemoteOpener = func(context.Context) (goalCommandRemote, error) {
					if phase == "connect" {
						return nil, context.DeadlineExceeded
					}
					return goalTimeoutRemote{failRead: phase == "read"}, nil
				}
				args := []string{action, "--session", "session-1"}
				if action == "set" {
					args = append(args, "finish the task")
				}
				if action == "complete" {
					args = append(args, "--confirm")
				}
				var stdout, stderr bytes.Buffer
				if code := goalSubcommand(args, &stdout, &stderr); code != 1 {
					t.Fatalf("exit code = %d", code)
				}
				if stdout.Len() != 0 {
					t.Fatal("timed-out command reported success")
				}
				presentation := client.PresentGoalRequestError(context.DeadlineExceeded)
				if strings.TrimSpace(stderr.String()) != presentation.Error() {
					t.Fatalf("command did not use the timeout presentation: %s", stderr.String())
				}
			})
		}
	}
}

func TestGoalTimeoutPresentationPreservesCause(t *testing.T) {
	for _, cause := range []error{
		context.DeadlineExceeded,
		fmt.Errorf("request failed: %w", context.DeadlineExceeded),
		&net.DNSError{IsTimeout: true},
	} {
		presented := client.PresentGoalRequestError(cause)
		var timeout client.GoalRequestTimeoutError
		if !errors.As(presented, &timeout) {
			t.Fatalf("expected timeout presentation for %T, got %T", cause, presented)
		}
		if !errors.Is(presented, cause) {
			t.Fatal("timeout presentation lost diagnostic cause")
		}
		var mutationTimeout client.GoalRequestTimeoutError
		if !errors.As(goalMutationCommandError("session-1", cause), &mutationTimeout) {
			t.Fatal("mutation did not present the timeout")
		}
	}
	cause := errors.New("invalid goal")
	if client.PresentGoalRequestError(cause) != cause {
		t.Fatal("unrelated error was replaced")
	}
}

type goalMutationOutputRemote struct {
	goalCommandRemote
	response serverapi.RuntimeGoalSetResponse
}

func (r goalMutationOutputRemote) SetGoal(context.Context, serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalSetResponse, error) {
	return r.response, nil
}

func (goalMutationOutputRemote) Close() error { return nil }

type goalCommandOutputWriter struct {
	label  string
	events *[]string
	buffer bytes.Buffer
}

func (w *goalCommandOutputWriter) Write(value []byte) (int, error) {
	*w.events = append(*w.events, w.label+":"+string(value))
	return w.buffer.Write(value)
}

func TestGoalSetPrintsCommittedResultBeforeOneWarning(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "")
	previous := goalCommandRemoteOpener
	t.Cleanup(func() { goalCommandRemoteOpener = previous })
	goalCommandRemoteOpener = func(context.Context) (goalCommandRemote, error) {
		return goalMutationOutputRemote{
			response: serverapi.RuntimeGoalSetResponse{
				Result:     completedGoalMutationResponse(clientui.RuntimeGoalStatusActive).Result,
				Diagnostic: errors.New("post-commit release warning"),
			},
		}, nil
	}
	var events []string
	stdout := &goalCommandOutputWriter{label: "stdout", events: &events}
	stderr := &goalCommandOutputWriter{label: "stderr", events: &events}

	if code := goalSubcommand([]string{"set", "--session", "session-1", "finish", "the", "task"}, stdout, stderr); code != 0 {
		t.Fatalf("exit code = %d, want success", code)
	}
	if len(events) != 2 || events[0] != "stdout:Goal: finish the task\nStatus: active\n" ||
		events[1] != "stderr:Warning: post-commit release warning\n" {
		t.Fatalf("output events = %#v, want committed result followed by one warning", events)
	}
}

type goalDeadlineRemote struct {
	goalCommandRemote
	t         *testing.T
	deadline  time.Time
	completed bool
	response  serverapi.RuntimeGoalMutationResponse
}

func (r *goalDeadlineRemote) ShowGoal(ctx context.Context, _ serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	deadline, ok := ctx.Deadline()
	if !ok || deadline != r.deadline {
		r.t.Fatal("goal read must share the connection's command deadline")
	}
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func (r *goalDeadlineRemote) CompleteGoal(ctx context.Context, _ serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalMutationResponse, error) {
	deadline, ok := ctx.Deadline()
	if !ok || deadline != r.deadline {
		r.t.Fatal("goal completion must use the remaining command budget")
	}
	r.completed = true
	return r.response, nil
}

func (r *goalDeadlineRemote) Close() error { return nil }

func TestGoalCompleteSharesFifteenSecondBudget(t *testing.T) {
	previous := goalCommandRemoteOpener
	t.Cleanup(func() { goalCommandRemoteOpener = previous })
	remote := &goalDeadlineRemote{t: t, response: completedGoalMutationResponse(clientui.RuntimeGoalStatusComplete)}
	goalCommandRemoteOpener = func(ctx context.Context) (goalCommandRemote, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("goal connection must have a command deadline")
		}
		remaining := time.Until(deadline)
		if remaining < 14*time.Second || remaining > 15*time.Second {
			t.Fatalf("expected a fifteen-second command budget, got %s", remaining)
		}
		remote.deadline = deadline
		return remote, nil
	}
	if code := goalSubcommand([]string{"complete", "--session", "session-1", "--confirm"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !remote.completed {
		t.Fatal("goal was not completed")
	}
}

func TestGoalCompleteRejectsNonCompletedMutationResult(t *testing.T) {
	for _, test := range []struct {
		name     string
		response serverapi.RuntimeGoalMutationResponse
	}{
		{name: "invalid"},
		{
			name: "authoritative Clear",
			response: serverapi.RuntimeGoalMutationResponse{Result: clientui.GoalMutationResult{
				Kind: clientui.GoalMutationResultAuthoritativeClear,
			}},
		},
		{name: "active Goal", response: completedGoalMutationResponse(clientui.RuntimeGoalStatusActive)},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := goalCommandRemoteOpener
			t.Cleanup(func() { goalCommandRemoteOpener = previous })
			remote := &goalDeadlineRemote{t: t, response: test.response}
			goalCommandRemoteOpener = func(ctx context.Context) (goalCommandRemote, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("goal connection must have a command deadline")
				}
				remote.deadline = deadline
				return remote, nil
			}
			var stdout, stderr bytes.Buffer
			if code := goalSubcommand(
				[]string{"complete", "--session", "session-1", "--confirm"},
				&stdout,
				&stderr,
			); code != 1 {
				t.Fatalf("exit code = %d, want failure", code)
			}
			if !remote.completed {
				t.Fatal("Goal completion request was not sent")
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("completion output stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func completedGoalMutationResponse(status clientui.RuntimeGoalStatus) serverapi.RuntimeGoalMutationResponse {
	now := time.Unix(1, 0).UTC()
	return serverapi.RuntimeGoalMutationResponse{Result: clientui.GoalMutationResult{
		Kind: clientui.GoalMutationResultAuthoritativeGoal,
		Goal: &clientui.Goal{
			ID:        "goal-1",
			Objective: "finish the task",
			Status:    status,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}}
}
