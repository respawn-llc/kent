package workflowstore

import (
	"errors"
	"sort"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/metadata"
	"core/server/workflow"
	"core/shared/runtimeids"
)

func TestAssociateTaskSessionBindsFreshSessionToCurrentNode(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	associatedAt := time.UnixMilli(1_700_000_000_000).UTC()

	association, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  started.Mutation.Created[0].Reference,
		AssociatedAt: associatedAt,
	})
	if err != nil {
		t.Fatalf("AssociateTaskSession: %v", err)
	}
	if !association.CurrentNode.Equal(started.Mutation.Created[0].Reference) ||
		association.SessionID != sessionID ||
		!association.AssociatedAt.Equal(associatedAt) {
		t.Fatalf("association = %+v, want session bound to started current node", association)
	}
	count, err := store.CountTaskSessions(ctx, task.ID)
	if err != nil {
		t.Fatalf("CountTaskSessions: %v", err)
	}
	if count != 2 {
		t.Fatalf("task session count = %d, want initial and associated Sessions", count)
	}
	latest, err := store.LatestTaskSessionForNode(ctx, started.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("LatestTaskSessionForNode: %v", err)
	}
	if latest != association {
		t.Fatalf("latest node association = %+v, want %+v", latest, association)
	}
}

func TestContinueSessionUsesRetainedSessionThinkingContract(t *testing.T) {
	for _, test := range []struct {
		name   string
		source workflow.ContextSource
	}{
		{
			name:   "immediate source",
			source: workflow.ContextSource{Kind: workflow.ContextSourceImmediateSource},
		},
		{
			name: "selected Node",
			source: workflow.ContextSource{
				Kind:    workflow.ContextSourceSelectedNode,
				NodeKey: "plan",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireRetainedSessionThinkingContract(t, test.source)
		})
	}
}

func requireRetainedSessionThinkingContract(
	t *testing.T,
	contextSource workflow.ContextSource,
) {
	t.Helper()
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	store.roleResolver = completionTargetCatalog{
		roles: map[string]workflow.TargetAgentRole{
			"coder": {
				Identity:         "coder",
				QuestionsEnabled: true,
				Thinking: workflow.ThinkingCapability{
					ReasoningCapable: true,
					Finite:           true,
					Levels:           []string{"low"},
				},
			},
			"reviewer": {
				Identity:         "reviewer",
				QuestionsEnabled: true,
				Thinking: workflow.ThinkingCapability{
					ReasoningCapable: true,
					Finite:           true,
					Levels:           []string{"high", "xhigh"},
				},
			},
		},
	}
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(_ workflow.Definition, req *WorkflowGraphSaveRequest) {
		edge := workflowGraphSaveEdgeRecord(t, req.Edges, edgeByKey(t, definition, "review").ID)
		edge.ContextMode = workflow.ContextModeContinueSession
		edge.ContextSource = contextSource
		edge.ThinkingSelection = workflow.ThinkingSelectionPreviousNode
		edge.Parameters = append(edge.Parameters, workflow.Parameter{
			Key:     "thinking",
			Purpose: workflow.ParameterPurposeTargetThinking,
		})
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	sessionID := currentNodeSessionForStoreTest(t, ctx, store, started.Reference)
	setPersistedSessionRoleForTest(t, cfg, binding, store.metadata, sessionID, "reviewer")

	startContext, err := store.ResolveCurrentNodeStartContext(ctx, started.Reference)
	if err != nil {
		t.Fatalf("ResolveCurrentNodeStartContext: %v", err)
	}
	var thinkingParameterPresent bool
	for _, option := range startContext.TransitionOptions {
		if option.ID != "review" {
			continue
		}
		for _, parameter := range option.Parameters {
			thinkingParameterPresent = thinkingParameterPresent ||
				parameter.Key == "thinking"
		}
	}
	if !thinkingParameterPresent {
		t.Fatalf("retained Session thinking parameter omitted: %+v", startContext.TransitionOptions)
	}

	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       started.Reference,
		TransitionID: "review",
		OutputValues: map[string]string{
			"summary":  "plan complete",
			"thinking": "high",
		},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode with retained Session thinking: %v", err)
	}
	target := completed.Mutation.Created[0]
	if target.SessionID == nil || *target.SessionID != sessionID {
		t.Fatalf("target Session = %v, want retained %q", target.SessionID, sessionID)
	}
	if target.AgentExecutionSelection == nil ||
		target.AgentExecutionSelection.Assignee != "reviewer" ||
		target.AgentExecutionSelection.Thinking == nil ||
		*target.AgentExecutionSelection.Thinking != workflow.ThinkingValue("high") ||
		target.AgentExecutionSelection.Origin != workflow.AssigneeOriginRetainedSession {
		t.Fatalf(
			"target execution selection = %+v, want retained reviewer/high",
			target.AgentExecutionSelection,
		)
	}
}

