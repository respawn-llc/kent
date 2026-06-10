package workflowruntime

import (
	"bytes"
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
	Transitions        []CompletionTransition
}

type CompletionTransition struct {
	ID          string
	DisplayName string
	Description string
	Parameters  []workflow.Parameter
}

type Config struct {
	RunID                        workflow.RunID
	Contract                     CompletionContract
	CompletionMode               config.WorkflowCompletionMode
	MaxFinalAnswerViolations     int
	MaxInvalidCompletionAttempts int
	Controller                   Controller
	Instructions                 TaskInstructions
}

type TaskInstructions struct {
	TaskID          string
	TaskShortID     string
	TaskTitle       string
	TaskBody        string
	WorkflowID      string
	WorkflowShortID string
	NodeID          string
	NodeKey         string
	NodeDisplayName string
	ContextMode     string
	SourceSessionID string
	Transitions     []TransitionInstruction
	NodePrompt      string
}

type TransitionInstruction struct {
	ID          string
	DisplayName string
	Description string
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
		Description: "Complete the current workflow node by selecting a transition and returning required transition parameters.",
		Schema:      schema,
		Strict:      true,
	}, nil
}

func CompletionJSONSchema(contract CompletionContract) (json.RawMessage, error) {
	transitions := normalizedTransitions(contract.Transitions)
	transitionIDs := sortedTransitionIDs(transitions)
	properties := map[string]any{
		"commentary": commentaryProperty(),
	}
	required := []string{}
	if len(transitions) > 1 {
		properties["transition"] = transitionProperty(transitionIDs)
		required = append(required, "transition")
	}
	required = append(required, "commentary")
	for _, parameter := range schemaParameters(transitions) {
		name := strings.TrimSpace(parameter.Key)
		properties[name] = map[string]any{
			"type":        "string",
			"description": strings.TrimSpace(parameter.Description),
		}
		if len(transitions) == 1 {
			required = append(required, name)
		}
	}
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
	if len(transitions) > 1 {
		schema["oneOf"] = transitionBranchSchemas(transitions)
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func transitionBranchSchemas(transitions []CompletionTransition) []any {
	branches := make([]any, 0, len(transitions))
	for _, transition := range transitions {
		id := strings.TrimSpace(transition.ID)
		if id == "" {
			continue
		}
		properties := map[string]any{
			"transition": transitionProperty([]string{id}),
			"commentary": commentaryProperty(),
		}
		required := []string{"transition", "commentary"}
		for _, parameter := range normalizedParameters(transition.Parameters) {
			key := strings.TrimSpace(parameter.Key)
			if key == "" {
				continue
			}
			properties[key] = parameterProperty(parameter)
			required = append(required, key)
		}
		branches = append(branches, map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           properties,
			"required":             required,
		})
	}
	return branches
}

func transitionProperty(transitionIDs []string) map[string]any {
	property := map[string]any{
		"type":        "string",
		"description": "Transition to take. Required when multiple outgoing transitions are available.",
	}
	if len(transitionIDs) > 0 {
		property["enum"] = transitionIDs
	}
	return property
}

func commentaryProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "Brief explanation of what was completed and why this transition was selected.",
	}
}

func parameterProperty(parameter workflow.Parameter) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": strings.TrimSpace(parameter.Description),
	}
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
	transitions := normalizedTransitions(contract.Transitions)
	knownParameters := parameterSet(schemaParameters(transitions))
	parsed := ParsedCompletion{OutputValues: map[string]string{}}
	issues := []ValidationIssue{}
	seen := map[string]bool{}
	invalidFields := map[string]bool{}
	for _, key := range sortedRawMessageKeys(payload) {
		value := payload[key]
		field := strings.TrimSpace(key)
		if field == "" {
			issues = append(issues, ValidationIssue{Code: "invalid_field", Message: "field name is required"})
			continue
		}
		seen[field] = true
		switch field {
		case "transition":
			text, ok, issue := decodeStringValue(value, field)
			if !ok {
				issues = append(issues, issue)
				invalidFields[field] = true
				continue
			}
			parsed.TransitionID = strings.TrimSpace(text)
		case "commentary":
			text, ok, issue := decodeStringValue(value, field)
			if !ok {
				issues = append(issues, issue)
				invalidFields[field] = true
				continue
			}
			parsed.Commentary = text
		default:
			if field == "transition_id" {
				issues = append(issues, ValidationIssue{Code: "unknown_field", Field: field, Message: "field is not part of the workflow completion schema"})
				continue
			}
			if !knownParameters[field] {
				issues = append(issues, ValidationIssue{Code: "unknown_parameter", Field: field, Message: "parameter is not declared by this workflow run"})
				continue
			}
			text, ok, issue := decodeStringValue(value, field)
			if !ok {
				issues = append(issues, issue)
				invalidFields[field] = true
				continue
			}
			parsed.OutputValues[field] = text
		}
	}
	selected := CompletionTransition{}
	hasSelected := false
	if !invalidFields["transition"] {
		var transitionIssues []ValidationIssue
		selected, hasSelected, transitionIssues = selectedTransition(parsed.TransitionID, seen["transition"], transitions)
		issues = append(issues, transitionIssues...)
	}
	if hasSelected {
		parsed.TransitionID = strings.TrimSpace(selected.ID)
		selectedParameters := normalizedParameters(selected.Parameters)
		selectedParameterSet := parameterSet(selectedParameters)
		for _, key := range sortedStringKeys(parsed.OutputValues) {
			if !selectedParameterSet[key] {
				issues = append(issues, ValidationIssue{Code: "unexpected_parameter", Field: key, Message: "parameter is not declared by the selected transition"})
			}
		}
		for _, parameter := range selectedParameters {
			key := strings.TrimSpace(parameter.Key)
			if strings.TrimSpace(parsed.OutputValues[key]) == "" {
				issues = append(issues, ValidationIssue{Code: "required_parameter_missing", Field: key, Message: "parameter is required by the selected transition"})
			}
		}
	}
	if !seen["commentary"] {
		issues = append(issues, ValidationIssue{Code: "required_field_missing", Field: "commentary", Message: "commentary is required"})
	}
	if len(issues) > 0 {
		return ParsedCompletion{}, ValidationError{Issues: issues}
	}
	return parsed, nil
}

