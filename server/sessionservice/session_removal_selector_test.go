package sessionservice

import (
	"errors"
	"path/filepath"
	"testing"

	"core/server/session"
	"core/shared/protoapi"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestRemovalSelectorsRejectInvokingSession(t *testing.T) {
	fixture := newSessionRemovalServiceFixture(t)
	identity := fixture.session.Identity()
	caller := identity.ID.String()
	for _, target := range []runtimeids.SessionSelector{identity.ID, *identity.ShortID} {
		for _, archive := range []bool{false, true} {
			var err error
			if archive {
				_, err = fixture.service.ArchiveSession(t.Context(), &sessionlaunchpb.SessionArchiveRequest{
					SessionId: protoapi.SessionSelectorToProto(target), CallerSessionId: &caller,
					OutputPath: filepath.Join(t.TempDir(), "archive.tar.zst"),
				})
			} else {
				_, err = fixture.service.DeleteSession(t.Context(), &sessionlaunchpb.SessionDeleteRequest{
					SessionId: protoapi.SessionSelectorToProto(target), CallerSessionId: &caller,
				})
			}
			var self *serverapi.SessionSelfTargetError
			if !errors.As(err, &self) || self.SessionID != identity.ID {
				t.Fatalf("target %s archive %t: %v", target, archive, err)
			}
			if _, err := fixture.metadata.ResolvePersistedSession(t.Context(), caller); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRemovalNumericSelectorDeletionAndMissingOutcome(t *testing.T) {
	fixture := newSessionRemovalServiceFixture(t)
	identity := fixture.session.Identity()
	target := protoapi.SessionSelectorToProto(*identity.ShortID)
	result, err := fixture.service.DeleteSession(t.Context(), &sessionlaunchpb.SessionDeleteRequest{SessionId: target})
	if err != nil || result.SessionId != identity.ID.String() {
		t.Fatalf("delete: %+v, %v", result, err)
	}
	_, err = fixture.service.DeleteSession(t.Context(), &sessionlaunchpb.SessionDeleteRequest{SessionId: target})
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("missing selector: %v", err)
	}
}
