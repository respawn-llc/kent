package serverapi

import (
	"testing"

	"buf.build/go/protovalidate"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"
)

func TestWorkflowDerivedEdgeWiringRejectsUnknownApplicabilityFacts(t *testing.T) {
	edge := &pb.DerivedEdgeWiring{
		EdgeId: runtimeids.NewGraphEntityID(),
		AssigneeSelectionApplicability: &pb.SelectorApplicability{
			Available:        true,
			ParameterVisible: true,
			Reason:           999,
		},
		ThinkingSelectionApplicability: &pb.SelectorApplicability{
			Available:        true,
			ParameterVisible: true,
			Reason:           pb.SelectorApplicabilityReason_WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE,
		},
	}
	if err := protovalidate.Validate(edge); err == nil {
		t.Fatal("unknown selector applicability reason was accepted")
	}
}
