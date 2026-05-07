package client

import (
	"context"
	"errors"

	"builder/shared/serverapi"
)

type RuntimeControlClient interface {
	SetSessionName(ctx context.Context, req serverapi.RuntimeSetSessionNameRequest) error
	SetThinkingLevel(ctx context.Context, req serverapi.RuntimeSetThinkingLevelRequest) error
	SetFastModeEnabled(ctx context.Context, req serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error)
	SetReviewerEnabled(ctx context.Context, req serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error)
	SetAutoCompactionEnabled(ctx context.Context, req serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error)
	AppendLocalEntry(ctx context.Context, req serverapi.RuntimeAppendLocalEntryRequest) error
	ShouldCompactBeforeUserMessage(ctx context.Context, req serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error)
	SubmitUserMessage(ctx context.Context, req serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error)
	SubmitUserTurn(ctx context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error)
	SubmitUserShellCommand(ctx context.Context, req serverapi.RuntimeSubmitUserShellCommandRequest) error
	CompactContext(ctx context.Context, req serverapi.RuntimeCompactContextRequest) error
	CompactContextForPreSubmit(ctx context.Context, req serverapi.RuntimeCompactContextForPreSubmitRequest) error
	HasQueuedUserWork(ctx context.Context, req serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error)
	SubmitQueuedUserMessages(ctx context.Context, req serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error)
	Interrupt(ctx context.Context, req serverapi.RuntimeInterruptRequest) error
	QueueUserMessage(ctx context.Context, req serverapi.RuntimeQueueUserMessageRequest) error
	DiscardQueuedUserMessagesMatching(ctx context.Context, req serverapi.RuntimeDiscardQueuedUserMessagesMatchingRequest) (serverapi.RuntimeDiscardQueuedUserMessagesMatchingResponse, error)
	RecordPromptHistory(ctx context.Context, req serverapi.RuntimeRecordPromptHistoryRequest) error
	ShowGoal(ctx context.Context, req serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error)
	SetGoal(ctx context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error)
	PauseGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)
	ResumeGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)
	CompleteGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)
	ClearGoal(ctx context.Context, req serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error)
}

type loopbackRuntimeControlClient struct {
	service serverapi.RuntimeControlService
}

func NewLoopbackRuntimeControlClient(service serverapi.RuntimeControlService) RuntimeControlClient {
	return &loopbackRuntimeControlClient{service: service}
}

func (c *loopbackRuntimeControlClient) SetSessionName(ctx context.Context, req serverapi.RuntimeSetSessionNameRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.SetSessionName(ctx, req)
}

func (c *loopbackRuntimeControlClient) SetThinkingLevel(ctx context.Context, req serverapi.RuntimeSetThinkingLevelRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.SetThinkingLevel(ctx, req)
}

func (c *loopbackRuntimeControlClient) SetFastModeEnabled(ctx context.Context, req serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeSetFastModeEnabledResponse{}, errors.New("runtime control service is required")
	}
	return c.service.SetFastModeEnabled(ctx, req)
}

func (c *loopbackRuntimeControlClient) SetReviewerEnabled(ctx context.Context, req serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeSetReviewerEnabledResponse{}, errors.New("runtime control service is required")
	}
	return c.service.SetReviewerEnabled(ctx, req)
}

func (c *loopbackRuntimeControlClient) SetAutoCompactionEnabled(ctx context.Context, req serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeSetAutoCompactionEnabledResponse{}, errors.New("runtime control service is required")
	}
	return c.service.SetAutoCompactionEnabled(ctx, req)
}

func (c *loopbackRuntimeControlClient) AppendLocalEntry(ctx context.Context, req serverapi.RuntimeAppendLocalEntryRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.AppendLocalEntry(ctx, req)
}

func (c *loopbackRuntimeControlClient) ShouldCompactBeforeUserMessage(ctx context.Context, req serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, errors.New("runtime control service is required")
	}
	return c.service.ShouldCompactBeforeUserMessage(ctx, req)
}

