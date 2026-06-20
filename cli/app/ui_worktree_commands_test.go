package app

import (
	"context"
	"core/cli/app/internal/worktreeui"
	"core/cli/tui"
	sharedclient "core/shared/client"
	"core/shared/clientui"
	"core/shared/serverapi"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type worktreeCommandTestClient struct {
	listResp        serverapi.WorktreeListResponse
	listErr         error
	listCtx         context.Context
	listRequests    []serverapi.WorktreeListRequest
	resolveCtx      context.Context
	resolveResp     serverapi.WorktreeCreateTargetResolveResponse
	resolveErr      error
	createCtx       context.Context
	createResp      serverapi.WorktreeCreateResponse
	createErr       error
	deleteCtx       context.Context
	deleteResp      serverapi.WorktreeDeleteResponse
	deleteErr       error
	switchCtx       context.Context
	switchResp      serverapi.WorktreeSwitchResponse
	switchErr       error
	resolveRequests []serverapi.WorktreeCreateTargetResolveRequest
	createRequests  []serverapi.WorktreeCreateRequest
	deleteRequests  []serverapi.WorktreeDeleteRequest
	switchRequests  []serverapi.WorktreeSwitchRequest
	leaseFailures   map[string]int
}

func (c *worktreeCommandTestClient) ListWorktrees(ctx context.Context, req serverapi.WorktreeListRequest) (serverapi.WorktreeListResponse, error) {
	c.listCtx = ctx
	c.listRequests = append(c.listRequests, req)
	return c.listResp, c.listErr
}

func (c *worktreeCommandTestClient) ResolveWorktreeCreateTarget(ctx context.Context, req serverapi.WorktreeCreateTargetResolveRequest) (serverapi.WorktreeCreateTargetResolveResponse, error) {
	c.resolveCtx = ctx
	c.resolveRequests = append(c.resolveRequests, req)
	if c.resolveErr != nil {
		return serverapi.WorktreeCreateTargetResolveResponse{}, c.resolveErr
	}
	if c.resolveResp.Resolution.Kind != "" {
		return c.resolveResp, nil
	}
	return serverapi.WorktreeCreateTargetResolveResponse{Resolution: serverapi.WorktreeCreateTargetResolution{Input: req.Target, Kind: serverapi.WorktreeCreateTargetResolutionKindNewBranch}}, nil
}

func (c *worktreeCommandTestClient) CreateWorktree(ctx context.Context, req serverapi.WorktreeCreateRequest) (serverapi.WorktreeCreateResponse, error) {
	c.createCtx = ctx
	c.createRequests = append(c.createRequests, req)
	if c.consumeLeaseFailure("create", req.ControllerLeaseID) {
		return serverapi.WorktreeCreateResponse{}, serverapi.ErrInvalidControllerLease
	}
	return c.createResp, c.createErr
}

func (c *worktreeCommandTestClient) SwitchWorktree(ctx context.Context, req serverapi.WorktreeSwitchRequest) (serverapi.WorktreeSwitchResponse, error) {
	c.switchCtx = ctx
	c.switchRequests = append(c.switchRequests, req)
	if c.consumeLeaseFailure("switch", req.ControllerLeaseID) {
		return serverapi.WorktreeSwitchResponse{}, serverapi.ErrInvalidControllerLease
	}
	return c.switchResp, c.switchErr
}

func (c *worktreeCommandTestClient) DeleteWorktree(ctx context.Context, req serverapi.WorktreeDeleteRequest) (serverapi.WorktreeDeleteResponse, error) {
	c.deleteCtx = ctx
	c.deleteRequests = append(c.deleteRequests, req)
	if c.consumeLeaseFailure("delete", req.ControllerLeaseID) {
		return serverapi.WorktreeDeleteResponse{}, serverapi.ErrInvalidControllerLease
	}
	return c.deleteResp, c.deleteErr
}

func (c *worktreeCommandTestClient) consumeLeaseFailure(kind string, leaseID string) bool {
	if c == nil || c.leaseFailures == nil {
		return false
	}
	key := kind + ":" + strings.TrimSpace(leaseID)
	remaining := c.leaseFailures[key]
	if remaining <= 0 {
		return false
	}
	c.leaseFailures[key] = remaining - 1
	return true
}

func newWorktreeTestRuntimeClient(sessionID string) *sessionRuntimeClient {
	reads := &countingSessionViewClient{view: clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: sessionID}}}
	runtimeClient := newUIRuntimeClientWithReads(sessionID, reads, sharedclient.NewLoopbackRuntimeControlClient(nil)).(*sessionRuntimeClient)
	runtimeClient.SetControllerLeaseID("lease-1")
	return runtimeClient
}

