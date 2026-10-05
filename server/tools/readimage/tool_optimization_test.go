package readimage

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"core/server/tools"

	webpencoder "github.com/deepteams/webp"
	webpdecoder "golang.org/x/image/webp"
)

func readImageTestResult(t *testing.T, name string, content []byte, callID string, input string) tools.Result {
	t.Helper()
	workspace := t.TempDir()
	writeReadImageTestFile(t, workspace, name, content)
	return callReadImageTool(t, newReadImageTestTool(t, workspace, true), callID, input)
}

func requireReadImageTestError(t *testing.T, name string, content []byte, callID string, input string) {
	t.Helper()
	result := readImageTestResult(t, name, content, callID, input)
	if !result.IsError {
		t.Fatalf("expected tool error for %q", name)
	}
}

func TestCall_OptimizesLargeJPEGToSmallerWebPOutput(t *testing.T) {
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

	result := readImageTestResult(t, "photo.jpg", original.Bytes(), "call-optimized", `{"path":"photo.jpg"}`)
	if result.IsError {
		t.Fatalf("expected success result, got error payload: %s", string(result.Output))
	}

	mimeType, payload := decodeSingleImageDataURL(t, result)
	if mimeType != "image/webp" {
		t.Fatalf("expected optimized WebP output, got %q", mimeType)
	}
	if len(payload) >= original.Len() {
		t.Fatalf("expected optimized output smaller than original, got optimized=%d original=%d", len(payload), original.Len())
	}
	if int64(len(payload)) > maxFileSizeBytes {
		t.Fatalf("expected optimized output under attachment cap, got %d", len(payload))
	}
}

