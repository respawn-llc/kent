package sessionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"core/server/auth"
	"core/server/llm"
	"core/server/metadata"
	"core/server/registry"
	runtimepkg "core/server/runtime"
	"core/server/session"
	"core/server/tools"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/toolspec"
	"core/shared/transcript"
)

type sessionRuntimeTestLLMClient struct {
	responses []llm.Response
}

func (c *sessionRuntimeTestLLMClient) Generate(_ context.Context, _ llm.Request) (llm.Response, error) {
	if len(c.responses) == 0 {
		return llm.Response{}, nil
	}
	resp := c.responses[0]
	c.responses = c.responses[1:]
	return resp, nil
}

type sessionRuntimeTestTool struct {
	name toolspec.ID
}

func (t sessionRuntimeTestTool) Call(_ context.Context, c tools.Call) (tools.Result, error) {
	out, _ := json.Marshal(map[string]string{"tool": string(t.name)})
	return tools.Result{CallID: c.ID, Name: c.Name, Output: out}, nil
}

type patchDetailCapture struct {
	mu    sync.Mutex
	value string
}

func (c *patchDetailCapture) Set(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = value
}

func (c *patchDetailCapture) Get() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

func TestClaimActivationReusesDuplicateRequest(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)

	handle, takeover, claim, err := svc.claimActivation("session-1", "req-1", "")
	if err != nil {
		t.Fatalf("claimActivation: %v", err)
	}
	if takeover != nil {
		t.Fatalf("claimActivation takeover = %+v, want nil", takeover)
	}
	if claim != activationClaimReuse {
		t.Fatal("expected duplicate activation to reuse existing controller")
	}
	if handle != svc.handles["session-1"] {
		t.Fatal("expected duplicate activation to return existing handle")
	}
}

func TestClaimActivationAllowsTakeoverAfterReady(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)

	handle, takeover, claim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation: %v", err)
	}
	if takeover == nil {
		t.Fatal("expected takeover activation to allocate pending takeover state")
	}
	if claim != activationClaimTakeover {
		t.Fatalf("claimActivation claim = %v, want takeover", claim)
	}
	if handle != svc.handles["session-1"] {
		t.Fatal("expected takeover activation to return existing handle")
	}
}

func TestClaimActivationReusesPendingTakeoverRequest(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)

	handle, takeover, claim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation first takeover: %v", err)
	}
	if claim != activationClaimTakeover {
		t.Fatalf("first claimActivation claim = %v, want takeover", claim)
	}
	reusedHandle, reusedTakeover, reusedClaim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation pending retry: %v", err)
	}
	if reusedClaim != activationClaimTakeoverReuse {
		t.Fatalf("pending retry claim = %v, want takeover reuse", reusedClaim)
	}
	if reusedHandle != handle {
		t.Fatal("expected pending retry to return same handle")
	}
	if reusedTakeover != takeover {
		t.Fatal("expected pending retry to return same takeover state")
	}
	handle.controllerLeaseID = ""
	if ok, err := svc.completeTakeover(context.Background(), "session-1", handle, takeover, "req-2", "lease-2", ""); err != nil || !ok {
		t.Fatal("expected completeTakeover to succeed")
	}
	resp, err := activationResponseForTakeover(reusedTakeover)
	if err != nil {
		t.Fatalf("activationResponseForTakeover: %v", err)
	}
	if resp.LeaseID != "lease-2" {
		t.Fatalf("takeover lease id = %q, want lease-2", resp.LeaseID)
	}
}

func TestPendingTakeoverRetryUnblocksWhenTakeoverFails(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)

	handle, takeover, claim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation first takeover: %v", err)
	}
	if claim != activationClaimTakeover {
		t.Fatalf("first claimActivation claim = %v, want takeover", claim)
	}
	_, reusedTakeover, reusedClaim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation pending retry: %v", err)
	}
	if reusedClaim != activationClaimTakeoverReuse {
		t.Fatalf("pending retry claim = %v, want takeover reuse", reusedClaim)
	}
	errCh := make(chan error, 1)
	go func() {
		if err := waitForRuntimeTakeoverReady(context.Background(), reusedTakeover); err != nil {
			errCh <- err
			return
		}
		_, err := activationResponseForTakeover(reusedTakeover)
		errCh <- err
	}()

	expectedErr := errors.Join(serverapi.ErrSessionAlreadyControlled, errors.New("takeover lost"))
	svc.failTakeover("session-1", handle, takeover, expectedErr)

	err = <-errCh
	if !errors.Is(err, serverapi.ErrSessionAlreadyControlled) {
		t.Fatalf("takeover waiter error = %v, want session already controlled", err)
	}
}

func TestCloseReleasedRuntimeHandleSignalsPendingTakeoverWaiters(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)

	handle, takeover, claim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation first takeover: %v", err)
	}
	if claim != activationClaimTakeover {
		t.Fatalf("first claimActivation claim = %v, want takeover", claim)
	}
	_, reusedTakeover, reusedClaim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation pending retry: %v", err)
	}
	if reusedClaim != activationClaimTakeoverReuse {
		t.Fatalf("pending retry claim = %v, want takeover reuse", reusedClaim)
	}
	errCh := make(chan error, 1)
	go func() {
		if err := waitForRuntimeTakeoverReady(context.Background(), reusedTakeover); err != nil {
			errCh <- err
			return
		}
		_, err := activationResponseForTakeover(reusedTakeover)
		errCh <- err
	}()

	svc.closeReleasedRuntimeHandle("session-1", handle)

	err = <-errCh
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("takeover waiter error = %v, want invalid controller lease", err)
	}
	if _, ok := svc.handles["session-1"]; ok {
		t.Fatal("expected runtime handle removed after closeReleasedRuntimeHandle")
	}
	if takeover.err == nil {
		t.Fatal("expected takeover terminal error to be recorded")
	}
}

func TestClaimActivationRejectsConcurrentDifferentTakeoverRequest(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)

	_, _, claim, err := svc.claimActivation("session-1", "req-2", "")
	if err != nil {
		t.Fatalf("claimActivation first takeover: %v", err)
	}
	if claim != activationClaimTakeover {
		t.Fatalf("first claimActivation claim = %v, want takeover", claim)
	}
	_, _, _, err = svc.claimActivation("session-1", "req-3", "")
	if !errors.Is(err, serverapi.ErrSessionAlreadyControlled) {
		t.Fatalf("claimActivation competing takeover error = %v, want session already controlled", err)
	}
}

