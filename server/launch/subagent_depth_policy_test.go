package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/shared/config"
	"core/shared/protocol"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

func TestPlannerReadsOlderAncestryAsMetadataWithoutOpeningTranscript(t *testing.T) {
	planner, containerDir, persistence := newMetadataOnlyAncestryPlanner(t)
	caller := createMetadataOnlyAncestryCaller(t, containerDir, persistence)
	syntheticAncestorID := mustMetadataOnlyAncestrySessionID(t, "11111111-1111-4111-8111-111111111111")
	callerRecord, err := persistence.ResolvePersistedSession(context.Background(), caller.Meta().SessionID)
	if err != nil {
		t.Fatalf("resolve caller persisted session: %v", err)
	}
	callerRecord.Meta = cloneMetadataOnlyAncestryMeta(callerRecord.Meta)
	callerRecord.Meta.ParentAgentSessionID = &syntheticAncestorID
	resolver := &metadataOnlyAncestryResolver{
		base: persistence,
		records: map[string]session.PersistedSessionRecord{
			caller.Meta().SessionID: callerRecord,
			syntheticAncestorID.String(): {
				SessionDir: filepath.Join(t.TempDir(), "must-not-open"),
				Meta: &session.Meta{
					SessionID: syntheticAncestorID.String(),
					Category:  metadataOnlyAncestryCategory(sessioncontract.SessionCategorySubagent),
				},
			},
		},
	}
	planner.PersistedSessions = resolver

	plan, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(mustMetadataOnlyAncestrySessionID(t, caller.Meta().SessionID))),
	})
	if err != nil {
		t.Fatalf("PlanSession metadata-only ancestry: %v", err)
	}
	if plan.Descriptor.SessionID().IsZero() {
		t.Fatal("metadata-only ancestry did not create a child")
	}
	if want := []string{caller.Meta().SessionID, syntheticAncestorID.String()}; !reflect.DeepEqual(resolver.calls, want) {
		t.Fatalf("resolver calls = %v, want %v", resolver.calls, want)
	}
}

func newMetadataOnlyAncestryPlanner(t *testing.T) (Planner, string, *sessiontest.Persistence) {
	t.Helper()
	root := t.TempDir()
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	persistence := sessiontest.NewPersistence()
	return Planner{
		Config: config.App{
			WorkspaceRoot:   "/tmp/workspace-a",
			PersistenceRoot: root,
			Settings: testsetup.ProviderSettings(config.Settings{
				Model:            "gpt-6-sol",
				MaxSubagentDepth: 2,
			}),
		},
		ContainerDir:      containerDir,
		StoreOptions:      persistence.Options(),
		PersistedSessions: persistence,
		SessionProjects:   testSessionProjectResolver{}, ManagedWorktreeRoots: testSessionProjectResolver{},
	}, containerDir, persistence
}

func createMetadataOnlyAncestryCaller(t *testing.T, containerDir string, persistence *sessiontest.Persistence) *session.Store {
	t.Helper()
	store, err := session.NewLazy(containerDir, filepath.Base(containerDir), "/tmp/workspace-a", sessioncontract.SessionCategoryMain, persistence.Options()...)
	if err != nil {
		t.Fatalf("create caller session: %v", err)
	}
	if err := session.InitializeCreationContext(store, nil, session.SessionCreationSourceIndependent, session.ChildContextOptions{}); err != nil {
		t.Fatalf("initialize caller creation context: %v", err)
	}
	if err := store.EnsureDurable(); err != nil {
		t.Fatalf("persist caller session: %v", err)
	}
	return store
}

type metadataOnlyAncestryResolver struct {
	base    session.PersistedSessionResolver
	records map[string]session.PersistedSessionRecord
	calls   []string
}

func (r *metadataOnlyAncestryResolver) ResolvePersistedSession(ctx context.Context, sessionID string) (session.PersistedSessionRecord, error) {
	r.calls = append(r.calls, sessionID)
	if record, ok := r.records[sessionID]; ok {
		record.Meta = cloneMetadataOnlyAncestryMeta(record.Meta)
		return record, nil
	}
	return r.base.ResolvePersistedSession(ctx, sessionID)
}

func cloneMetadataOnlyAncestryMeta(meta *session.Meta) *session.Meta {
	if meta == nil {
		return nil
	}
	cloned := *meta
	if meta.ParentAgentSessionID != nil {
		id := *meta.ParentAgentSessionID
		cloned.ParentAgentSessionID = &id
	}
	return &cloned
}

func metadataOnlyAncestryCategory(category sessioncontract.SessionCategory) *sessioncontract.SessionCategory {
	copied := category
	return &copied
}

func mustMetadataOnlyAncestrySessionID(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("parse session id %q: %v", raw, err)
	}
	return id
}

