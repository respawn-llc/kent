package worktreecreateform

import "core/shared/serverapi"

type Field uint8

const (
	FieldBranchTarget Field = iota
	FieldBaseRef
	FieldActions
)

type Action uint8

const (
	ActionCreate Action = iota
	ActionCancel
)

func OrderedFields(kind serverapi.WorktreeCreateTargetResolutionKind) []Field {
	fields := []Field{FieldBranchTarget}
	if kind == serverapi.WorktreeCreateTargetResolutionKindNewBranch {
		fields = append(fields, FieldBaseRef)
	}
	fields = append(fields, FieldActions)
	return fields
}

func MoveField(field Field, kind serverapi.WorktreeCreateTargetResolutionKind, delta int) Field {
	fields := OrderedFields(kind)
	index := 0
	for idx, candidate := range fields {
		if candidate == field {
			index = idx
			break
		}
	}
	index += delta
	if index < 0 {
		index = 0
	}
	if index >= len(fields) {
		index = len(fields) - 1
	}
	return fields[index]
}

func MoveAction(action Action, delta int) Action {
	index := int(action) + delta
	if index < int(ActionCreate) {
		index = int(ActionCreate)
	}
	if index > int(ActionCancel) {
		index = int(ActionCancel)
	}
	return Action(index)
}
