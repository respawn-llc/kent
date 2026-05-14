package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	defaultManualBrowserRedirectURI = "http://localhost:1455/auth/callback"
	oauthCallbackPath               = "/auth/callback"
	oauthCancelPath                 = "/cancel"
	oauthBindAddress                = "127.0.0.1:1455"
	oauthListenerRetryMax           = 10
	oauthListenerRetryDelay         = 200 * time.Millisecond
	defaultOAuthOriginator          = "builder"
)

type BrowserAuthSession struct {
	AuthorizeURL string
	RedirectURI  string
	State        string
	CodeVerifier string
}

type BrowserCallback struct {
	Code  string
	State string
}

type OAuthCallbackListener struct {
	redirectURI string
	resultCh    chan BrowserCallback
	errCh       chan error
	server      *http.Server
	listener    net.Listener
}

func BeginOpenAIBrowserFlow(opts OpenAIOAuthOptions, redirectURI string) (BrowserAuthSession, error) {
	opts = normalizeOpenAIOAuthOptions(opts)
	if strings.TrimSpace(redirectURI) == "" {
		redirectURI = defaultManualBrowserRedirectURI
	}

	state, err := randomBase64URL(24)
	if err != nil {
		return BrowserAuthSession{}, fmt.Errorf("generate oauth state: %w", err)
	}
	codeVerifier, err := randomBase64URL(48)
	if err != nil {
		return BrowserAuthSession{}, fmt.Errorf("generate oauth code verifier: %w", err)
	}

	h := sha256.Sum256([]byte(codeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	issuer := strings.TrimSuffix(opts.Issuer, "/")
	endpoint := issuer + "/oauth/authorize"
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", opts.ClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("scope", "openid profile email offline_access")
	values.Set("code_challenge", challenge)
	values.Set("code_challenge_method", "S256")
	values.Set("id_token_add_organizations", "true")
	values.Set("codex_cli_simplified_flow", "true")
	values.Set("originator", defaultOAuthOriginator)
	values.Set("state", state)

	return BrowserAuthSession{
		AuthorizeURL: endpoint + "?" + values.Encode(),
		RedirectURI:  redirectURI,
		State:        state,
		CodeVerifier: codeVerifier,
	}, nil
}

func CompleteOpenAIBrowserFlow(ctx context.Context, opts OpenAIOAuthOptions, session BrowserAuthSession, callbackInput string) (Method, error) {
	parsed, err := ParseOAuthCallbackInput(callbackInput)
	if err != nil {
		return Method{}, err
	}
	if strings.TrimSpace(session.State) != "" && strings.TrimSpace(parsed.State) != "" && parsed.State != session.State {
		return Method{}, errors.New("oauth state mismatch")
	}
	if strings.TrimSpace(parsed.Code) == "" {
		return Method{}, errors.New("oauth callback is missing code")
	}
	return exchangeOpenAIAuthorizationCode(ctx, opts, parsed.Code, session.CodeVerifier, session.RedirectURI)
}

func ParseOAuthCallbackInput(input string) (BrowserCallback, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return BrowserCallback{}, errors.New("oauth callback input is empty")
	}

	if strings.Contains(input, "://") {
		u, err := url.Parse(input)
		if err != nil {
			return BrowserCallback{}, fmt.Errorf("parse callback url: %w", err)
		}
		q := u.Query()
		return BrowserCallback{Code: q.Get("code"), State: q.Get("state")}, nil
	}

	if strings.Contains(input, "code=") {
		q, err := url.ParseQuery(strings.TrimPrefix(input, "?"))
		if err != nil {
			return BrowserCallback{}, fmt.Errorf("parse callback query: %w", err)
		}
		return BrowserCallback{Code: q.Get("code"), State: q.Get("state")}, nil
	}

	return BrowserCallback{Code: input}, nil
}

func StartOAuthCallbackListener() (*OAuthCallbackListener, error) {
	var (
		ln              net.Listener
		err             error
		cancelAttempted bool
	)
	for attempts := 0; attempts < oauthListenerRetryMax; attempts++ {
		ln, err = net.Listen("tcp", oauthBindAddress)
		if err == nil {
			break
		}
		if isAddrInUse(err) {
			if !cancelAttempted {
				_ = sendOAuthCancelRequest()
				cancelAttempted = true
			}
			if attempts < oauthListenerRetryMax-1 {
				time.Sleep(oauthListenerRetryDelay)
				continue
			}
		}
		return nil, fmt.Errorf("listen oauth callback on %s: %w", oauthBindAddress, err)
	}
	if ln == nil {
		return nil, fmt.Errorf("listen oauth callback on %s: exhausted retries", oauthBindAddress)
	}
	resultCh := make(chan BrowserCallback, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == oauthCancelPath {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OAuth callback listener canceled"))
			return
		}
		if r.URL.Path != oauthCallbackPath {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not found"))
			return
		}
		q := r.URL.Query()
		if authErr := strings.TrimSpace(q.Get("error")); authErr != "" {
			authErrDesc := strings.TrimSpace(q.Get("error_description"))
			if authErrDesc == "" {
				authErrDesc = authErr
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Authorization failed: " + authErrDesc))
			select {
			case errCh <- fmt.Errorf("oauth callback returned error: %s", authErrDesc):
			default:
			}
			return
		}
		result := BrowserCallback{Code: q.Get("code"), State: q.Get("state")}
		if strings.TrimSpace(result.Code) == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Missing code in callback"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(authCompleteHTML()))
		select {
		case resultCh <- result:
		default:
		}
	})}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()
	return &OAuthCallbackListener{
		redirectURI: defaultManualBrowserRedirectURI,
		resultCh:    resultCh,
		errCh:       errCh,
		server:      srv,
		listener:    ln,
	}, nil
}

