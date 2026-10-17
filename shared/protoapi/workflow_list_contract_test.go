package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"google.golang.org/protobuf/proto"
)

func TestWorkflowListProjectScopeIsOptionalSubstantiveAndUnpadded(t *testing.T) {
	for _, project := range []*string{nil, proto.String("project-1"), proto.String("\uFEFF"), proto.String(""), proto.String("\u0085"), proto.String(" project-1 ")} {
		valid := project == nil || *project == "project-1" || *project == "\uFEFF"
		if err := protoapi.Validate(&pb.ListRequest{ProjectId: project}); (err == nil) != valid {
			t.Errorf("Project scope %v acceptance = %v", project, err)
		}
	}
}