func TestActivateSessionRuntimeWaitsForClosingHandleBeforeClaiming(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	fixture.service.authManager = auth.NewManager(auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "sk-test"},
		},
	}), nil, time.Now)
	ready := make(chan struct{})
	close(ready)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   "lease-1",
		closing:             true,
		ready:               ready,
		closed:              make(chan struct{}),
	}
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}

	done := make(chan serverapi.SessionRuntimeActivateResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
			ClientRequestID: "req-2",
			SessionID:       fixture.store.Meta().SessionID,
			ActiveSettings: config.Settings{
				Model: "gpt-5",
				Reviewer: config.ReviewerSettings{
					Frequency: "off",
				},
			},
		})
		done <- resp
		errCh <- err
	}()
	select {
	case <-done:
		t.Fatal("expected activation to wait for closing handle")
	default:
	}

	fixture.service.closeReleasedRuntimeHandle(fixture.store.Meta().SessionID, handle)
	if err := <-errCh; err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	if strings.TrimSpace((<-done).LeaseID) == "" {
		t.Fatal("expected replacement activation lease id")
	}
}

func TestActivateSessionRuntimeReplaysDuplicateRequestAfterReady(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   "lease-1",
		ready:               make(chan struct{}),
	}
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}
	done := make(chan serverapi.SessionRuntimeActivateResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
			ClientRequestID: "req-1",
			SessionID:       fixture.store.Meta().SessionID,
		})
		done <- resp
		errCh <- err
	}()
	select {
	case <-done:
		t.Fatal("expected duplicate activation to wait for ready handle")
	default:
	}
	close(handle.ready)
	if err := <-errCh; err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	if got := (<-done).LeaseID; got != "lease-1" {
		t.Fatalf("lease id = %q, want lease-1", got)
	}
	if handle.ownerRefs != 1 {
		t.Fatalf("owner refs after replay = %d, want 1", handle.ownerRefs)
	}
}

func TestActivateSessionRuntimeReplayOwnerSurvivesOriginalDisconnect(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           1,
		ownerIDs:            map[string]struct{}{"owner-1": {}},
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	resp, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-1",
		SessionID:       fixture.store.Meta().SessionID,
		OwnerID:         "owner-2",
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime replay: %v", err)
	}
	if resp.LeaseID != lease.LeaseID {
		t.Fatalf("replay lease = %q, want %q", resp.LeaseID, lease.LeaseID)
	}
	release, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
		OnlyIfIdle:      true,
		DropOwner:       true,
		OwnerID:         "owner-1",
	})
	if err != nil {
		t.Fatalf("ReleaseSessionRuntime original disconnect: %v", err)
	}
	if release.Released {
		t.Fatalf("release response = %+v, want unreleased while replay owner remains", release)
	}
	if closed.Load() != 0 {
		t.Fatalf("runtime closed despite replay owner, close count = %d", closed.Load())
	}
	if handle.ownerRefs != 1 {
		t.Fatalf("owner refs after original disconnect = %d, want replay owner", handle.ownerRefs)
	}
}

func TestActivateSessionRuntimeIdempotentReplayDoesNotDoubleCountSameOwner(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           1,
		ownerIDs:            map[string]struct{}{"owner-1": {}},
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	if _, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-1",
		SessionID:       fixture.store.Meta().SessionID,
		OwnerID:         "owner-1",
	}); err != nil {
		t.Fatalf("ActivateSessionRuntime replay: %v", err)
	}
	if handle.ownerRefs != 1 {
		t.Fatalf("owner refs after same-owner replay = %d, want 1", handle.ownerRefs)
	}
	release, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
		OnlyIfIdle:      true,
		DropOwner:       true,
		OwnerID:         "owner-1",
	})
	if err != nil {
		t.Fatalf("ReleaseSessionRuntime: %v", err)
	}
	if !release.Released {
		t.Fatalf("release response = %+v, want released after single owner disconnect", release)
	}
	if closed.Load() != 1 {
		t.Fatalf("runtime close count = %d, want 1", closed.Load())
	}
}

func TestActivateSessionRuntimeReissuesControllerLeaseForTakeover(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
	}
	close(handle.ready)
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}

	resp, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-2",
		SessionID:       fixture.store.Meta().SessionID,
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime takeover: %v", err)
	}
	if strings.TrimSpace(resp.LeaseID) == "" || resp.LeaseID == lease.LeaseID {
		t.Fatalf("takeover lease id = %q, want non-empty replacement for %q", resp.LeaseID, lease.LeaseID)
	}
	if handle.controllerRequestID != "req-2" {
		t.Fatalf("controller request id = %q, want req-2", handle.controllerRequestID)
	}
	if handle.controllerLeaseID != resp.LeaseID {
		t.Fatalf("controller lease id = %q, want %q", handle.controllerLeaseID, resp.LeaseID)
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("old takeover lease validation error = %v, want invalid controller lease", err)
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, resp.LeaseID); err != nil {
		t.Fatalf("new takeover lease should validate: %v", err)
	}
}

func TestActivateSessionRuntimeTakeoverResetsOwnerIDs(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	oldLease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease old: %v", err)
	}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   oldLease.LeaseID,
		ownerRefs:           1,
		ownerIDs:            map[string]struct{}{"owner-old": {}},
		ready:               make(chan struct{}),
		close:               func() {},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	resp, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-2",
		SessionID:       fixture.store.Meta().SessionID,
		OwnerID:         "owner-new",
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime takeover: %v", err)
	}
	if strings.TrimSpace(resp.LeaseID) == "" || resp.LeaseID == oldLease.LeaseID {
		t.Fatalf("takeover response = %+v, want new lease", resp)
	}
	if handle.ownerRefs != 1 {
		t.Fatalf("owner refs after takeover = %d, want 1", handle.ownerRefs)
	}
	if _, ok := handle.ownerIDs["owner-new"]; !ok || len(handle.ownerIDs) != 1 {
		t.Fatalf("owner ids after takeover = %+v, want only new owner", handle.ownerIDs)
	}

	if _, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-2",
		SessionID:       fixture.store.Meta().SessionID,
		OwnerID:         "owner-new",
	}); err != nil {
		t.Fatalf("ActivateSessionRuntime takeover replay: %v", err)
	}
	if handle.ownerRefs != 1 {
		t.Fatalf("owner refs after takeover replay = %d, want 1", handle.ownerRefs)
	}
}

