package workflowstore

import (
	"reflect"
	"testing"

	"core/server/workflow"
	"core/shared/runtimeids"
)

func TestWorkflowGraphSaveRejectsMalformedIdentityWithoutMutation(t *testing.T) {
	for _, invalid := range []string{
		"aaaaaaaa-aaaa-5aaa-8aaa-aaaaaaaaaaaa",
		"AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
		" aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		"aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa",
		"aaaaaaaa-aaaa-4aaa-1aaa-aaaaaaaaaaaa",
		"00000000-0000-0000-0000-000000000000",
		"",
	} {
		t.Run(invalid, func(t *testing.T) {
			f := newGraphSaveFixture(t, createValidWorkflow)
			req := f.request(f.record.Version, false, f.def)
			req.Metadata = &WorkflowGraphSaveMetadata{Name: "Must not persist"}
			nodeID := workflow.NodeID(invalid)
			groupID := workflow.TransitionGroupID(runtimeids.NewGraphEntityID())
			req.Nodes = append(req.Nodes, NodeRecord{
				ID: nodeID, WorkflowID: f.workflowID, Key: "added", Kind: workflow.NodeKindAgent,
				DisplayName: "Added", SubagentRole: workflow.DefaultAgentRole,
			})
			req.TransitionGroups = append(req.TransitionGroups, TransitionGroupRecord{
				ID: groupID, WorkflowID: f.workflowID, SourceNodeID: req.Nodes[1].ID,
				TransitionID: "added", DisplayName: "Added",
			})
			req.Edges = append(req.Edges, EdgeRecord{
				ID: workflow.EdgeID(runtimeids.NewGraphEntityID()), WorkflowID: f.workflowID,
				TransitionGroupID: groupID, Key: "added", TargetNodeID: nodeID,
				AssigneeSelection: workflow.AssigneeSelectionConfigured,
				ThinkingSelection: workflow.ThinkingSelectionConfigured,
				ContextMode:       workflow.ContextModeNewSession, PromptTemplate: "Work",
			})
			if _, err := f.store.PreviewWorkflowGraphSave(f.ctx, req); err == nil {
				t.Fatal("preview accepted malformed graph identity")
			}
			if _, err := f.store.SaveWorkflowGraph(f.ctx, req); err == nil {
				t.Fatal("save accepted malformed graph identity")
			}
			def, record := f.current(t)
			if !reflect.DeepEqual(def, f.def) || !reflect.DeepEqual(record, f.record) {
				t.Fatal("rejected graph save changed the Workflow")
			}
		})
	}
}

func TestWorkflowGraphSaveValidatesEveryIdentityAndReference(t *testing.T) {
	const invalid = "aaaaaaaa-aaaa-5aaa-8aaa-aaaaaaaaaaaa"
	for name, change := range map[string]func(*WorkflowGraphSaveRequest){
		"node group": func(req *WorkflowGraphSaveRequest) {
			req.NodeGroups = append(req.NodeGroups, NodeGroupRecord{ID: invalid, WorkflowID: req.WorkflowID, Key: "added"})
		},
		"node":                 func(req *WorkflowGraphSaveRequest) { req.Nodes[0].ID = workflow.NodeID(invalid) },
		"node group reference": func(req *WorkflowGraphSaveRequest) { id := invalid; req.Nodes[0].GroupID = &id },
		"join provider reference": func(req *WorkflowGraphSaveRequest) {
			for index := range req.Nodes {
				if req.Nodes[index].Kind == workflow.NodeKindJoin {
					req.Nodes[index].JoinInputProviders = []workflow.JoinInputProvider{{InputName: "input", ProviderEdgeID: workflow.EdgeID(invalid)}}
					return
				}
			}
		},
		"transition group":      func(req *WorkflowGraphSaveRequest) { req.TransitionGroups[0].ID = workflow.TransitionGroupID(invalid) },
		"source node reference": func(req *WorkflowGraphSaveRequest) { req.TransitionGroups[0].SourceNodeID = workflow.NodeID(invalid) },
		"edge":                  func(req *WorkflowGraphSaveRequest) { req.Edges[0].ID = workflow.EdgeID(invalid) },
		"transition group reference": func(req *WorkflowGraphSaveRequest) {
			req.Edges[0].TransitionGroupID = workflow.TransitionGroupID(invalid)
		},
		"target node reference": func(req *WorkflowGraphSaveRequest) { req.Edges[0].TargetNodeID = workflow.NodeID(invalid) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newGraphSaveFixture(t, createFanoutJoinWorkflow)
			req := f.request(f.record.Version, false, f.def)
			change(&req)
			if _, err := f.store.SaveWorkflowGraph(f.ctx, req); err == nil {
				t.Fatal("save accepted malformed graph identity or reference")
			}
			def, record := f.current(t)
			if !reflect.DeepEqual(def, f.def) || !reflect.DeepEqual(record, f.record) {
				t.Fatal("rejected graph save changed the Workflow")
			}
		})
	}
}
