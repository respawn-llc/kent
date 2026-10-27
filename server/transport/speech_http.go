package transport

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"core/server/speechservice"
	"core/shared/protoapi"
	speechpb "core/shared/protoapi/gen/kent/api/speech"
	"google.golang.org/protobuf/proto"
)

const SpeechUploadPath = "/speech"
const speechSignaturePurpose = "kent:speech-upload\x00"

func (a *sessionReattachAuthority) speechUploadURL(version string) string {
	signature := a.signCapability([]byte(speechSignaturePurpose), []byte(version))
	query := url.Values{"version": {version}, "signature": {base64.RawURLEncoding.EncodeToString(signature)}}
	return (&url.URL{Path: SpeechUploadPath, RawQuery: query.Encode()}).RequestURI()
}

func (g *Gateway) SpeechHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != SpeechUploadPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeSpeechFailure(w, http.StatusMethodNotAllowed, errors.New("speech upload requires POST"))
			return
		}
		if err := g.requireCoreActive(); err != nil {
			writeSpeechFailure(w, http.StatusServiceUnavailable, err)
			return
		}
		authority, err := g.sessionReattachAuthority()
		if err != nil {
			writeSpeechFailure(w, http.StatusInternalServerError, err)
			return
		}
		query := r.URL.Query()
		signature, err := base64.RawURLEncoding.DecodeString(query.Get("signature"))
		if err != nil || len(query) != 2 || len(query["version"]) != 1 || len(query["signature"]) != 1 ||
			query.Get("version") != g.identity.ProtocolVersion ||
			!hmac.Equal(signature, authority.signCapability([]byte(speechSignaturePurpose), []byte(g.identity.ProtocolVersion))) {
			writeSpeechFailure(w, http.StatusForbidden, errors.New("invalid speech upload authorization or protocol version"))
			return
		}
		client := g.deps.SpeechClient()
		if client == nil {
			writeSpeechFailure(w, http.StatusServiceUnavailable, errors.New("speech service is unavailable"))
			return
		}
		// The request body can be blocked in a network read when a provider
		// rejects early. A read deadline releases that read before Body.Close.
		body := &speechBody{ReadCloser: r.Body, controller: http.NewResponseController(w)}
		defer func() {
			if err := body.Close(); err != nil {
				slog.Warn("close speech ingress", "error", err)
			}
		}()
		result := client.Transcribe(r.Context(), r.Header.Get("Content-Type"), body)
		writeSpeechResult(w, http.StatusOK, result)
	})
}

type speechBody struct {
	io.ReadCloser
	controller *http.ResponseController
	once       sync.Once
	err        error
}

func (b *speechBody) Close() error {
	b.once.Do(func() {
		deadlineErr := b.controller.SetReadDeadline(time.Now())
		b.err = errors.Join(deadlineErr, b.ReadCloser.Close())
	})
	return b.err
}

func writeSpeechFailure(w http.ResponseWriter, status int, err error) {
	writeSpeechResult(w, status, speech.Failure(speechpb.FailureKind_FAILURE_KIND_CONNECTION, nil, err))
}

func writeSpeechResult(w http.ResponseWriter, status int, result *speechpb.UploadResult) {
	if err := protoapi.Validate(result); err != nil {
		slog.Error("invalid speech result", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	data, err := proto.Marshal(result)
	if err != nil {
		slog.Error("encode speech result", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/protobuf")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		slog.Warn("write speech result", "error", err)
	}
}
