package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"google.golang.org/protobuf/proto"
)

func TestProjectLabelRequestsRequireSubstantiveProject(t *testing.T) {
	for _, project := range []string{" \t", "\u0085", " project-1 ", "\uFEFF"} {
		for _, request := range []proto.Message{
			&pb.ProjectLabelCatalogRequest{ProjectId: project},
			&pb.ProjectLabelCreateRequest{ProjectId: project, Name: "Label"},
			&pb.ProjectLabelRenameRequest{ProjectId: project, LabelId: "11111111-1111-4111-8111-111111111111", Name: "Label"},
			&pb.ProjectLabelDeleteRequest{ProjectId: project, LabelId: "11111111-1111-4111-8111-111111111111"},
			&pb.ProjectLabelReorderRequest{ProjectId: project},
		} {
			valid := project == " project-1 " || project == "\uFEFF"
			if err := protoapi.Validate(request); (err == nil) != valid {
				t.Errorf("%T project %q acceptance = %v", request, project, err)
			}
		}
	}
}
