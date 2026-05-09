package primaryrun

import (
	"context"
	"strings"

	"builder/shared/clientui"
)

type gatedRuntimeClient struct {
	sessionID string
	inner     clientui.RuntimeClient
	gate      Gate
}

func NewGatedRuntimeClient(sessionID string, inner clientui.RuntimeClient, gate Gate) clientui.RuntimeClient {
	if inner == nil {
		return nil
	}
	if gate == nil || strings.TrimSpace(sessionID) == "" {
		return inner
	}
	return &gatedRuntimeClient{sessionID: strings.TrimSpace(sessionID), inner: inner, gate: gate}
}

func (c *gatedRuntimeClient) MainView() clientui.RuntimeMainView { return c.inner.MainView() }
func (c *gatedRuntimeClient) RefreshMainView() (clientui.RuntimeMainView, error) {
	return c.inner.RefreshMainView()
}
func (c *gatedRuntimeClient) Transcript() clientui.TranscriptPage { return c.inner.Transcript() }
func (c *gatedRuntimeClient) RefreshTranscript() (clientui.TranscriptPage, error) {
	return c.inner.RefreshTranscript()
}
func (c *gatedRuntimeClient) RefreshTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return c.inner.RefreshTranscriptPage(req)
}
func (c *gatedRuntimeClient) LoadTranscriptPage(req clientui.TranscriptPageRequest) (clientui.TranscriptPage, error) {
	return c.inner.LoadTranscriptPage(req)
}
func (c *gatedRuntimeClient) Status() clientui.RuntimeStatus           { return c.inner.Status() }
func (c *gatedRuntimeClient) SessionView() clientui.RuntimeSessionView { return c.inner.SessionView() }
func (c *gatedRuntimeClient) SetSessionName(name string) error         { return c.inner.SetSessionName(name) }
func (c *gatedRuntimeClient) SetThinkingLevel(level string) error {
	return c.inner.SetThinkingLevel(level)
}
func (c *gatedRuntimeClient) SetFastModeEnabled(enabled bool) (bool, error) {
	return c.inner.SetFastModeEnabled(enabled)
}
func (c *gatedRuntimeClient) SetReviewerEnabled(enabled bool) (bool, string, error) {
	return c.inner.SetReviewerEnabled(enabled)
}
func (c *gatedRuntimeClient) SetAutoCompactionEnabled(enabled bool) (bool, bool, error) {
	return c.inner.SetAutoCompactionEnabled(enabled)
}
func (c *gatedRuntimeClient) ShowGoal() (*clientui.RuntimeGoal, error) { return c.inner.ShowGoal() }
func (c *gatedRuntimeClient) SetGoal(objective string) (*clientui.RuntimeGoal, error) {
	return c.inner.SetGoal(objective)
}
func (c *gatedRuntimeClient) PauseGoal() (*clientui.RuntimeGoal, error)  { return c.inner.PauseGoal() }
func (c *gatedRuntimeClient) ResumeGoal() (*clientui.RuntimeGoal, error) { return c.inner.ResumeGoal() }
func (c *gatedRuntimeClient) ClearGoal() (*clientui.RuntimeGoal, error)  { return c.inner.ClearGoal() }
func (c *gatedRuntimeClient) AppendLocalEntry(role, text string) error {
	return c.inner.AppendLocalEntry(role, text)
}
func (c *gatedRuntimeClient) SubmitUserMessage(ctx context.Context, text string) (string, error) {
	lease, err := c.gate.AcquirePrimaryRun(c.sessionID)
	if err != nil {
		return "", err
	}
	defer lease.Release()
	return c.inner.SubmitUserMessage(ctx, text)
}

func (c *gatedRuntimeClient) SubmitUserShellCommand(ctx context.Context, command string) error {
	lease, err := c.gate.AcquirePrimaryRun(c.sessionID)
	if err != nil {
		return err
	}
	defer lease.Release()
	return c.inner.SubmitUserShellCommand(ctx, command)
}

func (c *gatedRuntimeClient) CompactContext(ctx context.Context, args string) error {
	return c.inner.CompactContext(ctx, args)
}

func (c *gatedRuntimeClient) HasQueuedUserWork() (bool, error) { return c.inner.HasQueuedUserWork() }

func (c *gatedRuntimeClient) SubmitQueuedUserMessages(ctx context.Context) (string, error) {
	lease, err := c.gate.AcquirePrimaryRun(c.sessionID)
	if err != nil {
		return "", err
	}
	defer lease.Release()
	return c.inner.SubmitQueuedUserMessages(ctx)
}

func (c *gatedRuntimeClient) Interrupt() error { return c.inner.Interrupt() }
func (c *gatedRuntimeClient) QueueUserMessage(text string) (clientui.QueuedUserMessage, error) {
	return c.inner.QueueUserMessage(text)
}
func (c *gatedRuntimeClient) DiscardQueuedUserMessage(queueItemID string) bool {
	return c.inner.DiscardQueuedUserMessage(queueItemID)
}
func (c *gatedRuntimeClient) RecordPromptHistory(text string) error {
	return c.inner.RecordPromptHistory(text)
}

var _ clientui.RuntimeClient = (*gatedRuntimeClient)(nil)
