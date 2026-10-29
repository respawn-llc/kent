package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"core/server/sessionstore"
	"core/shared/llmerrors"
	"core/shared/modelcontract"
	"core/shared/textutil"
	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

type responseStreamAccumulator struct {
	attempt                  *providerAttemptDiagnostic
	policy                   responsesPolicy
	callbacks                StreamCallbacks
	windowTokens             int
	assistantText            strings.Builder
	assistantStartedByOutput map[int64]bool
	assistantMessages        *assistantMessageAccumulator
	reasoning                *reasoningAccumulator
	standardServedModel      *string
	completed                *responses.Response
	finalizedOutput          []indexedResponseOutputItem
	responseError            *responseStreamError
}

type responseStreamError struct {
	Raw              string
	ProviderContract *responseStreamProviderContract
}

type responseStreamProviderContract struct {
	Message string
}

func newResponseStreamAccumulator(callbacks StreamCallbacks, windowTokens int, policy responsesPolicy) *responseStreamAccumulator {
	return &responseStreamAccumulator{
		policy:            policy,
		callbacks:         callbacks,
		windowTokens:      windowTokens,
		assistantMessages: newAssistantMessageAccumulator(),
		reasoning:         newReasoningAccumulator(),
	}
}

func (a *responseStreamAccumulator) hasCompleted() bool {
	return a != nil && a.completed != nil
}

