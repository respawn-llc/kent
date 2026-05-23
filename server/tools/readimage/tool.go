package readimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"builder/server/tools"
	patchtool "builder/server/tools/patch"
	"builder/shared/toolspec"
	"github.com/deepteams/webp"
)

const maxFileSizeBytes int64 = 800 << 10
const maxOriginalRasterSizeBytes int64 = 10 << 20
const minOptimizationSizeBytes int64 = 100 << 10
const maxDecodedPixels int64 = 16_000_000

const outsideWorkspaceRejectionInstruction = "If it's essential to the task, ask the user to place the file inside the workspace root."

var supportedImageMIMEs = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/gif":  {},
	"image/webp": {},
}

type Tool struct {
	workspaceRoot             string
	workspaceRootReal         string
	workspaceRootInfo         os.FileInfo
	workspaceOnly             bool
	allowOutsideWorkspace     bool
	outsideWorkspaceApprover  patchtool.OutsideWorkspaceApprover
	outsideWorkspaceAudit     OutsideWorkspaceAuditLogger
	outsideWorkspaceSessionMu sync.RWMutex
	outsideWorkspaceAllowed   bool
	supported                 bool
}

type OutsideWorkspaceAudit struct {
	RequestedPath string
	ResolvedPath  string
	Reason        string
}

type OutsideWorkspaceAuditLogger func(OutsideWorkspaceAudit)

type Option func(*Tool)

func WithAllowOutsideWorkspace(allow bool) Option {
	return func(t *Tool) {
		t.allowOutsideWorkspace = allow
	}
}

func WithOutsideWorkspaceApprover(approver patchtool.OutsideWorkspaceApprover) Option {
	return func(t *Tool) {
		t.outsideWorkspaceApprover = approver
	}
}

func WithOutsideWorkspaceAuditLogger(logger OutsideWorkspaceAuditLogger) Option {
	return func(t *Tool) {
		t.outsideWorkspaceAudit = logger
	}
}

type input struct {
	Path string `json:"path"`
	Raw  bool   `json:"raw,omitempty"`
}

type contentItem struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url,omitempty"`
	FileData string `json:"file_data,omitempty"`
	Filename string `json:"filename,omitempty"`
}

func New(workspaceRoot string, supported bool, opts ...Option) (*Tool, error) {
	rootAbs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, tools.WrapMissingWorkspaceRootError(rootAbs, fmt.Errorf("resolve workspace real path: %w", err))
		}
		return nil, fmt.Errorf("resolve workspace real path: %w", err)
	}
	rootInfo, err := os.Stat(rootReal)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, tools.WrapMissingWorkspaceRootError(rootAbs, fmt.Errorf("stat workspace root: %w", err))
		}
		return nil, fmt.Errorf("stat workspace root: %w", err)
	}
	t := &Tool{workspaceRoot: rootAbs, workspaceRootReal: rootReal, workspaceRootInfo: rootInfo, workspaceOnly: true, supported: supported}
	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}
	return t, nil
}

func (t *Tool) Name() toolspec.ID {
	return toolspec.ToolViewImage
}

func (t *Tool) Call(ctx context.Context, c tools.Call) (tools.Result, error) {
	if !t.supported {
		return tools.ErrorResult(c, "view_image is not allowed because this model does not support image/file inputs"), nil
	}

	var in input
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return tools.ErrorResult(c, fmt.Sprintf("invalid input: %v", err)), nil
	}
	requestedPath := strings.TrimSpace(in.Path)
	if requestedPath == "" {
		return tools.ErrorResult(c, "path is required"), nil
	}

	approvedOutside := map[string]bool{}
	resolvedPath, err := t.resolvePath(ctx, requestedPath, approvedOutside)
	if err != nil {
		return tools.ErrorResult(c, err.Error()), nil
	}

	file, info, err := openResolvedRegularFile(resolvedPath)
	if err != nil {
		return tools.ErrorResult(c, err.Error()), nil
	}
	defer func() { _ = file.Close() }()
	if isPDFPath(resolvedPath) && info.Size() > maxFileSizeBytes {
		return tools.ErrorResult(c, maxAttachmentSizeError(resolvedPath, info.Size())), nil
	}
	if in.Raw && info.Size() > maxFileSizeBytes {
		return tools.ErrorResult(c, rawAttachmentSizeError(resolvedPath, info.Size())), nil
	}
	if info.Size() > maxOriginalRasterSizeBytes {
		return tools.ErrorResult(c, fmt.Sprintf("file %q is too large (%d bytes). max readable size is %d bytes (10 MiB). resize or compress the image or PDF and try again", resolvedPath, info.Size(), maxOriginalRasterSizeBytes)), nil
	}

	data, err := readLimitedFile(file, maxOriginalRasterSizeBytes)
	if err != nil {
		return tools.ErrorResult(c, fmt.Sprintf("unable to read file at %q: %v", resolvedPath, err)), nil
	}
	if isPDFPath(resolvedPath) && int64(len(data)) > maxFileSizeBytes {
		return tools.ErrorResult(c, maxAttachmentSizeError(resolvedPath, int64(len(data)))), nil
	}
	if in.Raw && int64(len(data)) > maxFileSizeBytes {
		return tools.ErrorResult(c, rawAttachmentSizeError(resolvedPath, int64(len(data)))), nil
	}
	mimeType := detectFileMIME(resolvedPath, data)
	contentData, contentMIME, prepareErr := prepareFileForAttachment(resolvedPath, mimeType, data, in.Raw)
	if prepareErr != nil {
		return tools.ErrorResult(c, prepareErr.Error()), nil
	}
	if int64(len(contentData)) > maxFileSizeBytes {
		return tools.ErrorResult(c, maxAttachmentSizeError(resolvedPath, int64(len(contentData)))), nil
	}

	items, buildErr := buildContentItemsForFile(resolvedPath, contentMIME, contentData)
	if buildErr != nil {
		return tools.ErrorResult(c, buildErr.Error()), nil
	}
	body, marshalErr := json.Marshal(items)
	if marshalErr != nil {
		return tools.Result{}, marshalErr
	}

	return tools.Result{CallID: c.ID, Name: c.Name, Output: body}, nil
}

