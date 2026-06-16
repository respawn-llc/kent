package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/shared/transcript"
)

const missingToolOutputRepairWarningTemplate = "Transcript history was rolled back %d calls to repair after interruption"

type missingToolOutputRepairResult struct {
	RemovedCalls int
	Changed      bool
	Rewrite      session.EventRewriteResult
}

type repairToolCallKind uint8

const (
	repairToolCallKindFunction repairToolCallKind = iota + 1
	repairToolCallKindCustom
)

type missingToolOutputRepairScan struct {
	boundarySeq int64

	calls                map[string]repairToolCallKind
	materializedOutputs  map[string]map[repairToolCallKind]struct{}
	toolCompletions      map[string]storedToolCompletion
	toolCompletionExists map[string]struct{}
}

func newMissingToolOutputRepairScan() *missingToolOutputRepairScan {
	return &missingToolOutputRepairScan{
		calls:                make(map[string]repairToolCallKind),
		materializedOutputs:  make(map[string]map[repairToolCallKind]struct{}),
		toolCompletions:      make(map[string]storedToolCompletion),
		toolCompletionExists: make(map[string]struct{}),
	}
}

func repairMissingToolOutputsInSessionStore(store *session.Store, stepID string) (missingToolOutputRepairResult, bool, error) {
	if store == nil {
		return missingToolOutputRepairResult{}, false, nil
	}
	scan := newMissingToolOutputRepairScan()
	analyze := func(evt session.Event) error {
		return scan.apply(evt)
	}
	repairable := map[string]struct{}{}
	transform := func(evt session.Event) (session.EventRewriteDecision, error) {
		return transformMissingToolOutputEvent(evt, scan.boundarySeq, repairable)
	}
	extra := func() ([]session.EventInput, error) {
		repairable = scan.unfinishedCalls()
		if len(repairable) == 0 {
			return nil, nil
		}
		return []session.EventInput{{
			Kind: "local_entry",
			Payload: storedLocalEntry{
				Visibility: transcript.EntryVisibilityAll,
				Role:       string(transcript.EntryRoleDeveloperErrorFeedback),
				Text:       formatMissingToolOutputRepairWarning(len(repairable)),
			},
		}}, nil
	}
	rewrite, committed, err := store.AnalyzeAndRewriteEvents(stepID, analyze, transform, extra)
	return missingToolOutputRepairResult{
		RemovedCalls: len(repairable),
		Changed:      rewrite.Changed,
		Rewrite:      rewrite,
	}, committed, err
}

func (e *Engine) repairMissingToolOutputsAfterHTTP400(stepID string) (missingToolOutputRepairResult, bool, error) {
	if e == nil || e.store == nil {
		return missingToolOutputRepairResult{}, false, nil
	}
	if e.pendingToolCallStartStore().Len() > 0 {
		return missingToolOutputRepairResult{}, false, nil
	}
	preRepairCommittedCount := e.CommittedTranscriptEntryCount()
	result, committed, err := repairMissingToolOutputsInSessionStore(e.store, stepID)
	if committed {
		err = errors.Join(err, e.steer(stepID, steerRepairReloadIntent(result.Rewrite, preRepairCommittedCount)))
	}
	return result, committed, err
}

