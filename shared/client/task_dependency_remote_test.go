package client

import (
	"errors"
	"testing"

	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
)

func TestGeneratedTaskDependencyErrorPreservesLimitFacts(t *testing.T) {
	detail := &taskpb.DependencyErrorDetails{
		Reason:        taskpb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_BLOCKER_LIMIT,
		BlockerTaskId: "task-blocker", BlockedTaskId: "task-blocked",
		CurrentCount: proto.Int32(50), Limit: proto.Int32(50),
	}
	method := bootstrapMethod(taskpb.File_kent_api_workflow_task_lifecycle_proto, "TaskDependencyService", "Add")
	_, err := decodeGeneratedResult(method, &taskpb.DependencyAddResult{
		Outcome: &taskpb.DependencyAddResult_Error{Error: &taskpb.DependencyAddError{
			Code: "dependency", Detail: &taskpb.DependencyAddError_Dependency{Dependency: detail},
		}},
	}, taskDependencyGeneratedError[*taskpb.DependencyAddError])
	var typed *TaskDependencyError
	if !errors.As(err, &typed) || !proto.Equal(typed.Detail, detail) {
		t.Fatalf("dependency limit facts lost: %v", err)
	}
}
