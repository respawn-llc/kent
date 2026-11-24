package taskimport

import (
	"compress/gzip"
	"context"
	"core/internal/testharness/httpclient"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
	"encoding/json"
	"errors"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGitHubReadRetriesTransientFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		statuses   []*int
		wantStatus *int32
	}{
		{name: "network then server error", statuses: []*int{nil, new(http.StatusBadGateway), new(http.StatusOK)}},
		{name: "exhausted", statuses: []*int{new(http.StatusInternalServerError), new(http.StatusBadGateway), new(http.StatusServiceUnavailable)}, wantStatus: proto.Int32(http.StatusServiceUnavailable)},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			reader := newGitHubReader(&http.Client{Transport: httpclient.RoundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer fake" {
					t.Errorf("unexpected GitHub request: %s %s", request.Method, request.URL.Path)
				}
				if calls >= len(test.statuses) {
					t.Fatal("unexpected extra attempt")
				}
				status := test.statuses[calls]
				calls++
				if status == nil {
					return nil, &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}
				}
				response := httptest.NewRecorder()
				response.WriteHeader(*status)
				if *status == http.StatusOK {
					if _, err := io.WriteString(response, `{"login":"developer"}`); err != nil {
						t.Fatal(err)
					}
				}
				return response.Result(), nil
			})}, proto.String("fake"))
			account, err := reader.account(t.Context())
			if calls != len(test.statuses) {
				t.Fatalf("attempts = %d, want %d; error: %v", calls, len(test.statuses), err)
			}
			if test.wantStatus == nil {
				if err != nil || account.GetLogin() != "developer" {
					t.Fatalf("account = %v, error = %v", account, err)
				}
			} else {
				var failure *serverapi.TaskImportError
				if !errors.As(err, &failure) || failure.Detail.GetHttpStatus() != *test.wantStatus ||
					failure.Detail.GetPhase() != taskimportpb.FailurePhase_FAILURE_PHASE_CREDENTIALS {
					t.Fatalf("last GitHub failure = %v", err)
				}
			}
		})
	}
}

func TestGitHubReadDoesNotRetryAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	reader := newGitHubReader(&http.Client{Transport: httpclient.RoundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}
	})}, proto.String("fake"))
	_, err := reader.account(ctx)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("GitHub read returned %v after %d attempts", err, calls)
	}
}

func TestGitHubFailuresAndAccount(t *testing.T) {
	for _, test := range []struct {
		status    int
		remaining string
		code      taskimportpb.FailureCode
	}{
		{401, "", taskimportpb.FailureCode_FAILURE_CODE_AUTHENTICATION},
		{403, "0", taskimportpb.FailureCode_FAILURE_CODE_RATE_LIMIT},
		{404, "", taskimportpb.FailureCode_FAILURE_CODE_UNAVAILABLE},
		{429, "", taskimportpb.FailureCode_FAILURE_CODE_RATE_LIMIT},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer fake" {
					t.Error("selected credential not used")
				}
				w.Header().Set("X-RateLimit-Remaining", test.remaining)
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			reader := newGitHubReader(&http.Client{Transport: httpclient.NewURLRewriteTransport(target, http.DefaultTransport, "")}, proto.String("fake"))
			_, err = reader.account(t.Context())
			var failure *serverapi.TaskImportError
			if !errors.As(err, &failure) || failure.Detail.Code != test.code || calls != 1 {
				t.Fatalf("%v, calls=%d", err, calls)
			}
		})
	}
}

func TestCommentsArePaged(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var encoded io.WriteCloser
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("Content-Encoding", "gzip")
			encoded = gzip.NewWriter(w)
		} else {
			w.Header().Set("Content-Encoding", "zstd")
			var err error
			encoded, err = zstd.NewWriter(w)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		defer func() {
			if err := encoded.Close(); err != nil {
				t.Error(err)
			}
		}()
		if r.URL.Query().Get("page") == "1" {
			values := make([]githubComment, 100)
			for i := range values {
				values[i].Body = "    code"
				values[i].User.Login = "author"
			}
			if err := json.NewEncoder(encoded).Encode(values); err != nil {
				t.Error(err)
			}
		} else {
			if err := json.NewEncoder(encoded).Encode([]githubComment{{Body: "last"}}); err != nil {
				t.Error(err)
			}
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	reader := newGitHubReader(&http.Client{Transport: httpclient.NewURLRewriteTransport(target, http.DefaultTransport, "")}, nil)
	count := 0
	err = reader.comments(t.Context(), issueReference{Repository: repository{"owner", "repo"}, Number: 1}, func(comment githubComment) error {
		count++
		if count <= 100 && calls != 1 {
			t.Fatal("page was staged instead of delivered")
		}
		return nil
	})
	if err != nil || count != 101 || calls != 2 {
		t.Fatalf("comments=%d requests=%d error=%v", count, calls, err)
	}
}
