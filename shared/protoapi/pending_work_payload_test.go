package protoapi_test

import (
	"reflect"
	"testing"

	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeinput"
)

func TestPendingWorkPayloadContract(t *testing.T) {
	guidance, selector := "retain context", "feature branch"
	for _, tc := range []struct {
		name      string
		canonical string
		kind      runtimeinput.PendingWorkItemKind
		payload   func(*runtimepb.PendingWorkItem)
		queueable bool
	}{
		{"message", "hello 日本語", runtimeinput.PendingWorkItemKindMessage, func(item *runtimepb.PendingWorkItem) {
			item.Payload = &runtimepb.PendingWorkItem_Message{Message: &runtimepb.PendingWorkMessage{Text: "hello 日本語"}}
		}, true},
		{"shell", "$echo hello", runtimeinput.PendingWorkItemKindShell, func(item *runtimepb.PendingWorkItem) {
			item.Payload = &runtimepb.PendingWorkItem_Shell{Shell: &chatpb.ShellCommand{Command: "echo hello"}}
		}, true},
		{"compaction", "/compact", runtimeinput.PendingWorkItemKindManualCompaction, func(item *runtimepb.PendingWorkItem) {
			item.Payload = &runtimepb.PendingWorkItem_ManualCompaction{ManualCompaction: &runtimepb.ManualCompactionAdmission{}}
		}, false},
		{"guided compaction", "/compact " + guidance, runtimeinput.PendingWorkItemKindManualCompaction, func(item *runtimepb.PendingWorkItem) {
			item.Payload = &runtimepb.PendingWorkItem_ManualCompaction{ManualCompaction: &runtimepb.ManualCompactionAdmission{Guidance: &guidance}}
		}, false},
		{"worktree enter", "/wt switch " + selector, runtimeinput.PendingWorkItemKindWorktreeTransition, func(item *runtimepb.PendingWorkItem) {
			item.Payload = &runtimepb.PendingWorkItem_WorktreeTransition{WorktreeTransition: &runtimepb.PendingWorkWorktreeTransition{
				Transition: runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_ENTER, Selector: &selector,
			}}
		}, false},
		{"worktree leave", "/wt leave", runtimeinput.PendingWorkItemKindWorktreeTransition, func(item *runtimepb.PendingWorkItem) {
			item.Payload = &runtimepb.PendingWorkItem_WorktreeTransition{WorktreeTransition: &runtimepb.PendingWorkWorktreeTransition{
				Transition: runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_LEAVE,
			}}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newItem := func() *runtimepb.PendingWorkItem {
				item := &runtimepb.PendingWorkItem{
					Id:             "11111111-1111-4111-8111-111111111111",
					Lane:           runtimepb.PendingWorkLane_PENDING_WORK_LANE_STEER,
					State:          runtimepb.PendingWorkItemState_PENDING_WORK_ITEM_STATE_PENDING,
					CanonicalInput: tc.canonical,
				}
				tc.payload(item)
				return item
			}
			for _, lane := range []runtimepb.PendingWorkLane{
				runtimepb.PendingWorkLane_PENDING_WORK_LANE_STEER,
				runtimepb.PendingWorkLane_PENDING_WORK_LANE_QUEUE,
			} {
				item := newItem()
				item.Lane = lane
				work := &runtimepb.PendingWork{Items: []*runtimepb.PendingWorkItem{item}}
				encoded, err := protoapi.Encode(work)
				valid := lane == runtimepb.PendingWorkLane_PENDING_WORK_LANE_STEER || tc.queueable
				if (err == nil) != valid {
					t.Fatalf("lane %v validation = %v, want valid=%t", lane, err, valid)
				}
				if !valid {
					continue
				}
				var decoded runtimepb.PendingWork
				if err := protoapi.Decode(encoded, &decoded); err != nil {
					t.Fatal(err)
				}
				domain, err := protoapi.PendingWorkFromProto(&decoded)
				if err != nil {
					t.Fatal(err)
				}
				got := domain.Items[0]
				if got.Kind != tc.kind || got.ID.String() != item.Id || got.CanonicalInput != tc.canonical {
					t.Fatalf("decoded item = %+v", got)
				}
				roundtrip, err := protoapi.PendingWorkToProto(domain)
				if err != nil {
					t.Fatal(err)
				}
				if err := protoapi.Validate(roundtrip); err != nil {
					t.Fatal(err)
				}
				restored, err := protoapi.PendingWorkFromProto(roundtrip)
				if err != nil || !reflect.DeepEqual(domain, restored) {
					t.Fatalf("roundtrip = %+v, %v; want %+v", restored, err, domain)
				}
			}
			for _, invalid := range []struct {
				name   string
				mutate func(*runtimepb.PendingWorkItem)
			}{
				{"canonical mismatch", func(item *runtimepb.PendingWorkItem) { item.CanonicalInput = "different input" }},
				{"missing payload", func(item *runtimepb.PendingWorkItem) { item.Payload = nil }},
				{"invalid lane", func(item *runtimepb.PendingWorkItem) {
					item.Lane = runtimepb.PendingWorkLane_PENDING_WORK_LANE_UNSPECIFIED
				}},
				{"invalid state", func(item *runtimepb.PendingWorkItem) {
					item.State = runtimepb.PendingWorkItemState_PENDING_WORK_ITEM_STATE_UNSPECIFIED
				}},
				{"invalid identity", func(item *runtimepb.PendingWorkItem) { item.Id = "not-a-uuid" }},
			} {
				t.Run(invalid.name, func(t *testing.T) {
					item := newItem()
					invalid.mutate(item)
					if err := protoapi.Validate(item); err == nil {
						t.Fatal("invalid Pending Work accepted")
					}
				})
			}
		})
	}
}
