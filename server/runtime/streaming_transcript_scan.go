package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/config"
	"core/shared/toolspec"
	"core/shared/transcript"
)

// streamingTranscriptScan projects transcript-visible state from a stream of
// persisted events while retaining only the requested window plus a single
// in-flight assistant turn. It never materializes the full event history:
// committed entries outside the page/tail window are discarded as they stream
// by (see inMemoryTranscriptScan.appendEntry), and tool completions/materialized
// tool-result messages are held only until the assistant turn that owns them is
// flushed.
//
// An assistant turn's tool result can be persisted as a tool_completed event
// (synthesized at render) and/or a RoleTool message (materialized at render).
// Whether a call is materialized is only known once the turn's later events have
// streamed in, so the assistant message is buffered until the turn closes (the
// next non-tool event or end of stream) before its entries are emitted.
type streamingTranscriptScan struct {
	scan             *inMemoryTranscriptScan
	completions      map[string]tools.Result
	materialized     map[string]struct{}
	cacheWarningMode config.CacheWarningMode

	turn turnBuffer

	lastCommittedAssistantFinalAnswer string
}

type turnBuffer struct {
	assistant    *llm.Message
	callIDs      []string
	materialized []llm.Message
}

func newStreamingTranscriptScan(req inMemoryTranscriptScanRequest, cacheWarningMode config.CacheWarningMode) *streamingTranscriptScan {
	completions := make(map[string]tools.Result)
	materialized := make(map[string]struct{})
	return &streamingTranscriptScan{
		scan:             newInMemoryTranscriptScan(req, completions, materialized),
		completions:      completions,
		materialized:     materialized,
		cacheWarningMode: cacheWarningMode,
	}
}

func (s *streamingTranscriptScan) ApplyPersistedEvent(evt session.Event) error {
	if s == nil {
		return nil
	}
	switch strings.TrimSpace(evt.Kind) {
	case "message":
		var msg llm.Message
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			return fmt.Errorf("decode message event: %w", err)
		}
		for _, reconstructed := range reconstructPersistedMessages(msg) {
			s.applyReconstructedMessage(reconstructed, evt.Seq)
		}
	case "tool_completed":
		var completion storedToolCompletion
		if err := json.Unmarshal(evt.Payload, &completion); err != nil {
			return fmt.Errorf("decode tool_completed event: %w", err)
		}
		callID := strings.TrimSpace(completion.CallID)
		if callID == "" {
			return nil
		}
		s.completions[callID] = tools.Result{
			CallID:        completion.CallID,
			Name:          toolspec.ID(completion.Name),
			IsError:       completion.IsError,
			Output:        completion.Output,
			Summary:       completion.Summary,
			CondensedText: completion.CondensedText,
			Presentation:  completion.Presentation,
		}
	case "local_entry":
		s.closeTurn()
		var entry storedLocalEntry
		if err := json.Unmarshal(evt.Payload, &entry); err != nil {
			return fmt.Errorf("decode local_entry event: %w", err)
		}
		s.scan.appendEntry(*localEntryChatEntry(entry))
	case sessionEventCacheWarning:
		s.closeTurn()
		var warning transcript.CacheWarning
		if err := json.Unmarshal(evt.Payload, &warning); err != nil {
			return fmt.Errorf("decode %s event: %w", sessionEventCacheWarning, err)
		}
		s.scan.appendEntry(ChatEntry{
			Visibility: cacheWarningEntryVisibility(s.cacheWarningMode),
			Role:       cacheWarningTranscriptRole,
			Text:       transcript.CacheWarningText(warning),
		})
	case "history_replaced":
		s.closeTurn()
		payload, ignoredLegacy, err := decodePersistedHistoryReplacementPayload(evt.Payload)
		if err != nil {
			return fmt.Errorf("%w: %w", errDecodeHistoryReplacedEvent, err)
		}
		if ignoredLegacy {
			return nil
		}
		s.scan.MarkCompactionBoundary()
		for _, entry := range transcriptEntriesFromHistoryReplacement(llm.PrepareOpenAIInputItems(payload.Items)) {
			s.scan.appendEntry(entry)
		}
		if answer := strings.TrimSpace(payload.LastCommittedAssistantFinalAnswer); answer != "" {
			s.lastCommittedAssistantFinalAnswer = payload.LastCommittedAssistantFinalAnswer
		}
	}
	return nil
}

func (s *streamingTranscriptScan) applyReconstructedMessage(msg llm.Message, seq int64) {
	if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
		s.closeTurn()
		buffered := msg
		s.turn.assistant = &buffered
		s.turn.callIDs = s.turn.callIDs[:0]
		for _, call := range msg.ToolCalls {
			if callID := strings.TrimSpace(call.ID); callID != "" {
				s.turn.callIDs = append(s.turn.callIDs, callID)
			}
		}
		return
	}
	if msg.Role == llm.RoleTool && s.turn.assistant != nil && s.turnOwnsCall(strings.TrimSpace(msg.ToolCallID)) {
		s.turn.materialized = append(s.turn.materialized, msg)
		return
	}
	s.closeTurn()
	s.applyMessage(msg, seq)
}

func (s *streamingTranscriptScan) turnOwnsCall(callID string) bool {
	if callID == "" {
		return false
	}
	for _, id := range s.turn.callIDs {
		if id == callID {
			return true
		}
	}
	return false
}

func (s *streamingTranscriptScan) applyMessage(msg llm.Message, seq int64) {
	s.scan.ApplyMessage(msg, seq)
	s.lastCommittedAssistantFinalAnswer = applyLastCommittedAssistantFinalAnswer(s.lastCommittedAssistantFinalAnswer, msg)
}

func (s *streamingTranscriptScan) closeTurn() {
	if s.turn.assistant == nil {
		return
	}
	assistant := *s.turn.assistant
	materialized := s.turn.materialized
	callIDs := s.turn.callIDs

	for _, rm := range materialized {
		if callID := strings.TrimSpace(rm.ToolCallID); callID != "" {
			s.materialized[callID] = struct{}{}
		}
	}
	s.applyMessage(assistant, 0)
	for _, rm := range materialized {
		s.applyMessage(rm, 0)
	}

	for _, callID := range callIDs {
		delete(s.completions, callID)
		delete(s.materialized, callID)
	}
	s.turn = turnBuffer{callIDs: callIDs[:0]}
}

func (s *streamingTranscriptScan) PageSnapshot() transcriptPageSnapshot {
	s.closeTurn()
	return s.scan.PageSnapshot()
}

func (s *streamingTranscriptScan) RecentTailSnapshot() TranscriptWindowSnapshot {
	s.closeTurn()
	return s.scan.RecentTailSnapshot()
}

func (s *streamingTranscriptScan) TotalEntries() int {
	s.closeTurn()
	return s.scan.totalEntries
}

func (s *streamingTranscriptScan) LastCommittedAssistantFinalAnswer() string {
	s.closeTurn()
	return s.lastCommittedAssistantFinalAnswer
}

// reconstructPersistedMessages round-trips a persisted message through the same
// item encode/decode the chat projection uses, so streamed transcript entries
// are byte-identical to the historical chatStore-driven projection.
func reconstructPersistedMessages(msg llm.Message) []llm.Message {
	out := make([]llm.Message, 0, 1)
	walker := newResponseItemMessageWalker(func(m llm.Message) {
		out = append(out, m)
	})
	for _, item := range llm.ItemsFromMessages([]llm.Message{msg}) {
		walker.Apply(item)
	}
	walker.Flush()
	return out
}
