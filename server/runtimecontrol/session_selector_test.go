package runtimecontrol

import (
	"errors"
	"path/filepath"
	"testing"

	"core/server/metadata"
	"core/server/session"
	"core/server/sessionruntime"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func TestLiveControlsRejectResolvedSelfTargets(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	store, err := metadata.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	binding, err := store.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.Create(filepath.Join(root, "projects", binding.ProjectID, "sessions"), binding.WorkspaceName, workspace, sessioncontract.SessionCategoryMain, store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	caller := sess.Meta().SessionID
	service := NewService(sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{}), store)
	for _, raw := range []string{caller, "000" + sess.Identity().String()} {
		selector, err := runtimeids.ParseSessionSelector(raw)
		if err != nil {
			t.Fatal(err)
		}
		target := protoapi.SessionSelectorToProto(selector)
		for _, operation := range []func() error{
			func() error {
				_, err := service.LiveSteer(t.Context(), &runtimepb.LiveSteerRequest{SessionId: target, CallerSessionId: &caller, Text: "continue"})
				return err
			},
			func() error {
				_, err := service.LiveStop(t.Context(), &runtimepb.LiveStopRequest{SessionId: target, CallerSessionId: &caller})
				return err
			},
			func() error {
				_, err := service.LiveWait(t.Context(), &runtimepb.LiveWaitRequest{SessionId: target, CallerSessionId: &caller})
				return err
			},
			func() error {
				_, err := service.LiveWatch(t.Context(), &promptpb.LiveWatchRequest{SessionId: target, CallerSessionId: &caller})
				return err
			},
		} {
			var self *serverapi.SessionSelfTargetError
			if err := operation(); !errors.As(err, &self) || self.SessionID.String() != caller {
				t.Fatalf("self target %s = %v", raw, err)
			}
		}
	}
}

func TestLiveStopMissingNumericTargetIsIdle(t *testing.T) {
	store, err := metadata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service := NewService(sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{}), store)
	shortID, err := runtimeids.NewSessionShortID(123)
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.LiveStop(t.Context(), &runtimepb.LiveStopRequest{SessionId: protoapi.SessionSelectorToProto(shortID)})
	if err != nil || response.Status != runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_IDLE {
		t.Fatalf("missing numeric Stop = %+v, %v", response, err)
	}
}