func TestTaskStartCutoverEstablishesLiveBindingAndProvenance(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)
	sessionID := *started.Mutation.Created[0].SessionID

	currentNodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListCurrentNodes: %v", err)
	}
	if len(currentNodes) != 1 || currentNodes[0].SessionID == nil || *currentNodes[0].SessionID != sessionID {
		t.Fatalf("current nodes = %+v, want one node bound to %q", currentNodes, sessionID)
	}
	latest, err := store.LatestTaskSessionForNode(ctx, started.Mutation.Created[0].Reference)
	if err != nil {
		t.Fatalf("LatestTaskSessionForNode: %v", err)
	}
	if latest.SessionID != sessionID {
		t.Fatalf("latest association = %+v, want %q", latest, sessionID)
	}
	if count, err := store.CountTaskSessions(ctx, task.ID); err != nil || count != 1 {
		t.Fatalf("CountTaskSessions = %d, %v, want 1", count, err)
	}
	if err := store.ValidateCurrentNodeSessionBinding(ctx, sessionID, started.Mutation.Created[0].Reference); err != nil {
		t.Fatalf("ValidateCurrentNodeSessionBinding: %v", err)
	}
}

func TestAutomaticFanoutCutoverBindsIndependentExactSessions(t *testing.T) {
	ctx, store, binding := newTestStoreContext(t)
	workflowID := createFanoutJoinWorkflow(t, ctx, store)
	saveWorkflowGraphFixture(t, ctx, store, workflowID, func(def workflow.Definition, req *WorkflowGraphSaveRequest) {
		for _, key := range []string{"split_a", "split_b"} {
			edge := workflowGraphSaveEdgeRecord(t, req.Edges, edgeByKey(t, def, key).ID)
			edge.ContextMode = workflow.ContextModeContinueSession
			edge.ContextSource = workflow.ContextSource{Kind: workflow.ContextSourceImmediateSource}
		}
	})
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	plan, err := store.PlanCurrentNodeCompletion(ctx, CurrentNodeCompletionRequest{
		Source: started.Reference, OutputValues: map[string]string{"summary": "prepared fanout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions := plannedSessionsForStoreTest(t, ctx, store, plan.StartContexts())
	if _, err := store.CommitCurrentNodeCompletion(ctx, plan, sessions); err != nil {
		t.Fatal(err)
	}
	currentNodes, err := store.ListCurrentNodes(ctx, task.ID)
	if err != nil || len(currentNodes) != 2 {
		t.Fatalf("committed fanout = %+v, %v", currentNodes, err)
	}
	seen := map[runtimeids.SessionID]bool{*started.SessionID: true}
	for _, node := range currentNodes {
		if node.SessionID == nil || seen[*node.SessionID] {
			t.Fatalf("fanout reused a source or sibling identity: %+v", node)
		}
		seen[*node.SessionID] = true
		if err := store.ValidateCurrentNodeSessionBinding(ctx, *node.SessionID, node.Reference); err != nil {
			t.Fatal(err)
		}
		association, err := store.LatestTaskSessionForNode(ctx, node.Reference)
		if err != nil || association.SessionID != *node.SessionID {
			t.Fatalf("exact branch provenance = %+v, %v", association, err)
		}
	}
}

func TestResolveCurrentSessionStartContextTreatsRetainedNonCurrentSessionAsOrdinary(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  started.Mutation.Created[0].Reference,
		AssociatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AssociateTaskSession: %v", err)
	}

	_, err = store.ResolveCurrentSessionStartContext(ctx, sessionID)
	if !errors.Is(err, ErrSessionNotCurrentWorkflowNode) {
		t.Fatalf("ResolveCurrentSessionStartContext error = %v, want retained non-current absence", err)
	}
	if err := store.ValidateCurrentNodeSessionBinding(ctx, sessionID, started.Mutation.Created[0].Reference); !errors.Is(err, ErrSessionNotCurrentWorkflowNode) {
		t.Fatalf("ValidateCurrentNodeSessionBinding error = %v, want retained non-current absence", err)
	}
}

func TestAssociateTaskSessionUpsertsRepeatedSerialAssociation(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	firstAt := time.UnixMilli(1_700_000_000_000).UTC()
	secondAt := firstAt.Add(time.Second)
	first, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  started.Mutation.Created[0].Reference,
		AssociatedAt: firstAt,
	})
	if err != nil {
		t.Fatalf("first AssociateTaskSession: %v", err)
	}
	second, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  started.Mutation.Created[0].Reference,
		AssociatedAt: secondAt,
	})
	if err != nil {
		t.Fatalf("second AssociateTaskSession: %v", err)
	}
	if !first.CurrentNode.Equal(second.CurrentNode) || !second.AssociatedAt.Equal(secondAt) {
		t.Fatalf("repeated association = %+v, want same key with updated time", second)
	}
	var rowCount int
	if err := store.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM session_workflow_node_associations