func (a *responseStreamAccumulator) Consume(evt responses.ResponseStreamEventUnion) {
	if err := validateReasoningStreamEvent(evt); err != nil {
		a.recordReasoningProviderError(err)
		return
	}
	switch evt.Type {
	case "response.created":
		created := evt.AsResponseCreated()
		a.attempt.observeResponse(evt.Type, created.Response, a.windowTokens)
		observeStandardServedModel(&a.standardServedModel, string(created.Response.Model))
	case "response.output_text.delta":
		if evt.Delta == "" {
			return
		}
		a.consumeAssistantDelta(evt.OutputIndex, evt.Delta)
	case "response.output_text.done":
		a.assistantMessages.SetFinalizedText(evt.OutputIndex, evt.Text)
	case "response.output_item.added", "response.output_item.done":
		if evt.Type == "response.output_item.done" {
			a.finalizedOutput = append(a.finalizedOutput, indexedResponseOutputItem{Item: evt.Item, OutputIndex: evt.OutputIndex})
		}
		if err := a.assistantMessages.Upsert(evt.Item, evt.OutputIndex); err != nil {
			a.responseError = &responseStreamError{
				ProviderContract: &responseStreamProviderContract{Message: err.Error()},
			}
			return
		}
		a.reasoning.UpsertReasoningItem(evt.Item, evt.OutputIndex)
		a.recordReasoningAccumulatorError()
		if evt.Type == "response.output_item.done" && evt.Item.Type == "message" && a.callbacks.OnAssistantSourceComplete != nil {
			item := a.assistantMessages.byIndex[evt.OutputIndex].message
			item.OutputIndex = evt.OutputIndex
			text, _, err := a.assistantPresentation(item)
			if err != nil {
				a.responseError = &responseStreamError{ProviderContract: &responseStreamProviderContract{Message: err.Error()}}
				return
			}
			if text != "" {
				a.callbacks.OnAssistantSourceComplete(AssistantSource{
					Text: text, Phase: a.assistantMessages.Phase(evt.OutputIndex), OutputIndex: evt.OutputIndex,
				})
			}
		}
		if a.callbacks.OnOutputItem != nil && evt.Item.Type != "message" && evt.Item.Type != "reasoning" {
			items, _, _, _, _, _, _, err := parseOutputItems([]responses.ResponseOutputItemUnion{evt.Item}, a.policy)
			if err != nil {
				a.responseError = &responseStreamError{ProviderContract: &responseStreamProviderContract{Message: err.Error()}}
				return
			}
			for _, item := range items {
				item.OutputIndex = evt.OutputIndex
				a.callbacks.OnOutputItem(item)
			}
		}
	case "response.reasoning_summary_text.delta":
		coordinate, coordinateErr := reasoningSourceCoordinate(evt.OutputIndex, evt.SummaryIndex)
		if coordinateErr != nil {
			a.recordReasoningProviderError(coordinateErr)
			return
		}
		identity, identityErr := reasoningItemIdentity(evt.ItemID, evt.SummaryIndex)
		if identityErr != nil {
			a.recordReasoningProviderError(identityErr)
			return
		}
		a.reasoning.Append(reasoningRoleSummary, coordinate, identity, evt.Delta)
		a.recordReasoningAccumulatorError()
		a.emitReasoningSummaryDelta(coordinate)
	case "response.reasoning_summary_text.done":
		coordinate, coordinateErr := reasoningSourceCoordinate(evt.OutputIndex, evt.SummaryIndex)
		if coordinateErr != nil {
			a.recordReasoningProviderError(coordinateErr)
			return
		}
		identity, identityErr := reasoningItemIdentity(evt.ItemID, evt.SummaryIndex)
		if identityErr != nil {
			a.recordReasoningProviderError(identityErr)
			return
		}
		a.reasoning.Set(reasoningRoleSummary, coordinate, identity, evt.Text)
		a.recordReasoningAccumulatorError()
		a.emitReasoningSummaryDelta(coordinate)
	case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		if evt.Part.Type != "summary_text" {
			return
		}
		coordinate, coordinateErr := reasoningSourceCoordinate(evt.OutputIndex, evt.SummaryIndex)
		if coordinateErr != nil {
			a.recordReasoningProviderError(coordinateErr)
			return
		}
		identity, identityErr := reasoningItemIdentity(evt.ItemID, evt.SummaryIndex)
		if identityErr != nil {
			a.recordReasoningProviderError(identityErr)
			return
		}
		a.reasoning.Set(reasoningRoleSummary, coordinate, identity, evt.Part.Text)
		a.recordReasoningAccumulatorError()
		a.emitReasoningSummaryDelta(coordinate)
	case "response.completed":
		completedEvent := evt.AsResponseCompleted()
		a.attempt.observeResponse(evt.Type, completedEvent.Response, a.windowTokens)
		if !responseCompletedEventHasValidPayload(completedEvent) {
			a.responseError = &responseStreamError{
				Raw: completedEvent.RawJSON(),
				ProviderContract: &responseStreamProviderContract{
					Message: "OpenAI-compatible Responses stream emitted response.completed without a valid response payload",
				},
			}
			return
		}
		completed := completedEvent.Response
		observeStandardServedModel(&a.standardServedModel, string(completed.Model))
		a.completed = &completed
	case "response.failed":
		failed := evt.AsResponseFailed()
		a.attempt.observeResponse(evt.Type, failed.Response, a.windowTokens)
		a.responseError = &responseStreamError{Raw: failed.RawJSON()}
	case "response.incomplete":
		incomplete := evt.AsResponseIncomplete()
		a.attempt.observeResponse(evt.Type, incomplete.Response, a.windowTokens)
		raw := incomplete.RawJSON()
		if strings.TrimSpace(incomplete.Response.IncompleteDetails.Reason) == "" {
			a.responseError = &responseStreamError{
				Raw: raw,
				ProviderContract: &responseStreamProviderContract{
					Message: "OpenAI-compatible Responses stream emitted response.incomplete without incomplete_details.reason",
				},
			}
			return
		}
		a.responseError = &responseStreamError{Raw: raw}
	case "error":
		a.responseError = &responseStreamError{Raw: evt.RawJSON()}
	}
}

func (a *responseStreamAccumulator) assistantPresentation(item ResponseItem) (string, *session.NativeAssistantPresentation, error) {
	text := optionalStringValue(item.Content)
	streamed := ""
	if observed, ok := a.assistantMessages.byIndex[item.OutputIndex]; ok && observed.deltaText != nil {
		streamed = observed.deltaText.String()
	} else if pending := a.assistantMessages.pendingDeltas[item.OutputIndex]; pending != nil {
		streamed = pending.String()
	}
	if a.callbacks.OnAssistantDelta == nil && item.Content != nil && *item.Content != "" {
		streamed = ""
	}
	if streamed == "" {
		return text, nil, nil
	}
	if !completedAssistantTextReconcilesStream(streamed, text) {
		return "", nil, fmt.Errorf("completed assistant item %d conflicts with streamed assistant content", item.OutputIndex)
	}
	display := reconciledCompletedAssistantText(streamed, text)
	if display == text {
		return text, nil, nil
	}
	fact := &session.NativeAssistantPresentation{OutputIndex: item.OutputIndex}
	if display == strings.TrimLeftFunc(text, unicode.IsSpace) {
		fact.LeadingBytes = len(text) - len(display)
	} else {
		fact.TrailingWhitespace = []byte(display[len(text):])
	}
	return display, fact, nil
}

