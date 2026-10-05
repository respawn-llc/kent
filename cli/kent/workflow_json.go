package main

import (
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/workflowcontract"
)

// These are CLI reports and editable document fields, not the server API.
type workflowRecordJSON struct {
	ID                    runtimeids.WorkflowID             `json:"id"`
	Name                  string                            `json:"name"`
	Description           string                            `json:"description"`
	Version               int64                             `json:"version"`
	ExecutionTargetPolicy workflowExecutionTargetPolicyJSON `json:"execution_target_policy"`
	ProjectLink           *workflowListProjectLinkJSON      `json:"project_link,omitempty"`
}

type workflowExecutionTargetPolicyJSON struct {
	Mode      serverapi.WorkflowExecutionTargetMode `json:"mode"`
	CustomRef *string                               `json:"custom_ref,omitempty"`
}

type workflowListProjectLinkJSON struct {
	Default bool `json:"default"`
}

type workflowDefinitionJSON struct {
	NodeGroups       []workflowNodeGroupJSON       `json:"node_groups,omitempty"`
	Workflow         workflowRecordJSON            `json:"workflow"`
	Nodes            []workflowNodeJSON            `json:"nodes"`
	TransitionGroups []workflowTransitionGroupJSON `json:"transition_groups"`
	Edges            []workflowEdgeJSON            `json:"edges"`
	DerivedWiring    workflowDerivedWiringJSON     `json:"derived_wiring"`
}

type workflowNodeJSON struct {
	workflowGraphDocumentNode
	WorkflowID runtimeids.WorkflowID `json:"workflow_id"`
	GroupKey   string                `json:"group_key,omitempty"`
}

type workflowNodeGroupJSON struct {
	GroupID     string                `json:"group_id"`
	WorkflowID  runtimeids.WorkflowID `json:"workflow_id"`
	GroupKey    string                `json:"group_key"`
	DisplayName string                `json:"display_name"`
	SortOrder   int                   `json:"sort_order"`
}

type workflowTransitionGroupJSON struct {
	workflowGraphDocumentTransition
	WorkflowID runtimeids.WorkflowID `json:"workflow_id"`
}

type workflowEdgeJSON struct {
	workflowGraphDocumentEdge
	WorkflowID         runtimeids.WorkflowID           `json:"workflow_id"`
	InputBindings      []workflowInputBindingJSON      `json:"input_bindings,omitempty"`
	OutputRequirements []workflowOutputRequirementJSON `json:"output_requirements,omitempty"`
}

type workflowGraphDocumentNodeGroup struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
}

type workflowGraphDocumentTransition struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	TransitionID string `json:"transition_id"`
	DisplayName  string `json:"display_name"`
	Description  string `json:"description,omitempty"`
}

type workflowGraphDocumentEdge struct {
	ID                string                    `json:"id"`
	TransitionGroupID string                    `json:"transition_group_id"`
	Key               string                    `json:"key"`
	TargetNodeID      string                    `json:"target_node_id"`
	AssigneeSelection string                    `json:"assignee_selection"`
	ThinkingSelection string                    `json:"thinking_selection"`
	RequiresApproval  bool                      `json:"requires_approval"`
	ContextMode       string                    `json:"context_mode"`
	ContextSource     workflowContextSourceJSON `json:"context_source"`
	PromptTemplate    string                    `json:"prompt_template,omitempty"`
	Parameters        []workflowParameterJSON   `json:"parameters,omitempty"`
}

type workflowContextSourceJSON struct {
	Kind    string `json:"kind"`
	NodeKey string `json:"node_key,omitempty"`
}

type workflowParameterJSON struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Purpose     string `json:"purpose"`
}

type workflowJoinInputProviderJSON struct {
	InputName      string `json:"input_name"`
	ProviderEdgeID string `json:"provider_edge_id"`
}

type workflowInputBindingJSON struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Field  string `json:"field"`
}

type workflowOutputRequirementJSON struct {
	FieldName string `json:"field_name"`
}

type workflowDerivedWiringJSON struct {
	Nodes            []workflowDerivedNodeJSON           `json:"nodes,omitempty"`
	TransitionGroups []workflowDerivedTransitionJSON     `json:"transition_groups,omitempty"`
	Edges            []workflowDerivedEdgeJSON           `json:"edges,omitempty"`
	Diagnostics      []serverapi.WorkflowValidationError `json:"diagnostics,omitempty"`
}

type workflowDerivedNodeJSON struct {
	NodeID                  string                          `json:"node_id"`
	PossibleProvisionFields []serverapi.WorkflowOutputField `json:"possible_provision_fields,omitempty"`
	JoinOutputFields        []serverapi.WorkflowOutputField `json:"join_output_fields,omitempty"`
}

