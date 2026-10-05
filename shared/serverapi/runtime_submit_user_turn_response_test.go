package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
)

func TestRuntimeSubmitUserTurnResponseValidatesTypedOutcome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response *runtimepb.SubmitUserTurnSuccess
		wantErr  bool
	}{
		{name: "missing result", response: &runtimepb.SubmitUserTurnSuccess{}, wantErr: true},
		{
			name: "valid queued",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_Queued{
				Queued: &runtimepb.SubmitUserTurnQueued{Steered: true, QueueItemId: runtimeids.NewQueueItemID().String()},
			}},
		},
		{
			name: "queued requires queue identity",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_Queued{
				Queued: &runtimepb.SubmitUserTurnQueued{Steered: true},
			}},
			wantErr: true,
		},
		{
			name: "queued must be steered",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_Queued{
				Queued: &runtimepb.SubmitUserTurnQueued{QueueItemId: runtimeids.NewQueueItemID().String()},
			}},
			wantErr: true,
		},
		{
			name: "valid assistant final",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_AssistantFinal{
				AssistantFinal: &runtimepb.SubmitUserTurnAssistantFinal{Message: "answer"},
			}},
		},
		{
			name: "assistant final requires message",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_AssistantFinal{
				AssistantFinal: &runtimepb.SubmitUserTurnAssistantFinal{},
			}},
			wantErr: true,
		},
		{
			name: "assistant final rejects blank message",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_AssistantFinal{
				AssistantFinal: &runtimepb.SubmitUserTurnAssistantFinal{Message: " \n\t "},
			}},
			wantErr: true,
		},
		{
			name: "valid no final",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_NoFinal{
				NoFinal: &runtimepb.SubmitUserTurnNoFinal{},
			}},
		},
		{
			name: "valid silent final",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_SilentFinal{
				SilentFinal: &runtimepb.SubmitUserTurnSilentFinal{},
			}},
		},
		{
			name: "silent final rejects nonempty message",
			response: &runtimepb.SubmitUserTurnSuccess{Result: &runtimepb.SubmitUserTurnSuccess_SilentFinal{
				SilentFinal: &runtimepb.SubmitUserTurnSilentFinal{Message: "not blank"},
			}},
			wantErr: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := protoapi.Encode(testCase.response)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("Encode() error = %v, wantErr=%t", err, testCase.wantErr)
			}
		})
	}
}
