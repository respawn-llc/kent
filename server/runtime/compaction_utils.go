package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"builder/server/llm"
)

const (
	estimatedInlineImagePayloadTokens = 256
	estimatedInlineFilePayloadTokens  = 512
)

func (e *Engine) providerCapabilities(ctx context.Context) (llm.ProviderCapabilities, error) {
	if e.cfg.ProviderCapabilitiesOverride != nil {
		return *e.cfg.ProviderCapabilitiesOverride, nil
	}
	if caps, ok := llm.ProviderCapabilitiesFromLocked(e.store.Meta().Locked); ok {
		return caps, nil
	}
	return e.currentProviderCapabilities(ctx)
}

func (e *Engine) currentProviderCapabilities(ctx context.Context) (llm.ProviderCapabilities, error) {
	if e.cfg.ProviderCapabilitiesOverride != nil {
		return *e.cfg.ProviderCapabilitiesOverride, nil
	}
	provider, ok := e.llm.(llm.ProviderCapabilitiesClient)
	if !ok {
		return llm.ProviderCapabilities{}, fmt.Errorf("provider capabilities are unavailable for client %T", e.llm)
	}
	providerCaps, err := provider.ProviderCapabilities(ctx)
	if err != nil {
		return llm.ProviderCapabilities{}, err
	}
	return providerCaps, nil
}

func CompactionNoticeText(count int) string {
	return fmt.Sprintf("context compacted for the %s time", ordinal(count))
}

func ordinal(v int) string {
	if v <= 0 {
		return "0th"
	}
	if v%100 >= 11 && v%100 <= 13 {
		return fmt.Sprintf("%dth", v)
	}
	switch v % 10 {
	case 1:
		return fmt.Sprintf("%dst", v)
	case 2:
		return fmt.Sprintf("%dnd", v)
	case 3:
		return fmt.Sprintf("%drd", v)
	default:
		return fmt.Sprintf("%dth", v)
	}
}

func (e *Engine) trimCompactionInputToLimit(
	ctx context.Context,
	model string,
	instructions string,
	items []llm.ResponseItem,
	limit int,
) ([]llm.ResponseItem, int) {
	out := llm.CloneResponseItems(items)
	if limit <= 0 {
		return out, 0
	}
	eligibleCount := countCompactionEligibleItems(out)
	if eligibleCount <= 0 {
		return out, 0
	}
	type removedTokenEvaluation struct {
		items  []llm.ResponseItem
		tokens int
		ok     bool
		ready  bool
	}
	evaluations := make(map[int]removedTokenEvaluation, 8)
	evaluations[0] = removedTokenEvaluation{items: out, tokens: 0, ok: false, ready: false}
	evaluateRemoved := func(removed int) ([]llm.ResponseItem, int, bool) {
		if removed < 0 || removed > eligibleCount {
			return nil, 0, false
		}
		if cached, ok := evaluations[removed]; ok && cached.ready {
			return cached.items, cached.tokens, cached.ok
		}
		if removed == 0 {
			tokens, ok := e.inputTokensForItems(ctx, model, instructions, out)
			evaluations[removed] = removedTokenEvaluation{items: out, tokens: tokens, ok: ok, ready: true}
			return out, tokens, ok
		}
		candidateItems, candidateRemoved := trimOldestEligibleItems(out, removed)
		if candidateRemoved <= 0 {
			evaluations[removed] = removedTokenEvaluation{items: candidateItems, tokens: 0, ok: false, ready: true}
			return candidateItems, 0, false
		}
		tokens, ok := e.inputTokensForItems(ctx, model, instructions, candidateItems)
		evaluations[removed] = removedTokenEvaluation{items: candidateItems, tokens: tokens, ok: ok, ready: true}
		return candidateItems, tokens, ok
	}
	_, fullTokens, ok := evaluateRemoved(0)
	if !ok {
		return trimCompactionInputEstimated(out, limit)
	}
	if fullTokens <= limit {
		return out, 0
	}

	estimatedOverflow := fullTokens - limit
	step := compactionTrimStep(out, estimatedOverflow)
	if step < 1 {
		step = 1
	}
	if step > eligibleCount {
		step = eligibleCount
	}

	high := step
	highItems, highTokens, ok := evaluateRemoved(high)
	if !ok {
		return out, 0
	}
	highRemoved := high
	for highTokens > limit && high < eligibleCount {
		nextHigh := high * 2
		if nextHigh > eligibleCount {
			nextHigh = eligibleCount
		}
		high = nextHigh
		highItems, highTokens, ok = evaluateRemoved(high)
		if !ok {
			return trimCompactionInputEstimated(out, limit)
		}
		highRemoved = high
	}
	if highTokens > limit {
		return highItems, highRemoved
	}

	low := 0
	bestRemoved := high
	bestItems := highItems
	for low+1 < high {
		mid := (low + high) / 2
		midItems, midTokens, ok := evaluateRemoved(mid)
		if !ok {
			return trimCompactionInputEstimated(out, limit)
		}
		if midTokens <= limit {
			high = mid
			bestRemoved = mid
			bestItems = midItems
			continue
		}
		low = mid
	}
	return bestItems, bestRemoved
}

