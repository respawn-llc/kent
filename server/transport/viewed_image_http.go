package transport

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"core/server/sessionstore"
	"core/server/sessionview"
	"core/shared/imagefileio"
	"core/shared/protocol"
)

type viewedImageOriginalOpenerProvider interface {
	ViewedImageOriginalOpener() sessionview.ViewedImageOriginalOpener
}

type viewedImageReadSeeker struct {
	file    io.ReadSeeker
	readErr error
	seekErr error
}

func (s *viewedImageReadSeeker) Read(p []byte) (int, error) {
	n, err := s.file.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.readErr = err
	}
	return n, err
}

func (s *viewedImageReadSeeker) Seek(offset int64, whence int) (int64, error) {
	n, err := s.file.Seek(offset, whence)
	if err != nil {
		s.seekErr = err
	}
	return n, err
}

type viewedImageResponseWriter struct {
	http.ResponseWriter
	writeErr error
}

func (w *viewedImageResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if err != nil {
		w.writeErr = err
	}
	return n, err
}

func (g *Gateway) serveViewedImageOriginal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if err := g.requireCoreActive(); err != nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query()
	sourceValues, sourceOK := query["source"]
	signatureValues, signatureOK := query["signature"]
	if len(query) != 2 || !sourceOK || !signatureOK || len(sourceValues) != 1 || len(signatureValues) != 1 {
		http.NotFound(w, r)
		return
	}
	authority, err := g.sessionReattachAuthority()
	if err != nil {
		slog.Error("viewed image source authority unavailable", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	descriptor, err := authority.verifyViewedImageSource(sourceValues[0], signatureValues[0])
	if err != nil || descriptor.ProtocolVersion != protocol.Version {
		http.NotFound(w, r)
		return
	}
	state := &connectionState{attachedProject: descriptor.ProjectID}
	if err := g.requireSessionInActiveProject(r.Context(), state, descriptor.SessionID); err != nil {
		switch {
		case errors.Is(err, errSessionOutsideActiveProject),
			errors.Is(err, errActiveProjectRequired),
			errors.Is(err, session.ErrSessionNotFound):
			http.NotFound(w, r)
		default:
			slog.Error(
				"authorize viewed image original",
				"project_id", descriptor.ProjectID,
				"session_id", descriptor.SessionID,
				"error", err,
			)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return
	}
	var opener sessionview.ViewedImageOriginalOpener
	if provider, ok := g.deps.(viewedImageOriginalOpenerProvider); ok {
		opener = provider.ViewedImageOriginalOpener()
	}
	if opener == nil {
		slog.Error("viewed image original opener is unavailable")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	opened, err := opener.OpenViewedImageOriginal(r.Context(), descriptor.SessionID, descriptor.Path)
	if err != nil {
		var acquisitionError *imagefileio.AcquisitionError
		if errors.As(err, &acquisitionError) {
			slog.Warn(
				"viewed image original unavailable",
				"session_id", descriptor.SessionID,
				"path", descriptor.Path,
				"error", err,
			)
			http.NotFound(w, r)
			return
		}
		slog.Error(
			"open viewed image original",
			"session_id", descriptor.SessionID,
			"path", descriptor.Path,
			"error", err,
		)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if opened == nil || opened.File == nil || opened.Info == nil {
		if opened != nil && opened.File != nil {
			_ = opened.File.Close()
		}
		slog.Error("viewed image original opener returned incomplete file metadata")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := opened.File.Close(); err != nil {
			slog.Error(
				"close viewed image original",
				"session_id", descriptor.SessionID,
				"path", descriptor.Path,
				"error", err,
			)
		}
	}()

	w.Header().Set("Cache-Control", "private")
	content := &viewedImageReadSeeker{file: opened.File}
	observedWriter := &viewedImageResponseWriter{ResponseWriter: w}
	http.ServeContent(observedWriter, r, opened.Info.Name(), opened.Info.ModTime(), content)
	if content.readErr != nil || content.seekErr != nil || observedWriter.writeErr != nil {
		attrs := []any{
			"session_id", descriptor.SessionID,
			"path", descriptor.Path,
		}
		if content.readErr != nil {
			attrs = append(attrs, "read_error", content.readErr)
		}
		if content.seekErr != nil {
			attrs = append(attrs, "seek_error", content.seekErr)
		}
		if observedWriter.writeErr != nil {
			attrs = append(attrs, "write_error", observedWriter.writeErr)
		}
		slog.Error("stream viewed image original", attrs...)
	}
}
