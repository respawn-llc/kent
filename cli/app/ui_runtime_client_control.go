package app

import (
	"context"
	"strings"

	"builder/shared/clientui"
	"builder/shared/serverapi"
	"github.com/google/uuid"
)

func (c *sessionRuntimeClient) SetSessionName(name string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	if err := c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.SetSessionName(ctx, serverapi.RuntimeSetSessionNameRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Name: name})
	}); err != nil {
		return err
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Session.SessionName = name
	})
	return nil
}

func (c *sessionRuntimeClient) SetThinkingLevel(level string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	if err := c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.SetThinkingLevel(ctx, serverapi.RuntimeSetThinkingLevelRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Level: level})
	}); err != nil {
		return err
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.ThinkingLevel = level
	})
	return nil
}

func (c *sessionRuntimeClient) SetFastModeEnabled(enabled bool) (bool, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
		return c.controls.SetFastModeEnabled(ctx, serverapi.RuntimeSetFastModeEnabledRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Enabled: enabled})
	})
	if err == nil {
		c.patchMainView(func(view *clientui.RuntimeMainView) {
			view.Status.FastModeEnabled = enabled
		})
	}
	return resp.Changed, err
}

func (c *sessionRuntimeClient) SetReviewerEnabled(enabled bool) (bool, string, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
		return c.controls.SetReviewerEnabled(ctx, serverapi.RuntimeSetReviewerEnabledRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Enabled: enabled})
	})
	if err == nil {
		c.patchMainView(func(view *clientui.RuntimeMainView) {
			view.Status.ReviewerFrequency = resp.Mode
			view.Status.ReviewerEnabled = resp.Mode != "" && resp.Mode != "off"
		})
	}
	return resp.Changed, resp.Mode, err
}

func (c *sessionRuntimeClient) SetAutoCompactionEnabled(enabled bool) (bool, bool, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
		return c.controls.SetAutoCompactionEnabled(ctx, serverapi.RuntimeSetAutoCompactionEnabledRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Enabled: enabled})
	})
	if err != nil {
		return false, false, err
	}
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.AutoCompactionEnabled = resp.Enabled
	})
	return resp.Changed, resp.Enabled, nil
}

func (c *sessionRuntimeClient) ShowGoal() (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.ShowGoal(ctx, serverapi.RuntimeGoalShowRequest{SessionID: c.sessionID})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func (c *sessionRuntimeClient) SetGoal(objective string) (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.SetGoal(ctx, serverapi.RuntimeGoalSetRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Objective: objective, Actor: "user"})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func (c *sessionRuntimeClient) PauseGoal() (*clientui.RuntimeGoal, error) {
	return c.setGoalStatus(func(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.PauseGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ResumeGoal() (*clientui.RuntimeGoal, error) {
	return c.setGoalStatus(func(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.ResumeGoal(ctx, req)
	})
}

func (c *sessionRuntimeClient) ClearGoal() (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeGoalShowResponse, error) {
		return c.controls.ClearGoal(ctx, serverapi.RuntimeGoalClearRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Actor: "user"})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func (c *sessionRuntimeClient) setGoalStatus(call func(context.Context, serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error)) (*clientui.RuntimeGoal, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeGoalShowResponse, error) {
		return call(ctx, serverapi.RuntimeGoalStatusRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Actor: "user"})
	})
	if err != nil {
		return nil, err
	}
	goal := runtimeGoalFromAPI(resp.Goal)
	c.patchMainView(func(view *clientui.RuntimeMainView) {
		view.Status.Goal = cloneRuntimeGoal(goal)
	})
	return goal, nil
}

func runtimeGoalFromAPI(goal *serverapi.RuntimeGoal) *clientui.RuntimeGoal {
	if goal == nil {
		return nil
	}
	return &clientui.RuntimeGoal{
		ID:        goal.ID,
		Objective: goal.Objective,
		Status:    clientui.RuntimeGoalStatus(strings.TrimSpace(goal.Status)),
		Suspended: goal.Suspended,
	}
}

func cloneRuntimeGoal(goal *clientui.RuntimeGoal) *clientui.RuntimeGoal {
	if goal == nil {
		return nil
	}
	cloned := *goal
	return &cloned
}

func (c *sessionRuntimeClient) AppendLocalEntry(role, text string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.AppendLocalEntry(ctx, serverapi.RuntimeAppendLocalEntryRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Role: role, Text: text})
	})
}

func (c *sessionRuntimeClient) SubmitUserMessage(ctx context.Context, text string) (string, error) {
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSubmitUserTurnResponse, error) {
		return c.controls.SubmitUserTurn(ctx, serverapi.RuntimeSubmitUserTurnRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Text: text})
	})
	return resp.Message, err
}

func (c *sessionRuntimeClient) SubmitUserShellCommand(ctx context.Context, command string) error {
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.SubmitUserShellCommand(ctx, serverapi.RuntimeSubmitUserShellCommandRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Command: command})
	})
}

func (c *sessionRuntimeClient) CompactContext(ctx context.Context, args string) error {
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.CompactContext(ctx, serverapi.RuntimeCompactContextRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Args: args})
	})
}

func (c *sessionRuntimeClient) HasQueuedUserWork() (bool, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeUnavailableCall(ctx, c.recoverControllerLeaseSilently, func() (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
		return c.controls.HasQueuedUserWork(ctx, serverapi.RuntimeHasQueuedUserWorkRequest{SessionID: c.sessionID})
	})
	if err != nil {
		return false, err
	}
	return resp.HasQueuedUserWork, nil
}

func (c *sessionRuntimeClient) SubmitQueuedUserMessages(ctx context.Context) (string, error) {
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
		return c.controls.SubmitQueuedUserMessages(ctx, serverapi.RuntimeSubmitQueuedUserMessagesRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID})
	})
	return resp.Message, err
}

func (c *sessionRuntimeClient) Interrupt() error {
	ctx, cancel := c.controlContext()
	defer cancel()
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.Interrupt(ctx, serverapi.RuntimeInterruptRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID})
	})
}

func (c *sessionRuntimeClient) QueueUserMessage(text string) (clientui.QueuedUserMessage, error) {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeQueueUserMessageResponse, error) {
		return c.controls.QueueUserMessage(ctx, serverapi.RuntimeQueueUserMessageRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Text: text})
	})
	if err != nil {
		c.notifyConnectionState(err)
		return clientui.QueuedUserMessage{}, err
	}
	return clientui.QueuedUserMessage{ID: resp.QueueItemID, Text: resp.Text}, nil
}

func (c *sessionRuntimeClient) DiscardQueuedUserMessage(queueItemID string) bool {
	ctx, cancel := c.controlContext()
	defer cancel()
	resp, err := retryRuntimeControlCall(ctx, c.controllerLeaseIDValue, c.recoverControllerLease, func(controllerLeaseID string) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
		return c.controls.DiscardQueuedUserMessage(ctx, serverapi.RuntimeDiscardQueuedUserMessageRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, QueueItemID: queueItemID})
	})
	if err != nil {
		return false
	}
	return resp.Discarded
}

func (c *sessionRuntimeClient) RecordPromptHistory(text string) error {
	ctx, cancel := c.controlContext()
	defer cancel()
	return c.retryControlCallNoResult(ctx, func(controllerLeaseID string) error {
		return c.controls.RecordPromptHistory(ctx, serverapi.RuntimeRecordPromptHistoryRequest{ClientRequestID: uuid.NewString(), SessionID: c.sessionID, ControllerLeaseID: controllerLeaseID, Text: text})
	})
}
