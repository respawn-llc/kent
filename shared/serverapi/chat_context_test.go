package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	contextpb "core/shared/protoapi/gen/kent/api/chat_context"
	"core/shared/runtimeids"
)

func TestChatContextRequestAcceptsExactlyOneTarget(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	request := &contextpb.GetRequest{Target: &contextpb.Target{Target: &contextpb.Target_Session{
		Session: &contextpb.SessionTarget{SessionId: sessionID.String()},
	}}}
	if err := protoapi.Validate(request); err != nil {
		t.Fatalf("validate request: %v", err)
	}
}

func TestChatContextRequestRejectsMalformedTargets(t *testing.T) {
	tests := []struct {
		name    string
		request *contextpb.GetRequest
	}{
		{name: "missing target", request: &contextpb.GetRequest{}},
		{name: "missing arm", request: &contextpb.GetRequest{Target: &contextpb.Target{}}},
		{name: "noncanonical Session id", request: &contextpb.GetRequest{Target: &contextpb.Target{Target: &contextpb.Target_Session{
			Session: &contextpb.SessionTarget{SessionId: "session-1"},
		}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := protoapi.Validate(test.request); err == nil {
				t.Fatal("validate request succeeded")
			}
		})
	}
}

func TestChatContextResponseValidatesAllCompactionModes(t *testing.T) {
	for _, mode := range []contextpb.CompactionMode{
		contextpb.CompactionMode_COMPACTION_MODE_DISABLED,
		contextpb.CompactionMode_COMPACTION_MODE_LOCAL,
		contextpb.CompactionMode_COMPACTION_MODE_PROVIDER_NATIVE,
	} {
		t.Run(mode.String(), func(t *testing.T) {
			response := validChatContextResponse()
			response.Context.CompactionMode = mode
			if err := protoapi.Validate(response); err != nil {
				t.Fatalf("validate response: %v", err)
			}
		})
	}
}

func TestChatContextResponseRejectsInvalidFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*contextpb.Context)
	}{
		{name: "non-positive window", mutate: func(context *contextpb.Context) { context.ContextWindowTokens = 0 }},
		{name: "negative used", mutate: func(context *contextpb.Context) { context.UsedTokens = -1 }},
		{name: "negative threshold", mutate: func(context *contextpb.Context) { context.AutomaticThresholdTokens = -1 }},
		{name: "threshold above window", mutate: func(context *contextpb.Context) { context.AutomaticThresholdTokens = 101 }},
		{name: "negative completed count", mutate: func(context *contextpb.Context) { context.CompletedCompactionCount = -1 }},
		{name: "unknown mode", mutate: func(context *contextpb.Context) { context.CompactionMode = contextpb.CompactionMode(99) }},
		{name: "invalid remaining relation", mutate: func(context *contextpb.Context) { context.RemainingTokens++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := validChatContextResponse()
			test.mutate(response.Context)
			if err := protoapi.Validate(response); err == nil {
				t.Fatal("validate response succeeded")
			}
		})
	}
}

func TestChatContextResponseAllowsOverWindowUsage(t *testing.T) {
	response := validChatContextResponse()
	response.Context.UsedTokens = 125
	response.Context.RemainingTokens = -25
	if err := protoapi.Validate(response); err != nil {
		t.Fatalf("validate response: %v", err)
	}
}

func validChatContextResponse() *contextpb.GetSuccess {
	return &contextpb.GetSuccess{Context: &contextpb.Context{
		ContextWindowTokens:      100,
		UsedTokens:               40,
		RemainingTokens:          60,
		AutomaticThresholdTokens: 80,
		AutoCompactionEnabled:    true,
		CompactionMode:           contextpb.CompactionMode_COMPACTION_MODE_LOCAL,
		CompletedCompactionCount: 2,
		CompactionRunning:        false,
		ManualCompactAvailable:   true,
	}}
}
