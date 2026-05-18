package workflowruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"builder/server/llm"
	"builder/server/workflow"
	"builder/server/workflowstore"
	"builder/shared/config"
)

const (
	CompleteNodeToolName = "complete_node"
	structuredOutputName = "workflow_completion"
)

type CompletionMode string

const (
	CompletionModeStructuredOutput CompletionMode = "structured_output"
	CompletionModeTool             CompletionMode = "tool"
)

type CompletionContract struct {
	RunID              workflow.RunID
	ExpectedGeneration int64
	RequireGeneration  bool
	OutputFields       []workflow.OutputField
	TransitionIDs      []string
}

type Config struct {
	Contract                     CompletionContract
	CompletionMode               config.WorkflowCompletionMode
	MaxFinalAnswerViolations     int
	MaxInvalidCompletionAttempts int
	Controller                   Controller
}

type CompletionRequest struct {
	RunID              workflow.RunID
	ExpectedGeneration int64
	RequireGeneration  bool
	TransitionID       string
	OutputValues       map[string]string
	Commentary         string
}

type CompletionResult struct {
	TransitionID workflow.TransitionID
	State        string
}

type ViolationKind string

const (
	ViolationKindFinalAnswer       ViolationKind = "final_answer"
	ViolationKindInvalidCompletion ViolationKind = "invalid_completion"
)

type ViolationResult struct {
	Count       int64
	Interrupted bool
}

type Controller interface {
	CompleteWorkflowRun(ctx context.Context, req CompletionRequest) (CompletionResult, error)
	RecordWorkflowProtocolViolation(ctx context.Context, req ViolationRequest) (ViolationResult, error)
}

type ViolationRequest struct {
	RunID              workflow.RunID
	Kind               ViolationKind
	MaxCount           int
	Detail             string
	ExpectedGeneration int64
	RequireGeneration  bool
}

type StoreController struct {
	Store interface {
		CompleteRun(context.Context, workflowstore.CompleteRunRequest) (workflowstore.CompleteRunResult, error)
		RecordProtocolViolation(context.Context, workflowstore.RecordProtocolViolationRequest) (workflowstore.RecordProtocolViolationResult, error)
	}
}

func (c StoreController) CompleteWorkflowRun(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	if c.Store == nil {
		return CompletionResult{}, errors.New("workflow completion store is required")
	}
	result, err := c.Store.CompleteRun(ctx, workflowstore.CompleteRunRequest{
		RunID:              req.RunID,
		TransitionID:       req.TransitionID,
		OutputValues:       req.OutputValues,
		Commentary:         req.Commentary,
		Actor:              "agent",
		ExpectedGeneration: req.ExpectedGeneration,
		RequireGeneration:  req.RequireGeneration,
	})
	if err != nil {
		return CompletionResult{}, normalizeStoreCompletionError(err)
	}
	return CompletionResult{TransitionID: result.TransitionID, State: result.State}, nil
}

func (c StoreController) RecordWorkflowProtocolViolation(ctx context.Context, req ViolationRequest) (ViolationResult, error) {
	if c.Store == nil {
		return ViolationResult{}, errors.New("workflow completion store is required")
	}
	result, err := c.Store.RecordProtocolViolation(ctx, workflowstore.RecordProtocolViolationRequest{
		RunID:              req.RunID,
		Kind:               workflowstore.ProtocolViolationKind(req.Kind),
		MaxCount:           req.MaxCount,
		Detail:             req.Detail,
		ExpectedGeneration: req.ExpectedGeneration,
		RequireGeneration:  req.RequireGeneration,
	})
	if err != nil {
		return ViolationResult{}, err
	}
	return ViolationResult{Count: result.Count, Interrupted: result.Interrupted}, nil
}

func SelectCompletionMode(mode config.WorkflowCompletionMode, caps llm.ProviderCapabilities) (CompletionMode, error) {
	switch mode {
	case config.WorkflowCompletionModeTool:
		return CompletionModeTool, nil
	case config.WorkflowCompletionModeStructuredOutput:
		if !ProviderSupportsStructuredOutput(caps) {
			return "", fmt.Errorf("workflow structured output completion requires provider responses API support")
		}
		return CompletionModeStructuredOutput, nil
	case config.WorkflowCompletionModeAuto, "":
		if ProviderSupportsStructuredOutput(caps) {
			return CompletionModeStructuredOutput, nil
		}
		return CompletionModeTool, nil
	default:
		return "", fmt.Errorf("invalid workflow completion mode %q", mode)
	}
}

func ProviderSupportsStructuredOutput(caps llm.ProviderCapabilities) bool {
	return caps.SupportsResponsesAPI
}

func StructuredOutput(contract CompletionContract) (*llm.StructuredOutput, error) {
	schema, err := CompletionJSONSchema(contract)
	if err != nil {
		return nil, err
	}
	return &llm.StructuredOutput{
		Name:        structuredOutputName,
		Description: "Complete the current workflow node by selecting a transition and returning node output fields.",
		Schema:      schema,
		Strict:      true,
	}, nil
}

