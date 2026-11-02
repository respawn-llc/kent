package app

import (
	"testing"

	"core/cli/app/internal/worktreeui"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewSessionWorktreeDeletionExcludesDistinctGitMainWorktree(t *testing.T) {
	entry := &worktreepb.ListEntry{
		Topology: &worktreepb.TopologyEntry{Topology: &worktreepb.TopologyEntry_External{External: &worktreepb.ExternalFacts{
			Git: &worktreepb.GitFacts{CanonicalRoot: "/repo/main", IsMainWorktree: true, PathAvailable: true},
		}}},
		Projection: &worktreepb.ListProjection{Selector: "/repo/main"},
	}
	client := &worktreeCommandTestClient{
		workspaceResp: &worktreepb.WorkspaceListSuccess{WorkspaceId: "workspace", Worktrees: []*worktreepb.ListEntry{entry}},
	}
	model := newWorktreeTestModel(t, client)
	model.engine = nil
	model.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{ProjectId: "project", WorkspaceId: "workspace"}}
	_, cmd := model.inputController().handleWorktreeCommand("")
	model = applyWorktreeCmdMessages(t, model, cmd)
	model.worktrees.selection = 1
	if worktreeDeleteActionsAvailableForSelection(model) {
		t.Fatal("Git main worktree offered deletion")
	}
	_, cmd = model.inputController().startSelectedWorktreeDelete(false)
	applyWorktreeCmdMessages(t, model, cmd)
	if len(client.previewRequests) != 0 || len(client.deleteRequests) != 0 {
		t.Fatal("Git main worktree keyboard action requested deletion")
	}
}

func TestNewSessionWorktreeBrowseSelectLeaveAndDeleteStayUncreated(t *testing.T) {
	entry := testRegisteredWorktreeListEntry("wt-feature", "feature", "/wt/feature", "feature", false, false, true, true)
	entry.Projection.Switch = nil
	entry.Projection.DeletePreview = nil
	client := &worktreeCommandTestClient{
		workspaceResp: &worktreepb.WorkspaceListSuccess{WorkspaceId: "workspace", Worktrees: []*worktreepb.ListEntry{entry}},
		previewResp:   &worktreepb.DeletePreviewSuccess{Worktree: entry.Topology, DeletionSelector: "wt-feature"},
		deleteResp: &worktreepb.DeleteSuccess{Cleanup: &worktreepb.BranchCleanupOutcome{
			Kind: worktreepb.BranchCleanupOutcomeKind_WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED,
		}},
	}
	model := newWorktreeTestModel(t, client)
	model.engine = nil
	model.presentationTarget = &uiNewSessionTarget{creation: &chatpb.NewChatTarget{ProjectId: "project", WorkspaceId: "workspace"}}
	_, cmd := model.inputController().handleWorktreeCommand("")
	model = applyWorktreeCmdMessages(t, model, cmd)
	if len(model.worktrees.entries) != 1 || len(client.listRequests) != 0 || len(client.workspaceRequests) != 1 {
		t.Fatal("New Session picker did not browse workspace scope")
	}
	model.worktrees.selection = 1
	_, cmd = model.inputController().handleWorktreeOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
	applyWorktreeCmdMessages(t, model, cmd)
	if model.newChatTarget().GetWorktreeSelector() != "wt-feature" || len(client.enterRequests) != 0 {
		t.Fatal("selection did not remain a future creation input")
	}
	_, cmd = model.inputController().handleWorktreeCommand("leave")
	applyWorktreeCmdMessages(t, model, cmd)
	if model.newChatTarget().WorktreeSelector != nil || len(client.leaveRequests) != 0 {
		t.Fatal("leaving a future target mutated a Session")
	}
	_, cmd = model.inputController().handleWorktreeCommand("delete wt-feature")
	model = applyWorktreeCmdMessages(t, model, cmd)
	if len(client.previewRequests) != 1 || client.previewRequests[0].Scope.GetWorkspace() == nil {
		t.Fatal("workspace deletion did not obtain an ordinary preview")
	}
	cmd = model.worktreeDeleteCmd(model.worktrees.deleteConfirm.target, false)
	applyWorktreeCmdMessages(t, model, cmd)
	if len(client.deleteRequests) != 1 || client.deleteRequests[0].Scope.GetWorkspace() == nil || model.newSessionTarget() == nil {
		t.Fatal("workspace deletion required a Session")
	}
	if entry.Projection.Switch != nil || entry.Projection.DeletePreview != nil {
		t.Fatal("workspace projection was changed into a Session projection")
	}
	beforeAdoption := len(client.workspaceRequests)
	model.setKnownSessionTarget(detailTestSessionID)
	applyWorktreeCmdMessages(t, model, model.requestWorktreeListCmd())
	if len(client.workspaceRequests) != beforeAdoption+1 ||
		client.workspaceRequests[0].ProjectId != client.workspaceRequests[beforeAdoption].ProjectId ||
		client.workspaceRequests[0].WorkspaceId != client.workspaceRequests[beforeAdoption].WorkspaceId {
		t.Fatal("identity adoption without observation changed the workspace management scope")
	}
}

