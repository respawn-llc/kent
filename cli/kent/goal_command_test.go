package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"core/shared/serverapi"
	"core/shared/sessionenv"
)

type recordingGoalRemote struct {
	showReq          []serverapi.RuntimeGoalShowRequest
	setReq           []serverapi.RuntimeGoalSetRequest
	completeReq      []serverapi.RuntimeGoalStatusRequest
	goal             *serverapi.RuntimeGoal
	setErr           error
	showDeadline     time.Time
	completeDeadline time.Time
}

func (r *recordingGoalRemote) Close() error { return nil }

func (r *recordingGoalRemote) ShowGoal(ctx context.Context, req serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	r.showReq = append(r.showReq, req)
	if deadline, ok := ctx.Deadline(); ok {
		r.showDeadline = deadline
	}
	return serverapi.RuntimeGoalShowResponse{Goal: r.goal}, nil
}

func (r *recordingGoalRemote) SetGoal(_ context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
	r.setReq = append(r.setReq, req)
	if r.setErr != nil {
		return serverapi.RuntimeGoalShowResponse{}, r.setErr
	}
	return serverapi.RuntimeGoalShowResponse{Goal: r.goal}, nil
}

func (r *recordingGoalRemote) PauseGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func (r *recordingGoalRemote) ResumeGoal(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func (r *recordingGoalRemote) CompleteGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	r.completeReq = append(r.completeReq, req)
	if deadline, ok := ctx.Deadline(); ok {
		r.completeDeadline = deadline
	}
	return serverapi.RuntimeGoalShowResponse{Goal: r.goal}, nil
}

func (r *recordingGoalRemote) ClearGoal(context.Context, serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return serverapi.RuntimeGoalShowResponse{}, nil
}

func TestGoalCompleteUsesFreshTimeoutForCompletionRPC(t *testing.T) {
	t.Setenv(sessionenv.SessionIDEnv, "session-1")
	remote := &recordingGoalRemote{goal: &serverapi.RuntimeGoal{ID: "goal-1", Objective: "ship goal mode", Status: "active"}}
	restore := replaceGoalCommandRemoteOpener(t, remote)
	defer restore()

	stdout := new(strings.Builder)
	stderr := new(strings.Builder)
	if code := goalSubcommand([]string{"complete", "--confirm"}, stdout, stderr); code != 0 {
		t.Fatalf("goal complete --confirm exit = %d stderr=%q", code, stderr.String())
	}
	if remote.showDeadline.IsZero() || remote.completeDeadline.IsZero() {
		t.Fatalf("deadlines missing: show=%v complete=%v", remote.showDeadline, remote.completeDeadline)
	}
	if !remote.completeDeadline.After(remote.showDeadline) {
		t.Fatalf("complete deadline = %v, want fresh deadline after show deadline %v", remote.completeDeadline, remote.showDeadline)
	}
}

func replaceGoalCommandRemoteOpener(t *testing.T, remote *recordingGoalRemote) func() {
	t.Helper()
	previous := goalCommandRemoteOpener
	goalCommandRemoteOpener = func(context.Context) (goalCommandRemote, error) {
		return remote, nil
	}
	return func() { goalCommandRemoteOpener = previous }
}
