package speech

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"core/server/llm"
	"core/shared/config"
)

type capturedSpeechRequest struct {
	model    string
	prompt   *string
	keyterms []string
}

func newSpeechRequestServer(t *testing.T) (*httptest.Server, <-chan capturedSpeechRequest) {
	t.Helper()
	requests := make(chan capturedSpeechRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		request := capturedSpeechRequest{model: r.FormValue("model")}
		if values, present := r.MultipartForm.Value["prompt"]; present {
			request.prompt = &values[0]
		}
		request.keyterms = r.MultipartForm.Value["keyterm"]
		requests <- request
		_, _ = io.WriteString(w, `{"text":"transcribed"}`)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

func allocateSpeechInvocation(t *testing.T, configText, endpoint string) (Invocation, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	app, err := config.LoadGlobal(config.LoadOptions{ConfigRoot: root})
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	selection, err := EligibleDefaults(app.Settings)
	if err != nil {
		t.Fatalf("resolve speech selection: %v", err)
	}
	invocation, err := selection.Allocate(t.Context(), nil)
	if err != nil {
		t.Fatalf("allocate speech connection: %v", err)
	}
	invocation.Profile.Endpoint = endpoint
	return invocation, app.Settings.SpeechToTextPrompt
}

func transcribeSpeechRequest(t *testing.T, invocation Invocation, requests <-chan capturedSpeechRequest) capturedSpeechRequest {
	t.Helper()
	transcript, err := transcribe(t.Context(), invocation, nil, llm.SpeechEncoding{MIMEType: "audio/mp4", Extension: "m4a"}, io.NopCloser(strings.NewReader("audio")))
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if transcript != "transcribed" {
		t.Fatalf("transcript = %q", transcript)
	}
	return <-requests
}

func TestTranscriptionIncludesDefaultPromptForOpenAI(t *testing.T) {
	server, requests := newSpeechRequestServer(t)
	invocation, effectivePrompt := allocateSpeechInvocation(t, `
connection = "openai"
[connections.openai]
protocol = "responses"
endpoint = "https://api.openai.com/v1"
`, server.URL)

	request := transcribeSpeechRequest(t, invocation, requests)
	if request.prompt == nil || *request.prompt != effectivePrompt {
		t.Fatalf("request prompt = %v, want config-resolved prompt", request.prompt)
	}
}

func TestTranscriptionIncludesCustomPromptForOpenAI(t *testing.T) {
	server, requests := newSpeechRequestServer(t)
	invocation, effectivePrompt := allocateSpeechInvocation(t, `
connection = "openai"
speech_to_text_prompt = "  custom first line\nsecond line  \n"
[connections.openai]
protocol = "responses"
endpoint = "https://api.openai.com/v1"
`, server.URL)

	request := transcribeSpeechRequest(t, invocation, requests)
	if request.prompt == nil || *request.prompt != effectivePrompt {
		t.Fatalf("request prompt = %v, want config-resolved custom value", request.prompt)
	}
}

func TestTranscriptionIncludesPromptForSupportedModelOverride(t *testing.T) {
	server, requests := newSpeechRequestServer(t)
	invocation, effectivePrompt := allocateSpeechInvocation(t, `
connection = "openai"
speech_to_text_prompt = "  custom first line\nsecond line  \n"
[connections.openai]
protocol = "responses"
endpoint = "https://api.openai.com/v1"
speech_to_text_model = "gpt-4o-transcribe"
`, server.URL)

	request := transcribeSpeechRequest(t, invocation, requests)
	if request.model != "gpt-4o-transcribe" {
		t.Fatalf("request model = %q, want configured override", request.model)
	}
	if request.prompt == nil || *request.prompt != effectivePrompt {
		t.Fatalf("request prompt = %v, want config-resolved custom value", request.prompt)
	}
}

func TestTranscriptionOmitsPromptForUnsupportedModelOverride(t *testing.T) {
	for _, test := range []struct {
		name          string
		promptSetting string
	}{
		{name: "default"},
		{name: "custom", promptSetting: "speech_to_text_prompt = \"custom replacement\\n\"\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, requests := newSpeechRequestServer(t)
			configText := "connection = \"openai\"\n" + test.promptSetting + `
[connections.openai]
protocol = "responses"
endpoint = "https://api.openai.com/v1"
speech_to_text_model = "gpt-4o-transcribe-diarize"
`
			invocation, _ := allocateSpeechInvocation(t, configText, server.URL)

			request := transcribeSpeechRequest(t, invocation, requests)
			if request.model != "gpt-4o-transcribe-diarize" {
				t.Fatalf("request model = %q, want configured override", request.model)
			}
			if request.prompt != nil || len(request.keyterms) != 0 {
				t.Fatalf("unsupported model fields: prompt=%v keyterms=%v", request.prompt, request.keyterms)
			}
		})
	}
}

func TestTranscriptionOmitsPromptForUnverifiedModelOverride(t *testing.T) {
	server, requests := newSpeechRequestServer(t)
	invocation, _ := allocateSpeechInvocation(t, `
connection = "openai"
speech_to_text_prompt = "custom replacement"
[connections.openai]
protocol = "responses"
endpoint = "https://api.openai.com/v1"
speech_to_text_model = "gpt-transcribe-alias"
`, server.URL)

	request := transcribeSpeechRequest(t, invocation, requests)
	if request.model != "gpt-transcribe-alias" {
		t.Fatalf("request model = %q, want configured override", request.model)
	}
	if request.prompt != nil || len(request.keyterms) != 0 {
		t.Fatalf("unverified model fields: prompt=%v keyterms=%v", request.prompt, request.keyterms)
	}
}

func TestTranscriptionOmitsPromptForUnsupportedRoutes(t *testing.T) {
	for _, protocol := range []config.ConnectionProtocol{config.ConnectionChatGPT, config.ConnectionGrokOAuthAPI} {
		t.Run(string(protocol), func(t *testing.T) {
			server, requests := newSpeechRequestServer(t)
			configText := `
connection = "speech"
speech_to_text_prompt = "custom replacement"
[connections.speech]
protocol = "` + string(protocol) + `"
`
			invocation, _ := allocateSpeechInvocation(t, configText, server.URL)

			request := transcribeSpeechRequest(t, invocation, requests)
			if request.prompt != nil || len(request.keyterms) != 0 {
				t.Fatalf("unsupported route fields: prompt=%v keyterms=%v", request.prompt, request.keyterms)
			}
		})
	}
}

func TestTranscriptionNoOp(t *testing.T) {
	for _, text := range []string{"", " \n\t", "？！—…", "🎙️✨", "$ + ="} {
		if result := Normalize(text); result.GetNoOp() == nil {
			t.Errorf("expected successful no-op for %q", text)
		}
	}
	for _, text := range []string{"  Привет!\n", "你好", "مرحبا", "१२३", "42 🎙️"} {
		if result := Normalize(text); result.GetTranscript() != text {
			t.Errorf("provider text changed: %q", text)
		}
	}
}