type worktreeListFixture struct {
	t     *testing.T
	model *uiModel
}

func newWorktreeListFixture(t *testing.T, client *worktreeCommandTestClient) *worktreeListFixture {
	t.Helper()
	model := newWorktreeControllerTestModel(t, client, uiWorktreeOverlayPhaseList)
	model.worktrees.entries = []worktreeui.Item{
		mustProjectWorktreeItem(t, testRegisteredWorktreeListEntry("wt-feature", "feature", "/wt/feature", "feature", false, false, true, true)),
		mustProjectWorktreeItem(t, testRegisteredWorktreeListEntry("wt-current", "current", "/wt/current", "current", false, true, true, true)),
	}
	model.setInputMode(uiInputModeWorktree)
	return &worktreeListFixture{t: t, model: model}
}

func (f *worktreeListFixture) press(key tea.KeyMsg) tea.Cmd {
	f.t.Helper()
	next, cmd := uiInputController{model: f.model}.handleWorktreeOverlayKey(key)
	f.model = next.(*uiModel)
	return cmd
}

func (f *worktreeListFixture) update(msg tea.Msg) tea.Cmd {
	f.t.Helper()
	next, cmd := f.model.Update(msg)
	f.model = next.(*uiModel)
	return cmd
}

func (f *worktreeListFixture) beginSelectorDelete(selector string) tea.Cmd {
	f.model.worktrees.intent = uiWorktreeOpenIntent{
		OpenDelete: true,
		DeleteTarget: uiWorktreeDeleteIntentTarget{
			kind:     uiWorktreeDeleteIntentTargetSelector,
			selector: selector,
		},
	}
	return f.model.applyWorktreeIntent()
}

func (f *worktreeListFixture) listDone(cmd tea.Cmd) worktreeListDoneMsg {
	f.t.Helper()
	var result worktreeListDoneMsg
	found := false
	for _, msg := range collectCmdMessages(f.t, cmd) {
		if typed, ok := msg.(worktreeListDoneMsg); ok {
			if found {
				panic("worktree list command produced multiple worktreeListDoneMsg values")
			}
			result = typed
			found = true
		}
	}
	if !found {
		panic("worktree list command did not produce worktreeListDoneMsg")
	}
	return result
}

func TestWorktreeListControllerNavigatesRows(t *testing.T) {
	fixture := newWorktreeListFixture(t, nil)
	fixture.press(tea.KeyMsg{Type: tea.KeyDown})
	if fixture.model.worktrees.selection != 1 {
		t.Fatalf("selection after down = %d, want 1", fixture.model.worktrees.selection)
	}
	fixture.press(tea.KeyMsg{Type: tea.KeyEnd})
	if fixture.model.worktrees.selection != fixture.model.worktreeRowCount()-1 {
		t.Fatalf("selection after end = %d, want last", fixture.model.worktrees.selection)
	}
	fixture.press(tea.KeyMsg{Type: tea.KeyHome})
	if fixture.model.worktrees.selection != 0 {
		t.Fatalf("selection after home = %d, want create row", fixture.model.worktrees.selection)
	}
}

