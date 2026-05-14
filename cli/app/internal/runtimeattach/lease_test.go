package runtimeattach

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
)

type fakeRuntimeService struct {
	activateResponses []serverapi.SessionRuntimeActivateResponse
	activateErr       error
	releaseErr        error
	activateRequests  []serverapi.SessionRuntimeActivateRequest
	releaseRequests   []serverapi.SessionRuntimeReleaseRequest
}

func (s *fakeRuntimeService) ActivateSessionRuntime(_ context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
	s.activateRequests = append(s.activateRequests, req)
	if s.activateErr != nil {
		return serverapi.SessionRuntimeActivateResponse{}, s.activateErr
	}
	if len(s.activateResponses) == 0 {
		return serverapi.SessionRuntimeActivateResponse{}, nil
	}
	resp := s.activateResponses[0]
	s.activateResponses = s.activateResponses[1:]
	return resp, nil
}

func (s *fakeRuntimeService) ReleaseSessionRuntime(_ context.Context, req serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
	s.releaseRequests = append(s.releaseRequests, req)
	return serverapi.SessionRuntimeReleaseResponse{}, s.releaseErr
}

func TestActivateBuildsRequestAndTrimsLease(t *testing.T) {
	service := &fakeRuntimeService{activateResponses: []serverapi.SessionRuntimeActivateResponse{{LeaseID: " lease-1 "}}}
	lease, err := Activate(context.Background(), service, Request{
		SessionID:          "session-1",
		EnabledTools:       []toolspec.ID{"shell", "patch"},
		ActiveSettings:     config.Settings{Model: "gpt-test"},
		Source:             config.SourceReport{SettingsPath: "/config.toml"},
		NewClientRequestID: fixedIDs("request-1"),
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if lease.ID != "lease-1" {
		t.Fatalf("lease id = %q, want trimmed lease", lease.ID)
	}
	if len(service.activateRequests) != 1 {
		t.Fatalf("activate requests = %d, want 1", len(service.activateRequests))
	}
	req := service.activateRequests[0]
	if req.ClientRequestID != "request-1" || req.SessionID != "session-1" {
		t.Fatalf("request ids = %+v, want request/session ids", req)
	}
	if !reflect.DeepEqual(req.EnabledToolIDs, []string{"shell", "patch"}) {
		t.Fatalf("enabled tools = %#v, want shell/patch", req.EnabledToolIDs)
	}
	if req.ActiveSettings.Model != "gpt-test" || req.Source.SettingsPath != "/config.toml" {
		t.Fatalf("request config = %+v source = %+v", req.ActiveSettings, req.Source)
	}
}

func TestActivateRecoverReactivatesRuntimeWithFreshRequestID(t *testing.T) {
	service := &fakeRuntimeService{activateResponses: []serverapi.SessionRuntimeActivateResponse{{LeaseID: "lease-1"}, {LeaseID: " lease-2 "}}}
	lease, err := Activate(context.Background(), service, Request{
		SessionID:          "session-1",
		NewClientRequestID: fixedIDs("request-1", "request-2"),
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	recovered, err := lease.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if recovered != "lease-2" {
		t.Fatalf("recovered lease = %q, want lease-2", recovered)
	}
	gotIDs := []string{service.activateRequests[0].ClientRequestID, service.activateRequests[1].ClientRequestID}
	if !reflect.DeepEqual(gotIDs, []string{"request-1", "request-2"}) {
		t.Fatalf("request ids = %#v, want fresh ids", gotIDs)
	}
}

func TestActivateRecoverReportsReadOnlyTransition(t *testing.T) {
	service := &fakeRuntimeService{activateResponses: []serverapi.SessionRuntimeActivateResponse{{LeaseID: "lease-1"}, {ReadOnly: true}}}
	lease, err := Activate(context.Background(), service, Request{
		SessionID:          "session-1",
		NewClientRequestID: fixedIDs("request-1", "request-2"),
	})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	_, err = lease.Recover(context.Background())
	if !errors.Is(err, ErrReadOnlyControllerLease) {
		t.Fatalf("Recover error = %v, want read-only transition", err)
	}
}

func TestActivateRejectsEmptyLease(t *testing.T) {
	service := &fakeRuntimeService{activateResponses: []serverapi.SessionRuntimeActivateResponse{{LeaseID: " "}}}
	_, err := Activate(context.Background(), service, Request{NewClientRequestID: fixedIDs("request-1")})
	if !errors.Is(err, ErrEmptyControllerLease) {
		t.Fatalf("error = %v, want ErrEmptyControllerLease", err)
	}
}

func TestActivateAcceptsReadOnlyWithoutLease(t *testing.T) {
	service := &fakeRuntimeService{activateResponses: []serverapi.SessionRuntimeActivateResponse{{ReadOnly: true}}}
	lease, err := Activate(context.Background(), service, Request{NewClientRequestID: fixedIDs("request-1")})
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !lease.ReadOnly {
		t.Fatal("expected read-only lease")
	}
	if lease.ID != "" {
		t.Fatalf("lease id = %q, want empty", lease.ID)
	}
}

func TestReleaseSkipsNilOrEmptyLeaseAndIgnoresReleaseError(t *testing.T) {
	Release(nil, "session-1", "lease-1")
	service := &fakeRuntimeService{releaseErr: errors.New("release failed")}
	Release(service, "session-1", " ")
	if len(service.releaseRequests) != 0 {
		t.Fatalf("release requests = %d, want none", len(service.releaseRequests))
	}
	Release(service, "session-1", " lease-1 ")
	if len(service.releaseRequests) != 1 {
		t.Fatalf("release requests = %d, want 1", len(service.releaseRequests))
	}
	req := service.releaseRequests[0]
	if req.SessionID != "session-1" || req.LeaseID != "lease-1" || req.ClientRequestID == "" {
		t.Fatalf("release request = %+v, want session/lease/request ids", req)
	}
}

func fixedIDs(ids ...string) func() string {
	index := 0
	return func() string {
		if index >= len(ids) {
			return ids[len(ids)-1]
		}
		id := ids[index]
		index++
		return id
	}
}
