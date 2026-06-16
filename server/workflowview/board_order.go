package workflowview

import (
	"sort"

	"core/server/workflow"
	"core/shared/serverapi"
)

func boardColumnNodes(def serverapi.WorkflowDefinition) []serverapi.WorkflowNode {
	graph := newBoardColumnGraph(def)
	reachable := graph.reachableNodeIDs()
	orderedIDs := graph.orderedReachableVisibleNodeIDs(reachable)
	ordered := make([]serverapi.WorkflowNode, 0, len(graph.visibleNodes))
	emitted := make(map[string]bool, len(graph.visibleNodes))
	for _, nodeID := range orderedIDs {
		node, ok := graph.nodesByID[nodeID]
		if !ok || !boardVisibleNodeKind(node.Kind) || !reachable[nodeID] {
			continue
		}
		ordered = append(ordered, node)
		emitted[nodeID] = true
	}
	unreachable := make([]serverapi.WorkflowNode, 0, len(graph.visibleNodes)-len(ordered))
	for _, node := range graph.visibleNodes {
		if emitted[node.ID] || reachable[node.ID] {
			continue
		}
		unreachable = append(unreachable, node)
	}
	sortWorkflowNodesByKey(unreachable)
	return append(ordered, unreachable...)
}

type boardColumnGraph struct {
	nodesByID        map[string]serverapi.WorkflowNode
	visibleNodes     []serverapi.WorkflowNode
	startNodeIDs     []string
	outgoingBySource map[string][]string
}

func newBoardColumnGraph(def serverapi.WorkflowDefinition) boardColumnGraph {
	nodesByID := make(map[string]serverapi.WorkflowNode, len(def.Nodes))
	visibleNodes := make([]serverapi.WorkflowNode, 0, len(def.Nodes))
	startNodeIDs := make([]string, 0, 1)
	for _, node := range def.Nodes {
		nodesByID[node.ID] = node
		if boardVisibleNodeKind(node.Kind) {
			visibleNodes = append(visibleNodes, node)
		}
		if workflow.NodeKind(node.Kind) == workflow.NodeKindStart {
			startNodeIDs = append(startNodeIDs, node.ID)
		}
	}
	sortWorkflowNodeIDsByNodeKey(startNodeIDs, nodesByID)
	groupSourceByID := make(map[string]string, len(def.TransitionGroups))
	for _, group := range def.TransitionGroups {
		groupSourceByID[group.ID] = group.SourceNodeID
	}
	outgoingBySource := make(map[string][]string, len(def.TransitionGroups))
	for _, edge := range def.Edges {
		sourceID := groupSourceByID[edge.TransitionGroupID]
		if _, ok := nodesByID[sourceID]; !ok {
			continue
		}
		if _, ok := nodesByID[edge.TargetNodeID]; !ok {
			continue
		}
		outgoingBySource[sourceID] = append(outgoingBySource[sourceID], edge.TargetNodeID)
	}
	for sourceID, targetIDs := range outgoingBySource {
		sortWorkflowNodeIDsByNodeKey(targetIDs, nodesByID)
		outgoingBySource[sourceID] = dedupeStrings(targetIDs)
	}
	graph := boardColumnGraph{
		nodesByID:        nodesByID,
		visibleNodes:     visibleNodes,
		startNodeIDs:     startNodeIDs,
		outgoingBySource: outgoingBySource,
	}
	return graph
}

func (g boardColumnGraph) reachableNodeIDs() map[string]bool {
	reachable := make(map[string]bool, len(g.nodesByID))
	queue := append([]string(nil), g.startNodeIDs...)
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		if reachable[nodeID] {
			continue
		}
		reachable[nodeID] = true
		queue = append(queue, g.outgoingBySource[nodeID]...)
	}
	return reachable
}

func (g boardColumnGraph) orderedReachableVisibleNodeIDs(reachable map[string]bool) []string {
	precedence := make(map[string]map[string]bool, len(g.visibleNodes))
	reachableVisibleIDs := make([]string, 0, len(g.visibleNodes))
	for _, node := range g.visibleNodes {
		if !reachable[node.ID] {
			continue
		}
		reachableVisibleIDs = append(reachableVisibleIDs, node.ID)
	}
	for _, source := range g.visibleNodes {
		if !reachable[source.ID] {
			continue
		}
		for _, targetID := range g.visibleTargetsFrom(source.ID) {
			if !reachable[targetID] || source.ID == targetID {
				continue
			}
			if precedence[source.ID] == nil {
				precedence[source.ID] = map[string]bool{}
			}
			precedence[source.ID][targetID] = true
		}
	}
	return g.topologicalVisibleNodeIDs(reachableVisibleIDs, precedence)
}

