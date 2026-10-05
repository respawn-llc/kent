package protocol

type SubscribeResponse struct {
	Stream string `json:"stream"`
}

type StreamCompleteParams struct {
	Code                  int    `json:"code,omitempty"`
	Message               string `json:"message,omitempty"`
	TranscriptCloseReason string `json:"transcript_close_reason,omitempty"`
}
