package runtime

import (
	"cmp"
	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type ChatEntry struct {
	StepID                 *string
	Visibility             transcript.EntryVisibility
	RollbackTargetID       *string
	Role                   string
	Text                   string
	DurationMs             *int64
	CondensedText          string
	Phase                  llm.MessagePhase
	MessageType            llm.MessageType
	CompactionNumber       *int
	SourcePath             string
	WorktreeContext        *session.WorktreeContext
	CompactLabel           string
	ToolResultSummary      string
	ViewedImageKind        *transcript.ViewedImageKind
	ToolCallID             string
	QuestionAnswer         *tools.AskQuestionAnswer
	AnsweredBySessionID    *runtimeids.SessionID
	WebSearch              *transcript.WebSearchDetail
	NoticeID               string
	BackgroundActivityID   string
	BackgroundProcessID    string
	BackgroundExitCode     *int
	CacheWarning           *transcript.CacheWarning
	ToolOutputRepair       *transcript.ToolOutputRepairNotice
	ProviderModelMismatch  *transcript.ProviderModelMismatchNotice
	ReasoningOmission      *transcript.ReasoningOmissionNotice
	ConnectionFallback     *config.ConnectionReplacement
	ConnectionsUnavailable *transcript.ConnectionsUnavailableNotice
	ThinkingEffort         *string
	ToolCall               *transcript.ToolCallMeta
	CommittedProvenance    *TranscriptCommittedRowProvenance
	ReviewerFeedback       *ReviewerFeedbackChatEntry
	ReviewerError          *ReviewerErrorChatEntry
}

type ReviewerFeedbackChatEntry struct {
	ID          runtimeids.ReviewerFeedbackID
	Suggestions []string
}

type ReviewerErrorChatEntry struct {
	ID     runtimeids.ReviewerErrorID
	Detail string
}

type ChatSnapshot struct {
	Entries        []ChatEntry
	Streaming      []AssistantStreamSnapshot
	StreamingError string
}

type AssistantStreamMetadata struct {
	OutputIndex             *int64
	StepID                  string
	BaseRevision            int64
	BaseCommittedEntryCount int
}

type AssistantStreamSnapshot struct {
	SourceState AssistantSourceState
	Text        string
	Metadata    AssistantStreamMetadata
	StreamID    uuid.UUID
	Phase       llm.MessagePhase
}

type TranscriptWindowSnapshot struct {
	Snapshot     ChatSnapshot
	TotalEntries int
	Offset       int
}

type storedToolCompletion struct {
	CallID              string                   `json:"call_id"`
	Name                string                   `json:"name"`
	IsError             bool                     `json:"is_error"`
	ProviderItemOnly    bool                     `json:"provider_item_only,omitempty"`
	Output              json.RawMessage          `json:"output"`
	Summary             *string                  `json:"summary,omitempty"`
	CondensedText       *string                  `json:"condensed_text,omitempty"`
	Presentation        *transcript.ToolCallMeta `json:"presentation,omitempty"`
	ProviderItems       []llm.ResponseItem       `json:"provider_items,omitempty"`
	QuestionAnswer      *tools.AskQuestionAnswer `json:"question_answer,omitempty"`
	AnsweredBySessionID *runtimeids.SessionID    `json:"answered_by_session_id,omitempty"`
}

type chatStore struct {
	mu sync.RWMutex

	messageRecords []chatMessageRecord
	compact        *compactionCheckpoint
	local          []localChatEntry

	toolCompletions                   map[string]tools.Result
	toolCompletionProvenance          map[string]*TranscriptCommittedRowProvenance
	toolCompletionProviderItems       map[string][]llm.ResponseItem
	assistantToolCalls                map[string]struct{}
	assistantToolCallStepIDs          map[string]string
	materializedToolResults           map[string]struct{}
	synthesizedToolResults            map[string]struct{}
	assistantStreamIDsByEntry         map[int]uuid.UUID
	activeSegmentEntryStart           int
	streaming                         []AssistantStreamSnapshot
	streamingError                    string
	cwd                               string
	lastCommittedAssistantFinalAnswer *string
	transcriptEntryCount              int

	providerTokenEstimate      int
	providerTokenEstimateDirty bool
	tokenEstimator             llm.TokenEstimator
}

type chatMessageRecord struct {
	StepID        *string
	Message       *llm.Message
	ProviderItems []llm.ResponseItem
	Provenance    *TranscriptCommittedRowProvenance
}

type localChatEntry struct {
	Entry             ChatEntry
	AfterMessageCount int
	AfterToolCallID   *string
	MarksBoundary     bool
	Projected         bool
	Provenance        *TranscriptCommittedRowProvenance
}

type assistantStreamingAppend struct {
	stream     *AssistantStreamSnapshot
	superseded []AssistantStreamSnapshot
}

type compactionCheckpoint struct {
	Items []llm.ResponseItem
}

func newChatStore() *chatStore {
	cwd, _ := os.Getwd()
	return newChatStoreWithCWD(cwd)
}

func newChatStoreWithCWD(cwd string) *chatStore {
	return &chatStore{
		toolCompletions:             make(map[string]tools.Result, 16),
		toolCompletionProvenance:    make(map[string]*TranscriptCommittedRowProvenance, 16),
		toolCompletionProviderItems: make(map[string][]llm.ResponseItem, 16),
		assistantToolCalls:          make(map[string]struct{}, 16),
		assistantToolCallStepIDs:    make(map[string]string, 16),
		materializedToolResults:     make(map[string]struct{}, 16),
		synthesizedToolResults:      make(map[string]struct{}, 16),
		cwd:                         strings.TrimSpace(cwd),
		providerTokenEstimateDirty:  true,
		tokenEstimator:              llm.DefaultTokenEstimator{},
	}
}

func (s *chatStore) validateMessage(stepID *string, msg llm.Message) error {
	var err error
	msg, err = normalizeMessageForTranscriptChecked(msg, s.cwd)
	if err != nil {
		return fmt.Errorf("normalize message transcript presentation: %w", err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.validateMessageLocked(stepID, msg)
}

func (s *chatStore) appendMessage(stepID *string, msg llm.Message, provenances ...*TranscriptCommittedRowProvenance) error {
	var provenance *TranscriptCommittedRowProvenance
	if len(provenances) > 0 {
		provenance = provenances[0]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	msg, err = normalizeMessageForTranscriptChecked(msg, s.cwd)
	if err != nil {
		return fmt.Errorf("normalize message transcript presentation: %w", err)
	}
	if err := s.validateMessageLocked(stepID, msg); err != nil {
		return err
	}
	providerItems := llm.ItemsFromMessages([]llm.Message{msg})
	if msg.Role == llm.RoleTool && msg.ToolCallID != nil {
		if completion, ok := s.toolCompletions[*msg.ToolCallID]; ok && completion.ProviderItemOnly {
			providerItems = nil
		}
	}
	s.messageRecords = append(s.messageRecords, chatMessageRecord{
		StepID:        textutil.Pointer(stepID),
		Message:       textutil.Value(cloneChatStoreMessage(msg)),
		ProviderItems: providerItems,
		Provenance:    cloneTranscriptCommittedRowProvenance(provenance),
	})
	s.applyMessageStatsLocked(stepID, msg)
	s.placeAttachedLocalEntriesAfterMaterializedToolLocked(msg)
	s.providerTokenEstimateDirty = true
	return nil
}
func (s *chatStore) replaceHistory(stepID string, items []llm.ResponseItem) {
	s.replaceHistoryAtCommittedEntryStart(
		textutil.OptionalExactString(stepID),
		items,
		nil,
		transcriptEntriesFromHistoryReplacement(historyReplacementPayload{Items: items}),
	)
}

func (s *chatStore) replaceHistoryAtCommittedEntryStart(
	stepID *string,
	items []llm.ResponseItem,
	committedEntryStart *int,
	projectedEntries []ChatEntry,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	preparedItems := llm.PrepareResponsesInputItems(items)
	s.recordReplacementToolCallStepIDsLocked(stepID, preparedItems)
	s.installWorkingSetLocked(stepID, &compactionCheckpoint{Items: llm.CloneResponseItems(preparedItems)}, committedEntryStart, projectedEntries)
}

func (s *chatStore) beginGeneration(stepID *string, committedEntryStart int, entries []ChatEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.installWorkingSetLocked(stepID, nil, &committedEntryStart, entries)
}

// Preparation adds a base checkpoint without replacing activity already
// committed after compaction. Its transcript rows keep their original identity.
func (s *chatStore) prepareGeneration(items []llm.ResponseItem, entries []ChatEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compact = &compactionCheckpoint{Items: llm.CloneResponseItems(llm.PrepareResponsesInputItems(items))}
	for _, entry := range entries {
		s.appendProjectedEntryLocked(entry, false)
		s.local[len(s.local)-1].AfterMessageCount = len(s.messageRecords)
	}
	s.providerTokenEstimateDirty = true
}

func (s *chatStore) installWorkingSetLocked(stepID *string, checkpoint *compactionCheckpoint, committedEntryStart *int, projectedEntries []ChatEntry) {
	activeSegmentEntryStart := s.transcriptEntryCount
	if committedEntryStart != nil && *committedEntryStart > s.transcriptEntryCount {
		s.transcriptEntryCount = *committedEntryStart
		activeSegmentEntryStart = *committedEntryStart
	} else if committedEntryStart != nil {
		activeSegmentEntryStart = *committedEntryStart
	}
	// Provider/model history switches to the compacted checkpoint while the
	// transcript receives its typed projection at the same committed boundary.
	projectedStart := len(s.local)
	if stepID != nil {
		for index := range projectedEntries {
			projectedEntries[index].StepID = cloneOptionalStepID(stepID)
		}
	}
	s.appendProjectedHistoryReplacementEntriesLocked(projectedEntries)
	s.local = append([]localChatEntry(nil), s.local[projectedStart:]...)
	s.compact = checkpoint
	s.activeSegmentEntryStart = activeSegmentEntryStart
	s.pruneAssistantStreamIDsBeforeLocked(activeSegmentEntryStart)
	s.messageRecords = nil
	s.pruneToolCompletionsToWorkingSetLocked()
	s.providerTokenEstimateDirty = true
}

func (s *chatStore) pruneAssistantStreamIDsBeforeLocked(activeSegmentEntryStart int) {
	for entryIndex := range s.assistantStreamIDsByEntry {
		if entryIndex < activeSegmentEntryStart {
			delete(s.assistantStreamIDsByEntry, entryIndex)
		}
	}
}

func (s *chatStore) pruneToolCompletionsToWorkingSetLocked() {
	if len(s.toolCompletions) == 0 &&
		len(s.toolCompletionProvenance) == 0 &&
		len(s.toolCompletionProviderItems) == 0 &&
		len(s.assistantToolCalls) == 0 &&
		len(s.assistantToolCallStepIDs) == 0 &&
		len(s.materializedToolResults) == 0 &&
		len(s.synthesizedToolResults) == 0 {
		return
	}
	referenced := make(map[string]struct{}, len(s.toolCompletions))
	for _, item := range s.providerItemsSourceLocked() {
		if !isToolCallItem(item.Type) {
			continue
		}
		if callID, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID); present {
			referenced[callID] = struct{}{}
		}
	}
	pruneCallIDMapToReferenced(s.toolCompletions, referenced)
	pruneCallIDMapToReferenced(s.toolCompletionProvenance, referenced)
	pruneCallIDMapToReferenced(s.toolCompletionProviderItems, referenced)
	pruneCallIDMapToReferenced(s.assistantToolCalls, referenced)
	pruneCallIDMapToReferenced(s.assistantToolCallStepIDs, referenced)
	pruneCallIDMapToReferenced(s.materializedToolResults, referenced)
	pruneCallIDMapToReferenced(s.synthesizedToolResults, referenced)
}

func pruneCallIDMapToReferenced[V any](m map[string]V, referenced map[string]struct{}) {
	for callID := range m {
		if _, ok := referenced[callID]; !ok {
			delete(m, callID)
		}
	}
}

func (s *chatStore) estimatedProviderTokens() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.providerTokenEstimateDirty {
		return s.providerTokenEstimate
	}
	items, _ := s.snapshotProviderItemsLocked()
	total := llm.EstimateItemsTokens(s.tokenEstimator, items)
	if total < 0 {
		total = 0
	}
	s.providerTokenEstimate = total
	s.providerTokenEstimateDirty = false
	return total
}

func (s *chatStore) bindTokenEstimator(estimator llm.TokenEstimator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenEstimator = estimator
	s.providerTokenEstimateDirty = true
}

func (s *chatStore) snapshotItems() []llm.ResponseItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, _ := s.snapshotProviderItemsLocked()
	return items
}

func (s *chatStore) toolCallSnapshot(callID string) (llm.ToolCall, bool) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return llm.ToolCall{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return toolCallSnapshotFromItems(s.providerItemsSourceLocked(), callID)
}

func toolCallSnapshotFromItems(items []llm.ResponseItem, callID string) (llm.ToolCall, bool) {
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if !isToolCallItem(item.Type) {
			continue
		}
		itemCallID, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID)
		if !present {
			continue
		}
		if itemCallID != callID {
			continue
		}
		name, _ := textutil.OptionalTrimmed(item.Name)
		call := llm.ToolCall{
			ID:           itemCallID,
			Name:         name,
			Presentation: append(json.RawMessage(nil), item.ToolPresentation...),
		}
		if item.Type == llm.ResponseItemTypeCustomToolCall {
			call.Custom = true
			call.CustomInput = textutil.Pointer(item.CustomInput)
			if item.CustomInput != nil {
				call.Input = normalizeRuntimeToolInput(*item.CustomInput)
			}
		} else {
			call.Input = append(json.RawMessage(nil), item.Arguments...)
		}
		return call, true
	}
	return llm.ToolCall{}, false
}

