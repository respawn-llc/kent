package startup

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func handleTestOpenAIInputTokenCount(w http.ResponseWriter, r *http.Request, inputTokens int) bool {
	if r.URL.Path != "/responses/input_tokens" {
		return false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"object":"response.input_tokens","input_tokens":%d}`, inputTokens)
	return true
}

func writeTestOpenAICompletedResponseStream(w http.ResponseWriter, assistantText string, inputTokens, outputTokens int) {
	totalTokens := inputTokens + outputTokens
	encodedAssistantText, err := json.Marshal(assistantText)
	if err != nil {
		panic(fmt.Sprintf("marshal assistant text json: %v", err))
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":%d,\"output_tokens\":%d,\"total_tokens\":%d},\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"phase\":\"final\",\"content\":[{\"type\":\"output_text\",\"text\":%s}]}]}}\n\n", inputTokens, outputTokens, totalTokens, encodedAssistantText)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