func newWorktreeTestModel(t *testing.T, client *worktreeCommandTestClient, opts ...UIOption) *uiModel {
	t.Helper()
	originalDebounce := worktreeCreateResolveDebounce
	worktreeCreateResolveDebounce = time.Millisecond
	t.Cleanup(func() { worktreeCreateResolveDebounce = originalDebounce })

	allOpts := []UIOption{WithUIWorktreeClient(client), WithUISessionID("session-1")}
	allOpts = append(allOpts, opts...)
	model := newProjectedTestUIModel(newWorktreeTestRuntimeClient("session-1"), nil, nil, allOpts...)
	if runtimeClient, ok := model.runtimeClient().(*sessionRuntimeClient); ok && strings.TrimSpace(model.sessionName) != "" {
		runtimeClient.storeMainView(clientui.RuntimeMainView{Session: clientui.RuntimeSessionView{SessionID: model.sessionID, SessionName: model.sessionName}})
	}
	return model
}

func applyWorktreeCmdMessages(t *testing.T, model *uiModel, cmd tea.Cmd) *uiModel {
	t.Helper()
	for _, msg := range collectCmdMessages(t, cmd) {
		switch msg.(type) {
		case worktreeListDoneMsg, worktreeCreateDoneMsg, worktreeSwitchDoneMsg, worktreeDeleteDoneMsg, worktreeCreateTargetResolveDebounceMsg, worktreeCreateTargetResolveDoneMsg:
			next, nextCmd := model.Update(msg)
			model = next.(*uiModel)
			model = applyWorktreeCmdMessages(t, model, nextCmd)
		}
	}
	return model
}

func testMainWorktreeListResponse() serverapi.WorktreeListResponse {
	return serverapi.WorktreeListResponse{
		Target: clientui.SessionExecutionTarget{
			WorkspaceID:      "workspace-1",
			WorkspaceRoot:    "/repo",
			EffectiveWorkdir: "/repo",
		},
		Worktrees: []serverapi.WorktreeView{{
			WorktreeID:    "wt-main",
			DisplayName:   "main",
			CanonicalRoot: "/repo",
			BranchName:    "main",
			IsMain:        true,
			IsCurrent:     true,
		}},
	}
}

func testLinkedWorktreeListResponse() serverapi.WorktreeListResponse {
	return serverapi.WorktreeListResponse{
		Target: clientui.SessionExecutionTarget{
			WorkspaceID:      "workspace-1",
			WorkspaceRoot:    "/repo",
			WorktreeID:       "wt-feature",
			WorktreeRoot:     "/wt/feature-a",
			EffectiveWorkdir: "/wt/feature-a/pkg",
		},
		Worktrees: []serverapi.WorktreeView{
			{
				WorktreeID:    "wt-main",
				DisplayName:   "main",
				CanonicalRoot: "/repo",
				BranchName:    "main",
				IsMain:        true,
			},
			{
				WorktreeID:      "wt-feature",
				DisplayName:     "feature-a",
				CanonicalRoot:   "/wt/feature-a",
				BranchName:      "feature/a",
				IsCurrent:       true,
				Managed:         true,
				CreatedBranch:   true,
				OriginSessionID: "session-1",
			},
		},
	}
}

func TestWorktreeCreateDialogStartsFocusedOnTargetField(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if updated.worktrees.create.focus != uiWorktreeCreateFieldBranchTarget {
		t.Fatalf("focus = %v, want branch target", updated.worktrees.create.focus)
	}
}