func authCompleteHTML() string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Auth complete</title>
<style>
:root {
  color-scheme: dark;
  --bg: #0b0f14;
  --fg: #d6deeb;
  --muted: #7d8590;
  --primary: #7dd3fc;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: var(--bg);
  color: var(--fg);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
}
main { text-align: center; padding: 32px; }
h1 {
  margin: 0 0 24px;
  color: var(--primary);
  font-size: clamp(42px, 8vw, 82px);
  line-height: 1;
  letter-spacing: -0.04em;
}
p {
  margin: 0;
  color: var(--muted);
  font-size: 16px;
}
</style>
</head>
<body>
<main>
<h1>Auth complete</h1>
<p>You can close this tab now.</p>
</main>
</body>
</html>`
}

func (l *OAuthCallbackListener) RedirectURI() string {
	if l == nil {
		return ""
	}
	return l.redirectURI
}

func (l *OAuthCallbackListener) Wait(ctx context.Context, timeout time.Duration) (BrowserCallback, error) {
	if l == nil {
		return BrowserCallback{}, errors.New("oauth callback listener is nil")
	}
	if timeout <= 0 {
		timeout = defaultPollTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer l.Close()

	select {
	case <-waitCtx.Done():
		if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
			return BrowserCallback{}, errors.New("oauth browser callback timed out")
		}
		return BrowserCallback{}, waitCtx.Err()
	case serveErr := <-l.errCh:
		return BrowserCallback{}, fmt.Errorf("oauth callback server failed: %w", serveErr)
	case result := <-l.resultCh:
		return result, nil
	}
}

func (l *OAuthCallbackListener) Close() error {
	if l == nil {
		return nil
	}
	_ = l.server.Shutdown(context.Background())
	if l.listener != nil {
		return l.listener.Close()
	}
	return nil
}

func OpenBrowser(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return errors.New("empty url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}

func randomBase64URL(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sendOAuthCancelRequest() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+oauthBindAddress+oauthCancelPath, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) ||
		strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
