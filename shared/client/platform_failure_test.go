package client

import (
	"errors"
	"testing"

	"core/shared/protoapi"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	onboardingpb "core/shared/protoapi/gen/kent/api/onboarding"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	serverpb "core/shared/protoapi/gen/kent/api/server"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/serverapi"
)

func TestGeneratedOperationAuthenticationFailures(t *testing.T) {
	tests := []struct {
		name   string
		decode func() error
	}{
		{"project create", func() error {
			_, err := decodeGeneratedResult(projectCatalogMethod("Create"),
				&projectpb.CreateProjectResult{Outcome: &projectpb.CreateProjectResult_Error{
					Error: &projectpb.CreateProjectError{Code: "auth_required",
						Detail: &projectpb.CreateProjectError_AuthRequired{AuthRequired: &authpb.AuthRequiredDetails{}}},
				}}, projectCreateGeneratedError)
			return err
		}},
		{"chat settings", func() error {
			_, err := decodeGeneratedResult(
				bootstrapMethod(chatsettingspb.File_kent_api_chat_settings_chat_settings_proto, "ChatSettingsService", "Read"),
				&chatsettingspb.ReadResult{Outcome: &chatsettingspb.ReadResult_Error{
					Error: &chatsettingspb.ReadError{Code: "auth_required",
						Detail: &chatsettingspb.ReadError_AuthRequired{AuthRequired: &authpb.AuthRequiredDetails{}}},
				}}, protoapi.ChatSettingsErrorFromProto)
			return err
		}},
		{"session plan", func() error {
			_, err := decodeGeneratedResult(sessionLaunchMethod("Plan"),
				&sessionlaunchpb.SessionPlanResult{Outcome: &sessionlaunchpb.SessionPlanResult_Error{
					Error: &sessionlaunchpb.SessionPlanError{Code: "auth_required",
						Detail: &sessionlaunchpb.SessionPlanError_AuthRequired{AuthRequired: &authpb.AuthRequiredDetails{}}},
				}}, protoapi.SessionPlanErrorFromProto)
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode(); !errors.Is(err, serverapi.ErrServerAuthRequired) {
				t.Fatalf("authentication failure lost: %v", err)
			}
		})
	}
}

func TestGeneratedOnboardingReadinessFailure(t *testing.T) {
	details := &serverpb.ServerNotReadyDetails{
		Reason: serverpb.ServerNotReadyReason_SERVER_NOT_READY_REASON_ONBOARDING_REQUIRED,
	}
	_, err := decodeGeneratedResult(
		bootstrapMethod(onboardingpb.File_kent_api_onboarding_onboarding_proto, "OnboardingService", "Finalize"),
		&onboardingpb.FinalizeResult{Outcome: &onboardingpb.FinalizeResult_Error{
			Error: &onboardingpb.FinalizeError{Code: "server_not_ready",
				Detail: &onboardingpb.FinalizeError_ServerNotReady{ServerNotReady: details}},
		}}, protoapi.OnboardingFinalizeErrorFromProto)
	var notReady *serverapi.ServerNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("readiness failure lost: %v", err)
	}
	roundTrip, conversionErr := protoapi.ServerNotReadyToProto(notReady)
	if conversionErr != nil || roundTrip.Reason != details.Reason {
		t.Fatalf("readiness reason lost: %v, %v", roundTrip, conversionErr)
	}
}