WHERE session_id = ?
  AND node_id = ?
  AND transition_branch_key IS NULL`,
		sessionID.String(),
		string(started.Mutation.Created[0].Reference.NodeID),
	).Scan(&rowCount); err != nil {
		t.Fatalf("count serial associations: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("serial association rows = %d, want 1", rowCount)
	}
}

func TestAssociateTaskSessionKeepsOneAssociationPerBranch(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	branchA := workflow.TransitionBranchKey("branch-a")
	branchB := workflow.TransitionBranchKey("branch-b")
	branchAReference, err := workflow.NewCurrentNodeReference(task.ID, started.Mutation.Created[0].Reference.NodeID, &branchA)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference branch A: %v", err)
	}
	branchBReference, err := workflow.NewCurrentNodeReference(task.ID, started.Mutation.Created[0].Reference.NodeID, &branchB)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference branch B: %v", err)
	}
	associatedAt := time.UnixMilli(1_700_000_000_000).UTC()
	for _, currentNode := range []workflow.CurrentNodeReference{branchAReference, branchAReference, branchBReference} {
		if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
			SessionID:    sessionID,
			CurrentNode:  currentNode,
			AssociatedAt: associatedAt,
		}); err != nil {
			t.Fatalf("AssociateTaskSession %v: %v", currentNode, err)
		}
	}
	for _, currentNode := range []workflow.CurrentNodeReference{branchAReference, branchBReference} {
		latest, err := store.LatestTaskSessionForNode(ctx, currentNode)
		if err != nil {
			t.Fatalf("LatestTaskSessionForNode %v: %v", currentNode, err)
		}
		if latest.SessionID != sessionID || !latest.CurrentNode.Equal(currentNode) {
			t.Fatalf("latest branch association = %+v, want session %q at %v", latest, sessionID, currentNode)
		}
	}
	var rowCount int
	if err := store.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM session_workflow_node_associations
WHERE session_id = ?
  AND node_id = ?
  AND transition_branch_key IS NOT NULL`,
		sessionID.String(),
		string(started.Mutation.Created[0].Reference.NodeID),
	).Scan(&rowCount); err != nil {
		t.Fatalf("count branch associations: %v", err)
	}
	if rowCount != 2 {
		t.Fatalf("branch association rows = %d, want 2", rowCount)
	}
}

func TestLoadSessionReuseAssociationsUsesExistingSerialAndBranchLookups(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	serialReference := started.Reference
	branchA := workflow.TransitionBranchKey("branch-a")
	branchB := workflow.TransitionBranchKey("branch-b")
	branchAReference, err := workflow.NewCurrentNodeReference(task.ID, serialReference.NodeID, &branchA)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference branch A: %v", err)
	}
	branchBReference, err := workflow.NewCurrentNodeReference(task.ID, serialReference.NodeID, &branchB)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference branch B: %v", err)
	}
	associatedAt := time.UnixMilli(1_700_000_000_000).UTC()
	for _, reference := range []workflow.CurrentNodeReference{serialReference, branchAReference, branchBReference} {
		if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
			SessionID:    sessionID,
			CurrentNode:  reference,
			AssociatedAt: associatedAt,
		}); err != nil {
			t.Fatalf("AssociateTaskSession %v: %v", reference, err)
		}
	}

	associations, err := loadSessionReuseAssociations(ctx, store.queries, []workflow.CurrentNodeReference{
		serialReference,
		branchAReference,
		branchBReference,
	})
	if err != nil {
		t.Fatalf("LoadSessionReuseAssociations: %v", err)
	}
	if len(associations) != 3 {
		t.Fatalf("association count = %d, want 3", len(associations))
	}
	for _, want := range []workflow.CurrentNodeReference{serialReference, branchAReference, branchBReference} {
		found := false
		for _, association := range associations {
			if association.CurrentNode.Equal(want) && association.SessionID == sessionID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing retained association for %v: %+v", want, associations)
		}
	}
}

