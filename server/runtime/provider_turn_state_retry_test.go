package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/httpclient"
	"core/server/llm"
	"core/server/tools"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type providerTurnStateOAuthAuth struct {
	mu         sync.Mutex
	accountIDs []string
	next       int
}

func (a *providerTurnStateOAuthAuth) ResolveDispatchAuth(context.Context) (*llm.DispatchAuth, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := a.next
	if index >= len(a.accountIDs) {
		index = len(a.accountIDs) - 1
	} else {
		a.next++
	}
	return &llm.DispatchAuth{Header: "Bearer token", Mode: llm.OpenAIAuthMode{IsOAuth: true, AccountID: a.accountIDs[index]}}, nil
}

func TestToolLoopContinuationReplaysProviderTurnState(t *testing.T) {
	const firstState = "first-turn-state"
	var (
		mu     sync.Mutex
		states []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("x-codex-turn-state", firstState)
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			attempt := len(states)
			mu.Unlock()
			if attempt <= 2 {
				return []string{runtimeCompletedResponseEvent([]byte(fmt.Sprintf(`[{"type":"function_call","id":"item_%[1]d","call_id":"call_%[1]d","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}"}]`, attempt)))}
			}
			return []string{runtimeCompletedResponseEvent([]byte(`[{"type":"message","id":"final","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"done"}]}]`))}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	transport := newProviderTurnStateTransport(t, server, "account-1")
	client := llm.NewResponsesClient(transport, nil)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand},
		}),
		Config{Model: "gpt-6-sol"},
	)

	answer, err := engine.SubmitUserMessage(t.Context(), "run the tool")
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if got := messageContent(answer); got != "done" {
		t.Fatalf("assistant content = %q, want completed tool-loop answer", got)
	}

	mu.Lock()
	gotStates := append([]string(nil), states...)
	mu.Unlock()
	if fmt.Sprint(gotStates) != fmt.Sprint([]string{"", "", ""}) {
		t.Fatalf("tool-loop routing headers changed from socket opening: %q", gotStates)
	}
}

func TestNewAgentTurnStartsWithoutProviderTurnState(t *testing.T) {
	const firstTurnState = "first-turn-state"
	var (
		mu     sync.Mutex
		states []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("x-codex-turn-state", firstTurnState)
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			attempt := len(states)
			mu.Unlock()
			if attempt == 1 {
				return []string{runtimeToolCallEvent("call_1")}
			}
			return []string{runtimeFinalAnswerEvent("done")}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	transport := newProviderTurnStateTransport(t, server, "account-1")
	client := llm.NewResponsesClient(transport, nil)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand},
		}),
		Config{Model: "gpt-6-sol"},
	)
	for range 2 {
		if _, err := engine.SubmitUserMessage(t.Context(), "complete a turn"); err != nil {
			t.Fatalf("SubmitUserMessage: %v", err)
		}
	}

	mu.Lock()
	gotStates := append([]string(nil), states...)
	mu.Unlock()
	if fmt.Sprint(gotStates) != fmt.Sprint([]string{"", "", ""}) {
		t.Fatalf("successive Agent Turn routing headers changed from socket opening: %q", gotStates)
	}
}

func TestSocketKeepsOpeningAccountDespiteLaterCredentialChanges(t *testing.T) {
	const firstAccountState = "account-a-state"
	var (
		mu       sync.Mutex
		states   []string
		accounts []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("x-codex-turn-state", firstAccountState)
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			accounts = append(accounts, request.Header.Get("ChatGPT-Account-Id"))
			attempt := len(states)
			mu.Unlock()
			if attempt < 3 {
				return []string{runtimeToolCallEvent(fmt.Sprintf("call_%d", attempt))}
			}
			return []string{runtimeFinalAnswerEvent("done")}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	transport := newProviderTurnStateTransport(t, server, "account-a", "account-b", "account-b")
	client := llm.NewResponsesClient(transport, nil)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand},
		}),
		Config{Model: "gpt-6-sol"},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "change provider account"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}

	mu.Lock()
	gotStates := append([]string(nil), states...)
	gotAccounts := append([]string(nil), accounts...)
	mu.Unlock()
	if fmt.Sprint(gotStates) != fmt.Sprint([]string{"", "", ""}) {
		t.Fatalf("socket opening routing headers changed: %q", gotStates)
	}
	if fmt.Sprint(gotAccounts) != fmt.Sprint([]string{"account-a", "account-a", "account-a"}) {
		t.Fatalf("socket opening account changed: %q", gotAccounts)
	}
}

