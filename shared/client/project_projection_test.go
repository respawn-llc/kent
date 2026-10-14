package client_test

import (
	"testing"

	"core/shared/client"
	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSessionSummariesFromProtoCategoryValidation(t *testing.T) {
	for _, tt := range []struct {
		wire     projectpb.SessionCategory
		category sessioncontract.SessionCategory
		valid    bool
	}{
		{projectpb.SessionCategory_SESSION_CATEGORY_MAIN, sessioncontract.SessionCategoryMain, true},
		{projectpb.SessionCategory_SESSION_CATEGORY_SUBAGENT, sessioncontract.SessionCategorySubagent, true},
		{projectpb.SessionCategory_SESSION_CATEGORY_UNSPECIFIED, "", false},
		{projectpb.SessionCategory(3), "", false},
	} {
		wire := &projectpb.SessionSummary{
			SessionId: runtimeids.NewSessionID().String(), Category: tt.wire,
			UpdatedAt: timestamppb.Now(),
		}
		if !tt.valid {
			if err := protoapi.Validate(wire); err == nil {
				t.Fatalf("category %v accepted at the contract boundary", tt.wire)
			}
			continue
		}
		if err := protoapi.Validate(wire); err != nil {
			t.Fatal(err)
		}
		summaries := client.SessionSummariesFromProto([]*projectpb.SessionSummary{wire})
		if len(summaries) != 1 || summaries[0].Category != tt.category {
			t.Fatalf("category %v: got %v", tt.wire, summaries)
		}
	}
}