func TestLoadSessionReuseAssociationsTreatsMissingReferencesAsNormalWithoutDiagnostics(t *testing.T) {
	ctx, store, binding, _ := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	removeRetainedSessionHistoryForTest(t, ctx, store, started.Reference)
	branchKey := workflow.TransitionBranchKey("missing")
	branchReference, err := workflow.NewCurrentNodeReference(
		task.ID,
		started.Reference.NodeID,
		&branchKey,
	)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}

	diagnostics := testsetup.CaptureSlog(t)

	associations, err := loadSessionReuseAssociations(
		metadata.WithQueryFailureDiagnostics(ctx),
		store.queries,
		[]workflow.CurrentNodeReference{started.Reference, branchReference},
	)
	if err != nil {
		t.Fatalf("LoadSessionReuseAssociations: %v", err)
	}
	if len(associations) != 0 {
		t.Fatalf("missing retained associations = %+v, want none", associations)
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("missing retained association diagnostics = %q, want none", diagnostics.String())
	}
}

func TestLoadSessionReuseAssociationsRetainsBranchVisitAfterJoinCycle(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID).Mutation.Created[0]
	definition, _, err := store.GetDefinition(ctx, workflowID)
	if err != nil {
		t.Fatalf("GetDefinition: %v", err)
	}
	laterNodeID := started.Reference.NodeID
	for _, node := range definition.Nodes {
		if node.Kind() == workflow.NodeKindAgent && workflow.NodeIDOf(node) != laterNodeID {
			laterNodeID = workflow.NodeIDOf(node)
			break
		}
	}
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	branchKey := workflow.TransitionBranchKey("implementation")
	branchBeforeJoin, err := workflow.NewCurrentNodeReference(task.ID, started.Reference.NodeID, &branchKey)
	if err != nil {
		t.Fatalf("branch before Join reference: %v", err)
	}
	branchAfterJoin, err := workflow.NewCurrentNodeReference(task.ID, laterNodeID, &branchKey)
	if err != nil {
		t.Fatalf("branch after Join reference: %v", err)
	}
	associatedAt := time.UnixMilli(1_700_000_000_000).UTC()
	for _, reference := range []workflow.CurrentNodeReference{branchBeforeJoin, branchAfterJoin} {
		if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
			SessionID:    sessionID,
			CurrentNode:  reference,
			AssociatedAt: associatedAt,
		}); err != nil {
			t.Fatalf("AssociateTaskSession %v: %v", reference, err)
		}
	}

	associations, err := loadSessionReuseAssociations(ctx, store.queries, []workflow.CurrentNodeReference{
		branchBeforeJoin,
		branchAfterJoin,
	})
	if err != nil {
		t.Fatalf("LoadSessionReuseAssociations after Join cycle: %v", err)
	}
	if len(associations) != 2 {
		t.Fatalf("association count = %d, want 2 retained branch visits", len(associations))
	}
	for _, want := range []workflow.CurrentNodeReference{branchBeforeJoin, branchAfterJoin} {
		found := false
		for _, association := range associations {
			if association.CurrentNode.Equal(want) && association.SessionID == sessionID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing retained branch visit %v: %+v", want, associations)
		}
	}
}

