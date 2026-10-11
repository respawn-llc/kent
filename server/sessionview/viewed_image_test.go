package sessionview

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/shared/imagefileio"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
)

func TestMain(m *testing.M) {
	imagefileio.ExitIfWorker(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	os.Exit(m.Run())
}

func TestViewedImageReadsCurrentOriginalWithoutRuntime(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	path := filepath.Join(workdir, "original.png")
	original := []byte{0x89, 'P', 'N', 'G'}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := service.GetViewedImageOriginal(t.Context(), &sessionpb.ViewedImageOriginalRequest{
		SessionId: store.Meta().SessionID,
		Path:      "original.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.GetContent(), original) {
		t.Fatalf("original = %v, want %v", result.GetContent(), original)
	}
}

func TestViewedImageMissingOriginalIsUnavailable(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	result, err := service.GetViewedImageOriginal(t.Context(), &sessionpb.ViewedImageOriginalRequest{
		SessionId: store.Meta().SessionID,
		Path:      "missing.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetUnavailable() == nil {
		t.Fatalf("source = %v, want unavailable", result)
	}
}

func TestViewedImageUnreadableOriginalIsUnavailable(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	path := filepath.Join(workdir, "unreadable.png")
	if err := os.WriteFile(path, []byte("image"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, 0600); err != nil {
			t.Error(err)
		}
	})
	result, err := service.GetViewedImageOriginal(t.Context(), &sessionpb.ViewedImageOriginalRequest{
		SessionId: store.Meta().SessionID,
		Path:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetUnavailable() == nil {
		t.Fatalf("source = %v, want unavailable", result)
	}
}

func TestViewedImageOversizedOriginalIsUnavailable(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	path := filepath.Join(workdir, "oversized.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(imagefileio.MaxReadBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := service.GetViewedImageOriginal(t.Context(), &sessionpb.ViewedImageOriginalRequest{
		SessionId: store.Meta().SessionID, Path: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetUnavailable() == nil {
		t.Fatalf("source = %v, want unavailable", result)
	}
}

func TestViewedImageReadableSymlinkOriginal(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	original := []byte("current original")
	target := filepath.Join(workdir, "target.png")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(workdir, "link.png")); err != nil {
		t.Fatal(err)
	}
	result, err := service.GetViewedImageOriginal(t.Context(), &sessionpb.ViewedImageOriginalRequest{
		SessionId: store.Meta().SessionID, Path: "link.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.GetContent(), original) {
		t.Fatalf("original = %v, want %v", result.GetContent(), original)
	}
}

func TestViewedImageBrokenSymlinkIsUnavailable(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	if err := os.Symlink(filepath.Join(workdir, "gone.png"), filepath.Join(workdir, "link.png")); err != nil {
		t.Fatal(err)
	}
	result, err := service.GetViewedImageOriginal(t.Context(), &sessionpb.ViewedImageOriginalRequest{
		SessionId: store.Meta().SessionID, Path: "link.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GetUnavailable() == nil {
		t.Fatalf("source = %v, want unavailable", result)
	}
}

func TestViewedImageOperationalFailureIsNotUnavailable(t *testing.T) {
	want := errors.New("Session metadata unavailable")
	service := NewService(failingPersistedSessionResolver{err: want}, nil, nil)
	result, err := service.GetViewedImageOriginal(t.Context(), &sessionpb.ViewedImageOriginalRequest{
		SessionId: "session", Path: "/image.png",
	})
	if !errors.Is(err, want) || result != nil {
		t.Fatalf("result = %v, error = %v", result, err)
	}
}

func TestViewedImageCanceledReaderIsOperationalFailure(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	path := filepath.Join(workdir, "original.png")
	if err := os.WriteFile(path, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := service.GetViewedImageOriginal(ctx, &sessionpb.ViewedImageOriginalRequest{
		SessionId: store.Meta().SessionID, Path: path,
	})
	if !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("result = %v, error = %v", result, err)
	}
}
