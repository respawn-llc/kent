package llm

import (
	"encoding/json"
	"net/http"
)

func (c *CodexDispatchContext) observeTurnStateMetadata(raw []byte) {
	var metadata struct {
		Headers struct {
			TurnState any `json:"x-codex-turn-state"`
		} `json:"headers"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		c.observeInvalidTurnState(codexTurnStateSourceMetadata)
		return
	}
	value := metadata.Headers.TurnState
	for value != nil {
		switch candidate := value.(type) {
		case string:
			c.observeTurnStateCandidate(candidate, codexTurnStateSourceMetadata)
			return
		case []any:
			// Codex accepts the first value, including nested arrays.
			if len(candidate) != 0 {
				value = candidate[0]
				continue
			}
		}
		c.observeInvalidTurnState(codexTurnStateSourceMetadata)
		return
	}
}

func (c *CodexDispatchContext) observeTurnStateHTTPHeader(header http.Header) {
	if c == nil || header == nil {
		return
	}
	values := header.Values(codexTurnStateHeader)
	if len(values) != 0 {
		c.observeTurnStateCandidate(values[0], codexTurnStateSourceHTTPHeader)
	}
}

func observeCodexTurnStateResponseHeader(dispatch *CodexDispatchContext, response *http.Response, observed *bool) {
	if observed == nil || *observed || response == nil {
		return
	}
	*observed = true
	dispatch.observeTurnStateHTTPHeader(response.Header)
}
