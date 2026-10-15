package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"google.golang.org/protobuf/proto"
)

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
