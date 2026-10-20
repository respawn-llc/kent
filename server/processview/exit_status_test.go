package processview

import (
	"strconv"
	"testing"
	"time"

	shelltool "core/server/tools/shell"
	"core/shared/protoapi"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func TestProcessExitStatusRoundTrips(t *testing.T) {
	for _, status := range []int{-1, 0, 255, 2147483648, 3221225477, 4294967295} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			now := time.Now()
			process := ProcessFromSnapshot(shelltool.Snapshot{
				ID: uuid.NewString(), OwnerSessionID: uuid.NewString(),
				State: "completed", Command: "child", Workdir: "/tmp",
				StartedAt: now, FinishedAt: now, LastUpdatedAt: now,
				ExitCode: &status, LogPath: "/tmp/child.log",
			})
			encoded, err := proto.Marshal(process)
			if err != nil {
				t.Fatal(err)
			}
			decoded := &processpb.BackgroundProcess{}
			if err := proto.Unmarshal(encoded, decoded); err != nil {
				t.Fatal(err)
			}
			if err := protoapi.Validate(decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.ExitCode == nil || int64(*decoded.ExitCode) != int64(status) {
				t.Fatalf("exit status = %v, want %d", decoded.ExitCode, status)
			}
		})
	}
}
