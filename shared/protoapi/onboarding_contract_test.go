package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/onboarding"
)

func TestOnboardingDisabledSkillNamesAreSubstantive(t *testing.T) {
	for _, name := range []string{" \t", "\u0085", " Skill Name ", "\uFEFF"} {
		valid := name == " Skill Name " || name == "\uFEFF"
		if err := protoapi.Validate(&pb.FinalizeRequest{DisabledSkillNames: []string{name}}); (err == nil) != valid {
			t.Errorf("name %q acceptance = %v", name, err)
		}
	}
}
