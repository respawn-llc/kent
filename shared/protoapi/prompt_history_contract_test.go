package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
)

func TestRecordHistoryRequiresSubstantiveText(t *testing.T) {
	for _, test := range []struct {
		text  string
		valid bool
	}{
		{"", false},
		{" \t\n", false},
		{"\u0085", false},
		{"\uFEFF", true},
		{"  prompt\n", true},
	} {
		request := &promptpb.RecordHistoryRequest{
			SessionId: "11111111-1111-4111-8111-111111111111",
			Text:      test.text,
		}
		if err := protoapi.Validate(request); (err == nil) != test.valid {
			t.Errorf("text %q acceptance = %v", test.text, err)
		}
	}
}
