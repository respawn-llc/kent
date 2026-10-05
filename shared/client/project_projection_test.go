package client_test

import (
	"testing"

	"core/shared/client"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/sessioncontract"
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
		summaries, err := client.SessionSummariesFromProto([]*projectpb.SessionSummary{
			{SessionId: "session-category-test", Category: tt.wire},
		})
		if !tt.valid {
			if err == nil || summaries != nil {
				t.Fatalf("category %v: got %v, %v; want nil and error", tt.wire, summaries, err)
			}
			continue
		}
		if err != nil || len(summaries) != 1 || summaries[0].Category != tt.category {
			t.Fatalf("category %v: got %v, %v", tt.wire, summaries, err)
		}
	}
}