func TestPlannerUsesSelectedCallerRoleDepthLimit(t *testing.T) {
	planner, containerDir, persistence := newSubagentDepthPlanner(t, "max_subagent_depth = 1\n[subagents.worker]\nmax_subagent_depth = 0\n")
	caller := createDepthPolicyCaller(t, containerDir, persistence, sessioncontract.SessionCategoryMain, sessiontest.AgentRole("worker"))
	callerID := mustTypedIntentSessionID(t, caller.Meta().SessionID)
	opened, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(callerID),
	})
	if err != nil {
		t.Fatalf("open selected-role caller interactively: %v", err)
	}
	if opened.ActiveSettings.MaxSubagentDepth != 0 {
		t.Fatalf("opened caller max_subagent_depth = %d, want selected role limit 0", opened.ActiveSettings.MaxSubagentDepth)
	}
	before, err := os.ReadDir(containerDir)
	if err != nil {
		t.Fatalf("read session container before launch: %v", err)
	}

	_, err = planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
	})
	assertSubagentDepthError(t, err, 1, 0)

	after, err := os.ReadDir(containerDir)
	if err != nil {
		t.Fatalf("read session container after launch: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("session directories after rejected launch = %d, want %d", len(after), len(before))
	}
}

func TestPlannerUsesGlobalDepthForBaseConfigInteractiveCaller(t *testing.T) {
	planner, containerDir, persistence := newSubagentDepthPlanner(t, "max_subagent_depth = 1\n[subagents.default]\nmax_subagent_depth = 0\n")
	caller := createTypedIntentSession(t, containerDir, sessioncontract.SessionCategoryMain, persistence)
	callerID := mustTypedIntentSessionID(t, caller.Meta().SessionID)

	opened, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeInteractive,
		Intent: serverapi.OpenExistingSessionLaunchIntent(callerID),
	})
	if err != nil {
		t.Fatalf("open base-config caller interactively: %v", err)
	}
	if opened.ActiveSettings.MaxSubagentDepth != 1 {
		t.Fatalf("opened caller max_subagent_depth = %d, want global limit 1", opened.ActiveSettings.MaxSubagentDepth)
	}

	childPlan, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
	})
	if err != nil {
		t.Fatalf("launch child from base-config caller: %v", err)
	}
	child := testStoreForPlannerPlan(t, planner, childPlan)
	if child.Meta().ParentAgentSessionID == nil || *child.Meta().ParentAgentSessionID != callerID {
		t.Fatalf("child parent agent = %v, want %q", child.Meta().ParentAgentSessionID, callerID)
	}
}

func TestPlannerUsesHeadlessDefaultRoleDepthLimit(t *testing.T) {
	planner, containerDir, persistence := newSubagentDepthPlanner(t, "max_subagent_depth = 1\n[subagents.default]\nmax_subagent_depth = 0\n")
	caller := createDepthPolicyCaller(
		t,
		containerDir,
		persistence,
		sessioncontract.SessionCategorySubagent,
		sessiontest.AgentRole(config.DefaultSubagentRole),
	)
	callerID := mustTypedIntentSessionID(t, caller.Meta().SessionID)

	_, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
	})
	assertSubagentDepthError(t, err, 1, 0)
}

func TestPlannerRoleWithoutDepthOverrideInheritsGlobal(t *testing.T) {
	planner, containerDir, persistence := newSubagentDepthPlanner(t, "max_subagent_depth = 1\n[subagents.worker]\n")
	caller := createDepthPolicyCaller(
		t,
		containerDir,
		persistence,
		sessioncontract.SessionCategorySubagent,
		sessiontest.AgentRole("worker"),
	)
	callerID := mustTypedIntentSessionID(t, caller.Meta().SessionID)
	childPlan, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
	})
	if err != nil {
		t.Fatalf("launch depth-1 child under inherited global limit: %v", err)
	}
	child := testStoreForPlannerPlan(t, planner, childPlan)
	if err := child.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("worker")}); err != nil {
		t.Fatalf("set depth-1 caller role: %v", err)
	}

	childID := mustTypedIntentSessionID(t, child.Meta().SessionID)
	_, err = planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(childID)),
	})
	assertSubagentDepthError(t, err, 2, 1)
}

func TestPlannerDoesNotUseGlobalLimitForUnavailableCallerRole(t *testing.T) {
	planner, containerDir, persistence := newSubagentDepthPlanner(t, "max_subagent_depth = 1\n")
	caller := createDepthPolicyCaller(
		t,
		containerDir,
		persistence,
		sessioncontract.SessionCategorySubagent,
		sessiontest.AgentRole("removed"),
	)
	callerID := mustTypedIntentSessionID(t, caller.Meta().SessionID)

	_, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
	})
	if err == nil {
		t.Fatal("launch from caller with unavailable selected role succeeded")
	}
	var policyErr *protocol.SubagentLaunchPolicyError
	if errors.As(err, &policyErr) {
		t.Fatalf("launch error = %+v, want selected-role resolution failure", policyErr)
	}
}

