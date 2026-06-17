package serverstatus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusReportsAvailableRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0"}`))
	}))
	defer server.Close()

	service := NewUpdateStatusService("1.1.0", WithLatestReleaseURL(server.URL), WithHTTPClient(server.Client()))
	status := service.Status(context.Background())

	if !status.Checked || !status.Available || status.LatestVersion != "1.2.0" || status.CurrentVersion != "1.1.0" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestStatusSkipsDevVersion(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	defer server.Close()

	service := NewUpdateStatusService("dev", WithLatestReleaseURL(server.URL), WithHTTPClient(server.Client()))
	status := service.Status(context.Background())

	if !status.Checked || status.Available || calls != 0 {
		t.Fatalf("unexpected dev status=%+v calls=%d", status, calls)
	}
}

func TestStatusCachesFailedCheck(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	service := NewUpdateStatusService("1.1.0", WithLatestReleaseURL(server.URL), WithHTTPClient(server.Client()))
	first := service.Status(context.Background())
	second := service.Status(context.Background())

	if !first.Checked || !second.Checked || first.Available || second.Available || calls != 1 {
		t.Fatalf("unexpected failed-check cache: first=%+v second=%+v calls=%d", first, second, calls)
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("1.10.0", "1.9.9") <= 0 {
		t.Fatal("expected numeric semver ordering")
	}
	if compareVersions("1.0.0", "1.0.0") != 0 {
		t.Fatal("expected equal versions")
	}
	if compareVersions("1.0.0", "1.0.1") >= 0 {
		t.Fatal("expected patch ordering")
	}
}
