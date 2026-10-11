package workflowrunner

import (
	"path/filepath"
	"testing"

	"core/internal/testharness/testsetup"

	"core/server/metadata"
	"core/server/session"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

func workflowPromptIdentityFixture(t *testing.T) (*metadata.Store, runtimeids.SessionIdentity) {
	t.Helper()
	root, workspace := t.TempDir(), t.TempDir()
	store := testsetup.OpenStore(t, root)
	binding, err := store.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := session.Create(filepath.Join(root, "projects", binding.ProjectID, "sessions"), binding.WorkspaceName, workspace, sessioncontract.SessionCategoryMain, store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatal(err)
	}
	return store, sess.Identity()
}

func TestCurrentSessionReconstructionPreservesBranchScopedPromptIdentity(t *testing.T) {
	implementation := workflow.TransitionBranchKey("implementation")
	review := workflow.TransitionBranchKey("review")
	identities := map[string]string{}
	for name, branchKey := range map[string]*workflow.TransitionBranchKey{
		"implementation": &implementation,
		"review":         &review,
	} {
		reference, err := workflow.NewCurrentNodeReference("task-1", "node-1", branchKey)
		if err != nil {
			t.Fatalf("NewCurrentNodeReference %s: %v", name, err)
		}
		instructions, err := BuildCurrentSessionTaskInstructions(t.Context(), testsetup.OpenStore(t, t.TempDir()), workflowstore.CurrentNodeStartContext{
			Task:        workflowstore.TaskRecord{ID: "task-1"},
			Workflow:    workflowstore.WorkflowRecord{ID: testsetup.WorkflowID(t, "workflowrunner-inspection")},
			Node:        workflowstore.NodeRecord{ID: "node-1"},
			CurrentNode: workflow.CurrentNode{Reference: reference},
		})

		if err != nil {
			t.Fatalf("reconstruct %s Current Node instructions: %v", name, err)
		}
		identities[name] = workflowruntime.CurrentNodePromptIdentity(instructions.CurrentNode)
	}
	if identities["implementation"] == identities["review"] {
		t.Fatal("reopened parallel Current Nodes at the same task/node must retain distinct prompt identities")
	}
}

func TestBuildCurrentSessionTaskInstructionsRendersEnteringSourceSessionID(t *testing.T) {
	store, source := workflowPromptIdentityFixture(t)
	sourceSessionID := source.ID
	targetSessionID := runtimeids.NewSessionID()
	selectedContextSessionID := runtimeids.NewSessionID()
	instructions, err := BuildCurrentSessionTaskInstructions(t.Context(), store, workflowstore.CurrentNodeStartContext{
		Task:             workflowstore.TaskRecord{ID: "task-1"},
		Workflow:         workflowstore.WorkflowRecord{ID: testsetup.WorkflowID(t, "workflowrunner-session-placeholder")},
		Node:             workflowstore.NodeRecord{ID: "node-review"},
		CurrentNode:      workflow.CurrentNode{Reference: mustCurrentNodeReference(t, "task-1", "node-review", nil), SessionID: &targetSessionID},
		SourceSessionID:  &selectedContextSessionID,
		PromptSessionID:  &sourceSessionID,
		TransitionPrompt: "{{.SessionId}}",
	})
	if err != nil {
		t.Fatalf("BuildCurrentSessionTaskInstructions: %v", err)
	}
	if instructions.TransitionPrompt != source.String() {
		t.Fatalf("rendered transition prompt = %q, want entering source Session ID %q", instructions.TransitionPrompt, sourceSessionID)
	}
}

func TestBuildCurrentSessionTaskInstructionsRendersMissingSourceSessionIDAsEmpty(t *testing.T) {
	instructions, err := BuildCurrentSessionTaskInstructions(t.Context(), testsetup.OpenStore(t, t.TempDir()), workflowstore.CurrentNodeStartContext{
		Task:             workflowstore.TaskRecord{ID: "task-1"},
		Workflow:         workflowstore.WorkflowRecord{ID: testsetup.WorkflowID(t, "workflowrunner-missing-session-placeholder")},
		Node:             workflowstore.NodeRecord{ID: "node-review"},
		CurrentNode:      workflow.CurrentNode{Reference: mustCurrentNodeReference(t, "task-1", "node-review", nil)},
		TransitionPrompt: "{{.SessionId}}",
	})

	if err != nil {
		t.Fatalf("BuildCurrentSessionTaskInstructions: %v", err)
	}
	if instructions.TransitionPrompt != "" {
		t.Fatalf("rendered transition prompt = %q, want empty missing source Session ID", instructions.TransitionPrompt)
	}
}

func TestRenderCurrentNodePromptRendersSourceSessionID(t *testing.T) {
	sourceSessionID := runtimeids.NewSessionID()
	input := workflowstore.CurrentNodeStartContext{
		Task:            workflowstore.TaskRecord{ID: "task-1"},
		Workflow:        workflowstore.WorkflowRecord{ID: testsetup.WorkflowID(t, "workflowrunner-preview-session-placeholder")},
		Node:            workflowstore.NodeRecord{ID: "node-review"},
		CurrentNode:     workflow.CurrentNode{Reference: mustCurrentNodeReference(t, "task-1", "node-review", nil)},
		PromptSessionID: &sourceSessionID,
	}
	prompt, err := renderCurrentNodePrompt(t.Context(), testsetup.OpenStore(t, t.TempDir()), "{{.SessionId}}", input)
	if err != nil {
		t.Fatalf("renderCurrentNodePrompt: %v", err)
	}
	if prompt != sourceSessionID.String() {
		t.Fatalf("rendered preview prompt = %q, want source Session ID %q", prompt, sourceSessionID)
	}
}

func TestBuildCurrentSessionTaskInstructionsRendersPriorSessionID(t *testing.T) {
	store, prior := workflowPromptIdentityFixture(t)
	priorSessionID := prior.ID
	instructions, err := BuildCurrentSessionTaskInstructions(t.Context(), store, workflowstore.CurrentNodeStartContext{
		Task:        workflowstore.TaskRecord{ID: "task-1"},
		Workflow:    workflowstore.WorkflowRecord{ID: testsetup.WorkflowID(t, "workflowrunner-prior-session-placeholder")},
		Node:        workflowstore.NodeRecord{ID: "node-audit"},
		CurrentNode: workflow.CurrentNode{Reference: mustCurrentNodeReference(t, "task-1", "node-audit", nil)},
		PriorSessionIDs: map[workflow.ModelKey]*runtimeids.SessionID{
			"review": &priorSessionID,
		},
		TransitionPrompt: "{{.Params.review.session_id}}",
	})

	if err != nil {
		t.Fatalf("BuildCurrentSessionTaskInstructions: %v", err)
	}
	if instructions.TransitionPrompt != prior.String() {
		t.Fatalf("rendered transition prompt = %q, want prior Session ID %q", instructions.TransitionPrompt, priorSessionID)
	}
}

func TestBuildCurrentSessionTaskInstructionsRendersMissingPriorSessionIDAsEmpty(t *testing.T) {
	instructions, err := BuildCurrentSessionTaskInstructions(t.Context(), testsetup.OpenStore(t, t.TempDir()), workflowstore.CurrentNodeStartContext{
		Task:        workflowstore.TaskRecord{ID: "task-1"},
		Workflow:    workflowstore.WorkflowRecord{ID: testsetup.WorkflowID(t, "workflowrunner-missing-prior-session-placeholder")},
		Node:        workflowstore.NodeRecord{ID: "node-audit"},
		CurrentNode: workflow.CurrentNode{Reference: mustCurrentNodeReference(t, "task-1", "node-audit", nil)},
		PriorSessionIDs: map[workflow.ModelKey]*runtimeids.SessionID{
			"review": nil,
		},
		TransitionPrompt: "{{.Params.review.session_id}}",
	})

	if err != nil {
		t.Fatalf("BuildCurrentSessionTaskInstructions: %v", err)
	}
	if instructions.TransitionPrompt != "" {
		t.Fatalf("rendered transition prompt = %q, want empty missing prior Session ID", instructions.TransitionPrompt)
	}
}

func mustCurrentNodeReference(t *testing.T, taskID workflow.TaskID, nodeID workflow.NodeID, branch *workflow.TransitionBranchKey) workflow.CurrentNodeReference {
	t.Helper()
	reference, err := workflow.NewCurrentNodeReference(taskID, nodeID, branch)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}
	return reference
}