func (s *chatStore) restoreToolCompletionRecord(record session.ToolCompletionRecord, provenances ...*TranscriptCommittedRowProvenance) error {
	completion, err := storedToolCompletionFromSessionRecord(record)
	if err != nil {
		return fmt.Errorf("restore session tool completion record: %w", err)
	}
	s.recordToolCompletionWithProviderItems(tools.Result{
		CallID:              completion.CallID,
		Name:                toolspec.ID(completion.Name),
		IsError:             completion.IsError,
		ProviderItemOnly:    completion.ProviderItemOnly,
		Output:              completion.Output,
		Summary:             completion.Summary,
		CondensedText:       completion.CondensedText,
		Presentation:        completion.Presentation,
		QuestionAnswer:      completion.QuestionAnswer,
		AnsweredBySessionID: completion.AnsweredBySessionID,
	}, completion.ProviderItems, provenances...)
	return nil
}

func (s *chatStore) recordToolCompletionWithProviderItems(res tools.Result, providerItems []llm.ResponseItem, provenances ...*TranscriptCommittedRowProvenance) {
	callID := strings.TrimSpace(res.CallID)
	if callID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCompletions[callID] = res
	if len(provenances) > 0 {
		s.toolCompletionProvenance[callID] = cloneTranscriptCommittedRowProvenance(provenances[0])
	}
	if len(providerItems) > 0 {
		s.toolCompletionProviderItems[callID] = llm.CloneResponseItems(providerItems)
	} else {
		delete(s.toolCompletionProviderItems, callID)
	}
	s.providerTokenEstimateDirty = true
	if _, ok := s.assistantToolCalls[callID]; ok {
		if _, materialized := s.materializedToolResults[callID]; !materialized {
			if _, synthesized := s.synthesizedToolResults[callID]; !synthesized {
				s.synthesizedToolResults[callID] = struct{}{}
				s.transcriptEntryCount++
			}
		}
	}
}

