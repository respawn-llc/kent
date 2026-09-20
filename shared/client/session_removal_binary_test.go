package client

import (
	"testing"

	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
)

func TestSessionRemovalRejectsInternalFailureWithoutDetails(t *testing.T) {
	_, err := decodeGeneratedResult(sessionLifecycleMethod("Archive"),
		&sessionlaunchpb.SessionArchiveResult{Outcome: &sessionlaunchpb.SessionArchiveResult_Error{Error: &sessionlaunchpb.SessionArchiveError{
			Code: "internal_failure",
		}}}, func(failure *sessionlaunchpb.SessionArchiveError) error {
			t.Fatal("malformed internal failure reached the operation decoder")
			return nil
		})
	if err == nil {
		t.Fatal("malformed internal failure was accepted")
	}
}

func TestSessionRemovalInternalFailurePreservesServerCause(t *testing.T) {
	cause := "server-authored Session removal failure"
	internal := &sharedpb.InternalFailureDetails{Cause: &cause}
	_, archiveErr := decodeGeneratedResult(sessionLifecycleMethod("Archive"),
		&sessionlaunchpb.SessionArchiveResult{Outcome: &sessionlaunchpb.SessionArchiveResult_Error{Error: &sessionlaunchpb.SessionArchiveError{
			Code: "internal_failure",
			Detail: &sessionlaunchpb.SessionArchiveError_InternalFailure{
				InternalFailure: internal,
			},
		}}}, sessionArchiveGeneratedError)
	_, deleteErr := decodeGeneratedResult(sessionLifecycleMethod("Delete"),
		&sessionlaunchpb.SessionDeleteResult{Outcome: &sessionlaunchpb.SessionDeleteResult_Error{Error: &sessionlaunchpb.SessionDeleteError{
			Code: "internal_failure",
			Detail: &sessionlaunchpb.SessionDeleteError_InternalFailure{
				InternalFailure: internal,
			},
		}}}, sessionDeleteGeneratedError)
	for _, err := range []error{archiveErr, deleteErr} {
		if err == nil || err.Error() != cause {
			t.Fatalf("Session removal error = %v, want server cause", err)
		}
	}
}
