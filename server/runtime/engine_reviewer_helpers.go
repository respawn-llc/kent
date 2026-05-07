package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"builder/server/llm"
	"builder/server/session"
	"builder/shared/transcript"
)

type reviewerSuggestionsResult struct {
	Suggestions           []string
	CacheHitPercent       int
	HasCacheHitPercentage bool
}

type reviewerRequestConfig struct {
	Model             string
	ThinkingLevel     string
	ModelCapabilities session.LockedModelCapabilities
}

func (e *Engine) runReviewerSuggestions(ctx context.Context, stepID string, reviewerClient llm.Client) (reviewerSuggestionsResult, error) {
	e.ensureOrchestrationCollaborators()
	return e.reviewerFlow.RunSuggestions(ctx, stepID, reviewerClient)
}

func parseReviewerSuggestionsObject(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	var payload struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	return sanitizeReviewerSuggestions(payload.Suggestions)
}

func sanitizeReviewerSuggestions(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, suggestion := range in {
		trimmed := strings.TrimSpace(suggestion)
		if trimmed == "" {
			continue
		}
		if strings.EqualFold(trimmed, reviewerNoopToken) {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildReviewerRequestMessages(messages []llm.Message, workspaceRoot string, model string, thinkingLevel string, headless bool, disabledSkills map[string]bool) ([]llm.Message, error) {
	return buildReviewerRequestMessagesWithNow(messages, workspaceRoot, model, thinkingLevel, headless, disabledSkills, time.Now())
}

func buildReviewerRequestMessagesWithNow(messages []llm.Message, workspaceRoot string, model string, thinkingLevel string, headless bool, disabledSkills map[string]bool, now time.Time) ([]llm.Message, error) {
	return buildReviewerRequestMessagesWithBuilder(messages, newMetaContextBuilder(workspaceRoot, model, thinkingLevel, disabledSkills, now), headless)
}

func buildReviewerRequestMessagesWithBuilder(messages []llm.Message, builder metaContextBuilder, headless bool) ([]llm.Message, error) {
	metaMessages, transcriptSource := splitMetaContextMessages(messages)
	metaResult, err := builder.Build(metaContextBuildOptions{
		ExistingMessages:          metaMessages,
		IncludeAgents:             true,
		IncludeSkills:             true,
		IncludeEnvironment:        true,
		IncludeHeadless:           headless,
		PermissiveAgentsReadError: true,
	})
	if err != nil {
		return nil, err
	}
	metaMessages = metaResult.OrderedMetaMessages()
	out := make([]llm.Message, 0, len(metaMessages)+2+len(transcriptSource))
	out = append(out, metaMessages...)
	out = append(out, llm.Message{Role: llm.RoleDeveloper, Content: reviewerMetaBoundaryMessage})
	out = append(out, buildReviewerTranscriptMessages(transcriptSource)...)
	return out, nil
}

func buildReviewerTranscriptMessages(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(messages)+1)
	for _, message := range messages {
		out = append(out, reviewerTranscriptMessagesFromMessage(message)...)
	}
	if len(out) == 0 {
		out = append(out, llm.Message{Role: llm.RoleUser, Content: "No reviewable transcript entries were available for this turn."})
	}
	return out
}

func reviewerTranscriptMessagesFromMessage(message llm.Message) []llm.Message {
	if message.Role == llm.RoleDeveloper {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			return nil
		}
		if _, ok := classifyMetaContextMessage(message); ok {
			return nil
		}
		if message.MessageType == llm.MessageTypeErrorFeedback || message.MessageType == llm.MessageTypeInterruption {
			return nil
		}
		visibleEntries := VisibleChatEntriesFromMessage(message)
		if len(visibleEntries) == 0 {
			return []llm.Message{{Role: llm.RoleUser, Content: formatReviewerHiddenDeveloperMessage(message)}}
		}
		return reviewerMessagesFromChatEntries(visibleEntries)
	}
	if message.Role == llm.RoleTool && strings.TrimSpace(message.ToolCallID) == "" {
		return nil
	}
	if strings.TrimSpace(message.Content) == "" && len(message.ToolCalls) == 0 {
		return nil
	}
	return reviewerMessagesFromChatEntries(VisibleChatEntriesFromMessage(message))
}

func reviewerMessagesFromChatEntries(entries []ChatEntry) []llm.Message {
	out := make([]llm.Message, 0, len(entries))
	for _, entry := range entries {
		formatted := formatReviewerChatEntry(entry)
		if strings.TrimSpace(formatted) == "" {
			continue
		}
		out = append(out, llm.Message{Role: llm.RoleUser, Content: formatted})
	}
	return out
}

func formatReviewerHiddenDeveloperMessage(message llm.Message) string {
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return ""
	}
	return "Developer:\n" + content
}