func TestWorktreeCreateDialogBlankTargetSkipsDisabledBaseRef(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	if updated.worktrees.create.focus != uiWorktreeCreateFieldActions {
		t.Fatalf("focus after down = %v, want actions", updated.worktrees.create.focus)
	}
}

func TestListWorktreesForCurrentSessionUsesBoundedControlContext(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)

	if _, err := m.listWorktreesForCurrentSession(false); err != nil {
		t.Fatalf("listWorktreesForCurrentSession: %v", err)
	}
	if client.listCtx == nil {
		t.Fatal("expected list context recorded")
	}
	if _, ok := client.listCtx.Deadline(); !ok {
		t.Fatal("expected bounded control context deadline")
	}
	if len(client.listRequests) != 1 {
		t.Fatalf("expected one list request, got %+v", client.listRequests)
	}
	if client.listRequests[0].ControllerLeaseID != "lease-1" {
		t.Fatalf("controller lease id = %q, want lease-1", client.listRequests[0].ControllerLeaseID)
	}
}

func TestResolveWorktreeTokenFromEntriesUsesMatcherPrecedence(t *testing.T) {
	entries := []serverapi.WorktreeView{
		{WorktreeID: "wt-1", DisplayName: "feature", CanonicalRoot: "/wt/feature-display"},
		{WorktreeID: "wt-2", DisplayName: "other", BranchName: "feature", CanonicalRoot: "/wt/feature-branch"},
	}
	resolved, err := worktreeui.ResolveToken(entries, "feature")
	if err != nil {
		t.Fatalf("resolve worktree token: %v", err)
	}
	if resolved.WorktreeID != "wt-1" {
		t.Fatalf("resolved worktree id = %q, want wt-1", resolved.WorktreeID)
	}
}

func TestWorktreeCreateDialogNewBranchResolutionEnablesBaseRefFocus(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	updatedView, _ := updated.view.Update(tui.SetModeMsg{Mode: tui.ModeDetail})
	updated.view = updatedView.(tui.Model)
	if !updated.worktrees.open || updated.worktrees.phase != uiWorktreeOverlayPhaseCreate {
		t.Fatalf("expected create overlay open, open=%t phase=%q loading=%t", updated.worktrees.open, updated.worktrees.phase, updated.worktrees.loading)
	}
	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("feature/new"))
	next, _ = updated.Update(worktreeCreateTargetResolveDoneMsg{token: updated.worktrees.create.resolveToken, query: "feature/new", resp: serverapi.WorktreeCreateTargetResolveResponse{Resolution: serverapi.WorktreeCreateTargetResolution{Input: "feature/new", Kind: serverapi.WorktreeCreateTargetResolutionKindNewBranch}}})
	updated = next.(*uiModel)

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	if updated.worktrees.create.focus != uiWorktreeCreateFieldBaseRef {
		t.Fatalf("focus after down = %v, want base ref", updated.worktrees.create.focus)
	}
}

func TestWorktreeCreateDialogUsesRealAltScreenCursorWhenAvailable(t *testing.T) {
	state := newUITerminalCursorState()
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client, WithUITerminalCursorState(state))
	m.termWidth = 40
	m.termHeight = 14
	m.windowSizeKnown = true
	m.altScreenActive = true
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	updatedView, _ := updated.view.Update(tui.SetModeMsg{Mode: tui.ModeDetail})
	updated.view = updatedView.(tui.Model)
	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("feature/new"))
	_ = updated.View()
	if !updated.worktrees.inputCursor.Visible {
		t.Fatalf("expected worktree input cursor to be visible after render, cursor=%+v mode=%q", updated.worktrees.inputCursor, updated.view.Mode())
	}

	placement, ok := state.Snapshot()
	if !ok {
		t.Fatal("expected real cursor placement for worktree create input")
	}
	if !placement.AltScreen {
		t.Fatalf("expected alt-screen cursor placement, got %+v", placement)
	}
	if placement.CursorCol >= updated.termWidth {
		t.Fatalf("cursor col %d outside width %d", placement.CursorCol, updated.termWidth)
	}
	if strings.Contains(updated.View(), "\x1b[7") {
		t.Fatal("did not expect soft cursor when real terminal cursor is available")
	}
}