func TestUnidentifiedAccountOmitsAndDoesNotAcceptProviderTurnState(t *testing.T) {
	const accountState = "identified-account-state"
	var (
		mu       sync.Mutex
		states   []string
		accounts []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("x-codex-turn-state", accountState)
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			accounts = append(accounts, request.Header.Get("ChatGPT-Account-Id"))
			attempt := len(states)
			mu.Unlock()
			if attempt < 3 {
				return []string{runtimeToolCallEvent(fmt.Sprintf("call_%d", attempt))}
			}
			return []string{runtimeFinalAnswerEvent("done")}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	transport := newProviderTurnStateTransport(t, server, "", "account-a")
	client := llm.NewResponsesClient(transport, nil)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t, tools.HandlerRegistration{
			ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand},
		}),
		Config{Model: "gpt-6-sol"},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "continue with missing account identity"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}

	mu.Lock()
	gotStates := append([]string(nil), states...)
	gotAccounts := append([]string(nil), accounts...)
	mu.Unlock()
	if fmt.Sprint(gotStates) != fmt.Sprint([]string{"", "", ""}) {
		t.Fatalf("unidentified-account turn states = %q, want no exchange for the unknown account and no acceptance of its candidate", gotStates)
	}
	if fmt.Sprint(gotAccounts) != fmt.Sprint([]string{"", "", ""}) {
		t.Fatalf("request accounts = %q, want account identity omitted only for the unidentified dispatch", gotAccounts)
	}
}

func TestConcurrentSessionsKeepProviderTurnStateIsolatedByAccount(t *testing.T) {
	var (
		mu        sync.Mutex
		states    = make(map[string][]string)
		sessionID = make(map[string]string)
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		accountID := request.Header.Get("ChatGPT-Account-Id")
		w.Header().Set("x-codex-turn-state", accountID+"-turn-state")
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states[accountID] = append(states[accountID], request.Header.Get("x-codex-turn-state"))
			sessionID[accountID] = request.Header.Get("session-id")
			attempt := len(states[accountID])
			mu.Unlock()
			if attempt == 1 {
				return []string{runtimeToolCallEvent("call_1")}
			}
			return []string{runtimeFinalAnswerEvent("done")}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	makeEngine := func(accountID string) *Engine {
		t.Helper()
		transport := newProviderTurnStateTransport(t, server, accountID)
		client := llm.NewResponsesClient(transport, nil)
		return mustNewTestEngine(
			t,
			mustCreateTestSession(t),
			client,
			newTestToolRegistry(t, tools.HandlerRegistration{
				ID: toolspec.ToolExecCommand, Handler: fakeTool{name: toolspec.ToolExecCommand},
			}),
			Config{Model: "gpt-6-sol"},
		)
	}
	first := makeEngine("account-a")
	second := makeEngine("account-b")
	done := make(chan error, 2)
	go func() {
		_, err := first.SubmitUserMessage(t.Context(), "first concurrent Session")
		done <- err
	}()
	go func() {
		_, err := second.SubmitUserMessage(t.Context(), "second concurrent Session")
		done <- err
	}()
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("concurrent SubmitUserMessage: %v", err)
		}
	}

	mu.Lock()
	firstStates := append([]string(nil), states["account-a"]...)
	secondStates := append([]string(nil), states["account-b"]...)
	firstSession := sessionID["account-a"]
	secondSession := sessionID["account-b"]
	mu.Unlock()
	if fmt.Sprint(firstStates) != fmt.Sprint([]string{"", ""}) ||
		fmt.Sprint(secondStates) != fmt.Sprint([]string{"", ""}) {
		t.Fatalf("concurrent per-account request states = %q / %q, want each Session's own token", firstStates, secondStates)
	}
	if firstSession == "" || secondSession == "" || firstSession == secondSession {
		t.Fatalf("concurrent Session identities = %q / %q, want distinct nonempty Session IDs", firstSession, secondSession)
	}
}

