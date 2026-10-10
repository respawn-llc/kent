package sessionview

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"

	"core/server/session"
	"core/shared/imagefileio"
	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"

	"google.golang.org/protobuf/types/known/emptypb"
)

func (s *Service) GetViewedImageOriginal(ctx context.Context, req *sessionpb.ViewedImageOriginalRequest) (*sessionpb.ViewedImageOriginalSuccess, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if _, err := session.ResolvePersistedSessionRecord(ctx, s.persisted, req.SessionId); err != nil {
		return nil, err
	}
	if s.targets == nil {
		return nil, errors.New("Session execution-target resolver is required")
	}
	target, err := s.targets.ResolveSessionExecutionTarget(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	path := req.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(target.EffectiveWorkdir, path)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return unavailableViewedImage(req.Path, err), nil
	}
	data, err := imagefileio.Read(ctx, path, imagefileio.MaxReadBytes)
	if err != nil {
		var acquisitionError *imagefileio.AcquisitionError
		if errors.As(err, &acquisitionError) {
			return unavailableViewedImage(path, err), nil
		}
		return nil, err
	}
	return &sessionpb.ViewedImageOriginalSuccess{
		Source: &sessionpb.ViewedImageOriginalSuccess_Content{Content: data},
	}, nil
}

func unavailableViewedImage(path string, err error) *sessionpb.ViewedImageOriginalSuccess {
	slog.Warn("viewed image original unavailable", "path", path, "error", err)
	return &sessionpb.ViewedImageOriginalSuccess{
		Source: &sessionpb.ViewedImageOriginalSuccess_Unavailable{Unavailable: &emptypb.Empty{}},
	}
}
