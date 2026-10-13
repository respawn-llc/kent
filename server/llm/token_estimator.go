package llm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log/slog"
	"mime"
	"net/url"
	"strings"

	"core/server/llm/openaiwire"
	"core/shared/textutil"
	webp "golang.org/x/image/webp"
)

// Top-level tools have this implicit namespace in Codex's content accounting.
const defaultFunctionNamespace = "functions"
const maxOriginalImageTokens = 10_000

// TokenEstimator performs local accounting without provider requests.
type TokenEstimator interface {
	EstimateText(string) int
	EstimateItem(ResponseItem) int
	EstimateImageBytes(ImageEstimateInput) int
}

type ImageEstimateInput struct {
	Reference string
	Detail    *string
}

type DefaultTokenEstimator struct{}

type OpenAITokenEstimator struct {
	DefaultTokenEstimator
}

func (estimator OpenAITokenEstimator) EstimateItem(item ResponseItem) int {
	if (item.Type == ResponseItemTypeReasoning || item.Type == ResponseItemTypeCompaction) && item.EncryptedContent != nil {
		visibleBytes := len(*item.EncryptedContent)*3/4 - 650
		return textutil.ApproxTokenCount(visibleBytes)
	}
	return textutil.ApproxTokenCount(estimateDefaultItemBytes(estimator, item))
}

func (estimator OpenAITokenEstimator) EstimateImageBytes(input ImageEstimateInput) int {
	if input.Detail == nil || *input.Detail != "original" {
		return estimator.DefaultTokenEstimator.EstimateImageBytes(input)
	}
	mediaType, data, inline, err := decodeImageDataURL(input.Reference)
	if err != nil {
		slog.Warn("Unable to read original image dimensions; using ordinary image estimate", "error", err)
		return estimator.DefaultTokenEstimator.EstimateImageBytes(input)
	}
	if !inline {
		return estimator.DefaultTokenEstimator.EstimateImageBytes(input)
	}
	var config image.Config
	switch mediaType {
	case "image/png":
		config, err = png.DecodeConfig(bytes.NewReader(data))
	case "image/jpeg":
		config, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "image/gif":
		config, err = gif.DecodeConfig(bytes.NewReader(data))
	case "image/webp":
		config, err = webp.DecodeConfig(bytes.NewReader(data))
	default:
		slog.Warn("Unable to read original image dimensions; using ordinary image estimate", "error", fmt.Errorf("unsupported inline image media type %q", mediaType))
		return estimator.DefaultTokenEstimator.EstimateImageBytes(input)
	}
	if err != nil {
		slog.Warn("Unable to read original image dimensions; using ordinary image estimate", "error", err)
		return estimator.DefaultTokenEstimator.EstimateImageBytes(input)
	}
	if config.Width <= 0 || config.Height <= 0 {
		slog.Warn("Unable to read original image dimensions; using ordinary image estimate", "width", config.Width, "height", config.Height)
		return estimator.DefaultTokenEstimator.EstimateImageBytes(input)
	}
	patchesWide := (config.Width-1)/32 + 1
	patchesHigh := (config.Height-1)/32 + 1
	if patchesWide > maxOriginalImageTokens/patchesHigh {
		return maxOriginalImageTokens * 4
	}
	return patchesWide * patchesHigh * 4
}

func (DefaultTokenEstimator) EstimateText(text string) int {
	return textutil.ApproxTextTokenCount(text)
}

func (DefaultTokenEstimator) EstimateImageBytes(ImageEstimateInput) int {
	return 7373
}

func (estimator DefaultTokenEstimator) EstimateItem(item ResponseItem) int {
	return textutil.ApproxTokenCount(estimateDefaultItemBytes(estimator, item))
}

