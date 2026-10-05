package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/sessioncontract"
)

func TestSessionCategoryConversions(t *testing.T) {
	for _, tt := range []struct {
		category sessioncontract.SessionCategory
		wire     projectpb.SessionCategory
	}{
		{sessioncontract.SessionCategoryMain, 1},
		{sessioncontract.SessionCategorySubagent, 2},
	} {
		t.Run(string(tt.category), func(t *testing.T) {
			wire, err := protoapi.SessionCategoryToProto(tt.category)
			if err != nil || wire != tt.wire {
				t.Fatalf("encode: got %v, %v; want %v", wire, err, tt.wire)
			}
			category, err := protoapi.SessionCategoryFromProto(tt.wire)
			if err != nil || category != tt.category {
				t.Fatalf("decode: got %v, %v; want %v", category, err, tt.category)
			}
		})
	}
}

func TestSessionCategoryConversionsRejectInvalidValues(t *testing.T) {
	for _, category := range []sessioncontract.SessionCategory{"", "unknown"} {
		wire, err := protoapi.SessionCategoryToProto(category)
		if err == nil || wire != 0 {
			t.Fatalf("encode %q: got %v, %v; want zero and error", category, wire, err)
		}
	}
	for _, wire := range []projectpb.SessionCategory{0, -1, 3} {
		category, err := protoapi.SessionCategoryFromProto(wire)
		if err == nil || category != "" {
			t.Fatalf("decode %v: got %q, %v; want zero and error", wire, category, err)
		}
	}
}