func (s *chatStore) appendStreamingDelta(stepID string, baseRevision int64, baseCommittedEntryCount int, delta llm.AssistantDelta) assistantStreamingAppend {
	if delta.Text == "" {
		return assistantStreamingAppend{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nextMetadata := newAssistantStreamMetadata(stepID, baseRevision, baseCommittedEntryCount)
	nextMetadata.OutputIndex = textutil.Pointer(delta.OutputIndex)
	var superseded []AssistantStreamSnapshot
	if len(s.streaming) > 0 && assistantStreamingSegmentChanged(&s.streaming[0].Metadata, nextMetadata) {
		superseded = s.streamingSnapshotLocked()
		s.streaming = nil
	}
	var stream *AssistantStreamSnapshot
	for i := range s.streaming {
		if textutil.EqualOptional(s.streaming[i].Metadata.OutputIndex, delta.OutputIndex) {
			stream = &s.streaming[i]
			break
		}
	}
	if stream != nil && delta.OutputIndex == nil && stream.Phase != "" && delta.Phase != "" && stream.Phase != delta.Phase {
		superseded = append(superseded, *stream)
		*stream = AssistantStreamSnapshot{Metadata: *nextMetadata, StreamID: uuid.New()}
	}
	if stream == nil {
		s.streaming = append(s.streaming, AssistantStreamSnapshot{Metadata: *nextMetadata, StreamID: uuid.New()})
		stream = &s.streaming[len(s.streaming)-1]
	}
	stream.Text += delta.Text
	if delta.Phase != "" {
		stream.Phase = delta.Phase
	}
	result := *stream
	result.Metadata = *cloneAssistantStreamMetadata(&stream.Metadata)
	return assistantStreamingAppend{stream: &result, superseded: superseded}
}

func (s *chatStore) streamingSnapshot() ([]AssistantStreamSnapshot, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.streamingSnapshotLocked(), s.streamingError
}

func (s *chatStore) completeStreamingSource(stepID string, revision int64, committedCount int, source llm.AssistantSource) (*AssistantStreamSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := source.OutputIndex
	var stream *AssistantStreamSnapshot
	for i := range s.streaming {
		if textutil.EqualOptional(s.streaming[i].Metadata.OutputIndex, &index) {
			stream = &s.streaming[i]
			break
		}
	}
	if stream == nil {
		metadata := newAssistantStreamMetadata(stepID, revision, committedCount)
		metadata.OutputIndex = &index
		s.streaming = append(s.streaming, AssistantStreamSnapshot{Metadata: *metadata, StreamID: uuid.New()})
		stream = &s.streaming[len(s.streaming)-1]
	}
	if stream.SourceState != AssistantSourceStreaming || !strings.HasPrefix(source.Text, stream.Text) {
		return nil, fmt.Errorf("assistant source completion conflicts with unfinished source at output %d", index)
	}
	stream.Text, stream.Phase, stream.SourceState = source.Text, source.Phase, AssistantSourceComplete
	result := *stream
	result.Metadata = *cloneAssistantStreamMetadata(&stream.Metadata)
	return &result, nil
}

func (s *chatStore) nativeToolCallLocator(callID string) *transcript.CommittedRowLocator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var locator *transcript.CommittedRowLocator
	s.walkProjectionLocked(func(record chatMessageRecord) {
		if record.Message.NativeResponse == nil {
			return
		}
		for _, call := range record.Message.ToolCalls {
			if call.ID != callID {
				continue
			}
			rows := locateTranscriptCommittedRowFacts(transcriptCommittedRowFactsFromMessage(
				*record.Message, nil, nil, nil, transcriptMessageProjectionContext{Provenance: record.Provenance},
			))
			for _, row := range rows {
				if row.ToolCall != nil && row.ToolCall.ToolCallID == callID {
					locator = textutil.Value(row.Locator)
					return
				}
			}
		}
	}, func(localChatEntry) {}, nil)
	return locator
}

func (s *chatStore) recordAssistantStreamFinalization(committedEntryStart int, streamID *uuid.UUID) {
	if s == nil || committedEntryStart < 0 || streamID == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assistantStreamIDsByEntry == nil {
		s.assistantStreamIDsByEntry = make(map[int]uuid.UUID)
	}
	s.assistantStreamIDsByEntry[committedEntryStart] = *streamID
}

func (s *chatStore) discardStreaming() []AssistantStreamSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	streams := s.streamingSnapshotLocked()
	s.streaming = nil
	return streams
}