func (g boardColumnGraph) topologicalVisibleNodeIDs(reachableVisibleIDs []string, precedence map[string]map[string]bool) []string {
	components := g.stronglyConnectedVisibleComponents(reachableVisibleIDs, precedence)
	componentByNodeID := make(map[string]int, len(reachableVisibleIDs))
	for componentID, component := range components {
		for _, nodeID := range component {
			componentByNodeID[nodeID] = componentID
		}
	}
	componentEdges := make(map[int]map[int]bool, len(components))
	indegree := make(map[int]int, len(components))
	for componentID := range components {
		indegree[componentID] = 0
	}
	for sourceID, targetIDs := range precedence {
		sourceComponent := componentByNodeID[sourceID]
		for targetID := range targetIDs {
			targetComponent := componentByNodeID[targetID]
			if sourceComponent == targetComponent {
				continue
			}
			if componentEdges[sourceComponent] == nil {
				componentEdges[sourceComponent] = map[int]bool{}
			}
			if componentEdges[sourceComponent][targetComponent] {
				continue
			}
			componentEdges[sourceComponent][targetComponent] = true
			indegree[targetComponent]++
		}
	}
	available := make([]int, 0, len(components))
	for componentID := range components {
		if indegree[componentID] == 0 {
			available = append(available, componentID)
		}
	}
	orderedComponents := make([]int, 0, len(components))
	emitted := make(map[int]bool, len(components))
	for len(available) > 0 {
		g.sortComponentIDsByKey(available, components)
		componentID := available[0]
		available = available[1:]
		if emitted[componentID] {
			continue
		}
		orderedComponents = append(orderedComponents, componentID)
		emitted[componentID] = true
		targetComponentIDs := mapIntKeys(componentEdges[componentID])
		g.sortComponentIDsByKey(targetComponentIDs, components)
		for _, targetComponentID := range targetComponentIDs {
			indegree[targetComponentID]--
			if indegree[targetComponentID] == 0 {
				available = append(available, targetComponentID)
			}
		}
	}
	if len(orderedComponents) != len(components) {
		for componentID := range components {
			if !emitted[componentID] {
				orderedComponents = append(orderedComponents, componentID)
			}
		}
	}
	ordered := make([]string, 0, len(reachableVisibleIDs))
	for _, componentID := range orderedComponents {
		ordered = append(ordered, components[componentID]...)
	}
	return ordered
}

func (g boardColumnGraph) stronglyConnectedVisibleComponents(nodeIDs []string, precedence map[string]map[string]bool) [][]string {
	indexByNodeID := make(map[string]int, len(nodeIDs))
	lowByNodeID := make(map[string]int, len(nodeIDs))
	onStack := make(map[string]bool, len(nodeIDs))
	stack := make([]string, 0, len(nodeIDs))
	nextIndex := 0
	components := make([][]string, 0)
	orderedNodeIDs := append([]string(nil), nodeIDs...)
	sortWorkflowNodeIDsByNodeKey(orderedNodeIDs, g.nodesByID)
	var visit func(string)
	visit = func(nodeID string) {
		indexByNodeID[nodeID] = nextIndex
		lowByNodeID[nodeID] = nextIndex
		nextIndex++
		stack = append(stack, nodeID)
		onStack[nodeID] = true
		targetIDs := mapKeys(precedence[nodeID])
		sortWorkflowNodeIDsByNodeKey(targetIDs, g.nodesByID)
		for _, targetID := range targetIDs {
			if _, seen := indexByNodeID[targetID]; !seen {
				visit(targetID)
				lowByNodeID[nodeID] = min(lowByNodeID[nodeID], lowByNodeID[targetID])
				continue
			}
			if onStack[targetID] {
				lowByNodeID[nodeID] = min(lowByNodeID[nodeID], indexByNodeID[targetID])
			}
		}
		if lowByNodeID[nodeID] != indexByNodeID[nodeID] {
			return
		}
		component := make([]string, 0)
		for len(stack) > 0 {
			stackNodeID := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[stackNodeID] = false
			component = append(component, stackNodeID)
			if stackNodeID == nodeID {
				break
			}
		}
		sortWorkflowNodeIDsByNodeKey(component, g.nodesByID)
		components = append(components, component)
	}
	for _, nodeID := range orderedNodeIDs {
		if _, seen := indexByNodeID[nodeID]; !seen {
			visit(nodeID)
		}
	}
	return components
}

func (g boardColumnGraph) sortComponentIDsByKey(componentIDs []int, components [][]string) {
	sort.SliceStable(componentIDs, func(i, j int) bool {
		leftTerminal := g.componentHasTerminalNode(components[componentIDs[i]])
		rightTerminal := g.componentHasTerminalNode(components[componentIDs[j]])
		if leftTerminal != rightTerminal {
			return !leftTerminal
		}
		return workflowNodeKeyLess(g.nodesByID[components[componentIDs[i]][0]], g.nodesByID[components[componentIDs[j]][0]])
	})
}

func (g boardColumnGraph) componentHasTerminalNode(component []string) bool {
	for _, nodeID := range component {
		if workflow.NodeKind(g.nodesByID[nodeID].Kind) == workflow.NodeKindTerminal {
			return true
		}
	}
	return false
}

func (g boardColumnGraph) visibleTargetsFrom(sourceID string) []string {
	targets := make([]string, 0)
	seenHidden := map[string]bool{}
	queue := append([]string(nil), g.outgoingBySource[sourceID]...)
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		node, ok := g.nodesByID[nodeID]
		if !ok {
			continue
		}
		if boardVisibleNodeKind(node.Kind) {
			targets = append(targets, nodeID)
			continue
		}
		if seenHidden[nodeID] {
			continue
		}
		seenHidden[nodeID] = true
		queue = append(queue, g.outgoingBySource[nodeID]...)
	}
	sortWorkflowNodeIDsByNodeKey(targets, g.nodesByID)
	return dedupeStrings(targets)
}

func sortWorkflowNodesByKey(nodes []serverapi.WorkflowNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return workflowNodeKeyLess(nodes[i], nodes[j])
	})
}

func sortWorkflowNodeIDsByNodeKey(nodeIDs []string, nodesByID map[string]serverapi.WorkflowNode) {
	sort.SliceStable(nodeIDs, func(i, j int) bool {
		return workflowNodeKeyLess(nodesByID[nodeIDs[i]], nodesByID[nodeIDs[j]])
	})
}

func workflowNodeKeyLess(left serverapi.WorkflowNode, right serverapi.WorkflowNode) bool {
	if left.Key != right.Key {
		return left.Key < right.Key
	}
	return left.ID < right.ID
}

func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	deduped := values[:0]
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		deduped = append(deduped, value)
	}
	return deduped
}

func mapKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func mapIntKeys(values map[int]bool) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