func TestWorktreeListControllerEnterCreateRowOpensCreateDialogThroughUpdate(t *testing.T) {
	fixture := newWorktreeListFixture(t, nil)
	fixture.model.worktrees.selection = 0
	fixture.update(tea.KeyMsg{Type: tea.KeyEnter})
	if fixture.model.worktrees.phase != uiWorktreeOverlayPhaseCreate {
		t.Fatalf("phase after enter create row = %q, want create", fixture.model.worktrees.phase)
	}
}

func TestWorktreeListControllerEnterWorktreeSubmitsSwitch(t *testing.T) {
	client := &worktreeCommandTestClient{workspaceResp: workspaceListFixture(testMainWorktreeListResponse())}
	fixture := newWorktreeListFixture(t, client)
	fixture.model.worktrees.selection = 1
	cmd := fixture.press(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected switch command")
	}
	if !fixture.model.worktrees.switchPending {
		t.Fatal("expected switch pending state")
	}
	result := cmd()
	if _, ok := result.(worktreeSwitchDoneMsg); !ok {
		t.Fatalf("command message type = %T, want worktreeSwitchDoneMsg", result)
	}
	if len(client.enterRequests) != 1 || client.enterRequests[0].Selector != "wt-feature" {
		t.Fatalf("enter requests = %+v, want stable worktree ID", client.enterRequests)
	}
}

func TestWorktreeListControllerQueuesStableSwitchTarget(t *testing.T) {
	fixture := newWorktreeListFixture(t, nil)
	fixture.model.worktrees.selection = 1
	fixture.model.worktrees.switchPending = true

	cmd := fixture.press(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("queued switch returned a command while another switch is pending")
	}
	if fixture.model.worktrees.queuedTransition == nil ||
		fixture.model.worktrees.queuedTransition.Selector == nil ||
		*fixture.model.worktrees.queuedTransition.Selector != "wt-feature" {
		t.Fatalf("queued transition = %+v, want stable worktree ID", fixture.model.worktrees.queuedTransition)
	}
}

func TestWorktreeListControllerDeleteKeysSetIntent(t *testing.T) {
	fixture := newWorktreeListFixture(t, nil)
	fixture.model.worktrees.selection = 1
	cmd := fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("expected refresh command for delete intent")
	}
	wantIdentity := worktreeui.SelectionIdentityForItem(fixture.model.worktrees.entries[0])
	target := fixture.model.worktrees.intent.DeleteTarget
	if !fixture.model.worktrees.intent.OpenDelete ||
		target.kind != uiWorktreeDeleteIntentTargetIdentity ||
		target.identity != wantIdentity ||
		!fixture.model.worktrees.intent.PreferDeleteBranch {
		t.Fatalf("intent = %+v, want delete+branch for selected worktree identity", fixture.model.worktrees.intent)
	}
}

func TestWorktreeListControllerGitMainRowHasNoDeleteActions(t *testing.T) {
	listResponse := testLinkedWorktreeListResponse()
	gitMain := &worktreepb.ListEntry{
		Topology: &worktreepb.TopologyEntry{
			Topology: &worktreepb.TopologyEntry_External{
				External: &worktreepb.ExternalFacts{
					Git: &worktreepb.GitFacts{
						CanonicalRoot:  "/repo/git-main",
						HeadObject:     "deadbeef",
						BranchName:     appStringPointer("main"),
						IsMainWorktree: true,
						PathAvailable:  true,
					},
				},
			},
		},
		Projection: &worktreepb.ListProjection{
			Selector: "main",
			Switch: &worktreepb.SwitchOperation{
				Kind:     worktreepb.SwitchOperationKind_WORKTREE_SWITCH_OPERATION_ENTER,
				Selector: appStringPointer("main"),
			},
		},
	}
	listResponse.Worktrees = append(listResponse.Worktrees, gitMain)
	client := &worktreeCommandTestClient{}
	fixture := newWorktreeListFixture(t, client)
	fixture.model.worktrees.entries = make([]worktreeui.Item, 0, len(listResponse.Worktrees))
	for _, entry := range listResponse.Worktrees {
		fixture.model.worktrees.entries = append(fixture.model.worktrees.entries, mustProjectWorktreeItem(t, entry))
	}
	fixture.model.worktrees.selection = len(fixture.model.worktrees.entries)
	target, ok := fixture.model.selectedWorktreeRow()
	if !ok || worktreeDeleteActionsAvailableForSelection(fixture.model) {
		t.Fatalf("selected Git main row = %+v, want no projected delete action", target)
	}
	for _, key := range []rune{'d', 'x'} {
		cmd := fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd != nil ||
			fixture.model.worktrees.intent.OpenDelete ||
			fixture.model.worktrees.phase != uiWorktreeOverlayPhaseList ||
			fixture.model.transientStatus != "" ||
			len(client.deleteRequests) != 0 {
			t.Fatalf("Git main key %q changed deletion state: cmd=%v intent=%+v phase=%q status=%q requests=%d",
				key, cmd, fixture.model.worktrees.intent, fixture.model.worktrees.phase, fixture.model.transientStatus, len(client.deleteRequests))
		}
	}
}

