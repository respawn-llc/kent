package askquestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"core/prompts"
	"core/server/tools"

	"github.com/google/uuid"
)

// Request is the internal broker request. It is intentionally not the
// model-facing tool payload shape because internal approval workflows carry
// fields that must never be exposed through the ask_question tool contract.
type Request struct {
	ID                     string           `json:"-"`
	Question               string           `json:"-"`
	Suggestions            []string         `json:"-"`
	RecommendedOptionIndex int              `json:"-"`
	Approval               bool             `json:"-"`
	ApprovalOptions        []ApprovalOption `json:"-"`
}

// ToolRequest is the model-facing ask_question payload. Keep this limited to
// ordinary question flows; internal approval uses Request instead.
type ToolRequest struct {
	Question               string   `json:"question"`
	Suggestions            []string `json:"suggestions,omitempty"`
	RecommendedOptionIndex int      `json:"recommended_option_index,omitempty"`
}

type ApprovalDecision string

const (
	ApprovalDecisionAllowOnce    ApprovalDecision = "allow_once"
	ApprovalDecisionAllowSession ApprovalDecision = "allow_session"
	ApprovalDecisionDeny         ApprovalDecision = "deny"
)

type ApprovalOption struct {
	Decision ApprovalDecision `json:"decision"`
	Label    string           `json:"label"`
}

type ApprovalPayload struct {
	Decision   ApprovalDecision `json:"decision"`
	Commentary string           `json:"commentary,omitempty"`
}

type Response struct {
	RequestID            string           `json:"request_id"`
	Answer               string           `json:"answer,omitempty"`
	SelectedOptionNumber int              `json:"selected_option_number,omitempty"`
	FreeformAnswer       string           `json:"freeform_answer,omitempty"`
	Approval             *ApprovalPayload `json:"approval,omitempty"`
}

type Broker struct {
	mu    sync.Mutex
	queue []*pending
	// onAsk switches the broker into synchronous handler mode. When unset, Ask
	// uses queued submit mode and requests complete only via Submit.
	onAsk func(Request) (Response, error)
}

type pending struct {
	req       Request
	ch        chan responseResult
	completed bool
}

type responseResult struct {
	response Response
	err      error
}

func NewBroker() *Broker {
	return &Broker{}
}

func (b *Broker) SetAskHandler(handler func(Request) (Response, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onAsk = handler
}

func (b *Broker) Ask(ctx context.Context, req Request) (Response, error) {
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	req.Suggestions = normalizedSuggestions(req.Suggestions)
	req.RecommendedOptionIndex = normalizedRecommendedOptionIndex(req.RecommendedOptionIndex, len(req.Suggestions))
	if req.Question == "" {
		return Response{}, errors.New("question is required")
	}
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}

	h := b.askHandler()
	if h != nil {
		// Synchronous handler mode has exactly one completion path: the handler
		// return value. Requests are never queued in this mode.
		return b.askSync(ctx, req, h)
	}
	// Queued submit mode has exactly one completion path: Submit delivering a
	// validated response to the pending request.
	return b.askQueued(ctx, req)
}

func (b *Broker) askHandler() func(Request) (Response, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.onAsk
}

func (b *Broker) askSync(ctx context.Context, req Request, handler func(Request) (Response, error)) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	resp, err := handler(req)
	if err != nil {
		return Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if resp.RequestID == "" {
		resp.RequestID = req.ID
	}
	if err := validateResponse(req, resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func (b *Broker) askQueued(ctx context.Context, req Request) (Response, error) {

	p := &pending{req: req, ch: make(chan responseResult, 1)}
	b.mu.Lock()
	b.queue = append(b.queue, p)
	b.mu.Unlock()
	defer b.dequeue(req.ID)

	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case rr := <-p.ch:
		return b.finishQueuedResponse(req, rr)
	}
}

func (b *Broker) finishQueuedResponse(req Request, rr responseResult) (Response, error) {
	if rr.err != nil {
		return Response{}, rr.err
	}
	if rr.response.RequestID == "" {
		rr.response.RequestID = req.ID
	}
	if err := validateResponse(req, rr.response); err != nil {
		return Response{}, err
	}
	return rr.response, nil
}

func (b *Broker) Submit(requestID string, resp Response) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.queue {
		if p.req.ID == requestID {
			return b.deliverPendingResponseLocked(p, responseResult{response: resp})
		}
	}
	return fmt.Errorf("request %s not found", requestID)
}

func (b *Broker) deliverPendingResponseLocked(p *pending, rr responseResult) error {
	if p.completed {
		return fmt.Errorf("request %s already completed", p.req.ID)
	}
	if rr.err == nil {
		if rr.response.RequestID == "" {
			rr.response.RequestID = p.req.ID
		}
		if err := validateResponse(p.req, rr.response); err != nil {
			return err
		}
	}
	p.completed = true
	p.ch <- rr
	return nil
}

func validateRequest(req Request) error {
	if req.Approval {
		if req.RecommendedOptionIndex != 0 {
			return errors.New("approval questions must not set recommended_option_index")
		}
		if len(req.Suggestions) > 0 {
			return errors.New("approval questions must not set suggestions")
		}
	}
	if !req.Approval {
		return nil
	}
	if len(req.ApprovalOptions) == 0 {
		return errors.New("approval questions require approval_options")
	}
	seen := make(map[ApprovalDecision]struct{}, len(req.ApprovalOptions))
	for _, option := range req.ApprovalOptions {
		if err := validateApprovalDecision(option.Decision); err != nil {
			return fmt.Errorf("invalid approval option: %w", err)
		}
		if option.Label == "" {
			return fmt.Errorf("approval option %q requires a label", option.Decision)
		}
		if _, ok := seen[option.Decision]; ok {
			return fmt.Errorf("duplicate approval option %q", option.Decision)
		}
		seen[option.Decision] = struct{}{}
	}
	return nil
}