func TestReviewerSharesMainAgentTurnProviderTurnState(t *testing.T) {
	const firstState = "main-turn-state"
	store := mustCreateTestSession(t)
	mainSessionID := store.Meta().SessionID
	reviewerSession := reviewerSessionID(mainSessionID)
	var (
		mu         sync.Mutex
		states     []string
		sessionIDs []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("x-codex-turn-state", firstState)
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			sessionIDs = append(sessionIDs, request.Header.Get("session-id"))
			mu.Unlock()
			if request.Header.Get("session-id") == reviewerSession {
				return []string{runtimeFinalAnswerEvent(`{"suggestions":[]}`)}
			}
			return []string{runtimeFinalAnswerEvent("main response")}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	mainClient := llm.NewResponsesClient(newProviderTurnStateTransport(t, server, "account-1"), nil)
	reviewerClient := llm.NewResponsesClient(newProviderTurnStateTransport(t, server, "account-1"), nil)
	engine := mustNewTestEngine(t, store, mainClient, newTestToolRegistry(t), Config{
		Model: "gpt-6-sol",
		Reviewer: ReviewerConfig{
			Model: "gpt-6-sol", Client: reviewerClient,
		},
	})
	for turn := range 2 {
		err := withActiveTestRun(t, engine, ActiveKindUserTurn, func(ctx context.Context, stepID string) error {
			sendMain := func() error {
				request, err := engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
				if err != nil {
					return err
				}
				_, err = mainClient.Generate(ctx, request, llm.StreamCallbacks{})
				return err
			}
			sendReviewer := func() error {
				request, err := engine.buildReviewerDispatchRequest(ctx, stepID)
				if err != nil {
					return err
				}
				_, err = reviewerClient.Generate(ctx, request, llm.StreamCallbacks{})
				return err
			}
			if turn == 0 {
				if err := sendMain(); err != nil {
					return err
				}
				if err := sendReviewer(); err != nil {
					return err
				}
				return sendMain()
			}
			if err := sendReviewer(); err != nil {
				return err
			}
			return sendMain()
		})
		if err != nil {
			t.Fatalf("Reviewer routing turn %d: %v", turn, err)
		}
	}

	mu.Lock()
	gotStates := append([]string(nil), states...)
	gotSessionIDs := append([]string(nil), sessionIDs...)
	mu.Unlock()
	if fmt.Sprint(gotStates) != fmt.Sprint([]string{"", firstState, "", firstState, ""}) {
		t.Fatalf("main/Reviewer turn states = %q, want shared first value in either request order and fresh state on the next turn", gotStates)
	}
	if fmt.Sprint(gotSessionIDs) != fmt.Sprint([]string{mainSessionID, reviewerSession, mainSessionID, reviewerSession, mainSessionID}) {
		t.Fatalf("main/Reviewer Session identities = %q, want existing distinct Reviewer metadata identity", gotSessionIDs)
	}
}

func TestReviewerMismatchedRouteKeepsInvocationStateIsolated(t *testing.T) {
	withGenerateRetryDelays(t, []time.Duration{0})
	tests := []struct {
		name               string
		reviewerAccountIDs []string
		reviewerEndpoint   *string
		wantStates         []string
		wantAccounts       []string
	}{
		{
			name:               "different account",
			reviewerAccountIDs: []string{"account-b", "account-b"},
			wantStates:         []string{"", "", "reviewer-route-state", ""},
			wantAccounts:       []string{"account-a", "account-b", "account-b", "account-a"},
		},
		{
			name:               "different endpoint",
			reviewerAccountIDs: []string{"account-a", "account-a"},
			reviewerEndpoint:   textutil.Value("https://chatgpt-alt.invalid/backend-api/codex/responses"),
			wantStates:         []string{"", "", "reviewer-route-state", ""},
			wantAccounts:       []string{"account-a", "account-a", "account-a", "account-a"},
		},
		{
			name:               "unidentified account",
			reviewerAccountIDs: []string{"", ""},
			wantStates:         []string{"", "", "", ""},
			wantAccounts:       []string{"account-a", "", "", "account-a"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := mustCreateTestSession(t)
			mainSessionID := store.Meta().SessionID
			reviewerSession := reviewerSessionID(mainSessionID)
			var (
				mu               sync.Mutex
				states           []string
				accounts         []string
				sessionIDs       []string
				reviewerOpenings int
			)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				record := func() {
					mu.Lock()
					defer mu.Unlock()
					states = append(states, request.Header.Get("x-codex-turn-state"))
					accounts = append(accounts, request.Header.Get("ChatGPT-Account-Id"))
					sessionIDs = append(sessionIDs, request.Header.Get("session-id"))
				}
				isReviewer := request.Header.Get("session-id") == reviewerSession
				if isReviewer {
					mu.Lock()
					reviewerOpenings++
					first := reviewerOpenings == 1
					mu.Unlock()
					w.Header().Set("x-codex-turn-state", "reviewer-route-state")
					if first {
						record()
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = w.Write([]byte(`{"error":{"message":"retry reviewer request","type":"server_error"}}`))
						return
					}
				} else {
					w.Header().Set("x-codex-turn-state", "main-route-state")
				}
				nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
					record()
					if isReviewer {
						return []string{runtimeFinalAnswerEvent(`{"suggestions":[]}`)}
					}
					return []string{runtimeFinalAnswerEvent("main response")}
				}).ServeHTTP(w, request)
			}))
			t.Cleanup(server.Close)

			mainClient := llm.NewResponsesClient(newProviderTurnStateTransport(t, server, "account-a"), nil)
			reviewerTransport := newProviderTurnStateTransportAtEndpoint(
				t,
				server,
				test.reviewerEndpoint,
				test.reviewerAccountIDs...,
			)
			reviewerClient := llm.NewResponsesClient(reviewerTransport, nil)
			engine := mustNewTestEngine(t, store, mainClient, newTestToolRegistry(t), Config{
				Model: "gpt-6-sol",
				Reviewer: ReviewerConfig{
					Model: "gpt-6-sol", Client: reviewerClient,
				},
			})
			err := withActiveTestRun(t, engine, ActiveKindUserTurn, func(ctx context.Context, stepID string) error {
				mainRequest, err := engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
				if err != nil {
					return err
				}
				if _, err := mainClient.Generate(ctx, mainRequest, llm.StreamCallbacks{}); err != nil {
					return err
				}
				reviewerRequest, err := engine.buildReviewerDispatchRequest(ctx, stepID)
				if err != nil {
					return err
				}
				if _, err := engine.generateWithRetryClient(ctx,
					stepID,
					newObservedModelClient(reviewerClient),
					reviewerRequest, llm.StreamCallbacks{}, nil); err != nil {
					return err
				}
				followupRequest, err := engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
				if err != nil {
					return err
				}
				_, err = mainClient.Generate(ctx, followupRequest, llm.StreamCallbacks{})
				return err
			})
			if err != nil {
				t.Fatalf("mismatched Reviewer route request sequence: %v", err)
			}

			mu.Lock()
			gotStates := append([]string(nil), states...)
			gotAccounts := append([]string(nil), accounts...)
			gotSessionIDs := append([]string(nil), sessionIDs...)
			mu.Unlock()
			if fmt.Sprint(gotStates) != fmt.Sprint(test.wantStates) {
				t.Fatalf("request turn states = %q, want %q", gotStates, test.wantStates)
			}
			if fmt.Sprint(gotAccounts) != fmt.Sprint(test.wantAccounts) {
				t.Fatalf("request accounts = %q, want %q", gotAccounts, test.wantAccounts)
			}
			if fmt.Sprint(gotSessionIDs) != fmt.Sprint([]string{mainSessionID, reviewerSession, reviewerSession, mainSessionID}) {
				t.Fatalf("request Session identities = %q, want distinct Reviewer metadata identity", gotSessionIDs)
			}
		})
	}
}