func TestCompleteTakeoverDoesNotMutateHandleWhenPreviousLeaseReleaseFails(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	takeover := &runtimeTakeover{requestID: "req-2", ready: make(chan struct{})}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
		takeover:            takeover,
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := fixture.service.completeTakeover(ctx, fixture.store.Meta().SessionID, handle, takeover, "req-2", "lease-new", "")
	if err == nil || errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("completeTakeover error = %v, want operational error without invalid controller lease marker", err)
	}
	if ok {
		t.Fatal("completeTakeover ok = true, want false when previous lease release fails")
	}
	if handle.controllerRequestID != "req-1" || handle.controllerLeaseID != lease.LeaseID || handle.takeover != nil {
		t.Fatalf("handle mutated after failed takeover completion: %+v", handle)
	}
	select {
	case <-takeover.ready:
		if !errors.Is(takeover.err, context.Canceled) {
			t.Fatalf("takeover error = %v, want context canceled", takeover.err)
		}
	default:
		t.Fatal("takeover waiter was not signaled after failed takeover completion")
	}
	_, retryTakeover, claim, err := fixture.service.claimActivation(fixture.store.Meta().SessionID, "req-3", "")
	if err != nil {
		t.Fatalf("claimActivation retry: %v", err)
	}
	if claim != activationClaimTakeover || retryTakeover == nil || retryTakeover == takeover {
		t.Fatalf("retry claim = %v takeover=%+v, want fresh takeover", claim, retryTakeover)
	}
}

func TestActivateSessionRuntimeHonorsCanceledContextBeforeInstallingHandle(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fixture.service.ActivateSessionRuntime(ctx, serverapi.SessionRuntimeActivateRequest{ClientRequestID: "req-1", SessionID: fixture.store.Meta().SessionID})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ActivateSessionRuntime error = %v, want context canceled", err)
	}
	if len(fixture.service.handles) != 0 {
		t.Fatalf("expected no installed handles after canceled activation, got %+v", fixture.service.handles)
	}
}

func TestActivateSessionRuntimeReleasesLeaseOnActivationFailure(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	_, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-1",
		SessionID:       fixture.store.Meta().SessionID,
		EnabledToolIDs:  []string{"not-a-tool"},
	})
	if !errors.Is(err, errUnknownToolID) {
		t.Fatalf("ActivateSessionRuntime error = %v, want errUnknownToolID", err)
	}
	var leaseID string
	if err := fixture.metadata.DB().QueryRowContext(context.Background(), `SELECT id FROM runtime_leases WHERE session_id = ?`, fixture.store.Meta().SessionID).Scan(&leaseID); err != nil {
		t.Fatalf("query activation failure lease: %v", err)
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, leaseID); !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("activation failure lease validation error = %v, want invalid controller lease", err)
	}
}

func TestActivateSessionRuntimeIgnoresRecoveredWarningProviderError(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	fixture.service.authManager = auth.NewManager(auth.NewMemoryStore(auth.State{
		Scope: auth.ScopeGlobal,
		Method: auth.Method{
			Type:   auth.MethodAPIKey,
			APIKey: &auth.APIKeyMethod{Key: "sk-test"},
		},
	}), nil, time.Now)
	fixture.service.WithGeneratedRecoveredWarningProvider(func() (string, bool, error) {
		return "", false, errors.New("recovered dir unreadable")
	})

	resp, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-warning-error",
		SessionID:       fixture.store.Meta().SessionID,
		ActiveSettings: config.Settings{
			Model: "gpt-5",
			Reviewer: config.ReviewerSettings{
				Frequency: "off",
			},
		},
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime should ignore recovered warning lookup errors: %v", err)
	}
	if strings.TrimSpace(resp.LeaseID) == "" {
		t.Fatalf("expected runtime activation lease, got %+v", resp)
	}
}

func TestActivateSessionRuntimeAttachesReadOnlyToExternalActiveRuntime(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimes := registry.NewRuntimeRegistry()
	engine, err := runtimepkg.New(fixture.store, &sessionRuntimeTestLLMClient{}, tools.NewRegistry(), runtimepkg.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	defer func() { _ = engine.Close() }()
	runtimes.Register(fixture.store.Meta().SessionID, engine)
	t.Cleanup(func() { runtimes.Unregister(fixture.store.Meta().SessionID, engine) })
	fixture.service.runtimes = runtimes

	resp, err := fixture.service.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-external",
		SessionID:       fixture.store.Meta().SessionID,
		ActiveSettings:  config.Settings{Model: "gpt-5"},
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	if !resp.ReadOnly || strings.TrimSpace(resp.LeaseID) != "" {
		t.Fatalf("response = %+v, want read-only without lease", resp)
	}
	if len(fixture.service.handles) != 0 {
		t.Fatalf("external active runtime should not leave controller handles, got %+v", fixture.service.handles)
	}
	if !runtimes.IsSessionRuntimeActive(fixture.store.Meta().SessionID) {
		t.Fatal("expected external runtime to remain registered")
	}
}

func TestAppendRecoveredWarningIfNeededPersistsOnce(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	warning := "generated warning"
	if err := fixture.store.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable: %v", err)
	}
	fixture.service.WithGeneratedRecoveredWarning(warning)
	if err := fixture.service.appendRecoveredWarningIfNeeded(fixture.store); err != nil {
		t.Fatalf("append warning: %v", err)
	}
	if err := fixture.service.appendRecoveredWarningIfNeeded(fixture.store); err != nil {
		t.Fatalf("append duplicate warning: %v", err)
	}
	count := 0
	if err := fixture.store.WalkEvents(func(evt session.Event) error {
		if evt.Kind != "local_entry" {
			return nil
		}
		var entry recoveredWarningEntry
		if err := json.Unmarshal(evt.Payload, &entry); err != nil {
			return err
		}
		if entry.Role == "warning" && entry.Text == warning {
			count++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk events: %v", err)
	}
	if count != 1 {
		t.Fatalf("warning count = %d, want 1", count)
	}
	if !fixture.store.Meta().GeneratedRecoveredWarningIssued {
		t.Fatal("expected generated recovered warning marker to be persisted")
	}
	reopened, err := session.OpenByID(fixture.config.PersistenceRoot, fixture.store.Meta().SessionID, fixture.metadata.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if !reopened.Meta().GeneratedRecoveredWarningIssued {
		t.Fatal("expected generated recovered warning marker to survive reopen")
	}
	if err := fixture.service.appendRecoveredWarningIfNeeded(reopened); err != nil {
		t.Fatalf("append warning after reopen: %v", err)
	}
	reopenedCount := 0
	if err := reopened.WalkEvents(func(evt session.Event) error {
		if evt.Kind != "local_entry" {
			return nil
		}
		var entry recoveredWarningEntry
		if err := json.Unmarshal(evt.Payload, &entry); err != nil {
			return err
		}
		if entry.Role == "warning" && entry.Text == warning {
			reopenedCount++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk reopened events: %v", err)
	}
	if reopenedCount != 1 {
		t.Fatalf("reopened warning count = %d, want 1", reopenedCount)
	}
}

func TestAppendRecoveredWarningIfNeededIgnoresProviderError(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	fixture.service.WithGeneratedRecoveredWarningProvider(func() (string, bool, error) {
		return "", false, errors.New("recovered dir unreadable")
	})
	if err := fixture.service.appendRecoveredWarningIfNeeded(fixture.store); err != nil {
		t.Fatalf("expected warning lookup errors to be non-fatal, got %v", err)
	}
}

func TestReleaseSessionRuntimeWaitsForHandleReadyBeforeClose(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := make(chan struct{}, 1)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
		close: func() {
			closed <- struct{}{}
		},
	}
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}
	done := make(chan error, 1)
	go func() {
		_, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
			ClientRequestID: "rel-1",
			SessionID:       fixture.store.Meta().SessionID,
			LeaseID:         lease.LeaseID,
		})
		done <- err
	}()
	select {
	case <-closed:
		t.Fatal("expected release to wait for ready handle before close")
	default:
	}
	close(handle.ready)
	if err := <-done; err != nil {
		t.Fatalf("ReleaseSessionRuntime: %v", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("expected close after ready handle release")
	}
}

func TestReleaseSessionRuntimeOnlyIfIdleKeepsActivePrimaryRun(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimeRegistry := registry.NewRuntimeRegistry()
	fixture.service.runtimes = runtimeRegistry
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           1,
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	active, err := runtimeRegistry.AcquirePrimaryRun(fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("AcquirePrimaryRun: %v", err)
	}
	defer active.Release()

	resp, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
		OnlyIfIdle:      true,
	})
	if err != nil {
		t.Fatalf("ReleaseSessionRuntime OnlyIfIdle: %v", err)
	}
	if !resp.Active || resp.Released {
		t.Fatalf("release response = %+v, want active not released", resp)
	}
	if closed.Load() != 0 {
		t.Fatalf("runtime close count = %d, want 0", closed.Load())
	}
	if got := fixture.service.handles[fixture.store.Meta().SessionID]; got != handle {
		t.Fatalf("runtime handle = %+v, want preserved active handle", got)
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("active runtime lease should remain valid: %v", err)
	}
	if handle.ownerRefs != 1 {
		t.Fatalf("connected owner refs = %d, want preserved owner", handle.ownerRefs)
	}
}

