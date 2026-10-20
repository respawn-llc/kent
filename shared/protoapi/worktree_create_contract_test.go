package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/worktree"
)

func TestWorktreeCreateSuccessRequiresRegisteredTopology(t *testing.T) {
	git := &pb.GitFacts{CanonicalRoot: "/repo/feature", HeadObject: "abc123"}
	for _, registered := range []bool{true, false} {
		topology := &pb.TopologyEntry{Topology: &pb.TopologyEntry_MainWorkspace{
			MainWorkspace: &pb.MainWorkspaceFacts{Git: git},
		}}
		if registered {
			topology.Topology = &pb.TopologyEntry_Registered{Registered: &pb.RegisteredFacts{
				Git: git, Kent: &pb.KentFacts{
					WorktreeId: "worktree-1", CanonicalRoot: "/repo/feature", DisplayName: "feature",
				},
			}}
		}
		result := &pb.CreateSuccess{Worktree: &pb.ListEntry{
			Topology: topology, Projection: &pb.ListProjection{Selector: "feature"},
		}}
		if err := protoapi.Validate(result); (err == nil) != registered {
			t.Errorf("registered=%v acceptance = %v", registered, err)
		}
	}
}
