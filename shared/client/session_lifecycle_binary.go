package client

import (
	"context"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"

	"google.golang.org/protobuf/types/known/emptypb"
)

func (c *Remote) GetInitialInput(ctx context.Context, request *sessionlaunchpb.SessionInitialInputRequest) (*sessionlaunchpb.SessionInitialInputSuccess, error) {
	return callGeneratedBinary(c, ctx, sessionLifecycleMethod("GetInitialInput"), request,
		&sessionlaunchpb.SessionInitialInputResult{}, func(failure *sessionlaunchpb.SessionInitialInputError) error {
			return generatedOperationFailure(failure.Code)
		})
}

func (c *Remote) PersistInputDraft(ctx context.Context, request *sessionlaunchpb.SessionPersistInputDraftRequest) (*emptypb.Empty, error) {
	control, err := c.draftControl(ctx, request.SessionId)
	if err != nil {
		return nil, err
	}
	method := sessionLifecycleMethod("PersistInputDraft")
	result := &sessionlaunchpb.SessionPersistInputDraftResult{}
	if err := control.callBinary(ctx, method, request, result); err != nil {
		return nil, err
	}
	return decodeGeneratedResult(method, result, func(failure *sessionlaunchpb.SessionPersistInputDraftError) error {
		return generatedOperationFailure(failure.Code)
	})
}