func TestWorktreeCreateDialogClearsResolveErrorWhenTargetBecomesEmpty(t *testing.T) {
	client := &worktreeCommandTestClient{
		listResp:   testMainWorktreeListResponse(),
		resolveErr: errors.New("resolve failed: bad repo state"),
	}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	if updated.worktrees.create.errorText == "" {
		t.Fatal("expected resolve error before clearing input")
	}

	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace(""))
	next, _ = updated.Update(worktreeCreateTargetResolveDebounceMsg{token: updated.worktrees.create.resolveToken})
	updated = next.(*uiModel)

	if updated.worktrees.create.errorText != "" {
		t.Fatalf("expected resolve error cleared for empty input, got %q", updated.worktrees.create.errorText)
	}
	if updated.worktrees.create.resolution.Kind != "" {
		t.Fatalf("expected empty resolution after clearing input, got %+v", updated.worktrees.create.resolution)
	}
}

func TestWorktreeCreateDialogSubmitResolvesTargetWithBoundedContext(t *testing.T) {
	client := &worktreeCommandTestClient{
		listResp:    testMainWorktreeListResponse(),
		resolveResp: serverapi.WorktreeCreateTargetResolveResponse{Resolution: serverapi.WorktreeCreateTargetResolution{Input: "main", Kind: serverapi.WorktreeCreateTargetResolutionKindExistingBranch}},
		createResp:  serverapi.WorktreeCreateResponse{Target: clientui.SessionExecutionTarget{EffectiveWorkdir: "/repo"}, Worktree: serverapi.WorktreeView{WorktreeID: "wt-main", DisplayName: "main", CanonicalRoot: "/repo"}},
	}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("main"))
	updated.worktrees.create.focus = uiWorktreeCreateFieldActions
	updated.worktrees.create.action = uiWorktreeCreateActionCreate
	updated.worktrees.create.syncFocus()

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if client.resolveCtx == nil {
		t.Fatal("expected resolve context recorded")
	}
	if _, ok := client.resolveCtx.Deadline(); !ok {
		t.Fatal("expected bounded resolve context deadline")
	}
	if len(client.createRequests) != 1 {
		t.Fatalf("expected one create request after async resolution, got %+v", client.createRequests)
	}
}

func TestWorktreeCreateTargetResolutionSubmitSchedulesSpinnerTick(t *testing.T) {
	withDeterministicSpinnerClock(t)

	client := &worktreeCommandTestClient{
		listResp:   testMainWorktreeListResponse(),
		createResp: serverapi.WorktreeCreateResponse{Target: clientui.SessionExecutionTarget{EffectiveWorkdir: "/repo"}, Worktree: serverapi.WorktreeView{WorktreeID: "wt-main", DisplayName: "main", CanonicalRoot: "/repo"}},
	}
	updated, cmd := submitWorktreeCreateAfterTargetResolution(t, client)
	if !updated.worktrees.create.submitting {
		t.Fatal("expected create submitting state")
	}
	if updated.spinnerTickToken == 0 {
		t.Fatal("expected create submit to start spinner ticking")
	}
	if updated.spinnerTickDue.IsZero() {
		t.Fatal("expected create submit to record spinner tick deadline")
	}

	msgs := collectCmdMessages(t, cmd)
	sawCreateDone := false
	sawSpinnerTick := false
	for _, msg := range msgs {
		switch typed := msg.(type) {
		case worktreeCreateDoneMsg:
			sawCreateDone = true
		case spinnerTickMsg:
			if typed.token != updated.spinnerTickToken {
				t.Fatalf("spinner tick token = %d, want %d", typed.token, updated.spinnerTickToken)
			}
			sawSpinnerTick = true
		}
	}
	if !sawCreateDone {
		t.Fatalf("expected returned command to emit create completion, got %+v", msgs)
	}
	if !sawSpinnerTick {
		t.Fatalf("expected returned command to emit spinner tick, got %+v", msgs)
	}
	if len(client.createRequests) != 1 {
		t.Fatalf("expected one create request, got %+v", client.createRequests)
	}
}

