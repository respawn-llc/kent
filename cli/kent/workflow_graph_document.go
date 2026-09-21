package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"core/shared/jsoncontract"
	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"

	invjsonschema "github.com/invopop/jsonschema"
	"google.golang.org/protobuf/proto"
)

type workflowGraphDocument struct {
	WorkflowID      runtimeids.WorkflowID      `json:"workflow_id"`
	ExpectedVersion int64                      `json:"expected_version" jsonschema:"minimum=0"`
	Graph           workflowGraphDocumentGraph `json:"graph"`
}

type workflowGraphDocumentGraph struct {
	NodeGroups       []workflowGraphDocumentNodeGroup  `json:"node_groups"`
	Nodes            []workflowGraphDocumentNode       `json:"nodes"`
	TransitionGroups []workflowGraphDocumentTransition `json:"transition_groups"`
	Edges            []workflowGraphDocumentEdge       `json:"edges"`
}

type workflowGraphDocumentNode struct {
	ID                 string                          `json:"id"`
	Key                string                          `json:"key"`
	Kind               string                          `json:"kind"`
	DisplayName        string                          `json:"display_name"`
	GroupID            *string                         `json:"group_id" jsonschema:"nullable"`
	SubagentRole       string                          `json:"subagent_role,omitempty"`
	CompletionMode     string                          `json:"completion_mode,omitempty"`
	ScriptPath         *string                         `json:"script_path,omitempty" jsonschema:"nullable"`
	JoinInputProviders []workflowJoinInputProviderJSON `json:"join_input_providers,omitempty"`
}

func allowWorkflowGraphUnknownFields(schema *invjsonschema.Schema) {
	schema.AdditionalProperties = invjsonschema.TrueSchema
}

type workflowGraphDocumentContractSource struct{}

func (workflowGraphDocumentContractSource) JSONSchema() *invjsonschema.Schema {
	schema := (&invjsonschema.Reflector{
		Anonymous:      true,
		DoNotReference: true,
	}).Reflect(workflowGraphDocument{})
	allowWorkflowGraphUnknownFields(schema)
	schema.Properties.Set("workflow_id", &invjsonschema.Schema{Type: "string"})
	graph := workflowGraphSchemaProperty(schema, "graph")
	allowWorkflowGraphUnknownFields(graph)
	for _, collection := range []string{"node_groups", "nodes", "transition_groups", "edges"} {
		allowWorkflowGraphUnknownFields(workflowGraphSchemaArrayItems(graph, collection))
	}
	node := workflowGraphSchemaArrayItems(graph, "nodes")
	node.PropertyNames = &invjsonschema.Schema{
		Not: &invjsonschema.Schema{Const: "group_key"},
	}
	allowWorkflowGraphUnknownFields(workflowGraphSchemaArrayItems(node, "join_input_providers"))
	edge := workflowGraphSchemaArrayItems(graph, "edges")
	allowWorkflowGraphUnknownFields(workflowGraphSchemaProperty(edge, "context_source"))
	allowWorkflowGraphUnknownFields(workflowGraphSchemaArrayItems(edge, "parameters"))
	return schema
}

func workflowGraphSchemaProperty(schema *invjsonschema.Schema, name string) *invjsonschema.Schema {
	property, found := schema.Properties.Get(name)
	if !found {
		panic(fmt.Sprintf("workflow graph schema is missing property %q", name))
	}
	return property
}

func workflowGraphSchemaArrayItems(schema *invjsonschema.Schema, name string) *invjsonschema.Schema {
	property := workflowGraphSchemaProperty(schema, name)
	if property.Items == nil {
		panic(fmt.Sprintf("workflow graph schema property %q has no item schema", name))
	}
	return property.Items
}

type workflowGraphDocumentContract struct {
	schema jsoncontract.Internal
}

func prepareWorkflowGraphDocumentContract() (workflowGraphDocumentContract, error) {
	schema, err := jsoncontract.NewPreparer(false).Internal(
		"Workflow graph CLI document",
		workflowGraphDocumentContractSource{},
	)
	if err != nil {
		return workflowGraphDocumentContract{}, err
	}
	return workflowGraphDocumentContract{schema: schema}, nil
}

func workflowGraphDocumentFromDefinition(definition *pb.WorkflowDefinition) (workflowGraphDocument, error) {
	id, err := runtimeids.ParseWorkflowID(definition.Workflow.Id)
	if err != nil {
		return workflowGraphDocument{}, err
	}
	return workflowGraphDocumentFromDraft(
		id,
		definition.Workflow.Version,
		protoapi.WorkflowGraphDraftFromDefinition(definition),
	)
}

