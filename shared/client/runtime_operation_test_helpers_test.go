package client

import (
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
)

func runtimeSubmitUserTurnRequestForTest(sessionID, text string) *runtimepb.SubmitUserTurnRequest {
	return &runtimepb.SubmitUserTurnRequest{QueueItemId: runtimeids.NewQueueItemID().String(), SessionId: sessionID,
		Input: &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: text}},
	}
}
