package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/sessionenv"
)

type RunLiveWatchResult struct {
	Response *promptpb.LiveWatchSuccess
	Error    error
	Close    func() error
}

func RunLiveWatchWithCleanup(ctx context.Context, opts Options, targetSessionID runtimeids.SessionSelector) RunLiveWatchResult {
	caller, err := LiveSteerCallerSessionID()
	if err != nil {
		return RunLiveWatchResult{Error: err}
	}
	liveClient, closeFn, err := startRuntimeLiveControlClient(ctx, opts)
	if err != nil {
		return RunLiveWatchResult{Error: err, Close: closeFn}
	}
	response, err := liveClient.LiveWatch(ctx, &promptpb.LiveWatchRequest{SessionId: protoapi.SessionSelectorToProto(targetSessionID), CallerSessionId: caller})
	return RunLiveWatchResult{Response: response, Error: err, Close: closeFn}
}

func RunLiveSteer(ctx context.Context, opts Options, targetSessionID runtimeids.SessionSelector, text string) (*runtimepb.LiveSteerSuccess, error) {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, errors.New("text is required")
	}
	callerSessionID, err := LiveSteerCallerSessionID()
	if err != nil {
		return nil, err
	}
	liveClient, closeFn, err := startRuntimeLiveControlClient(ctx, opts)
	if err != nil {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return liveClient.LiveSteer(ctx, &runtimepb.LiveSteerRequest{
		SessionId:       protoapi.SessionSelectorToProto(targetSessionID),
		CallerSessionId: callerSessionID,
		Text:            trimmedText,
	})
}

func LiveSteerCallerSessionID() (*string, error) {
	raw, ok := sessionenv.LookupSessionID(os.LookupEnv)
	if !ok {
		return nil, nil
	}
	sessionID, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid invoking Session ID %q: %w", raw, err)
	}
	value := sessionID.String()
	return &value, nil
}

func RunLiveStop(ctx context.Context, opts Options, targetSessionID runtimeids.SessionSelector) (*runtimepb.LiveStopSuccess, error) {
	caller, err := LiveSteerCallerSessionID()
	if err != nil {
		return nil, err
	}
	liveClient, closeFn, err := startRuntimeLiveControlClient(ctx, opts)
	if err != nil {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return liveClient.LiveStop(ctx, &runtimepb.LiveStopRequest{
		SessionId: protoapi.SessionSelectorToProto(targetSessionID), CallerSessionId: caller,
	})
}

type RunLiveWaitResult struct {
	Result *runtimepb.LiveWaitSuccess
	Error  error
	Close  func() error
}

func RunLiveWait(ctx context.Context, opts Options, targetSessionID runtimeids.SessionSelector) (*runtimepb.LiveWaitSuccess, error) {
	result := RunLiveWaitWithCleanup(ctx, opts, targetSessionID)
	if result.Close != nil {
		_ = result.Close()
	}
	return result.Result, result.Error
}

func RunLiveWaitWithCleanup(ctx context.Context, opts Options, targetSessionID runtimeids.SessionSelector) RunLiveWaitResult {
	caller, err := LiveSteerCallerSessionID()
	if err != nil {
		return RunLiveWaitResult{Error: err}
	}
	liveClient, closeFn, err := startRuntimeLiveControlClient(ctx, opts)
	if err != nil {
		return RunLiveWaitResult{Error: err, Close: closeFn}
	}
	resp, err := liveClient.LiveWait(ctx, &runtimepb.LiveWaitRequest{SessionId: protoapi.SessionSelectorToProto(targetSessionID), CallerSessionId: caller})
	return RunLiveWaitResult{
		Result: resp,
		Error:  err,
		Close:  closeFn,
	}
}
