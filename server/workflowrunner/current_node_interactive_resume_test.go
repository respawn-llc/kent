package workflowrunner

import (
	"testing"
	"testing/synctest"
	"time"

	"core/internal/testharness/scriptedllm"
	"core/internal/testharness/testsetup"
	"core/internal/testharness/workflowfixture"
	"core/server/llm"
	"core/server/runtimecontrol"
	"core/server/workflow"
	"core/server/workflowexecution"
	"core/server/workflowstore"
	"core/shared/config"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
)

func waitForInteractiveResumeResponse(t *testing.T, barrier *testsetup.StartBarrier) {
	t.Helper()
	select {
	case <-barrier.Entered():
	case <-time.After(currentNodeRunnerWait):
		t.Fatal("model request did not reach its response barrier")
	}
}

func TestTaskResumeReusesSessionRunningAfterInteractiveSend(t *testing.T) {
	for _, reopened := range []bool{false, true} {
		name := "retained"
		if reopened {
			name = "reopened"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				initialResponse := testsetup.NewStartBarrier()
				interactiveResponse := testsetup.NewStartBarrier()
				initial := ScriptedFinalAnswer(`{"transition":"done","commentary":"finished"}`)
				initial.BeforeResponse = initialResponse.ArriveAndWait
				interactive := ScriptedFinalAnswer(`{"transition":"done","commentary":"finished"}`)
				interactive.BeforeResponse = interactiveResponse.ArriveAndWait
				client := NewCompactingScriptedClient(
					llm.ProviderCapabilities{ProviderID: "test", SupportsResponsesAPI: true},
					nil,
					initial,
					interactive,
					ScriptedFinalAnswer(`{"transition":"done","commentary":"finished"}`),
				)
				f := newCurrentNodeRunnerFixtureWithClient(t, client)
				t.Cleanup(initialResponse.Unblock)
				t.Cleanup(interactiveResponse.Unblock)
				f.starter.cfg.Settings.CompactionMode = config.CompactionModeNone
				writeCurrentNodeConfig(t, f.starter.cfg)
				task := f.createTask(t, createCurrentNodeAgentWorkflow(t, f.store))
				currentNode := f.startTask(t, task)
				waitForInteractiveResumeResponse(t, initialResponse)
				association, err := f.store.LatestTaskSessionForNode(t.Context(), currentNode)
				if err != nil {
					t.Fatalf("resolve Current Node Session: %v", err)
				}
				sessionID := association.SessionID
				initialExecution, active := f.authority.SessionExecution(sessionID)
				if !active {
					t.Fatal("initial Workflow execution is not active")
				}
				initialResource, hasResource := initialExecution.Scope().Resource()
				if !hasResource {
					t.Fatal("initial Workflow execution has no Session resource")
				}
				if err := f.controller.Interrupt(t.Context(), workflowexecution.InterruptSelector{TaskID: task.ID}); err != nil {
					t.Fatalf("interrupt Task: %v", err)
				}
				f.waitForTaskQuiescence(t, task.ID)
				f.waitForSessionExecutionQuiescence(t, sessionID)
				f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
					return len(nodes) == 1 &&
						nodes[0].Reference.Equal(currentNode) &&
						nodes[0].Scheduling != nil &&
						nodes[0].Scheduling.Interruption != nil
				})
				if reopened {
					dormantID, _ := f.waitForDormantRetainedSession(t, task.ID)
					if dormantID != sessionID {
						t.Fatalf("dormant Session = %s, want %s", dormantID, sessionID)
					}
				}
				resource := f.openRetainedRuntime(t, sessionID)
				if reopened == (resource == initialResource) {
					t.Fatalf("reopened=%t, initial resource=%v, opened resource=%v", reopened, initialResource, resource)
				}
				if mode := f.retainedRuntimeCompactionMode(t, sessionID); mode != string(config.CompactionModeNone) {
					t.Fatalf("compaction mode = %q, want disabled", mode)
				}

				service := runtimecontrol.NewService(f.authority, f.metadata).
					WithPromptHistoryStore(f.metadata)
				beforeQueue, err := f.store.ListCurrentNodes(t.Context(), task.ID)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := service.PrepareUserTurn(t.Context(), sessionID.String(), runtimeinput.Text("Queue this for later."))
				if err != nil {
					t.Fatal(err)
				}
				prepared.QueueItemID = runtimeids.NewQueueItemID().String()
				queued, queueErr := service.AdmitChatQueuedUserInput(t.Context(), sessionID.String(), prepared)
				if queueErr == nil || queued.Accepted {
					t.Fatalf("queue on interrupted Session: accepted=%t, error=%v; want rejection", queued.Accepted, queueErr)
				}
				afterQueue, err := f.store.ListCurrentNodes(t.Context(), task.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(afterQueue) != 1 || beforeQueue[0].Scheduling.State != afterQueue[0].Scheduling.State {
					t.Fatal("rejected queue changed the interrupted Current Node")
				}
				if _, active := f.authority.SessionExecution(sessionID); active {
					t.Fatal("rejected queue started an execution")
				}
				queueItemID := runtimeids.NewQueueItemID().String()
				submission, err := service.SubmitUserTurn(t.Context(), &runtimepb.SubmitUserTurnRequest{
					SessionId:   sessionID.String(),
					QueueItemId: queueItemID,
					Input: &runtimepb.UserTurnInput{
						Input: &runtimepb.UserTurnInput_Text{Text: "Continue working on this Task."},
					},
				})
				if err != nil {
					t.Fatalf("send to interrupted Current Node Session: %v", err)
				}
				if submission.GetQueued().GetQueueItemId() != queueItemID {
					t.Fatalf("submission = %+v, want accepted input %s", submission, queueItemID)
				}
				waitForInteractiveResumeResponse(t, interactiveResponse)
				running, active := f.authority.SessionExecution(sessionID)
				if !active {
					t.Fatal("interactive Send did not leave an active execution")
				}
				if calls := client.CompactionCalls(); len(calls) != 0 {
					t.Fatalf("compaction calls before Resume = %d, want none", len(calls))
				}

				for range 2 {
					if _, err := f.controller.ResumeTask(t.Context(), task.ID, nil); err != nil {
						t.Fatalf("Resume with the interactive turn running: %v", err)
					}
					afterResume, active := f.authority.SessionExecution(sessionID)
					if !active || afterResume.Scope().ID() != running.Scope().ID() {
						t.Fatal("Resume stopped or replaced the running interactive execution")
					}
					owner, workflowOwned := afterResume.Scope().Workflow()
					if !workflowOwned || !owner.CurrentNode.Equal(currentNode) {
						t.Fatalf("running execution owner = %+v, Workflow=%t; want Current Node %+v", owner, workflowOwned, currentNode)
					}
				}
				if requests := client.Requests(); len(requests) != 2 {
					t.Fatalf("model requests while response is held = %d, want initial and interactive turns only", len(requests))
				}
				if calls := client.CompactionCalls(); len(calls) != 0 {
					t.Fatalf("compaction calls after Resume = %d, want none", len(calls))
				}
				interactiveResponse.Unblock()
				definition, _, err := f.store.GetDefinition(t.Context(), task.WorkflowID)
				if err != nil {
					t.Fatal(err)
				}
				terminalID := currentNodeKindID(t, definition, workflow.NodeKindTerminal)
				f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
					return len(nodes) == 1 && nodes[0].Reference.NodeID == terminalID
				})
				f.waitForTaskQuiescence(t, task.ID)
			})
		})
	}
}

