package app

import (
	"context"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"testing"
)

type executionTargetReader struct {
	request *sessionpb.MainViewRequest
	target  *worktreepb.SessionExecutionTarget
}

func (r *executionTargetReader) GetSessionMainView(
	_ context.Context,
	request *sessionpb.MainViewRequest) (*sessionpb.MainViewSuccess, error) {
	r.request = request
	return &sessionpb.MainViewSuccess{
		MainView: &runtimepb.MainView{
			Session: &runtimepb.SessionView{ExecutionTarget: r.target}}}, nil
}

func TestLoadSelectedSessionExecutionTargetUsesMainViewAuthority(t *testing.T) {
	reader := &executionTargetReader{
		target: &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:         " /workspace ",
			WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE}}

	target, err := loadSelectedSessionExecutionTarget(context.Background(), reader, "selected-session")
	if err != nil {
		t.Fatalf("loadSelectedSessionExecutionTarget: %v", err)
	}
	if target.WorkspaceRoot != "/workspace" ||
		target.WorkspaceAvailability != projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE {
		t.Fatalf("execution target = %+v", target)
	}
	if reader.request.SessionId != "selected-session" {
		t.Fatalf("requested session = %q, want %q", reader.request.SessionId, "selected-session")
	}
}

func TestLoadSelectedSessionExecutionTargetPreservesUnavailableTarget(t *testing.T) {
	reader := &executionTargetReader{
		target: &worktreepb.SessionExecutionTarget{
			WorkspaceRoot:         "/missing",
			WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING}}
	target, err := loadSelectedSessionExecutionTarget(context.Background(), reader, "selected-session")
	if err != nil {
		t.Fatalf("loadSelectedSessionExecutionTarget: %v", err)
	}
	if target.WorkspaceAvailability != projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING {
		t.Fatalf("execution target = %+v, want missing", target)
	}
}