func TestCurrentNodeRuntimeConfigWiresAuthorityScopeAndNaturalNodeIdentity(t *testing.T) {
	branch := workflow.TransitionBranchKey("implementation")
	reference, err := workflow.NewCurrentNodeReference("task-1", "node-1", &branch)
	if err != nil {
		t.Fatalf("NewCurrentNodeReference: %v", err)
	}
	scopeID := runtimeids.NewExecutionScopeID()

	awareness := &taskAwarenessSource{
		comments:     &taskCommentCountProbe{},
		dependencies: &taskDependencyCountProbe{},
	}
	runtimeConfig, err := BuildCurrentNodeRuntimeConfig(
		t.Context(), testsetup.OpenStore(t, t.TempDir()),
		workflowstore.CurrentNodeStartContext{
			Task:        workflowstore.TaskRecord{ID: "task-1"},
			Workflow:    workflowstore.WorkflowRecord{ID: testsetup.WorkflowID(t, "workflowrunner-inspection")},
			Node:        workflowstore.NodeRecord{ID: "node-1"},
			CurrentNode: workflow.CurrentNode{Reference: reference},
			TransitionOptions: []workflowstore.TransitionOption{{
				ID:         "done",
				Parameters: []workflow.Parameter{{Key: "summary"}},
			}},
		},
		scopeID,
		workflowruntime.TaskPromptDeliveryAssignment,
		workflowruntime.CompletionModeTool,
		3,
		true,
		nil,
		awareness,
	)
	if err != nil {
		t.Fatalf("BuildCurrentNodeRuntimeConfig: %v", err)
	}
	if runtimeConfig.ScopeID != scopeID {
		t.Fatalf("Current Node execution scope = %s, want authority scope %s", runtimeConfig.ScopeID, scopeID)
	}
	if runtimeConfig.TaskPromptDelivery != workflowruntime.TaskPromptDeliveryAssignment {
		t.Fatalf("Current Node task prompt delivery = %v, want Assignment", runtimeConfig.TaskPromptDelivery)
	}
	if !runtimeConfig.Instructions.CurrentNode.Equal(reference) {
		t.Fatalf("Current Node execution identity = %+v, want %+v", runtimeConfig.Instructions.CurrentNode, reference)
	}
	if len(runtimeConfig.Contract.Transitions) != 1 || runtimeConfig.Contract.Transitions[0].ID != "done" {
		t.Fatalf("Current Node completion contract = %+v", runtimeConfig.Contract)
	}
	if runtimeConfig.TaskAwarenessSource != awareness {
		t.Fatal("Current Node execution omitted the composed Task awareness source")
	}
}
