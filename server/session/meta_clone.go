package session

import "core/shared/config"

func cloneMeta(in Meta) Meta {
	out := in
	if in.ConnectionID != nil {
		id := *in.ConnectionID
		out.ConnectionID = &id
	}
	if in.ProtectedInputDraft != nil {
		text := *in.ProtectedInputDraft
		out.ProtectedInputDraft = &text
	}
	if in.PreviousSessionID != nil {
		previousSessionID := *in.PreviousSessionID
		out.PreviousSessionID = &previousSessionID
	}
	if in.ParentAgentSessionID != nil {
		parentAgentSessionID := *in.ParentAgentSessionID
		out.ParentAgentSessionID = &parentAgentSessionID
	}
	out.Continuation = cloneContinuationContext(in.Continuation)
	out.ChatSettings = cloneChatSettingsOverrides(in.ChatSettings)
	out.RetainedToolSelection = config.CloneToolSelection(in.RetainedToolSelection)
	if in.OriginalThinkingEffort != nil {
		effort := *in.OriginalThinkingEffort
		out.OriginalThinkingEffort = &effort
	}
	out.WorktreeReminder = CloneWorktreeReminderState(in.WorktreeReminder)
	out.RebindReminder = CloneSessionRebindReminder(in.RebindReminder)
	if in.UsageState != nil {
		usage := *in.UsageState
		out.UsageState = &usage
	}
	if in.Goal != nil {
		goal := *in.Goal
		out.Goal = &goal
	}
	out.Locked = cloneLockedContract(in.Locked)
	out.ActiveWorkflowAssignment = cloneMessageRecord(in.ActiveWorkflowAssignment)
	out.ActiveWorkflowAssignmentState = cloneActiveWorkflowAssignmentState(in.ActiveWorkflowAssignmentState)
	return out
}

func cloneActiveWorkflowAssignmentState(in *ActiveWorkflowAssignmentState) *ActiveWorkflowAssignmentState {
	if in == nil {
		return nil
	}
	return &ActiveWorkflowAssignmentState{}
}

func cloneMessageRecord(in *MessageRecord) *MessageRecord {
	if in == nil {
		return nil
	}
	out, err := normalizeMessageRecord(*in)
	if err != nil {
		panic(err)
	}
	return &out
}