func TestWorktreeListControllerMainWorkspaceDeleteKeysShowNotice(t *testing.T) {
	listResponse := testMainWorktreeListResponse()
	fixture := newWorktreeListFixture(t, nil)
	fixture.model.worktrees.entries = []worktreeui.Item{
		mustProjectWorktreeItem(t, listResponse.Worktrees[0]),
	}
	fixture.model.worktrees.selection = 1

	for _, key := range []rune{'d', 'x'} {
		fixture.model.transientStatus = ""
		fixture.model.transientStatusKind = uiStatusNoticeInfo
		fixture.model.worktrees.intent = uiWorktreeOpenIntent{}

		cmd := fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if fixture.model.worktrees.intent.OpenDelete ||
			fixture.model.worktrees.phase != uiWorktreeOverlayPhaseList ||
			fixture.model.transientStatus == "" ||
			fixture.model.transientStatusKind != uiStatusNoticeError {
			t.Fatalf("Main Workspace key %q did not show deletion error: cmd=%v intent=%+v phase=%q status=%q kind=%v",
				key, cmd, fixture.model.worktrees.intent, fixture.model.worktrees.phase, fixture.model.transientStatus, fixture.model.transientStatusKind)
		}
	}
}

func TestWorktreeListDeleteIntentSurvivesSelectorChangeDuringRefresh(t *testing.T) {
	entry := testRefreshedWorktreeListResponse().Worktrees[1]
	fixture := newWorktreeListFixture(t, &worktreeCommandTestClient{previewResp: &worktreepb.DeletePreviewSuccess{Worktree: entry.Topology, DeletionSelector: entry.Projection.Selector}})
	fixture.model.worktrees.selection = 1
	fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	cmd := fixture.update(worktreeListDoneMsg{
		token:     fixture.model.worktreeListGeneration,
		workspace: &worktreepb.WorkspaceListSuccess{Worktrees: testRefreshedWorktreeListResponse().Worktrees},
	})
	fixture.model = applyWorktreeCmdMessages(t, fixture.model, cmd)
	if fixture.model.worktrees.phase != uiWorktreeOverlayPhaseDeleteConfirm {
		t.Fatalf("phase = %q, want delete confirmation", fixture.model.worktrees.phase)
	}
	target := fixture.model.worktrees.deleteConfirm.target
	if worktreeui.WorktreeID(target) != "wt-feature" {
		t.Fatalf("delete target worktree id = %q, want wt-feature", worktreeui.WorktreeID(target))
	}
	if target.DeletePreview.DeletionSelector != "feature-renamed" {
		t.Fatalf("delete target selector = %q, want feature-renamed", target.DeletePreview.DeletionSelector)
	}
}

