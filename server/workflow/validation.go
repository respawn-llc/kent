package workflow

import (
	"fmt"
	"strings"
	"text/template"
	"text/template/parse"

	"builder/shared/workflowkey"
)

var reservedOutputFieldNames = map[string]bool{
	"commentary":    true,
	"transition_id": true,
}

func ValidateDefinition(def Definition, opts ValidationOptions) ValidationResult {
	context := opts.Context
	if context == "" {
		context = ValidationContextDraft
	}
	state := newValidationState(def, opts, context)
	state.validateShape()
	state.validateGraph()
	state.validateFanouts()
	return ValidationResult{Context: context, Errors: state.errors}
}

type validationState struct {
	def            Definition
	opts           ValidationOptions
	context        ValidationContext
	errors         []ValidationError
	nodesByID      map[NodeID]Node
	nodeKeys       map[ModelKey]NodeID
	groupsByID     map[TransitionGroupID]TransitionGroup
	edgesByID      map[EdgeID]Edge
	edgesByGroup   map[TransitionGroupID][]Edge
	groupsBySource map[NodeID][]TransitionGroup
	outgoingByNode map[NodeID][]Edge
	incomingByNode map[NodeID][]Edge
	startNodes     []Node
}

func newValidationState(def Definition, opts ValidationOptions, context ValidationContext) *validationState {
	return &validationState{
		def:            def,
		opts:           opts,
		context:        context,
		nodesByID:      map[NodeID]Node{},
		nodeKeys:       map[ModelKey]NodeID{},
		groupsByID:     map[TransitionGroupID]TransitionGroup{},
		edgesByID:      map[EdgeID]Edge{},
		edgesByGroup:   map[TransitionGroupID][]Edge{},
		groupsBySource: map[NodeID][]TransitionGroup{},
		outgoingByNode: map[NodeID][]Edge{},
		incomingByNode: map[NodeID][]Edge{},
	}
}

func (s *validationState) validateShape() {
	if strings.TrimSpace(string(s.def.ID)) == "" {
		s.addHard(CodeMissingWorkflowID, "workflow id is required", ValidationError{})
	}
	if !validDisplayName(s.def.DisplayName) {
		s.addHard(CodeInvalidDisplayName, "workflow display name must be non-empty and at most 120 characters", ValidationError{WorkflowID: s.def.ID})
	}

	s.indexNodes()
	s.indexTransitionGroups()
	s.indexEdges()
	s.validateNodes()
	s.validateTransitionGroups()
	s.validateEdges()
}

func (s *validationState) indexNodes() {
	for _, node := range s.def.Nodes {
		ref := ValidationError{WorkflowID: s.def.ID, NodeID: node.ID}
		if strings.TrimSpace(string(node.WorkflowID)) != "" && node.WorkflowID != s.def.ID {
			s.addHard(CodeCrossWorkflowReference, "node references another workflow", ref)
		}
		if strings.TrimSpace(string(node.ID)) == "" {
			s.addHard(CodeMissingNodeID, "node id is required", ref)
			continue
		}
		if _, exists := s.nodesByID[node.ID]; exists {
			s.addHard(CodeDuplicateNodeID, "node id must be unique", ref)
			continue
		}
		s.nodesByID[node.ID] = node
		if node.Kind == NodeKindStart {
			s.startNodes = append(s.startNodes, node)
		}
	}
}

