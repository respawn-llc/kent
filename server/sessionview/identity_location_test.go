package sessionview

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"core/server/sessionstore"
	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/runtimeids"
)

type identityLocationRecord struct {
	record session.PersistedSessionRecord
	err    error
}

func (r identityLocationRecord) ResolvePersistedSession(context.Context, string) (session.PersistedSessionRecord, error) {
	return r.record, r.err
}

func TestSessionIdentityLocation(t *testing.T) {
	id, err := runtimeids.ParseSessionID("33333333-3333-4333-8333-333333333333")
	if err != nil {
		t.Fatal(err)
	}
	shortID, err := runtimeids.NewSessionShortID(73)
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []*runtimeids.SessionShortID{&shortID, nil} {
		dir := filepath.Join(t.TempDir(), "unopened-session")
		record := session.PersistedSessionRecord{
			Meta: &session.Meta{SessionID: id.String()}, SessionDir: dir, ShortID: number,
		}
		service := NewService(identityLocationRecord{record: record}, nil, nil, nil)
		result, err := service.GetSessionIdentityLocation(t.Context(), &sessionpb.IdentityLocationRequest{
			SessionId: protoapi.SessionSelectorToProto(id),
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionId != id.String() || result.LogPath != session.RunLogPath(dir) {
			t.Fatalf("identity/location=%+v", result)
		}
		if number == nil && result.ShortId != nil || number != nil && (result.ShortId == nil || *result.ShortId != number.Int64()) {
			t.Fatalf("short ID=%v, want %v", result.ShortId, number)
		}
	}
	cause := errors.New("metadata unavailable")
	service := NewService(identityLocationRecord{err: cause}, nil, nil, nil)
	result, err := service.GetSessionIdentityLocation(t.Context(), &sessionpb.IdentityLocationRequest{
		SessionId: protoapi.SessionSelectorToProto(id),
	})
	if !errors.Is(err, cause) || result != nil {
		t.Fatalf("read failure result=%v error=%v", result, err)
	}
}