func (a *responseStreamAccumulator) consumeAssistantDelta(outputIndex int64, text string) {
	phase := a.assistantMessages.Phase(outputIndex)
	if a.assistantStartedByOutput == nil {
		a.assistantStartedByOutput = make(map[int64]bool)
	}
	if !a.assistantStartedByOutput[outputIndex] {
		// Some OpenAI-compatible streams emit provisional leading whitespace
		// before the assistant's durable content is known.
		text = strings.TrimLeftFunc(text, unicode.IsSpace)
	}
	if text == "" {
		return
	}
	a.assistantMessages.AppendDelta(outputIndex, text)
	a.emitAssistantDelta(AssistantDelta{Text: text, Phase: phase, OutputIndex: textutil.Value(outputIndex)})
	a.assistantStartedByOutput[outputIndex] = true
}

func (a *responseStreamAccumulator) emitAssistantDelta(delta AssistantDelta) {
	a.assistantText.WriteString(delta.Text)
	if a.callbacks.OnAssistantDelta != nil {
		a.callbacks.OnAssistantDelta(delta)
	}
}

func responseCompletedEventHasValidPayload(evt responses.ResponseCompletedEvent) bool {
	if !evt.JSON.Response.Valid() || !evt.Response.JSON.Output.Valid() {
		return false
	}
	var output []json.RawMessage
	if err := json.Unmarshal([]byte(evt.Response.JSON.Output.Raw()), &output); err != nil {
		return false
	}
	return output != nil
}

func (a *responseStreamAccumulator) Err(providerID string, responseStatus *responsesStatus) error {
	if a == nil || a.responseError == nil {
		return nil
	}
	var status *int
	if responseStatus != nil {
		status = &responseStatus.Code
	}
	if a.responseError.ProviderContract != nil {
		return llmerrors.NewProviderContractError(providerID, status, errors.New(a.responseError.ProviderContract.Message))
	}
	return &ssestream.StreamError{
		Message: a.responseError.Raw,
		Event:   ssestream.Event{Data: []byte(a.responseError.Raw)},
	}
}

func (a *responseStreamAccumulator) emitReasoningSummaryDelta(coordinate reasoningCoordinate) {
	if a.callbacks.OnReasoningSummaryDelta == nil || a.reasoning == nil || a.reasoning.err != nil {
		return
	}
	entry := a.reasoning.Current(reasoningRoleSummary, coordinate)
	if entry == nil {
		return
	}
	a.callbacks.OnReasoningSummaryDelta(a.policy.reasoningDelta(*entry))
}

func (a *responseStreamAccumulator) recordReasoningProviderError(err error) {
	if a == nil || err == nil || a.responseError != nil {
		return
	}
	a.responseError = &responseStreamError{
		ProviderContract: &responseStreamProviderContract{Message: err.Error()},
	}
}

func (a *responseStreamAccumulator) recordReasoningAccumulatorError() {
	if a == nil || a.reasoning == nil || a.reasoning.err == nil || a.responseError != nil {
		return
	}
	a.responseError = &responseStreamError{
		ProviderContract: &responseStreamProviderContract{
			Message: a.reasoning.err.Error(),
		},
	}
}