func (s *validationState) indexTransitionGroups() {
	seenTransitionBySource := map[NodeID]map[string]TransitionGroupID{}
	for _, group := range s.def.TransitionGroups {
		ref := ValidationError{WorkflowID: s.def.ID, TransitionGroupID: group.ID, NodeID: group.SourceNodeID}
		if strings.TrimSpace(string(group.WorkflowID)) != "" && group.WorkflowID != s.def.ID {
			s.addHard(CodeCrossWorkflowReference, "transition group references another workflow", ref)
		}
		if strings.TrimSpace(string(group.ID)) == "" {
			s.addHard(CodeMissingTransitionGroupID, "transition group id is required", ref)
			continue
		}
		if _, exists := s.groupsByID[group.ID]; exists {
			s.addHard(CodeDuplicateTransitionGroupID, "transition group id must be unique", ref)
			continue
		}
		s.groupsByID[group.ID] = group
		s.groupsBySource[group.SourceNodeID] = append(s.groupsBySource[group.SourceNodeID], group)
		transitionID := strings.TrimSpace(string(group.TransitionID))
		if transitionID == "" {
			s.addHard(CodeMissingTransitionID, "transition id is required", ref)
		} else if !validModelKey(transitionID) {
			s.addHard(CodeInvalidTransitionID, "transition id must "+workflowkey.Description, ref)
		} else {
			bySource := seenTransitionBySource[group.SourceNodeID]
			if bySource == nil {
				bySource = map[string]TransitionGroupID{}
				seenTransitionBySource[group.SourceNodeID] = bySource
			}
			if _, exists := bySource[transitionID]; exists {
				s.addHard(CodeDuplicateTransitionID, "transition id must be unique per source node", ref)
			}
			bySource[transitionID] = group.ID
		}
		if !validDisplayName(group.DisplayName) {
			s.addHard(CodeInvalidDisplayName, "transition group display name must be non-empty and at most 120 characters", ref)
		}
	}
}

func (s *validationState) indexEdges() {
	for _, edge := range s.def.Edges {
		ref := ValidationError{WorkflowID: s.def.ID, EdgeID: edge.ID, TransitionGroupID: edge.TransitionGroupID}
		if strings.TrimSpace(string(edge.WorkflowID)) != "" && edge.WorkflowID != s.def.ID {
			s.addHard(CodeCrossWorkflowReference, "edge references another workflow", ref)
		}
		if strings.TrimSpace(string(edge.ID)) == "" {
			s.addHard(CodeMissingEdgeID, "edge id is required", ref)
			continue
		}
		if _, exists := s.edgesByID[edge.ID]; exists {
			s.addHard(CodeDuplicateEdgeID, "edge id must be unique", ref)
			continue
		}
		s.edgesByID[edge.ID] = edge
		s.edgesByGroup[edge.TransitionGroupID] = append(s.edgesByGroup[edge.TransitionGroupID], edge)
		group, exists := s.groupsByID[edge.TransitionGroupID]
		if exists {
			s.outgoingByNode[group.SourceNodeID] = append(s.outgoingByNode[group.SourceNodeID], edge)
			s.incomingByNode[edge.TargetNodeID] = append(s.incomingByNode[edge.TargetNodeID], edge)
		}
	}
}

func (s *validationState) validateNodes() {
	for _, node := range s.def.Nodes {
		ref := ValidationError{WorkflowID: s.def.ID, NodeID: node.ID}
		if strings.TrimSpace(string(node.Key)) == "" {
			s.addHard(CodeMissingNodeKey, "node key is required", ref)
		} else if !validModelKey(string(node.Key)) {
			s.addHard(CodeInvalidNodeKey, "node key must "+workflowkey.Description, ref)
		} else if previousNodeID, exists := s.nodeKeys[node.Key]; exists && previousNodeID != node.ID {
			s.addHard(CodeDuplicateNodeKey, "node key must be unique", ref)
		} else {
			s.nodeKeys[node.Key] = node.ID
		}
		if !validDisplayName(node.DisplayName) {
			s.addHard(CodeInvalidDisplayName, "node display name must be non-empty and at most 120 characters", ref)
		}
		switch node.Kind {
		case NodeKindStart, NodeKindAgent, NodeKindJoin, NodeKindTerminal:
		default:
			s.addHard(CodeInvalidNodeKind, "node kind is invalid", ref)
		}
		s.validateOutputFields(node)
	}
	if len(s.startNodes) == 0 {
		s.addHard(CodeMissingStartNode, "workflow must contain exactly one start node", ValidationError{WorkflowID: s.def.ID})
	}
	if len(s.startNodes) > 1 {
		s.addHard(CodeMultipleStartNodes, "workflow must contain exactly one start node", ValidationError{WorkflowID: s.def.ID})
	}
}

