package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"builder/server/llm"
	"builder/server/session"
	"builder/shared/toolspec"
	"builder/shared/transcript"
)

type defaultMessageLifecycle struct {
	engine     *Engine
	background backgroundNoticeScheduler
}

func (m *defaultMessageLifecycle) RestoreMessages() error {
	e := m.engine
	meta := e.store.Meta()
	recoveredHandoff := newPersistedHandoffRecovery()
	reminderIssued := meta.CompactionSoonReminderIssued
	if err := e.store.WalkEvents(func(evt session.Event) error {
		switch evt.Kind {
		case "message":
			var msg llm.Message
			if err := json.Unmarshal(evt.Payload, &msg); err != nil {
				return fmt.Errorf("decode message event: %w", err)
			}
			e.chat.appendMessage(msg)
			recoveredHandoff.ApplyMessage(msg)
			if isCompactionSoonReminderMessage(msg) {
				reminderIssued = true
			}
		case "tool_completed":
			if err := e.chat.restoreToolCompletionPayload(evt.Payload); err != nil {
				return err
			}
			if err := recoveredHandoff.ApplyToolCompletion(evt.Payload); err != nil {
				return err
			}
		case "local_entry":
			var entry storedLocalEntry
			if err := json.Unmarshal(evt.Payload, &entry); err != nil {
				return fmt.Errorf("decode local_entry event: %w", err)
			}
			e.restoreLocalDiagnostic(entry.DiagnosticKey)
			e.chat.appendLocalEntryWithOngoingTextAndVisibility(entry.Role, entry.Text, entry.OngoingText, entry.Visibility)
		case sessionEventCacheWarning:
			if err := applyPersistedCacheWarningToChat(e.chat, evt.Payload, e.cfg.CacheWarningMode); err != nil {
				return err
			}
		case sessionEventCacheRequestObserved:
			if err := e.restorePromptCacheRequest(evt.Payload); err != nil {
				return err
			}
		case sessionEventCacheResponseObserved:
			if err := e.restorePromptCacheResponse(evt.Payload); err != nil {
				return err
			}
		case "history_replaced":
			payload, ignoredLegacy, err := decodePersistedHistoryReplacementPayload(evt.Payload)
			if err != nil {
				return fmt.Errorf("decode history_replaced event: %w", err)
			}
			if ignoredLegacy {
				return nil
			}
			e.resetLocalDiagnostics()
			e.chat.replaceHistory(payload.Items)
			e.compactionCount++
			recoveredHandoff.ClearSatisfiedByCompaction()
			reminderIssued = false
		}
		return nil
	}); err != nil {
		return err
	}
	e.setCompactionSoonReminderIssued(reminderIssued)
	if err := e.store.SetCompactionSoonReminderIssued(reminderIssued); err != nil {
		return err
	}
	if futureMessage := recoveredHandoff.PendingFutureMessage(); futureMessage != "" {
		e.queuePendingHandoffFutureMessage(futureMessage)
	}
	if req, ok := recoveredHandoff.PendingRequest(); ok {
		e.queueHandoffRequest(req.summarizerPrompt, req.futureAgentMessage)
	}
	return nil
}

func isCompactionSoonReminderMessage(msg llm.Message) bool {
	return msg.Role == llm.RoleDeveloper && msg.MessageType == llm.MessageTypeCompactionSoonReminder && strings.TrimSpace(msg.Content) != ""
}

func itemsContainCompactionSoonReminder(items []llm.ResponseItem) bool {
	for _, msg := range llm.MessagesFromItems(items) {
		if isCompactionSoonReminderMessage(msg) {
			return true
		}
	}
	return false
}

type persistedHandoffRecovery struct {
	toolCalls            map[string]llm.ToolCall
	pending              *handoffRequest
	pendingFutureMessage string
}

func newPersistedHandoffRecovery() *persistedHandoffRecovery {
	return &persistedHandoffRecovery{toolCalls: make(map[string]llm.ToolCall)}
}

func (r *persistedHandoffRecovery) ApplyMessage(msg llm.Message) {
	if r == nil {
		return
	}
	if msg.MessageType == llm.MessageTypeHandoffFutureMessage && strings.TrimSpace(msg.Content) != "" {
		r.pendingFutureMessage = ""
	}
	if msg.Role != llm.RoleAssistant {
		return
	}
	for _, call := range msg.ToolCalls {
		if toolspec.ID(strings.TrimSpace(call.Name)) != toolspec.ToolTriggerHandoff {
			continue
		}
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			continue
		}
		r.toolCalls[callID] = llm.ToolCall{
			ID:    callID,
			Name:  string(toolspec.ToolTriggerHandoff),
			Input: append(json.RawMessage(nil), call.Input...),
		}
	}
}

func (r *persistedHandoffRecovery) ApplyToolCompletion(payload []byte) error {
	if r == nil {
		return nil
	}
	var completion storedToolCompletion
	if err := json.Unmarshal(payload, &completion); err != nil {
		return fmt.Errorf("decode tool_completed event: %w", err)
	}
	if toolspec.ID(strings.TrimSpace(completion.Name)) != toolspec.ToolTriggerHandoff || completion.IsError {
		delete(r.toolCalls, strings.TrimSpace(completion.CallID))
		return nil
	}
	callID := strings.TrimSpace(completion.CallID)
	if callID == "" {
		return nil
	}
	call, ok := r.toolCalls[callID]
	if !ok {
		return nil
	}
	delete(r.toolCalls, callID)
	req, ok := handoffRequestFromToolCall(call)
	if !ok {
		return nil
	}
	r.pending = req
	return nil
}

