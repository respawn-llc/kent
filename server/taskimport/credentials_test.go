package taskimport

import (
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestCredentialSelection(t *testing.T) {
	candidates := []*string{proto.String("cli-env"), proto.String("server-env"), proto.String("cli-login"), proto.String("server-login")}
	for index := range candidates {
		got := selectCredential(candidates[0], candidates[1], candidates[2], candidates[3])
		if got == nil || *got != *candidates[index] {
			t.Fatalf("wrong candidate at precedence %d", index)
		}
		candidates[index] = nil
	}
	if selectCredential(nil, nil, nil, nil) != nil {
		t.Fatal("anonymous selected a credential")
	}
}