func (s *validationState) validateTransitionGroups() {
	for _, group := range s.def.TransitionGroups {
		ref := ValidationError{WorkflowID: s.def.ID, TransitionGroupID: group.ID, NodeID: group.SourceNodeID}
		if strings.TrimSpace(string(group.SourceNodeID)) == "" {
			s.addHard(CodeEdgeTransitionGroupMissing, "transition group source node is required", ref)
		} else if _, exists := s.nodesByID[group.SourceNodeID]; !exists {
			s.addHard(CodeEdgeTransitionGroupMissing, "transition group source node must exist", ref)
		}
		if len(s.edgesByGroup[group.ID]) == 0 {
			s.addHard(CodeEmptyTransitionGroup, "transition group must contain at least one edge", ref)
		}
		seenEdgeKeys := map[ModelKey]EdgeID{}
		for _, edge := range s.edgesByGroup[group.ID] {
			if strings.TrimSpace(string(edge.Key)) == "" || !validModelKey(string(edge.Key)) {
				continue
			}
			if previousID, exists := seenEdgeKeys[edge.Key]; exists && previousID != edge.ID {
				s.addHard(CodeDuplicateEdgeKey, "edge key must be unique per transition group", ValidationError{WorkflowID: s.def.ID, EdgeID: edge.ID, TransitionGroupID: edge.TransitionGroupID})
			}
			seenEdgeKeys[edge.Key] = edge.ID
		}
	}
}

func (s *validationState) validateEdges() {
	for _, edge := range s.def.Edges {
		ref := ValidationError{WorkflowID: s.def.ID, EdgeID: edge.ID, TransitionGroupID: edge.TransitionGroupID}
		group, groupExists := s.groupsByID[edge.TransitionGroupID]
		if !groupExists {
			s.addHard(CodeEdgeTransitionGroupMissing, "edge transition group must exist", ref)
		}
		if strings.TrimSpace(string(edge.Key)) == "" {
			s.addHard(CodeMissingEdgeKey, "edge key is required", ref)
		} else if !validModelKey(string(edge.Key)) {
			s.addHard(CodeInvalidEdgeKey, "edge key must "+workflowkey.Description, ref)
		}
		if strings.TrimSpace(string(edge.TargetNodeID)) == "" {
			s.addHard(CodeEdgeTargetMissing, "edge target node is required", ref)
		} else if _, exists := s.nodesByID[edge.TargetNodeID]; !exists {
			s.addHard(CodeEdgeTargetMissing, "edge target node must exist", ref)
		}
		if !validContextMode(edge.ContextMode) {
			s.addHard(CodeInvalidContextMode, "edge context mode is invalid", ref)
		}
		if groupExists {
			source := s.nodesByID[group.SourceNodeID]
			s.validateOutputRequirements(source, edge)
			s.validateInputBindings(source, edge)
		}
	}
}

func (s *validationState) validateOutputFields(node Node) {
	seen := map[string]bool{}
	for _, field := range node.OutputFields {
		ref := ValidationError{WorkflowID: s.def.ID, NodeID: node.ID}
		name := strings.TrimSpace(field.Name)
		if name == "" || !validModelKey(name) || len(name) > MaxOutputFieldNameChars || reservedOutputFieldNames[name] {
			s.addHard(CodeInvalidOutputField, "output field name is invalid", ref)
		}
		if seen[name] {
			s.addHard(CodeDuplicateOutputField, "output field name must be unique per node", ref)
		}
		seen[name] = true
		description := strings.TrimSpace(field.Description)
		if description == "" {
			s.addHard(CodeOutputFieldDescriptionRequired, "output field description is required", ref)
		} else if len(description) > MaxOutputFieldDescriptionChars {
			s.addHard(CodeOutputSchemaTooLarge, "output field description is too large", ref)
		}
	}
}

