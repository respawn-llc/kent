package protoapi_test

import (
	"fmt"
	"testing"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func TestTaskCreateDependencyLimitAppliesPerRole(t *testing.T) {
	for _, counts := range []struct{ blockers, blocked int }{{50, 50}, {51, 0}, {0, 51}} {
		request := &taskpb.CreateRequest{ProjectId: "project-1", Title: "Task"}
		for role, count := range map[taskpb.DependencyRole]int{
			taskpb.DependencyRole_DEPENDENCY_ROLE_BLOCKER: counts.blockers,
			taskpb.DependencyRole_DEPENDENCY_ROLE_BLOCKED: counts.blocked,
		} {
			for index := range count {
				request.DependencyIntents = append(request.DependencyIntents, &taskpb.DependencyCreateIntent{
					RelatedTaskId: fmt.Sprintf("task-%d-%d", role, index), NewTaskRole: role,
				})
			}
		}
		valid := counts.blockers <= 50 && counts.blocked <= 50
		if err := protoapi.Validate(request); (err == nil) != valid {
			t.Errorf("role counts %+v acceptance = %v", counts, err)
		}
	}
}
