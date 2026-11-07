package startup

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"core/internal/testharness/httpclient"
	"core/shared/client"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/protocol"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestServeShutdownCancelsSignInBeforeDraining(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		configured      bool
		listenerFailure bool
		disconnect      bool
	}{
		{name: "onboarding"},
		{name: "configured", configured: true},
		{name: "onboarding-listener-failure-disconnected", listenerFailure: true, disconnect: true},
		{name: "configured-listener-failure-disconnected", configured: true, listenerFailure: true, disconnect: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			configured := scenario.configured
			workspace := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			if configured {
				registerServeWorkspace(t, workspace)
			} else {
				configureServeTestServerPort(t)
			}
			server := startServeTestServer(t, Request{WorkspaceRoot: workspace, WorkspaceRootExplicit: true})
			defer func() { _ = server.Close() }()

			entered := make(chan struct{})
			canceled := make(chan struct{})
			base := http.DefaultTransport
			http.DefaultTransport = httpclient.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				host, _, err := net.SplitHostPort(r.URL.Host)
				if err == nil && host == "127.0.0.1" {
					return base.RoundTrip(r)
				}
				close(entered)
				<-r.Context().Done()
				close(canceled)
				return nil, r.Context().Err()
			})
			defer func() { http.DefaultTransport = base }()
			serveCtx, stopServing := context.WithCancel(t.Context())
			defer stopServing()
			shutdown := stopServing
			expectedError := error(context.Canceled)
			if scenario.listenerFailure {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = listener.Close() }()
				previous := localSocketListener
				localSocketListener = func(config.App) (net.Listener, func(), bool, error) {
					return listener, func() { _ = listener.Close() }, true, nil
				}
				defer func() { localSocketListener = previous }()
				shutdown = func() { _ = listener.Close() }
				expectedError = net.ErrClosed
			}
			served := make(chan error, 1)
			releaseServeTestPortForConfig(server.cfg)
			go func() { served <- server.Serve(serveCtx) }()
			response := waitForServeResponse(t, http.DefaultClient, config.ServerHTTPBaseURL(server.cfg)+protocol.HealthPath)
			_ = response.Body.Close()
			remote, err := client.DialRemoteURL(t.Context(), config.ServerRPCURL(server.cfg))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = remote.Close() }()
			target := &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_AddConnection{
				AddConnection: &authpb.ConnectionDefinition{Id: "shutdown-test", Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT},
			}}
			if !configured {
				_, err := remote.ConfigureConnection(t.Context(), &authpb.ConfigureConnectionRequest{
					Change: &authpb.ConfigureConnectionRequest_PendingSetup{
						PendingSetup: target.GetAddConnection(),
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				target = &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_PendingSetup{PendingSetup: &emptypb.Empty{}}}
			}
			completed := make(chan error, 1)
			go func() {
				_, err := remote.CompleteBootstrap(t.Context(), &authpb.CompleteBootstrapRequest{
					Target: target,
					Mode:   authpb.BootstrapMode_BOOTSTRAP_MODE_DEVICE_CODE,
					Continuation: &authpb.BootstrapContinuation{
						Protocol: authpb.ConnectionProtocol_CONNECTION_PROTOCOL_CHATGPT,
						Grant: &authpb.BootstrapContinuation_Device{Device: &authpb.DeviceBootstrapContinuation{
							Code: "device", UserCode: "user", PollIntervalSeconds: 1,
						}},
					},
				})
				completed <- err
			}()
			select {
			case <-entered:
			case err := <-completed:
				t.Fatalf("sign-in did not begin: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("sign-in did not begin")
			}
			if scenario.disconnect {
				if err := remote.Close(); err != nil {
					t.Fatal(err)
				}
				<-completed
				select {
				case <-canceled:
					t.Fatal("client disconnection canceled server-owned sign-in")
				default:
				}
			}
			shutdown()
			select {
			case err := <-served:
				if !errors.Is(err, expectedError) {
					t.Fatalf("Serve error = %v", err)
				}
				select {
				case <-canceled:
				default:
					t.Fatal("server returned before canceling sign-in")
				}
			case <-time.After(2 * time.Second):
				_ = server.Close()
				<-served
				t.Fatal("shutdown waited for sign-in instead of canceling it before draining requests")
			}
		})
	}
}