func (s *validationState) validateOutputRequirements(source Node, edge Edge) {
	known := nodeOutputFieldSet(source)
	for _, req := range edge.OutputRequirements {
		field := strings.TrimSpace(req.FieldName)
		if field == "" || !known[field] {
			s.addHard(CodeUnknownOutputRequirement, "output requirement references an unknown source output field", ValidationError{WorkflowID: s.def.ID, NodeID: source.ID, EdgeID: edge.ID})
		}
	}
}

func (s *validationState) validateInputBindings(source Node, edge Edge) {
	seen := map[string]bool{}
	outputFields := nodeOutputFieldSet(source)
	outputFields["commentary"] = true
	for _, binding := range edge.InputBindings {
		ref := ValidationError{WorkflowID: s.def.ID, NodeID: source.ID, EdgeID: edge.ID}
		name := strings.TrimSpace(binding.Name)
		if name == "" || !validModelKey(name) || len(name) > MaxOutputFieldNameChars || seen[name] {
			s.addHard(CodeInvalidInputBinding, "input binding name is invalid or duplicated", ref)
			continue
		}
		seen[name] = true
		field := strings.TrimSpace(binding.Field)
		switch binding.Source {
		case BindingSourceTask:
			if !validTaskBindingField(field) {
				s.addHard(CodeInvalidInputBinding, "task input binding references unknown field", ref)
			}
		case BindingSourceTransitionOutput:
			if !outputFields[field] {
				s.addHard(CodeInvalidInputBinding, "transition output binding references unknown field", ref)
			}
		case BindingSourceJoin:
			if field == "" || !validModelKey(field) {
				s.addHard(CodeInvalidInputBinding, "join input binding field is invalid", ref)
			}
		default:
			s.addHard(CodeInvalidInputBinding, "input binding source is invalid", ref)
		}
	}
}

func (s *validationState) validateGraph() {
	s.validateKindConstraints()
	s.validateRuntimeSupport()
	if len(s.startNodes) != 1 {
		return
	}
	s.validateStartOutgoingShape()
	reachable := s.reachableFrom(s.startNodes[0].ID)
	for nodeID, node := range s.nodesByID {
		if !reachable[nodeID] {
			s.addSemantic(CodeNodeUnreachableFromStart, "node is not reachable from start", ValidationError{WorkflowID: s.def.ID, NodeID: node.ID})
		}
		if node.Kind != NodeKindTerminal && !s.canReachTerminal(nodeID) {
			s.addSemantic(CodeNonTerminalCannotReachTerminal, "non-terminal node cannot reach a terminal node", ValidationError{WorkflowID: s.def.ID, NodeID: node.ID})
		}
	}
	s.validatePromptPlaceholders()
}

func (s *validationState) validateKindConstraints() {
	for _, node := range s.def.Nodes {
		ref := ValidationError{WorkflowID: s.def.ID, NodeID: node.ID}
		incoming := len(s.incomingByNode[node.ID])
		outgoingGroups := s.groupsBySource[node.ID]
		switch node.Kind {
		case NodeKindStart:
			if strings.TrimSpace(node.SubagentRole) != "" || strings.TrimSpace(node.PromptTemplate) != "" || len(node.OutputFields) > 0 || incoming > 0 {
				s.addHard(CodeInvalidStartNode, "start node must be non-executable and have no inputs", ref)
			}
		case NodeKindAgent:
			if strings.TrimSpace(node.SubagentRole) == "" {
				s.addSemantic(CodeAgentRoleRequired, "agent node requires a subagent role", ref)
			} else if !IsDefaultAgentRole(node.SubagentRole) && (s.opts.RoleResolver == nil || !s.opts.RoleResolver.RoleExists(strings.TrimSpace(node.SubagentRole))) {
				s.addSemantic(CodeAgentRoleMissing, "agent node references a missing subagent role", ref)
			}
		case NodeKindJoin:
			if strings.TrimSpace(node.SubagentRole) != "" || strings.TrimSpace(node.PromptTemplate) != "" {
				s.addHard(CodeJoinIsExecutable, "join node must be non-executable", ref)
			}
			if incoming < 2 {
				s.addHard(CodeInvalidJoinNode, "join node must have at least two incoming branches", ref)
			}
			if len(node.OutputFields) > 0 {
				s.addHard(CodeInvalidJoinNode, "join node cannot define agent output fields", ref)
			}
			if len(outgoingGroups) != 1 {
				s.addSemantic(CodeInvalidJoinOutgoingShape, "join node must have exactly one outgoing transition group", ref)
			}
		case NodeKindTerminal:
			if strings.TrimSpace(node.SubagentRole) != "" || strings.TrimSpace(node.PromptTemplate) != "" {
				s.addHard(CodeTerminalIsExecutable, "terminal node must be non-executable", ref)
			}
			if len(outgoingGroups) > 0 {
				s.addHard(CodeTerminalHasOutgoingEdge, "terminal node cannot have outgoing edges", ref)
			}
			if len(node.OutputFields) > 0 {
				s.addHard(CodeTerminalIsExecutable, "terminal node cannot define agent output fields", ref)
			}
		}
	}
}

