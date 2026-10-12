package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"core/shared/client"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/protocol"
)

func TestRemoteResolvesStableSourceForMissingViewedImageOriginal(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	server := newViewedImageHTTPTestServer(t, fixture.gateway)
	remote := newViewedImageSourceRemote(t, server, fixture.bindingA.ProjectID)
	defer func() { _ = remote.Close() }()

	request := &sessionpb.ViewedImageOriginalSourceRequest{
		SessionId: fixture.ownSessionID,
		Path:      "missing/original.png",
	}
	first, err := remote.ResolveViewedImageOriginalSource(context.Background(), request)
	if err != nil {
		t.Fatalf("resolve missing original: %v", err)
	}
	second, err := remote.ResolveViewedImageOriginalSource(context.Background(), request)
	if err != nil {
		t.Fatalf("resolve missing original again: %v", err)
	}
	if first.Url == "" {
		t.Fatal("source URL is empty")
	}
	parsed, err := url.ParseRequestURI(first.Url)
	if err != nil {
		t.Fatalf("source URL is not a relative request URI: %v", err)
	}
	if parsed.IsAbs() {
		t.Fatalf("source URL is absolute: %q", first.Url)
	}
	if second.Url != first.Url {
		t.Fatalf("source URL changed for the same scope: first=%q second=%q", first.Url, second.Url)
	}
}

func TestRemoteRejectsViewedImageSourceOutsideAttachedProject(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	server := newViewedImageHTTPTestServer(t, fixture.gateway)
	remote := newViewedImageSourceRemote(t, server, fixture.bindingA.ProjectID)
	defer func() { _ = remote.Close() }()

	if _, err := remote.ResolveViewedImageOriginalSource(context.Background(), &sessionpb.ViewedImageOriginalSourceRequest{
		SessionId: fixture.ownSessionID,
		Path:      "missing/original.png",
	}); err != nil {
		t.Fatalf("resolved image source inside the attached Project: %v", err)
	}
	_, err := remote.ResolveViewedImageOriginalSource(context.Background(), &sessionpb.ViewedImageOriginalSourceRequest{
		SessionId: fixture.foreignSessionID,
		Path:      "missing/original.png",
	})
	if err == nil {
		t.Fatal("resolved image source outside the attached Project")
	}
}

func TestViewedImageHTTPDoesNotCacheUnavailableOriginal(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	server := newViewedImageHTTPTestServer(t, fixture.gateway)
	remote := newViewedImageSourceRemote(t, server, fixture.bindingA.ProjectID)
	defer func() { _ = remote.Close() }()

	source, err := remote.ResolveViewedImageOriginalSource(context.Background(), &sessionpb.ViewedImageOriginalSourceRequest{
		SessionId: fixture.ownSessionID,
		Path:      "missing/original.png",
	})
	if err != nil {
		t.Fatalf("resolve unavailable original source: %v", err)
	}
	response, err := http.Get(server.URL + source.Url)
	if err != nil {
		t.Fatalf("request unavailable original: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unavailable image response = status %d, cache policy %q",
			response.StatusCode, response.Header.Get("Cache-Control"))
	}
}

func TestViewedImageHTTPRejectsSourceFromAnotherProtocolVersion(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.appCore.Config().WorkspaceRoot, "original.png"), []byte("image"), 0600); err != nil {
		t.Fatalf("write image original: %v", err)
	}
	authority, err := fixture.gateway.sessionReattachAuthority()
	if err != nil {
		t.Fatalf("load image source signing authority: %v", err)
	}
	sourceURL, err := authority.issueViewedImageSourceURL(viewedImageSourceDescriptor{
		ProjectID:       fixture.bindingA.ProjectID,
		SessionID:       fixture.ownSessionID,
		Path:            "original.png",
		ProtocolVersion: "unsupported",
	})
	if err != nil {
		t.Fatalf("issue previous-version image source URL: %v", err)
	}

	server := newViewedImageHTTPTestServer(t, fixture.gateway)
	response, err := http.Get(server.URL + sourceURL)
	if err != nil {
		t.Fatalf("request previous-version image source: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("previous-version image response = status %d, cache policy %q",
			response.StatusCode, response.Header.Get("Cache-Control"))
	}
}

func TestViewedImageHTTPRechecksSessionProjectAccess(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.workspaceB, "private.png"), []byte("private image"), 0600); err != nil {
		t.Fatalf("write foreign image original: %v", err)
	}
	sourceURL := issueViewedImageSourceURLForTest(t, fixture, viewedImageSourceDescriptor{
		ProjectID:       fixture.bindingA.ProjectID,
		SessionID:       fixture.foreignSessionID,
		Path:            "private.png",
		ProtocolVersion: protocol.Version,
	})

	server := newViewedImageHTTPTestServer(t, fixture.gateway)
	response, err := http.Get(server.URL + sourceURL)
	if err != nil {
		t.Fatalf("request foreign image source: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("foreign image response = status %d, cache policy %q",
			response.StatusCode, response.Header.Get("Cache-Control"))
	}
}

