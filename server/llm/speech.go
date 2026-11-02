package llm

import (
	"slices"
	"time"
)

type SpeechEncoding struct {
	MIMEType  string
	Extension string
}

type SpeechProfile struct {
	Endpoint     string
	Model        *string
	Encodings    []SpeechEncoding
	MaxBytes     int64
	MaxDuration  time.Duration
	promptModels []string
}

func (p SpeechProfile) SupportsPrompt() bool {
	return p.Model != nil && slices.Contains(p.promptModels, *p.Model)
}

func speechProfile(endpoint string, model *string, maxBytes int64, promptModels []string) *SpeechProfile {
	return &SpeechProfile{
		Endpoint: endpoint, Model: model, MaxBytes: maxBytes, MaxDuration: 10 * time.Minute,
		promptModels: promptModels,
		Encodings: []SpeechEncoding{
			{MIMEType: "audio/mp4", Extension: "m4a"},
			{MIMEType: "audio/webm", Extension: "webm"},
			{MIMEType: "audio/ogg", Extension: "ogg"},
		},
	}
}