func (t *Tool) resolvePath(ctx context.Context, path string, approvedOutside map[string]bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(t.workspaceRoot, candidate)
	}
	candidate = filepath.Clean(candidate)
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %q: %w", path, err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	real = filepath.Clean(real)

	guard := patchtool.NewOutsideWorkspaceGuard(
		t.workspaceRoot,
		t.workspaceRootReal,
		t.workspaceRootInfo,
		t.workspaceOnly,
		t.allowOutsideWorkspace,
		t.outsideWorkspaceApprover,
		func() bool {
			t.outsideWorkspaceSessionMu.RLock()
			defer t.outsideWorkspaceSessionMu.RUnlock()
			return t.outsideWorkspaceAllowed
		},
		func(allow bool) {
			t.outsideWorkspaceSessionMu.Lock()
			t.outsideWorkspaceAllowed = allow
			t.outsideWorkspaceSessionMu.Unlock()
		},
		outsideWorkspaceRejectionInstruction,
		patchtool.OutsideWorkspaceErrorLabels{
			OutsidePath:          "view_image path outside workspace",
			ApprovalFailed:       "outside-workspace read approval failed",
			RejectedByUserPrefix: "view_image path outside workspace rejected by user",
		},
		patchtool.OutsideWorkspaceFailureFactory{
			ApprovalFailed: readImageOutsideWorkspaceApprovalFailed,
			UserDenied:     readImageOutsideWorkspaceUserDenied,
		},
		patchtool.IsPathInTemporaryDir,
		func(req patchtool.OutsideWorkspaceRequest, reason string) {
			t.logOutsideWorkspaceApproval(req, reason)
		},
	)
	return guard.Allow(ctx, path, real, approvedOutside)
}

func (t *Tool) logOutsideWorkspaceApproval(req patchtool.OutsideWorkspaceRequest, reason string) {
	if t.outsideWorkspaceAudit == nil {
		return
	}
	t.outsideWorkspaceAudit(OutsideWorkspaceAudit{
		RequestedPath: req.RequestedPath,
		ResolvedPath:  req.ResolvedPath,
		Reason:        reason,
	})
}

func detectFileMIME(path string, data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sniffed := normalizeMIME(http.DetectContentType(data))
	if sniffed != "" && sniffed != "application/octet-stream" {
		return sniffed
	}
	extMIME := normalizeMIME(mime.TypeByExtension(strings.ToLower(filepath.Ext(path))))
	if extMIME != "" {
		return extMIME
	}
	return sniffed
}

func normalizeMIME(raw string) string {
	main := strings.TrimSpace(strings.Split(raw, ";")[0])
	return strings.ToLower(main)
}

func openResolvedRegularFile(path string) (*os.File, os.FileInfo, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("stat path at %q: %v", path, err)
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("path %q is not a regular file", path)
	}
	file, err := openReadOnlyNoFollow(path)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to locate file at %q: %v", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("stat file at %q: %v; close file: %w", path, err, closeErr)
		}
		return nil, nil, fmt.Errorf("stat file at %q: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		if closeErr := file.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("path %q is not a regular file; close file: %w", path, closeErr)
		}
		return nil, nil, fmt.Errorf("path %q is not a regular file", path)
	}
	if !os.SameFile(pathInfo, info) {
		if closeErr := file.Close(); closeErr != nil {
			return nil, nil, fmt.Errorf("path %q changed while opening; retry the tool call; close file: %w", path, closeErr)
		}
		return nil, nil, fmt.Errorf("path %q changed while opening; retry the tool call", path)
	}
	return file, info, nil
}

