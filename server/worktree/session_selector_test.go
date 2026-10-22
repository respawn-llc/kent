package worktree

import (
	"testing"

	"core/shared/protoapi"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
)

func TestWorktreeStatusAcceptsSessionSelectors(t *testing.T) {
	env := newServiceTestEnv(t)
	identity := env.session.Identity()
	for _, selector := range []runtimeids.SessionSelector{identity.ID, *identity.ShortID} {
		status, err := env.service.GetWorktreeStatus(t.Context(), &worktreepb.StatusRequest{
			SessionId: protoapi.SessionSelectorToProto(selector),
		})
		if err != nil || status.Target.GetWorkspaceId() != env.binding.WorkspaceID {
			t.Fatalf("status for %s: %+v, %v", selector, status, err)
		}
	}
}