func TestWorktreeSlashDeleteResolvesSelectorsWithServer(t *testing.T) {
	for _, selector := range []string{"wt-feature", "/wt/feature-a"} {
		t.Run(selector, func(t *testing.T) {
			listResponse := testLinkedWorktreeListResponse()
			client := &worktreeCommandTestClient{
				workspaceResp: workspaceListFixture(listResponse),
				previewResp:   testDeletePreview(listResponse.Worktrees[1]),
			}
			model := newWorktreeTestModel(t, client)
			model.statusConfig.ExecutionTarget = listResponse.Target

			next, cmd := model.inputController().handleWorktreeCommand("delete " + selector)
			updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

			if len(client.previewRequests) != 1 {
				t.Fatalf("preview requests = %+v, want one authoritative resolution", client.previewRequests)
			}
			if got := client.previewRequests[0]; got.Scope.GetWorkspace() == nil || got.Selector != selector {
				t.Fatalf("preview request = %+v, want workspace-scoped selector %q", got, selector)
			}
			if updated.worktrees.phase != uiWorktreeOverlayPhaseDeleteConfirm {
				t.Fatalf("phase = %q, want delete confirmation", updated.worktrees.phase)
			}
			if target := updated.worktrees.deleteConfirm.target; worktreeui.WorktreeID(target) != "wt-feature" || !target.IsCurrent {
				t.Fatalf("delete target = %+v, want current listed worktree", target)
			}
		})
	}
}

func TestWorktreeSlashDeleteRejectsResolvedMainWorkspace(t *testing.T) {
	for _, selector := range []string{"wt-main", "/repo"} {
		t.Run(selector, func(t *testing.T) {
			listResponse := testMainWorktreeListResponse()
			client := &worktreeCommandTestClient{
				workspaceResp: workspaceListFixture(listResponse),
				previewResp:   testDeletePreview(listResponse.Worktrees[0]),
			}
			model := newWorktreeTestModel(t, client)

			next, cmd := model.inputController().handleWorktreeCommand("delete " + selector)
			updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

			if updated.worktrees.phase != uiWorktreeOverlayPhaseList {
				t.Fatalf("phase = %q, want list after main-workspace rejection", updated.worktrees.phase)
			}
			if updated.worktrees.visibleErrorText() == "" {
				t.Fatal("expected main-workspace rejection to be visible")
			}
		})
	}
}

func TestWorktreeSlashDeleteKeepsResolvedTopologyAndListedCurrentFact(t *testing.T) {
	listResponse := testLinkedWorktreeListResponse()
	resolvedEntry := testResolvedFeatureWorktreeEntry()
	client := &worktreeCommandTestClient{
		workspaceResp: workspaceListFixture(listResponse),
		previewResp:   testDeletePreview(resolvedEntry),
	}
	model := newWorktreeTestModel(t, client)
	model.statusConfig.ExecutionTarget = listResponse.Target

	next, cmd := model.inputController().handleWorktreeCommand("delete wt-feature")
	updated := applyWorktreeCmdMessages(t, next.(*uiModel), cmd)

	target := updated.worktrees.deleteConfirm.target
	requireResolvedFeatureTopology(t, target)
	if !target.IsCurrent {
		t.Fatalf("delete target = %+v, want current fact preserved from list", target)
	}
	if got := updated.worktrees.entries[1]; got.CanonicalRoot != target.CanonicalRoot {
		t.Fatalf("listed target = %+v, want reconciliation with resolved target", got)
	}
}

func TestAbandonedWorktreeDeleteResolutionCannotReopenConfirmation(t *testing.T) {
	listResponse := testLinkedWorktreeListResponse()
	client := &worktreeCommandTestClient{
		previewResp: testDeletePreview(listResponse.Worktrees[1]),
	}
	fixture := newWorktreeListFixture(t, client)
	resolveCmd := fixture.beginSelectorDelete("wt-feature")

	fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	fixture.update(tea.KeyMsg{Type: tea.KeyEsc})
	fixture.update(resolveCmd())

	if fixture.model.worktrees.phase != uiWorktreeOverlayPhaseList {
		t.Fatalf("phase = %q, want abandoned resolution to leave list open", fixture.model.worktrees.phase)
	}
	if fixture.model.worktrees.isLoading() {
		t.Fatal("abandoned selector resolution left list loading")
	}
}