func (s *validationState) validateRuntimeSupport() {
	for _, edge := range s.def.Edges {
		ref := ValidationError{WorkflowID: s.def.ID, EdgeID: edge.ID, TransitionGroupID: edge.TransitionGroupID}
		targetKind := NodeKind("")
		target := Node{}
		targetExists := false
		if resolvedTarget, exists := s.nodesByID[edge.TargetNodeID]; exists {
			target = resolvedTarget
			targetKind = target.Kind
			targetExists = true
		}
		source := Node{}
		sourceExists := false
		if group, groupExists := s.groupsByID[edge.TransitionGroupID]; groupExists {
			source, sourceExists = s.nodesByID[group.SourceNodeID]
		}
		contextSource, contextSourceValid := s.validateContextSource(edge, source, sourceExists, target, targetExists, ref)
		if edge.ContextMode == ContextModeContinueSession && contextSourceValid {
			selectedSource, ok := s.contextSourceNode(contextSource, source, sourceExists)
			if ok && targetExists && selectedSource.Kind == NodeKindAgent && target.Kind == NodeKindAgent && strings.TrimSpace(selectedSource.SubagentRole) != strings.TrimSpace(target.SubagentRole) {
				s.addSemantic(CodeInvalidContinueSessionRole, "continue_session requires source and target agent nodes to use the same subagent role", ref)
			}
		}
		for _, issue := range UnsupportedRuntimeFeatures(RuntimeSupportEdge{ContextMode: edge.ContextMode, RequiresApproval: edge.RequiresApproval, TargetKind: targetKind, InputBindings: edge.InputBindings}) {
			s.addSemantic(issue.Code, issue.Message, ref)
		}
	}
}

func (s *validationState) validateContextSource(edge Edge, source Node, sourceExists bool, target Node, targetExists bool, ref ValidationError) (ContextSource, bool) {
	contextSource := CanonicalContextSource(edge.ContextSource)
	switch contextSource.Kind {
	case ContextSourceImmediateSource:
		if edge.ContextMode != ContextModeNewSession && sourceExists && source.Kind != NodeKindAgent {
			s.addSemantic(CodeInvalidContextSource, "immediate context source for continuation must be an agent node", ref)
			return contextSource, false
		}
		return contextSource, true
	case ContextSourceSelectedNode:
		if edge.ContextMode == ContextModeNewSession {
			s.addSemantic(CodeInvalidContextSource, "selected context source requires a continuation context mode", ref)
			return contextSource, false
		}
		nodeKey := strings.TrimSpace(string(contextSource.NodeKey))
		if nodeKey == "" || !validModelKey(nodeKey) {
			s.addSemantic(CodeInvalidContextSource, "selected context source node key is invalid", ref)
			return contextSource, false
		}
		selectedID, exists := s.nodeKeys[contextSource.NodeKey]
		if !exists {
			s.addSemantic(CodeInvalidContextSource, "selected context source node does not exist", ref)
			return contextSource, false
		}
		selected := s.nodesByID[selectedID]
		if selected.Kind != NodeKindAgent {
			s.addSemantic(CodeInvalidContextSource, "selected context source must be an agent node", ref)
			return contextSource, false
		}
		if targetExists && selected.ID == target.ID {
			s.addSemantic(CodeInvalidContextSource, "selected context source cannot be the edge target node", ref)
			return contextSource, false
		}
		if !sourceExists || len(s.startNodes) != 1 {
			return contextSource, true
		}
		if selected.ID != source.ID && !s.nodeDominates(selected.ID, source.ID) {
			s.addSemantic(CodeInvalidContextSource, "selected context source must be guaranteed before the edge source", ref)
			return contextSource, false
		}
		return contextSource, true
	default:
		s.addSemantic(CodeInvalidContextSource, "context source kind is invalid", ref)
		return contextSource, false
	}
}

