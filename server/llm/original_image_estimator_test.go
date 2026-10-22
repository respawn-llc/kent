package llm_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"core/server/llm"
	"core/shared/config"

	webpencoder "github.com/deepteams/webp"
)

func TestOpenAITokenEstimatorOriginalInlineImageRoundsEachDimensionToThirtyTwoPixels(t *testing.T) {
	for _, test := range []struct {
		width, height int
		wantBytes     int
	}{
		{1, 1, 4},
		{31, 32, 4},
		{32, 32, 4},
		{33, 32, 8},
		{32, 33, 8},
		{33, 33, 16},
	} {
		t.Run(fmt.Sprintf("%dx%d", test.width, test.height), func(t *testing.T) {
			reference := imageDataURL(t, "image/png", encodePNG(t, test.width, test.height))
			detail := "original"
			got := (llm.OpenAITokenEstimator{}).EstimateImageBytes(llm.ImageEstimateInput{
				Reference: reference,
				Detail:    &detail,
			})
			if got != test.wantBytes {
				t.Fatalf("original %dx%d image estimate = %d bytes, want %d", test.width, test.height, got, test.wantBytes)
			}
		})
	}
}

func TestOpenAITokenEstimatorOriginalInlineJPEGUsesThirtyTwoPixelPatches(t *testing.T) {
	reference := imageDataURL(t, "image/jpeg", encodeJPEG(t, 33, 32))
	detail := "original"
	got := (llm.OpenAITokenEstimator{}).EstimateImageBytes(llm.ImageEstimateInput{
		Reference: reference,
		Detail:    &detail,
	})
	if got != 8 {
		t.Fatalf("original 33x32 JPEG estimate = %d bytes, want 8", got)
	}
}

func TestOpenAITokenEstimatorOriginalInlineGIFUsesThirtyTwoPixelPatches(t *testing.T) {
	reference := imageDataURL(t, "image/gif", encodeGIF(t, 33, 32))
	detail := "original"
	got := (llm.OpenAITokenEstimator{}).EstimateImageBytes(llm.ImageEstimateInput{
		Reference: reference,
		Detail:    &detail,
	})
	if got != 8 {
		t.Fatalf("original 33x32 GIF estimate = %d bytes, want 8", got)
	}
}

func TestOpenAITokenEstimatorOriginalInlineWebPUsesThirtyTwoPixelPatches(t *testing.T) {
	reference := imageDataURL(t, "image/webp", encodeWebP(t, 33, 32))
	detail := "original"
	got := (llm.OpenAITokenEstimator{}).EstimateImageBytes(llm.ImageEstimateInput{
		Reference: reference,
		Detail:    &detail,
	})
	if got != 8 {
		t.Fatalf("original 33x32 WebP estimate = %d bytes, want 8", got)
	}
}

func TestOpenAITokenEstimatorOriginalInlineImageCapsAtTenThousandTokens(t *testing.T) {
	reference := imageDataURL(t, "image/gif", encodeGIF(t, 3201, 3201))
	detail := "original"
	got := (llm.OpenAITokenEstimator{}).EstimateImageBytes(llm.ImageEstimateInput{
		Reference: reference,
		Detail:    &detail,
	})
	if got != 40_000 {
		t.Fatalf("original 3201x3201 image estimate = %d bytes, want 40000", got)
	}
}

func TestOpenAITokenEstimatorUsesOrdinaryEstimateWhenOriginalDimensionsAreUnavailable(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	for _, test := range []struct {
		name      string
		reference string
	}{
		{"malformed base64", "data:image/png;base64,%"},
		{"undecodable inline image", "data:image/png;base64,AAAA"},
		{"unsupported inline media type", "data:application/octet-stream;base64,AAAA"},
		{"non-base64 inline image", "data:image/png,AAAA"},
		{"remote image URL", server.URL + "/image.png"},
	} {
		t.Run(test.name, func(t *testing.T) {
			detail := "original"
			got := (llm.OpenAITokenEstimator{}).EstimateImageBytes(llm.ImageEstimateInput{
				Reference: test.reference,
				Detail:    &detail,
			})
			if got != 7373 {
				t.Fatalf("unreadable original image estimate = %d bytes, want ordinary estimate 7373", got)
			}
		})
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("original image estimation made %d remote requests, want none", got)
	}
}