func (s *chatStore) setStreamingError(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamingError = strings.TrimSpace(text)
}

func (s *chatStore) clearStreamingError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamingError = ""
}

func newAssistantStreamMetadata(stepID string, baseRevision int64, baseCommittedEntryCount int) *AssistantStreamMetadata {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return nil
	}
	if baseRevision < 0 || baseCommittedEntryCount < 0 {
		return nil
	}
	return &AssistantStreamMetadata{
		StepID:                  stepID,
		BaseRevision:            baseRevision,
		BaseCommittedEntryCount: baseCommittedEntryCount,
	}
}

func assistantStreamingSegmentChanged(current *AssistantStreamMetadata, next *AssistantStreamMetadata) bool {
	if current == nil || next == nil {
		return current != next
	}
	return current.StepID != next.StepID ||
		current.BaseRevision != next.BaseRevision ||
		current.BaseCommittedEntryCount != next.BaseCommittedEntryCount
}

func (s *chatStore) streamingSnapshotLocked() []AssistantStreamSnapshot {
	streams := slices.Clone(s.streaming)
	for i := range streams {
		streams[i].Metadata = *cloneAssistantStreamMetadata(&streams[i].Metadata)
	}
	slices.SortStableFunc(streams, func(a, b AssistantStreamSnapshot) int {
		if a.Metadata.OutputIndex == nil || b.Metadata.OutputIndex == nil {
			return 0
		}
		return cmp.Compare(*a.Metadata.OutputIndex, *b.Metadata.OutputIndex)
	})
	return streams
}

