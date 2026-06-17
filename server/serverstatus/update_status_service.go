package serverstatus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"core/shared/clientui"
	brand "core/shared/config"
)

const defaultLatestReleaseURL = "https://api.github.com/repos/" + brand.RepoSlug + "/releases/latest"

type UpdateStatusService struct {
	currentVersion string
	latestURL      string
	client         *http.Client

	mu       sync.Mutex
	status   clientui.UpdateStatus
	inflight chan struct{}
	disabled bool
}

type UpdateStatusOption func(*UpdateStatusService)

func WithHTTPClient(client *http.Client) UpdateStatusOption {
	return func(s *UpdateStatusService) {
		if client != nil {
			s.client = client
		}
	}
}

func WithLatestReleaseURL(url string) UpdateStatusOption {
	return func(s *UpdateStatusService) {
		if strings.TrimSpace(url) != "" {
			s.latestURL = strings.TrimSpace(url)
		}
	}
}

func NewUpdateStatusService(currentVersion string, opts ...UpdateStatusOption) *UpdateStatusService {
	s := &UpdateStatusService{
		currentVersion: strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"),
		latestURL:      defaultLatestReleaseURL,
		client:         http.DefaultClient,
		status: clientui.UpdateStatus{
			CurrentVersion: strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"),
		},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.disabled = !isComparableVersion(s.currentVersion)
	if s.disabled {
		s.status.Checked = true
	}
	return s
}

func (s *UpdateStatusService) Status(ctx context.Context) clientui.UpdateStatus {
	if s == nil {
		return clientui.UpdateStatus{}
	}
	s.mu.Lock()
	if s.disabled {
		status := s.status
		s.mu.Unlock()
		return status
	}
	if s.status.Checked {
		status := s.status
		s.mu.Unlock()
		return status
	}
	if s.inflight == nil {
		s.inflight = make(chan struct{})
		done := s.inflight
		go s.refresh(done)
	}
	done := s.inflight
	s.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *UpdateStatusService) Start() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.disabled {
		s.mu.Unlock()
		return
	}
	if s.status.Checked || s.inflight != nil {
		s.mu.Unlock()
		return
	}
	s.inflight = make(chan struct{})
	done := s.inflight
	s.mu.Unlock()
	go s.refresh(done)
}

func (s *UpdateStatusService) refresh(done chan struct{}) {
	defer close(done)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	latest, err := s.fetchLatestVersion(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() { s.inflight = nil }()
	if err != nil {
		s.status.Checked = true
		return
	}
	s.status.Checked = true
	s.status.LatestVersion = latest
	s.status.Available = compareVersions(latest, s.currentVersion) > 0
}

func (s *UpdateStatusService) fetchLatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kent")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New(resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	latest := strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v")
	if !isComparableVersion(latest) {
		return "", errors.New("latest release tag is not semantic")
	}
	return latest, nil
}

func isComparableVersion(version string) bool {
	parts := versionParts(version)
	return len(parts) == 3
}

func compareVersions(left string, right string) int {
	leftParts := versionParts(left)
	rightParts := versionParts(right)
	if len(leftParts) != 3 || len(rightParts) != 3 {
		return 0
	}
	for idx := 0; idx < 3; idx++ {
		if leftParts[idx] > rightParts[idx] {
			return 1
		}
		if leftParts[idx] < rightParts[idx] {
			return -1
		}
	}
	return 0
}

func versionParts(version string) []int {
	rawParts := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
	if len(rawParts) != 3 {
		return nil
	}
	parts := make([]int, 0, 3)
	for _, raw := range rawParts {
		if raw == "" {
			return nil
		}
		value := 0
		for _, r := range raw {
			if r < '0' || r > '9' {
				return nil
			}
			value = value*10 + int(r-'0')
		}
		parts = append(parts, value)
	}
	return parts
}
