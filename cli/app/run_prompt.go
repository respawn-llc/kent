package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/cli/app/internal/startupconfig"
	"core/shared/apicontract"
	"core/shared/protoapi"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/durationpb"
)

type RunPromptResult struct {
	Identity          *runtimeids.SessionIdentity
	SessionName       *string
	Outcome           RunPromptOutcome
	Duration          time.Duration
	Warnings          []string
	AdmissionWarnings []string
}

func (r RunPromptResult) AllWarnings() []string {
	return append(append([]string(nil), r.Warnings...), r.AdmissionWarnings...)
}

type RunPromptOutcome interface {
	runPromptOutcome()
}

type RunPromptFinalAnswer struct {
	Text string
}

func (RunPromptFinalAnswer) runPromptOutcome() {}

type RunPromptPendingQuestion struct {
	Question serverapi.ObservationQuestion
}

func (RunPromptPendingQuestion) runPromptOutcome() {}

func runPrompt(ctx context.Context, client apicontract.RunPromptService, opts Options, initialSessionID, prompt string, timeout time.Duration, progress serverapi.RunPromptProgressSink) (result RunPromptResult, err error) {
	collector := &runAdmissionCollector{progress: progress}
	defer func() {
		var warningErr error
		result.AdmissionWarnings, warningErr = collector.warnings(opts.RunAgentFlag)
		err = errors.Join(err, warningErr)
	}()
	intent, err := runPromptLaunchIntent(opts, initialSessionID)
	if err != nil {
		return RunPromptResult{}, err
	}
	callerSessionID := runPromptCallerSessionID(opts)
	overrides, err := protoapi.RunPromptOverridesToProto(runPromptOverridesFromOptions(opts))
	if err != nil {
		return RunPromptResult{}, err
	}
	request := &runpromptpb.Request{
		Intent:          intent,
		CallerSessionId: callerSessionID,
		Prompt:          prompt,
		Overrides:       overrides,
	}
	if timeout != 0 {
		request.Timeout = durationpb.New(timeout)
	}
	response, err := client.RunPrompt(ctx, request, collector)
	var denied *serverapi.SubagentLaunchDeniedError
	if callerSessionID != nil && errors.As(err, &denied) && denied.Kind == serverapi.SubagentLaunchDenialCallerMissing {
		err = startupconfig.WorkspaceContextSessionError(*callerSessionID, err)
	}
	if response == nil && err != nil {
		return RunPromptResult{}, err
	}
	warnings, warningErr := runPromptWarnings(response)
	identity := protoapi.SessionIdentityFromProto(response.SessionId, response.ShortId)
	if err != nil && response.GetOutcome() == nil {
		return RunPromptResult{
			Identity:    &identity,
			SessionName: response.SessionName,
			Duration:    response.Duration.AsDuration(),
			Warnings:    warnings,
		}, errors.Join(err, warningErr)
	}
	if warningErr != nil {
		return RunPromptResult{}, errors.Join(err, warningErr)
	}
	var outcome RunPromptOutcome
	switch selected := response.GetOutcome().(type) {
	case *runpromptpb.Success_FinalAnswer:
		outcome = RunPromptFinalAnswer{Text: selected.FinalAnswer}
	case *runpromptpb.Success_Question:
		question, conversionErr := protoapi.ObservationQuestionFromProto(selected.Question)
		if conversionErr != nil {
			return RunPromptResult{}, errors.Join(err, conversionErr)
		}
		outcome = RunPromptPendingQuestion{Question: question}
	default:
		return RunPromptResult{}, errors.Join(err, errors.New("Run response has no outcome"))
	}
	result = RunPromptResult{
		Identity:    &identity,
		SessionName: response.SessionName,
		Outcome:     outcome,
		Duration:    response.Duration.AsDuration(),
		Warnings:    warnings,
	}
	return result, err
}

func runPromptWarnings(response *runpromptpb.Success) ([]string, error) {
	warnings := append([]string(nil), response.Warnings...)
	for _, warning := range response.SelectionWarnings {
		switch warning {
		case runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_AGENT_IGNORED_TO_PRESERVE_CACHE:
			warnings = append(warnings, "Warning: your agent selection was ignored to preserve the cache of an existing session. Start a new session or compact to change the agent.")
		case runpromptpb.RunSelectionWarning_RUN_SELECTION_WARNING_MODEL_IGNORED_TO_PRESERVE_CACHE:
			warnings = append(warnings, "Warning: your model selection was ignored to preserve the cache of an existing session. Start a new session or compact to change the model.")
		default:
			return nil, fmt.Errorf("Run response contains unsupported selection warning %d", warning)
		}
	}
	return warnings, nil
}

func runPromptCallerSessionID(opts Options) *string {
	sessionID := strings.TrimSpace(opts.WorkspaceContextSessionID)
	if sessionID == "" {
		return nil
	}
	return &sessionID
}

func runPromptLaunchIntent(opts Options, initialSessionID string) (*sessionlaunchpb.SessionLaunchRequestIntent, error) {
	if initialSessionID != "" {
		sessionID, err := runtimeids.ParseSessionSelector(initialSessionID)
		if err != nil {
			return nil, err
		}
		return &sessionlaunchpb.SessionLaunchRequestIntent{Intent: &sessionlaunchpb.SessionLaunchRequestIntent_OpenExistingSessionId{OpenExistingSessionId: protoapi.SessionSelectorToProto(sessionID)}}, nil
	}
	if rawParentID := strings.TrimSpace(opts.WorkspaceContextSessionID); rawParentID != "" {
		parsedParentID, err := runtimeids.ParseSessionID(rawParentID)
		if err != nil {
			return nil, err
		}
		return protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.ParentAgentSessionCreateOrigin(parsedParentID)))
	}
	return protoapi.SessionLaunchRequestIntentToProto(serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()))
}

func runPromptOverridesFromOptions(opts Options) serverapi.RunPromptOverrides {
	var agentRole *string
	if opts.AgentRole != nil {
		value := strings.TrimSpace(*opts.AgentRole)
		agentRole = &value
	}
	return serverapi.RunPromptOverrides{
		AgentRole:           agentRole,
		Model:               strings.TrimSpace(opts.Model),
		ThinkingLevel:       strings.TrimSpace(opts.ThinkingLevel),
		Theme:               strings.TrimSpace(opts.Theme),
		ModelTimeoutSeconds: opts.ModelTimeoutSeconds,
		Tools:               strings.TrimSpace(opts.Tools),
	}
}
