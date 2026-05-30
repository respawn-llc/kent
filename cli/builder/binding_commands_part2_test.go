package main

import (
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestRetargetSessionWorkspaceDoesNotFallbackForExplicitLoopbackPortOpenFailure(t *testing.T) {
	resetBindingCommandRetargetHooks(t)
	t.Setenv("BUILDER_SERVER_PORT", "65432")
	newWorkspace, newCfg := newBindingCommandWorkspaceConfig(t)
	if got := newCfg.Settings.ServerHost; got != "127.0.0.1" {
		t.Fatalf("server host = %q, want default loopback", got)
	}
	if got := newCfg.Source.Sources["server_host"]; got != "default" {
		t.Fatalf("server_host source = %q, want default", got)
	}
	if got := newCfg.Source.Sources["server_port"]; got != "env" {
		t.Fatalf("server_port source = %q, want env", got)
	}
	bindingCommandRemoteOpener = func(context.Context, string) (config.App, *client.Remote, error) {
		return config.App{}, nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect refused")}
	}
	localCalls := 0
	bindingCommandLocalSessionLifecycleClient = func(config.App) client.SessionLifecycleClient {
		localCalls++
		return bindingCommandTimeoutSessionLifecycleStub{}
	}

	_, err := retargetSessionWorkspace(context.Background(), "session-123", newWorkspace)
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Fatalf("retargetSessionWorkspace error = %v, want net.OpError", err)
	}
	if localCalls != 0 {
		t.Fatalf("local calls = %d, want 0", localCalls)
	}
}

func TestResolveWorkspaceBindingAppliesRPCTimeout(t *testing.T) {
	originalTimeout := bindingCommandRPCTimeout
	bindingCommandRPCTimeout = 20 * time.Millisecond
	t.Cleanup(func() { bindingCommandRPCTimeout = originalTimeout })

	stub := bindingCommandTimeoutProjectViewStub{
		resolveProjectPath: func(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
			<-ctx.Done()
			return serverapi.ProjectResolvePathResponse{}, ctx.Err()
		},
	}
	start := time.Now()
	_, err := resolveWorkspaceBinding(context.Background(), stub, "/tmp/workspace")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resolveWorkspaceBinding error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("resolveWorkspaceBinding timeout took too long: %v", elapsed)
	}
}

func TestBindingCommandProjectRPCWrappersApplyTimeout(t *testing.T) {
	originalTimeout := bindingCommandRPCTimeout
	bindingCommandRPCTimeout = 20 * time.Millisecond
	t.Cleanup(func() { bindingCommandRPCTimeout = originalTimeout })

	deadlineErrAfterCancel := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	stub := bindingCommandTimeoutProjectViewStub{
		listProjects: func(ctx context.Context, req serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error) {
			return serverapi.ProjectListResponse{}, deadlineErrAfterCancel(ctx)
		},
		createProject: func(ctx context.Context, req serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
			return serverapi.ProjectCreateResponse{}, deadlineErrAfterCancel(ctx)
		},
		attachWorkspace: func(ctx context.Context, req serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
			return serverapi.ProjectAttachWorkspaceResponse{}, deadlineErrAfterCancel(ctx)
		},
		rebindWorkspace: func(ctx context.Context, req serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
			return serverapi.ProjectRebindWorkspaceResponse{}, deadlineErrAfterCancel(ctx)
		},
	}

	assertDeadlineExceeded := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s error = %v, want deadline exceeded", name, err)
		}
	}

	_, err := listProjectsWithTimeout(context.Background(), stub)
	assertDeadlineExceeded("listProjectsWithTimeout", err)
	_, err = createProjectWithTimeout(context.Background(), stub, "project", "/tmp/workspace")
	assertDeadlineExceeded("createProjectWithTimeout", err)
	_, err = attachWorkspaceToProject(context.Background(), stub, "project-1", "/tmp/workspace")
	assertDeadlineExceeded("attachWorkspaceToProject", err)
	_, err = rebindWorkspaceWithTimeout(context.Background(), stub, "/tmp/old", "/tmp/new")
	assertDeadlineExceeded("rebindWorkspaceWithTimeout", err)
}

func TestRetargetSessionWorkspaceWithTimeoutAppliesTimeout(t *testing.T) {
	originalTimeout := bindingCommandRPCTimeout
	bindingCommandRPCTimeout = 20 * time.Millisecond
	t.Cleanup(func() { bindingCommandRPCTimeout = originalTimeout })

	stub := bindingCommandTimeoutSessionLifecycleStub{retargetSessionWorkspace: func(ctx context.Context, req serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
		<-ctx.Done()
		return serverapi.SessionRetargetWorkspaceResponse{}, ctx.Err()
	}}

	start := time.Now()
	_, err := retargetSessionWorkspaceWithTimeout(context.Background(), stub, "session-1", "/tmp/workspace")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retargetSessionWorkspaceWithTimeout error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("retargetSessionWorkspaceWithTimeout took too long: %v", elapsed)
	}
}
