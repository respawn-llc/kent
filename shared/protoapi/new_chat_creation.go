package protoapi

import (
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func NewChatSessionPlanRequest(target *chatpb.NewChatTarget, draft *string) *sessionlaunchpb.SessionPlanRequest {
	target = proto.Clone(target).(*chatpb.NewChatTarget)
	origin := target.Origin
	if origin == nil {
		origin = &sessionlaunchpb.SessionCreateRequestOrigin{Origin: &sessionlaunchpb.SessionCreateRequestOrigin_Independent{Independent: &emptypb.Empty{}}}
	}
	return &sessionlaunchpb.SessionPlanRequest{
		Mode:                sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent:              &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_CreateNew{CreateNew: origin}},
		InitialChatSettings: target.InitialSettings, InitialInputDraft: draft, InitialName: target.Name, Overrides: target.Invocation,
	}
}
