package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
)

func TestTaskCommentMutationsRequireSubstantiveBody(t *testing.T) {
	for _, body := range []string{" \t\n", "\u0085", "  comment\n", "\uFEFF"} {
		for _, request := range []proto.Message{
			&taskpb.CommentAddRequest{TaskId: "task-1", Body: body, Author: taskpb.CommentAuthorKind_COMMENT_AUTHOR_KIND_USER},
			&taskpb.CommentReplaceRequest{CommentId: "comment-1", Body: body},
		} {
			valid := body == "  comment\n" || body == "\uFEFF"
			if err := protoapi.Validate(request); (err == nil) != valid {
				t.Errorf("%T body %q acceptance = %v", request, body, err)
			}
		}
	}
}