func workflowGraphDocumentFromDraft(
	workflowID runtimeids.WorkflowID,
	expectedVersion int64,
	graph *pb.GraphDraft,
) (workflowGraphDocument, error) {
	document := workflowGraphDocument{
		WorkflowID:      workflowID,
		ExpectedVersion: expectedVersion,
		Graph: workflowGraphDocumentGraph{
			NodeGroups:       make([]workflowGraphDocumentNodeGroup, 0, len(graph.NodeGroups)),
			Nodes:            make([]workflowGraphDocumentNode, 0, len(graph.Nodes)),
			TransitionGroups: make([]workflowGraphDocumentTransition, 0, len(graph.TransitionGroups)),
			Edges:            make([]workflowGraphDocumentEdge, 0, len(graph.Edges)),
		},
	}
	for _, group := range graph.NodeGroups {
		document.Graph.NodeGroups = append(document.Graph.NodeGroups, workflowGraphDocumentNodeGroup{
			ID: group.Id, Key: group.Key, DisplayName: group.DisplayName,
		})
	}
	for _, node := range graph.Nodes {
		value, err := workflowDocumentNode(node)
		if err != nil {
			return workflowGraphDocument{}, err
		}
		document.Graph.Nodes = append(document.Graph.Nodes, value)
	}
	for _, group := range graph.TransitionGroups {
		document.Graph.TransitionGroups = append(document.Graph.TransitionGroups, workflowGraphDocumentTransition{
			ID: group.Id, SourceNodeID: group.SourceNodeId, TransitionID: group.TransitionId,
			DisplayName: group.DisplayName, Description: group.Description,
		})
	}
	for _, edge := range graph.Edges {
		value, err := workflowDocumentEdge(edge)
		if err != nil {
			return workflowGraphDocument{}, err
		}
		document.Graph.Edges = append(document.Graph.Edges, value)
	}
	return document, nil
}

func (c workflowGraphDocumentContract) Decode(data []byte) (workflowGraphDocument, error) {
	if err := c.schema.Validate(data); err != nil {
		return workflowGraphDocument{}, err
	}
	var document workflowGraphDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return workflowGraphDocument{}, fmt.Errorf("decode Workflow graph document: %w", err)
	}
	return document, nil
}

func (d workflowGraphDocument) WorkflowGraphDraft() (*pb.GraphDraft, error) {
	graph := &pb.GraphDraft{}
	for _, group := range d.Graph.NodeGroups {
		graph.NodeGroups = append(graph.NodeGroups, &pb.GraphDraftNodeGroup{Id: group.ID, Key: group.Key, DisplayName: group.DisplayName})
	}
	for _, node := range d.Graph.Nodes {
		kind, err := protoapi.WorkflowNodeKind.Encode(strings.TrimSpace(node.Kind))
		if err != nil {
			return nil, err
		}
		projected := &pb.GraphDraftNode{
			Id: node.ID, Key: node.Key, Kind: kind, DisplayName: node.DisplayName,
			GroupId: node.GroupID, ScriptPath: node.ScriptPath,
		}
		if node.SubagentRole != "" {
			projected.SubagentRole = proto.String(node.SubagentRole)
		}
		if node.CompletionMode != "" {
			mode, err := protoapi.WorkflowCompletionMode.Encode(strings.TrimSpace(node.CompletionMode))
			if err != nil {
				return nil, err
			}
			projected.CompletionMode = &mode
		}
		for _, provider := range node.JoinInputProviders {
			projected.JoinInputProviders = append(projected.JoinInputProviders, &pb.DraftJoinInputProvider{
				InputName: provider.InputName, ProviderEdgeId: provider.ProviderEdgeID,
			})
		}
		graph.Nodes = append(graph.Nodes, projected)
	}
	for _, group := range d.Graph.TransitionGroups {
		graph.TransitionGroups = append(graph.TransitionGroups, &pb.GraphDraftTransitionGroup{
			Id: group.ID, SourceNodeId: group.SourceNodeID, TransitionId: group.TransitionID,
			DisplayName: group.DisplayName, Description: group.Description,
		})
	}
	for _, edge := range d.Graph.Edges {
		value, err := workflowDocumentEdgeToProto(edge)
		if err != nil {
			return nil, err
		}
		graph.Edges = append(graph.Edges, value)
	}
	if err := protoapi.Validate(&pb.GraphValidateDraftRequest{
		WorkflowId: d.WorkflowID.String(),
		Graph:      graph,
		Modes:      []pb.ValidationMode{pb.ValidationMode_WORKFLOW_VALIDATION_MODE_DRAFT},
	}); err != nil {
		return nil, err
	}
	return graph, nil
}