func (a *responseStreamAccumulator) Response() (ResponsesResponse, error) {
	if a.completed == nil {
		return ResponsesResponse{}, fmt.Errorf("Responses output requested before completion")
	}
	if len(a.assistantMessages.pendingDeltas) > 0 {
		return ResponsesResponse{}, fmt.Errorf("completed response has assistant deltas without a matching output item")
	}
	usage := Usage{WindowTokens: a.windowTokens}
	streamText, streamPhase, streamProviderPhase, streamOutputIndex, streamDeltaText, hasResolvedStream := a.assistantMessages.Resolve()
	rawDeltaText := a.assistantText.String()
	finalText := textutil.Pointer(streamText)
	finalTextPresent := hasResolvedStream && streamText != nil
	if hasResolvedStream && streamText == nil && streamDeltaText != "" {
		finalText = textutil.Value(streamDeltaText)
		finalTextPresent = true
	}
	if !hasResolvedStream && rawDeltaText != "" {
		finalText = textutil.Value(rawDeltaText)
		finalTextPresent = true
	}
	finalPhase := streamPhase
	finalProviderPhase := streamProviderPhase
	finalReasoning := a.reasoning.Entries()
	finalReasoningItems := a.reasoning.Items()

	var err error
	usage, err = a.policy.usage(a.completed.Usage, a.windowTokens)
	if err != nil {
		return ResponsesResponse{}, err
	}
	output := a.finalizedOutput
	parsedItems, parsedText, parsedPhase, parsedProviderPhase, parsedCalls, parsedReasoning, parsedReasoningItems, err := parseIndexedOutputItems(output, a.policy)
	if err != nil {
		return ResponsesResponse{}, err
	}
	var assistantPresentation []session.NativeAssistantPresentation
	if responseItemsContainAssistantMessage(parsedItems) {
		var segments []assistantOutputSegment
		for itemIndex, item := range parsedItems {
			if item.Type != ResponseItemTypeMessage {
				continue
			}
			text, presentation, err := a.assistantPresentation(item)
			if err != nil {
				return ResponsesResponse{}, err
			}
			if presentation != nil {
				presentation.OutputIndex = int64(itemIndex)
				assistantPresentation = append(assistantPresentation, *presentation)
			}
			phase, _ := textutil.OptionalValue(item.Phase)
			content := textutil.Pointer(item.Content)
			if content != nil {
				content = textutil.Value(text)
			}
			segments = append(segments, assistantOutputSegment{
				Text: text, Content: content, Phase: phase, OutputIndex: item.OutputIndex,
				ProviderPhase: parsedProviderPhase,
			})
		}
		parsedText, _, _, _, _, _ = resolveAssistantOutput(segments)
		// Reconciliation is item-local; aggregate phase selection must not compare
		// one native message's deltas with another message's terminal text.
		finalText = parsedText
		finalTextPresent = parsedText != nil
	}
	if parsedPhase != "" {
		finalPhase = parsedPhase
		finalProviderPhase = parsedProviderPhase
	}
	// Like Codex, accept only output_item.done items. The completed
	// event supplies completion metadata, not conversation output.
	finalCalls := parsedCalls
	mergedReasoning, mergeErr := mergeReasoningEntries(parsedReasoning, finalReasoning)
	if mergeErr != nil {
		return ResponsesResponse{}, mergeErr
	}
	finalReasoning = a.policy.reasoningEntries(mergedReasoning)
	finalReasoningItems = mergeReasoningItems(parsedReasoningItems, finalReasoningItems)
	finalOutputItems := repairAssistantOutputItems(parsedItems, finalText, finalTextPresent, finalPhase, streamOutputIndex, hasResolvedStream)

	providerEvidence, evidenceErr := providerUsageEvidenceFromResponse(*a.completed, finalOutputItems)
	if evidenceErr != nil {
		return ResponsesResponse{}, evidenceErr
	}

	body := session.NativeResponseBody{Format: session.NativeResponseOpenAI, Items: make([][]byte, len(output))}
	body.AssistantPresentation = assistantPresentation
	switch a.policy.(type) {
	case grokResponsesPolicy, grokCLIResponsesPolicy:
		body.Format = session.NativeResponseGrok
	}
	for i, item := range output {
		body.Items[i] = []byte(item.Item.RawJSON())
	}
	var native *NativeResponse
	if len(body.Items) > 0 {
		native, err = newNativeResponse(body, parsedItems)
		if err != nil {
			return ResponsesResponse{}, err
		}
		native = native.WithReasoningType(body.ReasoningType)
	}
	var endTurn *bool
	if field, present := a.completed.JSON.ExtraFields["end_turn"]; present {
		if err := json.Unmarshal([]byte(field.Raw()), &endTurn); err != nil {
			return ResponsesResponse{}, fmt.Errorf("decode completed response end_turn: %w", err)
		}
	}
	// Codex treats an omitted end_turn as complete unless tool execution or
	// pending input requires another step. Text and phase are presentation.
	if endTurn == nil {
		endTurn = textutil.Value(true)
	}
	return ResponsesResponse{
		NativeResponse:   native,
		EndTurn:          endTurn,
		AssistantText:    finalText,
		ProviderPhase:    finalProviderPhase,
		ServedModel:      textutil.Pointer(a.standardServedModel),
		ToolCalls:        finalCalls,
		Reasoning:        finalReasoning,
		ReasoningItems:   finalReasoningItems,
		OutputItems:      finalOutputItems,
		Usage:            usage,
		ProviderEvidence: providerEvidence,
	}, nil
}

