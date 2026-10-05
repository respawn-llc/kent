package serverstatus

import (
	"bytes"
	"compress/gzip"
	"context"
	"core/internal/testharness/httpclient"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitHubReleaseMetadataSourceReturnsValidatedRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer server.Close()

	metadata, err := newGitHubReleaseMetadataSource(server.Client(), server.URL).LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if metadata.Version.String() != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", metadata.Version)
	}
}

func TestDefaultGitHubReleaseMetadataSourceNegotiatesAndDecodesCompressedResponse(t *testing.T) {
	var acceptEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding = r.Header.Get("Accept-Encoding")
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, _ = writer.Write([]byte(`{"tag_name":"v1.2.3"}`))
		_ = writer.Close()
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()

	metadata, err := newGitHubReleaseMetadataSource(nil, server.URL).LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if metadata.Version.String() != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", metadata.Version)
	}
	if acceptEncoding != "zstd,gzip" {
		t.Fatalf("Accept-Encoding = %q, want zstd,gzip", acceptEncoding)
	}
}

func TestGitHubReleaseMetadataSourceClassifiesTransportFailure(t *testing.T) {
	cause := errors.New("network unavailable")
	client := &http.Client{Transport: httpclient.RoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}

	_, err := newGitHubReleaseMetadataSource(client, "https://release.invalid/latest").LatestRelease(context.Background())
	var transportError *releaseTransportError
	if !errors.As(err, &transportError) || !errors.Is(err, cause) {
		t.Fatalf("error = %v, want transport error wrapping %v", err, cause)
	}
}

func TestGitHubReleaseMetadataSourceClassifiesHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := newGitHubReleaseMetadataSource(server.Client(), server.URL).LatestRelease(context.Background())
	var statusError *releaseHTTPStatusError
	if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("error = %v, want HTTP 503 status error", err)
	}
}

func TestGitHubReleaseMetadataSourceBoundsInvalidMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxReleaseMetadataBytes+1)))
	}))
	defer server.Close()

	_, err := newGitHubReleaseMetadataSource(server.Client(), server.URL).LatestRelease(context.Background())
	var metadataError *releaseMetadataError
	if !errors.As(err, &metadataError) {
		t.Fatalf("error = %v, want metadata error", err)
	}
}

func TestGitHubReleaseMetadataSourceRejectsInvalidTags(t *testing.T) {
	for _, body := range []string{
		`{`,
		`{}`,
		`{"tag_name":null}`,
		`{"tag_name":12}`,
		`{"tag_name":""}`,
		`{"tag_name":" "}`,
		`{"tag_name":"dev"}`,
		`{"tag_name":"1.2"}`,
		`{"tag_name":"1.2.3-preview"}`,
		`{"tag_name":"1.18446744073709551616.0"}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			source := newGitHubReleaseMetadataSource(server.Client(), server.URL)
			_, sourceErr := source.LatestRelease(context.Background())
			var metadataError *releaseMetadataError
			if !errors.As(sourceErr, &metadataError) || errors.Unwrap(metadataError) == nil {
				t.Fatalf("error = %v, want metadata error with cause", sourceErr)
			}

			service := newUpdateStatusService("1.1.0", false, source, time.Now)
			t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })
			result, err := service.status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if result.kind != updateStatusCheckFailed || result.cause != sourceErr.Error() {
				t.Fatalf("result = %#v, want check failure preserving source cause %v", result, sourceErr)
			}
		})
	}
}

func TestUpdateStatusServiceComparesGitHubReleaseVersions(t *testing.T) {
	for _, test := range []struct {
		tag  string
		want updateStatusKind
	}{
		{tag: "v1.10.0", want: updateStatusAvailable},
		{tag: "v1.9.0", want: updateStatusCurrent},
		{tag: "v1.8.0", want: updateStatusCurrent},
		{tag: "v18446744073709551615.0.0", want: updateStatusAvailable},
	} {
		t.Run(test.tag, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"tag_name":"` + test.tag + `"}`))
			}))
			defer server.Close()
			service := newUpdateStatusService("1.9.0", false, newGitHubReleaseMetadataSource(server.Client(), server.URL), time.Now)
			t.Cleanup(func() { requireUpdateStatusServiceClosed(t, service) })
			result, err := service.status(context.Background())
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if result.kind != test.want || result.current != "1.9.0" || result.latest != strings.TrimPrefix(test.tag, "v") {
				t.Fatalf("result = %#v, want kind %d with release %s", result, test.want, test.tag)
			}
		})
	}
}
