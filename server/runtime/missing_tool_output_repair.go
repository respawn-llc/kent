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
	removeCalls       map[string]struct{}
	dedupeCalls       map[string]struct{}
	keptCalls         map[string]struct{}
	dropOutputs       map[string]map[repairToolCallKind]int
	dedupeOutputs     map[string]map[repairToolCallKind]struct{}
	filterCompletions map[string]repairToolCallKind
	keptOutputs       map[string]map[repairToolCallKind]struct{}
}

type repairToolCallKind uint8

const (
	repairToolCallKindFunction repairToolCallKind = iota + 1
	repairToolCallKindCustom
)

type missingToolOutputRepairScan struct {
	boundarySeq int64

	calls                map[string]repairToolCallKind
	callCounts           map[string]int
	seenCalls            map[string]struct{}
	materializedOutputs  map[string]map[repairToolCallKind]int
	invalidOutputs       map[string]map[repairToolCallKind]int
	toolCompletions      map[string]storedToolCompletion
	toolCompletionExists map[string]struct{}
}

func newMissingToolOutputRepairScan() *missingToolOutputRepairScan {
	return &missingToolOutputRepairScan{
		calls:                make(map[string]repairToolCallKind),
		callCounts:           make(map[string]int),
		seenCalls:            make(map[string]struct{}),
		materializedOutputs:  make(map[string]map[repairToolCallKind]int),
		invalidOutputs:       make(map[string]map[repairToolCallKind]int),
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
				Text:       fmt.Sprintf(missingToolOutputRepairWarningTemplate, plan.affectedCalls()),
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
	result, committed, err := repairMissingToolOutputsInSessionStore(e.store, stepID)
	if committed {
		err = errors.Join(err, e.steer(stepID, steerRepairInvalidationIntent(result.Rewrite, result.RemovedIDs, result.RemovedCallIDs)))
	}
	return result, committed, err
}

func (e *Engine) applyMissingToolOutputRepairProjection(result steeringRepairInvalidation) error {
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
			if _, exists := callKinds[callID]; exists {
				continue
			}
			callKinds[callID] = repairKindForOutputItem(llm.ToolOutputItemType(item.Type == llm.ResponseItemTypeCustomToolCall))
		}
	}
	out := make([]llm.ResponseItem, 0, len(items))
	seenCalls := make(map[string]struct{})
	seenOutputs := make(map[string]map[repairToolCallKind]struct{})
	for _, item := range items {
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			callID = strings.TrimSpace(item.ID)
		}
		if isProviderOutputAttachmentForAnyCall(item, removed) {
			continue
		}
		if _, remove := removed[callID]; remove && (isToolCallItem(item.Type) || isToolOutputItem(item.Type)) {
			continue
		}
		if isToolCallItem(item.Type) {
			if _, repairCall := affected[callID]; repairCall {
				if _, seen := seenCalls[callID]; seen {
					continue
				}
			}
			seenCalls[callID] = struct{}{}
		}
		if isToolOutputItem(item.Type) && len(affected) > 0 {
			callKind := callKinds[callID]
			outputKind := repairKindForOutputItem(item.Type)
			if callKind == 0 {
				continue
			}
			if _, seenCall := seenCalls[callID]; !seenCall {
				continue
			}
			if _, repairOutput := affected[callID]; repairOutput && outputKind != callKind {
				continue
			}
			if seenOutputs[callID] == nil {
				seenOutputs[callID] = make(map[repairToolCallKind]struct{})
			}
			if _, seen := seenOutputs[callID][outputKind]; seen {
				continue
			}
			seenOutputs[callID][outputKind] = struct{}{}
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
		tailStart := 0
		if s.compact != nil {
			tailStart = s.compact.CutoffItemCount
			if tailStart < 0 {
				tailStart = 0
			}
			if tailStart > len(s.items) {
				tailStart = len(s.items)
			}
		}
		prefixMessageCount := len(llm.MessagesFromItems(s.items[:tailStart]))
		originalMessageCount := len(llm.MessagesFromItems(s.items))
		originalTail := llm.CloneResponseItems(s.items[tailStart:])
		repairedTail := responseItemsAfterMissingToolOutputRepair(originalTail, affectedCallIDs, removedCallIDs)
		s.items = append(llm.CloneResponseItems(s.items[:tailStart]), repairedTail...)
		repairedMessageCount := len(llm.MessagesFromItems(s.items))
		if s.compact != nil {
			if s.compact.CutoffMessageCount > repairedMessageCount {
				s.compact.CutoffMessageCount = repairedMessageCount
			}
		}
		for idx := range s.local {
			after := s.local[idx].AfterMessageCount
			if originalMessageCount != repairedMessageCount && after > prefixMessageCount {
				after = prefixMessageCount + repairedTailMessageCountThroughOriginalCount(originalTail, affectedCallIDs, removedCallIDs, after-prefixMessageCount)
			}
			if after > repairedMessageCount {
				after = repairedMessageCount
			}
			s.local[idx].AfterMessageCount = after
		}
		s.messageCount = repairedMessageCount
		s.transcriptEntryCount = s.recomputeTranscriptEntryCountLocked()
		callKinds := repairCallKindsFromItems(s.providerItemsSourceLocked())
		for _, id := range affectedCallIDs {
			callID := strings.TrimSpace(id)
			if callID == "" || len(s.toolCompletionProviderItems[callID]) == 0 {
				continue
			}
			filtered := firstMatchingProviderOutput(s.toolCompletionProviderItems[callID], callID, callKinds[callID])
			if len(filtered) == 0 {
				delete(s.toolCompletionProviderItems, callID)
				continue
			}
			s.toolCompletionProviderItems[callID] = filtered
		}
		for _, id := range removedCallIDs {
			delete(s.assistantToolCalls, id)
			delete(s.materializedToolResults, id)
			delete(s.synthesizedToolResults, id)
			delete(s.toolCompletions, id)
			delete(s.toolCompletionProviderItems, id)
		}
		s.lastCommittedAssistantFinalAnswer = ""
		for _, msg := range llm.MessagesFromItems(s.providerItemsSourceLocked()) {
			s.applyLastCommittedAssistantFinalAnswerLocked(msg)
		}
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

func (s *chatStore) recomputeTranscriptEntryCountLocked() int {
	materializedToolResults := collectMaterializedToolCalls(s.items)
	scan := newInMemoryTranscriptScan(inMemoryTranscriptScanRequest{Offset: 0, Limit: 0}, s.toolCompletions, materializedToolResults)
	localIndex := 0
	appendLocalEntries := func(processedMessages int) {
		for localIndex < len(s.local) {
			if s.local[localIndex].AfterMessageCount > processedMessages {
				break
			}
			scan.appendEntry(s.local[localIndex].Entry)
			localIndex++
		}
	}
	appendLocalEntries(0)
	processedMessages := 0
	walker := newResponseItemMessageWalker(func(msg llm.Message) {
		scan.ApplyMessage(msg)
		processedMessages++
		appendLocalEntries(processedMessages)
	})
	for _, item := range s.items {
		walker.Apply(item)
	}
	walker.Flush()
	appendLocalEntries(processedMessages)
	return scan.totalEntries
}

func repairCallKindsFromItems(items []llm.ResponseItem) map[string]repairToolCallKind {
	out := make(map[string]repairToolCallKind)
	for _, item := range items {
		if !isToolCallItem(item.Type) {
			continue
		}
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			callID = strings.TrimSpace(item.ID)
		}
		if callID != "" {
			if _, exists := out[callID]; exists {
				continue
			}
			out[callID] = repairKindForOutputItem(llm.ToolOutputItemType(item.Type == llm.ResponseItemTypeCustomToolCall))
		}
	}
	return out
}