func (e *Engine) inputTokensForItems(ctx context.Context, model string, instructions string, items []llm.ResponseItem) (int, bool) {
	req, ok := buildTokenCountRequestForItems(model, instructions, items)
	if !ok {
		return 0, false
	}
	return e.requestInputTokensPrecisely(ctx, req)
}

func trimCompactionInputEstimated(items []llm.ResponseItem, limit int) ([]llm.ResponseItem, int) {
	out := llm.CloneResponseItems(items)
	if limit <= 0 {
		return out, 0
	}
	trimmed := 0
	for estimateItemsTokens(out) > limit {
		next, removed := trimOldestEligibleItems(out, 1)
		if removed <= 0 {
			break
		}
		out = next
		trimmed += removed
	}
	return out, trimmed
}

func countCompactionEligibleItems(items []llm.ResponseItem) int {
	return len(compactionTrimUnits(items))
}

func compactionTrimStep(items []llm.ResponseItem, overflowTokens int) int {
	if overflowTokens <= 0 {
		return 1
	}
	units := compactionTrimUnits(items)
	eligibleCount := len(units)
	eligibleEstimatedTokens := 0
	for _, unit := range units {
		eligibleEstimatedTokens += estimateItemsTokens(unit.items(items))
	}
	if eligibleCount <= 0 {
		return 1
	}
	avgTokensPerItem := eligibleEstimatedTokens / eligibleCount
	if avgTokensPerItem < 1 {
		avgTokensPerItem = 1
	}
	step := (overflowTokens + avgTokensPerItem - 1) / avgTokensPerItem
	if step < 1 {
		step = 1
	}
	if step > eligibleCount {
		step = eligibleCount
	}
	return step
}

func buildTokenCountRequestForItems(model string, instructions string, items []llm.ResponseItem) (llm.Request, bool) {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return llm.Request{}, false
	}
	req := llm.Request{
		Model:        trimmedModel,
		SystemPrompt: strings.TrimSpace(instructions),
		Items:        sanitizeItemsForLLM(items),
		Tools:        []llm.Tool{},
	}
	if err := req.Validate(); err != nil {
		return llm.Request{}, false
	}
	return req, true
}

type compactionTrimUnit struct {
	indexes []int
}

func (u compactionTrimUnit) items(source []llm.ResponseItem) []llm.ResponseItem {
	if len(u.indexes) == 0 || len(source) == 0 {
		return nil
	}
	items := make([]llm.ResponseItem, 0, len(u.indexes))
	for _, idx := range u.indexes {
		if idx < 0 || idx >= len(source) {
			continue
		}
		items = append(items, source[idx])
	}
	return items
}

func compactionTrimUnits(items []llm.ResponseItem) []compactionTrimUnit {
	if len(items) == 0 {
		return nil
	}
	units := make([]compactionTrimUnit, 0, len(items))
	callUnits := make(map[string]int, len(items))
	for idx, item := range items {
		switch item.Type {
		case llm.ResponseItemTypeFunctionCall, llm.ResponseItemTypeCustomToolCall:
			if !isCompactionTrimEligible(item) {
				continue
			}
			units = append(units, compactionTrimUnit{indexes: []int{idx}})
			if callID := compactionTrimCallID(item); callID != "" {
				callUnits[callID] = len(units) - 1
			}
		case llm.ResponseItemTypeFunctionCallOutput, llm.ResponseItemTypeCustomToolOutput:
			callID := compactionTrimCallID(item)
			if unitIdx, ok := callUnits[callID]; ok {
				units[unitIdx].indexes = append(units[unitIdx].indexes, idx)
				continue
			}
			if !isCompactionTrimEligible(item) {
				continue
			}
			units = append(units, compactionTrimUnit{indexes: []int{idx}})
		default:
			if !isCompactionTrimEligible(item) {
				continue
			}
			units = append(units, compactionTrimUnit{indexes: []int{idx}})
		}
	}
	return units
}