func (c *loopbackRuntimeControlClient) SubmitUserMessage(ctx context.Context, req serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeSubmitUserMessageResponse{}, errors.New("runtime control service is required")
	}
	return c.service.SubmitUserMessage(ctx, req)
}

func (c *loopbackRuntimeControlClient) SubmitUserTurn(ctx context.Context, req serverapi.RuntimeSubmitUserTurnRequest) (serverapi.RuntimeSubmitUserTurnResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeSubmitUserTurnResponse{}, errors.New("runtime control service is required")
	}
	return c.service.SubmitUserTurn(ctx, req)
}

func (c *loopbackRuntimeControlClient) SubmitUserShellCommand(ctx context.Context, req serverapi.RuntimeSubmitUserShellCommandRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.SubmitUserShellCommand(ctx, req)
}

func (c *loopbackRuntimeControlClient) CompactContext(ctx context.Context, req serverapi.RuntimeCompactContextRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.CompactContext(ctx, req)
}

func (c *loopbackRuntimeControlClient) CompactContextForPreSubmit(ctx context.Context, req serverapi.RuntimeCompactContextForPreSubmitRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.CompactContextForPreSubmit(ctx, req)
}

func (c *loopbackRuntimeControlClient) HasQueuedUserWork(ctx context.Context, req serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeHasQueuedUserWorkResponse{}, errors.New("runtime control service is required")
	}
	return c.service.HasQueuedUserWork(ctx, req)
}

func (c *loopbackRuntimeControlClient) SubmitQueuedUserMessages(ctx context.Context, req serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, errors.New("runtime control service is required")
	}
	return c.service.SubmitQueuedUserMessages(ctx, req)
}

func (c *loopbackRuntimeControlClient) Interrupt(ctx context.Context, req serverapi.RuntimeInterruptRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.Interrupt(ctx, req)
}

func (c *loopbackRuntimeControlClient) QueueUserMessage(ctx context.Context, req serverapi.RuntimeQueueUserMessageRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.QueueUserMessage(ctx, req)
}

func (c *loopbackRuntimeControlClient) DiscardQueuedUserMessagesMatching(ctx context.Context, req serverapi.RuntimeDiscardQueuedUserMessagesMatchingRequest) (serverapi.RuntimeDiscardQueuedUserMessagesMatchingResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeDiscardQueuedUserMessagesMatchingResponse{}, errors.New("runtime control service is required")
	}
	return c.service.DiscardQueuedUserMessagesMatching(ctx, req)
}

func (c *loopbackRuntimeControlClient) RecordPromptHistory(ctx context.Context, req serverapi.RuntimeRecordPromptHistoryRequest) error {
	if c == nil || c.service == nil {
		return errors.New("runtime control service is required")
	}
	return c.service.RecordPromptHistory(ctx, req)
}

func (c *loopbackRuntimeControlClient) ShowGoal(ctx context.Context, req serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeGoalShowResponse{}, errors.New("runtime control service is required")
	}
	return c.service.ShowGoal(ctx, req)
}

func (c *loopbackRuntimeControlClient) SetGoal(ctx context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeGoalShowResponse{}, errors.New("runtime control service is required")
	}
	return c.service.SetGoal(ctx, req)
}

func (c *loopbackRuntimeControlClient) PauseGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeGoalShowResponse{}, errors.New("runtime control service is required")
	}
	return c.service.PauseGoal(ctx, req)
}

func (c *loopbackRuntimeControlClient) ResumeGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeGoalShowResponse{}, errors.New("runtime control service is required")
	}
	return c.service.ResumeGoal(ctx, req)
}

func (c *loopbackRuntimeControlClient) CompleteGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeGoalShowResponse{}, errors.New("runtime control service is required")
	}
	return c.service.CompleteGoal(ctx, req)
}

func (c *loopbackRuntimeControlClient) ClearGoal(ctx context.Context, req serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if c == nil || c.service == nil {
		return serverapi.RuntimeGoalShowResponse{}, errors.New("runtime control service is required")
	}
	return c.service.ClearGoal(ctx, req)
}
