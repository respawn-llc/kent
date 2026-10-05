package workflow

import (
	"strings"
	"text/template"
	"text/template/parse"
)

type PromptParameterReference struct {
	Name        string
	Placeholder string
}

type PromptPriorParameterReference struct {
	TransitionKey ModelKey
	ParameterKey  string
	Placeholder   string
}

type PromptTemplateReferences struct {
	Params      []PromptParameterReference
	PriorParams []PromptPriorParameterReference
	SessionID   bool
	Invalid     []PromptReferenceIssue
}

type PromptReferenceIssue struct {
	Placeholder string
	Message     string
}

func ExtractPromptTemplateReferences(promptTemplate string) (PromptTemplateReferences, error) {
	prompt := strings.TrimSpace(promptTemplate)
	if prompt == "" {
		return PromptTemplateReferences{}, nil
	}
	tmpl, err := template.New("workflow node prompt validation").Parse(prompt)
	if err != nil {
		return PromptTemplateReferences{}, err
	}
	refs := PromptTemplateReferences{}
	for _, parsed := range tmpl.Templates() {
		if parsed.Tree != nil {
			walkTemplateNode(parsed.Tree.Root, &refs)
		}
	}
	return refs, nil
}

func walkTemplateNode(node parse.Node, refs *PromptTemplateReferences) {
	switch typed := node.(type) {
	case nil:
		return
	case *parse.ListNode:
		walkTemplateNodeList(typed, refs)
	case *parse.ActionNode:
		walkTemplateNode(typed.Pipe, refs)
	case *parse.IfNode:
		walkTemplateNode(typed.Pipe, refs)
		walkTemplateNodeList(typed.List, refs)
		walkTemplateNodeList(typed.ElseList, refs)
	case *parse.RangeNode:
		walkTemplateNode(typed.Pipe, refs)
		walkTemplateNodeList(typed.List, refs)
		walkTemplateNodeList(typed.ElseList, refs)
	case *parse.WithNode:
		walkTemplateNode(typed.Pipe, refs)
		walkTemplateNodeList(typed.List, refs)
		walkTemplateNodeList(typed.ElseList, refs)
	case *parse.TemplateNode:
		walkTemplateNode(typed.Pipe, refs)
	case *parse.PipeNode:
		for _, command := range typed.Cmds {
			walkTemplateNode(command, refs)
		}
	case *parse.CommandNode:
		if len(typed.Args) > 0 {
			if ident, ok := typed.Args[0].(*parse.IdentifierNode); ok && ident.Ident == "index" && indexCommandTouchesPromptNamespace(typed.Args[1:]) {
				refs.Invalid = append(refs.Invalid, PromptReferenceIssue{Placeholder: "index", Message: "dynamic prompt reference lookup is not supported"})
			}
		}
		for _, arg := range typed.Args {
			walkTemplateNode(arg, refs)
		}
	case *parse.ChainNode:
		walkTemplateNode(typed.Node, refs)
		if len(typed.Field) > 0 && promptNamespace(typed.Field[0]) {
			refs.Invalid = append(refs.Invalid, PromptReferenceIssue{Placeholder: "." + strings.Join(typed.Field, "."), Message: "prompt reference shape is unsupported"})
		}
	case *parse.FieldNode:
		recordPromptFieldReference(typed.Ident, refs)
	case *parse.VariableNode:
		if variableTouchesPromptNamespace(typed.Ident) {
			refs.Invalid = append(refs.Invalid, PromptReferenceIssue{Placeholder: strings.Join(typed.Ident, "."), Message: "variable prompt reference lookup is not supported"})
		}
	}
}

func indexCommandTouchesPromptNamespace(args []parse.Node) bool {
	if len(args) == 0 {
		return false
	}
	if _, ok := args[0].(*parse.DotNode); ok {
		return true
	}
	for _, arg := range args {
		switch typed := arg.(type) {
		case *parse.FieldNode:
			if len(typed.Ident) > 0 && promptNamespace(typed.Ident[0]) {
				return true
			}
		case *parse.ChainNode:
			if len(typed.Field) > 0 && promptNamespace(typed.Field[0]) {
				return true
			}
			if indexCommandTouchesPromptNamespace([]parse.Node{typed.Node}) {
				return true
			}
		case *parse.VariableNode:
			if variableTouchesPromptNamespace(typed.Ident) {
				return true
			}
		case *parse.StringNode:
			if promptNamespace(typed.Text) {
				return true
			}
		}
	}
	return false
}

func variableTouchesPromptNamespace(ident []string) bool {
	for _, part := range ident {
		if part == "$Inputs" || part == "$Params" || promptNamespace(part) {
			return true
		}
	}
	return false
}

func walkTemplateNodeList(list *parse.ListNode, refs *PromptTemplateReferences) {
	if list == nil {
		return
	}
	for _, node := range list.Nodes {
		walkTemplateNode(node, refs)
	}
}