func compactionTrimCallID(item llm.ResponseItem) string {
	callID := strings.TrimSpace(item.CallID)
	if callID != "" {
		return callID
	}
	return strings.TrimSpace(item.ID)
}

func trimOldestEligibleItems(items []llm.ResponseItem, count int) ([]llm.ResponseItem, int) {
	out := llm.CloneResponseItems(items)
	if count <= 0 {
		return out, 0
	}
	units := compactionTrimUnits(out)
	if len(units) == 0 {
		return out, 0
	}
	if count > len(units) {
		count = len(units)
	}
	trimmedIndexes := make(map[int]struct{}, count)
	for _, unit := range units[:count] {
		for _, idx := range unit.indexes {
			trimmedIndexes[idx] = struct{}{}
		}
	}
	trimmed := 0
	filtered := make([]llm.ResponseItem, 0, len(out)-len(trimmedIndexes))
	for idx, item := range out {
		if _, ok := trimmedIndexes[idx]; ok {
			trimmed++
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, trimmed
}

func isCompactionTrimEligible(item llm.ResponseItem) bool {
	if item.Type != llm.ResponseItemTypeMessage {
		return true
	}
	return item.Role != llm.RoleUser
}

func sanitizeRemoteCompactionOutput(output []llm.ResponseItem) ([]llm.ResponseItem, error) {
	filtered := make([]llm.ResponseItem, 0, len(output))
	hasCheckpoint := false
	typeCounts := make(map[string]int)
	for _, item := range output {
		typeCounts[outputItemTypeLabel(item)]++
		switch item.Type {
		case llm.ResponseItemTypeMessage:
			if item.Role == llm.RoleUser && strings.TrimSpace(item.Content) != "" {
				filtered = append(filtered, item)
			}
		case llm.ResponseItemTypeCompaction:
			if strings.TrimSpace(item.EncryptedContent) == "" {
				continue
			}
			filtered = append(filtered, item)
			hasCheckpoint = true
		case llm.ResponseItemTypeReasoning:
			if strings.TrimSpace(item.EncryptedContent) == "" {
				continue
			}
			filtered = append(filtered, item)
			hasCheckpoint = true
		case llm.ResponseItemTypeOther:
			if !itemHasEncryptedCheckpoint(item) {
				continue
			}
			filtered = append(filtered, item)
			hasCheckpoint = true
		}
	}
	if !hasCheckpoint {
		return nil, fmt.Errorf("%w (types=%s)", errRemoteCompactionMissingCheckpoint, formatOutputTypeCounts(typeCounts))
	}
	return filtered, nil
}

func outputItemTypeLabel(item llm.ResponseItem) string {
	if v := strings.TrimSpace(string(item.Type)); v != "" {
		return v
	}
	if len(item.Raw) > 0 {
		var decoded struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item.Raw, &decoded); err == nil {
			if v := strings.TrimSpace(decoded.Type); v != "" {
				return v
			}
		}
	}
	return "unknown"
}

func itemHasEncryptedCheckpoint(item llm.ResponseItem) bool {
	if strings.TrimSpace(item.EncryptedContent) != "" {
		return true
	}
	if len(item.Raw) == 0 || !json.Valid(item.Raw) {
		return false
	}
	var decoded struct {
		EncryptedContent string `json:"encrypted_content"`
	}
	if err := json.Unmarshal(item.Raw, &decoded); err != nil {
		return false
	}
	return strings.TrimSpace(decoded.EncryptedContent) != ""
}

func formatOutputTypeCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}
	return strings.Join(parts, ",")
}

func (e *Engine) rebuildLocalCompactionHistory(ctx context.Context, model string, items []llm.ResponseItem, summary string, carryoverLimit int) []llm.ResponseItem {
	userMessages := make([]llm.ResponseItem, 0, len(items))
	for _, item := range items {
		if item.Type == llm.ResponseItemTypeMessage && item.Role == llm.RoleUser && item.MessageType != llm.MessageTypeCompactionSummary && strings.TrimSpace(item.Content) != "" {
			userMessages = append(userMessages, item)
		}
	}

	if carryoverLimit <= 0 {
		carryoverLimit = 20_000
	}
	selected := e.selectLocalCarryoverMessages(ctx, model, userMessages, carryoverLimit)

	summaryMessage := llm.ResponseItem{
		Type:        llm.ResponseItemTypeMessage,
		Role:        llm.RoleDeveloper,
		MessageType: llm.MessageTypeCompactionSummary,
		Content:     strings.TrimSpace(summary),
	}

	out := make([]llm.ResponseItem, 0, len(selected)+1)
	out = append(out, selected...)
	out = append(out, summaryMessage)
	return out
}

