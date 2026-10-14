package sessionnaming

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"core/prompts"
	"core/server/llm"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func PrepareRequest(target llm.NanoTarget, task string, commentary *string) (llm.Request, error) {
	budget := target.Settings.ModelContextWindow * 85 / 100
	taskRunes := utf8.RuneCountInString(task)
	commentaryRunes := 0
	if commentary != nil {
		commentaryRunes = utf8.RuneCountInString(*commentary)
	}
	total := taskRunes + commentaryRunes
	var best *llm.Request
	for low, high := 0, total; low <= high; {
		keep := low + (high-low)/2
		taskKeep := keep
		if total > 0 {
			taskKeep = keep * taskRunes / total
		}
		taskExcerpt := textutil.MiddleTruncate(task, max(min(taskRunes, 3), taskKeep))
		var responseExcerpt *string
		if commentary != nil {
			responseExcerpt = textutil.Value(textutil.MiddleTruncate(*commentary, max(min(commentaryRunes, 3), keep-taskKeep)))
		}
		prompt, err := prompts.RenderSessionNamingPrompt(taskExcerpt, responseExcerpt)
		if err != nil {
			return llm.Request{}, err
		}
		request := llm.Request{Model: target.Settings.Model, MaxTokens: target.Settings.ModelContextWindow - budget,
			ToolChoiceMode: llm.ToolChoiceModeAutomatic,
			Items:          llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: &prompt}})}
		tokens, err := llm.EstimateRequestTokens(target.Estimator, request)
		if err != nil {
			return llm.Request{}, err
		}
		if tokens <= budget {
			best = &request
			low = keep + 1
		} else {
			high = keep - 1
		}
	}
	if best == nil {
		return llm.Request{}, errors.New("minimal session-naming prompt exceeds nano input budget")
	}
	return *best, nil
}

func InterpretTitle(result string, category sessioncontract.SessionCategory, shortID runtimeids.SessionShortID) (string, error) {
	title := strings.TrimSpace(result)
	if title == "" || strings.ContainsAny(title, "\r\n") {
		return "", errors.New("session-naming result must be a nonblank single line")
	}
	if category == sessioncontract.SessionCategorySubagent {
		return fmt.Sprintf("Subagent %s: %s", shortID.String(), title), nil
	}
	return title, nil
}