func (e *Engine) reloadProjectionFromPersistedTranscriptAfterRepair() error {
	if e == nil || e.store == nil {
		return nil
	}
	chat := newChatStoreWithCWD(e.transcriptWorkingDir())
	diagnostics := newDiagnosticDedupeStore()
	requestCache := newRequestCacheTracker()
	compactionCount := 0
	lastWorkflowRunID := ""
	reminderIssued := e.store.Meta().CompactionSoonReminderIssued
	if err := e.store.WalkEvents(func(evt session.Event) error {
		switch strings.TrimSpace(evt.Kind) {
		case "message":
			var msg llm.Message
			if err := json.Unmarshal(evt.Payload, &msg); err != nil {
				return fmt.Errorf("decode message event: %w", err)
			}
			chat.appendMessage(msg)
			if isCompactionSoonReminderMessage(msg) {
				reminderIssued = true
			}
		case "tool_completed":
			if err := chat.restoreToolCompletionPayload(evt.Payload); err != nil {
				return err
			}
		case "local_entry":
			var entry storedLocalEntry
			if err := json.Unmarshal(evt.Payload, &entry); err != nil {
				return fmt.Errorf("decode local_entry event: %w", err)
			}
			diagnostics.RestoreLocal(entry.DiagnosticKey)
			chat.appendLocalEntryRecord(*localEntryChatEntry(entry))
		case sessionEventCacheWarning:
			if err := applyPersistedCacheWarningToChat(chat, evt.Payload, e.cfg.CacheWarningMode); err != nil {
				return err
			}
		case sessionEventCacheRequestObserved:
			var request persistedCacheRequestObserved
			if err := json.Unmarshal(evt.Payload, &request); err != nil {
				return fmt.Errorf("decode %s event: %w", sessionEventCacheRequestObserved, err)
			}
		case sessionEventCacheResponseObserved:
			var response persistedCacheResponseObserved
			if err := json.Unmarshal(evt.Payload, &response); err != nil {
				return fmt.Errorf("decode %s event: %w", sessionEventCacheResponseObserved, err)
			}
			requestCache.RecordResponse(response)
		case "history_replaced":
			payload, ignoredLegacy, err := decodePersistedHistoryReplacementPayload(evt.Payload)
			if err != nil {
				return fmt.Errorf("%w: %w", errDecodeHistoryReplacedEvent, err)
			}
			if ignoredLegacy {
				return nil
			}
			diagnostics.Reset()
			chat.replaceHistory(payload.Items)
			compactionCount++
			lastWorkflowRunID = strings.TrimSpace(payload.WorkflowRunID)
			reminderIssued = false
		}
		return nil
	}); err != nil {
		return err
	}

	e.transcriptRuntimeState().replaceChatProjection(chat)
	e.compactionRuntimeState().SetCount(compactionCount)
	e.setLastCompactionWorkflowRunID(lastWorkflowRunID)
	e.setCompactionSoonReminderIssued(reminderIssued)
	e.baseMetaInjected = len(chat.snapshotMessages()) > 0
	e.mu.Lock()
	e.diagnostics = diagnostics
	e.usageState = newUsageTrackingState()
	e.mu.Unlock()
	modelState := e.modelRequests()
	modelState.mu.Lock()
	modelState.requestCache = requestCache
	modelState.mu.Unlock()
	e.resetCurrentPreciseInputTracking()
	return errors.Join(
		e.store.SetCompactionSoonReminderIssued(reminderIssued),
		e.store.SetUsageState(nil),
	)
}

func formatMissingToolOutputRepairWarning(removedCalls int) string {
	return fmt.Sprintf(missingToolOutputRepairWarningTemplate, removedCalls)
}

func (s *missingToolOutputRepairScan) apply(evt session.Event) error {
	if s == nil {
		return nil
	}
	switch strings.TrimSpace(evt.Kind) {
	case "history_replaced":
		payload, ignoredLegacy, err := decodePersistedHistoryReplacementPayload(evt.Payload)
		if err != nil {
			return fmt.Errorf("%w: %w", errDecodeHistoryReplacedEvent, err)
		}
		if ignoredLegacy {
			return nil
		}
		_ = payload
		s.resetAtBoundary(evt.Seq)
	case "message":
		if evt.Seq <= s.boundarySeq {
			return nil
		}
		var msg llm.Message
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			return fmt.Errorf("decode message event: %w", err)
		}
		s.applyMessage(msg)
	case "tool_completed":
		if evt.Seq <= s.boundarySeq {
			return nil
		}
		var completion storedToolCompletion
		if err := json.Unmarshal(evt.Payload, &completion); err != nil {
			return fmt.Errorf("decode tool_completed event: %w", err)
		}
		callID := strings.TrimSpace(completion.CallID)
		if callID == "" {
			return nil
		}
		s.toolCompletions[callID] = completion
		s.toolCompletionExists[callID] = struct{}{}
	}
	return nil
}

