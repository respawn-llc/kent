package llm

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/openai/openai-go/v3/responses"
	"golang.org/x/net/http/httpguts"
)

const codexTurnStateHeader = "x-codex-turn-state"

type CodexRequestKind string

const (
	CodexRequestKindTurn       CodexRequestKind = "turn"
	CodexRequestKindCompaction CodexRequestKind = "compaction"
)

func (k CodexRequestKind) Optional() *CodexRequestKind {
	return &k
}

type CodexDispatchFacts struct {
	SessionID               string
	StepID                  string
	CompactionGeneration    int
	RequestKind             *CodexRequestKind
	TurnStateLifetime       *CodexTurnStateLifetime
	SharedTurnStateLifetime *CodexTurnStateLifetime
}

type CodexDispatchContext struct {
	facts CodexDispatchFacts
	state *CodexDispatchState
}

type CodexDispatchState struct {
	mu          sync.Mutex
	turnState   *CodexTurnState
	diagnostics map[CodexTurnStateDiagnosticCategory]struct{}
}

type CodexTurnStateLifetime struct {
	mu    sync.Mutex
	route *codexTurnStateRoute
	state *CodexTurnState
}

type CodexTurnState struct {
	mu       sync.Mutex
	value    string
	hasValue bool
}

type codexTurnStateRoute struct {
	accountID string
	endpoint  string
}

type CodexTurnStateDiagnosticCategory string

const (
	CodexTurnStateDiagnosticInvalid CodexTurnStateDiagnosticCategory = "provider_turn_state_invalid"
)

type codexTurnStateSource string

const (
	codexTurnStateSourceHTTPHeader codexTurnStateSource = "http_header"
	codexTurnStateSourceMetadata   codexTurnStateSource = "response_metadata"
)

type codexTurnMetadata struct {
	SessionID   string            `json:"session_id"`
	ThreadID    string            `json:"thread_id"`
	TurnID      string            `json:"turn_id"`
	WindowID    string            `json:"window_id"`
	RequestKind *CodexRequestKind `json:"request_kind,omitempty"`
}

type codexDispatchProjection struct {
	TurnMetadataJSON string
	RoutingHint      string
}

func NewCodexDispatchContext(facts CodexDispatchFacts) (*CodexDispatchContext, error) {
	if err := facts.validate(); err != nil {
		return nil, err
	}
	lifetime := facts.TurnStateLifetime
	if lifetime == nil {
		lifetime = NewCodexTurnStateLifetime()
		facts.TurnStateLifetime = lifetime
	}
	return &CodexDispatchContext{
		facts: facts,
		state: &CodexDispatchState{turnState: lifetime.currentState()},
	}, nil
}

func NewCodexTurnStateLifetime() *CodexTurnStateLifetime {
	return &CodexTurnStateLifetime{state: &CodexTurnState{}}
}

func (c *CodexDispatchContext) TurnMetadataJSON() (string, error) {
	if err := c.validate(); err != nil {
		return "", err
	}
	payload, err := json.Marshal(codexTurnMetadata{
		SessionID:   c.facts.SessionID,
		ThreadID:    c.facts.SessionID,
		TurnID:      c.facts.StepID,
		WindowID:    fmt.Sprintf("%s:%d", c.facts.SessionID, c.facts.CompactionGeneration),
		RequestKind: c.facts.RequestKind,
	})
	if err != nil {
		return "", fmt.Errorf("marshal Codex turn metadata: %w", err)
	}
	return string(payload), nil
}

func (c *CodexDispatchContext) outboundProjection(
	model string,
	serviceTier *responses.ResponseNewParamsServiceTier,
) (codexDispatchProjection, error) {
	metadata, err := c.TurnMetadataJSON()
	if err != nil {
		return codexDispatchProjection{}, err
	}
	routingHint, err := buildCodexRoutingHint(model, serviceTier)
	if err != nil {
		return codexDispatchProjection{}, err
	}
	return codexDispatchProjection{
		TurnMetadataJSON: metadata,
		RoutingHint:      routingHint,
	}, nil
}

func (c *CodexDispatchContext) turnStateForRequest() (string, bool) {
	return c.currentTurnState()
}

func (c *CodexDispatchContext) currentTurnState() (string, bool) {
	if c == nil || c.state == nil {
		return "", false
	}
	c.state.mu.Lock()
	state := c.state.turnState
	c.state.mu.Unlock()
	return state.current()
}

