package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"core/shared/apicontract"
	protoapi "core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

type workflowDeleteStub struct {
	apicontract.WorkflowService
	preview        *pb.DeletePreviewSuccess
	result         *pb.DeleteSuccess
	previewRequest *pb.DeletePreviewRequest
	deleteRequest  *pb.DeleteRequest
}

func TestWorkflowValidateDefaultsToExecution(t *testing.T) {
	fixture := newWorktreeCommandFixture(t)
	created, err := fixture.core.WorkflowClient().CreateWorkflow(t.Context(), &pb.CreateRequest{Name: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	var explicitOut, defaultOut, stderr bytes.Buffer
	explicitCode := workflowValidateSubcommand([]string{created.Workflow.Id, "--mode", "execution", "--json"}, &explicitOut, &stderr)
	var explicit workflowValidationJSON
	if err := json.Unmarshal(explicitOut.Bytes(), &explicit); err != nil {
		t.Fatalf("explicit validation: %v; stderr=%s", err, &stderr)
	}
	stderr.Reset()
	defaultCode := workflowValidateSubcommand([]string{created.Workflow.Id, "--json"}, &defaultOut, &stderr)
	var implicit workflowValidationJSON
	if err := json.Unmarshal(defaultOut.Bytes(), &implicit); err != nil {
		t.Fatalf("default validation: %v; stderr=%s", err, &stderr)
	}
	// Independent validations may enumerate graph diagnostics in different orders.
	for _, result := range []*workflowValidationJSON{&explicit, &implicit} {
		slices.SortFunc(result.Errors, func(a, b serverapi.WorkflowValidationError) int {
			return strings.Compare(a.Code, b.Code)
		})
	}
	if defaultCode != explicitCode || !reflect.DeepEqual(implicit, explicit) {
		t.Fatalf("default validation = %+v (%d), explicit execution = %+v (%d)", implicit, defaultCode, explicit, explicitCode)
	}
}

func TestWorkflowListAcceptsLargeBeyondEndOffsets(t *testing.T) {
	newWorktreeCommandFixture(t)
	for _, offset := range []string{"2147483648", "9007199254740991"} {
		t.Run(offset, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := workflowListSubcommand([]string{"--offset", offset, "--json"}, &stdout, &stderr); code != 0 {
				t.Fatalf("large beyond-end offset failed (%d): %s", code, &stderr)
			}
			var response workflowListOutput
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Workflows == nil || len(response.Workflows) != 0 || response.NextOffset != nil {
				t.Fatalf("beyond-end output = %+v", response)
			}
		})
	}
}

func TestWorkflowListRejectsOffsetsAboveSafeIntegerRange(t *testing.T) {
	newWorktreeCommandFixture(t)
	for _, offset := range []string{"9007199254740992", "9223372036854775807"} {
		t.Run(offset, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := workflowListSubcommand([]string{"--offset", offset, "--json"}, &stdout, &stderr); code != 2 {
				t.Fatalf("out-of-range offset exit=%d, stdout=%s, stderr=%s", code, &stdout, &stderr)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("invalid input must report an error without a result: stdout=%s, stderr=%s", &stdout, &stderr)
			}
		})
	}
}

type workflowPaginationStub struct {
	apicontract.WorkflowService
	request  *pb.ListRequest
	response *pb.ListSuccess
}

func (s *workflowPaginationStub) ListWorkflows(
	_ context.Context,
	request *pb.ListRequest,
) (*pb.ListSuccess, error) {
	s.request = request
	return s.response, nil
}

