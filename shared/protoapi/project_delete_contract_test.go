package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/project"
)

func TestProjectDeleteOutcomeRequiresBlockersExactlyWhenNotDeleted(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		for _, blocked := range []bool{false, true} {
			response := &pb.DeleteProjectSuccess{ProjectId: "project-1", Deleted: deleted}
			if blocked {
				response.Blockers = []*pb.ProjectDeleteBlocker{{Code: "active_sessions", Count: 1}}
			}
			if err := protoapi.Validate(response); (err == nil) != (deleted != blocked) {
				t.Errorf("deleted=%t blocked=%t acceptance=%v", deleted, blocked, err)
			}
		}
	}
}