func (c *CodexDispatchContext) bindTurnState(accountID *string, endpoint string) {
	if c == nil || c.state == nil || c.facts.TurnStateLifetime == nil {
		return
	}
	lifetime := c.facts.TurnStateLifetime
	var state *CodexTurnState
	if c.facts.SharedTurnStateLifetime != nil {
		var shared bool
		state, shared = c.facts.SharedTurnStateLifetime.bindRouteIfUnboundOrMatches(accountID, endpoint)
		if shared {
			lifetime.clear()
		} else {
			state = lifetime.bindRoute(accountID, endpoint)
		}
	} else {
		state = lifetime.bindRoute(accountID, endpoint)
	}
	c.state.mu.Lock()
	c.state.turnState = state
	c.state.mu.Unlock()
}

func (c *CodexDispatchContext) observeTurnStateCandidate(value string, source codexTurnStateSource) {
	if c == nil || c.state == nil {
		return
	}
	c.state.mu.Lock()
	turnState := c.state.turnState
	c.state.mu.Unlock()
	if validateCodexHeaderValue(value) != nil {
		c.observeInvalidTurnState(source)
		return
	}
	if turnState == nil {
		return
	}
	turnState.accept(value)
}

func (c *CodexDispatchContext) observeInvalidTurnState(source codexTurnStateSource) {
	c.state.mu.Lock()
	shouldWarn := c.state.recordDiagnosticLocked(CodexTurnStateDiagnosticInvalid)
	requestKind := c.facts.RequestKind
	c.state.mu.Unlock()
	c.warnUnusableTurnState(shouldWarn, CodexTurnStateDiagnosticInvalid, source, requestKind)
}

func (l *CodexTurnStateLifetime) currentState() *CodexTurnState {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

func (l *CodexTurnStateLifetime) bindRoute(accountID *string, endpoint string) *CodexTurnState {
	if l == nil || accountID == nil {
		return nil
	}
	route := codexTurnStateRoute{accountID: *accountID, endpoint: endpoint}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.route == nil || *l.route != route {
		l.route = &route
		l.state = &CodexTurnState{}
	}
	return l.state
}

func (l *CodexTurnStateLifetime) bindRouteIfUnboundOrMatches(accountID *string, endpoint string) (*CodexTurnState, bool) {
	if l == nil || accountID == nil {
		return nil, false
	}
	route := codexTurnStateRoute{accountID: *accountID, endpoint: endpoint}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.route != nil && *l.route != route {
		return nil, false
	}
	if l.route == nil {
		l.route = &route
		l.state = &CodexTurnState{}
	}
	return l.state, true
}

func (l *CodexTurnStateLifetime) clear() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.route = nil
	l.state = &CodexTurnState{}
	l.mu.Unlock()
}

func (s *CodexTurnState) current() (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value, s.hasValue
}

func (s *CodexTurnState) accept(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasValue {
		s.value = value
		s.hasValue = true
	}
}

func (s *CodexDispatchState) recordDiagnosticLocked(category CodexTurnStateDiagnosticCategory) bool {
	if s.diagnostics == nil {
		s.diagnostics = make(map[CodexTurnStateDiagnosticCategory]struct{}, 1)
	}
	if _, exists := s.diagnostics[category]; exists {
		return false
	}
	s.diagnostics[category] = struct{}{}
	return true
}

func (c *CodexDispatchContext) warnUnusableTurnState(
	shouldWarn bool,
	category CodexTurnStateDiagnosticCategory,
	source codexTurnStateSource,
	requestKind *CodexRequestKind,
) {
	if !shouldWarn {
		return
	}
	attributes := []any{
		"category", category,
		"source", source,
	}
	if requestKind != nil {
		attributes = append(attributes, "request_kind", *requestKind)
	}
	slog.Warn("ignored unusable provider turn state", attributes...)
}

