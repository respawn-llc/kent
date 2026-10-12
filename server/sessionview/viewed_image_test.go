package sessionview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"core/shared/imagefileio"
)

func TestOpenViewedImageOriginalResolvesRelativePath(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	original := []byte("current original")
	if err := os.WriteFile(filepath.Join(workdir, "original.png"), original, 0600); err != nil {
		t.Fatal(err)
	}

	opened, err := service.OpenViewedImageOriginal(t.Context(), store.Meta().SessionID, "original.png")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.File.Close()
	if opened.Info.Name() != "original.png" || opened.Info.Size() != int64(len(original)) {
		t.Fatalf("original metadata = %q, %d bytes", opened.Info.Name(), opened.Info.Size())
	}
	content, err := io.ReadAll(opened.File)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("original = %v, want %v", content, original)
	}
}

func TestOpenViewedImageOriginalRejectsUnavailableAndNonRegularSources(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})

	tests := []struct {
		name string
		path string
	}{
		{name: "missing", path: "missing.png"},
		{name: "directory", path: "."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened, err := service.OpenViewedImageOriginal(t.Context(), store.Meta().SessionID, test.path)
			if opened != nil {
				_ = opened.File.Close()
				t.Fatal("opened an unusable image source")
			}
			var acquisitionError *imagefileio.AcquisitionError
			if !errors.As(err, &acquisitionError) {
				t.Fatalf("source error = %v, want acquisition error", err)
			}
		})
	}
}

func TestOpenViewedImageOriginalOpensSourceAboveBufferedReadLimit(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	file, err := os.Create(filepath.Join(workdir, "large.png"))
	if err != nil {
		t.Fatal(err)
	}
	const size = imagefileio.MaxReadBytes + 1
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := service.OpenViewedImageOriginal(t.Context(), store.Meta().SessionID, "large.png")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.File.Close()
	if opened.Info.Size() != size {
		t.Fatalf("opened size = %d, want %d", opened.Info.Size(), size)
	}
}

func TestOpenViewedImageOriginalFollowsReadableSymlink(t *testing.T) {
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
	opened, err := service.OpenViewedImageOriginal(t.Context(), store.Meta().SessionID, "link.png")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.File.Close()
	content, err := io.ReadAll(opened.File)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("original = %v, want %v", content, original)
	}
}

func TestOpenViewedImageOriginalRejectsBrokenSymlink(t *testing.T) {
	workdir := t.TempDir()
	store := newSessionViewStore(t, t.TempDir(), "workspace", workdir)
	service := NewService(newTestSessionResolver(store), nil, staticExecutionTargetResolver{
		target: availableSessionExecutionTarget(workdir),
	})
	if err := os.Symlink(filepath.Join(workdir, "gone.png"), filepath.Join(workdir, "link.png")); err != nil {
		t.Fatal(err)
	}
	opened, err := service.OpenViewedImageOriginal(t.Context(), store.Meta().SessionID, "link.png")
	if opened != nil {
		_ = opened.File.Close()
		t.Fatal("opened a broken symlink")
	}
	var acquisitionError *imagefileio.AcquisitionError
	if !errors.As(err, &acquisitionError) {
		t.Fatalf("source error = %v, want acquisition error", err)
	}
}

func TestViewedImageOperationalFailureIsNotUnavailable(t *testing.T) {
	want := errors.New("Session metadata unavailable")
	service := NewService(failingPersistedSessionResolver{err: want}, nil, nil)
	opened, err := service.OpenViewedImageOriginal(t.Context(), "session", "/image.png")
	if !errors.Is(err, want) || opened != nil {
		t.Fatalf("opened = %v, error = %v", opened, err)
	}
}

func TestOpenViewedImageOriginalRejectsCanceledSourceResolution(t *testing.T) {
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
	opened, err := service.OpenViewedImageOriginal(ctx, store.Meta().SessionID, path)
	if !errors.Is(err, context.Canceled) || opened != nil {
		t.Fatalf("opened = %v, error = %v", opened, err)
	}
}