func TestReleaseSessionRuntimeOnlyIfIdleDropsOwnerWhenRequested(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimeRegistry := registry.NewRuntimeRegistry()
	fixture.service.runtimes = runtimeRegistry
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           1,
		ready:               make(chan struct{}),
		close:               func() {},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	active, err := runtimeRegistry.AcquirePrimaryRun(fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("AcquirePrimaryRun: %v", err)
	}
	defer active.Release()

	resp, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
		OnlyIfIdle:      true,
		DropOwner:       true,
	})
	if err != nil {
		t.Fatalf("ReleaseSessionRuntime OnlyIfIdle DropOwner: %v", err)
	}
	if !resp.Active || resp.Released {
		t.Fatalf("release response = %+v, want active not released", resp)
	}
	if handle.ownerRefs != 0 {
		t.Fatalf("owner refs = %d, want dropped owner", handle.ownerRefs)
	}
}

func TestReleaseSessionRuntimeOnlyIfIdleKeepsSubscriberRuntime(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimeRegistry := registry.NewRuntimeRegistry()
	fixture.service.runtimes = runtimeRegistry
	engine, err := runtimepkg.New(fixture.store, &sessionRuntimeTestLLMClient{}, tools.NewRegistry(), runtimepkg.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	runtimeRegistry.Register(fixture.store.Meta().SessionID, engine)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           1,
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	sub, err := runtimeRegistry.SubscribeSessionActivity(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("SubscribeSessionActivity: %v", err)
	}
	defer func() { _ = sub.Close() }()

	resp, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
		OnlyIfIdle:      true,
		DropOwner:       true,
	})
	if err != nil {
		t.Fatalf("ReleaseSessionRuntime OnlyIfIdle with subscriber: %v", err)
	}
	if resp.Released {
		t.Fatalf("release response = %+v, want unreleased while subscriber is connected", resp)
	}
	if closed.Load() != 0 {
		t.Fatalf("runtime close count = %d, want 0", closed.Load())
	}
	if handle.ownerRefs != 0 {
		t.Fatalf("owner refs = %d, want dropped owner", handle.ownerRefs)
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("subscriber runtime lease should remain valid: %v", err)
	}
}

func TestReleaseSessionRuntimeOnlyIfIdleClosesIdleRuntime(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	fixture.service.runtimes = registry.NewRuntimeRegistry()
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	resp, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
		OnlyIfIdle:      true,
	})
	if err != nil {
		t.Fatalf("ReleaseSessionRuntime OnlyIfIdle: %v", err)
	}
	if !resp.Released || resp.Active {
		t.Fatalf("release response = %+v, want released not active", resp)
	}
	if closed.Load() != 1 {
		t.Fatalf("runtime close count = %d, want 1", closed.Load())
	}
	if _, ok := fixture.service.handles[fixture.store.Meta().SessionID]; ok {
		t.Fatal("expected runtime handle removed after idle release")
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("released runtime lease validation error = %v, want invalid controller lease", err)
	}
}

func TestIdleRuntimeUnloadReleasesOrphanedRuntimeAfterPrimaryRunFinishes(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimeRegistry := registry.NewRuntimeRegistry()
	runtimeRegistry.SetInterestObserver(fixture.service.runtimeInterestChanged)
	fixture.service.runtimes = runtimeRegistry
	fixture.service.idleUnloadDelay = 100 * time.Millisecond
	fixture.service.runFinishedUnloadDelay = 40 * time.Millisecond
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := make(chan struct{}, 1)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           0,
		ready:               make(chan struct{}),
		close: func() {
			closed <- struct{}{}
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	active, err := runtimeRegistry.AcquirePrimaryRun(fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("AcquirePrimaryRun: %v", err)
	}
	fixture.service.runtimeInterestChanged(fixture.store.Meta().SessionID, registry.RuntimeInterestChanged)
	select {
	case <-closed:
		t.Fatal("idle reaper closed runtime while primary run was active")
	case <-time.After(30 * time.Millisecond):
	}
	active.Release()
	fixture.service.runtimeInterestChanged(fixture.store.Meta().SessionID, registry.RuntimeInterestRunFinished)
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for idle reaper to close orphaned runtime")
	}
}