func (c *CodexDispatchContext) TurnStateDiagnostics() []CodexTurnStateDiagnosticCategory {
	if c == nil || c.state == nil {
		return nil
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if _, exists := c.state.diagnostics[CodexTurnStateDiagnosticInvalid]; exists {
		return []CodexTurnStateDiagnosticCategory{CodexTurnStateDiagnosticInvalid}
	}
	return nil
}

func (c *CodexDispatchContext) validate() error {
	if c == nil {
		return fmt.Errorf("%w: Codex dispatch context is required", ErrInvalidRequest)
	}
	if c.state == nil {
		return fmt.Errorf("%w: Codex dispatch state is required", ErrInvalidRequest)
	}
	return c.facts.validate()
}

func (c *CodexDispatchContext) validateForSession(sessionID string) error {
	if err := c.validate(); err != nil {
		return err
	}
	if sessionID != c.facts.SessionID {
		return fmt.Errorf(
			"%w: Codex dispatch session ID %q does not match request session ID %q",
			ErrInvalidRequest,
			c.facts.SessionID,
			sessionID,
		)
	}
	return nil
}

func validateOpenAIDispatchSessionID(sessionID string) error {
	if err := validateCodexHeaderValue(sessionID); err != nil {
		return fmt.Errorf("%w: Session ID is not wire-realizable: %v", ErrInvalidRequest, err)
	}
	return nil
}

func validateSessionDispatchPairing(sessionID *string, dispatch *CodexDispatchContext) error {
	if sessionID != nil {
		if err := validateOpenAIDispatchSessionID(*sessionID); err != nil {
			return err
		}
	}
	if dispatch == nil {
		return nil
	}
	if sessionID == nil {
		return fmt.Errorf("%w: Session identity is required with Codex dispatch context", ErrInvalidRequest)
	}
	return dispatch.validateForSession(*sessionID)
}

func validateOpenAIDispatch(
	sessionID string,
	model string,
	dispatch *CodexDispatchContext,
	isChatGPTCodex bool,
	serviceTier *responses.ResponseNewParamsServiceTier,
) (*codexDispatchProjection, error) {
	if !isChatGPTCodex {
		return nil, nil
	}
	if dispatch == nil {
		return nil, fmt.Errorf("%w: Codex dispatch context is required for OAuth dispatch", ErrInvalidRequest)
	}
	if err := dispatch.validateForSession(sessionID); err != nil {
		return nil, err
	}
	projection, err := dispatch.outboundProjection(model, serviceTier)
	if err != nil {
		return nil, err
	}
	return &projection, nil
}

func validateCodexHeaderValue(value string) error {
	if len(value) == 0 {
		return fmt.Errorf("value is required")
	}
	if value[0] == ' ' || value[0] == '\t' || value[len(value)-1] == ' ' || value[len(value)-1] == '\t' {
		return fmt.Errorf("value has leading or trailing SP/HTAB")
	}
	if !httpguts.ValidHeaderFieldValue(value) {
		return fmt.Errorf("value is not a valid HTTP header field value")
	}
	return nil
}

func buildCodexRoutingHint(model string, serviceTier *responses.ResponseNewParamsServiceTier) (string, error) {
	if len(model) == 0 {
		return "", fmt.Errorf("%w: Codex routing model is required", ErrInvalidRequest)
	}
	if model[0] == ' ' || model[0] == '\t' || model[len(model)-1] == ' ' || model[len(model)-1] == '\t' {
		return "", fmt.Errorf("%w: Codex routing model has leading or trailing SP/HTAB", ErrInvalidRequest)
	}
	if strings.Contains(model, ";") {
		return "", fmt.Errorf("%w: Codex routing model contains semicolon", ErrInvalidRequest)
	}
	if !httpguts.ValidHeaderFieldValue(model) {
		return "", fmt.Errorf("%w: Codex routing model is not a valid HTTP header field value", ErrInvalidRequest)
	}
	hint := "model=" + model
	if serviceTier != nil {
		hint += ";tier=" + string(*serviceTier)
	}
	if !httpguts.ValidHeaderFieldValue(hint) {
		return "", fmt.Errorf("%w: Codex routing hint is not a valid HTTP header field value", ErrInvalidRequest)
	}
	return hint, nil
}

func (f CodexDispatchFacts) validate() error {
	if strings.TrimSpace(f.SessionID) == "" {
		return fmt.Errorf("%w: Codex session ID is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(f.StepID) == "" {
		return fmt.Errorf("%w: Codex Step ID is required", ErrInvalidRequest)
	}
	if f.CompactionGeneration < 0 {
		return fmt.Errorf("%w: Codex compaction generation must be >= 0", ErrInvalidRequest)
	}
	if f.RequestKind == nil {
		return nil
	}
	switch *f.RequestKind {
	case CodexRequestKindTurn, CodexRequestKindCompaction:
		return nil
	default:
		return fmt.Errorf("%w: unknown Codex request kind %q", ErrInvalidRequest, *f.RequestKind)
	}
}
