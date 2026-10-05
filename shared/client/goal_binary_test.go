package client

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRemoteSetGoalPreservesSuccessfulDiagnostic(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	server := newRemoteTestServer(t, func(ws *websocket.Conn) {
		acceptRemoteHandshake(t, ws)
		var encoded []byte
		if err := websocket.Message.Receive(ws, &encoded); err != nil {
			return
		}
		envelope, err := protoapi.DecodeEnvelope(encoded)
		if err != nil {
			t.Errorf("decode Goal Set envelope: %v", err)
			return
		}
		call := envelope.GetCall()
		if call == nil || call.Correlation == nil {
			return
		}
		method := bootstrapMethod(runtimepb.File_kent_api_runtime_runtime_proto, "GoalService", "Set")
		operation, err := protoapi.OperationFromDescriptor(method)
		if err != nil {
			t.Errorf("Goal Set operation: %v", err)
			return
		}
		if call.Operation != operation.Name {
			return
		}
		request := &runtimepb.GoalSetRequest{}
		if err := protoapi.Decode(call.Payload, request); err != nil {
			t.Errorf("decode Goal Set request: %v", err)
			return
		}
		now := timestamppb.Now()
		result := &runtimepb.GoalSetResult{
			Outcome: &runtimepb.GoalSetResult_Success{
				Success: &runtimepb.GoalSetSuccess{
					Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
					Outcome: &runtimepb.GoalSetSuccess_Mutation{
						Mutation: &runtimepb.GoalMutationSuccess{
							Kind: runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL,
							Goal: &runtimepb.Goal{
								Id:        "goal-1",
								Objective: "ship the feature",
								Status:    runtimepb.GoalStatus_RUNTIME_GOAL_STATUS_ACTIVE,
								CreatedAt: now,
								UpdatedAt: now,
							},
						},
					},
					Diagnostic: &runtimepb.GoalSetError{
						Code: "internal_failure",
						Detail: &runtimepb.GoalSetError_InternalFailure{
							InternalFailure: &sharedpb.InternalFailureDetails{
								Operation: stringPointer("runtime.detach"),
								Cause:     stringPointer("release failed"),
							},
						},
					},
				},
			},
		}
		sendRemoteDescriptorResult(t, ws, method, call.Correlation, result)
	})
	remote, err := DialRemoteURL(context.Background(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatalf("DialRemoteURL: %v", err)
	}
	defer func() { _ = remote.Close() }()

	response, err := remote.SetGoal(context.Background(), &runtimepb.GoalSetRequest{
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		}},
		Objective:       "ship the feature",
		Actor:           "user",
		ExecutionPolicy: runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
	})
	if err != nil {
		t.Fatalf("SetGoal: %v", err)
	}
	if response.GetMutation().Kind != runtimepb.GoalMutationResultKind_GOAL_MUTATION_RESULT_KIND_AUTHORITATIVE_GOAL {
		t.Fatalf("mutation result = %+v, want authoritative Goal", response.GetMutation())
	}
	if response.Diagnostic == nil || response.Diagnostic.GetCode() == "" {
		t.Fatalf("diagnostic = %v, want post-commit diagnostic", response.Diagnostic)
	}
}

func TestGoalSetGeneratedErrorPreservesUnknownDetailsAndFields(t *testing.T) {
	unknown := []byte{0x98, 0x06, 0x07}
	nestedUnknown := []byte{0xa0, 0x06, 0x09}
	failure := &runtimepb.GoalSetError{
		Code: "future_code",
		Detail: &runtimepb.GoalSetError_InternalFailure{
			InternalFailure: &sharedpb.InternalFailureDetails{
				Operation: stringPointer("goal.set"),
				Cause:     stringPointer("fixture failure"),
			},
		},
	}
	failure.ProtoReflect().SetUnknown(unknown)
	failure.GetInternalFailure().ProtoReflect().SetUnknown(nestedUnknown)

	err := goalSetGeneratedError(failure)
	var generated *GoalSetGeneratedError
	if !errors.As(err, &generated) {
		t.Fatalf("error = %T %v, want GoalSetGeneratedError", err, err)
	}
	if generated.Failure != failure {
		t.Fatalf("generated Failure = %p, want original %p", generated.Failure, failure)
	}
	if generated.Failure.Code != "future_code" ||
		generated.Failure.GetInternalFailure().GetOperation() != "goal.set" ||
		generated.Failure.GetInternalFailure().GetCause() != "fixture failure" {
		t.Fatalf("generated Failure = %+v, want typed future detail", generated.Failure)
	}
	if !bytes.Equal(generated.Failure.ProtoReflect().GetUnknown(), unknown) {
		t.Fatalf("outer unknown fields = %x, want %x", generated.Failure.ProtoReflect().GetUnknown(), unknown)
	}
	if !bytes.Equal(generated.Failure.GetInternalFailure().ProtoReflect().GetUnknown(), nestedUnknown) {
		t.Fatalf(
			"nested unknown fields = %x, want %x",
			generated.Failure.GetInternalFailure().ProtoReflect().GetUnknown(),
			nestedUnknown,
		)
	}
}