func TestViewedImageHTTPReportsOperationalSessionAccessFailure(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	sourceURL := issueViewedImageSourceURLForTest(t, fixture, viewedImageSourceDescriptor{
		ProjectID:       fixture.bindingA.ProjectID,
		SessionID:       fixture.ownSessionID,
		Path:            "original.png",
		ProtocolVersion: protocol.Version,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "http://server"+sourceURL, nil)
	response := httptest.NewRecorder()

	fixture.gateway.ViewedImageHandler().ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("operational Session access failure status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("operational Session access failure cache policy = %q, want no-store", response.Header().Get("Cache-Control"))
	}
}

func TestViewedImageHTTPRequiresActiveCore(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.appCore.Config().WorkspaceRoot, "original.png"), []byte("image"), 0600); err != nil {
		t.Fatalf("write image original: %v", err)
	}
	sourceURL := issueViewedImageSourceURLForTest(t, fixture, viewedImageSourceDescriptor{
		ProjectID:       fixture.bindingA.ProjectID,
		SessionID:       fixture.ownSessionID,
		Path:            "original.png",
		ProtocolVersion: protocol.Version,
	})
	gatedGateway, err := NewGateway(&workflowCoreGate{
		Core:    fixture.appCore,
		failure: errors.New("Core is not active"),
	}, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("create gated Gateway: %v", err)
	}
	server := newViewedImageHTTPTestServer(t, gatedGateway)

	response, err := http.Get(server.URL + sourceURL)
	if err != nil {
		t.Fatalf("request image source while Core is inactive: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("inactive Core image response = status %d, cache policy %q",
			response.StatusCode, response.Header.Get("Cache-Control"))
	}
}

func TestViewedImageHTTPRejectsTamperedSourceDescriptor(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.appCore.Config().WorkspaceRoot, "original.png"), []byte("image"), 0600); err != nil {
		t.Fatalf("write image original: %v", err)
	}
	server := newViewedImageHTTPTestServer(t, fixture.gateway)
	remote := newViewedImageSourceRemote(t, server, fixture.bindingA.ProjectID)
	defer func() { _ = remote.Close() }()

	source, err := remote.ResolveViewedImageOriginalSource(context.Background(), &sessionpb.ViewedImageOriginalSourceRequest{
		SessionId: fixture.ownSessionID,
		Path:      "original.png",
	})
	if err != nil {
		t.Fatalf("resolve viewed image source: %v", err)
	}
	parsed, err := url.ParseRequestURI(source.Url)
	if err != nil {
		t.Fatalf("parse viewed image source URL: %v", err)
	}
	query := parsed.Query()
	descriptor := query.Get("source")
	if descriptor[0] == 'A' {
		descriptor = "B" + descriptor[1:]
	} else {
		descriptor = "A" + descriptor[1:]
	}
	query.Set("source", descriptor)
	parsed.RawQuery = query.Encode()

	response, err := http.Get(server.URL + parsed.RequestURI())
	if err != nil {
		t.Fatalf("request tampered image source: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("tampered source response = status %d, cache policy %q",
			response.StatusCode, response.Header.Get("Cache-Control"))
	}
}

func newViewedImageSourceRemote(t *testing.T, server *httptest.Server, projectID string) *client.Remote {
	t.Helper()
	remote, err := client.DialRemoteURLForProject(
		context.Background(),
		"ws"+server.URL[len("http"):]+protocol.RPCPath,
		projectID,
	)
	if err != nil {
		t.Fatalf("DialRemoteURLForProject: %v", err)
	}
	return remote
}

func newViewedImageHTTPTestServer(t *testing.T, gateway *Gateway) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(protocol.RPCPath, gateway.Handler())
	mux.Handle(ViewedImageBodyPath, gateway.ViewedImageHandler())
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func issueViewedImageSourceURLForTest(
	t *testing.T,
	fixture routePolicyFixture,
	descriptor viewedImageSourceDescriptor,
) string {
	t.Helper()
	authority, err := fixture.gateway.sessionReattachAuthority()
	if err != nil {
		t.Fatalf("load image source signing authority: %v", err)
	}
	sourceURL, err := authority.issueViewedImageSourceURL(descriptor)
	if err != nil {
		t.Fatalf("issue image source URL: %v", err)
	}
	return sourceURL
}
