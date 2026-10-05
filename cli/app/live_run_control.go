package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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

func RunLiveWatchWithCleanup(ctx context.Context, opts Options, targetSessionID runtimeids.SessionID) RunLiveWatchResult {
	liveClient, closeFn, err := startRuntimeLiveControlClient(ctx, opts)
	if err != nil {
		return RunLiveWatchResult{Error: err, Close: closeFn}
	}
	response, err := liveClient.LiveWatch(ctx, &promptpb.LiveWatchRequest{SessionId: targetSessionID.String()})
	return RunLiveWatchResult{Response: response, Error: err, Close: closeFn}
}

func RunLiveSteer(ctx context.Context, opts Options, targetSessionID runtimeids.SessionID, text string) (*runtimepb.LiveSteerSuccess, error) {
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
		SessionId:       targetSessionID.String(),
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
	if !sessionID.IsCanonicalUUIDv4() {
		return nil, fmt.Errorf("invalid invoking Session ID %q: canonical UUIDv4 required", raw)
	}
	value := sessionID.String()
	return &value, nil
}

func RunLiveStop(ctx context.Context, opts Options, targetSessionID runtimeids.SessionID) (*runtimepb.LiveStopSuccess, error) {
	liveClient, closeFn, err := startRuntimeLiveControlClient(ctx, opts)
	if err != nil {
		if closeFn != nil {
			_ = closeFn()
		}
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return liveClient.LiveStop(ctx, &runtimepb.LiveStopRequest{
		SessionId: targetSessionID.String(),
	})
}

type RunLiveWaitResult struct {
	Result *runtimepb.LiveWaitSuccess
	Error  error
	Close  func() error
}

func RunLiveWait(ctx context.Context, opts Options, targetSessionID runtimeids.SessionID) (*runtimepb.LiveWaitSuccess, error) {
	result := RunLiveWaitWithCleanup(ctx, opts, targetSessionID)
	if result.Close != nil {
		_ = result.Close()
	}
	return result.Result, result.Error
}

func RunLiveWaitWithCleanup(ctx context.Context, opts Options, targetSessionID runtimeids.SessionID) RunLiveWaitResult {
	liveClient, closeFn, err := startRuntimeLiveControlClient(ctx, opts)
	if err != nil {
		return RunLiveWaitResult{Error: err, Close: closeFn}
	}
	resp, err := liveClient.LiveWait(ctx, &runtimepb.LiveWaitRequest{SessionId: targetSessionID.String()})
	return RunLiveWaitResult{
		Result: resp,
		Error:  err,
		Close:  closeFn,
	}
}
