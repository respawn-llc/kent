package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"google.golang.org/protobuf/proto"
)

func TestWorkflowDeleteOutcomeRequiresBlockersExactlyWhenNotDeleted(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		for _, blocked := range []bool{false, true} {
			response := &pb.DeleteSuccess{
				Deleted: deleted,
				Impact: &pb.DeleteImpact{WorkflowId: "11111111-1111-4111-8111-111111111111"},
			}
			if blocked {
				response.Blockers = []*pb.DeleteBlocker{{Code: "busy", Message: "Busy", Count: 1}}
			}
			if err := protoapi.Validate(response); (err == nil) != (deleted != blocked) {
				t.Errorf("deleted=%t blocked=%t acceptance=%v", deleted, blocked, err)
			}
		}
	}
}

func TestWorkflowDraftNodeGroupKeyResolvesUnlessExplicitIDOverrides(t *testing.T) {
	for _, test := range []struct {
		key   string
		id    *string
		valid bool
	}{
		{key: "", valid: true},
		{key: " \u0085", valid: true},
		{key: " group ", valid: true},
		{key: "missing"},
		{key: "missing", id: proto.String("explicit"), valid: true},
		{key: "missing", id: proto.String(" "), valid: true},
	} {
		graph := &pb.GraphDraft{
			NodeGroups: []*pb.GraphDraftNodeGroup{{Id: "", Key: " group "}},
			Nodes:      []*pb.GraphDraftNode{{Kind: pb.NodeKind_WORKFLOW_NODE_KIND_START, GroupKey: test.key, GroupId: test.id}},
		}
		if err := protoapi.Validate(graph); (err == nil) != test.valid {
			t.Errorf("key %q explicit ID %v acceptance = %v", test.key, test.id, err)
		}
	}
}

func TestWorkflowSaveGroupsRequireSubstantiveIdentity(t *testing.T) {
	for _, group := range []*pb.GraphDraftNodeGroup{
		{Id: " group-id ", Key: " group-key "},
		{Id: "\uFEFF", Key: "\uFEFF"},
		{Id: " \u0085", Key: "key"},
		{Id: "id", Key: " \t"},
	} {
		graph := &pb.GraphDraft{NodeGroups: []*pb.GraphDraftNodeGroup{group}}
		if err := protoapi.Validate(graph); err != nil {
			t.Fatalf("reportable draft rejected: %v", err)
		}
		valid := group.Id == " group-id " || group.Id == "\uFEFF"
		for _, request := range []proto.Message{
			&pb.GraphSaveRequest{WorkflowId: "11111111-1111-4111-8111-111111111111", Graph: graph},
			&pb.GraphSavePreviewRequest{WorkflowId: "11111111-1111-4111-8111-111111111111", Graph: graph},
		} {
			if err := protoapi.Validate(request); (err == nil) != valid {
				t.Errorf("%T group %+v acceptance = %v", request, group, err)
			}
		}
	}
}

func TestWorkflowSavedGroupIdentitiesAreUniqueAfterNormalization(t *testing.T) {
	for _, test := range []struct {
		groups []*pb.GraphDraftNodeGroup
		valid  bool
	}{
		{groups: []*pb.GraphDraftNodeGroup{{Id: " a ", Key: " x "}, {Id: "b", Key: "y"}}, valid: true},
		{groups: []*pb.GraphDraftNodeGroup{{Id: " a ", Key: "x"}, {Id: "a", Key: "y"}}},
		{groups: []*pb.GraphDraftNodeGroup{{Id: "a", Key: " x "}, {Id: "b", Key: "x"}}},
	} {
		graph := &pb.GraphDraft{NodeGroups: test.groups}
		if err := protoapi.Validate(graph); err != nil {
			t.Fatalf("reportable draft rejected: %v", err)
		}
		for _, request := range []proto.Message{
			&pb.GraphSaveRequest{WorkflowId: "11111111-1111-4111-8111-111111111111", Graph: graph},
			&pb.GraphSavePreviewRequest{WorkflowId: "11111111-1111-4111-8111-111111111111", Graph: graph},
		} {
			if err := protoapi.Validate(request); (err == nil) != test.valid {
				t.Errorf("%T groups %+v acceptance = %v", request, test.groups, err)
			}
		}
	}
}

func TestWorkflowSavedNodeGroupMembershipUsesNormalizedIdentity(t *testing.T) {
	for _, test := range []struct {
		id    *string
		valid bool
	}{
		{valid: true},
		{id: proto.String("group"), valid: true},
		{id: proto.String(" group "), valid: true},
		{id: proto.String("missing")},
		{id: proto.String(" ")},
	} {
		graph := &pb.GraphDraft{
			NodeGroups: []*pb.GraphDraftNodeGroup{{Id: " group ", Key: "key"}},
			Nodes:      []*pb.GraphDraftNode{{Kind: pb.NodeKind_WORKFLOW_NODE_KIND_START, GroupId: test.id}},
		}
		if err := protoapi.Validate(graph); err != nil {
			t.Fatalf("reportable draft rejected: %v", err)
		}
		for _, request := range []proto.Message{
			&pb.GraphSaveRequest{WorkflowId: "11111111-1111-4111-8111-111111111111", Graph: graph},
			&pb.GraphSavePreviewRequest{WorkflowId: "11111111-1111-4111-8111-111111111111", Graph: graph},
		} {
			if err := protoapi.Validate(request); (err == nil) != test.valid {
				t.Errorf("%T node %+v acceptance = %v", request, graph.Nodes[0], err)
			}
		}
	}
}

func TestWorkflowSaveOutcomeHasBlockersExactlyWhenUnsaved(t *testing.T) {
	for _, saved := range []bool{false, true} {
		for _, blocked := range []bool{false, true} {
			response := &pb.GraphSaveSuccess{Saved: saved, Impact: &pb.GraphSaveImpact{}}
			if saved {
				response.Definition = &pb.WorkflowDefinition{
					Workflow: &pb.WorkflowRecord{
						Id: "11111111-1111-4111-8111-111111111111", Name: "Workflow",
						ExecutionTargetPolicy: &pb.ExecutionTargetConfiguration{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_NONE},
					},
					DerivedWiring: &pb.DerivedWiring{},
				}
			}
			if blocked {
				response.Blockers = []*pb.GraphSaveBlocker{{Code: "version_changed", Message: "Version changed"}}
			}
			if err := protoapi.Validate(response); (err == nil) != (saved != blocked) {
				t.Errorf("saved=%v blocked=%v acceptance=%v", saved, blocked, err)
			}
		}
	}
}