func firstMatchingProviderOutput(items []llm.ResponseItem, callID string, callKind repairToolCallKind) []llm.ResponseItem {
	if callKind == 0 {
		return nil
	}
	out := make([]llm.ResponseItem, 0, 2)
	for _, item := range items {
		if strings.TrimSpace(item.CallID) == callID && repairKindForOutputItem(item.Type) == callKind {
			if len(out) == 0 {
				out = append(out, llm.CloneResponseItems([]llm.ResponseItem{item})...)
			}
			continue
		}
		if len(out) > 0 && isProviderOutputAttachmentForCall(item, callID) {
			out = append(out, llm.CloneResponseItems([]llm.ResponseItem{item})...)
		}
	}
	return out
}

func repairedTailMessageCountThroughOriginalCount(items []llm.ResponseItem, affectedCallIDs []string, removedCallIDs []string, originalMessageCount int) int {
	if originalMessageCount <= 0 {
		return 0
	}
	messages := llm.MessagesFromItems(items)
	if originalMessageCount >= len(messages) {
		return len(llm.MessagesFromItems(responseItemsAfterMissingToolOutputRepair(items, affectedCallIDs, removedCallIDs)))
	}
	prefixItems := llm.ItemsFromMessages(messages[:originalMessageCount])
	return len(llm.MessagesFromItems(responseItemsAfterMissingToolOutputRepair(prefixItems, affectedCallIDs, removedCallIDs)))
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
	s.callCounts = make(map[string]int)
	s.seenCalls = make(map[string]struct{})
	s.materializedOutputs = make(map[string]map[repairToolCallKind]int)
	s.invalidOutputs = make(map[string]map[repairToolCallKind]int)
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
			s.callCounts[callID]++
			if _, exists := s.calls[callID]; !exists {
				s.calls[callID] = repairKindForToolCall(call)
			}
			s.seenCalls[callID] = struct{}{}
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
		if _, seen := s.seenCalls[callID]; !seen {
			if s.invalidOutputs[callID] == nil {
				s.invalidOutputs[callID] = make(map[repairToolCallKind]int)
			}
			s.invalidOutputs[callID][kind]++
			return
		}
		if s.materializedOutputs[callID] == nil {
			s.materializedOutputs[callID] = make(map[repairToolCallKind]int)
		}
		s.materializedOutputs[callID][kind]++
	}
}