func (s *validationState) contextSourceNode(contextSource ContextSource, immediate Node, immediateExists bool) (Node, bool) {
	switch contextSource.Kind {
	case ContextSourceImmediateSource:
		return immediate, immediateExists
	case ContextSourceSelectedNode:
		nodeID, exists := s.nodeKeys[contextSource.NodeKey]
		if !exists {
			return Node{}, false
		}
		node, exists := s.nodesByID[nodeID]
		return node, exists
	default:
		return Node{}, false
	}
}

func (s *validationState) validateStartOutgoingShape() {
	start := s.startNodes[0]
	groups := s.groupsBySource[start.ID]
	if s.context == ValidationContextDraft {
		return
	}
	if len(groups) != 1 {
		s.addSemantic(CodeInvalidStartOutgoingShape, "task start requires exactly one outgoing transition group", ValidationError{WorkflowID: s.def.ID, NodeID: start.ID})
		return
	}
	edges := s.edgesByGroup[groups[0].ID]
	if len(edges) != 1 {
		s.addSemantic(CodeInvalidStartOutgoingShape, "task start transition group requires exactly one edge", ValidationError{WorkflowID: s.def.ID, NodeID: start.ID, TransitionGroupID: groups[0].ID})
		return
	}
	target, exists := s.nodesByID[edges[0].TargetNodeID]
	if !exists || target.Kind != NodeKindAgent {
		s.addSemantic(CodeInvalidStartOutgoingShape, "task start edge must target an agent node", ValidationError{WorkflowID: s.def.ID, NodeID: start.ID, EdgeID: edges[0].ID})
	}
}

func (s *validationState) validatePromptPlaceholders() {
	for _, edge := range s.def.Edges {
		target, exists := s.nodesByID[edge.TargetNodeID]
		if !exists {
			continue
		}
		bindings := map[string]bool{}
		for _, binding := range edge.InputBindings {
			bindings[strings.TrimSpace(binding.Name)] = true
		}
		placeholders, err := templatePlaceholders(target.PromptTemplate)
		if err != nil {
			s.addSemantic(CodeInvalidTemplatePlaceholder, "prompt template syntax is invalid", ValidationError{WorkflowID: s.def.ID, NodeID: target.ID, EdgeID: edge.ID})
			continue
		}
		for _, placeholder := range placeholders {
			if !validModelKey(placeholder) || !bindings[placeholder] {
				s.addSemantic(CodeInvalidTemplatePlaceholder, "prompt template references an unknown input binding", ValidationError{WorkflowID: s.def.ID, NodeID: target.ID, EdgeID: edge.ID})
			}
		}
	}
}

func (s *validationState) nodeDominates(candidate NodeID, target NodeID) bool {
	if candidate == target {
		return true
	}
	if len(s.startNodes) != 1 {
		return false
	}
	reachableWithoutCandidate := s.reachableFromSkipping(s.startNodes[0].ID, candidate)
	return !reachableWithoutCandidate[target]
}

