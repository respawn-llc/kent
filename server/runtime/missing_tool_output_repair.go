package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/shared/transcript"
)

const missingToolOutputRepairWarningTemplate = "Transcript history was rolled back %d calls to repair after interruption"

type missingToolOutputRepairResult struct {
	RemovedCalls   int
	RemovedIDs     []string
	RemovedCallIDs []string
	Changed        bool
	Rewrite        session.EventRewriteResult
}

type missingToolOutputRepairPlan struct {
	removeCalls map[string]struct{}
	dropOutputs map[string]map[repairToolCallKind]struct{}
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
	plan := missingToolOutputRepairPlan{}
	transform := func(evt session.Event) (session.EventRewriteDecision, error) {
		return transformMissingToolOutputEvent(evt, scan.boundarySeq, plan)
	}
	extra := func() ([]session.EventInput, error) {
		plan = scan.repairPlan()
		if plan.affectedCalls() == 0 {
			return nil, nil
		}
		return []session.EventInput{{
			Kind: "local_entry",
			Payload: storedLocalEntry{
				Visibility: transcript.EntryVisibilityAll,
				Role:       string(transcript.EntryRoleDeveloperErrorFeedback),
				Text:       formatMissingToolOutputRepairWarning(plan.affectedCalls()),
			},
		}}, nil
	}
	rewrite, committed, err := store.AnalyzeAndRewriteEventsAfterLatestBoundary(stepID, missingToolOutputRepairBoundary, analyze, transform, extra)
	removedIDs := sortedMissingToolOutputRepairCallIDs(plan.affectedCallIDs())
	return missingToolOutputRepairResult{
		RemovedCalls:   plan.affectedCalls(),
		RemovedIDs:     removedIDs,
		RemovedCallIDs: sortedMissingToolOutputRepairCallIDs(plan.removeCalls),
		Changed:        rewrite.Changed,
		Rewrite:        rewrite,
	}, committed, err
}

func missingToolOutputRepairBoundary(evt session.Event) (bool, error) {
	if strings.TrimSpace(evt.Kind) != "history_replaced" {
		return false, nil
	}
	_, ignoredLegacy, err := decodePersistedHistoryReplacementPayload(evt.Payload)
	if err != nil {
		return false, fmt.Errorf("%w: %w", errDecodeHistoryReplacedEvent, err)
	}
	return !ignoredLegacy, nil
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
		err = errors.Join(err, e.steer(stepID, steerRepairReloadIntent(result.Rewrite, result.RemovedIDs, result.RemovedCallIDs, preRepairCommittedCount)))
	}
	return result, committed, err
}

func (e *Engine) applyMissingToolOutputRepairProjection(result steeringRepairReload) error {
	if e == nil || e.store == nil {
		return nil
	}
	chat := e.transcriptRuntimeState().chatProjection()
	if chat == nil {
		return nil
	}
	chat.applyMissingToolOutputRepair(result.removedCallIDs, result.removedToolCallIDs, result.rewrite.AppendedEvents)
	e.mu.Lock()
	e.usageState = newUsageTrackingState()
	e.mu.Unlock()
	e.resetCurrentPreciseInputTracking()
	return e.store.SetUsageState(nil)
}

func sortedMissingToolOutputRepairCallIDs(ids map[string]struct{}) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func responseItemsAfterMissingToolOutputRepair(items []llm.ResponseItem, affectedCallIDs []string, removedCallIDs []string) []llm.ResponseItem {
	if len(items) == 0 || (len(affectedCallIDs) == 0 && len(removedCallIDs) == 0) {
		return llm.CloneResponseItems(items)
	}
	removed := make(map[string]struct{}, len(removedCallIDs))
	for _, id := range removedCallIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			removed[trimmed] = struct{}{}
		}
	}
	affected := make(map[string]struct{}, len(affectedCallIDs))
	for _, id := range affectedCallIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			affected[trimmed] = struct{}{}
		}
	}
	if len(removed) == 0 && len(affected) == 0 {
		return llm.CloneResponseItems(items)
	}
	callKinds := make(map[string]repairToolCallKind)
	for _, item := range items {
		if !isToolCallItem(item.Type) {
			continue
		}
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			callID = strings.TrimSpace(item.ID)
		}
		if callID != "" {
			callKinds[callID] = repairKindForOutputItem(llm.ToolOutputItemType(item.Type == llm.ResponseItemTypeCustomToolCall))
		}
	}
	out := make([]llm.ResponseItem, 0, len(items))
	for _, item := range items {
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			callID = strings.TrimSpace(item.ID)
		}
		if _, remove := removed[callID]; remove && (isToolCallItem(item.Type) || isToolOutputItem(item.Type)) {
			continue
		}
		if _, repairOutput := affected[callID]; repairOutput && isToolOutputItem(item.Type) {
			if callKind := callKinds[callID]; callKind != 0 && repairKindForOutputItem(item.Type) != callKind {
				continue
			}
		}
		out = append(out, llm.CloneResponseItems([]llm.ResponseItem{item})...)
	}
	return out
}