func TestNewListDeleteIntentSupersedesPendingSelectorResolution(t *testing.T) {
	listResponse := testLinkedWorktreeListResponse()
	client := &worktreeCommandTestClient{
		previewResp: testDeletePreview(listResponse.Worktrees[1]),
	}
	fixture := newWorktreeListFixture(t, client)
	resolveCmd := fixture.beginSelectorDelete("wt-feature")
	fixture.model.worktrees.selection = 2

	fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	fixture.update(resolveCmd())

	if fixture.model.worktrees.phase != uiWorktreeOverlayPhaseList {
		t.Fatalf("phase = %q, want newer list delete intent to retain ownership", fixture.model.worktrees.phase)
	}
	wantIdentity := worktreeui.SelectionIdentityForItem(fixture.model.worktrees.entries[1])
	if fixture.model.worktrees.intent.DeleteTarget.identity != wantIdentity {
		t.Fatalf("delete intent = %+v, want selected current worktree", fixture.model.worktrees.intent)
	}
}

func TestListRefreshCannotOverwriteResolvedDeleteTopology(t *testing.T) {
	listResponse := testLinkedWorktreeListResponse()
	resolvedEntry := testResolvedFeatureWorktreeEntry()
	client := &worktreeCommandTestClient{
		workspaceResp: workspaceListFixture(listResponse),
		previewResp:   testDeletePreview(resolvedEntry),
	}
	fixture := newWorktreeListFixture(t, client)
	resolveCmd := fixture.beginSelectorDelete("wt-feature")
	refreshCmd := fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	fixture.update(resolveCmd())
	fixture.update(fixture.listDone(refreshCmd))

	requireResolvedFeatureTopology(t, fixture.model.worktrees.deleteConfirm.target)
}

