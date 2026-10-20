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

func newNumberedRuntimeControlFixture(t *testing.T) (*session.Store, *Service) {
	t.Helper()
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
	service := NewService(sessionruntime.NewAuthority(sessionruntime.AuthorityOptions{}), store).WithPersistedSessionResolver(store)
	return sess, service
}

func TestLiveControlsRejectResolvedSelfTargets(t *testing.T) {
	sess, service := newNumberedRuntimeControlFixture(t)
	caller := sess.Meta().SessionID
	for _, raw := range []string{caller, "000" + sess.Identity().String()} {
		selector, err := runtimeids.ParseSessionSelector(raw)
		if err != nil {
			t.Fatal(err)
		}
		target := protoapi.SessionSelectorToProto(selector)
		for _, operation := range []func() error{
			func() error {
				_, err := service.LiveSteer(t.Context(), &runtimepb.LiveSteerRequest{SessionId: target, CallerSessionId: &caller, QueueItemId: runtimeids.NewQueueItemID().String(), Text: "continue"})
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
			var captured *serverapi.SessionTargetError
			if err := operation(); !errors.As(err, &self) || self.SessionID.String() != caller ||
				!errors.As(err, &captured) || captured.Projection() == nil ||
				captured.Projection().String() != sess.Identity().String() {
				t.Fatalf("self target %s = %v", raw, err)
			}
		}
	}
}

func TestGoalSelectorsReadSamePersistedGoal(t *testing.T) {
	sess, service := newNumberedRuntimeControlFixture(t)
	goal, _, err := sess.SetGoal("finish the task", session.GoalActorUser)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []runtimeids.SessionSelector{sess.Identity().ID, *sess.Identity().ShortID} {
		result, err := service.ShowGoal(t.Context(), &runtimepb.GoalShowRequest{SessionId: protoapi.SessionSelectorToProto(target)})
		if err != nil || result.Goal == nil || result.Goal.Id != goal.ID {
			t.Fatalf("goal for %s: %+v, %v", target, result, err)
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

func TestLiveWaitFailureRetainsResolvedSessionIdentity(t *testing.T) {
	sess, service := newNumberedRuntimeControlFixture(t)
	for _, selector := range []runtimeids.SessionSelector{sess.Identity().ID, *sess.Identity().ShortID} {
		_, err := service.LiveWait(t.Context(), &runtimepb.LiveWaitRequest{SessionId: protoapi.SessionSelectorToProto(selector)})
		var failure *serverapi.SessionTargetError
		if !errors.Is(err, serverapi.ErrRuntimeUnavailable) || !errors.As(err, &failure) ||
			failure.Projection() == nil || failure.Projection().ID != sess.Identity().ID || failure.Projection().ShortID == nil ||
			*failure.Projection().ShortID != *sess.Identity().ShortID {
			t.Fatalf("resolved failure identity = %v, %v", failure, err)
		}
	}
}

func TestLiveWaitProjectionFailureRetainsCanonicalTarget(t *testing.T) {
	sess, service := newNumberedRuntimeControlFixture(t)
	if err := service.metadata.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := service.LiveWait(t.Context(), &runtimepb.LiveWaitRequest{SessionId: protoapi.SessionSelectorToProto(sess.Identity().ID)})
	var failure *serverapi.SessionTargetError
	if !errors.As(err, &failure) || failure.SessionID() == nil || *failure.SessionID() != sess.Identity().ID || failure.Projection() != nil {
		t.Fatalf("projection failure lost known target: %v", err)
	}
}