func TestIdleRuntimeUnloadRetriesAfterNonRuntimePrimaryWorkFinishes(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimeRegistry := registry.NewRuntimeRegistry()
	fixture.service.runtimes = runtimeRegistry
	fixture.service.idleUnloadDelay = 20 * time.Millisecond
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := make(chan struct{}, 1)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           0,
		ready:               make(chan struct{}),
		close: func() {
			closed <- struct{}{}
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	active, err := runtimeRegistry.AcquirePrimaryRun(fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("AcquirePrimaryRun: %v", err)
	}

	fixture.service.runtimeInterestChanged(fixture.store.Meta().SessionID, registry.RuntimeInterestChanged)
	select {
	case <-closed:
		t.Fatal("idle reaper closed runtime while primary work was active")
	case <-time.After(60 * time.Millisecond):
	}
	active.Release()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for idle reaper retry after primary work finished")
	}
}

func TestIdleRuntimeUnloadUsesThreeMinuteDefaultAfterRunFinishes(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	if fixture.service.runFinishedIdleUnloadDelay() != 3*time.Minute {
		t.Fatalf("run finished idle unload delay = %s, want 3m", fixture.service.runFinishedIdleUnloadDelay())
	}
}

func TestIdleRuntimeUnloadUsesRunFinishedDebounceInsteadOfDisconnectDebounce(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	fixture.service.runtimes = registry.NewRuntimeRegistry()
	fixture.service.idleUnloadDelay = 10 * time.Millisecond
	fixture.service.runFinishedUnloadDelay = 40 * time.Millisecond
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := make(chan struct{}, 1)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           0,
		ready:               make(chan struct{}),
		close: func() {
			closed <- struct{}{}
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	fixture.service.runtimeInterestChanged(fixture.store.Meta().SessionID, registry.RuntimeInterestRunFinished)
	select {
	case <-closed:
		t.Fatal("idle reaper used disconnect debounce after run finished")
	case <-time.After(25 * time.Millisecond):
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for run-finished idle reaper")
	}
}

func TestIdleRuntimeUnloadWaitsForTransientSubscriberReconnect(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimeRegistry := registry.NewRuntimeRegistry()
	runtimeRegistry.SetInterestObserver(fixture.service.runtimeInterestChanged)
	fixture.service.runtimes = runtimeRegistry
	fixture.service.idleUnloadDelay = 30 * time.Millisecond
	engine, err := runtimepkg.New(fixture.store, &sessionRuntimeTestLLMClient{}, tools.NewRegistry(), runtimepkg.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	runtimeRegistry.Register(fixture.store.Meta().SessionID, engine)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := make(chan struct{}, 1)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           0,
		ready:               make(chan struct{}),
		close: func() {
			closed <- struct{}{}
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	sub, err := runtimeRegistry.SubscribeSessionActivity(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("SubscribeSessionActivity: %v", err)
	}
	fixture.service.runtimeInterestChanged(fixture.store.Meta().SessionID, registry.RuntimeInterestChanged)
	select {
	case <-closed:
		t.Fatal("idle reaper closed runtime while subscriber was connected")
	case <-time.After(60 * time.Millisecond):
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("close subscription: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for idle reaper to close after subscriber disconnect")
	}
}

func TestIdleRuntimeUnloadWaitsForSubscriberReconnectBeforeDebounce(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	runtimeRegistry := registry.NewRuntimeRegistry()
	runtimeRegistry.SetInterestObserver(fixture.service.runtimeInterestChanged)
	fixture.service.runtimes = runtimeRegistry
	fixture.service.idleUnloadDelay = 40 * time.Millisecond
	engine, err := runtimepkg.New(fixture.store, &sessionRuntimeTestLLMClient{}, tools.NewRegistry(), runtimepkg.Config{Model: "gpt-5"})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	runtimeRegistry.Register(fixture.store.Meta().SessionID, engine)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := make(chan struct{}, 1)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           0,
		ready:               make(chan struct{}),
		close: func() {
			closed <- struct{}{}
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	fixture.service.runtimeInterestChanged(fixture.store.Meta().SessionID, registry.RuntimeInterestChanged)
	time.Sleep(15 * time.Millisecond)
	sub, err := runtimeRegistry.SubscribeSessionActivity(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("SubscribeSessionActivity reconnect: %v", err)
	}
	select {
	case <-closed:
		t.Fatal("idle reaper closed runtime despite subscriber reconnect before debounce")
	case <-time.After(80 * time.Millisecond):
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("close subscription: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for idle reaper to close after reconnected subscriber disconnect")
	}
}

func TestIdleRuntimeUnloadIgnoresStaleTimerAfterOwnerReconnect(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	fixture.service.runtimes = registry.NewRuntimeRegistry()
	fixture.service.idleUnloadDelay = 30 * time.Millisecond
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := make(chan struct{}, 1)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ownerRefs:           0,
		ready:               make(chan struct{}),
		close: func() {
			closed <- struct{}{}
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	fixture.service.runtimeInterestChanged(fixture.store.Meta().SessionID, registry.RuntimeInterestChanged)
	fixture.service.mu.Lock()
	handle.ownerRefs = 1
	fixture.service.mu.Unlock()
	fixture.service.cancelScheduledIdleUnload(fixture.store.Meta().SessionID)

	select {
	case <-closed:
		t.Fatal("idle reaper closed runtime after owner reconnected")
	case <-time.After(90 * time.Millisecond):
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("owner reconnect should keep runtime lease valid: %v", err)
	}
}

func TestReleaseSessionRuntimeClosesHandleWhenLeaseValidatedAndWaitCanceled(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	if _, err := fixture.metadata.ValidateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("ValidateRuntimeLease setup: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = fixture.service.ReleaseSessionRuntime(ctx, serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReleaseSessionRuntime error = %v, want context deadline exceeded", err)
	}
	if closed.Load() != 1 {
		t.Fatalf("expected closeFn to run exactly once, got %d", closed.Load())
	}
	if _, ok := fixture.service.handles[fixture.store.Meta().SessionID]; ok {
		t.Fatal("expected runtime handle removed after canceled wait with validated lease")
	}
}

func TestReleaseSessionRuntimeRejectsInvalidLeaseWithoutClosingHandle(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   "lease-missing",
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle
	_, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         "lease-missing",
	})
	if err == nil {
		t.Fatal("expected lease validation error for missing lease record")
	}
	if closed.Load() != 0 {
		t.Fatalf("expected invalid lease to preserve runtime handle, close count = %d", closed.Load())
	}
	if _, ok := fixture.service.handles[fixture.store.Meta().SessionID]; ok {
		return
	}
	t.Fatal("expected runtime handle to remain when lease validation fails")
}

func TestReleaseSessionRuntimeRejectsReleasedLeaseWithoutClosingHandle(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	if _, err := fixture.metadata.ReleaseRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("ReleaseRuntimeLease setup: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	_, err = fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
	})
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("ReleaseSessionRuntime error = %v, want invalid controller lease", err)
	}
	if closed.Load() != 0 {
		t.Fatalf("released stale lease closed runtime handle, close count = %d", closed.Load())
	}
	if got := fixture.service.handles[fixture.store.Meta().SessionID]; got != handle {
		t.Fatalf("runtime handle = %+v, want preserved handle", got)
	}
}

