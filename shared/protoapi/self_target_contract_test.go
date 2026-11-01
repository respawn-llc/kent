package protoapi_test

import (
	"errors"
	"testing"

	"core/shared/protoapi"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/emptypb"
)

func TestSelfTargetRequiresResolvedIdentity(t *testing.T) {
	id := runtimeids.NewSessionID()
	for _, test := range []struct {
		name     string
		context  *sharedpb.SessionTargetContext
		accepted bool
	}{
		{name: "unresolved", context: &sharedpb.SessionTargetContext{Stage: &sharedpb.SessionTargetContext_Unresolved{Unresolved: &emptypb.Empty{}}}},
		{name: "canonical", context: &sharedpb.SessionTargetContext{Stage: &sharedpb.SessionTargetContext_CanonicalSessionId{CanonicalSessionId: id.String()}}, accepted: true},
		{name: "projected", context: &sharedpb.SessionTargetContext{Stage: &sharedpb.SessionTargetContext_Projected{Projected: &sharedpb.SessionIdentity{SessionId: id.String()}}}, accepted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := &sharedpb.SessionSelfTargetDetails{SessionTarget: test.context}
			wire, err := protoapi.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			decoded := &sharedpb.SessionSelfTargetDetails{}
			err = protoapi.Decode(wire, decoded)
			if (err == nil) != test.accepted {
				t.Fatalf("decode error = %v, want accepted=%t", err, test.accepted)
			}
			if !test.accepted {
				return
			}
			failure := protoapi.SessionSelfTargetFromProto(decoded)
			var self *serverapi.SessionSelfTargetError
			var target *serverapi.SessionTargetError
			if !errors.As(failure, &self) || self.SessionID != id || !errors.As(failure, &target) || *target.SessionID() != id {
				t.Fatalf("resolved identity not preserved: %v", failure)
			}
		})
	}
}