func decodeStringValue(value json.RawMessage, field string) (string, bool, ValidationIssue) {
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false, ValidationIssue{Code: "non_string_value", Field: field, Message: "value must be a string"}
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false, ValidationIssue{Code: "non_string_value", Field: field, Message: "value must be a string"}
	}
	return text, true, ValidationIssue{}
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
		"status":     "completed",
		"transition": string(result.TransitionID),
		"state":      result.State,
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
		issues = append(issues, normalizeStoreValidationIssue(issue))
	}
	return ValidationError{Issues: issues}
}

func normalizeStoreValidationIssue(issue workflowstore.CompletionValidationIssue) ValidationIssue {
	field := strings.TrimSpace(issue.Field)
	code := strings.TrimSpace(issue.Code)
	message := strings.TrimSpace(issue.Message)
	switch code {
	case "transition_id_required":
		return ValidationIssue{Code: "transition_required", Field: "transition", Message: "transition is required when multiple transitions are available"}
	case "invalid_transition_id":
		return ValidationIssue{Code: "invalid_transition", Field: "transition", Message: "transition is not available in the run-start snapshot"}
	case "no_outgoing_transition":
		return ValidationIssue{Code: code, Field: "transition", Message: "no outgoing transition is available in the run-start snapshot"}
	case "required_output_missing":
		return ValidationIssue{Code: "required_parameter_missing", Field: field, Message: "parameter is required by the selected transition"}
	case "unknown_output_field":
		return ValidationIssue{Code: "unknown_parameter", Field: field, Message: "parameter is not declared by this workflow run"}
	case "output_field_required":
		return ValidationIssue{Code: "parameter_required", Field: field, Message: "parameter name is required"}
	case "output_too_large":
		return ValidationIssue{Code: "parameter_too_large", Field: field, Message: "parameter value is too large"}
	}
	if field == "transition_id" {
		field = "transition"
	}
	return ValidationIssue{Code: code, Field: field, Message: message}
}

func selectedTransition(value string, provided bool, transitions []CompletionTransition) (CompletionTransition, bool, []ValidationIssue) {
	if len(transitions) == 0 {
		return CompletionTransition{}, false, []ValidationIssue{{Code: "no_outgoing_transition", Field: "transition", Message: "no outgoing transition is available for this workflow run"}}
	}
	transitionID := strings.TrimSpace(value)
	if transitionID == "" {
		if len(transitions) == 1 {
			return transitions[0], true, nil
		}
		message := "transition is required when multiple transitions are available"
		if provided {
			message = "transition must be non-empty when multiple transitions are available"
		}
		return CompletionTransition{}, false, []ValidationIssue{{Code: "required_field_missing", Field: "transition", Message: message}}
	}
	for _, transition := range transitions {
		if strings.TrimSpace(transition.ID) == transitionID {
			return transition, true, nil
		}
	}
	return CompletionTransition{}, false, []ValidationIssue{{Code: "invalid_transition", Field: "transition", Message: "transition is not declared by this workflow run"}}
}

func normalizedTransitions(transitions []CompletionTransition) []CompletionTransition {
	out := []CompletionTransition{}
	seen := map[string]bool{}
	for _, transition := range transitions {
		id := strings.TrimSpace(transition.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, CompletionTransition{
			ID:          id,
			DisplayName: strings.TrimSpace(transition.DisplayName),
			Description: strings.TrimSpace(transition.Description),
			Parameters:  normalizedParameters(transition.Parameters),
		})
	}
	return out
}

func normalizedParameters(parameters []workflow.Parameter) []workflow.Parameter {
	out := []workflow.Parameter{}
	seen := map[string]bool{}
	for _, parameter := range parameters {
		key := strings.TrimSpace(parameter.Key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, workflow.Parameter{Key: key, Description: strings.TrimSpace(parameter.Description)})
	}
	return out
}

func schemaParameters(transitions []CompletionTransition) []workflow.Parameter {
	out := []workflow.Parameter{}
	seen := map[string]bool{}
	for _, transition := range transitions {
		for _, parameter := range normalizedParameters(transition.Parameters) {
			key := strings.TrimSpace(parameter.Key)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, workflow.Parameter{Key: key, Description: strings.TrimSpace(parameter.Description)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.TrimSpace(out[i].Key) < strings.TrimSpace(out[j].Key)
	})
	return out
}

func parameterSet(parameters []workflow.Parameter) map[string]bool {
	out := make(map[string]bool, len(parameters))
	for _, parameter := range parameters {
		key := strings.TrimSpace(parameter.Key)
		if key != "" {
			out[key] = true
		}
	}
	return out
}

func sortedTransitionIDs(transitions []CompletionTransition) []string {
	out := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		id := strings.TrimSpace(transition.ID)
		if id != "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
