package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/session_launch"
	"google.golang.org/protobuf/proto"
)

func TestSessionTransitionRecordedPromptRequiresTextOnlyForPromptedActions(t *testing.T) {
	for _, action := range []pb.SessionTransitionAction{
		pb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NEW_SESSION,
		pb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK,
		pb.SessionTransitionAction_SESSION_TRANSITION_ACTION_RESUME,
	} {
		for _, recorded := range []bool{false, true} {
			for _, text := range []string{"", " \t ", "prompt"} {
				request := &pb.SessionTransition{
					Action: action, InitialPrompt: text, InitialPromptHistoryRecorded: recorded,
				}
				if action == pb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK {
					request.ForkRollbackTargetId = proto.String("target")
				}
				valid := action == pb.SessionTransitionAction_SESSION_TRANSITION_ACTION_RESUME || !recorded || text == "prompt"
				if err := protoapi.Validate(request); (err == nil) != valid {
					t.Errorf("action=%v recorded=%t text=%q acceptance=%v", action, recorded, text, err)
				}
			}
		}
	}
}
