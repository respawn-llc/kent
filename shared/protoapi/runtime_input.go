package protoapi

import (
	"fmt"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/textutil"
)

func UserTurnInputToProto(input runtimeinput.Input) (*runtimepb.UserTurnInput, error) {
	switch input.Kind {
	case runtimeinput.KindText:
		return &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_Text{Text: *input.Text}}, nil
	case runtimeinput.KindPromptCommand:
		return &runtimepb.UserTurnInput{Input: &runtimepb.UserTurnInput_PromptCommand{
			PromptCommand: &runtimepb.PromptCommandInput{Name: input.PromptCommand.Name, Arguments: input.PromptCommand.Arguments},
		}}, nil
	default:
		return nil, fmt.Errorf("invalid user input kind %q", input.Kind)
	}
}

func UserTurnInputFromProto(input *runtimepb.UserTurnInput) runtimeinput.Input {
	switch selected := input.Input.(type) {
	case *runtimepb.UserTurnInput_Text:
		return runtimeinput.Text(selected.Text)
	default:
		command := input.Input.(*runtimepb.UserTurnInput_PromptCommand).PromptCommand
		return runtimeinput.Command(command.Name, command.Arguments)
	}
}

func ManualCompactionAdmissionToProto(admission runtimeinput.ManualCompactionAdmission) *runtimepb.ManualCompactionAdmission {
	return &runtimepb.ManualCompactionAdmission{Guidance: textutil.Pointer(admission.Guidance)}
}

func ManualCompactionAdmissionFromProto(admission *runtimepb.ManualCompactionAdmission) runtimeinput.ManualCompactionAdmission {
	return runtimeinput.ManualCompactionAdmission{Guidance: textutil.Pointer(admission.Guidance)}
}

func PendingWorkKindToProto(kind runtimeinput.PendingWorkItemKind) (runtimepb.PendingWorkItemKind, error) {
	return enumToProto(pendingWorkKindValues, kind, "Pending Work kind")
}

var pendingWorkKindValues = map[runtimepb.PendingWorkItemKind]runtimeinput.PendingWorkItemKind{
	runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MESSAGE:             runtimeinput.PendingWorkItemKindMessage,
	runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MANUAL_COMPACTION:   runtimeinput.PendingWorkItemKindManualCompaction,
	runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_WORKTREE_TRANSITION: runtimeinput.PendingWorkItemKindWorktreeTransition,
}

var pendingWorkLaneValues = map[runtimepb.PendingWorkLane]runtimeinput.PendingWorkLane{
	runtimepb.PendingWorkLane_PENDING_WORK_LANE_QUEUE: runtimeinput.PendingWorkLaneQueue,
	runtimepb.PendingWorkLane_PENDING_WORK_LANE_STEER: runtimeinput.PendingWorkLaneSteer,
}

var pendingWorkTransitionValues = map[runtimepb.PendingWorkWorktreeTransitionKind]runtimeinput.PendingWorkWorktreeTransitionKind{
	runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_ENTER: runtimeinput.PendingWorkWorktreeTransitionEnter,
	runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_LEAVE: runtimeinput.PendingWorkWorktreeTransitionLeave,
}

func PendingWorkToProto(work runtimeinput.PendingWork) (*runtimepb.PendingWork, error) {
	result := &runtimepb.PendingWork{Items: make([]*runtimepb.PendingWorkItem, 0, len(work.Items))}
	for _, item := range work.Items {
		kind, err := PendingWorkKindToProto(item.Kind)
		if err != nil {
			return nil, err
		}
		value := &runtimepb.PendingWorkItem{
			Id: item.ID.String(), Kind: kind, CanonicalInput: item.CanonicalInput,
			State: runtimepb.PendingWorkItemState_PENDING_WORK_ITEM_STATE_PENDING,
		}
		value.Lane, err = enumToProto(pendingWorkLaneValues, item.Lane, "Pending Work lane")
		if err != nil {
			return nil, err
		}
		switch item.Kind {
		case runtimeinput.PendingWorkItemKindMessage:
			value.Payload = &runtimepb.PendingWorkItem_Message{Message: &runtimepb.PendingWorkMessage{Text: item.Message.Text}}
		case runtimeinput.PendingWorkItemKindManualCompaction:
			value.Payload = &runtimepb.PendingWorkItem_ManualCompaction{ManualCompaction: ManualCompactionAdmissionToProto(*item.ManualCompaction)}
		case runtimeinput.PendingWorkItemKindWorktreeTransition:
			transition, err := enumToProto(pendingWorkTransitionValues, item.WorktreeTransition.Transition, "Pending Work transition")
			if err != nil {
				return nil, err
			}
			value.Payload = &runtimepb.PendingWorkItem_WorktreeTransition{WorktreeTransition: &runtimepb.PendingWorkWorktreeTransition{
				Transition: transition, Selector: textutil.Pointer(item.WorktreeTransition.Selector),
			}}
		}
		result.Items = append(result.Items, value)
	}
	return result, nil
}

func PendingWorkFromProto(work *runtimepb.PendingWork) (runtimeinput.PendingWork, error) {
	result := runtimeinput.PendingWork{Items: make([]runtimeinput.PendingWorkItem, 0, len(work.Items))}
	for _, item := range work.Items {
		id, err := runtimeids.QueueItemIDFromProto(item.Id)
		if err != nil {
			return runtimeinput.PendingWork{}, err
		}
		kind := pendingWorkKindValues[item.Kind]
		value := runtimeinput.PendingWorkItem{ID: id, Kind: kind, CanonicalInput: item.CanonicalInput, State: runtimeinput.PendingWorkItemStatePending}
		value.Lane = pendingWorkLaneValues[item.Lane]
		switch payload := item.Payload.(type) {
		case *runtimepb.PendingWorkItem_Message:
			value.Message = &runtimeinput.PendingWorkMessage{Text: payload.Message.Text}
		case *runtimepb.PendingWorkItem_ManualCompaction:
			admission := ManualCompactionAdmissionFromProto(payload.ManualCompaction)
			value.ManualCompaction = &admission
		default:
			worktree := item.Payload.(*runtimepb.PendingWorkItem_WorktreeTransition)
			transition := runtimeinput.PendingWorkWorktreeTransition{Selector: textutil.Pointer(worktree.WorktreeTransition.Selector)}
			transition.Transition = pendingWorkTransitionValues[worktree.WorktreeTransition.Transition]
			value.WorktreeTransition = &transition
		}
		result.Items = append(result.Items, value)
	}
	return result, nil
}
