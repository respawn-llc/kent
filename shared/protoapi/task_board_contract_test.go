package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestTaskBoardProjectMustBeSubstantiveWithoutExactTrim(t *testing.T) {
	for _, project := range []string{" \t", "\u0085", " project-1 ", "\uFEFF"} {
		request := &taskpb.BoardGetRequest{
			ProjectId:   project,
			LabelFilter: &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_None{None: &emptypb.Empty{}}},
		}
		valid := project == " project-1 " || project == "\uFEFF"
		if err := protoapi.Validate(request); (err == nil) != valid {
			t.Errorf("project %q acceptance = %v", project, err)
		}
	}
}
