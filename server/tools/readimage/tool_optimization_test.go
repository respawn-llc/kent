package readimage

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"core/server/tools"
)

func TestCall_OptimizesLargeJPEGToSmallerJPEGOutput(t *testing.T) {
	workspace := t.TempDir()
	var original bytes.Buffer
	if err := jpeg.Encode(&original, generatedPhotoLikeImage(1024), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	if int64(original.Len()) < minOptimizationSizeBytes {
		t.Fatalf("test image is too small for optimization path: %d", original.Len())
	}
	if int64(original.Len()) <= maxFileSizeBytes {
		t.Fatalf("test image must exceed attachment cap before optimization: %d", original.Len())
	}
	writeReadImageTestFile(t, workspace, "photo.jpg", original.Bytes())

	tool := newReadImageTestTool(t, workspace, true)
	result := callReadImageTool(t, tool, "call-optimized", `{"path":"photo.jpg"}`)
	if result.IsError {
		t.Fatalf("expected success result, got error payload: %s", string(result.Output))
	}

	mimeType, payload := decodeSingleImageDataURL(t, result)
	if mimeType != "image/jpeg" {
		t.Fatalf("expected optimized jpeg output, got %q", mimeType)
	}
	if len(payload) >= original.Len() {
		t.Fatalf("expected optimized output smaller than original, got optimized=%d original=%d", len(payload), original.Len())
	}
	if int64(len(payload)) > maxFileSizeBytes {
		t.Fatalf("expected optimized output under attachment cap, got %d", len(payload))
	}
}

func TestCall_OptimizesTransparentPNGToJPEGOutput(t *testing.T) {
	workspace := t.TempDir()
	var original bytes.Buffer
	if err := png.Encode(&original, generatedTransparentHighEntropyImage(384)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if int64(original.Len()) < minOptimizationSizeBytes {
		t.Fatalf("test image is too small for optimization path: %d", original.Len())
	}
	writeReadImageTestFile(t, workspace, "screenshot.png", original.Bytes())

	tool := newReadImageTestTool(t, workspace, true)
	result := callReadImageTool(t, tool, "call-transparent-png", `{"path":"screenshot.png"}`)
	if result.IsError {
		t.Fatalf("expected success result, got error payload: %s", string(result.Output))
	}

	mimeType, payload := decodeSingleImageDataURL(t, result)
	if mimeType != "image/jpeg" {
		t.Fatalf("expected jpeg output, got %q", mimeType)
	}
	if int64(len(payload)) > maxFileSizeBytes {
		t.Fatalf("expected optimized output under attachment cap, got %d", len(payload))
	}
	decoded, format, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode optimized jpeg: %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("expected jpeg decode format, got %q", format)
	}
	r, g, b := averageRGB16(decoded, image.Rect(0, 0, 8, 8))
	if r < 0xd000 || g < 0xd000 || b < 0xd000 {
		t.Fatalf("expected transparent pixels to flatten against white, got rgba16=(%d,%d,%d)", r, g, b)
	}
}

func TestCall_RawImageSkipsOptimization(t *testing.T) {
	workspace := t.TempDir()
	var original bytes.Buffer
	if err := jpeg.Encode(&original, generatedPhotoLikeImage(512), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	writeReadImageTestFile(t, workspace, "photo.jpg", original.Bytes())

	tool := newReadImageTestTool(t, workspace, true)
	result := callReadImageTool(t, tool, "call-raw", `{"path":"photo.jpg","raw":true}`)
	if result.IsError {
		t.Fatalf("expected success result, got error payload: %s", string(result.Output))
	}

	mimeType, payload := decodeSingleImageDataURL(t, result)
	if mimeType != "image/jpeg" {
		t.Fatalf("expected raw jpeg output, got %q", mimeType)
	}
	if !bytes.Equal(payload, original.Bytes()) {
		t.Fatalf("expected raw image bytes to be preserved")
	}
}

func generatedPhotoLikeImage(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*13 + y*7) % 256),
				G: uint8((x*5 + y*11) % 256),
				B: uint8((x*3 + y*17) % 256),
				A: 255,
			})
		}
	}
	return img
}

func generatedTransparentHighEntropyImage(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*97 + y*53 + x*y*11) % 256),
				G: uint8((x*29 + y*131 + x*y*7) % 256),
				B: uint8((x*173 + y*19 + x*y*3) % 256),
				A: 255,
			})
		}
	}
	for y := 0; y < 16 && y < size; y++ {
		for x := 0; x < 16 && x < size; x++ {
			img.SetRGBA(x, y, color.RGBA{A: 0})
		}
	}
	return img
}

func averageRGB16(img image.Image, bounds image.Rectangle) (uint32, uint32, uint32) {
	clipped := bounds.Intersect(img.Bounds())
	if clipped.Empty() {
		return 0, 0, 0
	}
	var rTotal uint64
	var gTotal uint64
	var bTotal uint64
	count := uint64(clipped.Dx() * clipped.Dy())
	for y := clipped.Min.Y; y < clipped.Max.Y; y++ {
		for x := clipped.Min.X; x < clipped.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			rTotal += uint64(r)
			gTotal += uint64(g)
			bTotal += uint64(b)
		}
	}
	return uint32(rTotal / count), uint32(gTotal / count), uint32(bTotal / count)
}

func decodeSingleImageDataURL(t *testing.T, result tools.Result) (string, []byte) {
	t.Helper()
	var items []map[string]any
	if err := json.Unmarshal(result.Output, &items); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one content item, got %d", len(items))
	}
	if got := items[0]["type"]; got != "input_image" {
		t.Fatalf("expected input_image type, got %#v", got)
	}
	url, ok := items[0]["image_url"].(string)
	if !ok {
		t.Fatalf("expected image_url string, got %#v", items[0]["image_url"])
	}
	if !strings.HasPrefix(url, "data:") {
		t.Fatalf("expected data URL, got %q", url)
	}
	parts := strings.SplitN(strings.TrimPrefix(url, "data:"), ";base64,", 2)
	if len(parts) != 2 {
		t.Fatalf("expected base64 data URL, got %q", url)
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode base64 image: %v", err)
	}
	return parts[0], decoded
}