func providerUsageEvidenceFromResponse(
	response responses.Response,
	outputItems []ResponseItem,
) (modelcontract.ProviderUsageEvidence, error) {
	evidence := modelcontract.ProviderUsageEvidence{}
	if id := strings.TrimSpace(response.ID); id != "" {
		evidence.ResponseID = textutil.Value(id)
	}
	if response.CreatedAt > 0 {
		createdAt := time.Unix(int64(response.CreatedAt), 0).UTC()
		evidence.ResponseCreatedAt = &createdAt
	}
	if model := strings.TrimSpace(string(response.Model)); model != "" {
		evidence.ServedModel = textutil.Value(model)
	}
	if tier := strings.TrimSpace(string(response.ServiceTier)); tier != "" {
		evidence.ServedServiceTier = textutil.Value(tier)
	}
	if response.JSON.Usage.Valid() {
		raw := json.RawMessage(response.Usage.RawJSON())
		evidence.Usage = &raw
	}
	if field, ok := response.JSON.ExtraFields["usage_metadata"]; ok && field.Raw() != "" {
		raw := json.RawMessage(field.Raw())
		evidence.UsageMetadata = &raw
	}
	seenHostedIDs := make(map[string]struct{})
	appendHostedTool := func(output responses.ResponseOutputItemUnion) error {
		if output.Type != "web_search_call" {
			return nil
		}
		webSearch := output.AsWebSearchCall()
		var actionKind *string
		if raw := webSearch.JSON.Action.Raw(); raw != respjson.Omitted && raw != respjson.Null {
			actionType := strings.TrimSpace(webSearch.Action.Type)
			switch actionType {
			case "search", "open_page", "find_in_page":
				actionKind = textutil.Value(actionType)
			default:
				return fmt.Errorf("hosted web search output has unsupported action type %q", actionType)
			}
		}
		if strings.TrimSpace(webSearch.ID) == "" || strings.TrimSpace(string(webSearch.Status)) == "" {
			return fmt.Errorf("hosted web search output is missing required identity fields")
		}
		if _, exists := seenHostedIDs[webSearch.ID]; exists {
			return nil
		}
		seenHostedIDs[webSearch.ID] = struct{}{}
		tool := modelcontract.HostedToolUsageEvidence{
			ID:         textutil.Value(webSearch.ID),
			Type:       textutil.Value(string(webSearch.Type)),
			Status:     textutil.Value(string(webSearch.Status)),
			ActionKind: actionKind,
		}
		if field, ok := webSearch.JSON.ExtraFields["usage"]; ok && field.Raw() != "" {
			raw := json.RawMessage(field.Raw())
			tool.Usage = &raw
		}
		evidence.HostedTools = append(evidence.HostedTools, tool)
		return nil
	}
	for _, item := range outputItems {
		if item.Type != ResponseItemTypeOther || len(item.Raw) == 0 {
			continue
		}
		var output responses.ResponseOutputItemUnion
		if err := json.Unmarshal(item.Raw, &output); err != nil {
			return modelcontract.ProviderUsageEvidence{}, fmt.Errorf("decode hosted web search output: %w", err)
		}
		if err := appendHostedTool(output); err != nil {
			return modelcontract.ProviderUsageEvidence{}, err
		}
	}
	return evidence, nil
}

func responseItemsContainAssistantMessage(items []ResponseItem) bool {
	for _, item := range items {
		if item.Type == ResponseItemTypeMessage && item.Role != nil && *item.Role == RoleAssistant {
			return true
		}
	}
	return false
}

func AssistantResponseTextExtendsStream(streamed string, candidate string) bool {
	if candidate == "" {
		return false
	}
	if streamed == "" {
		return true
	}
	return strings.HasPrefix(candidate, streamed)
}