func TestOpenAITokenEstimatorUsesOrdinaryEstimateForNonOriginalDetail(t *testing.T) {
	reference := imageDataURL(t, "image/png", encodePNG(t, 33, 32))
	autoDetail, highDetail := "auto", "high"
	for _, detail := range []*string{nil, &autoDetail, &highDetail} {
		got := (llm.OpenAITokenEstimator{}).EstimateImageBytes(llm.ImageEstimateInput{
			Reference: reference,
			Detail:    detail,
		})
		if got != 7373 {
			t.Fatalf("non-original image detail %v estimate = %d bytes, want ordinary estimate 7373", detail, got)
		}
	}
}

func TestConnectionEstimatorAppliesOriginalImagePatchEstimateOnlyToFirstPartyOpenAI(t *testing.T) {
	reference := imageDataURL(t, "image/png", encodePNG(t, 33, 32))
	detail := "original"
	openAIEndpoint := config.DefaultOpenAIResponsesEndpoint
	compatibleEndpoint := "https://example.test/v1"
	grokEnvironmentVariable := "GROK_API_KEY"
	for _, test := range []struct {
		name       string
		connection config.ProviderConnection
		wantBytes  int
	}{
		{
			name: "OpenAI API",
			connection: config.ProviderConnection{
				Protocol: config.ConnectionResponses,
				Endpoint: &openAIEndpoint,
			},
			wantBytes: 8,
		},
		{
			name:       "ChatGPT",
			connection: config.ProviderConnection{Protocol: config.ConnectionChatGPT},
			wantBytes:  8,
		},
		{
			name: "OpenAI-compatible endpoint with first-party override",
			connection: config.ProviderConnection{
				Protocol: config.ConnectionResponses,
				Endpoint: &compatibleEndpoint,
				Capabilities: config.ProviderCapabilitiesOverride{
					ProviderID: "openai", SupportsResponsesAPI: true, IsOpenAIFirstParty: true,
				},
			},
			wantBytes: 7373,
		},
		{
			name: "Grok",
			connection: config.ProviderConnection{
				Protocol: config.ConnectionGrokAPIKey, EnvironmentVariable: &grokEnvironmentVariable,
			},
			wantBytes: 3060,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			estimator, err := llm.ResolveConnectionTokenEstimator(test.connection)
			if err != nil {
				t.Fatal(err)
			}
			got := estimator.EstimateImageBytes(llm.ImageEstimateInput{Reference: reference, Detail: &detail})
			if got != test.wantBytes {
				t.Fatalf("resolved image estimate = %d bytes, want %d", got, test.wantBytes)
			}
		})
	}
}

func TestOpenAITokenEstimatorLeavesPDFFileEstimateUnchanged(t *testing.T) {
	item := llm.ResponseItem{
		Type: llm.ResponseItemTypeFunctionCallOutput,
		Output: json.RawMessage(
			`[{"type":"input_file","file_data":"data:application/pdf;base64,AAAA","filename":"abc"}]`,
		),
	}
	if got := (llm.OpenAITokenEstimator{}).EstimateItem(item); got != 513 {
		t.Fatalf("PDF file estimate = %d tokens, want 513", got)
	}
}

func TestOpenAITokenEstimatorAccountsOriginalImagesInMessagesAndToolOutputs(t *testing.T) {
	imageURL := imageDataURL(t, "image/png", encodePNG(t, 33, 32))
	content := json.RawMessage(fmt.Sprintf(
		`[{"type":"input_text","text":"abc"},{"type":"input_image","image_url":%q,"detail":"original"}]`,
		imageURL,
	))
	message := json.RawMessage(fmt.Sprintf(`{"type":"message","role":"user","content":%s}`, content))
	items := []llm.ResponseItem{
		{Type: llm.ResponseItemTypeMessage, Raw: message},
		{Type: llm.ResponseItemTypeFunctionCallOutput, Output: content},
	}

	estimator := llm.OpenAITokenEstimator{}
	for _, item := range items {
		if got := estimator.EstimateItem(item); got != 3 {
			t.Fatalf("%s text and original image estimate = %d tokens, want 3", item.Type, got)
		}
	}
}

func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return data.Bytes()
}

func encodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, width, height)), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return data.Bytes()
}

func encodeGIF(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, width, height), color.Palette{color.White, color.Black})
	var data bytes.Buffer
	if err := gif.Encode(&data, img, nil); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}
	return data.Bytes()
}

func encodeWebP(t *testing.T, width, height int) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := webpencoder.Encode(&data, image.NewRGBA(image.Rect(0, 0, width, height)), webpencoder.OptionsForPreset(webpencoder.PresetPicture, 80)); err != nil {
		t.Fatalf("encode WebP: %v", err)
	}
	return data.Bytes()
}

func imageDataURL(t *testing.T, mediaType string, data []byte) string {
	t.Helper()
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}