func recordPromptFieldReference(ident []string, refs *PromptTemplateReferences) {
	if len(ident) == 0 {
		return
	}
	placeholder := "." + strings.Join(ident, ".")
	switch ident[0] {
	case "Inputs":
		refs.Invalid = append(refs.Invalid, PromptReferenceIssue{Placeholder: placeholder, Message: ".Inputs prompt references are unsupported; use .Params.<parameter_key>"})
	case "Params":
		switch len(ident) {
		case 2:
			refs.Params = append(refs.Params, PromptParameterReference{Name: ident[1], Placeholder: placeholder})
		case 3:
			refs.PriorParams = append(refs.PriorParams, PromptPriorParameterReference{TransitionKey: ModelKey(ident[1]), ParameterKey: ident[2], Placeholder: placeholder})
		default:
			refs.Invalid = append(refs.Invalid, PromptReferenceIssue{Placeholder: placeholder, Message: ".Params references must use .Params.<parameter_key> or .Params.<transition_key>.<parameter_key>"})
		}
	default:
		if promptBuiltin(ident[0]) {
			if len(ident) != 1 {
				refs.Invalid = append(refs.Invalid, PromptReferenceIssue{Placeholder: placeholder, Message: "prompt built-in references must not be chained"})
			}
			if ident[0] == "SessionId" && len(ident) == 1 {
				refs.SessionID = true
			}
			return
		}
		refs.Invalid = append(refs.Invalid, PromptReferenceIssue{Placeholder: placeholder, Message: "prompt field reference is unsupported"})
	}
}

func promptNamespace(value string) bool {
	return value == "Inputs" || value == "Params"
}

func promptBuiltin(value string) bool {
	switch value {
	case "TaskId", "TaskShortId", "TaskTitle", "TaskBody", "NodeId", "NodeKey", "NodeDisplayName", "SessionId":
		return true
	default:
		return false
	}
}

type PromptSessionNodeScope struct {
	BranchKey *TransitionBranchKey
	Ambiguous bool
}

type PromptSessionReferenceResolution struct {
	Matched              int
	Guaranteed           []TransitionGroup
	SourceNodeID         NodeID
	BranchKey            *TransitionBranchKey
	BranchScopeAmbiguous bool
}

type promptSessionReferenceCandidate struct {
	group         TransitionGroup
	fanoutGroupID TransitionGroupID
	branchKey     TransitionBranchKey
}

