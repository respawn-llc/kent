package app

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
)

type runtimeAttachmentTestServer struct {
	runtime        client.SessionRuntimeClient
	sessionEvents  client.SessionActivityClient
	promptEvents   client.PromptActivityClient
	sessionViews   client.SessionViewClient
	runtimeControl client.RuntimeControlClient
}

func (s runtimeAttachmentTestServer) SessionRuntimeClient() client.SessionRuntimeClient {
	return s.runtime
}

func (s runtimeAttachmentTestServer) SessionActivityClient() client.SessionActivityClient {
	return s.sessionEvents
}

func (s runtimeAttachmentTestServer) PromptActivityClient() client.PromptActivityClient {
	return s.promptEvents
}

func (s runtimeAttachmentTestServer) SessionViewClient() client.SessionViewClient {
	return s.sessionViews
}

func (s runtimeAttachmentTestServer) RuntimeControlClient() client.RuntimeControlClient {
	return s.runtimeControl
}

func (runtimeAttachmentTestServer) PromptControlClient() client.PromptControlClient { return nil }
func (runtimeAttachmentTestServer) ApprovalViewClient() client.ApprovalViewClient   { return nil }
func (runtimeAttachmentTestServer) AskViewClient() client.AskViewClient             { return nil }
func (runtimeAttachmentTestServer) ProcessControlClient() client.ProcessControlClient {
	return nil
}
func (runtimeAttachmentTestServer) ProcessOutputClient() client.ProcessOutputClient { return nil }
func (runtimeAttachmentTestServer) ProcessViewClient() client.ProcessViewClient     { return nil }
func (runtimeAttachmentTestServer) WorktreeClient() client.WorktreeClient           { return nil }

func TestRuntimeAttachmentSubscribeFailureReleasesRuntime(t *testing.T) {
	for _, tc := range []struct {
		name            string
		sessionErr      error
		promptErr       error
		wantPromptStart bool
	}{
		{name: "session subscribe failure", sessionErr: errors.New("session subscribe failed")},
		{name: "prompt subscribe failure", promptErr: errors.New("prompt subscribe failed"), wantPromptStart: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			releaseCount := 0
			released := make(chan context.Context, 1)
			promptStarted := false
			server := runtimeAttachmentTestServer{
				runtime: &recordingSessionRuntimeClient{
					activate: func(context.Context, serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
						return serverapi.SessionRuntimeActivateResponse{LeaseID: "lease-1"}, nil
					},
					release: func(ctx context.Context, req serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
						releaseCount++
						released <- ctx
						if req.SessionID != "session-1" || req.LeaseID != "lease-1" {
							t.Fatalf("unexpected release request: %+v", req)
						}
						return serverapi.SessionRuntimeReleaseResponse{}, nil
					},
				},
				sessionEvents: &recordingSessionActivityClient{
					subscribe: func(context.Context, serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
						if tc.sessionErr != nil {
							return nil, tc.sessionErr
						}
						return noOpSessionActivitySubscription{}, nil
					},
				},
				promptEvents: &recordingPromptActivityClient{
					subscribe: func(context.Context, serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
						promptStarted = true
						if tc.promptErr != nil {
							return nil, tc.promptErr
						}
						return nil, nil
					},
				},
			}

			_, err := prepareSharedRuntime(context.Background(), server, sessionLaunchPlan{SessionID: "session-1"}, io.Discard, "test")
			wantErr := tc.sessionErr
			if wantErr == nil {
				wantErr = tc.promptErr
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("prepareSharedRuntime error = %v, want %v", err, wantErr)
			}
			if promptStarted != tc.wantPromptStart {
				t.Fatalf("prompt started = %v, want %v", promptStarted, tc.wantPromptStart)
			}
			if releaseCount != 1 {
				t.Fatalf("release count = %d, want exactly 1", releaseCount)
			}
			select {
			case ctx := <-released:
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("expected bounded release context")
				}
				if remaining := time.Until(deadline); remaining <= 0 || remaining > runtimeReleaseTimeout {
					t.Fatalf("release deadline remaining = %v, want within %v", remaining, runtimeReleaseTimeout)
				}
			default:
				t.Fatal("expected release context")
			}
		})
	}
}

func TestRuntimeAttachmentCloseReleasesRuntime(t *testing.T) {
	releaseCount := 0
	server := runtimeAttachmentTestServer{
		runtime: &recordingSessionRuntimeClient{
			activate: func(context.Context, serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
				return serverapi.SessionRuntimeActivateResponse{LeaseID: "lease-close"}, nil
			},
			release: func(_ context.Context, req serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
				releaseCount++
				if req.SessionID != "session-close" || req.LeaseID != "lease-close" {
					t.Fatalf("unexpected release request: %+v", req)
				}
				return serverapi.SessionRuntimeReleaseResponse{}, nil
			},
		},
		sessionEvents: &recordingSessionActivityClient{
			subscribe: func(context.Context, serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
				return noOpSessionActivitySubscription{}, nil
			},
		},
		promptEvents: &recordingPromptActivityClient{
			subscribe: func(context.Context, serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
				return nil, nil
			},
		},
		sessionViews:   &countingSessionViewClient{},
		runtimeControl: &leaseRetryRuntimeControlClient{},
	}

	plan, err := prepareSharedRuntime(context.Background(), server, sessionLaunchPlan{SessionID: "session-close"}, io.Discard, "test")
	if err != nil {
		t.Fatalf("prepareSharedRuntime: %v", err)
	}
	plan.Close()
	if releaseCount != 1 {
		t.Fatalf("release count = %d, want exactly 1", releaseCount)
	}
}

func TestRuntimeAttachmentLeaseRecoveryUsesActivation(t *testing.T) {
	activateCalls := 0
	server := runtimeAttachmentTestServer{
		runtime: &recordingSessionRuntimeClient{
			activate: func(_ context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
				activateCalls++
				if req.SessionID != "session-recover" {
					t.Fatalf("session id = %q, want session-recover", req.SessionID)
				}
				if req.ActiveSettings.Model != "gpt-test" {
					t.Fatalf("model = %q, want gpt-test", req.ActiveSettings.Model)
				}
				return serverapi.SessionRuntimeActivateResponse{LeaseID: map[int]string{1: "lease-1", 2: "lease-2"}[activateCalls]}, nil
			},
		},
	}
	lease, manager, err := activateSharedRuntime(context.Background(), server, sessionLaunchPlan{
		SessionID:      "session-recover",
		ActiveSettings: config.Settings{Model: "gpt-test"},
	})
	if err != nil {
		t.Fatalf("activateSharedRuntime: %v", err)
	}
	if lease.ID != "lease-1" || manager.Value() != "lease-1" {
		t.Fatalf("initial lease = %q manager = %q, want lease-1", lease.ID, manager.Value())
	}
	recovered, err := manager.Recover(context.Background())
	if err != nil {
		t.Fatalf("recover lease: %v", err)
	}
	if recovered != "lease-2" || manager.Value() != "lease-2" {
		t.Fatalf("recovered lease = %q manager = %q, want lease-2", recovered, manager.Value())
	}
	if activateCalls != 2 {
		t.Fatalf("activate calls = %d, want 2", activateCalls)
	}
}
