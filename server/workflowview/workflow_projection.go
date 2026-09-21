package workflowview

import (
	"core/server/workflow"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/textutil"
)

func apiContextSource(in workflow.ContextSource) (*pb.ContextSource, error) {
	source := workflow.CanonicalContextSource(in)
	kind, err := protoapi.WorkflowContextSourceKind.Encode(string(source.Kind))
	if err != nil {
		return nil, err
	}
	return &pb.ContextSource{Kind: kind, NodeKey: textutil.OptionalExactString(string(source.NodeKey))}, nil
}