func TestWorktreeCreateCompletionStopsSpinnerAfterOverlayError(t *testing.T) {
	withDeterministicSpinnerClock(t)

	client := &worktreeCommandTestClient{
		listResp:   testMainWorktreeListResponse(),
		createErr:  errors.New("create failed"),
		createResp: serverapi.WorktreeCreateResponse{Target: clientui.SessionExecutionTarget{EffectiveWorkdir: "/repo"}, Worktree: serverapi.WorktreeView{WorktreeID: "wt-main", DisplayName: "main", CanonicalRoot: "/repo"}},
	}
	updated, cmd := submitWorktreeCreateAfterTargetResolution(t, client)
	if updated.spinnerTickToken == 0 {
		t.Fatal("expected create submit to start spinner ticking")
	}
	var done worktreeCreateDoneMsg
	foundDone := false
	for _, msg := range collectCmdMessages(t, cmd) {
		if typed, ok := msg.(worktreeCreateDoneMsg); ok {
			done = typed
			foundDone = true
		}
	}
	if !foundDone {
		t.Fatal("expected create completion from command")
	}

	next, _ := updated.Update(done)
	completed := next.(*uiModel)
	if completed.worktrees.create.submitting {
		t.Fatal("expected create completion to clear submitting state")
	}
	if completed.spinnerTickToken != 0 {
		t.Fatalf("expected create error completion to stop spinner ticking, got token %d", completed.spinnerTickToken)
	}
}

func submitWorktreeCreateAfterTargetResolution(t *testing.T, client *worktreeCommandTestClient) (*uiModel, tea.Cmd) {
	t.Helper()
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("main"))
	updated.worktrees.create.focus = uiWorktreeCreateFieldActions
	updated.worktrees.create.action = uiWorktreeCreateActionCreate
	updated.worktrees.create.syncFocus()

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected create target resolution command")
	}

	next, cmd = updated.Update(worktreeCreateTargetResolveDoneMsg{
		token: updated.worktrees.create.resolveToken,
		query: "main",
		resp: serverapi.WorktreeCreateTargetResolveResponse{Resolution: serverapi.WorktreeCreateTargetResolution{
			Input: "main",
			Kind:  serverapi.WorktreeCreateTargetResolutionKindExistingBranch,
		}},
	})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected create command after target resolution")
	}
	return updated, cmd
}

func TestWorktreeCreateDialogLeavesBranchNameBlankWithoutSessionNameSuggestion(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if got := updated.worktrees.create.branchTarget.Text(); got != "" {
		t.Fatalf("branch target default = %q, want empty", got)
	}
}

func TestWorktreeCreateDialogBlankBranchNameValidationDoesNotSendRequest(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	updated.worktrees.create.baseRef.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("HEAD"))
	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace(""))
	updated.worktrees.create.focus = uiWorktreeCreateFieldActions
	updated.worktrees.create.action = uiWorktreeCreateActionCreate
	updated.worktrees.create.syncFocus()

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if len(client.createRequests) != 0 {
		t.Fatalf("expected no create requests, got %+v", client.createRequests)
	}
	if updated.worktrees.create.errorText != "Branch or ref is required" {
		t.Fatalf("error text = %q, want branch validation", updated.worktrees.create.errorText)
	}
	if strings.TrimSpace(updated.transientStatus) != "" {
		t.Fatalf("expected no status-line error mirror, got %q", updated.transientStatus)
	}
	if !updated.worktrees.open || updated.worktrees.phase != uiWorktreeOverlayPhaseCreate {
		t.Fatalf("expected create dialog to remain open, open=%t phase=%q", updated.worktrees.open, updated.worktrees.phase)
	}
}

