//go:build integration && !windows

package pty_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"core/internal/testharness/corefixture"
	"core/internal/testharness/pty/blackbox"
	"core/internal/testharness/testsetup"
	"core/server/core"
	serverstartup "core/server/startup"
	"core/shared/client"
	"core/shared/config"
	projectpb "core/shared/protoapi/gen/kent/api/project"

	"github.com/BurntSushi/toml"
)

//go:embed blackbox/testdata/config.toml
var smokeConfig []byte

type memorySmokeEnvironment struct {
	Root, Workspace, Host string
	Port                  int
	Stub                  *blackbox.ResponsesStub
	Server                *memorySmokeServer
}

type memorySmokeServer struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error // published by closing done
}

func (s *memorySmokeServer) Done() <-chan struct{} { return s.done }
func (s *memorySmokeServer) Error() error {
	select {
	case <-s.done:
		if !errors.Is(s.err, context.Canceled) {
			return s.err
		}
	default:
	}
	return nil
}

func newMemorySmokeEnvironment(t *testing.T, operations []blackbox.RequiredOperation) *memorySmokeEnvironment {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	stub, err := blackbox.StartResponsesStub(operations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := stub.Stop(); err != nil {
			t.Errorf("stop smoke stub: %v", err)
		}
	})
	var document map[string]any
	if _, err := toml.Decode(string(smokeConfig), &document); err != nil {
		t.Fatal(err)
	}
	document["connections"].(map[string]any)["stub"].(map[string]any)["endpoint"] = stub.URL()
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(document); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.toml"), encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	t.Setenv("KENT_SERVER_HOST", "127.0.0.1")
	t.Setenv("KENT_SERVER_PORT", strconv.Itoa(port))
	cfg, err := config.Load(workspace, workspace, config.LoadOptions{ConfigRoot: root})
	if err != nil {
		_ = probe.Close()
		t.Fatal(err)
	}
	appCore, _ := corefixture.Open(t, cfg, testsetup.OpenStore(t, root), core.Options{})
	server := &serverstartup.ServeServer{Core: appCore}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	handle := &memorySmokeServer{cancel: cancel, done: make(chan struct{})}
	go func() {
		handle.err = server.Serve(ctx)
		close(handle.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-handle.done:
			if err := handle.Error(); err != nil {
				t.Errorf("stop smoke server: %v", err)
			}
		case <-time.After(smokeCleanupWait):
			t.Error("smoke server did not stop")
		}
	})
	return &memorySmokeEnvironment{Root: root, Workspace: workspace, Host: "127.0.0.1", Port: port, Stub: stub, Server: handle}
}

func (e *memorySmokeEnvironment) WaitReady() error {
	ctx, cancel := context.WithTimeout(context.Background(), smokeEventWait)
	defer cancel()
	httpClient := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer httpClient.CloseIdleConnections()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(e.Host, strconv.Itoa(e.Port))+"/readyz", nil)
		if err != nil {
			return err
		}
		response, err := httpClient.Do(request)
		if err == nil {
			var body struct {
				Ready bool `json:"ready"`
				PID   int  `json:"pid"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&body)
			closeErr := response.Body.Close()
			if decodeErr != nil || closeErr != nil {
				return errors.Join(decodeErr, closeErr)
			}
			if response.StatusCode == http.StatusOK && body.Ready && body.PID == os.Getpid() {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.Server.Done():
			return fmt.Errorf("smoke server exited before readiness: %v", e.Server.Error())
		case <-ticker.C:
		}
	}
}

func (e *memorySmokeEnvironment) BindProject() (returnErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), smokeEventWait)
	defer cancel()
	remote, err := client.DialRemoteURL(ctx, "ws://"+net.JoinHostPort(e.Host, strconv.Itoa(e.Port))+"/rpc")
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, remote.Close()) }()
	created, err := remote.CreateProject(ctx, &projectpb.CreateProjectRequest{DisplayName: "PTY Harness", WorkspaceRoot: e.Workspace})
	if err != nil {
		return err
	}
	plan, err := remote.PlanWorkspaceBinding(ctx, &projectpb.PlanWorkspaceBindingRequest{Path: e.Workspace, Mode: projectpb.WorkspaceBindingPlanMode_WORKSPACE_BINDING_PLAN_MODE_INTERACTIVE})
	if err != nil {
		return err
	}
	if plan.Kind != projectpb.WorkspaceBindingPlanKind_WORKSPACE_BINDING_PLAN_KIND_BOUND || plan.Binding == nil || plan.Binding.WorkspaceId != created.Binding.WorkspaceId {
		return errors.New("smoke project binding is not bound")
	}
	return nil
}

func (e *memorySmokeEnvironment) ClientEnvironment() ([]string, error) {
	home := filepath.Join(e.Root, "client-home")
	runtime := filepath.Join(home, "runtime")
	temporary := filepath.Join(home, "tmp")
	for _, path := range []string{home, runtime, temporary} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, err
		}
	}
	return []string{"HOME=" + home, "TMPDIR=" + temporary, "XDG_RUNTIME_DIR=" + runtime,
		"PATH=" + os.Getenv("PATH"), "SHELL=/bin/sh", "TZ=UTC",
		"KENT_PERSISTENCE_ROOT=" + e.Root, "KENT_SERVER_HOST=" + e.Host,
		"KENT_SERVER_PORT=" + strconv.Itoa(e.Port), "TERM=xterm-256color", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}, nil
}
