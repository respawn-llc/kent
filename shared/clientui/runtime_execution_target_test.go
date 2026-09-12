package clientui

import (
	projectpb "core/shared/protoapi/gen/kent/api/project"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestNormalizeSessionExecutionTargetPreservesNilWorktreeAbsence(t *testing.T) {
	target := NormalizeSessionExecutionTarget(&worktreepb.SessionExecutionTarget{
		WorkspaceId:           proto.String(" workspace-1 "),
		WorkspaceName:         " Workspace ",
		WorkspaceRoot:         " /repo ",
		WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		Worktree:              nil,
		CwdRelpath:            " . ",
		EffectiveWorkdir:      " /repo ",
	})

	if target.Worktree != nil {
		t.Fatalf("normalized workspace-root target worktree = %+v, want nil", target.Worktree)
	}
	if target.GetWorkspaceId() != "workspace-1" || target.CwdRelpath != "." || target.EffectiveWorkdir != "/repo" {
		t.Fatalf("normalized target = %+v, want trimmed workspace-root fields", target)
	}
}

func TestSessionExecutionTargetsEqualNormalizesPresentWorktree(t *testing.T) {
	left := &worktreepb.SessionExecutionTarget{
		WorkspaceId:           proto.String("workspace-1"),
		WorkspaceName:         "Workspace",
		WorkspaceRoot:         "/repo",
		WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		Worktree: &worktreepb.SessionExecutionWorktreeTarget{
			Id:           " worktree-1 ",
			Name:         " Task worktree ",
			Root:         " /repo/.kent-worktree ",
			Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING,
		},
		CwdRelpath:       " subdir ",
		EffectiveWorkdir: " /repo/.kent-worktree/subdir ",
	}
	right := &worktreepb.SessionExecutionTarget{
		WorkspaceId:           proto.String("workspace-1"),
		WorkspaceName:         "Workspace",
		WorkspaceRoot:         "/repo",
		WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
		Worktree: &worktreepb.SessionExecutionWorktreeTarget{
			Id:           "worktree-1",
			Name:         "Task worktree",
			Root:         "/repo/.kent-worktree",
			Availability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_MISSING,
		},
		CwdRelpath:       "subdir",
		EffectiveWorkdir: "/repo/.kent-worktree/subdir",
	}

	if !SessionExecutionTargetsEqual(left, right) {
		t.Fatalf("targets should compare equal after normalization:\nleft=%+v\nright=%+v", left, right)
	}
}