func CompletionJSONSchema(contract CompletionContract) (json.RawMessage, error) {
	transitionIDs := sortedUniqueNonEmptyStrings(contract.TransitionIDs)
	transitionProperty := map[string]any{
		"type":        "string",
		"description": "Transition ID to take. Required when multiple outgoing transitions are available.",
	}
	if len(transitionIDs) > 0 {
		transitionProperty["enum"] = transitionIDs
	}
	properties := map[string]any{
		"transition_id": transitionProperty,
		"commentary": map[string]any{
			"type":        "string",
			"description": "Brief explanation of what was completed and why this transition was selected.",
		},
	}
	required := []string{"transition_id", "commentary"}
	for _, field := range sortedOutputFields(contract.OutputFields) {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		required = append(required, name)
		properties[name] = map[string]any{
			"type":        "string",
			"description": strings.TrimSpace(field.Description),
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

type ParsedCompletion struct {
	TransitionID string
	Commentary   string
	OutputValues map[string]string
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

type ValidationError struct {
	Issues []ValidationIssue `json:"issues"`
}

func (e ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "workflow completion is invalid"
	}
	messages := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		if strings.TrimSpace(issue.Field) != "" {
			messages = append(messages, issue.Field+": "+issue.Message)
			continue
		}
		messages = append(messages, issue.Message)
	}
	return "workflow completion is invalid: " + strings.Join(messages, "; ")
}

func DecodeCompletion(raw json.RawMessage, contract CompletionContract) (ParsedCompletion, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ParsedCompletion{}, ValidationError{Issues: []ValidationIssue{{
			Code:    "invalid_json",
			Message: "completion must be a JSON object",
		}}}
	}
	if payload == nil {
		return ParsedCompletion{}, ValidationError{Issues: []ValidationIssue{{
			Code:    "invalid_json",
			Message: "completion must be a JSON object",
		}}}
	}
	knownOutputs := outputFieldSet(contract.OutputFields)
	parsed := ParsedCompletion{OutputValues: map[string]string{}}
	issues := []ValidationIssue{}
	seen := map[string]bool{}
	for _, key := range sortedRawMessageKeys(payload) {
		value := payload[key]
		field := strings.TrimSpace(key)
		if field == "" {
			issues = append(issues, ValidationIssue{Code: "invalid_field", Message: "field name is required"})
			continue
		}
		seen[field] = true
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			issues = append(issues, ValidationIssue{Code: "non_string_value", Field: field, Message: "value must be a string"})
			continue
		}
		switch field {
		case "transition_id":
			parsed.TransitionID = strings.TrimSpace(text)
		case "commentary":
			parsed.Commentary = text
		default:
			if !knownOutputs[field] {
				issues = append(issues, ValidationIssue{Code: "unknown_output_field", Field: field, Message: "field is not declared by this workflow node"})
				continue
			}
			parsed.OutputValues[field] = text
		}
	}
	if !seen["transition_id"] || parsed.TransitionID == "" {
		issues = append(issues, ValidationIssue{Code: "required_field_missing", Field: "transition_id", Message: "transition_id is required"})
	} else if validTransitions := transitionIDSet(contract.TransitionIDs); len(validTransitions) > 0 && !validTransitions[parsed.TransitionID] {
		issues = append(issues, ValidationIssue{Code: "invalid_transition_id", Field: "transition_id", Message: "transition_id is not declared by this workflow run"})
	}
	if !seen["commentary"] {
		issues = append(issues, ValidationIssue{Code: "required_field_missing", Field: "commentary", Message: "commentary is required"})
	}
	for _, field := range sortedOutputFields(contract.OutputFields) {
		name := strings.TrimSpace(field.Name)
		if name != "" && !seen[name] {
			issues = append(issues, ValidationIssue{Code: "required_field_missing", Field: name, Message: "declared output field is required"})
		}
	}
	if len(issues) > 0 {
		return ParsedCompletion{}, ValidationError{Issues: issues}
	}
	return parsed, nil
}

func sortedRawMessageKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ToolErrorPayload(err error) json.RawMessage {
	issues := []ValidationIssue{{Code: "invalid_completion", Message: strings.TrimSpace(err.Error())}}
	var validation ValidationError
	if errors.As(err, &validation) {
		issues = validation.Issues
	}
	raw, marshalErr := json.Marshal(map[string]any{
		"error":  "workflow completion rejected",
		"issues": issues,
	})
	if marshalErr != nil {
		return json.RawMessage(`{"error":"workflow completion rejected"}`)
	}
	return raw
}

func ToolSuccessPayload(result CompletionResult) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		"status":        "completed",
		"transition_id": string(result.TransitionID),
		"state":         result.State,
	})
	if err != nil {
		return json.RawMessage(`{"status":"completed"}`)
	}
	return raw
}

func normalizeStoreCompletionError(err error) error {
	var validation workflowstore.CompletionValidationError
	if !errors.As(err, &validation) {
		return err
	}
	issues := make([]ValidationIssue, 0, len(validation.Issues))
	for _, issue := range validation.Issues {
		issues = append(issues, ValidationIssue{
			Code:    strings.TrimSpace(issue.Code),
			Field:   strings.TrimSpace(issue.Field),
			Message: strings.TrimSpace(issue.Message),
		})
	}
	return ValidationError{Issues: issues}
}

func outputFieldSet(fields []workflow.OutputField) map[string]bool {
	out := make(map[string]bool, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func sortedOutputFields(fields []workflow.OutputField) []workflow.OutputField {
	out := append([]workflow.OutputField(nil), fields...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].Name) < strings.TrimSpace(out[j].Name)
	})
	return out
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func sortedUniqueNonEmptyStrings(values []string) []string {
	out := uniqueNonEmptyStrings(values)
	sort.Strings(out)
	return out
}

func transitionIDSet(values []string) map[string]bool {
	ids := uniqueNonEmptyStrings(values)
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}
