package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"core/shared/apicontract"
	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/runtimeids"
)

type sessionIdentityReadStub struct {
	apicontract.SessionViewService
	result *sessionpb.IdentityLocationSuccess
	err    error
}

func (s sessionIdentityReadStub) GetSessionIdentityLocation(context.Context, *sessionpb.IdentityLocationRequest) (*sessionpb.IdentityLocationSuccess, error) {
	return s.result, s.err
}

func TestSessionIdentityDiagnostic(t *testing.T) {
	id, err := runtimeids.ParseSessionID("33333333-3333-4333-8333-333333333333")
	if err != nil {
		t.Fatal(err)
	}
	shortID, err := runtimeids.NewSessionShortID(73)
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []*runtimeids.SessionShortID{&shortID, nil} {
		result := &sessionpb.IdentityLocationSuccess{
			SessionId: id.String(), ShortId: protoapi.SessionShortIDToProto(number),
			LogPath: "/server-owned-location/steps.log",
		}
		var stdout bytes.Buffer
		if err := writeSessionIdentity(t.Context(), sessionIdentityReadStub{result: result}, id, &stdout); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(stdout.Bytes(), []byte(id.String())) || !bytes.Contains(stdout.Bytes(), []byte(result.LogPath)) {
			t.Fatalf("missing diagnostic facts: %s", &stdout)
		}
		if number != nil && !bytes.Contains(stdout.Bytes(), []byte(number.String())) {
			t.Fatalf("missing short ID: %s", &stdout)
		}
		if bytes.Count(stdout.Bytes(), []byte{'\n'}) != 3 {
			t.Fatalf("expected three labeled facts: %s", &stdout)
		}
	}
	cause := errors.New("server unavailable")
	var stdout bytes.Buffer
	if err := writeSessionIdentity(t.Context(), sessionIdentityReadStub{err: cause}, id, &stdout); !errors.Is(err, cause) || stdout.Len() != 0 {
		t.Fatalf("read failure: output=%s error=%v", &stdout, err)
	}
}
