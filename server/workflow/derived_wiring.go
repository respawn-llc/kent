package workflow

import (
	"fmt"
	"strings"
)

type DerivedWiring struct {
	Diagnostics []ValidationError

	inputBindingsByEdge              map[EdgeID][]InputBinding
	requiredProvisionFieldsByEdge    map[EdgeID][]OutputField
	requiredProvisionFieldsByGroup   map[TransitionGroupID][]OutputField
	possibleProvisionFieldsByNode    map[NodeID][]OutputField
	requiredProviderFieldsByJoinEdge map[EdgeID][]OutputField
	joinOutputFieldsByNode           map[NodeID][]OutputField
}

func DeriveWiring(def Definition) DerivedWiring {
	derived := DerivedWiring{
		inputBindingsByEdge:              map[EdgeID][]InputBinding{},
		requiredProvisionFieldsByEdge:    map[EdgeID][]OutputField{},
		requiredProvisionFieldsByGroup:   map[TransitionGroupID][]OutputField{},
		possibleProvisionFieldsByNode:    map[NodeID][]OutputField{},
		requiredProviderFieldsByJoinEdge: map[EdgeID][]OutputField{},
		joinOutputFieldsByNode:           map[NodeID][]OutputField{},
	}
	nodesByID := make(map[NodeID]Node, len(def.Nodes))
	groupsByID := make(map[TransitionGroupID]TransitionGroup, len(def.TransitionGroups))
	edgesByID := make(map[EdgeID]Edge, len(def.Edges))
	edgesByGroup := make(map[TransitionGroupID][]Edge, len(def.Edges))
	groupsBySource := make(map[NodeID][]TransitionGroup, len(def.TransitionGroups))
	incomingByNode := make(map[NodeID][]Edge, len(def.Edges))
	for _, node := range def.Nodes {
		if strings.TrimSpace(string(node.ID)) != "" {
			nodesByID[node.ID] = node
		}
	}
	for _, group := range def.TransitionGroups {
		if strings.TrimSpace(string(group.ID)) != "" {
			groupsByID[group.ID] = group
			groupsBySource[group.SourceNodeID] = append(groupsBySource[group.SourceNodeID], group)
		}
	}
	for _, edge := range def.Edges {
		if strings.TrimSpace(string(edge.ID)) != "" {
			edgesByID[edge.ID] = edge
		}
		edgesByGroup[edge.TransitionGroupID] = append(edgesByGroup[edge.TransitionGroupID], edge)
		incomingByNode[edge.TargetNodeID] = append(incomingByNode[edge.TargetNodeID], edge)
	}
	for _, edge := range def.Edges {
		target, targetExists := nodesByID[edge.TargetNodeID]
		if !targetExists {
			continue
		}
		requiredFields := targetRequiredInputFields(target)
		if len(requiredFields) == 0 {
			continue
		}
		if group, groupExists := groupsByID[edge.TransitionGroupID]; groupExists {
			if source, sourceExists := nodesByID[group.SourceNodeID]; sourceExists && source.Kind == NodeKindStart {
				derived.addDiagnostic(CodeInvalidFirstNodeInput, fmt.Sprintf("%s cannot declare upstream inputs as the first executable node", nodeMessageSubject(target)), ValidationError{NodeID: target.ID, EdgeID: edge.ID, TransitionGroupID: edge.TransitionGroupID})
				continue
			}
		}
		derived.inputBindingsByEdge[edge.ID] = inputBindingsForFields(requiredFields)
		derived.addRequiredProvisionFields(edge.ID, edge.TransitionGroupID, requiredFields)
		if group, groupExists := groupsByID[edge.TransitionGroupID]; groupExists {
			derived.addPossibleProvisionFields(group.SourceNodeID, requiredFields)
		}
	}
	for _, node := range def.Nodes {
		if node.Kind == NodeKindJoin {
			derived.deriveJoinProviderRequirements(node, nodesByID, groupsByID, edgesByGroup, groupsBySource, incomingByNode)
		}
	}
	return derived
}

func (w DerivedWiring) InputBindingsForEdge(edgeID EdgeID) []InputBinding {
	return append([]InputBinding(nil), w.inputBindingsByEdge[edgeID]...)
}

func (w DerivedWiring) RequiredProvisionFieldsForTransitionGroup(groupID TransitionGroupID) []OutputField {
	return append([]OutputField(nil), w.requiredProvisionFieldsByGroup[groupID]...)
}