func TestLocalCompactionKeepsRoutingStateSeparateFromMainTurn(t *testing.T) {
	withGenerateRetryDelays(t, []time.Duration{0})
	const (
		mainTurnState     = "main-turn-state"
		localCompactState = "local-compaction-state"
	)
	var (
		mu     sync.Mutex
		states []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		first := len(states) == 0
		mu.Unlock()
		if first {
			w.Header().Set("x-codex-turn-state", mainTurnState)
		} else {
			w.Header().Set("x-codex-turn-state", localCompactState)
		}
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			attempt := len(states)
			mu.Unlock()
			if attempt == 2 {
				return []string{`{"type":"error","status":503,"error":{"code":"server_error","type":"server_error","message":"retry local compaction"}}`}
			}
			if attempt == 3 {
				return []string{runtimeFinalAnswerEvent("local summary")}
			}
			return []string{runtimeFinalAnswerEvent("main response")}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	mainClient := llm.NewResponsesClient(newProviderTurnStateTransport(t, server, "account-1"), nil)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		mainClient,
		newTestToolRegistry(t),
		Config{Model: "gpt-6-sol", CompactionMode: "local"},
	)
	err := withActiveTestRun(t, engine, ActiveKindUserTurn, func(ctx context.Context, stepID string) error {
		if err := engine.steer(stepID, steerMessagesWithPersistenceIntent(
			steeringPriorityNormal,
			steeringMessageEventNone,
			true,
			[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact this turn")}},
		)); err != nil {
			return err
		}
		mainRequest, err := engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
		if err != nil {
			return err
		}
		if _, err := mainClient.Generate(ctx, mainRequest, llm.StreamCallbacks{}); err != nil {
			return err
		}
		if _, _, err := engine.compactNow(ctx, stepID, compactionModeManual, compactionInstructionsInput{}, false); err != nil {
			return err
		}
		followupRequest, err := engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
		if err != nil {
			return err
		}
		_, err = mainClient.Generate(ctx, followupRequest, llm.StreamCallbacks{})
		return err
	})
	if err != nil {
		t.Fatalf("main turn with local compaction: %v", err)
	}

	mu.Lock()
	gotStates := append([]string(nil), states...)
	mu.Unlock()
	wantStates := []string{"", "", "", ""}
	if fmt.Sprint(gotStates) != fmt.Sprint(wantStates) {
		t.Fatalf("main/local-compaction request states = %q, want separate local retry state and unchanged main-turn state %q", gotStates, wantStates)
	}
}

func TestRemoteCompactionRetryAndLocalFallbackKeepTurnStateScopes(t *testing.T) {
	withCompactionRetryDelays(t, []time.Duration{0})
	withGenerateRetryDelays(t, []time.Duration{0})
	const (
		mainTurnState = "main-turn-state"
	)
	var (
		mu     sync.Mutex
		states []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Upgrade") == "websocket" {
			w.Header().Set("x-codex-turn-state", mainTurnState)
			nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
				mu.Lock()
				states = append(states, request.Header.Get("x-codex-turn-state"))
				attempt := len(states)
				mu.Unlock()
				if attempt == 4 {
					return []string{`{"type":"error","status":503,"error":{"type":"server_error","code":"server_error","message":"retry local compaction"}}`}
				}
				if attempt == 5 {
					return []string{runtimeFinalAnswerEvent("local summary")}
				}
				return []string{runtimeFinalAnswerEvent("generation response")}
			}).ServeHTTP(w, request)
			return
		}
		mu.Lock()
		states = append(states, request.Header.Get("x-codex-turn-state"))
		attempt := len(states)
		mu.Unlock()

		switch attempt {
		case 2:
			w.Header().Set("x-codex-turn-state", "first-compaction-candidate")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"retry remote compaction","type":"server_error"}}`))
		case 3:
			w.Header().Set("x-codex-turn-state", "later-compaction-candidate")
			writeRuntimeCompletedResponseSSE(w, []byte(`[{"id":"cmp_1","type":"compaction","encrypted_content":"first"},{"id":"cmp_2","type":"compaction","encrypted_content":"second"}]`))
		default:
			http.Error(w, "unexpected model request", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	client := llm.NewResponsesClient(newProviderTurnStateTransport(t, server, "account-1"), nil)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t),
		Config{Model: "gpt-6-sol", CompactionMode: "native"},
	)
	err := withActiveTestRun(t, engine, ActiveKindUserTurn, func(ctx context.Context, stepID string) error {
		if err := engine.steer(stepID, steerMessagesWithPersistenceIntent(
			steeringPriorityNormal,
			steeringMessageEventNone,
			true,
			[]llm.Message{{Role: llm.RoleUser, Content: textutil.Value("compact within this turn")}},
		)); err != nil {
			return err
		}
		request, err := engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
		if err != nil {
			return err
		}
		if _, err := client.Generate(ctx, request, llm.StreamCallbacks{}); err != nil {
			return err
		}
		if _, _, err := engine.compactNow(ctx, stepID, compactionModeManual, compactionInstructionsInput{}, false); err != nil {
			return err
		}
		request, err = engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
		if err != nil {
			return err
		}
		_, err = client.Generate(ctx, request, llm.StreamCallbacks{})
		return err
	})
	if err != nil {
		t.Fatalf("main turn with remote compaction: %v", err)
	}

	mu.Lock()
	gotStates := append([]string(nil), states...)
	mu.Unlock()
	wantStates := []string{"", mainTurnState, mainTurnState, "", "", ""}
	if fmt.Sprint(gotStates) != fmt.Sprint(wantStates) {
		t.Fatalf("generation/remote-compaction/local-fallback request states = %q, want turn-state replay and isolated local fallback %q", gotStates, wantStates)
	}
}

func TestStandaloneCompactionStartsWithoutPriorAgentTurnState(t *testing.T) {
	const mainTurnState = "main-turn-state"
	var (
		mu     sync.Mutex
		states []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Upgrade") == "websocket" {
			w.Header().Set("x-codex-turn-state", mainTurnState)
			nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
				mu.Lock()
				states = append(states, request.Header.Get("x-codex-turn-state"))
				mu.Unlock()
				return []string{runtimeFinalAnswerEvent("ordinary work")}
			}).ServeHTTP(w, request)
			return
		}
		mu.Lock()
		states = append(states, request.Header.Get("x-codex-turn-state"))
		attempt := len(states)
		mu.Unlock()

		switch attempt {
		case 2:
			w.Header().Set("x-codex-turn-state", "standalone-compaction-state")
			writeRuntimeCompletedResponseSSE(w, []byte(`[{"id":"cmp_1","type":"compaction","encrypted_content":"opaque"}]`))
		default:
			http.Error(w, "unexpected model request", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	client := llm.NewResponsesClient(newProviderTurnStateTransport(t, server, "account-1"), nil)
	engine := mustNewTestEngine(
		t,
		mustCreateTestSession(t),
		client,
		newTestToolRegistry(t),
		Config{Model: "gpt-6-sol", CompactionMode: "native"},
	)
	if _, err := engine.SubmitUserMessage(t.Context(), "prepare standalone compaction"); err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	waitEngineLifecycleTasks(t, engine)
	if err := engine.CompactContext(t.Context(), ""); err != nil {
		t.Fatalf("CompactContext: %v", err)
	}
	waitEngineLifecycleTasks(t, engine)

	mu.Lock()
	gotStates := append([]string(nil), states...)
	mu.Unlock()
	if fmt.Sprint(gotStates) != fmt.Sprint([]string{"", ""}) {
		t.Fatalf("turn/standalone-compaction request states = %q, want fresh standalone operation state", gotStates)
	}
}

func TestInvalidProviderTurnStateDoesNotChangeSuccessfulModelOutcome(t *testing.T) {
	var requestedStates []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("x-codex-turn-state", "")
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			requestedStates = append(requestedStates, request.Header.Get("x-codex-turn-state"))
			return []string{runtimeFinalAnswerEvent("completed")}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	client := llm.NewResponsesClient(newProviderTurnStateTransport(t, server, "account-1"), nil)
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{Model: "gpt-6-sol"})
	answer, err := engine.SubmitUserMessage(t.Context(), "complete normally")
	if err != nil {
		t.Fatalf("SubmitUserMessage: %v", err)
	}
	if got := messageContent(answer); got != "completed" {
		t.Fatalf("assistant content = %q, want the provider's successful answer", got)
	}
	if fmt.Sprint(requestedStates) != fmt.Sprint([]string{""}) {
		t.Fatalf("request turn states = %q, want initial absence", requestedStates)
	}
}

func TestGenerateWithRetryReplaysExactProviderTurnState(t *testing.T) {
	withGenerateRetryDelays(t, []time.Duration{0})
	const turnState = "opaque,state=value"
	var mu sync.Mutex
	var states []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		first := len(states) == 0
		mu.Unlock()
		if first {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			mu.Unlock()
			w.Header().Set("x-codex-turn-state", turnState)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"retry me","type":"server_error"}}`))
			return
		}
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			mu.Lock()
			states = append(states, request.Header.Get("x-codex-turn-state"))
			mu.Unlock()
			return []string{runtimeCompletedResponseEvent(nil)}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)

	dispatch, err := llm.NewCodexDispatchContext(llm.CodexDispatchFacts{
		SessionID: "session-1", StepID: "step-1",
		CompactionGeneration: 1, RequestKind: llm.CodexRequestKindTurn.Optional(),
	})
	if err != nil {
		t.Fatalf("NewCodexDispatchContext: %v", err)
	}
	transport := newProviderTurnStateTransport(t, server, "account-1")
	client := llm.NewResponsesClient(transport, nil)
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{Model: "gpt-6-sol"})
	_, err = engine.generateWithRetryClient(context.Background(), runtimeTestStepID("provider-turn-state"), newObservedModelClient(client), llm.Request{
		Model: "gpt-6-sol", SessionID: textutil.Value("session-1"), CodexDispatch: dispatch,
		ToolChoiceMode: llm.ToolChoiceModeAutomatic, ReasoningEffort: "high",
	}, llm.StreamCallbacks{}, nil)
	if err != nil {
		t.Fatalf("generateWithRetryClient: %v", err)
	}
	mu.Lock()
	gotStates := append([]string(nil), states...)
	mu.Unlock()
	if fmt.Sprint(gotStates) != fmt.Sprint([]string{"", turnState}) {
		t.Fatalf("received turn states = %q, want exact bounded-retry replay", gotStates)
	}
}

