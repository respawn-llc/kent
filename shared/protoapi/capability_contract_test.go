package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/capability"
	"google.golang.org/protobuf/proto"
)

func TestCapabilityKnownModelIdentity(t *testing.T) {
	for _, tc := range []struct {
		id    *string
		known bool
		valid bool
	}{
		{nil, true, false},
		{proto.String(" \t"), true, false},
		{proto.String("model"), false, false},
		{proto.String(" model "), true, true},
		{proto.String("\uFEFF"), true, true},
	} {
		facts := &pb.ModelFacts{
			KnownModels:     []*pb.ModelFact{{ModelId: tc.id, Known: tc.known, Verbosity: &pb.ModelVerbosityFact{Source: "catalog"}}},
			UnknownFallback: &pb.ModelFact{Verbosity: &pb.ModelVerbosityFact{Source: "fallback"}},
		}
		if err := protoapi.Validate(facts); (err == nil) != tc.valid {
			t.Errorf("id=%v known=%v acceptance = %v", tc.id, tc.known, err)
		}
	}
}
