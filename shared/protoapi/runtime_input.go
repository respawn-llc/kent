package protoapi

import (
	"fmt"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/textutil"
)

func UserTurnInputToProto(input runtimeinput.Input) (*runtimepb.UserTurnInput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
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

func UserTurnInputFromProto(input *runtimepb.UserTurnInput) (runtimeinput.Input, error) {
	switch selected := input.GetInput().(type) {
	case *runtimepb.UserTurnInput_Text:
		return runtimeinput.Text(selected.Text), nil
	case *runtimepb.UserTurnInput_PromptCommand:
		return runtimeinput.Command(selected.PromptCommand.Name, selected.PromptCommand.Arguments), nil
	default:
		return runtimeinput.Input{}, fmt.Errorf("user input selection is required")
	}
}

func ManualCompactionAdmissionToProto(admission runtimeinput.ManualCompactionAdmission) *runtimepb.ManualCompactionAdmission {
	return &runtimepb.ManualCompactionAdmission{Guidance: textutil.Pointer(admission.Guidance)}
}

func ManualCompactionAdmissionFromProto(admission *runtimepb.ManualCompactionAdmission) runtimeinput.ManualCompactionAdmission {
	return runtimeinput.ManualCompactionAdmission{Guidance: textutil.Pointer(admission.Guidance)}
}

func PendingWorkKindToProto(kind runtimeinput.PendingWorkItemKind) (runtimepb.PendingWorkItemKind, error) {
	switch kind {
	case runtimeinput.PendingWorkItemKindMessage:
		return runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MESSAGE, nil
	case runtimeinput.PendingWorkItemKindManualCompaction:
		return runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MANUAL_COMPACTION, nil
	case runtimeinput.PendingWorkItemKindWorktreeTransition:
		return runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_WORKTREE_TRANSITION, nil
	default:
		return runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_UNSPECIFIED, fmt.Errorf("invalid Pending Work kind %q", kind)
	}
}

func PendingWorkKindFromProto(kind runtimepb.PendingWorkItemKind) (runtimeinput.PendingWorkItemKind, error) {
	switch kind {
	case runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MESSAGE:
		return runtimeinput.PendingWorkItemKindMessage, nil
	case runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_MANUAL_COMPACTION:
		return runtimeinput.PendingWorkItemKindManualCompaction, nil
	case runtimepb.PendingWorkItemKind_PENDING_WORK_ITEM_KIND_WORKTREE_TRANSITION:
		return runtimeinput.PendingWorkItemKindWorktreeTransition, nil
	default:
		return "", fmt.Errorf("invalid Pending Work kind %v", kind)
	}
}

func PendingWorkToProto(work runtimeinput.PendingWork) (*runtimepb.PendingWork, error) {
	if err := work.Validate(); err != nil {
		return nil, err
	}
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
		if item.Lane == runtimeinput.PendingWorkLaneQueue {
			value.Lane = runtimepb.PendingWorkLane_PENDING_WORK_LANE_QUEUE
		} else {
			value.Lane = runtimepb.PendingWorkLane_PENDING_WORK_LANE_STEER
		}
		switch item.Kind {
		case runtimeinput.PendingWorkItemKindMessage:
			value.Payload = &runtimepb.PendingWorkItem_Message{Message: &runtimepb.PendingWorkMessage{Text: item.Message.Text}}
		case runtimeinput.PendingWorkItemKindManualCompaction:
			value.Payload = &runtimepb.PendingWorkItem_ManualCompaction{ManualCompaction: ManualCompactionAdmissionToProto(*item.ManualCompaction)}
		case runtimeinput.PendingWorkItemKindWorktreeTransition:
			transition := runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_LEAVE
			if item.WorktreeTransition.Transition == runtimeinput.PendingWorkWorktreeTransitionEnter {
				transition = runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_ENTER
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
		id, err := runtimeids.ParseQueueItemID(item.Id)
		if err != nil {
			return runtimeinput.PendingWork{}, err
		}
		kind, err := PendingWorkKindFromProto(item.Kind)
		if err != nil {
			return runtimeinput.PendingWork{}, err
		}
		value := runtimeinput.PendingWorkItem{ID: id, Kind: kind, CanonicalInput: item.CanonicalInput, State: runtimeinput.PendingWorkItemStatePending}
		switch item.Lane {
		case runtimepb.PendingWorkLane_PENDING_WORK_LANE_QUEUE:
			value.Lane = runtimeinput.PendingWorkLaneQueue
		case runtimepb.PendingWorkLane_PENDING_WORK_LANE_STEER:
			value.Lane = runtimeinput.PendingWorkLaneSteer
		default:
			return runtimeinput.PendingWork{}, fmt.Errorf("invalid Pending Work lane %v", item.Lane)
		}
		switch payload := item.Payload.(type) {
		case *runtimepb.PendingWorkItem_Message:
			value.Message = &runtimeinput.PendingWorkMessage{Text: payload.Message.Text}
		case *runtimepb.PendingWorkItem_ManualCompaction:
			admission := ManualCompactionAdmissionFromProto(payload.ManualCompaction)
			value.ManualCompaction = &admission
		case *runtimepb.PendingWorkItem_WorktreeTransition:
			transition := runtimeinput.PendingWorkWorktreeTransition{Selector: textutil.Pointer(payload.WorktreeTransition.Selector)}
			switch payload.WorktreeTransition.Transition {
			case runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_ENTER:
				transition.Transition = runtimeinput.PendingWorkWorktreeTransitionEnter
			case runtimepb.PendingWorkWorktreeTransitionKind_PENDING_WORK_WORKTREE_TRANSITION_KIND_LEAVE:
				transition.Transition = runtimeinput.PendingWorkWorktreeTransitionLeave
			default:
				return runtimeinput.PendingWork{}, fmt.Errorf("invalid Pending Work transition %v", payload.WorktreeTransition.Transition)
			}
			value.WorktreeTransition = &transition
		default:
			return runtimeinput.PendingWork{}, fmt.Errorf("Pending Work payload is required")
		}
		result.Items = append(result.Items, value)
	}
	return result, result.Validate()
}