func TestWorktreeCreateDialogBlankBaseRefValidationDoesNotSendRequest(t *testing.T) {
	client := &worktreeCommandTestClient{listResp: testMainWorktreeListResponse()}
	m := newWorktreeTestModel(t, client)
	m.input = "/worktree create"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)
	updated.worktrees.create.branchTarget.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("feature/branch"))
	updated.worktrees.create.baseRef.Replace(strings.NewReplacer("\r", "", "\n", "").Replace(""))
	updated.worktrees.create.focus = uiWorktreeCreateFieldActions
	updated.worktrees.create.action = uiWorktreeCreateActionCreate
	updated.worktrees.create.syncFocus()

	next, cmd = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if len(client.createRequests) != 0 {
		t.Fatalf("expected no create requests, got %+v", client.createRequests)
	}
	if updated.worktrees.create.errorText != "Base ref is required" {
		t.Fatalf("error text = %q, want base ref validation", updated.worktrees.create.errorText)
	}
	if strings.TrimSpace(updated.transientStatus) != "" {
		t.Fatalf("expected no status-line error mirror, got %q", updated.transientStatus)
	}
	if !updated.worktrees.open || updated.worktrees.phase != uiWorktreeOverlayPhaseCreate {
		t.Fatalf("expected create dialog to remain open, open=%t phase=%q", updated.worktrees.open, updated.worktrees.phase)
	}
}

func TestWorktreeSwitchCommandPrefersDisplayNameMatchBeforeBranchMatch(t *testing.T) {
	resp := testMainWorktreeListResponse()
	resp.Worktrees = append(resp.Worktrees,
		serverapi.WorktreeView{WorktreeID: "wt-display", DisplayName: "shared", CanonicalRoot: "/wt/shared-display", BranchName: "feature/display"},
		serverapi.WorktreeView{WorktreeID: "wt-branch", DisplayName: "other", CanonicalRoot: "/wt/shared-branch", BranchName: "shared"},
	)
	client := &worktreeCommandTestClient{listResp: resp}
	m := newWorktreeTestModel(t, client)
	m.input = "/wt switch shared"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if len(client.switchRequests) != 1 {
		t.Fatalf("expected one switch request, got %+v", client.switchRequests)
	}
	if client.switchRequests[0].WorktreeID != "wt-display" {
		t.Fatalf("switch target = %q, want wt-display", client.switchRequests[0].WorktreeID)
	}
}

func TestWorktreeSwitchCommandsCoalesceWhileInFlight(t *testing.T) {
	client := &worktreeCommandTestClient{
		listResp:   testLinkedWorktreeListResponse(),
		switchResp: serverapi.WorktreeSwitchResponse{Worktree: serverapi.WorktreeView{WorktreeID: "wt-main", DisplayName: "main"}},
	}
	m := newWorktreeTestModel(t, client)

	next, firstCmd := m.inputController().handleWorktreeSwitchCommand("feature-a")
	updated := next.(*uiModel)
	if firstCmd == nil {
		t.Fatal("expected first switch command")
	}
	next, secondCmd := updated.inputController().handleWorktreeSwitchCommand("main")
	updated = next.(*uiModel)
	if secondCmd != nil {
		t.Fatal("did not expect second switch command while first is in flight")
	}
	if len(client.switchRequests) != 0 {
		t.Fatalf("switch RPC started before command execution: %+v", client.switchRequests)
	}

	updated = applyWorktreeCmdMessages(t, updated, firstCmd)
	if len(client.switchRequests) != 2 {
		t.Fatalf("expected serialized first and follow-up switch RPCs, got %+v", client.switchRequests)
	}
	if client.switchRequests[0].WorktreeID != "wt-feature" || client.switchRequests[1].WorktreeID != "wt-main" {
		t.Fatalf("unexpected switch request order: %+v", client.switchRequests)
	}
	if updated.worktrees.switchPending {
		t.Fatal("expected switch pending cleared after serialized follow-up completion")
	}
}

