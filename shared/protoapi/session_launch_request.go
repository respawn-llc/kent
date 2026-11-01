package protoapi

import (
	"fmt"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/emptypb"
)

func SessionLaunchRequestIntentToProto(intent serverapi.SessionLaunchIntent) (*sessionlaunchpb.SessionLaunchRequestIntent, error) {
	if id, present := intent.SessionID(); present {
		return &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId{
			OpenExistingSessionId: SessionSelectorToProto(id),
		}}, nil
	}
	origin, _ := intent.CreateOrigin()
	requestOrigin := &sessionlaunchpb.SessionCreateRequestOrigin{}
	switch origin.Kind() {
	case serverapi.SessionCreateOriginIndependent:
		requestOrigin.Origin = &sessionlaunchpb.SessionCreateRequestOrigin_Independent{Independent: &emptypb.Empty{}}
	case serverapi.SessionCreateOriginPreviousSession:
		id, _ := origin.SessionID()
		requestOrigin.Origin = &sessionlaunchpb.SessionCreateRequestOrigin_PreviousSessionId{PreviousSessionId: SessionSelectorToProto(id)}
	case serverapi.SessionCreateOriginParentAgent:
		id, _ := origin.SessionID()
		requestOrigin.Origin = &sessionlaunchpb.SessionCreateRequestOrigin_ParentAgentSessionId{ParentAgentSessionId: SessionSelectorToProto(id)}
	case serverapi.SessionCreateOriginNavigationOnly:
		id, _ := origin.SessionID()
		requestOrigin.Origin = &sessionlaunchpb.SessionCreateRequestOrigin_NavigationOnlySessionId{NavigationOnlySessionId: SessionSelectorToProto(id)}
	default:
		return nil, fmt.Errorf("invalid Session creation origin")
	}
	return &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_CreateNew{CreateNew: requestOrigin}}, nil
}
