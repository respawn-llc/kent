package client

import (
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
)

func runtimeSubmitUserTurnRequestForTest(sessionID, text string) *runtimepb.SubmitUserTurnRequest {
	return &runtimepb.SubmitUserTurnRequest{
		SessionId: sessionID,
		Input:     &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: text}},
	}
}