func TestUnorderedListWithoutResolvedIdentityCannotDismissDeleteConfirmation(t *testing.T) {
	resolvedEntry := testResolvedFeatureWorktreeEntry()
	for _, tc := range []struct {
		name     string
		response *worktreepb.ListSuccess
	}{
		{
			name:     "target omitted",
			response: testMainWorktreeListResponse(),
		},
		{
			name: "target reclassified external",
			response: &worktreepb.ListSuccess{
				Worktrees: []*worktreepb.ListEntry{
					testRegisteredWorktreeListEntry("wt-main", "main", "/repo", "main", true, true, true, false),
					testExternalWorktreeListEntry("/wt/feature-renamed", "feature-renamed", false),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &worktreeCommandTestClient{
				workspaceResp: workspaceListFixture(tc.response),
				previewResp:   testDeletePreview(resolvedEntry),
			}
			fixture := newWorktreeListFixture(t, client)
			resolveCmd := fixture.beginSelectorDelete("wt-feature")
			refreshCmd := fixture.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
			fixture.update(resolveCmd())
			fixture.update(fixture.listDone(refreshCmd))

			if fixture.model.worktrees.phase != uiWorktreeOverlayPhaseDeleteConfirm {
				t.Fatalf("phase = %q, want resolved confirmation retained", fixture.model.worktrees.phase)
			}
			if target := fixture.model.worktrees.deleteConfirm.target; worktreeui.WorktreeID(target) != "wt-feature" || target.CanonicalRoot != "/wt/feature-renamed" {
				t.Fatalf("delete target = %+v, want resolved target retained", target)
			}
		})
	}
}

func TestClosedWorktreeDeleteResolutionCannotOpenReplacementOverlay(t *testing.T) {
	listResponse := testLinkedWorktreeListResponse()
	client := &worktreeCommandTestClient{
		previewResp: testDeletePreview(listResponse.Worktrees[1]),
	}
	model := newWorktreeTestModel(t, client)
	model.openWorktreeOverlay(uiWorktreeOpenIntent{})
	staleResult := model.worktreeDeleteTargetResolveCmd("wt-feature", false)()

	model.closeWorktreeOverlay()
	model.openWorktreeOverlay(uiWorktreeOpenIntent{})
	updated := updateUIModel(t, model, staleResult)

	if updated.worktrees.phase != uiWorktreeOverlayPhaseList {
		t.Fatalf("phase = %q, want replacement list overlay", updated.worktrees.phase)
	}
}

func TestClosedWorktreeListResultCannotHydrateReplacementOverlay(t *testing.T) {
	client := &worktreeCommandTestClient{
		workspaceResp: &worktreepb.WorkspaceListSuccess{
			Worktrees: []*worktreepb.ListEntry{
				testRegisteredWorktreeListEntry("wt-stale", "stale", "/wt/stale", "stale", false, false, true, true),
			},
		},
	}
	model := newWorktreeTestModel(t, client)
	model.openWorktreeOverlay(uiWorktreeOpenIntent{})
	staleResult := model.requestWorktreeListCmd()()

	model.closeWorktreeOverlay()
	model.openWorktreeOverlay(uiWorktreeOpenIntent{})
	_ = model.requestWorktreeListCmd()
	updated := updateUIModel(t, model, staleResult)

	if len(updated.worktrees.entries) != 0 {
		t.Fatalf("replacement entries = %+v, want pending replacement list", updated.worktrees.entries)
	}
	if !updated.worktrees.listPending {
		t.Fatal("stale list result cleared replacement list pending state")
	}
}

func TestWorktreeListRefreshPreservesSelectionAndDeleteTargetByKentID(t *testing.T) {
	fixture := newWorktreeListFixture(t, nil)
	fixture.model.worktrees.selection = 1
	fixture.model.openDeleteWorktreeDialog(
		fixture.model.worktrees.entries[0],
		false,
		uiWorktreeDeleteTargetAuthorityListed,
	)

	fixture.model.recordWorktreeSelection()
	err := fixture.model.applyWorktreeEntries(testRefreshedWorktreeListResponse().Worktrees)
	if err != nil {
		t.Fatalf("applyWorktreeListResponse: %v", err)
	}

	if fixture.model.worktrees.selection != 2 {
		t.Fatalf("selection = %d, want row 2", fixture.model.worktrees.selection)
	}
	target := fixture.model.worktrees.deleteConfirm.target
	if worktreeui.WorktreeID(target) != "wt-feature" {
		t.Fatalf("delete target worktree id = %q, want wt-feature", worktreeui.WorktreeID(target))
	}
	if target.Entry.Projection.Selector != "feature-renamed" {
		t.Fatalf("delete target selector = %q, want feature-renamed", target.Entry.Projection.Selector)
	}
}

func testDeletePreview(entry *worktreepb.ListEntry) *worktreepb.DeletePreviewSuccess {
	return &worktreepb.DeletePreviewSuccess{
		Worktree: entry.Topology, DeletionSelector: entry.Projection.Selector,
	}
}

func workspaceListFixture(response *worktreepb.ListSuccess) *worktreepb.WorkspaceListSuccess {
	return &worktreepb.WorkspaceListSuccess{WorkspaceId: response.GetTarget().GetWorkspaceId(), Worktrees: response.Worktrees}
}

func testResolvedFeatureWorktreeEntry() *worktreepb.ListEntry {
	return testRegisteredWorktreeListEntry("wt-feature", "renamed", "/wt/feature-renamed", "feature/renamed", false, false, true, true)
}

func testRefreshedWorktreeListResponse() *worktreepb.ListSuccess {
	return &worktreepb.ListSuccess{
		Worktrees: []*worktreepb.ListEntry{
			testRegisteredWorktreeListEntry("wt-other", "other", "/wt/other", "feature", false, false, true, true),
			testRegisteredWorktreeListEntry("wt-feature", "renamed", "/wt/feature", "feature-renamed", false, false, true, true),
		},
	}
}

func requireResolvedFeatureTopology(t *testing.T, target worktreeui.Item) {
	t.Helper()
	if target.CanonicalRoot != "/wt/feature-renamed" ||
		worktreeui.DisplayName(target) != "renamed" ||
		worktreeui.BranchName(target) != "feature/renamed" ||
		target.DeletePreview.DeletionSelector != "feature/renamed" {
		t.Fatalf("delete target = %+v, want authoritative resolved topology", target)
	}
}