func cloneAssistantStreamMetadata(metadata *AssistantStreamMetadata) *AssistantStreamMetadata {
	if metadata == nil {
		return nil
	}
	copyMetadata := *metadata
	copyMetadata.OutputIndex = textutil.Pointer(metadata.OutputIndex)
	return &copyMetadata
}

func cloneTranscriptStreamID(streamID *uuid.UUID) *uuid.UUID {
	if streamID == nil {
		return nil
	}
	copied := *streamID
	return &copied
}

func (s *chatStore) appendLocalEntryRecord(entry ChatEntry, afterToolCallID *string, provenances ...*TranscriptCommittedRowProvenance) {
	if strings.TrimSpace(entry.Text) == "" && entry.CacheWarning == nil && entry.ToolOutputRepair == nil && entry.ProviderModelMismatch == nil && entry.ReasoningOmission == nil && entry.ConnectionFallback == nil && entry.ConnectionsUnavailable == nil && entry.ReviewerFeedback == nil && entry.ReviewerError == nil {
		return
	}
	entry.Visibility = normalizeRuntimeEntryVisibility(entry.Visibility)
	entry.CondensedText = strings.TrimSpace(entry.CondensedText)
	entry.NoticeID = strings.TrimSpace(entry.NoticeID)
	entry.CacheWarning = copyCacheWarning(entry.CacheWarning)
	entry.ToolOutputRepair = textutil.Pointer(entry.ToolOutputRepair)
	entry.ProviderModelMismatch = textutil.Pointer(entry.ProviderModelMismatch)
	entry.ReasoningOmission = textutil.Pointer(entry.ReasoningOmission)
	entry.ConnectionFallback = textutil.Pointer(entry.ConnectionFallback)
	entry.ConnectionsUnavailable = textutil.Pointer(entry.ConnectionsUnavailable)
	if entry.ReviewerFeedback != nil {
		feedback := *entry.ReviewerFeedback
		feedback.Suggestions = append([]string(nil), entry.ReviewerFeedback.Suggestions...)
		entry.ReviewerFeedback = &feedback
	}
	if entry.ReviewerError != nil {
		reviewerError := *entry.ReviewerError
		entry.ReviewerError = &reviewerError
	}
	if afterToolCallID != nil {
		callID := strings.TrimSpace(*afterToolCallID)
		if callID == "" {
			panic("append local transcript entry: after-tool call identity is empty")
		}
		afterToolCallID = &callID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.local = append(s.local, localChatEntry{
		Entry:             entry,
		AfterMessageCount: len(s.messageRecords),
		AfterToolCallID:   afterToolCallID,
		Provenance: func() *TranscriptCommittedRowProvenance {
			if len(provenances) == 0 {
				return cloneTranscriptCommittedRowProvenance(entry.CommittedProvenance)
			}
			return cloneTranscriptCommittedRowProvenance(provenances[0])
		}(),
		Projected: entry.ReviewerFeedback != nil || entry.ReviewerError != nil,
	})
	s.transcriptEntryCount++
}

func (s *chatStore) placeAttachedLocalEntriesAfterMaterializedToolLocked(msg llm.Message) {
	if msg.Role != llm.RoleTool {
		return
	}
	callID, present := textutil.OptionalTrimmed(msg.ToolCallID)
	if !present {
		return
	}
	changed := false
	for index := range s.local {
		attachedCallID := s.local[index].AfterToolCallID
		if attachedCallID == nil || strings.TrimSpace(*attachedCallID) != callID {
			continue
		}
		s.local[index].AfterMessageCount = len(s.messageRecords)
		changed = true
	}
	if changed {
		sort.SliceStable(s.local, func(left, right int) bool {
			return s.local[left].AfterMessageCount < s.local[right].AfterMessageCount
		})
	}
}

func (s *chatStore) appendProjectedHistoryReplacementEntriesLocked(entries []ChatEntry) {
	for idx, entry := range entries {
		s.appendProjectedEntryLocked(entry, idx == 0)
	}
}

func (s *chatStore) appendProjectedEntryLocked(entry ChatEntry, marksBoundary bool) {
	entry.Visibility = normalizeRuntimeEntryVisibility(entry.Visibility)
	entry.ToolCallID = strings.TrimSpace(entry.ToolCallID)
	s.local = append(s.local, localChatEntry{
		Entry:             entry,
		AfterMessageCount: 0,
		MarksBoundary:     marksBoundary,
		Projected:         true,
		Provenance:        cloneTranscriptCommittedRowProvenance(entry.CommittedProvenance),
	})
	s.transcriptEntryCount++
}

func (s *chatStore) committedEntryCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.transcriptEntryCount
}

func (s *chatStore) cachedLastCommittedAssistantFinalAnswer() *string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return textutil.Pointer(s.lastCommittedAssistantFinalAnswer)
}

func (s *chatStore) seedLastCommittedAssistantFinalAnswerIfAbsent(answer *string) {
	if answer == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastCommittedAssistantFinalAnswer == nil {
		s.lastCommittedAssistantFinalAnswer = textutil.Pointer(answer)
	}
}

func (s *chatStore) snapshotMessages() []llm.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items, _ := s.snapshotProviderItemsLocked()
	return llm.MessagesFromItems(items)
}