func estimateDefaultItemBytes(estimator TokenEstimator, item ResponseItem) int {
	size := EstimateItemContentBytes(estimator, item)
	switch item.Type {
	case ResponseItemTypeFunctionCall, ResponseItemTypeCustomToolCall:
		size += stringBytes(item.Name) + len(defaultFunctionNamespace)
	case ResponseItemTypeFunctionCallOutput, ResponseItemTypeCustomToolOutput:
		size += stringBytes(item.CallID) + stringBytes(item.Name)
	}
	return size
}

// EstimateItemContentBytes shares content traversal with providers that override
// image, reasoning, or rounding accounting. Accounting follows OpenAI Codex history.rs at
// 0b47040dc9eb08d9c37d51e0824870ce5b27101b (Copyright 2025 OpenAI, Apache-2.0).
func EstimateItemContentBytes(estimator TokenEstimator, item ResponseItem) int {
	size := 0
	switch item.Type {
	case ResponseItemTypeMessage:
		size = stringBytes(item.Content)
		var message struct {
			Content json.RawMessage `json:"content"`
		}
		if len(item.Raw) > 0 && json.Unmarshal(item.Raw, &message) == nil {
			if content, structured := openaiwire.DecodeInputContentItems(message.Content); structured {
				size = estimateInputContentBytes(estimator, content)
			}
		}
	case ResponseItemTypeFunctionCall, ResponseItemTypeCustomToolCall:
		if item.Type == ResponseItemTypeFunctionCall {
			size += len(item.Arguments)
		} else {
			size += stringBytes(item.CustomInput)
		}
	case ResponseItemTypeReasoning, ResponseItemTypeCompaction:
		size = stringBytes(item.Content) + stringBytes(item.EncryptedContent)
		for _, summary := range item.ReasoningSummary {
			size += len(summary.Text)
		}
	case ResponseItemTypeFunctionCallOutput, ResponseItemTypeCustomToolOutput:
		content, structured := openaiwire.DecodeInputContentItems(item.Output)
		if item.Type == ResponseItemTypeFunctionCallOutput && structured {
			size = estimateInputContentBytes(estimator, content)
		} else {
			size += len(openaiwire.OutputText(bytes.TrimSpace(item.Output)))
		}
	}
	return size
}

func estimateInputContentBytes(estimator TokenEstimator, content []openaiwire.InputContent) int {
	size := 0
	for _, part := range content {
		switch part.Type {
		case "input_text":
			size += len(part.Text)
		case "input_image":
			var detail *string
			if part.Detail != "" {
				detail = &part.Detail
			}
			size += estimator.EstimateImageBytes(ImageEstimateInput{Reference: part.ImageURL, Detail: detail})
		case "input_file":
			size += 2048 + referenceBytes(part.FileData) + referenceBytes(part.FileID) +
				referenceBytes(part.FileURL) + len(part.Filename)
		}
	}
	return size
}

func referenceBytes(value string) int {
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		return 0
	}
	return len(value)
}

func decodeImageDataURL(reference string) (string, []byte, bool, error) {
	parsed, err := url.Parse(reference)
	if err != nil {
		return "", nil, false, err
	}
	if !strings.EqualFold(parsed.Scheme, "data") {
		return "", nil, false, nil
	}
	header, payload, ok := strings.Cut(parsed.Opaque, ",")
	if !ok {
		return "", nil, true, fmt.Errorf("inline image data URL has no payload separator")
	}
	metadata, base64Encoded := strings.CutSuffix(header, ";base64")
	if !base64Encoded {
		return "", nil, true, fmt.Errorf("inline image data URL is not base64 encoded")
	}
	mediaType, _, err := mime.ParseMediaType(metadata)
	if err != nil {
		return "", nil, true, fmt.Errorf("parse inline image media type: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, true, fmt.Errorf("decode inline image base64: %w", err)
	}
	return mediaType, data, true, nil
}

func stringBytes(value *string) int {
	if value == nil {
		return 0
	}
	return len(*value)
}

func EstimateItemsTokens(estimator TokenEstimator, items []ResponseItem) int {
	total := 0
	for _, item := range items {
		total += estimator.EstimateItem(item)
	}
	return total
}