func (e *Engine) selectLocalCarryoverMessages(
	ctx context.Context,
	model string,
	userMessages []llm.ResponseItem,
	carryoverLimit int,
) []llm.ResponseItem {
	if len(userMessages) == 0 {
		return nil
	}
	fallback := selectLocalCarryoverMessagesEstimated(userMessages, carryoverLimit)
	if _, ok := e.llm.(llm.RequestInputTokenCountClient); !ok {
		return fallback
	}
	if !e.preciseInputTokenCountSupported(ctx) {
		return fallback
	}

	usedPrecise := false
	low := 1
	high := len(userMessages)
	best := 1
	for low <= high {
		mid := (low + high) / 2
		candidate := llm.CloneResponseItems(userMessages[len(userMessages)-mid:])
		tokens := estimateItemsTokens(candidate)
		if precise, ok := e.inputTokensForItems(ctx, model, "", candidate); ok {
			usedPrecise = true
			tokens = precise
		} else {
			break
		}
		if tokens <= carryoverLimit {
			best = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if !usedPrecise {
		return fallback
	}
	if best < 1 {
		best = 1
	}
	return llm.CloneResponseItems(userMessages[len(userMessages)-best:])
}

func selectLocalCarryoverMessagesEstimated(userMessages []llm.ResponseItem, carryoverLimit int) []llm.ResponseItem {
	selected := make([]llm.ResponseItem, 0, len(userMessages))
	budget := 0
	for i := len(userMessages) - 1; i >= 0; i-- {
		item := userMessages[i]
		estimated := estimateItemsTokens([]llm.ResponseItem{item})
		if len(selected) > 0 && budget+estimated > carryoverLimit {
			break
		}
		budget += estimated
		selected = append(selected, item)
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}

func estimateItemsTokens(items []llm.ResponseItem) int {
	totalTokens := 0
	for _, item := range items {
		totalTokens += estimateTextTokens(item.Content)
		totalTokens += estimateTextTokens(item.ID)
		totalTokens += estimateTextTokens(item.Name)
		totalTokens += estimateTextTokens(item.CallID)
		totalTokens += estimateTextTokens(item.EncryptedContent)
		totalTokens += estimateTextTokens(string(item.Arguments))
		if outputTokens, ok := estimateStructuredOutputTokens(item.Output); ok {
			totalTokens += outputTokens
		} else {
			totalTokens += estimateTextTokens(string(item.Output))
		}
		totalTokens += estimateTextTokens(string(item.Raw))
		for _, summary := range item.ReasoningSummary {
			totalTokens += estimateTextTokens(summary.Role)
			totalTokens += estimateTextTokens(summary.Text)
		}
	}
	if totalTokens <= 0 {
		return 0
	}
	return totalTokens
}

func estimateStructuredOutputTokens(raw json.RawMessage) (int, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || !strings.HasPrefix(trimmed, "[") {
		return 0, false
	}

	var items []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
		Detail   string `json:"detail"`
		FileID   string `json:"file_id"`
		FileData string `json:"file_data"`
		FileURL  string `json:"file_url"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, false
	}
	if len(items) == 0 {
		return 0, false
	}

	total := 0
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "input_text":
			total += estimateTextTokens(item.Text)
		case "input_image":
			total += estimatedInlineImagePayloadTokens
			total += estimateReferenceTokens(item.ImageURL)
			total += estimateReferenceTokens(item.FileID)
			total += estimateTextTokens(item.Detail)
		case "input_file":
			total += estimatedInlineFilePayloadTokens
			total += estimateReferenceTokens(item.FileData)
			total += estimateReferenceTokens(item.FileID)
			total += estimateReferenceTokens(item.FileURL)
			total += estimateTextTokens(item.Filename)
		default:
			return 0, false
		}
	}
	return total, true
}

func estimateReferenceTokens(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		return 0
	}
	return estimateTextTokens(trimmed)
}

func estimateTextTokens(value string) int {
	if value == "" {
		return 0
	}
	return (len(value) + 3) / 4
}

// estimateTextTokensPOC keeps the more sophisticated local heuristic around for
// future calibration work, but the runtime currently uses estimateTextTokens.
func estimateTextTokensPOC(value string) int {
	if value == "" {
		return 0
	}
	total := 0
	currentKind := estimatedWordKindNone
	currentLen := 0
	prevWasLower := false
	prevWasDigit := false

	flushWord := func() {
		if currentLen <= 0 {
			currentKind = estimatedWordKindNone
			prevWasLower = false
			prevWasDigit = false
			return
		}
		total += estimateWordPieceTokensPOC(currentLen, currentKind)
		currentKind = estimatedWordKindNone
		currentLen = 0
		prevWasLower = false
		prevWasDigit = false
	}

	for _, r := range value {
		switch {
		case unicode.IsSpace(r):
			flushWord()
		case isCJKRunePOC(r):
			flushWord()
			total++
		case isEstimatedWordRunePOC(r):
			nextKind := classifyEstimatedWordRunePOC(r)
			nextIsLower := unicode.IsLower(r)
			nextIsDigit := unicode.IsDigit(r)
			if currentLen > 0 && shouldSplitEstimatedWordPiecePOC(currentKind, nextKind, prevWasLower, nextIsLower, prevWasDigit, nextIsDigit) {
				flushWord()
			}
			if currentLen == 0 {
				currentKind = nextKind
			} else if currentKind != nextKind {
				currentKind = estimatedWordKindMixed
			}
			currentLen++
			prevWasLower = nextIsLower
			prevWasDigit = nextIsDigit
		default:
			flushWord()
			total += estimatePunctuationTokensPOC(r)
		}
	}
	flushWord()
	return total
}

type estimatedWordKind uint8

const (
	estimatedWordKindNone estimatedWordKind = iota
	estimatedWordKindLower
	estimatedWordKindUpper
	estimatedWordKindDigit
	estimatedWordKindOtherLetter
	estimatedWordKindMixed
)

func isEstimatedWordRunePOC(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func classifyEstimatedWordRunePOC(r rune) estimatedWordKind {
	switch {
	case unicode.IsDigit(r):
		return estimatedWordKindDigit
	case r <= unicode.MaxASCII && unicode.IsLower(r):
		return estimatedWordKindLower
	case r <= unicode.MaxASCII && unicode.IsUpper(r):
		return estimatedWordKindUpper
	default:
		return estimatedWordKindOtherLetter
	}
}

func shouldSplitEstimatedWordPiecePOC(currentKind, nextKind estimatedWordKind, prevWasLower, nextIsLower, prevWasDigit, nextIsDigit bool) bool {
	if currentKind == estimatedWordKindNone {
		return false
	}
	if prevWasDigit != nextIsDigit {
		return true
	}
	if prevWasLower && nextKind == estimatedWordKindUpper {
		return true
	}
	if currentKind == estimatedWordKindOtherLetter || nextKind == estimatedWordKindOtherLetter {
		return false
	}
	if currentKind == estimatedWordKindUpper && nextIsLower {
		return false
	}
	return false
}

func estimateWordPieceTokensPOC(length int, kind estimatedWordKind) int {
	if length <= 0 {
		return 0
	}
	estimate := 1
	switch kind {
	case estimatedWordKindDigit:
		estimate = estimatePiecewiseWordTokensPOC(length, 3, 16, 3)
	case estimatedWordKindUpper:
		estimate = estimatePiecewiseWordTokensPOC(length, 4, 16, 4)
	case estimatedWordKindLower:
		estimate = estimatePiecewiseWordTokensPOC(length, 7, 16, 4)
	case estimatedWordKindOtherLetter:
		estimate = estimatePiecewiseWordTokensPOC(length, 5, 16, 4)
	case estimatedWordKindMixed:
		estimate = estimatePiecewiseWordTokensPOC(length, 5, 16, 4)
	default:
		return estimate
	}
	if length >= 128 {
		denseFallback := (length + 3) / 4
		if denseFallback > estimate {
			estimate = denseFallback
		}
	}
	return estimate
}

func estimatePiecewiseWordTokensPOC(length int, shortDivisor int, tailStart int, tailDivisor int) int {
	if length <= 0 {
		return 0
	}
	if length <= tailStart {
		return 1 + (length-1)/shortDivisor
	}
	head := 1 + (tailStart-1)/shortDivisor
	return head + (length-tailStart)/tailDivisor
}

func estimatePunctuationTokensPOC(r rune) int {
	if unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return 1
	}
	return 1
}

func isCJKRunePOC(r rune) bool {
	return unicode.In(r,
		unicode.Han,
		unicode.Hiragana,
		unicode.Katakana,
		unicode.Hangul,
	)
}