func (s *validationState) reachableFromSkipping(start NodeID, skip NodeID) map[NodeID]bool {
	visited := map[NodeID]bool{}
	if start == skip {
		return visited
	}
	stack := []NodeID{start}
	for len(stack) > 0 {
		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[nodeID] || nodeID == skip {
			continue
		}
		visited[nodeID] = true
		for _, edge := range s.outgoingByNode[nodeID] {
			if !visited[edge.TargetNodeID] && edge.TargetNodeID != skip {
				stack = append(stack, edge.TargetNodeID)
			}
		}
	}
	return visited
}

func (s *validationState) reachableFrom(start NodeID) map[NodeID]bool {
	visited := map[NodeID]bool{}
	stack := []NodeID{start}
	for len(stack) > 0 {
		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true
		for _, edge := range s.outgoingByNode[nodeID] {
			if !visited[edge.TargetNodeID] {
				stack = append(stack, edge.TargetNodeID)
			}
		}
	}
	return visited
}

func (s *validationState) canReachTerminal(start NodeID) bool {
	visited := map[NodeID]bool{}
	stack := []NodeID{start}
	for len(stack) > 0 {
		nodeID := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true
		if s.nodesByID[nodeID].Kind == NodeKindTerminal {
			return true
		}
		for _, edge := range s.outgoingByNode[nodeID] {
			if !visited[edge.TargetNodeID] {
				stack = append(stack, edge.TargetNodeID)
			}
		}
	}
	return false
}

func (s *validationState) validateFanouts() {
	for _, group := range s.def.TransitionGroups {
		edges := s.edgesByGroup[group.ID]
		if len(edges) <= 1 {
			continue
		}
		if !s.fanoutHasValidJoin(group, edges) {
			s.addSemantic(CodeInvalidFanoutJoinTopology, "fan-out transition group must have one unambiguous nearest common join without terminal, nested fan-out, or cycle before it", ValidationError{WorkflowID: s.def.ID, TransitionGroupID: group.ID, NodeID: group.SourceNodeID})
		}
	}
}

func (s *validationState) fanoutHasValidJoin(group TransitionGroup, edges []Edge) bool {
	branchJoinDistances := make([]map[NodeID]int, 0, len(edges))
	for _, edge := range edges {
		distances, ok := s.branchJoinDistances(edge.TargetNodeID)
		if !ok || len(distances) == 0 {
			return false
		}
		branchJoinDistances = append(branchJoinDistances, distances)
	}
	common := map[NodeID]int{}
	for joinID, distance := range branchJoinDistances[0] {
		common[joinID] = distance
	}
	for _, distances := range branchJoinDistances[1:] {
		for joinID := range common {
			distance, exists := distances[joinID]
			if !exists {
				delete(common, joinID)
				continue
			}
			common[joinID] += distance
		}
	}
	if len(common) == 0 {
		return false
	}
	nearestDistance := 0
	var nearestJoinID NodeID
	nearestCount := 0
	for joinID, distance := range common {
		if nearestCount == 0 || distance < nearestDistance {
			nearestDistance = distance
			nearestJoinID = joinID
			nearestCount = 1
			continue
		}
		if distance == nearestDistance {
			nearestCount++
		}
	}
	return nearestCount == 1 && nearestJoinID != group.SourceNodeID
}

func (s *validationState) branchJoinDistances(start NodeID) (map[NodeID]int, bool) {
	type frame struct {
		nodeID   NodeID
		distance int
		path     map[NodeID]bool
	}
	distances := map[NodeID]int{}
	stack := []frame{{nodeID: start, distance: 0, path: map[NodeID]bool{}}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current.path[current.nodeID] {
			return nil, false
		}
		node, exists := s.nodesByID[current.nodeID]
		if !exists {
			return nil, false
		}
		if node.Kind == NodeKindJoin {
			previous, exists := distances[current.nodeID]
			if !exists || current.distance < previous {
				distances[current.nodeID] = current.distance
			}
			continue
		}
		if node.Kind == NodeKindTerminal {
			return nil, false
		}
		groups := s.groupsBySource[current.nodeID]
		for _, branchGroup := range groups {
			if len(s.edgesByGroup[branchGroup.ID]) > 1 {
				return nil, false
			}
		}
		nextPath := cloneBoolMap(current.path)
		nextPath[current.nodeID] = true
		for _, edge := range s.outgoingByNode[current.nodeID] {
			stack = append(stack, frame{nodeID: edge.TargetNodeID, distance: current.distance + 1, path: nextPath})
		}
	}
	return distances, true
}

