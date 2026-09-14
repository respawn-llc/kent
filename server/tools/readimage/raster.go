package readimage

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"path/filepath"
	"strings"

	webpencoder "github.com/deepteams/webp"
	webpdecoder "golang.org/x/image/webp"
)

func prepareFileForAttachment(path, mimeType string, data []byte, raw bool) ([]byte, string, error) {
	if mimeType == "application/pdf" || strings.EqualFold(filepath.Ext(path), ".pdf") {
		return data, "application/pdf", nil
	}

	if !strings.HasPrefix(mimeType, "image/") {
		return data, mimeType, nil
	}
	if _, ok := supportedImageMIMEs[mimeType]; !ok {
		return data, mimeType, fmt.Errorf("cannot attach image at %q: unsupported image format %q", path, mimeType)
	}
	img, decodedMIME, err := decodeSupportedRasterImage(path, mimeType, data)
	if err != nil {
		return data, mimeType, err
	}
	if raw || int64(len(data)) < minOptimizationSizeBytes {
		return data, decodedMIME, nil
	}

	optimized, optimizedMIME, ok := optimizeRasterImage(img)
	if !ok || len(optimized) >= len(data) {
		return data, decodedMIME, nil
	}
	return optimized, optimizedMIME, nil
}

func decodeSupportedRasterImage(path, mimeType string, data []byte) (image.Image, string, error) {
	var cfg image.Config
	var format string
	var err error
	if mimeType == "image/webp" {
		cfg, err = webpdecoder.DecodeConfig(bytes.NewReader(data))
		format = "webp"
	} else {
		cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return nil, "", fmt.Errorf("cannot attach image at %q: unable to decode image: %v", path, err)
	}
	mimeType, ok := mimeTypeForImageFormat(format)
	if !ok {
		return nil, "", fmt.Errorf("cannot attach image at %q: unsupported image format %q", path, format)
	}
	if _, ok := supportedImageMIMEs[mimeType]; !ok {
		return nil, "", fmt.Errorf("cannot attach image at %q: unsupported image format %q", path, mimeType)
	}
	if err := validateDecodedDimensions(path, cfg.Width, cfg.Height); err != nil {
		return nil, "", err
	}
	switch mimeType {
	case "image/webp":
		img, err := webpdecoder.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", fmt.Errorf("cannot attach WebP at %q: %w", path, err)
		}
		return img, mimeType, nil
	case "image/gif":
		img, err := decodeStillGIF(path, data)
		if err != nil {
			return nil, "", err
		}
		return img, mimeType, nil
	}
	img, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("cannot attach image at %q: unable to decode image: %v", path, err)
	}
	decodedMIME, ok := mimeTypeForImageFormat(decodedFormat)
	if !ok {
		return nil, "", fmt.Errorf("cannot attach image at %q: unsupported image format %q", path, decodedFormat)
	}
	return img, decodedMIME, nil
}

func decodeStillGIF(path string, data []byte) (image.Image, error) {
	frames, err := countGIFFrames(data, 2)
	if err != nil {
		return nil, fmt.Errorf("cannot attach GIF at %q: %v", path, err)
	}
	if frames != 1 {
		return nil, fmt.Errorf("cannot attach GIF at %q: animated GIFs are not supported; use a still image or PDF", path)
	}
	img, err := gif.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot attach GIF at %q: %v", path, err)
	}
	return img, nil
}

func validateDecodedDimensions(path string, width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("cannot attach image at %q: invalid image dimensions %dx%d", path, width, height)
	}
	pixels := int64(width) * int64(height)
	if pixels > maxDecodedPixels {
		return fmt.Errorf("cannot attach image at %q: decoded image dimensions %dx%d exceed the supported pixel limit of %d", path, width, height, maxDecodedPixels)
	}
	return nil
}

func mimeTypeForImageFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "png":
		return "image/png", true
	case "jpeg":
		return "image/jpeg", true
	case "gif":
		return "image/gif", true
	case "webp":
		return "image/webp", true
	default:
		return "", false
	}
}

func optimizeRasterImage(img image.Image) ([]byte, string, bool) {
	if img == nil {
		return nil, "", false
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return nil, "", false
	}
	for _, quality := range []float32{85, 75, 65, 55} {
		var out bytes.Buffer
		if err := webpencoder.Encode(&out, img, webpencoder.OptionsForPreset(webpencoder.PresetPicture, quality)); err != nil {
			slog.Warn("WebP optimization failed", "error", err)
			return nil, "", false
		}
		if int64(out.Len()) <= maxFileSizeBytes {
			// Do not let the encoder validate its own output. Check dimensions
			// before allocating pixels, then decode the entire alpha/colour payload.
			cfg, err := webpdecoder.DecodeConfig(bytes.NewReader(out.Bytes()))
			if err != nil || cfg.Width != bounds.Dx() || cfg.Height != bounds.Dy() {
				slog.Warn("WebP optimization produced invalid dimensions", "error", err, "width", cfg.Width, "height", cfg.Height)
				return nil, "", false
			}
			if _, err := webpdecoder.Decode(bytes.NewReader(out.Bytes())); err != nil {
				slog.Warn("WebP optimization produced invalid image data", "error", err)
				return nil, "", false
			}
			return out.Bytes(), "image/webp", true
		}
	}
	return nil, "", false
}
