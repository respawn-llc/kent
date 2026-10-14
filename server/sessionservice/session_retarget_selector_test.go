package sessionservice

import (
	"testing"

	"core/server/metadata"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
)

func TestRetargetSelectorsPreserveInvokingSessionScheduling(t *testing.T) {
	fixture := newSessionRemovalServiceFixture(t)
	identity := fixture.session.Identity()
	for _, target := range []runtimeids.SessionSelector{identity.ID, *identity.ShortID} {
		for _, self := range []bool{true, false} {
			caller := runtimeids.NewSessionID()
			if self {
				caller = identity.ID
			}
			retargeter := &sessionLifecycleRetargeterStub{result: metadata.SessionWorkspaceRetargetResult{Binding: metadata.Binding{
				ProjectID: "target", ProjectKey: "TAR", ProjectName: "Target",
				WorkspaceID: "workspace", CanonicalRoot: t.TempDir(), WorkspaceName: "target", WorkspaceStatus: "available",
			}}}
			fixture.service.WithWorkspaceRetargeter(retargeter)
			_, err := fixture.service.RetargetSessionWorkspace(t.Context(), &sessionlaunchpb.SessionRetargetWorkspaceRequest{
				SessionId:     protoapi.SessionSelectorToProto(target),
				WorkspaceRoot: t.TempDir(),
				Origin: &sessionlaunchpb.RuntimeStepOrigin{
					SessionId: caller.String(),
					StepId:    "22222222-2222-4222-8222-222222222222",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if retargeter.req.SessionID != identity.ID.String() || retargeter.scheduled != self {
				t.Fatalf("target %s, self %t: request=%+v, scheduled=%t", target, self, retargeter.req, retargeter.scheduled)
			}
		}
	}
}