func filterPromptSessionReferenceCandidates(
	candidates []promptSessionReferenceCandidate,
	currentFanoutGroups map[TransitionGroupID]struct{},
	currentBranch TransitionBranchKey,
) []promptSessionReferenceCandidate {
	hasCandidateInCurrentFanout := false
	for _, candidate := range candidates {
		if _, belongsToCurrentFanout := currentFanoutGroups[candidate.fanoutGroupID]; belongsToCurrentFanout {
			hasCandidateInCurrentFanout = true
			break
		}
	}
	if !hasCandidateInCurrentFanout {
		return candidates
	}
	filtered := make([]promptSessionReferenceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, belongsToCurrentFanout := currentFanoutGroups[candidate.fanoutGroupID]; belongsToCurrentFanout &&
			candidate.branchKey == currentBranch {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func promptSessionFanoutGroupsForCurrentNode(
	topology fanoutTopology,
	currentNodeID NodeID,
	currentBranch TransitionBranchKey,
) map[TransitionGroupID]struct{} {
	groups := map[TransitionGroupID]struct{}{}
	for groupID, edges := range topology.edgesByGroup {
		if len(edges) < 2 {
			continue
		}
		for _, edge := range edges {
			if TransitionBranchKey(strings.TrimSpace(string(edge.Key))) != currentBranch {
				continue
			}
			traversal, ok := fanoutBranchTraversal(
				topology.nodesByID,
				topology.groupsBySource,
				topology.edgesByGroup,
				topology.outgoingByNode,
				edge.TargetNodeID,
			)
			if !ok {
				continue
			}
			if _, exists := traversal.pathNodes[currentNodeID]; exists {
				groups[groupID] = struct{}{}
			}
		}
	}
	return groups
}

func ResolvePromptSessionNodeScope(
	def Definition,
	nodeID NodeID,
	currentNodeID NodeID,
	currentBranch *TransitionBranchKey,
) PromptSessionNodeScope {
	topology := newFanoutTopology(def)
	candidates := []promptSessionReferenceCandidate{}
	for _, group := range def.TransitionGroups {
		edges := topology.edgesByGroup[group.ID]
		if len(edges) < 2 {
			continue
		}
		for _, edge := range edges {
			traversal, ok := fanoutBranchTraversal(
				topology.nodesByID,
				topology.groupsBySource,
				topology.edgesByGroup,
				topology.outgoingByNode,
				edge.TargetNodeID,
			)
			if !ok {
				continue
			}
			if _, exists := traversal.pathNodes[nodeID]; !exists {
				continue
			}
			branchKey := TransitionBranchKey(strings.TrimSpace(string(edge.Key)))
			if branchKey == "" {
				continue
			}
			candidates = append(candidates, promptSessionReferenceCandidate{
				fanoutGroupID: group.ID,
				branchKey:     branchKey,
			})
		}
	}
	if currentBranch != nil {
		currentFanoutGroups := promptSessionFanoutGroupsForCurrentNode(topology, currentNodeID, *currentBranch)
		matching := filterPromptSessionReferenceCandidates(candidates, currentFanoutGroups, *currentBranch)
		if len(matching) == 1 {
			branchKey := matching[0].branchKey
			return PromptSessionNodeScope{BranchKey: &branchKey}
		}
		if len(matching) > 1 {
			return PromptSessionNodeScope{Ambiguous: true}
		}
		return PromptSessionNodeScope{}
	}
	if len(candidates) == 0 {
		return PromptSessionNodeScope{}
	}
	if len(candidates) != 1 {
		return PromptSessionNodeScope{Ambiguous: true}
	}
	branchKey := candidates[0].branchKey
	return PromptSessionNodeScope{BranchKey: &branchKey}
}

func ResolvePromptSessionReference(
	def Definition,
	transitionKey ModelKey,
	consumerSourceNodeID NodeID,
	currentNodeID NodeID,
	currentBranch *TransitionBranchKey,
) PromptSessionReferenceResolution {
	prior := ResolvePriorTransitionGroups(def, transitionKey, consumerSourceNodeID)
	resolution := PromptSessionReferenceResolution{
		Matched:    prior.Matched,
		Guaranteed: append([]TransitionGroup(nil), prior.Guaranteed...),
	}
	if len(prior.Guaranteed) == 1 {
		resolution.SourceNodeID = prior.Guaranteed[0].SourceNodeID
		scope := ResolvePromptSessionNodeScope(def, resolution.SourceNodeID, currentNodeID, currentBranch)
		resolution.BranchKey = scope.BranchKey
		resolution.BranchScopeAmbiguous = scope.Ambiguous
		return resolution
	}
	if len(prior.Guaranteed) != 0 || prior.Matched == 0 {
		return resolution
	}

	startNodeID, hasSingleStart := singleStartNodeID(def.Nodes)
	if !hasSingleStart {
		return resolution
	}
	topology := newFanoutTopology(def)
	candidates := []promptSessionReferenceCandidate{}
	for _, group := range def.TransitionGroups {
		if strings.TrimSpace(string(group.TransitionID)) != strings.TrimSpace(string(transitionKey)) {
			continue
		}
		for _, fanoutGroup := range def.TransitionGroups {
			fanoutEdges := topology.edgesByGroup[fanoutGroup.ID]
			if len(fanoutEdges) < 2 {
				continue
			}
			branchKeys := make([]TransitionBranchKey, 0, len(fanoutEdges))
			for _, edge := range fanoutEdges {
				branchKeys = append(branchKeys, TransitionBranchKey(strings.TrimSpace(string(edge.Key))))
			}
			join, ok := ResolveFanoutJoin(def, branchKeys)
			if !ok {
				continue
			}
			joinID := NodeIDOf(join.Join)
			if consumerSourceNodeID != joinID &&
				!nodeDominatesFromStart(startNodeID, joinID, consumerSourceNodeID, topology.outgoingByNode) {
				continue
			}
			for _, edge := range fanoutEdges {
				traversal, ok := fanoutBranchTraversal(
					topology.nodesByID,
					topology.groupsBySource,
					topology.edgesByGroup,
					topology.outgoingByNode,
					edge.TargetNodeID,
				)
				if !ok {
					continue
				}
				if _, exists := traversal.pathNodes[group.SourceNodeID]; !exists {
					continue
				}
				branchKey := TransitionBranchKey(strings.TrimSpace(string(edge.Key)))
				if branchKey == "" {
					continue
				}
				candidates = append(candidates, promptSessionReferenceCandidate{
					group:         group,
					fanoutGroupID: fanoutGroup.ID,
					branchKey:     branchKey,
				})
			}
		}
	}
	if currentBranch != nil {
		currentFanoutGroups := promptSessionFanoutGroupsForCurrentNode(topology, currentNodeID, *currentBranch)
		candidates = filterPromptSessionReferenceCandidates(candidates, currentFanoutGroups, *currentBranch)
	}
	if len(candidates) == 0 {
		return resolution
	}
	if len(candidates) > 1 {
		firstGroupID := candidates[0].group.ID
		sameGroup := true
		for _, candidate := range candidates[1:] {
			sameGroup = sameGroup && candidate.group.ID == firstGroupID
		}
		if !sameGroup {
			resolution.Guaranteed = make([]TransitionGroup, 0, len(candidates))
			for _, candidate := range candidates {
				resolution.Guaranteed = append(resolution.Guaranteed, candidate.group)
			}
			return resolution
		}
	}
	resolution.Guaranteed = []TransitionGroup{candidates[0].group}
	resolution.SourceNodeID = candidates[0].group.SourceNodeID
	if len(candidates) > 1 {
		resolution.BranchScopeAmbiguous = true
		return resolution
	}
	resolution.BranchKey = &candidates[0].branchKey
	return resolution
}