func completedAssistantTextReconcilesStream(streamed string, completed string) bool {
	return AssistantResponseTextExtendsStream(streamed, completed) ||
		(streamed != "" &&
			completed != "" &&
			(strings.TrimRightFunc(streamed, unicode.IsSpace) == completed ||
				streamed == strings.TrimLeftFunc(completed, unicode.IsSpace)))
}

func reconciledCompletedAssistantText(streamed string, completed string) string {
	// Streamed bytes are already visible to clients. A completed payload may
	// normalize provisional whitespace, but it must never retract that prefix.
	if streamed != "" &&
		(streamed == strings.TrimLeftFunc(completed, unicode.IsSpace) ||
			strings.TrimRightFunc(streamed, unicode.IsSpace) == completed) {
		return streamed
	}
	return completed
}

func repairAssistantOutputItems(items []ResponseItem, text *string, textPresent bool, phase MessagePhase, outputIndex int64, hasResolvedStream bool) []ResponseItem {
	if len(items) == 0 {
		return nil
	}
	repaired := CloneResponseItems(items)
	assistantIndexes := make([]int, 0, len(repaired))
	for idx := len(repaired) - 1; idx >= 0; idx-- {
		if repaired[idx].Type == ResponseItemTypeMessage &&
			repaired[idx].Role != nil &&
			*repaired[idx].Role == RoleAssistant {
			assistantIndexes = append(assistantIndexes, idx)
		}
	}
	if len(assistantIndexes) == 0 {
		if !textPresent {
			return repaired
		}
		assistant := ResponseItem{
			Type:        ResponseItemTypeMessage,
			OutputIndex: outputIndex,
			Role:        textutil.Value(RoleAssistant),
			Phase:       optionalMessagePhase(phase),
			Content:     textutil.Pointer(text),
		}
		if !hasResolvedStream {
			return append([]ResponseItem{assistant}, repaired...)
		}
		insertAt := len(repaired)
		for idx, item := range repaired {
			if item.OutputIndex > outputIndex {
				insertAt = idx
				break
			}
		}
		repaired = append(repaired, ResponseItem{})
		copy(repaired[insertAt+1:], repaired[insertAt:])
		repaired[insertAt] = assistant
		return repaired
	}
	targetAssistantIdx := assistantIndexes[0]
	if hasResolvedStream {
		for _, idx := range assistantIndexes {
			if repaired[idx].OutputIndex == outputIndex {
				targetAssistantIdx = idx
				break
			}
		}
	}
	if len(assistantIndexes) == 1 && textPresent && text != nil &&
		(repaired[targetAssistantIdx].Content == nil ||
			*repaired[targetAssistantIdx].Content != *text) {
		repaired[targetAssistantIdx].Content = textutil.Pointer(text)
	}
	if repaired[targetAssistantIdx].Phase == nil && phase != "" {
		repaired[targetAssistantIdx].Phase = textutil.Value(phase)
	}
	return repaired
}

type assistantMessageAccumulator struct {
	byIndex       map[int64]assistantAccumulatorItem
	order         []int64
	pendingDeltas map[int64]*strings.Builder
}

type assistantAccumulatorItem struct {
	message       ResponseItem
	providerPhase *ProviderPhase
	finalizedText *string
	deltaText     *strings.Builder
}

func newAssistantMessageAccumulator() *assistantMessageAccumulator {
	return &assistantMessageAccumulator{
		byIndex:       make(map[int64]assistantAccumulatorItem),
		order:         make([]int64, 0, 4),
		pendingDeltas: make(map[int64]*strings.Builder),
	}
}