func (r *persistedHandoffRecovery) ClearSatisfiedByCompaction() {
	if r == nil {
		return
	}
	if r.pending != nil {
		if futureMessage := strings.TrimSpace(r.pending.futureAgentMessage); futureMessage != "" {
			r.pendingFutureMessage = futureMessage
		}
	}
	r.pending = nil
	r.toolCalls = make(map[string]llm.ToolCall)
}

func (r *persistedHandoffRecovery) PendingFutureMessage() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.pendingFutureMessage)
}

func (r *persistedHandoffRecovery) PendingRequest() (*handoffRequest, bool) {
	if r == nil || r.pending == nil {
		return nil, false
	}
	req := *r.pending
	return &req, true
}

func handoffRequestFromToolCall(call llm.ToolCall) (*handoffRequest, bool) {
	if toolspec.ID(strings.TrimSpace(call.Name)) != toolspec.ToolTriggerHandoff {
		return nil, false
	}
	var input struct {
		SummarizerPrompt   string `json:"summarizer_prompt,omitempty"`
		FutureAgentMessage string `json:"future_agent_message,omitempty"`
	}
	if len(call.Input) > 0 {
		if err := json.Unmarshal(call.Input, &input); err != nil {
			return nil, false
		}
	}
	return &handoffRequest{
		summarizerPrompt:   strings.TrimSpace(input.SummarizerPrompt),
		futureAgentMessage: strings.TrimSpace(input.FutureAgentMessage),
	}, true
}

func normalizeQueuedUserMessages(messages []QueuedUserMessage) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		trimmed := strings.TrimSpace(message.Text)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func queuedUserMessageIDs(messages []QueuedUserMessage) []string {
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		id := strings.TrimSpace(message.ID)
		if id == "" || strings.TrimSpace(message.Text) == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (m *defaultMessageLifecycle) FlushPendingUserInjections(stepID string) (int, error) {
	e := m.engine
	e.mu.Lock()
	pending := append([]QueuedUserMessage(nil), e.pendingInjected...)
	e.pendingInjected = nil
	e.mu.Unlock()
	flushed := 0
	pendingNotices := []llm.Message(nil)
	if m.background != nil {
		pendingNotices = m.background.DrainPendingNotices()
	}

	queuedMessages := normalizeQueuedUserMessages(pending)
	if len(queuedMessages) > 0 {
		joined := strings.Join(queuedMessages, "\n\n")
		if err := e.appendUserMessageWithoutConversationUpdate(stepID, joined); err != nil {
			return flushed, err
		}
		flushed++
		e.emit(Event{Kind: EventUserMessageFlushed, StepID: stepID, UserMessage: joined, UserMessageBatch: queuedMessages, UserMessageBatchQueueItemIDs: queuedUserMessageIDs(pending), CommittedTranscriptChanged: true})
	}
	for _, notice := range pendingNotices {
		if err := e.appendMessage(stepID, notice); err != nil {
			return flushed, err
		}
		flushed++
	}
	return flushed, nil
}

func (m *defaultMessageLifecycle) InjectAgentsIfNeeded(stepID string) error {
	e := m.engine
	meta := e.store.Meta()
	if meta.AgentsInjected {
		return nil
	}
	builder := newActiveMetaContextBuilder(meta, e.cfg.Model, e.ThinkingLevel(), e.cfg.DisabledSkills, time.Now())
	metaResult, err := builder.Build(metaContextBuildOptions{
		IncludeAgents:        true,
		IncludeSkills:        true,
		IncludeEnvironment:   true,
		IncludeSkillWarnings: true,
	})
	if err != nil {
		return err
	}
	for _, warning := range metaResult.SkillWarnings {
		if err := e.appendPersistedLocalEntryRecord(stepID, storedLocalEntry{
			Visibility: transcript.EntryVisibilityAll,
			Role:       "warning",
			Text:       warning,
		}); err != nil {
			return err
		}
	}
	for _, message := range metaResult.OrderedInjectionMessages() {
		if err := e.appendMessage(stepID, message); err != nil {
			return err
		}
	}

	return e.store.MarkAgentsInjected()
}

func newActiveMetaContextBuilder(meta session.Meta, model, thinkingLevel string, disabledSkills map[string]bool, now time.Time) metaContextBuilder {
	roots := activeMetaContextRootsForMeta(meta)
	return newMetaContextBuilder(roots.discoveryRoot, model, thinkingLevel, disabledSkills, now).withEnvironmentCWD(roots.environmentCWD)
}

type activeMetaContextRoots struct {
	discoveryRoot  string
	environmentCWD string
}

func activeMetaContextRootsForMeta(meta session.Meta) activeMetaContextRoots {
	workspaceRoot := strings.TrimSpace(meta.WorkspaceRoot)
	roots := activeMetaContextRoots{discoveryRoot: workspaceRoot, environmentCWD: workspaceRoot}
	state := cloneRuntimeWorktreeReminderState(meta.WorktreeReminder)
	if state == nil {
		return roots
	}

	switch state.Mode {
	case session.WorktreeReminderModeEnter:
		if state.WorktreePath != "" {
			roots.discoveryRoot = state.WorktreePath
		}
	case session.WorktreeReminderModeExit:
		if state.WorkspaceRoot != "" {
			roots.discoveryRoot = state.WorkspaceRoot
		}
	}
	if state.EffectiveCwd != "" {
		roots.environmentCWD = state.EffectiveCwd
	} else {
		roots.environmentCWD = roots.discoveryRoot
	}
	return roots
}
