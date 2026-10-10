package requestfixture

import (
	"testing"

	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/durationpb"
)

func SessionID(t testing.TB, raw string) runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func LaunchIntent(t testing.TB, intent serverapi.SessionLaunchIntent) *sessionlaunchpb.SessionLaunchRequestIntent {
	t.Helper()
	value, err := protoapi.SessionLaunchRequestIntentToProto(intent)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func RunPrompt(t testing.TB, value serverapi.RunPromptRequest) *runpromptpb.Request {
	t.Helper()
	overrides, err := protoapi.RunPromptOverridesToProto(value.Overrides)
	if err != nil {
		t.Fatal(err)
	}
	request := &runpromptpb.Request{
		Intent: LaunchIntent(t, value.Intent), Prompt: value.Prompt,
		CallerSessionId: textutil.Pointer(value.CallerSessionID), Overrides: overrides,
	}
	if value.Timeout != 0 {
		request.Timeout = durationpb.New(value.Timeout)
	}
	return request
}
