package app

import (
	projectpb "core/shared/protoapi/gen/kent/api/project"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	textutil "core/shared/textutil"
	"testing"
)

func TestSessionWorkspaceIdentityChangeReopensMovedSession(t *testing.T) {
	sessionID, err := runtimeids.ParseSessionID("11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	model := newProjectedClosedUIModel(&runtimeControlFakeClient{}, WithUISessionID(sessionID.String()))
	identity := func(sequence uint64, workspaceID, root string) {
		target := &worktreepb.SessionExecutionTarget{WorkspaceId: textutil.Value(workspaceID),
			WorkspaceName:         workspaceID,
			WorkspaceRoot:         root,
			WorkspaceAvailability: projectpb.ProjectAvailability_PROJECT_AVAILABILITY_AVAILABLE,
			CwdRelpath:            ".",
			EffectiveWorkdir:      root}
		model.applyAdmittedTranscriptMessageState(transcriptTestMessage(sequence, &transcriptpb.SessionIdentity{SessionId: sessionID.String(),
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
			ExecutionTarget:       target}),
			runtimeTupleMergeResult{})
	}
	identity(1, "workspace-a", "/workspace-a")
	identity(2, "workspace-b", "/workspace-b")

	transition := model.Transition()
	if !transition.SessionRetargeted || transition.Action != UIActionOpenSession || transition.TargetSessionID != sessionID.String() {
		t.Fatalf("Session transition = %+v", transition)
	}
}
