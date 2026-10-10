package protoapi

import (
	"fmt"

	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/serverapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/types/known/durationpb"
)

func RunPromptRequestFromProto(value *runpromptpb.Request, intent serverapi.SessionLaunchIntent) (serverapi.RunPromptRequest, error) {
	if err := Validate(value); err != nil {
		return serverapi.RunPromptRequest{}, err
	}
	var err error
	request := serverapi.RunPromptRequest{Intent: intent, Prompt: value.Prompt, CallerSessionID: textutil.Pointer(value.CallerSessionId)}
	if value.Overrides != nil {
		request.Overrides, err = RunPromptOverridesFromProto(value.Overrides)
		if err != nil {
			return serverapi.RunPromptRequest{}, err
		}
	}
	if value.Timeout != nil {
		request.Timeout = value.Timeout.AsDuration()
		roundTrip := durationpb.New(request.Timeout)
		if roundTrip.Seconds != value.Timeout.Seconds || roundTrip.Nanos != value.Timeout.Nanos {
			return serverapi.RunPromptRequest{}, fmt.Errorf("Run Prompt timeout is outside the supported duration range")
		}
	}
	return request, request.Validate()
}