func (s *chatStore) snapshotProviderItemsLocked() ([]llm.ResponseItem, *int) {
	items := s.providerItemsSourceLocked()
	materializedToolResults := collectMaterializedToolCalls(items)
	out := make([]llm.ResponseItem, 0, len(items)+len(s.toolCompletions))
	pendingOutputs := make([]llm.ResponseItem, 0, len(s.toolCompletions))
	inFunctionOutputRun := false
	var replacementEnd *int
	flushPendingOutputs := func() {
		if len(pendingOutputs) == 0 {
			return
		}
		out = append(out, pendingOutputs...)
		pendingOutputs = pendingOutputs[:0]
	}
	for index, item := range items {
		if s.compact != nil && index == len(s.compact.Items) {
			replacementEnd = textutil.Value(len(out))
		}
		if !isToolOutputItem(item.Type) {
			if inFunctionOutputRun {
				flushPendingOutputs()
				inFunctionOutputRun = false
			} else if !isToolCallItem(item.Type) {
				flushPendingOutputs()
			}
		}
		out = append(out, item)
		if !isToolCallItem(item.Type) {
			if isToolOutputItem(item.Type) {
				inFunctionOutputRun = true
			}
			continue
		}
		callID, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID)
		if !present {
			continue
		}
		if _, ok := materializedToolResults[callID]; ok {
			continue
		}
		completion, ok := s.toolCompletions[callID]
		if !ok {
			continue
		}
		providerItems := s.toolCompletionProviderItems[callID]
		if len(providerItems) > 0 {
			pendingOutputs = append(pendingOutputs, llm.CloneResponseItems(providerItems)...)
			continue
		}
		itemName, _ := textutil.OptionalTrimmed(item.Name)
		completionName := strings.TrimSpace(string(completion.Name))
		if completionName == "" {
			completionName = itemName
		}
		pendingOutputs = append(pendingOutputs, llm.PrepareResponsesInputItems([]llm.ResponseItem{{
			Type:   llm.ToolOutputItemType(item.Type == llm.ResponseItemTypeCustomToolCall),
			CallID: textutil.Value(callID),
			Name:   textutil.OptionalExactString(completionName),
			Output: append(json.RawMessage(nil), completion.Output...),
		}})...)
	}
	flushPendingOutputs()
	if s.compact != nil && replacementEnd == nil {
		replacementEnd = textutil.Value(len(out))
	}
	return out, replacementEnd
}

// danglingToolCalls reports tool calls in the current provider-bound item
// sequence that have no accompanying output (neither a materialized tool message
// nor a recorded completion). These are exactly the calls a provider rejects
// with HTTP 400 because every tool call must be followed by its output.
func (s *chatStore) danglingToolCalls() []danglingToolCall {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.providerItemsSourceLocked()
	materialized := collectMaterializedToolCalls(items)
	seen := make(map[string]struct{})
	out := make([]danglingToolCall, 0)
	for _, item := range items {
		if !isToolCallItem(item.Type) {
			continue
		}
		callID, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID)
		if !present {
			continue
		}
		if _, ok := seen[callID]; ok {
			continue
		}
		if _, ok := materialized[callID]; ok {
			continue
		}
		if _, ok := s.toolCompletions[callID]; ok {
			continue
		}
		name, _ := textutil.OptionalTrimmed(item.Name)
		stepID, hasStepID := s.assistantToolCallStepIDs[callID]
		var ownedStepID *string
		if hasStepID {
			ownedStepID = textutil.Value(stepID)
		}
		seen[callID] = struct{}{}
		out = append(out, danglingToolCall{callID: callID, name: name, stepID: ownedStepID})
	}
	return out
}

func isToolCallItem(itemType llm.ResponseItemType) bool {
	return itemType == llm.ResponseItemTypeFunctionCall || itemType == llm.ResponseItemTypeCustomToolCall
}

func isToolOutputItem(itemType llm.ResponseItemType) bool {
	return itemType == llm.ResponseItemTypeFunctionCallOutput || itemType == llm.ResponseItemTypeCustomToolOutput
}

func (s *chatStore) providerItemsSourceLocked() []llm.ResponseItem {
	active := s.activeProviderItemsLocked()
	if s.compact == nil {
		return active
	}
	base := llm.CloneResponseItems(s.compact.Items)
	out := make([]llm.ResponseItem, 0, len(base)+len(active))
	out = append(out, base...)
	out = append(out, active...)
	return out
}

func (s *chatStore) activeProviderItemsLocked() []llm.ResponseItem {
	itemCount := 0
	for _, record := range s.messageRecords {
		itemCount += len(record.ProviderItems)
	}
	items := make([]llm.ResponseItem, 0, itemCount)
	for _, record := range s.messageRecords {
		items = append(items, record.ProviderItems...)
	}
	return llm.CloneResponseItems(items)
}

