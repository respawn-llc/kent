package protoapi_test

import (
	"strings"
	"testing"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
)

func TestTaskCompletionCommentaryByteLimit(t *testing.T) {
	for _, size := range []int{65536, 65537} {
		commentary := strings.Repeat("é", size/2) + strings.Repeat("x", size%2)
		taskID := "task-1"
		request := &taskpb.CompleteRequest{
			TaskId:     &taskID,
			ActorKind:  taskpb.CompleteActorKind_COMPLETE_ACTOR_KIND_USER,
			Force:      true,
			Commentary: &commentary,
		}
		if err := protoapi.Validate(request); (err == nil) != (size == 65536) {
			t.Errorf("commentary bytes %d acceptance = %v", size, err)
		}
	}
}