func TestGenerationMissingOutputRebuildReplaysProviderTurnState(t *testing.T) {
	var states []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if len(states) == 0 {
			states = append(states, request.Header.Get("x-codex-turn-state"))
			w.Header().Set("x-codex-turn-state", "generation-repair-state")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"missing tool output","type":"invalid_request_error"}}`))
			return
		}
		nativeResponsesSocketHandler(t, func(json.RawMessage) []string {
			states = append(states, request.Header.Get("x-codex-turn-state"))
			return []string{runtimeCompletedResponseEvent([]byte(`[{"type":"message","id":"repaired","role":"assistant","status":"completed","content":[{"type":"output_text","text":"repaired","annotations":[]}]}]`))}
		}).ServeHTTP(w, request)
	}))
	t.Cleanup(server.Close)
	transport := newProviderTurnStateTransport(t, server, "account-1")
	client := llm.NewResponsesClient(transport, nil)
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{Model: "gpt-6-sol"})
	steerDanglingToolCall(t, engine, "seed", llm.ToolCall{ID: "missing", Name: "exec_command", Input: []byte(`{}`)})
	err := engine.stepLifecycle.Run(t.Context(), exclusiveStepOptions{ActiveKind: ActiveKindUserTurn}, func(ctx context.Context, stepID string) error {
		_, err := engine.generateWithMissingToolOutputRepair(ctx, stepID, func() (llm.Request, error) {
			return engine.buildActiveTurnDispatchRequest(ctx, stepID, nil, true)
		}, llm.StreamCallbacks{}, nil)
		return err
	})
	if err != nil {
		t.Fatalf("generation repair: %v", err)
	}
	if fmt.Sprint(states) != fmt.Sprint([]string{"", "generation-repair-state"}) {
		t.Fatalf("changed generation payload states = %q, want same-turn state replay", states)
	}
}

func writeRuntimeCompletedResponseSSE(w http.ResponseWriter, output []byte) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", runtimeCompletedResponseEvent(output))
}

func runtimeCompletedResponseEvent(output []byte) string {
	if output == nil {
		output = []byte(`[]`)
	}
	return fmt.Sprintf(`{"type":"response.completed","response":{"id":"runtime-response","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2},"output":%s}}`, output)
}

func runtimeToolCallEvent(callID string) string {
	output := []byte(fmt.Sprintf(
		`[{"id":"fc_%[1]s","type":"function_call","call_id":"%[1]s","name":"exec_command","arguments":"{\"command\":\"pwd\"}"}]`,
		callID,
	))
	return runtimeCompletedResponseEvent(output)
}

func runtimeFinalAnswerEvent(content string) string {
	output := []byte(fmt.Sprintf(
		`[{"id":"msg_1","type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":%q}]}]`,
		content,
	))
	return runtimeCompletedResponseEvent(output)
}

func newProviderTurnStateTransport(t *testing.T, server *httptest.Server, accountIDs ...string) *llm.HTTPTransport {
	return newProviderTurnStateTransportAtEndpoint(t, server, nil, accountIDs...)
}

func newProviderTurnStateTransportAtEndpoint(
	t *testing.T,
	server *httptest.Server,
	endpoint *string,
	accountIDs ...string,
) *llm.HTTPTransport {
	t.Helper()
	if len(accountIDs) == 0 {
		t.Fatal("provider turn-state fixture requires an explicit account identity sequence")
	}
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	registration, err := llm.ResolveConnectionVariant(config.ProviderConnection{Protocol: config.ConnectionChatGPT})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != nil {
		registration.Variant.BaseURL = endpoint
	}
	transport, err := llm.NewHTTPTransport(&providerTurnStateOAuthAuth{accountIDs: accountIDs}, registration)
	if err != nil {
		t.Fatal(err)
	}
	transport.Client = &http.Client{
		Transport: httpclient.NewURLRewriteTransport(target, server.Client().Transport, ""),
	}
	return transport
}