func (s *missingToolOutputRepairScan) resetAtBoundary(seq int64) {
	s.boundarySeq = seq
	s.calls = make(map[string]repairToolCallKind)
	s.materializedOutputs = make(map[string]map[repairToolCallKind]struct{})
	s.toolCompletions = make(map[string]storedToolCompletion)
	s.toolCompletionExists = make(map[string]struct{})
}

func (s *missingToolOutputRepairScan) applyMessage(msg llm.Message) {
	switch msg.Role {
	case llm.RoleAssistant:
		for _, call := range msg.ToolCalls {
			callID := strings.TrimSpace(call.ID)
			if callID == "" {
				continue
			}
			s.calls[callID] = repairKindForToolCall(call)
		}
	case llm.RoleTool:
		callID := strings.TrimSpace(msg.ToolCallID)
		if callID == "" {
			return
		}
		kind := repairToolCallKindFunction
		if msg.MessageType == llm.MessageTypeCustomToolCallOutput {
			kind = repairToolCallKindCustom
		}
		if s.materializedOutputs[callID] == nil {
			s.materializedOutputs[callID] = make(map[repairToolCallKind]struct{})
		}
		s.materializedOutputs[callID][kind] = struct{}{}
	}
}

func (s *missingToolOutputRepairScan) unfinishedCalls() map[string]struct{} {
	out := make(map[string]struct{})
	if s == nil {
		return out
	}
	for callID, callKind := range s.calls {
		if s.callCompleted(callID, callKind) {
			continue
		}
		out[callID] = struct{}{}
	}
	return out
}

func (s *missingToolOutputRepairScan) callCompleted(callID string, callKind repairToolCallKind) bool {
	if outputs := s.materializedOutputs[callID]; len(outputs) > 0 {
		_, ok := outputs[callKind]
		return ok
	}
	completion, ok := s.toolCompletions[callID]
	if !ok {
		return false
	}
	if len(completion.ProviderItems) == 0 {
		return true
	}
	for _, item := range completion.ProviderItems {
		if strings.TrimSpace(item.CallID) != callID {
			continue
		}
		if repairKindForOutputItem(item.Type) == callKind {
			return true
		}
	}
	return false
}

func repairKindForToolCall(call llm.ToolCall) repairToolCallKind {
	if call.Custom {
		return repairToolCallKindCustom
	}
	return repairToolCallKindFunction
}

func repairKindForOutputItem(itemType llm.ResponseItemType) repairToolCallKind {
	if itemType == llm.ResponseItemTypeCustomToolOutput {
		return repairToolCallKindCustom
	}
	if itemType == llm.ResponseItemTypeFunctionCallOutput {
		return repairToolCallKindFunction
	}
	return 0
}

func transformMissingToolOutputEvent(evt session.Event, boundarySeq int64, repairable map[string]struct{}) (session.EventRewriteDecision, error) {
	if len(repairable) == 0 || evt.Seq <= boundarySeq || strings.TrimSpace(evt.Kind) != "message" {
		return session.EventRewriteDecision{Event: evt}, nil
	}
	var msg llm.Message
	if err := json.Unmarshal(evt.Payload, &msg); err != nil {
		return session.EventRewriteDecision{}, fmt.Errorf("decode message event: %w", err)
	}
	switch msg.Role {
	case llm.RoleAssistant:
		filtered := msg.ToolCalls[:0]
		for _, call := range msg.ToolCalls {
			if _, remove := repairable[strings.TrimSpace(call.ID)]; remove {
				continue
			}
			filtered = append(filtered, call)
		}
		msg.ToolCalls = filtered
		if strings.TrimSpace(msg.Content) == "" &&
			strings.TrimSpace(msg.CompactContent) == "" &&
			len(msg.ReasoningItems) == 0 &&
			len(msg.ToolCalls) == 0 {
			return session.EventRewriteDecision{Drop: true}, nil
		}
	case llm.RoleTool:
		if _, remove := repairable[strings.TrimSpace(msg.ToolCallID)]; remove {
			return session.EventRewriteDecision{Drop: true}, nil
		}
	default:
		return session.EventRewriteDecision{Event: evt}, nil
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return session.EventRewriteDecision{}, fmt.Errorf("marshal message event: %w", err)
	}
	evt.Payload = payload
	return session.EventRewriteDecision{Event: evt}, nil
}
