package sessionview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"core/server/sessionstore"
	"core/shared/imagefileio"
)

// OpenedViewedImageOriginal owns an opened regular image file and its metadata.
// The caller owns File and must close it.
type OpenedViewedImageOriginal struct {
	File *os.File
	Info os.FileInfo
}

// ViewedImageOriginalOpener opens the current Session original for streamed delivery.
type ViewedImageOriginalOpener interface {
	OpenViewedImageOriginal(context.Context, string, string) (*OpenedViewedImageOriginal, error)
}

func (s *Service) OpenViewedImageOriginal(
	ctx context.Context,
	sessionID string,
	requestedPath string,
) (*OpenedViewedImageOriginal, error) {
	path, err := s.resolveViewedImageOriginalPath(ctx, sessionID, requestedPath)
	if err != nil {
		return nil, err
	}
	file, info, err := imagefileio.OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	return &OpenedViewedImageOriginal{File: file, Info: info}, nil
}

func (s *Service) resolveViewedImageOriginalPath(
	ctx context.Context,
	sessionID string,
	requestedPath string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", errors.New("Session id is required")
	}
	if strings.TrimSpace(requestedPath) == "" {
		return "", errors.New("image path is required")
	}
	if _, err := session.ResolvePersistedSessionRecord(ctx, s.persisted, sessionID); err != nil {
		return "", err
	}
	if s.targets == nil {
		return "", errors.New("Session execution-target resolver is required")
	}
	target, err := s.targets.ResolveSessionExecutionTarget(ctx, sessionID)
	if err != nil {
		return "", err
	}
	path := requestedPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(target.EffectiveWorkdir, path)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", &imagefileio.AcquisitionError{Cause: err}
	}
	return path, nil
}