func TestReleaseSessionRuntimeSucceedsWhenHandleAlreadyMissingAfterLeaseValidated(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	if _, err := fixture.metadata.ValidateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("ValidateRuntimeLease: %v", err)
	}

	if _, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
	}); err != nil {
		t.Fatalf("ReleaseSessionRuntime retry: %v", err)
	}
}

func TestReleaseSessionRuntimeValidatesPersistedLeaseWhenHandleAlreadyMissing(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}

	if _, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
	}); err != nil {
		t.Fatalf("ReleaseSessionRuntime: %v", err)
	}

	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("released lease validation error = %v, want invalid controller lease", err)
	}
}

func TestReleasedRuntimeLeaseCannotBeValidatedAgain(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
		close:               func() {},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	if _, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
	}); err != nil {
		t.Fatalf("ReleaseSessionRuntime: %v", err)
	}
	if _, err := fixture.service.validateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("released runtime lease still validates: %v", err)
	}
	if _, err := fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-2",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         lease.LeaseID,
	}); err != nil {
		t.Fatalf("ReleaseSessionRuntime retry should be idempotent: %v", err)
	}
}

func TestOperationalLeaseErrorsAreNotClassifiedAsInvalidControllerLease(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := fixture.service.validateRuntimeLease(ctx, fixture.store.Meta().SessionID, lease.LeaseID); err == nil || errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("validateRuntimeLease canceled error = %v, want operational error without invalid controller lease marker", err)
	}
	if _, err := fixture.service.releaseRuntimeLease(ctx, fixture.store.Meta().SessionID, lease.LeaseID); err == nil || errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("releaseRuntimeLease canceled error = %v, want operational error without invalid controller lease marker", err)
	}
}

func TestReleaseSessionRuntimeRejectsMismatchedControllerLeaseWithoutClosingHandle(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	closed := atomic.Int32{}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
		close: func() {
			closed.Add(1)
		},
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	_, err = fixture.service.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "rel-1",
		SessionID:       fixture.store.Meta().SessionID,
		LeaseID:         "lease-other",
	})
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("ReleaseSessionRuntime error = %v, want invalid controller lease", err)
	}
	if closed.Load() != 0 {
		t.Fatalf("expected closeFn not to run for mismatched lease, got %d", closed.Load())
	}
	if got := fixture.service.handles[fixture.store.Meta().SessionID]; got != handle {
		t.Fatalf("expected runtime handle preserved for mismatched lease, got %+v", got)
	}
	if _, err := fixture.metadata.ValidateRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("expected original runtime lease to remain releasable after mismatched release, got %v", err)
	}
}

func TestRequireControllerLeaseAcceptsActiveController(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)
	if err := svc.RequireControllerLease(context.Background(), "session-1", "lease-1"); err != nil {
		t.Fatalf("RequireControllerLease: %v", err)
	}
}

func TestRequireControllerLeaseRejectsReleasedControllerLease(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	if _, err := fixture.metadata.ReleaseRuntimeLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID); err != nil {
		t.Fatalf("ReleaseRuntimeLease: %v", err)
	}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
	}
	close(handle.ready)
	fixture.service.handles[fixture.store.Meta().SessionID] = handle

	err = fixture.service.RequireControllerLease(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID)
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("RequireControllerLease error = %v, want invalid controller lease", err)
	}
}

func TestRequireControllerLeaseRejectsUnknownLease(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	close(svc.handles["session-1"].ready)
	err := svc.RequireControllerLease(context.Background(), "session-1", "lease-2")
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("RequireControllerLease error = %v, want invalid controller lease", err)
	}
}

func TestRequireControllerLeaseRejectsReplacedHandleAfterReadyWait(t *testing.T) {
	svc := &Service{handles: map[string]*runtimeHandle{
		"session-1": {
			controllerRequestID: "req-1",
			controllerLeaseID:   "lease-1",
			ready:               make(chan struct{}),
		},
	}}
	original := svc.handles["session-1"]
	replacement := &runtimeHandle{
		controllerRequestID: "req-2",
		controllerLeaseID:   "lease-2",
		ready:               make(chan struct{}),
	}
	close(replacement.ready)
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.RequireControllerLease(context.Background(), "session-1", "lease-1")
	}()
	svc.mu.Lock()
	svc.handles["session-1"] = replacement
	svc.mu.Unlock()
	close(original.ready)
	err := <-errCh
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("RequireControllerLease error = %v, want invalid controller lease", err)
	}
}

func TestRecordWorktreeTransitionPersistsPendingReminderState(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	lease, err := fixture.metadata.CreateRuntimeLease(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("CreateRuntimeLease: %v", err)
	}
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   lease.LeaseID,
		ready:               make(chan struct{}),
	}
	close(handle.ready)
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}

	err = fixture.service.RecordWorktreeTransition(context.Background(), fixture.store.Meta().SessionID, lease.LeaseID, session.WorktreeReminderState{
		Mode:                  session.WorktreeReminderModeEnter,
		Branch:                " feature/worktree ",
		WorktreePath:          " /tmp/worktree-a ",
		WorkspaceRoot:         " /tmp/workspace ",
		EffectiveCwd:          " /tmp/worktree-a/pkg ",
		HasIssuedInGeneration: true,
		IssuedCompactionCount: 9,
	})
	if err != nil {
		t.Fatalf("RecordWorktreeTransition: %v", err)
	}

	resolved, err := fixture.service.resolveStore(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolveStore: %v", err)
	}
	state := resolved.Meta().WorktreeReminder
	if state == nil {
		t.Fatal("expected persisted worktree reminder state")
	}
	if state.Mode != session.WorktreeReminderModeEnter {
		t.Fatalf("mode = %q, want enter", state.Mode)
	}
	if state.Branch != "feature/worktree" {
		t.Fatalf("branch = %q, want feature/worktree", state.Branch)
	}
	if state.WorktreePath != "/tmp/worktree-a" {
		t.Fatalf("worktree path = %q, want /tmp/worktree-a", state.WorktreePath)
	}
	if state.WorkspaceRoot != "/tmp/workspace" {
		t.Fatalf("workspace root = %q, want /tmp/workspace", state.WorkspaceRoot)
	}
	if state.EffectiveCwd != "/tmp/worktree-a/pkg" {
		t.Fatalf("effective cwd = %q, want /tmp/worktree-a/pkg", state.EffectiveCwd)
	}
	if state.HasIssuedInGeneration {
		t.Fatal("expected reminder issuance reset for new transition")
	}
	if state.IssuedCompactionCount != 0 {
		t.Fatalf("issued compaction count = %d, want 0", state.IssuedCompactionCount)
	}
}