func (w DerivedWiring) RequiredProvisionFieldsForEdge(edgeID EdgeID) []OutputField {
	return append([]OutputField(nil), w.requiredProvisionFieldsByEdge[edgeID]...)
}

func (w DerivedWiring) PossibleProvisionFieldsForNode(nodeID NodeID) []OutputField {
	return append([]OutputField(nil), w.possibleProvisionFieldsByNode[nodeID]...)
}

func (w DerivedWiring) RequiredProviderFieldsForJoinEdge(edgeID EdgeID) []OutputField {
	return append([]OutputField(nil), w.requiredProviderFieldsByJoinEdge[edgeID]...)
}

func (w DerivedWiring) JoinOutputFieldsForNode(nodeID NodeID) []OutputField {
	return append([]OutputField(nil), w.joinOutputFieldsByNode[nodeID]...)
}

func (w *DerivedWiring) addRequiredProvisionFields(edgeID EdgeID, groupID TransitionGroupID, fields []OutputField) {
	edgeMerged, edgeDiagnostics := appendCompatibleOutputFields(w.requiredProvisionFieldsByEdge[edgeID], fields, ValidationError{EdgeID: edgeID, TransitionGroupID: groupID})
	w.requiredProvisionFieldsByEdge[edgeID] = edgeMerged
	merged, diagnostics := appendCompatibleOutputFields(w.requiredProvisionFieldsByGroup[groupID], fields, ValidationError{TransitionGroupID: groupID})
	w.requiredProvisionFieldsByGroup[groupID] = merged
	w.Diagnostics = append(w.Diagnostics, edgeDiagnostics...)
	w.Diagnostics = append(w.Diagnostics, diagnostics...)
}

func (w *DerivedWiring) addPossibleProvisionFields(nodeID NodeID, fields []OutputField) {
	w.possibleProvisionFieldsByNode[nodeID] = appendUniqueOutputFields(w.possibleProvisionFieldsByNode[nodeID], fields)
}

func (w *DerivedWiring) addRequiredProviderFields(edgeID EdgeID, fields []OutputField, ref ValidationError) {
	merged, diagnostics := appendCompatibleOutputFields(w.requiredProviderFieldsByJoinEdge[edgeID], fields, ref)
	w.requiredProviderFieldsByJoinEdge[edgeID] = merged
	w.Diagnostics = append(w.Diagnostics, diagnostics...)
}

func (w *DerivedWiring) deriveJoinProviderRequirements(
	join Node,
	nodesByID map[NodeID]Node,
	groupsByID map[TransitionGroupID]TransitionGroup,
	edgesByGroup map[TransitionGroupID][]Edge,
	groupsBySource map[NodeID][]TransitionGroup,
	incomingByNode map[NodeID][]Edge,
) {
	groups := groupsBySource[join.ID]
	if len(groups) != 1 {
		w.addDiagnostic(CodeInvalidJoinOutgoingShape, fmt.Sprintf("%s must have exactly one outgoing transition group", nodeMessageSubject(join)), ValidationError{NodeID: join.ID})
		return
	}
	outgoingEdges := edgesByGroup[groups[0].ID]
	if len(outgoingEdges) != 1 {
		w.addDiagnostic(CodeInvalidJoinOutgoingShape, fmt.Sprintf("%s must have exactly one edge in its outgoing transition group", nodeMessageSubject(join)), ValidationError{NodeID: join.ID, TransitionGroupID: groups[0].ID})
		return
	}
	target, targetExists := nodesByID[outgoingEdges[0].TargetNodeID]
	if !targetExists {
		return
	}
	requiredFields := targetRequiredInputFields(target)
	if len(requiredFields) == 0 {
		return
	}
	w.joinOutputFieldsByNode[join.ID] = appendUniqueOutputFields(w.joinOutputFieldsByNode[join.ID], requiredFields)

	incomingEdges := incomingByNode[join.ID]
	incomingByID := make(map[EdgeID]Edge, len(incomingEdges))
	for _, edge := range incomingEdges {
		incomingByID[edge.ID] = edge
	}
	providersByInput := map[string][]JoinInputProvider{}
	for _, provider := range join.JoinInputProviders {
		inputName := strings.TrimSpace(provider.InputName)
		if inputName == "" {
			continue
		}
		providersByInput[inputName] = append(providersByInput[inputName], provider)
	}
	for _, field := range requiredFields {
		inputName := strings.TrimSpace(field.Name)
		providers := providersByInput[inputName]
		if len(providers) == 0 {
			w.addDiagnostic(CodeMissingJoinInputProvider, nodeJoinInputMessage(join, inputName, "requires a provider edge"), ValidationError{NodeID: join.ID, InputName: inputName})
			continue
		}
		if len(providers) != 1 {
			w.addDiagnostic(CodeDuplicateJoinInputProvider, nodeJoinInputMessage(join, inputName, "must have exactly one provider edge"), ValidationError{NodeID: join.ID, InputName: inputName})
			continue
		}
		providerEdge, ok := incomingByID[providers[0].ProviderEdgeID]
		if !ok {
			w.addDiagnostic(CodeInvalidJoinInputProvider, nodeJoinInputMessage(join, inputName, "provider must reference an incoming edge into the join"), ValidationError{NodeID: join.ID, EdgeID: providers[0].ProviderEdgeID, InputName: inputName, ProviderEdgeID: providers[0].ProviderEdgeID})
			continue
		}
		providerFields := []OutputField{field}
		ref := ValidationError{NodeID: join.ID, EdgeID: providerEdge.ID, TransitionGroupID: providerEdge.TransitionGroupID, InputName: inputName, ProviderEdgeID: providerEdge.ID}
		w.addRequiredProviderFields(providerEdge.ID, providerFields, ref)
		w.addRequiredProvisionFields(providerEdge.ID, providerEdge.TransitionGroupID, providerFields)
		if providerGroup, groupExists := groupsByID[providerEdge.TransitionGroupID]; groupExists {
			w.addPossibleProvisionFields(providerGroup.SourceNodeID, providerFields)
		}
	}
}

