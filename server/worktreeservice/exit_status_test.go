package worktree

import (
	"strconv"
	"testing"

	"core/shared/protoapi"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"google.golang.org/protobuf/proto"
)

func TestSetupExitStatusRoundTrips(t *testing.T) {
	for _, status := range []int{-1, 255, 2147483648, 3221225477, 4294967295} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			failed := setupFailureFromError(&setupScriptError{ExitCode: &status}, nil)
			processExit := failed.GetCause().GetProcessExit()
			if processExit == nil {
				t.Fatal("missing process exit failure")
			}
			encoded, err := proto.Marshal(processExit)
			if err != nil {
				t.Fatal(err)
			}
			decoded := &worktreepb.SetupProcessExit{}
			if err := proto.Unmarshal(encoded, decoded); err != nil {
				t.Fatal(err)
			}
			if err := protoapi.Validate(decoded); err != nil {
				t.Fatal(err)
			}
			if int64(decoded.ExitCode) != int64(status) {
				t.Fatalf("exit status = %d, want %d", decoded.ExitCode, status)
			}
		})
	}
	if err := protoapi.Validate(&worktreepb.SetupProcessExit{ExitCode: 0}); err == nil {
		t.Fatal("successful exit must not be accepted as a setup failure")
	}
}