func TestEnsureCurrentControllerLeaseLockedRejectsChangedLease(t *testing.T) {
	handle := &runtimeHandle{controllerRequestID: "req-1", controllerLeaseID: "lease-1", ready: make(chan struct{})}
	svc := &Service{handles: map[string]*runtimeHandle{"session-1": handle}}

	err := svc.ensureCurrentControllerLeaseLocked("session-1", "lease-2", handle)
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("ensureCurrentControllerLeaseLocked error = %v, want invalid controller lease", err)
	}
}

func TestEnsureCurrentControllerLeaseLockedRejectsReplacedHandle(t *testing.T) {
	original := &runtimeHandle{controllerRequestID: "req-1", controllerLeaseID: "lease-1", ready: make(chan struct{})}
	replacement := &runtimeHandle{controllerRequestID: "req-2", controllerLeaseID: "lease-1", ready: make(chan struct{})}
	svc := &Service{handles: map[string]*runtimeHandle{"session-1": replacement}}

	err := svc.ensureCurrentControllerLeaseLocked("session-1", "lease-1", original)
	if !errors.Is(err, serverapi.ErrInvalidControllerLease) {
		t.Fatalf("ensureCurrentControllerLeaseLocked error = %v, want invalid controller lease", err)
	}
}

func TestActiveRuntimeHandleReturnsActivationError(t *testing.T) {
	activationErr := errors.New("activation failed")
	handle := &runtimeHandle{controllerRequestID: "req-1", controllerLeaseID: "lease-1", activationErr: activationErr, ready: make(chan struct{})}
	close(handle.ready)
	svc := &Service{handles: map[string]*runtimeHandle{"session-1": handle}}

	resolved, err := svc.activeRuntimeHandle(context.Background(), "session-1")
	if !errors.Is(err, activationErr) {
		t.Fatalf("activeRuntimeHandle error = %v, want %v", err, activationErr)
	}
	if resolved != nil {
		t.Fatalf("activeRuntimeHandle returned handle %+v, want nil", resolved)
	}
}

func TestSyncExecutionTargetPersistsReminderWithoutActiveRuntime(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)

	err := fixture.service.SyncExecutionTarget(context.Background(), fixture.store.Meta().SessionID, clientui.SessionExecutionTarget{
		WorkspaceRoot:    " /tmp/workspace ",
		EffectiveWorkdir: " /tmp/workspace ",
	}, &session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeExit,
		Branch:        " feature/worktree ",
		WorktreePath:  " /tmp/worktree-a ",
		WorkspaceRoot: " /tmp/workspace ",
		EffectiveCwd:  " /tmp/workspace ",
	})
	if err != nil {
		t.Fatalf("SyncExecutionTarget: %v", err)
	}

	resolved, err := fixture.service.resolveStore(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolveStore: %v", err)
	}
	state := resolved.Meta().WorktreeReminder
	if state == nil {
		t.Fatal("expected persisted worktree reminder state")
	}
	if state.Mode != session.WorktreeReminderModeExit {
		t.Fatalf("mode = %q, want exit", state.Mode)
	}
	if state.Branch != "feature/worktree" {
		t.Fatalf("branch = %q, want feature/worktree", state.Branch)
	}
	if state.WorktreePath != "/tmp/worktree-a" {
		t.Fatalf("worktree path = %q, want /tmp/worktree-a", state.WorktreePath)
	}
	if state.EffectiveCwd != "/tmp/workspace" {
		t.Fatalf("effective cwd = %q, want /tmp/workspace", state.EffectiveCwd)
	}
}

func TestSyncExecutionTargetRebindsActiveRuntime(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	reboundRoot := ""
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   "lease-1",
		ready:               make(chan struct{}),
		rebind: func(root string) error {
			reboundRoot = root
			return nil
		},
	}
	close(handle.ready)
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}

	err := fixture.service.SyncExecutionTarget(context.Background(), fixture.store.Meta().SessionID, clientui.SessionExecutionTarget{
		EffectiveWorkdir: " /tmp/workspace/pkg ",
	}, nil)
	if err != nil {
		t.Fatalf("SyncExecutionTarget: %v", err)
	}
	if reboundRoot != "/tmp/workspace/pkg" {
		t.Fatalf("rebound root = %q, want /tmp/workspace/pkg", reboundRoot)
	}
}

func TestSyncExecutionTargetUpdatesActiveRuntimePatchTranscriptWorkdir(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	patchText := "*** Begin Patch\n*** Add File: probe.txt\n+hello\n*** End Patch\n"
	client := &sessionRuntimeTestLLMClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "patching", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call-patch", Name: string(toolspec.ToolPatch), Input: json.RawMessage(`{"patch":` + strconv.Quote(patchText) + `}`)}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	var detail patchDetailCapture
	engine, err := runtimepkg.New(fixture.store, client, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolPatch, Handler: sessionRuntimeTestTool{name: toolspec.ToolPatch}}), runtimepkg.Config{
		Model:                "gpt-5",
		TranscriptWorkingDir: "/old-worktree",
		OnEvent: func(evt runtimepkg.Event) {
			if evt.Kind != runtimepkg.EventToolCallStarted || evt.ToolCall == nil {
				return
			}
			meta, ok := transcript.DecodeToolCallMeta(evt.ToolCall.Presentation)
			if ok {
				detail.Set(meta.PatchDetail)
			}
		},
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	defer func() { _ = engine.Close() }()
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   "lease-1",
		ready:               make(chan struct{}),
		rebind:              runtimeRebindFunc(func(string) error { return nil }, engine),
	}
	close(handle.ready)
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}

	if err := fixture.service.SyncExecutionTarget(context.Background(), fixture.store.Meta().SessionID, clientui.SessionExecutionTarget{EffectiveWorkdir: "/new-worktree"}, nil); err != nil {
		t.Fatalf("SyncExecutionTarget: %v", err)
	}
	if _, err := engine.SubmitUserMessage(context.Background(), "apply patch"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	gotDetail := detail.Get()
	if !strings.Contains(gotDetail, "/new-worktree/probe.txt") {
		t.Fatalf("expected patch detail to use retargeted workdir, got %q", gotDetail)
	}
	if strings.Contains(gotDetail, "/old-worktree/probe.txt") {
		t.Fatalf("did not expect old workdir in patch detail, got %q", gotDetail)
	}
}