func (a *assistantMessageAccumulator) Upsert(item responses.ResponseOutputItemUnion, outputIndex int64) error {
	if a == nil || item.Type != "message" {
		return nil
	}
	parsedItems, _, _, providerPhase, _, _, _, err := parseOutputItems([]responses.ResponseOutputItemUnion{item}, openAIResponsesPolicy{})
	if err != nil {
		return err
	}
	if len(parsedItems) == 0 {
		return nil
	}
	assistant := parsedItems[0]
	if assistant.Type != ResponseItemTypeMessage ||
		assistant.Role == nil ||
		*assistant.Role != RoleAssistant {
		return nil
	}
	current, exists := a.byIndex[outputIndex]
	if !exists {
		a.order = append(a.order, outputIndex)
	}
	deltaText := current.deltaText
	if pending := a.pendingDeltas[outputIndex]; pending != nil {
		deltaText = pending
		delete(a.pendingDeltas, outputIndex)
	}
	a.byIndex[outputIndex] = assistantAccumulatorItem{
		message:       assistant,
		providerPhase: providerPhase,
		finalizedText: current.finalizedText,
		deltaText:     deltaText,
	}
	return nil
}

func (a *assistantMessageAccumulator) AppendDelta(outputIndex int64, text string) {
	if a == nil || text == "" {
		return
	}
	item, exists := a.byIndex[outputIndex]
	if !exists {
		buffer := a.pendingDeltas[outputIndex]
		if buffer == nil {
			buffer = &strings.Builder{}
			a.pendingDeltas[outputIndex] = buffer
		}
		buffer.WriteString(text)
		return
	}
	if item.deltaText == nil {
		item.deltaText = &strings.Builder{}
	}
	item.deltaText.WriteString(text)
	a.byIndex[outputIndex] = item
}

func (a *assistantMessageAccumulator) SetFinalizedText(outputIndex int64, text string) {
	if a == nil {
		return
	}
	item, exists := a.byIndex[outputIndex]
	if !exists {
		a.order = append(a.order, outputIndex)
		item = assistantAccumulatorItem{
			message: ResponseItem{
				Type:        ResponseItemTypeMessage,
				OutputIndex: outputIndex,
				Role:        textutil.Value(RoleAssistant),
			},
			providerPhase: AbsentProviderPhase(),
			deltaText:     a.pendingDeltas[outputIndex],
		}
		delete(a.pendingDeltas, outputIndex)
	}
	item.finalizedText = textutil.Value(text)
	a.byIndex[outputIndex] = item
}

func (a *assistantMessageAccumulator) Resolve() (*string, MessagePhase, *ProviderPhase, int64, string, bool) {
	if a == nil {
		return nil, "", AbsentProviderPhase(), 0, "", false
	}
	segments := make([]assistantOutputSegment, 0, len(a.order))
	for _, outputIndex := range a.order {
		item, ok := a.byIndex[outputIndex]
		if !ok ||
			item.message.Type != ResponseItemTypeMessage ||
			item.message.Role == nil ||
			*item.message.Role != RoleAssistant {
			continue
		}
		phase := MessagePhase("")
		if item.message.Phase != nil {
			phase = *item.message.Phase
		}
		text := item.message.Content
		if (text == nil || strings.TrimSpace(*text) == "") && item.finalizedText != nil {
			text = item.finalizedText
		}
		deltaText := ""
		if item.deltaText != nil {
			deltaText = item.deltaText.String()
		}
		if (text == nil || strings.TrimSpace(*text) == "") && deltaText != "" {
			text = textutil.Value(deltaText)
		}
		text = resolveAssistantContent(RoleAssistant, phase, text)
		segments = append(segments, assistantOutputSegment{
			Text:          optionalStringValue(text),
			Content:       textutil.Pointer(text),
			DeltaText:     deltaText,
			Phase:         phase,
			ProviderPhase: item.providerPhase,
			OutputIndex:   outputIndex,
		})
	}
	text, phase, providerPhase, outputIndex, deltaText, resolved := resolveAssistantOutput(segments)
	if len(a.pendingDeltas) > 0 {
		return nil, "", AbsentProviderPhase(), 0, "", false
	}
	return text, phase, providerPhase, outputIndex, deltaText, resolved
}

func (a *assistantMessageAccumulator) Phase(outputIndex int64) MessagePhase {
	if a == nil {
		return ""
	}
	item, ok := a.byIndex[outputIndex]
	if !ok ||
		item.message.Type != ResponseItemTypeMessage ||
		item.message.Role == nil ||
		*item.message.Role != RoleAssistant {
		return ""
	}
	return providerPhaseProjection(item.providerPhase)
}

func optionalMessagePhase(phase MessagePhase) *MessagePhase {
	if strings.TrimSpace(string(phase)) == "" {
		return nil
	}
	return textutil.Value(phase)
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
