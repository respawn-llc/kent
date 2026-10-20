package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
)

func TestProjectHomePageTokenContract(t *testing.T) {
	for _, tc := range []struct {
		token string
		valid bool
	}{
		{"0", true},
		{"+0001", true},
		{"9223372036854775807", true},
		{"-1", false},
		{"invalid", false},
		{"9223372036854775808", false},
	} {
		t.Run(tc.token, func(t *testing.T) {
			err := protoapi.Validate(&projectpb.ProjectHomeListRequest{PageToken: &tc.token})
			if (err == nil) != tc.valid {
				t.Fatalf("validation = %v, want valid=%t", err, tc.valid)
			}
		})
	}
}