func (s *missingToolOutputRepairScan) repairPlan() missingToolOutputRepairPlan {
	plan := missingToolOutputRepairPlan{
		removeCalls:       make(map[string]struct{}),
		dedupeCalls:       make(map[string]struct{}),
		keptCalls:         make(map[string]struct{}),
		dropOutputs:       make(map[string]map[repairToolCallKind]int),
		dedupeOutputs:     make(map[string]map[repairToolCallKind]struct{}),
		filterCompletions: make(map[string]repairToolCallKind),
		keptOutputs:       make(map[string]map[repairToolCallKind]struct{}),
	}
	if s == nil {
		return plan
	}
	for callID, callKind := range s.calls {
		if s.callCounts[callID] > 1 {
			plan.dedupeCalls[callID] = struct{}{}
		}
		outputs := s.materializedOutputs[callID]
		for outputKind, count := range outputs {
			if outputKind != callKind {
				if plan.dropOutputs[callID] == nil {
					plan.dropOutputs[callID] = make(map[repairToolCallKind]int)
				}
				plan.dropOutputs[callID][outputKind] += count
				continue
			}
			if count > 1 {
				if plan.dedupeOutputs[callID] == nil {
					plan.dedupeOutputs[callID] = make(map[repairToolCallKind]struct{})
				}
				plan.dedupeOutputs[callID][outputKind] = struct{}{}
			}
		}
		if s.callCompleted(callID, callKind) {
			if completion, ok := s.toolCompletions[callID]; ok && len(completion.ProviderItems) > 0 {
				if _, changed, hasValid := filteredCompletionProviderItems(completion, callKind); changed && hasValid {
					plan.filterCompletions[callID] = callKind
				}
			}
			continue
		}
		plan.removeCalls[callID] = struct{}{}
	}
	for callID, outputs := range s.invalidOutputs {
		for outputKind, count := range outputs {
			if plan.dropOutputs[callID] == nil {
				plan.dropOutputs[callID] = make(map[repairToolCallKind]int)
			}
			plan.dropOutputs[callID][outputKind] += count
		}
	}
	for callID, outputs := range s.materializedOutputs {
		if _, hasCall := s.calls[callID]; hasCall {
			continue
		}
		for outputKind, count := range outputs {
			if plan.dropOutputs[callID] == nil {
				plan.dropOutputs[callID] = make(map[repairToolCallKind]int)
			}
			plan.dropOutputs[callID][outputKind] += count
		}
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
	for callID := range p.dedupeCalls {
		out[callID] = struct{}{}
	}
	for callID, kinds := range p.dedupeOutputs {
		if len(kinds) == 0 {
			continue
		}
		out[callID] = struct{}{}
	}
	for callID := range p.filterCompletions {
		out[callID] = struct{}{}
	}
	return out
}

func (p missingToolOutputRepairPlan) affectedCalls() int {
	return len(p.affectedCallIDs())
}

func (s *missingToolOutputRepairScan) callCompleted(callID string, callKind repairToolCallKind) bool {
	if outputs := s.materializedOutputs[callID]; len(outputs) > 0 {
		if outputs[callKind] > 0 {
			return true
		}
	}
	completion, ok := s.toolCompletions[callID]
	if !ok {
		return false
	}
	if len(completion.ProviderItems) == 0 {
		return true
	}
	for _, item := range completion.ProviderItems {
		if strings.TrimSpace(item.CallID) == callID && repairKindForOutputItem(item.Type) == callKind {
			return true
		}
	}
	return false
}

func filteredCompletionProviderItems(completion storedToolCompletion, callKind repairToolCallKind) ([]llm.ResponseItem, bool, bool) {
	if len(completion.ProviderItems) == 0 {
		return nil, false, true
	}
	callID := strings.TrimSpace(completion.CallID)
	filtered := make([]llm.ResponseItem, 0, 1)
	changed := false
	hasValid := false
	for _, item := range completion.ProviderItems {
		if strings.TrimSpace(item.CallID) != callID || repairKindForOutputItem(item.Type) != callKind {
			if hasValid && isProviderOutputAttachmentForCall(item, callID) {
				filtered = append(filtered, llm.CloneResponseItems([]llm.ResponseItem{item})...)
			} else {
				changed = true
			}
			continue
		}
		if hasValid {
			changed = true
			continue
		}
		filtered = append(filtered, llm.CloneResponseItems([]llm.ResponseItem{item})...)
		hasValid = true
	}
	if len(filtered) != len(completion.ProviderItems) {
		changed = true
	}
	return filtered, changed, hasValid
}

func isProviderOutputAttachmentForCall(item llm.ResponseItem, callID string) bool {
	if item.Type != llm.ResponseItemTypeOther || item.LinkKind != llm.ResponseItemLinkToolOutputAttachment {
		return false
	}
	if strings.TrimSpace(item.LinkedCallID) == callID {
		return true
	}
	return strings.TrimSpace(item.CallID) == callID
}

func isProviderOutputAttachmentForAnyCall(item llm.ResponseItem, callIDs map[string]struct{}) bool {
	if item.Type != llm.ResponseItemTypeOther || item.LinkKind != llm.ResponseItemLinkToolOutputAttachment || len(callIDs) == 0 {
		return false
	}
	if _, exists := callIDs[strings.TrimSpace(item.LinkedCallID)]; exists {
		return true
	}
	_, exists := callIDs[strings.TrimSpace(item.CallID)]
	return exists
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
	if plan.affectedCalls() == 0 || evt.Seq <= boundarySeq {
		return session.EventRewriteDecision{Event: evt}, nil
	}
	if strings.TrimSpace(evt.Kind) == "tool_completed" {
		var completion storedToolCompletion
		if err := json.Unmarshal(evt.Payload, &completion); err != nil {
			return session.EventRewriteDecision{}, fmt.Errorf("decode tool_completed event: %w", err)
		}
		callID := strings.TrimSpace(completion.CallID)
		if _, remove := plan.removeCalls[callID]; remove {
			return session.EventRewriteDecision{Drop: true}, nil
		}
		callKind, filter := plan.filterCompletions[callID]
		if !filter {
			return session.EventRewriteDecision{Event: evt}, nil
		}
		filtered, changed, hasValid := filteredCompletionProviderItems(completion, callKind)
		if !hasValid {
			return session.EventRewriteDecision{Event: evt}, nil
		}
		if !changed {
			return session.EventRewriteDecision{Event: evt}, nil
		}
		completion.ProviderItems = filtered
		payload, err := json.Marshal(completion)
		if err != nil {
			return session.EventRewriteDecision{}, fmt.Errorf("marshal tool_completed event: %w", err)
		}
		evt.Payload = payload
		return session.EventRewriteDecision{Event: evt}, nil
	}
	if strings.TrimSpace(evt.Kind) != "message" {
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
			callID := strings.TrimSpace(call.ID)
			if callID == "" {
				filtered = append(filtered, call)
				continue
			}
			if _, remove := plan.removeCalls[callID]; remove {
				continue
			}
			if _, dedupe := plan.dedupeCalls[callID]; dedupe {
				if _, kept := plan.keptCalls[callID]; kept {
					continue
				}
				plan.keptCalls[callID] = struct{}{}
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
		if plan.dropOutputs[callID][kind] > 0 {
			plan.dropOutputs[callID][kind]--
			return session.EventRewriteDecision{Drop: true}, nil
		}
		if _, dedupe := plan.dedupeOutputs[callID][kind]; dedupe {
			if plan.keptOutputs[callID] == nil {
				plan.keptOutputs[callID] = make(map[repairToolCallKind]struct{})
			}
			if _, kept := plan.keptOutputs[callID][kind]; kept {
				return session.EventRewriteDecision{Drop: true}, nil
			}
			plan.keptOutputs[callID][kind] = struct{}{}
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