func TestSyncExecutionTargetDoesNotPersistReminderWhenActiveRuntimeRebindFails(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	handle := &runtimeHandle{
		controllerRequestID: "req-1",
		controllerLeaseID:   "lease-1",
		ready:               make(chan struct{}),
		rebind: func(string) error {
			return errors.New("rebind failed")
		},
	}
	close(handle.ready)
	fixture.service.handles = map[string]*runtimeHandle{fixture.store.Meta().SessionID: handle}

	err := fixture.service.SyncExecutionTarget(context.Background(), fixture.store.Meta().SessionID, clientui.SessionExecutionTarget{
		EffectiveWorkdir: "/tmp/workspace/pkg",
	}, &session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeExit,
		Branch:        "feature/worktree",
		WorktreePath:  "/tmp/worktree-a",
		WorkspaceRoot: "/tmp/workspace",
		EffectiveCwd:  "/tmp/workspace",
	})
	if err == nil || !strings.Contains(err.Error(), "rebind failed") {
		t.Fatalf("SyncExecutionTarget error = %v, want rebind failure", err)
	}

	resolved, err := fixture.service.resolveStore(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolveStore: %v", err)
	}
	if state := resolved.Meta().WorktreeReminder; state != nil {
		t.Fatalf("expected reminder state not persisted after failed rebind, got %+v", state)
	}
}

func TestRuntimeRebindDoesNotAdvanceTranscriptWorkdirWhenLocalRebindFails(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	patchText := "*** Begin Patch\n*** Add File: probe.txt\n+hello\n*** End Patch\n"
	client := &sessionRuntimeTestLLMClient{responses: []llm.Response{
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "patching", Phase: llm.MessagePhaseCommentary},
			ToolCalls: []llm.ToolCall{{ID: "call-patch", Name: string(toolspec.ToolPatch), Input: json.RawMessage(`{"patch":` + strconv.Quote(patchText) + `}`)}},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
		{
			Assistant: llm.Message{Role: llm.RoleAssistant, Content: "done", Phase: llm.MessagePhaseFinal},
			Usage:     llm.Usage{WindowTokens: 200000},
		},
	}}
	var detail patchDetailCapture
	engine, err := runtimepkg.New(fixture.store, client, tools.NewRegistry(tools.HandlerRegistration{ID: toolspec.ToolPatch, Handler: sessionRuntimeTestTool{name: toolspec.ToolPatch}}), runtimepkg.Config{
		Model:                "gpt-5",
		TranscriptWorkingDir: "/old-worktree",
		OnEvent: func(evt runtimepkg.Event) {
			if evt.Kind != runtimepkg.EventToolCallStarted || evt.ToolCall == nil {
				return
			}
			meta, ok := transcript.DecodeToolCallMeta(evt.ToolCall.Presentation)
			if ok {
				detail.Set(meta.PatchDetail)
			}
		},
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	defer func() { _ = engine.Close() }()
	rebindErr := runtimeRebindFunc(func(string) error { return errors.New("local rebind failed") }, engine)("/new-worktree")
	if rebindErr == nil || !strings.Contains(rebindErr.Error(), "local rebind failed") {
		t.Fatalf("runtimeRebindFunc error = %v, want local rebind failed", rebindErr)
	}
	if _, err := engine.SubmitUserMessage(context.Background(), "apply patch"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	gotDetail := detail.Get()
	if !strings.Contains(gotDetail, "/old-worktree/probe.txt") {
		t.Fatalf("expected patch detail to keep old workdir, got %q", gotDetail)
	}
	if strings.Contains(gotDetail, "/new-worktree/probe.txt") {
		t.Fatalf("did not expect failed rebind workdir in patch detail, got %q", gotDetail)
	}
}

func TestResolveStoreFallsBackThroughMetadataAuthority(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	resolved, err := fixture.service.resolveStore(context.Background(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolveStore: %v", err)
	}
	if resolved.Meta().SessionID != fixture.store.Meta().SessionID {
		t.Fatalf("resolved session id = %q, want %q", resolved.Meta().SessionID, fixture.store.Meta().SessionID)
	}
}

func TestResolveStoreRejectsUnknownSession(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	_, err := fixture.service.resolveStore(context.Background(), "session-missing")
	if err == nil {
		t.Fatal("expected resolveStore to reject unknown session")
	}
}

func TestActivateSessionRuntimeRejectsPathLikeSessionID(t *testing.T) {
	svc := &Service{handles: make(map[string]*runtimeHandle)}
	_, err := svc.ActivateSessionRuntime(context.Background(), serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: "req-1",
		SessionID:       "../session-1",
	})
	if !errors.Is(err, serverapi.ErrSessionIDNotSingle) {
		t.Fatalf("expected path-like session id rejection, got %v", err)
	}
}

func TestReleaseSessionRuntimeRejectsPathLikeSessionID(t *testing.T) {
	svc := &Service{handles: make(map[string]*runtimeHandle)}
	_, err := svc.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: "req-1",
		SessionID:       "sessions/workspace-a/session-1",
		LeaseID:         "lease-1",
	})
	if !errors.Is(err, serverapi.ErrSessionIDNotSingle) {
		t.Fatalf("expected path-like session id rejection, got %v", err)
	}
}

type sessionRuntimeFixture struct {
	config   config.App
	metadata *metadata.Store
	store    *session.Store
	service  *Service
}

func newSessionRuntimeFixture(t *testing.T) sessionRuntimeFixture {
	t.Helper()
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	appCfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	metadataStore, err := metadata.Open(appCfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	binding, err := metadataStore.RegisterWorkspaceBinding(context.Background(), appCfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	projectSessionsDir := filepath.Join(filepath.Join(appCfg.PersistenceRoot, "projects"), binding.ProjectID, "sessions")
	store, err := session.Create(projectSessionsDir, filepath.Base(projectSessionsDir), appCfg.WorkspaceRoot, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	if err := store.SetName("session-a"); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	service := NewService(appCfg.PersistenceRoot, metadataStore, nil, nil, nil, nil, nil, registry.NewSessionStoreRegistry(), metadataStore.AuthoritativeSessionStoreOptions()...)
	return sessionRuntimeFixture{config: appCfg, metadata: metadataStore, store: store, service: service}
}