func TestTaskResumeContinuesSiblingAfterSelectedInteractiveSend(t *testing.T) {
	for _, scenario := range []struct {
		name               string
		unavailableSibling bool
		unavailableRunning bool
	}{
		{name: "eligible sibling"},
		{name: "unavailable sibling", unavailableSibling: true},
		{name: "unavailable running role", unavailableRunning: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			testTaskResumeContinuesSiblingAfterSelectedInteractiveSend(t, scenario.unavailableSibling, scenario.unavailableRunning)
		})
	}
}

func testTaskResumeContinuesSiblingAfterSelectedInteractiveSend(t *testing.T, unavailableSibling, unavailableRunning bool) {
	barriers := []*testsetup.StartBarrier{
		testsetup.NewStartBarrier(),
		testsetup.NewStartBarrier(),
		testsetup.NewStartBarrier(),
		testsetup.NewStartBarrier(),
	}
	steps := make([]ScriptedRuntimeStep, len(barriers))
	for i, barrier := range barriers {
		steps[i] = ScriptedFinalAnswer(`{"commentary":"working"}`)
		steps[i].BeforeResponse = barrier.ArriveAndWait
	}
	client := newObservedScriptedClient(scriptedllm.Script{
		Steps: steps, AllowConcurrent: true,
	})
	f := newCurrentNodeRunnerFixtureWithClient(t, client)
	for _, barrier := range barriers {
		t.Cleanup(barrier.Unblock)
	}
	f.starter.cfg.Settings.CompactionMode = config.CompactionModeNone
	writeCurrentNodeConfig(t, f.starter.cfg)
	workflowID, branchNodes := createCurrentNodeDirectStartFanoutWorkflow(t, f.store)
	workflowfixture.SaveStoreGraph(t, t.Context(), f.store, workflowID, func(_ workflow.Definition, request *workflowstore.WorkflowGraphSaveRequest) {
		for i := range request.Nodes {
			if request.Nodes[i].ID == branchNodes["branch_b"] {
				request.Nodes[i].SubagentRole = "reviewer"
			}
		}
	})
	task := f.createTask(t, workflowID)
	_, err := f.controller.StartTask(t.Context(), task.ID, &workflowstore.ExecutionTargetCandidate{
		Snapshot: workflowstore.ExecutionTargetSnapshot{
			Mode: workflow.ExecutionTargetModeNone, Provenance: workflowstore.ExecutionTargetProvenanceResolved,
		},
		Root: workflowstore.ExecutionRoot{SourceWorkspaceID: f.workspaceID, SourceWorkspaceRoot: f.workspace},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForInteractiveResumeResponse(t, barriers[0])
	waitForInteractiveResumeResponse(t, barriers[1])
	if err := f.controller.Interrupt(t.Context(), workflowexecution.InterruptSelector{TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
	f.waitForTaskQuiescence(t, task.ID)
	nodes := f.waitForCurrentNode(t, task.ID, func(nodes []workflow.CurrentNode) bool {
		if len(nodes) != 2 {
			return false
		}
		for _, node := range nodes {
			if node.SessionID == nil || node.Scheduling == nil || node.Scheduling.State != workflow.CurrentNodeSchedulingInterrupted {
				return false
			}
		}
		return true
	})
	selected, sibling := nodes[0], nodes[1]
	if selected.Reference.NodeID != branchNodes["branch_a"] {
		selected, sibling = sibling, selected
	}
	if unavailableSibling {
		delete(f.roles, "reviewer")
	}
	f.openRetainedRuntime(t, *selected.SessionID)
	service := runtimecontrol.NewService(f.authority, f.metadata).WithPromptHistoryStore(f.metadata)
	if _, err := service.SubmitUserTurn(t.Context(), &runtimepb.SubmitUserTurnRequest{
		SessionId: selected.SessionID.String(), QueueItemId: runtimeids.NewQueueItemID().String(),
		Input: &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: "Continue this branch."}},
	}); err != nil {
		t.Fatal(err)
	}
	waitForInteractiveResumeResponse(t, barriers[2])
	selectedExecution, live := f.authority.SessionExecution(*selected.SessionID)
	if !live {
		t.Fatal("selected branch did not start")
	}
	if _, live := f.authority.SessionExecution(*sibling.SessionID); live {
		t.Fatal("Send started the unselected sibling")
	}
	afterSend, err := f.store.ListCurrentNodes(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range afterSend {
		if node.Reference.Equal(sibling.Reference) && node.Scheduling.State != workflow.CurrentNodeSchedulingInterrupted {
			t.Fatal("Send changed sibling scheduling")
		}
	}
	if unavailableRunning {
		delete(f.roles, "coder")
	}
	if unavailableSibling {
		if _, err := f.controller.ResumeTask(t.Context(), task.ID, nil); err == nil {
			t.Fatal("Task Resume accepted the unavailable sibling role")
		}
		current, active := f.authority.SessionExecution(*selected.SessionID)
		if !active || current.Scope().ID() != selectedExecution.Scope().ID() {
			t.Fatal("sibling validation failure replaced the selected execution")
		}
		if _, active := f.authority.SessionExecution(*sibling.SessionID); active {
			t.Fatal("unavailable sibling started")
		}
		if err := f.controller.Interrupt(t.Context(), workflowexecution.InterruptSelector{TaskID: task.ID}); err != nil {
			t.Fatal(err)
		}
		return
	}
	if _, err := f.controller.ResumeTask(t.Context(), task.ID, nil); err != nil {
		t.Fatalf("Resume remaining sibling: %v", err)
	}
	waitForInteractiveResumeResponse(t, barriers[3])
	afterResume, live := f.authority.SessionExecution(*selected.SessionID)
	if !live || afterResume.Scope().ID() != selectedExecution.Scope().ID() {
		t.Fatal("Resume replaced the selected running branch")
	}
	siblingExecution, live := f.authority.SessionExecution(*sibling.SessionID)
	if !live {
		t.Fatal("Resume did not start the interrupted sibling")
	}
	for _, expected := range []struct {
		session runtimeids.SessionID
		node    workflow.CurrentNodeReference
	}{
		{*selected.SessionID, selected.Reference},
		{*sibling.SessionID, sibling.Reference},
	} {
		handle, live := f.authority.SessionExecution(expected.session)
		owner, scoped := handle.Scope().Workflow()
		if !live || !scoped || !owner.CurrentNode.Equal(expected.node) {
			t.Fatalf("branch execution owner = %+v, want %+v", owner, expected.node)
		}
	}
	if _, err := f.controller.ResumeTask(t.Context(), task.ID, nil); err != nil {
		t.Fatalf("already-satisfied Resume: %v", err)
	}
	currentSibling, live := f.authority.SessionExecution(*sibling.SessionID)
	if !live || currentSibling.Scope().ID() != siblingExecution.Scope().ID() || len(client.Requests()) != 4 {
		t.Fatal("already-satisfied Resume replaced or duplicated sibling work")
	}
	if err := f.controller.Interrupt(t.Context(), workflowexecution.InterruptSelector{TaskID: task.ID}); err != nil {
		t.Fatal(err)
	}
}