func cloneChatStoreMessage(msg llm.Message) llm.Message {
	cloned := msg
	cloned.MessageType = textutil.Pointer(msg.MessageType)
	cloned.SourcePath = textutil.Pointer(msg.SourcePath)
	cloned.WorktreeContext = session.CloneWorktreeContext(msg.WorktreeContext)
	cloned.Content = textutil.Pointer(msg.Content)
	cloned.CompactContent = textutil.Pointer(msg.CompactContent)
	cloned.Name = textutil.Pointer(msg.Name)
	cloned.ToolCallID = textutil.Pointer(msg.ToolCallID)
	cloned.Phase = textutil.Pointer(msg.Phase)
	cloned.BackgroundActivityID = textutil.Pointer(msg.BackgroundActivityID)
	cloned.BackgroundExitCode = textutil.Pointer(msg.BackgroundExitCode)
	if len(msg.ToolCalls) > 0 {
		cloned.ToolCalls = append([]llm.ToolCall(nil), msg.ToolCalls...)
		for index := range cloned.ToolCalls {
			cloned.ToolCalls[index].Presentation = append(json.RawMessage(nil), msg.ToolCalls[index].Presentation...)
			cloned.ToolCalls[index].Input = append(json.RawMessage(nil), msg.ToolCalls[index].Input...)
			cloned.ToolCalls[index].ProviderItem = append(json.RawMessage(nil), msg.ToolCalls[index].ProviderItem...)
			cloned.ToolCalls[index].CustomInput = textutil.Pointer(msg.ToolCalls[index].CustomInput)
		}
	}
	if len(msg.ReasoningItems) > 0 {
		cloned.ReasoningItems = append([]llm.ReasoningItem(nil), msg.ReasoningItems...)
		for index := range cloned.ReasoningItems {
			cloned.ReasoningItems[index].Attribution = msg.ReasoningItems[index].Attribution.Clone()
		}
	}
	return cloned
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *chatStore) validateMessageLocked(stepID *string, msg llm.Message) error {
	if msg.Role != llm.RoleAssistant || stepID == nil {
		return nil
	}
	exactStepID := strings.TrimSpace(*stepID)
	for _, call := range msg.ToolCalls {
		callID := strings.TrimSpace(call.ID)
		if callID == "" {
			continue
		}
		if existing, present := s.assistantToolCallStepIDs[callID]; present && existing != exactStepID {
			return fmt.Errorf(
				"assistant tool call %q changed step identity from %q to %q",
				callID,
				existing,
				exactStepID,
			)
		}
	}
	return nil
}

func (s *chatStore) applyMessageStatsLocked(stepID *string, msg llm.Message) {
	s.applyLastCommittedAssistantFinalAnswerLocked(msg)
	delta := len(VisibleChatEntriesFromMessage(msg))
	switch msg.Role {
	case llm.RoleAssistant:
		for _, call := range msg.ToolCalls {
			callID := strings.TrimSpace(call.ID)
			if callID == "" {
				continue
			}
			if stepID != nil {
				s.assistantToolCallStepIDs[callID] = strings.TrimSpace(*stepID)
			}
			s.assistantToolCalls[callID] = struct{}{}
			if _, materialized := s.materializedToolResults[callID]; materialized {
				continue
			}
			if _, synthesized := s.synthesizedToolResults[callID]; synthesized {
				continue
			}
			if _, completed := s.toolCompletions[callID]; completed {
				s.synthesizedToolResults[callID] = struct{}{}
				delta++
			}
		}
	case llm.RoleTool:
		if callID, present := textutil.OptionalTrimmed(msg.ToolCallID); present {
			s.materializedToolResults[callID] = struct{}{}
			if _, synthesized := s.synthesizedToolResults[callID]; synthesized {
				delete(s.synthesizedToolResults, callID)
				delta--
			}
		}
	}
	s.transcriptEntryCount += delta
	if s.transcriptEntryCount < 0 {
		s.transcriptEntryCount = 0
	}
}

func (s *chatStore) recordReplacementToolCallStepIDsLocked(stepID *string, items []llm.ResponseItem) {
	if stepID == nil {
		return
	}
	exactStepID := strings.TrimSpace(*stepID)
	for _, item := range items {
		if !isToolCallItem(item.Type) {
			continue
		}
		callID, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID)
		if !present {
			continue
		}
		// Replacement items are born at the compaction boundary, which is also
		// the only ownership fact available when the active segment is restored.
		// Rebase live ownership to the same Step so restart cannot change it.
		s.assistantToolCallStepIDs[callID] = exactStepID
	}
}

func (s *chatStore) applyLastCommittedAssistantFinalAnswerLocked(msg llm.Message) {
	s.lastCommittedAssistantFinalAnswer = applyLastCommittedAssistantFinalAnswer(s.lastCommittedAssistantFinalAnswer, msg)
}

func applyLastCommittedAssistantFinalAnswer(current *string, msg llm.Message) *string {
	if messagePreservesLastCommittedAssistantFinalAnswer(msg) {
		return textutil.Pointer(current)
	}
	if isBlankFinalAnswer(msg) {
		return nil
	}
	if msg.Role == llm.RoleAssistant &&
		msg.Phase != nil &&
		*msg.Phase == llm.MessagePhaseFinal &&
		msg.Content != nil &&
		strings.TrimSpace(*msg.Content) != "" {
		return textutil.Pointer(msg.Content)
	}
	return nil
}

type transcriptDeliverySnapshot struct {
	Rows      []TranscriptCommittedRowFact
	Streaming []AssistantStreamSnapshot
}

type transcriptDeliveryFactScan struct {
	rows                     []TranscriptCommittedRowFact
	toolCompletions          map[string]tools.Result
	toolCompletionProvenance map[string]*TranscriptCommittedRowProvenance
	materializedToolCalls    map[string]struct{}
	streamIDsByEntry         map[int]uuid.UUID
	currentEntryIndex        int
	nativeCalls              map[string]struct{}
}

func newTranscriptDeliveryFactScan(completions map[string]tools.Result, completionProvenance map[string]*TranscriptCommittedRowProvenance, materializedToolCalls map[string]struct{}, streamIDsByEntry map[int]uuid.UUID, currentEntryIndex int) *transcriptDeliveryFactScan {
	return &transcriptDeliveryFactScan{toolCompletions: completions, toolCompletionProvenance: completionProvenance, materializedToolCalls: materializedToolCalls, streamIDsByEntry: streamIDsByEntry, currentEntryIndex: currentEntryIndex}
}