func TestAssociateTaskSessionRetainsVisitsAcrossNodes(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createMaterializedCurrentNodeWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	started := startTask(t, ctx, store, task.ID)
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	firstAt := time.UnixMilli(1_700_000_000_000).UTC()
	if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  started.Mutation.Created[0].Reference,
		AssociatedAt: firstAt,
	}); err != nil {
		t.Fatalf("AssociateTaskSession plan: %v", err)
	}
	completed, err := completeCurrentNode(t, store, ctx, CurrentNodeCompletionRequest{
		Source:       started.Mutation.Created[0].Reference,
		TransitionID: "review",
		OutputValues: map[string]string{"summary": "plan"},
	})
	if err != nil {
		t.Fatalf("CompleteCurrentNode plan: %v", err)
	}
	reviewReference := completed.Mutation.Created[0].Reference
	if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  reviewReference,
		AssociatedAt: firstAt.Add(time.Second),
	}); err != nil {
		t.Fatalf("AssociateTaskSession review: %v", err)
	}
	count, err := store.CountTaskSessions(ctx, task.ID)
	if err != nil {
		t.Fatalf("CountTaskSessions: %v", err)
	}
	if count != 3 {
		t.Fatalf("task session count = %d, want two prepared Sessions and one historical Session", count)
	}
	for _, currentNode := range []workflow.CurrentNodeReference{started.Mutation.Created[0].Reference, reviewReference} {
		latest, err := store.LatestTaskSessionForNode(ctx, currentNode)
		if err != nil {
			t.Fatalf("LatestTaskSessionForNode %v: %v", currentNode, err)
		}
		if latest.SessionID != sessionID {
			t.Fatalf("latest association = %+v, want reused session %q", latest, sessionID)
		}
	}
}

func TestAssociateTaskSessionRejectsCrossTaskOwnership(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	firstTask := createDefaultTask(t, ctx, store, binding.ProjectID)
	secondTask := createDefaultTask(t, ctx, store, binding.ProjectID)
	firstCurrentNode := startTask(t, ctx, store, firstTask.ID).Mutation.Created[0].Reference
	secondCurrentNode := startTask(t, ctx, store, secondTask.ID).Mutation.Created[0].Reference
	sessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	associatedAt := time.UnixMilli(1_700_000_000_000).UTC()
	if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  firstCurrentNode,
		AssociatedAt: associatedAt,
	}); err != nil {
		t.Fatalf("AssociateTaskSession first task: %v", err)
	}
	if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
		SessionID:    sessionID,
		CurrentNode:  secondCurrentNode,
		AssociatedAt: associatedAt,
	}); err == nil {
		t.Fatal("AssociateTaskSession cross task succeeded")
	}
	firstCount, err := store.CountTaskSessions(ctx, firstTask.ID)
	if err != nil {
		t.Fatalf("CountTaskSessions first: %v", err)
	}
	secondCount, err := store.CountTaskSessions(ctx, secondTask.ID)
	if err != nil {
		t.Fatalf("CountTaskSessions second: %v", err)
	}
	if firstCount != 2 || secondCount != 1 {
		t.Fatalf("task session counts = %d, %d; want one extra association on only the first Task", firstCount, secondCount)
	}
	if latest, err := store.LatestTaskSessionForNode(ctx, secondCurrentNode); err != nil || latest.SessionID == sessionID {
		t.Fatalf("second Task association = %+v, %v, want its original Session", latest, err)
	}
}

func TestLatestTaskSessionForNodeBreaksAssociationTimeTiesBySessionID(t *testing.T) {
	ctx, store, binding, cfg := newTestStoreWithConfigContext(t)
	workflowID := createValidWorkflow(t, ctx, store)
	linkWorkflow(t, ctx, store, binding.ProjectID, workflowID, true)
	task := createDefaultTask(t, ctx, store, binding.ProjectID)
	currentNode := startTask(t, ctx, store, task.ID).Mutation.Created[0].Reference
	firstSessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID first: %v", err)
	}
	secondSessionID, err := runtimeids.ParseSessionID(createTestSession(t, ctx, store, binding, cfg))
	if err != nil {
		t.Fatalf("ParseSessionID second: %v", err)
	}
	associatedAt := time.UnixMilli(1_700_000_000_000).UTC()
	for _, sessionID := range []runtimeids.SessionID{firstSessionID, secondSessionID} {
		if _, err := store.AssociateTaskSession(ctx, TaskSessionAssociationRequest{
			SessionID:    sessionID,
			CurrentNode:  currentNode,
			AssociatedAt: associatedAt,
		}); err != nil {
			t.Fatalf("AssociateTaskSession %q: %v", sessionID, err)
		}
	}
	ids := []string{firstSessionID.String(), secondSessionID.String()}
	sort.Strings(ids)
	latest, err := store.LatestTaskSessionForNode(ctx, currentNode)
	if err != nil {
		t.Fatalf("LatestTaskSessionForNode: %v", err)
	}
	if latest.SessionID.String() != ids[len(ids)-1] {
		t.Fatalf("latest session = %q, want greatest equal-time id %q", latest.SessionID, ids[len(ids)-1])
	}
}