func (s *validationState) addHard(code ValidationErrorCode, message string, ref ValidationError) {
	ref.Code = code
	ref.Message = message
	ref.BlocksContext = true
	s.errors = append(s.errors, ref)
}

func (s *validationState) addSemantic(code ValidationErrorCode, message string, ref ValidationError) {
	ref.Code = code
	ref.Message = message
	ref.BlocksContext = s.context != ValidationContextDraft
	s.errors = append(s.errors, ref)
}

func validDisplayName(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= MaxDisplayNameChars
}

func validModelKey(value string) bool {
	return workflowkey.Valid(value)
}

func validContextMode(value ContextMode) bool {
	switch value {
	case ContextModeNewSession, ContextModeContinueSession, ContextModeCompactAndContinueSession:
		return true
	default:
		return false
	}
}

func validTaskBindingField(field string) bool {
	switch strings.TrimSpace(field) {
	case "short_id", "title", "body", "source_url":
		return true
	default:
		return false
	}
}

func nodeOutputFieldSet(node Node) map[string]bool {
	out := map[string]bool{}
	for _, field := range node.OutputFields {
		name := strings.TrimSpace(field.Name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func templatePlaceholders(promptTemplate string) ([]string, error) {
	parsed, err := template.New("workflow_node_prompt").Parse(promptTemplate)
	if err != nil {
		return nil, err
	}
	if parsed.Tree == nil || parsed.Tree.Root == nil {
		return nil, nil
	}
	return templatePlaceholderWalker{}.collect(parsed.Tree.Root), nil
}

type templatePlaceholderWalker struct{}

func (templatePlaceholderWalker) collect(node parse.Node) []string {
	out := []string{}
	var walk func(parse.Node)
	var walkList func(*parse.ListNode)
	walkList = func(list *parse.ListNode) {
		if list == nil {
			return
		}
		for _, child := range list.Nodes {
			walk(child)
		}
	}
	walk = func(current parse.Node) {
		if current == nil {
			return
		}
		switch typed := current.(type) {
		case *parse.ListNode:
			walkList(typed)
		case *parse.ActionNode:
			if typed == nil {
				return
			}
			walk(typed.Pipe)
		case *parse.IfNode:
			if typed == nil {
				return
			}
			walk(typed.Pipe)
			walkList(typed.List)
			walkList(typed.ElseList)
		case *parse.RangeNode:
			if typed == nil {
				return
			}
			walk(typed.Pipe)
			walkList(typed.List)
			walkList(typed.ElseList)
		case *parse.WithNode:
			if typed == nil {
				return
			}
			walk(typed.Pipe)
			walkList(typed.List)
			walkList(typed.ElseList)
		case *parse.PipeNode:
			if typed == nil {
				return
			}
			for _, command := range typed.Cmds {
				walk(command)
			}
		case *parse.CommandNode:
			if typed == nil {
				return
			}
			for _, arg := range typed.Args {
				walk(arg)
			}
		case *parse.FieldNode:
			if typed == nil {
				return
			}
			if len(typed.Ident) == 2 && typed.Ident[0] == "Inputs" {
				out = append(out, typed.Ident[1])
			}
		}
	}
	walk(node)
	return out
}

func cloneBoolMap(in map[NodeID]bool) map[NodeID]bool {
	out := make(map[NodeID]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (c ValidationErrorCode) Error() string {
	return string(c)
}

func (e ValidationError) String() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