func normalizedSuggestions(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, suggestion := range in {
		trimmed := strings.TrimSpace(suggestion)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizedRecommendedOptionIndex(index int, suggestionCount int) int {
	if suggestionCount == 0 {
		return 0
	}
	if index < 1 || index > suggestionCount {
		return 0
	}
	return index
}

func validateResponse(req Request, resp Response) error {
	if !req.Approval {
		if resp.Approval != nil {
			return errors.New("non-approval questions must not return approval payloads")
		}
		if resp.SelectedOptionNumber > 0 {
			if len(req.Suggestions) == 0 {
				return errors.New("selected option numbers require suggestions")
			}
			if resp.SelectedOptionNumber > len(req.Suggestions) {
				return fmt.Errorf("selected option number %d is out of range", resp.SelectedOptionNumber)
			}
			return nil
		}
		if normalizedFreeformAnswer(resp) == "" {
			return errors.New("non-approval questions require an answer")
		}
		return nil
	}
	if resp.Approval == nil {
		return errors.New("approval questions require approval responses")
	}
	return validateApprovalDecision(resp.Approval.Decision)
}

func normalizedFreeformAnswer(resp Response) string {
	if trimmed := strings.TrimSpace(resp.FreeformAnswer); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(resp.Answer)
}

func buildToolOutputSummary(resp Response) (string, error) {
	freeform := normalizedFreeformAnswer(resp)
	if resp.SelectedOptionNumber > 0 {
		return selectedOptionToolOutputSummary(resp.SelectedOptionNumber, freeform), nil
	}
	if freeform == "" {
		return "", errors.New("non-approval questions require an answer")
	}
	return "User answered: " + freeform, nil
}

func selectedOptionToolOutputSummary(optionNumber int, freeform string) string {
	base := fmt.Sprintf("User chose option #%d.", optionNumber)
	if freeform == "" {
		return base
	}
	return base + " They also said: " + freeform
}

func buildOngoingToolOutputText(req Request, resp Response) string {
	freeform := normalizedFreeformAnswer(resp)
	if resp.SelectedOptionNumber <= 0 {
		return freeform
	}
	suggestions := normalizedSuggestions(req.Suggestions)
	optionIndex := resp.SelectedOptionNumber - 1
	if optionIndex < 0 || optionIndex >= len(suggestions) {
		return ""
	}
	base := suggestions[optionIndex]
	if freeform == "" {
		return base
	}
	return base + "\nUser also said:\n" + freeform
}

func validateApprovalDecision(decision ApprovalDecision) error {
	switch decision {
	case ApprovalDecisionAllowOnce, ApprovalDecisionAllowSession, ApprovalDecisionDeny:
		return nil
	default:
		return fmt.Errorf("unsupported approval decision %q", decision)
	}
}

func (b *Broker) Pending() []Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Request, 0, len(b.queue))
	for _, p := range b.queue {
		out = append(out, p.req)
	}
	return out
}

func (b *Broker) dequeue(requestID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*pending, 0, len(b.queue))
	for _, p := range b.queue {
		if p.req.ID == requestID {
			continue
		}
		out = append(out, p)
	}
	b.queue = out
}

func (r ToolRequest) request(callID string) Request {
	return Request{
		ID:                     callID,
		Question:               r.Question,
		Suggestions:            r.Suggestions,
		RecommendedOptionIndex: r.RecommendedOptionIndex,
	}
}

type Tool struct {
	broker           *Broker
	questionsEnabled func() bool
}

func NewTool(b *Broker, questionsEnabled func() bool) *Tool {
	return &Tool{broker: b, questionsEnabled: questionsEnabled}
}

func (t *Tool) Call(ctx context.Context, c tools.Call) (tools.Result, error) {
	if t.questionsEnabled != nil && !t.questionsEnabled() {
		return tools.ErrorResult(c, prompts.QuestionsDisabledPrompt), nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(c.Input, &raw); err != nil {
		return tools.ErrorResult(c, fmt.Sprintf("invalid input: %v", err)), nil
	}
	if _, ok := raw["action"]; ok {
		return tools.ErrorResult(c, "invalid input: field \"action\" is not allowed"), nil
	}
	if _, ok := raw["approval"]; ok {
		return tools.ErrorResult(c, "invalid input: field \"approval\" is not allowed"), nil
	}
	if _, ok := raw["approval_options"]; ok {
		return tools.ErrorResult(c, "invalid input: field \"approval_options\" is not allowed"), nil
	}

	var in ToolRequest
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return tools.ErrorResult(c, fmt.Sprintf("invalid input: %v", err)), nil
	}
	req := in.request(c.ID)
	resp, err := t.broker.Ask(ctx, req)
	if err != nil {
		return tools.ErrorResult(c, err.Error()), nil
	}
	summary, summaryErr := buildToolOutputSummary(resp)
	if summaryErr != nil {
		return tools.Result{}, summaryErr
	}
	body, marshalErr := json.Marshal(summary)
	if marshalErr != nil {
		return tools.Result{}, marshalErr
	}
	return tools.Result{CallID: c.ID, Name: c.Name, Output: body, OngoingText: buildOngoingToolOutputText(req, resp)}, nil
}