func formatReviewerChatEntry(entry ChatEntry) string {
	label := reviewerChatEntryLabel(entry)
	text := strings.TrimSpace(reviewerChatEntryText(entry))
	if text == "" {
		return strings.TrimSpace(label)
	}
	return strings.TrimSpace(label) + "\n" + text
}

func reviewerChatEntryText(entry ChatEntry) string {
	if entry.Role == "tool_call" {
		if text := reviewerToolPresentationText(entry.ToolCall); text != "" {
			return text
		}
	}
	return strings.TrimSpace(entry.Text)
}

func reviewerChatEntryLabel(entry ChatEntry) string {
	switch entry.Role {
	case "assistant":
		return "Agent:"
	case "user":
		return "User:"
	case "tool_call":
		return "Tool call:"
	case "tool_result_ok":
		return "Tool result:"
	case "tool_result_error":
		return "Tool result error:"
	case string(transcript.EntryRoleCompactionSummary):
		return "Compaction:"
	case string(transcript.EntryRoleManualCompactionCarryover):
		return "Carryover:"
	case "warning":
		return "Warning:"
	case "system":
		return "System:"
	default:
		role := strings.ReplaceAll(strings.TrimSpace(entry.Role), "_", " ")
		if role == "" {
			return "Unknown:"
		}
		return fmt.Sprintf("%s:", titleCaseASCII(role))
	}
}

func reviewerToolPresentationText(meta *transcript.ToolCallMeta) string {
	if meta == nil {
		return ""
	}
	if meta.UsesAskQuestionRendering() {
		lines := make([]string, 0, len(meta.Suggestions)+2)
		if question := strings.TrimSpace(meta.Question); question != "" {
			lines = append(lines, "question: "+question)
		}
		for _, suggestion := range meta.Suggestions {
			trimmed := strings.TrimSpace(suggestion)
			if trimmed == "" {
				continue
			}
			lines = append(lines, "suggestion: "+trimmed)
		}
		if meta.RecommendedOptionIndex > 0 {
			lines = append(lines, fmt.Sprintf("recommended_option_index: %d", meta.RecommendedOptionIndex))
		}
		return strings.Join(lines, "\n")
	}
	lines := make([]string, 0, 4)
	if command := strings.TrimSpace(meta.Command); command != "" {
		lines = append(lines, command)
	} else if compact := strings.TrimSpace(meta.CompactText); compact != "" {
		lines = append(lines, compact)
	}
	if inlineMeta := strings.TrimSpace(meta.InlineMeta); inlineMeta != "" {
		lines = append(lines, "meta: "+inlineMeta)
	}
	if detail := strings.TrimSpace(meta.PatchDetail); detail != "" {
		lines = append(lines, detail)
	}
	if len(lines) == 0 {
		return strings.TrimSpace(meta.ToolName)
	}
	return strings.Join(lines, "\n")
}

func titleCaseASCII(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "Unknown"
	}
	runes := []rune(trimmed)
	if len(runes) == 1 {
		return strings.ToUpper(trimmed)
	}
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}

