package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOptionalSessionNumberRequiresIdentity(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	number := int64(1)
	for _, pair := range []struct {
		id     *string
		number *int64
	}{{nil, nil}, {&id, nil}, {&id, &number}, {nil, &number}} {
		for _, message := range []proto.Message{
			&taskpb.ObserveQuestion{
				SessionId: pair.id, ShortId: pair.number,
				Question: &promptpb.ObservationQuestion{Question: &promptpb.ObservationQuestion_Ask{
					Ask: &promptpb.Question{SessionId: id, StepId: id, ToolCallId: "call-1", Question: "Continue?", CreatedAt: timestamppb.Now()},
				}},
			},
			&taskpb.ObserveFailure{SessionId: pair.id, ShortId: pair.number, Failure: &promptpb.LiveWatchFailure{Reason: "failed"}},
			&sessionpb.QuestionHistoryQuestion{AnsweredBySessionId: pair.id, AnsweredBySessionShortId: pair.number, Question: "Continue?", Answer: "Yes"},
		} {
			valid := pair.id != nil || pair.number == nil
			if err := protoapi.Validate(message); (err == nil) != valid {
				t.Errorf("%T identity=%v number=%v acceptance = %v", message, pair.id, pair.number, err)
			}
		}
	}
}