func (w *DerivedWiring) addDiagnostic(code ValidationErrorCode, message string, ref ValidationError) {
	ref.Code = code
	ref.Message = message
	ref.BlocksContext = true
	w.Diagnostics = append(w.Diagnostics, ref)
}

func nodeJoinInputMessage(join Node, inputName string, message string) string {
	name := strings.TrimSpace(inputName)
	if name == "" {
		return fmt.Sprintf("%s: join input %s", nodeMessageSubject(join), message)
	}
	return fmt.Sprintf("%s: join input %s %s", nodeMessageSubject(join), name, message)
}

func targetRequiredInputFields(node Node) []OutputField {
	if node.Kind != NodeKindAgent {
		return nil
	}
	fields := make([]OutputField, 0, len(node.InputFields))
	for _, input := range node.InputFields {
		name := strings.TrimSpace(input.Name)
		description := strings.TrimSpace(input.Description)
		if name == "" {
			continue
		}
		fields = append(fields, OutputField{Name: name, Description: description})
	}
	return fields
}

func inputBindingsForFields(fields []OutputField) []InputBinding {
	bindings := make([]InputBinding, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		bindings = append(bindings, InputBinding{Name: name, Source: BindingSourceTransitionOutput, Field: name})
	}
	return bindings
}

func appendUniqueOutputFields(existing []OutputField, additions []OutputField) []OutputField {
	out := append([]OutputField(nil), existing...)
	seen := make(map[string]int, len(out))
	for index, field := range out {
		seen[strings.TrimSpace(field.Name)] = index
	}
	for _, field := range additions {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = len(out)
		out = append(out, OutputField{Name: name, Description: strings.TrimSpace(field.Description)})
	}
	return out
}

func appendCompatibleOutputFields(existing []OutputField, additions []OutputField, ref ValidationError) ([]OutputField, []ValidationError) {
	out := append([]OutputField(nil), existing...)
	seen := make(map[string]int, len(out))
	for index, field := range out {
		seen[strings.TrimSpace(field.Name)] = index
	}
	diagnostics := []ValidationError{}
	for _, field := range additions {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		description := strings.TrimSpace(field.Description)
		if previousIndex, exists := seen[name]; exists {
			if strings.TrimSpace(out[previousIndex].Description) != description {
				diagnostic := ref
				diagnostic.Code = CodeProvisionFieldOverlap
				diagnostic.Message = "derived provision field has incompatible input definitions"
				diagnostic.BlocksContext = true
				diagnostic.FieldName = name
				diagnostics = append(diagnostics, diagnostic)
			}
			continue
		}
		seen[name] = len(out)
		out = append(out, OutputField{Name: name, Description: description})
	}
	return out, diagnostics
}
