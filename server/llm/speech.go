package llm

import "time"

type SpeechEncoding struct {
	MIMEType  string
	Extension string
}

type SpeechProfile struct {
	Endpoint    string
	Model       *string
	Encodings   []SpeechEncoding
	MaxBytes    int64
	MaxDuration time.Duration
}

func speechProfile(endpoint string, model *string, maxBytes int64) *SpeechProfile {
	return &SpeechProfile{
		Endpoint: endpoint, Model: model, MaxBytes: maxBytes, MaxDuration: 10 * time.Minute,
		Encodings: []SpeechEncoding{
			{MIMEType: "audio/mp4", Extension: "m4a"},
			{MIMEType: "audio/webm", Extension: "webm"},
			{MIMEType: "audio/ogg", Extension: "ogg"},
		},
	}
}