func localEntriesFromRepairAppendedEvents(events []session.Event) []ChatEntry {
	if len(events) == 0 {
		return nil
	}
	out := make([]ChatEntry, 0, len(events))
	for _, evt := range events {
		if strings.TrimSpace(evt.Kind) != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(evt.Payload, &entry); err == nil {
			if chatEntry := localEntryChatEntry(entry); chatEntry != nil {
				out = append(out, *chatEntry)
			}
		}
	}
	return out
}

func (s *chatStore) applyMissingToolOutputRepair(affectedCallIDs []string, removedCallIDs []string, appendedEvents []session.Event) {
	if s == nil {
		return
	}
	appendedEntries := localEntriesFromRepairAppendedEvents(appendedEvents)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(affectedCallIDs) > 0 || len(removedCallIDs) > 0 {
		s.items = responseItemsAfterMissingToolOutputRepair(s.items, affectedCallIDs, removedCallIDs)
		repairedMessageCount := len(llm.MessagesFromItems(s.items))
		if s.compact != nil {
			if s.compact.CutoffMessageCount > repairedMessageCount {
				s.compact.CutoffMessageCount = repairedMessageCount
			}
		}
		for idx := range s.local {
			if s.local[idx].AfterMessageCount > repairedMessageCount {
				s.local[idx].AfterMessageCount = repairedMessageCount
			}
		}
		s.messageCount = repairedMessageCount
		for _, id := range removedCallIDs {
			delete(s.assistantToolCalls, id)
			delete(s.materializedToolResults, id)
			delete(s.synthesizedToolResults, id)
			delete(s.toolCompletions, id)
			delete(s.toolCompletionProviderItems, id)
		}
		s.lastCommittedAssistantFinalAnswer = lastCommittedAssistantFinalAnswerFromItems(s.providerItemsSourceLocked())
		s.providerTokenEstimateDirty = true
	}
	for _, entry := range appendedEntries {
		if strings.TrimSpace(entry.Text) == "" {
			continue
		}
		entry.Visibility = transcript.NormalizeEntryVisibility(entry.Visibility)
		entry.OngoingText = strings.TrimSpace(entry.OngoingText)
		entry.NoticeID = strings.TrimSpace(entry.NoticeID)
		s.local = append(s.local, localChatEntry{
			Entry:             entry,
			AfterMessageCount: s.messageCount,
		})
		s.transcriptEntryCount++
	}
}

func lastCommittedAssistantFinalAnswerFromItems(items []llm.ResponseItem) string {
	messages := llm.MessagesFromItems(items)
	last := ""
	for _, msg := range messages {
		if messagePreservesLastCommittedAssistantFinalAnswer(msg) || isNoopFinalAnswer(msg) {
			continue
		}
		if msg.Role == llm.RoleAssistant && msg.Phase == llm.MessagePhaseFinal && strings.TrimSpace(msg.Content) != "" {
			last = msg.Content
			continue
		}
		last = ""
	}
	return last
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

func (s *missingToolOutputRepairScan) repairPlan() missingToolOutputRepairPlan {
	plan := missingToolOutputRepairPlan{
		removeCalls: make(map[string]struct{}),
		dropOutputs: make(map[string]map[repairToolCallKind]struct{}),
	}
	if s == nil {
		return plan
	}
	for callID, callKind := range s.calls {
		outputs := s.materializedOutputs[callID]
		for outputKind := range outputs {
			if outputKind == callKind {
				continue
			}
			if plan.dropOutputs[callID] == nil {
				plan.dropOutputs[callID] = make(map[repairToolCallKind]struct{})
			}
			plan.dropOutputs[callID][outputKind] = struct{}{}
		}
		if s.callCompleted(callID, callKind) {
			continue
		}
		plan.removeCalls[callID] = struct{}{}
	}
	return plan
}

func (p missingToolOutputRepairPlan) affectedCallIDs() map[string]struct{} {
	out := make(map[string]struct{})
	for callID := range p.removeCalls {
		out[callID] = struct{}{}
	}
	for callID, kinds := range p.dropOutputs {
		if len(kinds) == 0 {
			continue
		}
		out[callID] = struct{}{}
	}
	return out
}

func (p missingToolOutputRepairPlan) affectedCalls() int {
	return len(p.affectedCallIDs())
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

func transformMissingToolOutputEvent(evt session.Event, boundarySeq int64, plan missingToolOutputRepairPlan) (session.EventRewriteDecision, error) {
	if plan.affectedCalls() == 0 || evt.Seq <= boundarySeq || strings.TrimSpace(evt.Kind) != "message" {
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
			if _, remove := plan.removeCalls[strings.TrimSpace(call.ID)]; remove {
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
		callID := strings.TrimSpace(msg.ToolCallID)
		if _, remove := plan.removeCalls[callID]; remove {
			return session.EventRewriteDecision{Drop: true}, nil
		}
		kind := repairToolCallKindFunction
		if msg.MessageType == llm.MessageTypeCustomToolCallOutput {
			kind = repairToolCallKindCustom
		}
		if _, remove := plan.dropOutputs[callID][kind]; remove {
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