func TestWorktreeSwitchCompletionAppliesBeforeQueuedSwitchRuns(t *testing.T) {
	client := &worktreeCommandTestClient{
		listResp:   testLinkedWorktreeListResponse(),
		switchResp: serverapi.WorktreeSwitchResponse{Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a"}},
	}
	m := newWorktreeTestModel(t, client)

	next, firstCmd := m.inputController().handleWorktreeSwitchCommand("feature-a")
	updated := next.(*uiModel)
	if firstCmd == nil {
		t.Fatal("expected first switch command")
	}
	next, secondCmd := updated.inputController().handleWorktreeSwitchCommand("main")
	updated = next.(*uiModel)
	if secondCmd != nil {
		t.Fatal("did not expect second switch command while first is in flight")
	}

	var firstDone worktreeSwitchDoneMsg
	foundFirst := false
	for _, msg := range collectCmdMessages(t, firstCmd) {
		if typed, ok := msg.(worktreeSwitchDoneMsg); ok {
			firstDone = typed
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Fatal("expected first worktree switch completion")
	}
	client.switchErr = errors.New("queued switch failed")
	next, followCmd := updated.Update(firstDone)
	updated = next.(*uiModel)
	if updated.transientStatus != "Switched to feature-a" {
		t.Fatalf("expected first switch success status before queued follow-up, got %q", updated.transientStatus)
	}
	msgs := collectCmdMessages(t, followCmd)
	sawRefresh := false
	sawQueuedSwitch := false
	for _, msg := range msgs {
		switch typed := msg.(type) {
		case runtimeMainViewRefreshedMsg:
			sawRefresh = true
		case worktreeSwitchDoneMsg:
			if typed.err != nil {
				sawQueuedSwitch = true
			}
		}
	}
	if !sawRefresh {
		t.Fatalf("expected first switch to schedule main-view refresh before queued failure, got %+v", msgs)
	}
	if !sawQueuedSwitch {
		t.Fatalf("expected queued switch command to still run, got %+v", msgs)
	}
}

func TestWorktreeSwitchCompletionUsesSwitchTokenNotMutationToken(t *testing.T) {
	client := &worktreeCommandTestClient{
		listResp:   testLinkedWorktreeListResponse(),
		switchResp: serverapi.WorktreeSwitchResponse{Worktree: serverapi.WorktreeView{WorktreeID: "wt-feature", DisplayName: "feature-a"}},
	}
	m := newWorktreeTestModel(t, client)

	next, switchCmd := m.inputController().handleWorktreeSwitchCommand("feature-a")
	updated := next.(*uiModel)
	if switchCmd == nil {
		t.Fatal("expected switch command")
	}
	var switchDone worktreeSwitchDoneMsg
	for _, msg := range collectCmdMessages(t, switchCmd) {
		if typed, ok := msg.(worktreeSwitchDoneMsg); ok {
			switchDone = typed
		}
	}
	updated.worktrees.mutationToken++
	next, _ = updated.Update(switchDone)
	updated = next.(*uiModel)
	if updated.worktrees.switchPending {
		t.Fatal("expected switch completion to clear pending state despite unrelated mutation token change")
	}
	if updated.transientStatus != "Switched to feature-a" {
		t.Fatalf("expected switch completion to apply, got status %q", updated.transientStatus)
	}
}

func TestWorktreeDeleteTargetResolutionPrefersDisplayNameMatchBeforeBranchMatch(t *testing.T) {
	resp := testMainWorktreeListResponse()
	resp.Worktrees = append(resp.Worktrees,
		serverapi.WorktreeView{WorktreeID: "wt-display", DisplayName: "shared", CanonicalRoot: "/wt/shared-display", BranchName: "feature/display"},
		serverapi.WorktreeView{WorktreeID: "wt-branch", DisplayName: "other", CanonicalRoot: "/wt/shared-branch", BranchName: "shared"},
	)
	client := &worktreeCommandTestClient{listResp: resp}
	m := newWorktreeTestModel(t, client)
	m.input = "/wt delete shared"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	if updated.worktrees.phase != uiWorktreeOverlayPhaseDeleteConfirm {
		t.Fatalf("phase = %q, want delete_confirm", updated.worktrees.phase)
	}
	if updated.worktrees.deleteConfirm.target.WorktreeID != "wt-display" {
		t.Fatalf("delete target = %q, want wt-display", updated.worktrees.deleteConfirm.target.WorktreeID)
	}
	if strings.TrimSpace(updated.worktrees.errorText) != "" {
		t.Fatalf("expected no list error, got %q", updated.worktrees.errorText)
	}
}