func TestWorkflowListPaginationSuccess(t *testing.T) {
	offset, limit, nextOffset := int64(5), int32(2), int64(7)
	stub := &workflowPaginationStub{
		response: &pb.ListSuccess{
			Workflows:  []*pb.WorkflowRecord{},
			NextOffset: &nextOffset,
		},
	}
	response, err := listWorkflowPage(t.Context(), stub, &pb.ListRequest{
		Offset: &offset,
		Limit:  &limit,
	})
	if err != nil ||
		stub.request.Offset == nil ||
		*stub.request.Offset != offset ||
		stub.request.Limit == nil ||
		*stub.request.Limit != limit {
		t.Fatalf("request=%+v response=%+v err=%v", stub.request, response, err)
	}

	var stdout, stderr bytes.Buffer
	if code := writeWorkflowListResponse(
		&stdout,
		&stderr,
		response,
		workflowListExpectedScope{},
		true,
	); code != 0 || stderr.Len() != 0 {
		t.Fatalf("JSON exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var output struct {
		NextOffset *int64            `json:"next_offset"`
		Workflows  []json.RawMessage `json:"workflows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil ||
		output.NextOffset == nil ||
		*output.NextOffset != nextOffset ||
		len(output.Workflows) != 0 {
		t.Fatalf("output=%+v err=%v", output, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := writeWorkflowListResponse(
		&stdout,
		&stderr,
		response,
		workflowListExpectedScope{},
		false,
	); code != 0 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("human exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func (s *workflowDeleteStub) PreviewWorkflowDelete(
	_ context.Context,
	req *pb.DeletePreviewRequest,
) (*pb.DeletePreviewSuccess, error) {
	s.previewRequest = req
	return s.preview, nil
}

func (s *workflowDeleteStub) DeleteWorkflow(
	_ context.Context,
	req *pb.DeleteRequest,
) (*pb.DeleteSuccess, error) {
	s.deleteRequest = req
	return s.result, nil
}

func TestWorkflowDeleteUsesPreviewedImpactAndTypedOutcome(t *testing.T) {
	workflowID := mustWorkflowID(t, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	impact := &pb.DeleteImpact{
		WorkflowId: workflowID.String(), Version: 7, ProjectCount: 2, LinkCount: 3, TaskCount: 5,
	}
	t.Run("preview only", func(t *testing.T) {
		remote := &workflowDeleteStub{preview: &pb.DeletePreviewSuccess{Impact: impact}}
		var stdout, stderr bytes.Buffer
		if code := runWorkflowDelete(t.Context(), remote, workflowID, false, true, &stdout, &stderr); code != 1 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		if remote.deleteRequest != nil || remote.previewRequest.WorkflowId != workflowID.String() {
			t.Fatalf("preview=%+v delete=%+v", remote.previewRequest, remote.deleteRequest)
		}
		var output workflowDeleteJSON
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		expectedImpact, err := workflowDeleteImpactForCLI(impact)
		if err != nil {
			t.Fatal(err)
		}
		if output.Deleted || output.Impact != expectedImpact || len(output.Blockers) != 0 {
			t.Fatalf("output=%+v", output)
		}
	})

	t.Run("confirmed request copies preview counts", func(t *testing.T) {
		remote := &workflowDeleteStub{
			preview: &pb.DeletePreviewSuccess{Impact: impact},
			result:  &pb.DeleteSuccess{Deleted: true, Impact: impact},
		}
		var stdout, stderr bytes.Buffer
		if code := runWorkflowDelete(t.Context(), remote, workflowID, true, true, &stdout, &stderr); code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		want := &pb.DeleteRequest{
			WorkflowId: workflowID.String(), Confirmed: true, ExpectedVersion: 7,
			ExpectedProjectCount: 2, ExpectedLinkCount: 3, ExpectedTaskCount: 5,
		}
		if !proto.Equal(remote.deleteRequest, want) {
			t.Fatalf("delete request=%+v want=%+v", remote.deleteRequest, want)
		}
	})

	t.Run("typed blocker", func(t *testing.T) {
		blocker := &pb.DeleteBlocker{Code: "current_nodes", Message: "busy", Count: 1}
		remote := &workflowDeleteStub{
			preview: &pb.DeletePreviewSuccess{Impact: impact},
			result:  &pb.DeleteSuccess{Impact: impact, Blockers: []*pb.DeleteBlocker{blocker}},
		}
		var stdout, stderr bytes.Buffer
		if code := runWorkflowDelete(t.Context(), remote, workflowID, true, true, &stdout, &stderr); code != 1 || stderr.Len() != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		var output workflowDeleteJSON
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		if output.Deleted || len(output.Blockers) != 1 || output.Blockers[0].Code != blocker.Code || output.Blockers[0].Count != blocker.Count {
			t.Fatalf("output=%+v", output)
		}
	})
}

func TestWorkflowDeleteRejectsMismatchedOrInconsistentIdentity(t *testing.T) {
	workflowID := mustWorkflowID(t, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	otherID := mustWorkflowID(t, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	valid := &pb.DeleteImpact{WorkflowId: workflowID.String(), Version: 1}
	for _, test := range []struct {
		name    string
		preview *pb.DeleteImpact
		result  *pb.DeleteSuccess
	}{
		{name: "preview identity", preview: &pb.DeleteImpact{WorkflowId: otherID.String()}},
		{name: "result identity", preview: valid, result: &pb.DeleteSuccess{
			Deleted: true, Impact: &pb.DeleteImpact{WorkflowId: otherID.String(), Version: 1},
		}},
		{name: "deleted with blocker", preview: valid, result: &pb.DeleteSuccess{
			Deleted: true, Impact: valid,
			Blockers: []*pb.DeleteBlocker{{Code: "busy", Message: "busy", Count: 1}},
		}},
		{name: "not deleted without blocker", preview: valid, result: &pb.DeleteSuccess{Impact: valid}},
	} {
		t.Run(test.name, func(t *testing.T) {
			remote := &workflowDeleteStub{
				preview: &pb.DeletePreviewSuccess{Impact: test.preview},
				result:  test.result,
			}
			var stdout, stderr bytes.Buffer
			if code := runWorkflowDelete(t.Context(), remote, workflowID, true, true, &stdout, &stderr); code != 1 ||
				stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

type workflowGraphApplyStub struct {
	apicontract.WorkflowService
	definition *pb.WorkflowDefinition
	saves      []*pb.GraphSaveSuccess
	requests   []*pb.GraphSaveRequest
}

type workflowGraphMutationStub struct {
	apicontract.WorkflowService
	calls           []string
	definition      *pb.WorkflowDefinition
	preview         *pb.GraphSavePreviewSuccess
	previewRequests []*pb.GraphSavePreviewRequest
	save            *pb.GraphSaveSuccess
	saveRequests    []*pb.GraphSaveRequest
}

func (s *workflowGraphMutationStub) GetWorkflow(
	context.Context, *pb.GetRequest,

) (*pb.GetSuccess, error) {
	s.calls = append(s.calls, "get")
	return &pb.GetSuccess{Definition: s.definition}, nil
}

func (s *workflowGraphMutationStub) PreviewWorkflowGraphSave(
	_ context.Context,
	request *pb.GraphSavePreviewRequest,
) (*pb.GraphSavePreviewSuccess, error) {
	s.calls = append(s.calls, "preview")
	s.previewRequests = append(s.previewRequests, request)
	return s.preview, nil
}

func (s *workflowGraphMutationStub) SaveWorkflowGraph(
	_ context.Context,
	request *pb.GraphSaveRequest,
) (*pb.GraphSaveSuccess, error) {
	s.calls = append(s.calls, "save")
	s.saveRequests = append(s.saveRequests, request)
	return s.save, nil
}

func TestWorkflowGraphMutationPreviewsAndSavesCompleteDraft(t *testing.T) {
	workflowID := mustWorkflowID(t, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	definition := &pb.WorkflowDefinition{
		Workflow: &pb.WorkflowRecord{Id: workflowID.String(), Version: 7},
		Nodes: []*pb.WorkflowNode{
			{Id: "node-source", Key: "source", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "Source"},
			{Id: "node-target", Key: "target", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "Target"},
		},
		TransitionGroups: []*pb.WorkflowTransitionGroup{{
			Id: "group-existing", SourceNodeId: "node-source", TransitionId: "existing",
		}},
		Edges: []*pb.WorkflowEdge{{
			Id: "edge-existing", TransitionGroupId: "group-existing", Key: "existing",
			TargetNodeId: "node-target", AssigneeSelection: pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
			ThinkingSelection: pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED, ContextMode: pb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION,
		}},
	}
	tests := []struct {
		name   string
		mutate workflowGraphDraftMutation[struct{}]
		assert func(*testing.T, *pb.GraphDraft)
	}{
		{
			name: "node add",
			mutate: func(graph *pb.GraphDraft) (*pb.GraphDraft, struct{}, error) {
				graph, _, err := addWorkflowNodeDraftMutation(&pb.GraphDraftNode{
					Id: "node-added", Key: "added", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_TERMINAL, DisplayName: "Added",
				})(graph)
				return graph, struct{}{}, err
			},
			assert: func(t *testing.T, graph *pb.GraphDraft) {
				t.Helper()
				if len(graph.Nodes) != 3 || graph.Nodes[2].Id != "node-added" {
					t.Fatalf("node add graph = %+v", graph.Nodes)
				}
			},
		},
		{
			name: "node update",
			mutate: func(graph *pb.GraphDraft) (*pb.GraphDraft, struct{}, error) {
				displayName := "Renamed"
				graph, _, err := updateWorkflowNodeDraftMutation(workflowNodeUpdateDraftMutation{
					NodeKey: "source", DisplayName: &displayName,
				})(graph)
				return graph, struct{}{}, err
			},
			assert: func(t *testing.T, graph *pb.GraphDraft) {
				t.Helper()
				if graph.Nodes[0].DisplayName != "Renamed" {
					t.Fatalf("node update graph = %+v", graph.Nodes)
				}
			},
		},
		{
			name: "edge add",
			mutate: func(graph *pb.GraphDraft) (*pb.GraphDraft, struct{}, error) {
				graph, _, err := addWorkflowEdgeDraftMutation(workflowEdgeAddDraftMutation{
					SourceNodeKey: "source", TargetNodeKey: "target",
					TransitionID: "added", NewTransitionGroupID: "group-added",
					Edge: &pb.GraphDraftEdge{
						Id: "edge-added", Key: "added", AssigneeSelection: pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED,
						ThinkingSelection: pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED, ContextMode: pb.ContextMode_WORKFLOW_CONTEXT_MODE_NEW_SESSION, ContextSource: &pb.ContextSource{Kind: pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE},
					},
				})(graph)
				return graph, struct{}{}, err
			},
			assert: func(t *testing.T, graph *pb.GraphDraft) {
				t.Helper()
				if len(graph.TransitionGroups) != 2 || len(graph.Edges) != 2 ||
					graph.Edges[1].TransitionGroupId != "group-added" {
					t.Fatalf("edge add graph = %+v / %+v", graph.TransitionGroups, graph.Edges)
				}
			},
		},
		{
			name: "edge update",
			mutate: func(graph *pb.GraphDraft) (*pb.GraphDraft, struct{}, error) {
				key := "renamed"
				graph, _, err := updateWorkflowEdgeDraftMutation(workflowEdgeUpdateDraftMutation{
					EdgeID: "edge-existing", EdgeKey: &key,
				})(graph)
				return graph, struct{}{}, err
			},
			assert: func(t *testing.T, graph *pb.GraphDraft) {
				t.Helper()
				if graph.Edges[0].Key != "renamed" {
					t.Fatalf("edge update graph = %+v", graph.Edges)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			remote := &workflowGraphMutationStub{
				definition: definition,
				preview: &pb.GraphSavePreviewSuccess{
					CurrentVersion: 7, Changed: true, CanSave: true,
					Impact: emptyGraphImpact(), Blockers: []*pb.GraphSaveBlocker{},
				},
				save: &pb.GraphSaveSuccess{
					Saved: true, Changed: true, CanSave: true, CurrentVersion: 8,
					Impact: emptyGraphImpact(), Blockers: []*pb.GraphSaveBlocker{}, Definition: workflowSavedFixture(workflowID, 8),
				},
			}
			_, result, err := runWorkflowGraphMutation(t.Context(), remote, workflowID, test.mutate)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(remote.calls, []string{"get", "preview", "save"}) ||
				len(remote.previewRequests) != 1 || len(remote.saveRequests) != 1 ||
				remote.previewRequests[0].ExpectedVersion != 7 ||
				remote.saveRequests[0].ExpectedVersion != 7 ||
				!reflect.DeepEqual(remote.previewRequests[0].Graph, remote.saveRequests[0].Graph) ||
				result.Version != 8 {
				t.Fatalf(
					"calls=%v preview=%+v save=%+v result=%+v",
					remote.calls, remote.previewRequests, remote.saveRequests, result,
				)
			}
			test.assert(t, remote.saveRequests[0].Graph)
		})
	}
}

func TestWorkflowGraphMutationStopsBeforeSaveWhenPreviewIsBlocked(t *testing.T) {
	workflowID := mustWorkflowID(t, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	remote := &workflowGraphMutationStub{
		definition: &pb.WorkflowDefinition{
			Workflow: &pb.WorkflowRecord{Id: workflowID.String(), Version: 7},
		},
		preview: &pb.GraphSavePreviewSuccess{
			CurrentVersion: 7, Changed: true,
			Impact: emptyGraphImpact(),
			Blockers: []*pb.GraphSaveBlocker{{
				Code: "validation_failed", Message: "blocked", Count: 1,
				AffectedEntities: []*pb.GraphEntityReference{},
			}},
		},
	}
	_, _, err := runWorkflowGraphMutation(
		t.Context(),
		remote,
		workflowID,
		addWorkflowNodeDraftMutation(&pb.GraphDraftNode{
			Id: "node-added", Key: "added", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_TERMINAL, DisplayName: "Added",
		}),
	)
	if err == nil || !reflect.DeepEqual(remote.calls, []string{"get", "preview"}) ||
		len(remote.saveRequests) != 0 {
		t.Fatalf("calls=%v saves=%+v err=%v", remote.calls, remote.saveRequests, err)
	}
}

func (s *workflowGraphApplyStub) GetWorkflow(
	context.Context, *pb.GetRequest,

) (*pb.GetSuccess, error) {
	return &pb.GetSuccess{Definition: s.definition}, nil
}

func (s *workflowGraphApplyStub) SaveWorkflowGraph(
	_ context.Context,
	req *pb.GraphSaveRequest,
) (*pb.GraphSaveSuccess, error) {
	s.requests = append(s.requests, req)
	response := s.saves[0]
	s.saves = s.saves[1:]
	return response, nil
}

func TestWorkflowGraphApplyTypedOutcomesAndConfirmation(t *testing.T) {
	workflowID := mustWorkflowID(t, emptyWorkflowGraphDocumentID)
	contract, err := prepareWorkflowGraphDocumentContract()
	if err != nil {
		t.Fatal(err)
	}
	document, err := contract.Decode([]byte(emptyWorkflowGraphDocumentJSON))
	if err != nil {
		t.Fatal(err)
	}
	confirmationBlocker := &pb.GraphSaveBlocker{
		Code: "confirmation_required", Message: "confirm", Count: 1,
		AffectedEntities: []*pb.GraphEntityReference{},
	}
	confirmationImpact := emptyGraphImpact()
	confirmationImpact.RemovedEdgeCount = 1
	confirmationImpact.RemovedEntities = []*pb.GraphEntityReference{{
		EntityType: pb.GraphEntityType_WORKFLOW_GRAPH_ENTITY_TYPE_EDGE,
		EntityId:   workflowGraphDocumentEdgeOneID,
	}}
	for _, test := range []struct {
		name      string
		confirmed bool
		saves     []*pb.GraphSaveSuccess
		want      workflowGraphApplyOutcomeKind
	}{
		{
			name: "unchanged",
			saves: []*pb.GraphSaveSuccess{{
				Saved: true, CanSave: true, CurrentVersion: 1,
				Definition:        workflowSavedFixture(workflowID, 1),
				ValidationResults: []*pb.ModeValidationResult{},
				Impact:            emptyGraphImpact(), Blockers: []*pb.GraphSaveBlocker{},
			}},
			want: workflowGraphApplyUnchanged,
		},
		{
			name: "blocked",
			saves: []*pb.GraphSaveSuccess{{
				CurrentVersion:    1,
				ValidationResults: []*pb.ModeValidationResult{},
				Impact:            emptyGraphImpact(),
				Blockers: []*pb.GraphSaveBlocker{{
					Code: "validation_failed", Message: "invalid", Count: 1,
					AffectedEntities: []*pb.GraphEntityReference{},
				}},
			}},
			want: workflowGraphApplyBlocked,
		},
		{
			name: "confirmation required",
			saves: []*pb.GraphSaveSuccess{{
				Changed: true, CanSave: true, ConfirmationRequired: true, CurrentVersion: 1,
				ValidationResults: []*pb.ModeValidationResult{},
				Impact:            confirmationImpact, Blockers: []*pb.GraphSaveBlocker{confirmationBlocker},
			}},
			want: workflowGraphApplyConfirmationRequired,
		},
		{
			name:      "confirmed save",
			confirmed: true,
			saves: []*pb.GraphSaveSuccess{
				{
					Changed: true, CanSave: true, ConfirmationRequired: true, CurrentVersion: 1,
					ValidationResults: []*pb.ModeValidationResult{},
					Impact:            confirmationImpact, Blockers: []*pb.GraphSaveBlocker{confirmationBlocker},
				},
				{
					Saved: true, Changed: true, CanSave: true, CurrentVersion: 2,
					Definition:        workflowSavedFixture(workflowID, 2),
					ValidationResults: []*pb.ModeValidationResult{},
					Impact:            confirmationImpact, Blockers: []*pb.GraphSaveBlocker{},
				},
			},
			want: workflowGraphApplySaved,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			remote := &workflowGraphApplyStub{
				definition: &pb.WorkflowDefinition{
					Workflow: &pb.WorkflowRecord{Id: workflowID.String(), Version: 1},
				},
				saves: append([]*pb.GraphSaveSuccess(nil), test.saves...),
			}
			outcome := runWorkflowGraphApply(t.Context(), remote, document, test.confirmed)
			if outcome.Outcome != test.want {
				t.Fatalf("outcome=%+v", outcome)
			}
			assertWorkflowGraphApplyProjection(t, outcome)
			if test.confirmed {
				if len(remote.requests) != 2 || remote.requests[1].Confirmation == nil ||
					remote.requests[1].Confirmation.ExpectedRemovedEdgeCount != 1 {
					t.Fatalf("requests=%+v", remote.requests)
				}
			}
		})
	}
}

func workflowSavedFixture(id runtimeids.WorkflowID, version int64) *pb.WorkflowDefinition {
	return &pb.WorkflowDefinition{
		Workflow: &pb.WorkflowRecord{
			Id: id.String(), Name: "Workflow", Version: version,
			ExecutionTargetPolicy: &pb.ExecutionTargetConfiguration{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE},
		},
		DerivedWiring: &pb.DerivedWiring{},
	}
}

func TestWorkflowGraphApplyChecksStaleVersionBeforeAddedIdentity(t *testing.T) {
	workflowID := mustWorkflowID(t, emptyWorkflowGraphDocumentID)
	remote := &workflowGraphApplyStub{saves: []*pb.GraphSaveSuccess{{
		CurrentVersion:    2,
		ValidationResults: []*pb.ModeValidationResult{},
		Impact:            emptyGraphImpact(),
		Blockers: []*pb.GraphSaveBlocker{{
			Code: "version_changed", Message: "changed", Count: 2,
			AffectedEntities: []*pb.GraphEntityReference{},
		}},
	}}}
	document := workflowGraphDocument{
		WorkflowID: workflowID, ExpectedVersion: 1,
		Graph: workflowGraphDocumentGraph{
			NodeGroups: []workflowGraphDocumentNodeGroup{},
			Nodes: []workflowGraphDocumentNode{{
				ID: "not-a-uuid", Key: "node", Kind: "agent", DisplayName: "Node",
			}},
			TransitionGroups: []workflowGraphDocumentTransition{},
			Edges:            []workflowGraphDocumentEdge{},
		},
	}
	outcome := runWorkflowGraphApply(t.Context(), remote, document, false)
	if outcome.Outcome != workflowGraphApplyBlocked || len(outcome.Blockers) != 1 ||
		outcome.Blockers[0].Code != "version_changed" || len(remote.requests) != 1 {
		t.Fatalf("outcome=%+v requests=%+v", outcome, remote.requests)
	}
	assertWorkflowGraphApplyProjection(t, outcome)
}

func assertWorkflowGraphApplyProjection(t *testing.T, outcome workflowGraphApplyOutcome) {
	t.Helper()
	wantExit := 1
	if outcome.Outcome == workflowGraphApplySaved || outcome.Outcome == workflowGraphApplyUnchanged {
		wantExit = 0
	}
	for _, jsonOut := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		exitCode := writeWorkflowGraphApplyOutcome(&stdout, &stderr, outcome, jsonOut)
		if exitCode != wantExit {
			t.Fatalf("json=%t exit=%d stdout=%q stderr=%q", jsonOut, exitCode, stdout.String(), stderr.String())
		}
		if jsonOut {
			var rendered workflowGraphApplyOutcome
			if err := json.Unmarshal(stdout.Bytes(), &rendered); err != nil ||
				rendered.Outcome != outcome.Outcome || stderr.Len() != 0 {
				t.Fatalf("JSON outcome=%+v stderr=%q err=%v", rendered, stderr.String(), err)
			}
			continue
		}
		if wantExit == 0 {
			if stdout.Len() == 0 || stderr.Len() != 0 {
				t.Fatalf("human stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		} else if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("human stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	}
}

func TestWorkflowGraphApplyLoadsFileAndStdin(t *testing.T) {
	const document = `{"workflow_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}`
	path := filepath.Join(t.TempDir(), "workflow.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		path  string
		stdin *strings.Reader
	}{
		{name: "file", path: path},
		{name: "stdin", path: "-", stdin: strings.NewReader(document)},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := loadWorkflowGraphApplyInput(test.path, test.stdin)
			if err != nil || string(data) != document {
				t.Fatalf("data=%q err=%v", data, err)
			}
		})
	}
}

func TestWorkflowGraphAddedIdentityAndDraftContracts(t *testing.T) {
	workflowID := mustWorkflowID(t, emptyWorkflowGraphDocumentID)
	groupID := "group-id"
	definition := &pb.WorkflowDefinition{
		Workflow: &pb.WorkflowRecord{Id: workflowID.String(), Version: 3},
		NodeGroups: []*pb.WorkflowNodeGroup{{
			GroupId: groupID, GroupKey: "group", DisplayName: "Group",
		}},
		Nodes: []*pb.WorkflowNode{{
			Id: "node-id", Key: "node", Kind: pb.NodeKind_WORKFLOW_NODE_KIND_AGENT, DisplayName: "Node", GroupId: &groupID,
		}},
		TransitionGroups: []*pb.WorkflowTransitionGroup{{
			Id: "transition-group-id", SourceNodeId: "node-id", TransitionId: "next",
		}},
		Edges: []*pb.WorkflowEdge{{
			Id: "edge-id", TransitionGroupId: "transition-group-id", Key: "branch", TargetNodeId: "node-id",
			AssigneeSelection: pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_PREVIOUS_NODE, ThinkingSelection: pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED,
			Parameters: []*pb.Parameter{{Key: "role", Purpose: pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_TARGET_ASSIGNEE}},
		}},
	}
	draft := protoapi.WorkflowGraphDraftFromDefinition(definition)
	if draft.NodeGroups[0].Id != "group-id" || draft.Nodes[0].Id != "node-id" ||
		draft.TransitionGroups[0].Id != "transition-group-id" || draft.Edges[0].Id != "edge-id" ||
		draft.Edges[0].AssigneeSelection != pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_PREVIOUS_NODE ||
		draft.Edges[0].Parameters[0].Purpose != pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_TARGET_ASSIGNEE {
		t.Fatalf("draft=%+v", draft)
	}
}

func TestWorkflowEdgeSelectionAndPureMutationValidation(t *testing.T) {
	for _, valid := range []string{"configured", " previous_node "} {
		if _, err := parseWorkflowSelectionMode("assignee-selection", valid); err != nil {
			t.Fatalf("valid selection %q: %v", valid, err)
		}
	}
	if _, err := parseWorkflowSelectionMode("thinking-selection", "invalid"); err == nil {
		t.Fatal("invalid selection accepted")
	}

	parameters, err := workflowEdgeParametersForAdd(
		[]*pb.Parameter{{Key: "ordinary", Description: "value", Purpose: pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_ORDINARY}},
		pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_PREVIOUS_NODE,
		pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED,
		nil,
		nil,
	)
	if err != nil || len(parameters) != 2 || parameters[1].Purpose != pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_TARGET_ASSIGNEE {
		t.Fatalf("parameters=%+v err=%v", parameters, err)
	}
	if _, err := workflowEdgeParametersForAdd(nil, pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED, pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED, &pb.Parameter{
		Key: "role", Purpose: pb.ParameterPurpose_WORKFLOW_PARAMETER_PURPOSE_TARGET_ASSIGNEE,
	}, nil); err == nil {
		t.Fatal("protected parameter accepted for a disabled selector")
	}

	graph := &pb.GraphDraft{
		Nodes: []*pb.GraphDraftNode{
			{Id: "source-id", Key: "source"},
			{Id: "target-id", Key: "target"},
		},
		TransitionGroups: []*pb.GraphDraftTransitionGroup{},
		Edges:            []*pb.GraphDraftEdge{},
	}
	mutate := addWorkflowEdgeDraftMutation(workflowEdgeAddDraftMutation{
		SourceNodeKey:        "source",
		TargetNodeKey:        "target",
		TransitionID:         "next",
		NewTransitionGroupID: "group-id",
		Edge: &pb.GraphDraftEdge{
			Id: "edge-id", Key: "branch", AssigneeSelection: pb.AssigneeSelection_WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED, ThinkingSelection: pb.ThinkingSelection_WORKFLOW_THINKING_SELECTION_CONFIGURED, ContextSource: &pb.ContextSource{Kind: pb.ContextSourceKind_WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE},
		},
	})
	updated, result, err := mutate(graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.TransitionGroups) != 1 || updated.TransitionGroups[0].Id != "group-id" ||
		len(updated.Edges) != 1 || result.Edge.Id != "edge-id" ||
		result.Edge.TransitionGroupId != "group-id" || result.Edge.TargetNodeId != "target-id" {
		t.Fatalf("updated=%+v result=%+v", updated, result)
	}
}

func TestWorkflowAndTaskSearchArityAndPaginationValidation(t *testing.T) {
	if err := validateWorkflowPagination(0, workflowCommandWorkflowListLimit); err != nil {
		t.Fatalf("valid Workflow pagination: %v", err)
	}
	for _, window := range [][2]int{{-1, 1}, {0, 0}, {0, serverapi.WorkflowPaginationMaxLimit + 1}} {
		if err := validateWorkflowPagination(window[0], window[1]); err == nil {
			t.Fatalf("invalid Workflow pagination %v accepted", window)
		}
	}
	for _, test := range []struct {
		name string
		run  func(*bytes.Buffer, *bytes.Buffer) int
	}{
		{"workflow list positional", func(stdout, stderr *bytes.Buffer) int {
			return workflowListSubcommand([]string{"extra"}, stdout, stderr)
		}},
		{"workflow list removed token", func(stdout, stderr *bytes.Buffer) int {
			return workflowListSubcommand([]string{"--page-token", "legacy"}, stdout, stderr)
		}},
		{"task search missing query", func(stdout, stderr *bytes.Buffer) int {
			return taskSearchSubcommand(nil, stdout, stderr)
		}},
		{"task search extra query", func(stdout, stderr *bytes.Buffer) int {
			return taskSearchSubcommand([]string{"needle", "extra"}, stdout, stderr)
		}},
		{"task search negative offset", func(stdout, stderr *bytes.Buffer) int {
			return taskSearchSubcommand([]string{"needle", "--offset", "-1"}, stdout, stderr)
		}},
		{"task search incompatible flags", func(stdout, stderr *bytes.Buffer) int {
			return taskSearchSubcommand([]string{"needle", "--fts5", "--case-sensitive"}, stdout, stderr)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := test.run(&stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}

	statuses, err := parseTaskSearchStatusKinds([]string{"done,active", "done"})
	if err != nil || len(statuses) != 2 ||
		statuses[0] != serverapi.WorkflowTaskStatusKindActive ||
		statuses[1] != serverapi.WorkflowTaskStatusKindDone {
		t.Fatalf("statuses=%v err=%v", statuses, err)
	}
}

func TestWorkflowDispatchHelpRemainsAvailable(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"graph", "--help"},
		{"edge", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := workflowSubcommand(args, &stdout, &stderr); code != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%q exit=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
	if selector, err := parseWorkflowSelector("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"); err != nil ||
		!strings.EqualFold(selector.String(), "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa") {
		t.Fatalf("selector=%v err=%v", selector, err)
	}
}

func mustWorkflowID(t *testing.T, raw string) runtimeids.WorkflowID {
	t.Helper()
	id, err := runtimeids.ParseWorkflowID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func emptyGraphImpact() *pb.GraphSaveImpact {
	return &pb.GraphSaveImpact{
		RemovedEntities: []*pb.GraphEntityReference{},
	}
}