func formatReviewerDeveloperInstruction(suggestions []string) string {
	b := strings.Builder{}
	b.WriteString("Supervisor agent gave you suggestions:\n")
	for idx, suggestion := range suggestions {
		b.WriteString(strconv.Itoa(idx + 1))
		b.WriteString(". ")
		b.WriteString(suggestion)
		b.WriteString("\n")
	}
	b.WriteString("\nIf no suggestions are applicable and you don't want to say anything to the user (not the supervisor!), respond with exactly ")
	b.WriteString(reviewerNoopToken)
	b.WriteString(" and no additional text. Otherwise, address the suggestions now. The supervisor can't hear you, your response will be to the user.")
	return b.String()
}

func reviewerStatusText(status ReviewerStatus, _ []string) string {
	statusText := ""
	switch strings.TrimSpace(status.Outcome) {
	case "failed":
		if strings.TrimSpace(status.Error) == "" {
			statusText = "Supervisor ran: failed to generate suggestions."
			break
		}
		statusText = fmt.Sprintf("Supervisor ran: failed to generate suggestions: %s", status.Error)
	case "no_suggestions":
		statusText = "Supervisor ran: no suggestions."
	case "followup_failed":
		if strings.TrimSpace(status.Error) == "" {
			statusText = fmt.Sprintf("Supervisor ran: %s, but follow-up failed.", reviewerSuggestionCountLabel(status.SuggestionsCount))
			break
		}
		statusText = fmt.Sprintf("Supervisor ran: %s, but follow-up failed: %s", reviewerSuggestionCountLabel(status.SuggestionsCount), status.Error)
	case "noop":
		statusText = fmt.Sprintf("Supervisor ran: %s, no changes applied.", reviewerSuggestionCountLabel(status.SuggestionsCount))
	case "applied":
		statusText = fmt.Sprintf("Supervisor ran: %s, applied.", reviewerSuggestionCountLabel(status.SuggestionsCount))
	default:
		statusText = "Supervisor ran."
	}
	if status.HasCacheHitPercentage {
		return statusText + "\n\n" + fmt.Sprintf("%d%% cache hit", status.CacheHitPercent)
	}
	return statusText
}

func reviewerSuggestionsText(suggestions []string) string {
	if len(suggestions) == 0 {
		return ""
	}
	b := strings.Builder{}
	b.WriteString("Supervisor suggested:\n")
	for idx, suggestion := range suggestions {
		b.WriteString(strconv.Itoa(idx + 1))
		b.WriteString(". ")
		b.WriteString(strings.TrimSpace(suggestion))
		if idx < len(suggestions)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func reviewerSuggestionsCompactLabel(count int) string {
	return "Supervisor made " + reviewerSuggestionCountLabel(count) + "."
}

func reviewerSuggestionCountLabel(count int) string {
	if count <= 1 {
		return "1 suggestion"
	}
	return fmt.Sprintf("%d suggestions", count)
}

func reviewerSessionID(sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return ""
	}
	return trimmed + "/supervisor"
}

func reviewerPromptCacheKey(sessionID string, compactionCount int) string {
	return conversationPromptCacheKey(reviewerSessionID(sessionID), compactionCount)
}

func appendMissingReviewerMetaContext(messages []llm.Message, workspaceRoot string, model string, thinkingLevel string, headless bool, disabledSkills map[string]bool) ([]llm.Message, error) {
	metaMessages, transcript := splitMetaContextMessages(messages)
	builder := newMetaContextBuilder(workspaceRoot, model, thinkingLevel, disabledSkills, time.Now())
	metaResult, err := builder.Build(metaContextBuildOptions{
		ExistingMessages:          metaMessages,
		IncludeAgents:             true,
		IncludeSkills:             true,
		IncludeEnvironment:        true,
		IncludeHeadless:           headless,
		PermissiveAgentsReadError: true,
	})
	if err != nil {
		return nil, err
	}
	out := append(metaResult.OrderedMetaMessages(), transcript...)
	return out, nil
}