func TestCall_OptimizesTransparentPNGToValidWebPOutput(t *testing.T) {
	var original bytes.Buffer
	if err := png.Encode(&original, generatedTransparentHighEntropyImage(384)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	reported, err := os.ReadFile("testdata/issue-308.png")
	if err != nil {
		t.Fatalf("read original regression image: %v", err)
	}
	for name, input := range map[string][]byte{"generated": original.Bytes(), "issue-308": reported} {
		t.Run(name, func(t *testing.T) {
			if int64(len(input)) < minOptimizationSizeBytes {
				t.Fatalf("test image is too small for optimization: %d", len(input))
			}
			result := readImageTestResult(t, "screenshot.png", input, "call-transparent-png", `{"path":"screenshot.png"}`)
			if result.IsError {
				t.Fatalf("expected success, got %s", result.Output)
			}
			mimeType, payload := decodeSingleImageDataURL(t, result)
			if mimeType != "image/webp" || len(payload) >= len(input) || int64(len(payload)) > maxFileSizeBytes {
				t.Fatalf("optimized output = %q, %d bytes; original = %d bytes", mimeType, len(payload), len(input))
			}
			decoded, err := webpdecoder.Decode(bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("independently decode optimized WebP: %v", err)
			}
			source, err := png.Decode(bytes.NewReader(input))
			if err != nil {
				t.Fatalf("decode source PNG: %v", err)
			}
			if decoded.Bounds() != source.Bounds() {
				t.Fatalf("optimized bounds = %v, want %v", decoded.Bounds(), source.Bounds())
			}
			for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
				for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
					_, _, _, want := source.At(x, y).RGBA()
					_, _, _, got := decoded.At(x, y).RGBA()
					if got != want {
						t.Fatalf("alpha at (%d, %d) = %d, want %d", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestCall_RawImageSkipsOptimization(t *testing.T) {
	var original bytes.Buffer
	if err := jpeg.Encode(&original, generatedPhotoLikeImage(512), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}

	result := readImageTestResult(t, "photo.jpg", original.Bytes(), "call-raw", `{"path":"photo.jpg","raw":true}`)
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

func TestCall_RawImageStillEnforcesAttachmentCap(t *testing.T) {
	var original bytes.Buffer
	if err := jpeg.Encode(&original, generatedPhotoLikeImage(1024), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	if int64(original.Len()) <= maxFileSizeBytes {
		t.Fatalf("test image must exceed attachment cap: %d", original.Len())
	}

	requireReadImageTestError(t, "large.jpg", original.Bytes(), "call-raw-large", `{"path":"large.jpg","raw":true}`)
}

func TestCall_StillGIFAcceptedAndAnimatedGIFRejected(t *testing.T) {
	workspace := t.TempDir()
	writeReadImageTestFile(t, workspace, "still.gif", encodedGIF(t, 1))
	writeReadImageTestFile(t, workspace, "animated.gif", encodedGIF(t, 2))

	tool := newReadImageTestTool(t, workspace, true)
	still := callReadImageTool(t, tool, "call-still-gif", `{"path":"still.gif"}`)
	if still.IsError {
		t.Fatalf("expected still gif success, got %s", string(still.Output))
	}
	mimeType, _ := decodeSingleImageDataURL(t, still)
	if mimeType != "image/gif" {
		t.Fatalf("expected still gif output, got %q", mimeType)
	}

	animated := callReadImageTool(t, tool, "call-animated-gif", `{"path":"animated.gif"}`)
	if !animated.IsError {
		t.Fatalf("expected animated gif to be rejected")
	}
}

func TestCall_AcceptsStillWebPWithoutChangingSmallOrRawInput(t *testing.T) {
	var original bytes.Buffer
	if err := webpencoder.Encode(&original, generatedTransparentHighEntropyImage(16), webpencoder.OptionsForPreset(webpencoder.PresetPicture, 85)); err != nil {
		t.Fatalf("encode WebP: %v", err)
	}
	for _, input := range []string{`{"path":"image.webp"}`, `{"path":"image.webp","raw":true}`} {
		result := readImageTestResult(t, "image.webp", original.Bytes(), "call-webp", input)
		if result.IsError {
			t.Fatalf("expected valid WebP to be accepted: %s", result.Output)
		}
		mimeType, payload := decodeSingleImageDataURL(t, result)
		if mimeType != "image/webp" || !bytes.Equal(payload, original.Bytes()) {
			t.Fatalf("small or raw WebP was changed: MIME = %q", mimeType)
		}
	}
}

func TestCall_RejectsInvalidWebPAlphaEvenWhenRaw(t *testing.T) {
	invalid, err := os.ReadFile("testdata/issue-308-invalid.webp")
	if err != nil {
		t.Fatalf("read original invalid WebP: %v", err)
	}
	for _, input := range []string{`{"path":"image.webp"}`, `{"path":"image.webp","raw":true}`} {
		requireReadImageTestError(t, "image.webp", invalid, "call-invalid-alpha", input)
	}
}

func TestCall_RejectsTruncatedWebP(t *testing.T) {
	requireReadImageTestError(t, "image.webp", minimalWebPHeader(), "call-webp", `{"path":"image.webp"}`)
}

func TestCall_CorruptImageReturnsToolError(t *testing.T) {
	requireReadImageTestError(t, "corrupt.png", make([]byte, 1024), "call-corrupt", `{"path":"corrupt.png"}`)
}

func TestCall_HugeDecodedDimensionsRejected(t *testing.T) {
	requireReadImageTestError(t, "huge-dimensions.png", pngWithDimensions(t, 100_000, 100_000), "call-huge-dimensions", `{"path":"huge-dimensions.png"}`)
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

func encodedGIF(t *testing.T, frames int) []byte {
	t.Helper()
	palette := []color.Color{color.Black, color.White}
	images := make([]*image.Paletted, 0, frames)
	delays := make([]int, 0, frames)
	for idx := 0; idx < frames; idx++ {
		img := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
		img.SetColorIndex(idx%2, idx%2, 1)
		images = append(images, img)
		delays = append(delays, 0)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: images, Delay: delays}); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func minimalWebPHeader() []byte {
	return []byte{
		'R', 'I', 'F', 'F',
		12, 0, 0, 0,
		'W', 'E', 'B', 'P',
		'V', 'P', '8', ' ',
		0, 0, 0, 0,
	}
}

func pngWithDimensions(t *testing.T, width, height uint32) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 2
	writePNGChunk(&buf, "IHDR", ihdr)
	writePNGChunk(&buf, "IEND", nil)
	return buf.Bytes()
}

func writePNGChunk(buf *bytes.Buffer, chunkType string, data []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	buf.Write(length[:])
	buf.WriteString(chunkType)
	buf.Write(data)
	checksum := crc32.NewIEEE()
	_, _ = checksum.Write([]byte(chunkType))
	_, _ = checksum.Write(data)
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], checksum.Sum32())
	buf.Write(crc[:])
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