type workflowDerivedTransitionJSON struct {
	TransitionGroupID       string                          `json:"transition_group_id"`
	RequiredProvisionFields []serverapi.WorkflowOutputField `json:"required_provision_fields,omitempty"`
}

type workflowDerivedEdgeJSON struct {
	EdgeID                         string                            `json:"edge_id"`
	InputBindings                  []workflowInputBindingJSON        `json:"input_bindings,omitempty"`
	RequiredProvisionFields        []serverapi.WorkflowOutputField   `json:"required_provision_fields,omitempty"`
	RequiredProviderFields         []serverapi.WorkflowOutputField   `json:"required_provider_fields,omitempty"`
	AssigneeSelectionApplicability workflowSelectorApplicabilityJSON `json:"assignee_selection_applicability"`
	ThinkingSelectionApplicability workflowSelectorApplicabilityJSON `json:"thinking_selection_applicability"`
}

type workflowSelectorApplicabilityJSON struct {
	Available        bool   `json:"available"`
	ParameterVisible bool   `json:"parameter_visible"`
	Reason           string `json:"reason"`
}

type projectWorkflowLinkJSON struct {
	ID         string                `json:"id"`
	ProjectID  string                `json:"project_id"`
	WorkflowID runtimeids.WorkflowID `json:"workflow_id"`
	Default    bool                  `json:"default"`
}

type workflowValidationJSON struct {
	Valid  bool                                `json:"valid"`
	Errors []serverapi.WorkflowValidationError `json:"errors"`
}

type workflowGraphImpactJSON struct {
	RemovedNodeGroupCount             int64                                           `json:"removed_node_group_count"`
	RemovedNodeCount                  int64                                           `json:"removed_node_count"`
	RemovedTransitionGroupCount       int64                                           `json:"removed_transition_group_count"`
	RemovedEdgeCount                  int64                                           `json:"removed_edge_count"`
	RemovedEntities                   []workflowcontract.WorkflowGraphEntityReference `json:"removed_entities"`
	NodeTaskReferenceCount            int64                                           `json:"node_task_reference_count"`
	EdgeTaskReferenceCount            int64                                           `json:"edge_task_reference_count"`
	ActiveCurrentNodeCount            int64                                           `json:"active_current_node_count"`
	PendingApprovalCount              int64                                           `json:"pending_approval_count"`
	StartNodeChangeCount              int64                                           `json:"start_node_change_count"`
	LastTerminalChangeCount           int64                                           `json:"last_terminal_change_count"`
	TaskReferencedNodeKindChangeCount int64                                           `json:"task_referenced_node_kind_change_count"`
}

type workflowGraphBlockerJSON struct {
	Code             string                                          `json:"code"`
	Message          string                                          `json:"message"`
	Count            int64                                           `json:"count"`
	AffectedEntities []workflowcontract.WorkflowGraphEntityReference `json:"affected_entities"`
}

type workflowDeleteJSON struct {
	Deleted  bool                        `json:"deleted"`
	Impact   workflowDeleteImpactJSON    `json:"impact"`
	Blockers []workflowDeleteBlockerJSON `json:"blockers,omitempty"`
}

type workflowDeleteImpactJSON struct {
	WorkflowID                     runtimeids.WorkflowID `json:"workflow_id"`
	Version                        int64                 `json:"version"`
	ProjectCount                   int64                 `json:"project_count"`
	LinkCount                      int64                 `json:"link_count"`
	DefaultReplacementProjectCount int64                 `json:"default_replacement_project_count"`
	TaskCount                      int64                 `json:"task_count"`
	CurrentNodeCount               int64                 `json:"current_node_count"`
	PendingApprovalCount           int64                 `json:"pending_approval_count"`
	BlockedTaskCount               int64                 `json:"blocked_task_count"`
}

type workflowDeleteBlockerJSON struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

type workflowUnlinkJSON struct {
	LinkID   string                      `json:"link_id"`
	Unlinked bool                        `json:"unlinked"`
	Blockers []workflowUnlinkBlockerJSON `json:"blockers,omitempty"`
}

type workflowUnlinkBlockerJSON struct {
	Code    string                   `json:"code"`
	Message string                   `json:"message"`
	Count   int32                    `json:"count,omitempty"`
	Tasks   []workflowUnlinkTaskJSON `json:"tasks,omitempty"`
}

type workflowUnlinkTaskJSON struct {
	TaskID  string `json:"task_id"`
	ShortID string `json:"short_id"`
	Title   string `json:"title,omitempty"`
}

type workflowLabelJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workflowLabelCatalogJSON struct {
	ProjectID string              `json:"project_id"`
	Labels    []workflowLabelJSON `json:"labels"`
}

type workflowLabelAssignmentJSON struct {
	TaskID   string   `json:"task_id"`
	LabelIDs []string `json:"label_ids"`
}