func readLimitedFile(file *os.File, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, errors.New("read limit must be non-negative")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds max readable size of %d bytes (10 MiB)", limit)
	}
	return data, nil
}

func readImageOutsideWorkspaceApprovalFailed(req patchtool.OutsideWorkspaceRequest, err error) error {
	path := readImageOutsideWorkspacePath(req)
	reason := strings.TrimSpace(err.Error())
	message := "outside-workspace read approval failed"
	if path != "" {
		message += " for " + path + "."
	} else {
		message += "."
	}
	if reason != "" {
		message += "\nReason: " + reason
	}
	return errors.New(message)
}

func readImageOutsideWorkspaceUserDenied(req patchtool.OutsideWorkspaceRequest, approval patchtool.OutsideWorkspaceApproval, rejectionInstruction string) error {
	path := readImageOutsideWorkspacePath(req)
	commentary := strings.TrimSpace(approval.Commentary)

	var builder strings.Builder
	builder.WriteString("view_image path outside workspace rejected by user")
	if path != "" {
		builder.WriteString(": ")
		builder.WriteString(path)
	}
	builder.WriteString(".")
	if commentary != "" {
		builder.WriteString(" User rejected the approval request for this tool call, and said: ")
		builder.WriteString(strconv.Quote(commentary))
		builder.WriteString(".")
	} else {
		builder.WriteString(" User rejected the approval request for this tool call.")
	}
	builder.WriteString(" Do not attempt to circumvent, hack around, or re-execute the same path. Treat this rejection as authoritative.")
	if instruction := strings.TrimSpace(rejectionInstruction); instruction != "" {
		builder.WriteString(" ")
		builder.WriteString(instruction)
	}
	return errors.New(builder.String())
}

func readImageOutsideWorkspacePath(req patchtool.OutsideWorkspaceRequest) string {
	if path := strings.TrimSpace(req.ResolvedPath); path != "" {
		return path
	}
	return strings.TrimSpace(req.RequestedPath)
}

func buildContentItemsForFile(path, mimeType string, data []byte) ([]contentItem, error) {
	if mimeType == "application/pdf" || isPDFPath(path) {
		filename := filepath.Base(path)
		if strings.TrimSpace(filename) == "" {
			filename = "document.pdf"
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		return []contentItem{{
			Type:     "input_file",
			FileData: "data:application/pdf;base64," + encoded,
			Filename: filename,
		}}, nil
	}

	if strings.HasPrefix(mimeType, "image/") {
		if _, ok := supportedImageMIMEs[mimeType]; !ok {
			return nil, fmt.Errorf("cannot attach image at %q: unsupported image format %q", path, mimeType)
		}
		return []contentItem{{
			Type:     "input_image",
			ImageURL: fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data)),
		}}, nil
	}

	return nil, fmt.Errorf("unsupported file type at %q: expected an image or PDF", path)
}

func prepareFileForAttachment(path, mimeType string, data []byte, raw bool) ([]byte, string, error) {
	if mimeType == "application/pdf" || isPDFPath(path) {
		return data, "application/pdf", nil
	}

	if !strings.HasPrefix(mimeType, "image/") {
		return data, mimeType, nil
	}
	img, decodedMIME, err := decodeSupportedRasterImage(path, data)
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

func decodeSupportedRasterImage(path string, data []byte) (image.Image, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
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
	case "image/gif":
		img, err := decodeStillGIF(path, data)
		if err != nil {
			return nil, "", err
		}
		return img, mimeType, nil
	case "image/webp":
		if err := validateStillWebP(path, data); err != nil {
			return nil, "", err
		}
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

func validateStillWebP(path string, data []byte) error {
	features, err := webp.GetFeatures(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("cannot attach WebP at %q: %v", path, err)
	}
	if features.HasAnimation {
		return fmt.Errorf("cannot attach WebP at %q: animated WebP images are not supported; use a still image or PDF", path)
	}
	return nil
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
	var out bytes.Buffer
	opts := webp.OptionsForPreset(webp.PresetPicture, 80)
	if err := webp.Encode(&out, img, opts); err != nil {
		return nil, "", false
	}
	return out.Bytes(), "image/webp", true
}

func maxAttachmentSizeError(path string, size int64) string {
	return fmt.Sprintf("file %q is too large (%d bytes). max supported size is %d bytes (800 KiB). compress the image or PDF and try again", path, size, maxFileSizeBytes)
}

func rawAttachmentSizeError(path string, size int64) string {
	return maxAttachmentSizeError(path, size) + ". raw=true bypasses compression and postprocessing, but the 800 KiB cap still applies; retry without raw=true to allow optimization"
}

func isPDFPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}
