package protoapi

import (
	"testing"

	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/proto"
)

func TestSessionSelectorContract(t *testing.T) {
	id := runtimeids.NewSessionID()
	for _, tc := range []struct {
		selector *sharedpb.SessionSelector
		valid    bool
	}{
		{&sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id.String()}}, true},
		{&sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_ShortId{ShortId: 1}}, true},
		{&sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_ShortId{ShortId: runtimeids.MaxSessionShortID}}, true},
		{&sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_ShortId{ShortId: 0}}, false},
		{&sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_ShortId{ShortId: -1}}, false},
		{&sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_ShortId{ShortId: runtimeids.MaxSessionShortID + 1}}, false},
		{&sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: "123"}}, false},
		{&sharedpb.SessionSelector{}, false},
	} {
		if err := Validate(tc.selector); (err == nil) != tc.valid {
			t.Fatalf("selector %v validation = %v, want valid=%t", tc.selector, err, tc.valid)
		}
		if tc.valid {
			selector, err := SessionSelectorFromProto(tc.selector)
			if err != nil {
				t.Fatal(err)
			}
			reconstructed := SessionSelectorToProto(selector)
			if !proto.Equal(reconstructed, tc.selector) {
				t.Fatalf("selector roundtrip = %v, want %v", reconstructed, tc.selector)
			}
		}
	}
}
