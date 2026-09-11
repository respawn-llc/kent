package protoapi

import (
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
)

func SessionLaunchDirectiveToProto(intent serverapi.SessionLaunchIntent, preparation *sessionlaunchpb.SessionLaunchPreparation) (*sessionlaunchpb.SessionDirective, error) {
	generatedIntent, err := SessionLaunchIntentToProto(intent)
	if err != nil {
		return nil, err
	}
	return &sessionlaunchpb.SessionDirective{
		Directive: &sessionlaunchpb.SessionDirective_Launch{Launch: &sessionlaunchpb.SessionLaunchDirective{
			Intent: generatedIntent, Preparation: preparation,
		}},
	}, nil
}