func TestPlannerAllowsSelectedRoleOverrideAtNestedDepth(t *testing.T) {
	planner, containerDir, persistence := newSubagentDepthPlanner(t, "max_subagent_depth = 1\n[subagents.high]\nmax_subagent_depth = 2\n")
	caller := createDepthPolicyCaller(
		t,
		containerDir,
		persistence,
		sessioncontract.SessionCategorySubagent,
		sessiontest.AgentRole("high"),
	)
	callerID := mustTypedIntentSessionID(t, caller.Meta().SessionID)
	childPlan, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(callerID)),
	})
	if err != nil {
		t.Fatalf("launch depth-1 child: %v", err)
	}
	child := testStoreForPlannerPlan(t, planner, childPlan)
	if err := child.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("high")}); err != nil {
		t.Fatalf("set depth-1 caller role: %v", err)
	}

	childID := mustTypedIntentSessionID(t, child.Meta().SessionID)
	grandchildPlan, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(childID)),
	})
	if err != nil {
		t.Fatalf("launch depth-2 child under role limit 2: %v", err)
	}
	grandchild := testStoreForPlannerPlan(t, planner, grandchildPlan)
	if grandchild.Meta().ParentAgentSessionID == nil || *grandchild.Meta().ParentAgentSessionID != childID {
		t.Fatalf("depth-2 child parent agent = %v, want %q", grandchild.Meta().ParentAgentSessionID, childID)
	}
}

func TestPlannerKeepsAbsoluteDepthAcrossCallerRoleChanges(t *testing.T) {
	planner, containerDir, persistence := newSubagentDepthPlanner(t, "max_subagent_depth = 2\n[subagents.high]\nmax_subagent_depth = 2\n[subagents.low]\nmax_subagent_depth = 1\n")
	root := createDepthPolicyCaller(
		t,
		containerDir,
		persistence,
		sessioncontract.SessionCategorySubagent,
		sessiontest.AgentRole("high"),
	)
	rootID := mustTypedIntentSessionID(t, root.Meta().SessionID)
	childPlan, err := planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(rootID)),
	})
	if err != nil {
		t.Fatalf("launch depth-1 child: %v", err)
	}
	child := testStoreForPlannerPlan(t, planner, childPlan)
	if err := child.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("low")}); err != nil {
		t.Fatalf("change depth-1 caller role: %v", err)
	}
	before, err := os.ReadDir(containerDir)
	if err != nil {
		t.Fatalf("read session container before launch: %v", err)
	}

	childID := mustTypedIntentSessionID(t, child.Meta().SessionID)
	_, err = planner.PlanSession(context.Background(), SessionRequest{
		Mode:   ModeHeadless,
		Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(childID)),
	})
	assertSubagentDepthError(t, err, 2, 1)

	after, err := os.ReadDir(containerDir)
	if err != nil {
		t.Fatalf("read session container after launch: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("session directories after rejected launch = %d, want %d", len(after), len(before))
	}
}

func newSubagentDepthPlanner(t *testing.T, settings string) (Planner, string, *sessiontest.Persistence) {
	t.Helper()
	cfg := loadLaunchConfig(t, t.TempDir(), settings)
	containerDir := filepath.Join(cfg.PersistenceRoot, "projects", testProjectID, "sessions")
	persistence := sessiontest.NewPersistence()
	return newPersistenceBackedTestPlanner(cfg, containerDir, persistence), containerDir, persistence
}

func createDepthPolicyCaller(t *testing.T, containerDir string, persistence *sessiontest.Persistence, category sessioncontract.SessionCategory, role *string) *session.Store {
	t.Helper()
	caller := createTypedIntentSession(t, containerDir, category, persistence)
	if role != nil {
		if err := caller.SetContinuationContext(session.ContinuationContext{AgentRole: role}); err != nil {
			t.Fatalf("set caller role: %v", err)
		}
	}
	return caller
}

func assertSubagentDepthError(t *testing.T, err error, attemptedDepth, maxDepth int) {
	t.Helper()
	var policyErr *protocol.SubagentLaunchPolicyError
	if !errors.As(err, &policyErr) {
		t.Fatalf("PlanSession error = %v, want structured depth rejection", err)
	}
	if policyErr.Kind != protocol.SubagentLaunchPolicyMaxDepthExceeded ||
		policyErr.AttemptedDepth == nil || *policyErr.AttemptedDepth != attemptedDepth ||
		policyErr.MaxDepth == nil || *policyErr.MaxDepth != maxDepth {
		t.Fatalf("depth policy error = %+v, want attempted depth %d and effective limit %d", policyErr, attemptedDepth, maxDepth)
	}
}