func (s *transcriptDeliveryFactScan) ApplyMessage(stepID *string, msg llm.Message, provenance *TranscriptCommittedRowProvenance) {
	if s == nil {
		return
	}
	if msg.Role == llm.RoleTool && msg.ToolCallID != nil {
		if _, native := s.nativeCalls[*msg.ToolCallID]; native {
			return
		}
	}
	materialized := s.materializedToolCalls
	if msg.NativeResponse != nil {
		materialized = nil
		if s.nativeCalls == nil {
			s.nativeCalls = make(map[string]struct{})
		}
		for _, call := range msg.ToolCalls {
			s.nativeCalls[call.ID] = struct{}{}
		}
	}
	var correlations []AssistantStreamCorrelation
	if msg.NativeResponse != nil {
		offset := 0
		for _, projected := range nativeResponseMessages(msg) {
			if id, ok := s.streamIDsByEntry[s.currentEntryIndex+offset]; ok {
				correlations = append(correlations, AssistantStreamCorrelation{OutputIndex: textutil.Value(projected.OutputIndex), StreamID: id})
			}
			offset += projected.entryCount()
		}
	} else if id, ok := s.streamIDsByEntry[s.currentEntryIndex]; ok {
		correlations = append(correlations, AssistantStreamCorrelation{StreamID: id})
	}
	facts := transcriptCommittedRowFactsForStep(
		stepID,
		transcriptCommittedRowFactsFromMessage(
			msg,
			correlations,
			s.toolCompletions,
			materialized,
			transcriptMessageProjectionContext{
				Provenance:           provenance,
				CompletionProvenance: s.toolCompletionProvenance,
			},
		),
	)
	s.rows = append(s.rows, facts...)
	s.currentEntryIndex += transcriptCommittedEntryCountFromMessage(msg, s.toolCompletions, materialized)
}

func (s *transcriptDeliveryFactScan) ApplyLocalEntry(entry ChatEntry, projected bool) {
	if s == nil {
		return
	}
	if projected {
		if fact, ok := transcriptCommittedRowFactFromChatEntry(entry); ok {
			s.rows = append(s.rows, fact)
		}
	} else if strings.TrimSpace(entry.Role) == string(transcript.EntryRoleReasoning) {
		if fact, ok := transcriptCommittedRowFactFromChatEntry(entry); ok {
			s.rows = append(s.rows, fact)
		}
	} else {
		if fact, ok := transcriptNoticeRowFactFromChatEntry(entry); ok {
			s.rows = append(s.rows, fact)
		}
	}
	s.currentEntryIndex++
}

func (s *transcriptDeliveryFactScan) MarkCompactionBoundary() {
	if s == nil {
		return
	}
	s.rows = nil
}

func (s *transcriptDeliveryFactScan) Snapshot() []TranscriptCommittedRowFact {
	if s == nil || len(s.rows) == 0 {
		return nil
	}
	out := make([]TranscriptCommittedRowFact, len(s.rows))
	copy(out, s.rows)
	return locateTranscriptCommittedRowFacts(out)
}

func (s *chatStore) walkProjectionLocked(
	applyMessage func(chatMessageRecord),
	applyLocalEntry func(localChatEntry),
	markCompactionBoundary func(),
) {
	messageIndex := 0
	localIndex := 0
	appendLocalEntries := func(messageCount int) {
		for localIndex < len(s.local) && s.local[localIndex].AfterMessageCount <= messageCount {
			local := s.local[localIndex]
			if local.MarksBoundary && markCompactionBoundary != nil {
				markCompactionBoundary()
			}
			applyLocalEntry(local)
			localIndex++
		}
	}
	if s.compact != nil && markCompactionBoundary != nil {
		markCompactionBoundary()
	}
	appendLocalEntries(0)
	for _, record := range s.messageRecords {
		if record.Message != nil {
			applyMessage(record)
		} else {
			for _, item := range record.ProviderItems {
				entry := configurationUpdateChatEntry(item)
				entry.StepID = cloneOptionalStepID(record.StepID)
				applyLocalEntry(localChatEntry{
					Entry: entry, Projected: true, Provenance: record.Provenance,
				})
			}
		}
		messageIndex++
		appendLocalEntries(messageIndex)
	}
	appendLocalEntries(messageIndex)
}

func (s *chatStore) deliverySnapshot() transcriptDeliverySnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	materializedToolResults := collectMaterializedToolCalls(s.activeProviderItemsLocked())
	for callID := range s.materializedToolResults {
		materializedToolResults[callID] = struct{}{}
	}
	streamIDsByEntry := make(map[int]uuid.UUID, len(s.assistantStreamIDsByEntry))
	for entryIndex, streamID := range s.assistantStreamIDsByEntry {
		streamIDsByEntry[entryIndex] = streamID
	}
	scan := newTranscriptDeliveryFactScan(s.toolCompletions, s.toolCompletionProvenance, materializedToolResults, streamIDsByEntry, s.activeSegmentEntryStart)
	s.walkProjectionLocked(
		func(record chatMessageRecord) {
			scan.ApplyMessage(record.StepID, *record.Message, record.Provenance)
		},
		func(local localChatEntry) {
			entry := local.Entry
			entry.CommittedProvenance = cloneTranscriptCommittedRowProvenance(local.Provenance)
			scan.ApplyLocalEntry(entry, local.Projected)
		},
		scan.MarkCompactionBoundary,
	)
	return transcriptDeliverySnapshot{
		Rows:      scan.Snapshot(),
		Streaming: s.streamingSnapshotLocked(),
	}
}
