package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/shared/clientui"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"

	tea "github.com/charmbracelet/bubbletea"
)

type transcriptPromptAnswerer struct {
	ctx                   context.Context
	control               promptBatchAnswerer
	connectionOutcomeSink func(error)
	nextGeneration        uint64
}

type promptBatchAnswerer interface {
	AnswerPromptBatch(context.Context, *promptpb.AnswerBatchRequest) (*promptpb.AnswerBatchSuccess, error)
}

type transcriptPromptKey struct {
	sessionID  runtimeids.SessionID
	stepID     runtimeids.StepID
	toolCallID clientui.ToolCallID
}

type activePromptAnswerDelivery struct {
	key        transcriptPromptKey
	generation uint64
	cancel     context.CancelFunc
}

type promptAnswerDeliveryResultMsg struct {
	key        transcriptPromptKey
	generation uint64
	err        error
}

func newTranscriptPromptAnswerer(ctx context.Context, control promptBatchAnswerer) *transcriptPromptAnswerer {
	if ctx == nil || control == nil {
		return nil
	}
	return &transcriptPromptAnswerer{
		ctx: ctx, control: control,
	}
}

func (a *transcriptPromptAnswerer) withConnectionOutcomeSink(sink func(error)) *transcriptPromptAnswerer {
	if a == nil {
		return nil
	}
	copy := *a
	copy.connectionOutcomeSink = sink
	return &copy
}

func (a *transcriptPromptAnswerer) event(prompt *transcriptpb.Prompt) askEvent {
	return askEvent{prompt: cloneTranscriptPromptForAsk(prompt)}
}

func (a *transcriptPromptAnswerer) delivery(
	prompt *transcriptpb.Prompt,
	answer clientui.PromptAnswer,
	answerErr error,
) (*activePromptAnswerDelivery, tea.Cmd, error) {
	if a == nil || a.ctx == nil || a.control == nil {
		return nil, nil, errors.New("prompt answer delivery is unavailable")
	}
	key, err := newTranscriptPromptKey(prompt)
	if err != nil {
		return nil, nil, err
	}
	a.nextGeneration++
	if a.nextGeneration == 0 {
		a.nextGeneration++
	}
	generation := a.nextGeneration
	deliveryCtx, cancel := context.WithCancel(a.ctx)
	active, err := newActivePromptAnswerDelivery(key, generation, cancel)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	submit, err := a.submitter(prompt, clonePromptAnswer(answer), answerErr)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return active, func() tea.Msg {
		err := context.Cause(deliveryCtx)
		if err == nil {
			err = submit(deliveryCtx)
			if a.connectionOutcomeSink != nil {
				a.connectionOutcomeSink(err)
			}
		}
		return promptAnswerDeliveryResultMsg{key: key, generation: generation, err: err}
	}, nil
}

func (a *transcriptPromptAnswerer) submitter(
	prompt *transcriptpb.Prompt,
	answer clientui.PromptAnswer,
	answerErr error,
) (func(context.Context) error, error) {
	entry := &promptpb.AnswerBatchEntry{ToolCallId: transcriptPromptToolCallID(prompt)}
	switch {
	case answerErr != nil:
		entry.Answer = &promptpb.AnswerBatchEntry_Declined{Declined: &promptpb.Declined{}}
	case transcriptPromptIsApproval(prompt) && answer.Approval != nil:
		decision, err := protoapi.ApprovalDecisionToProto(answer.Approval.Decision)
		if err != nil {
			return nil, err
		}
		entry.Answer = &promptpb.AnswerBatchEntry_ApprovalAnswer{ApprovalAnswer: &promptpb.ApprovalAnswer{
			Decision:   decision,
			Commentary: textutil.OptionalExactString(answer.Approval.Commentary),
		}}
	case transcriptPromptIsApproval(prompt):
		return nil, errors.New("approval response is required")
	case answer.Approval != nil:
		return nil, errors.New("question response cannot carry approval answer")
	default:
		question := &promptpb.QuestionAnswer{Freeform: textutil.OptionalExactString(answer.FreeformAnswer)}
		if answer.SelectedOptionNumber != nil {
			selected, err := protoapi.Int32(*answer.SelectedOptionNumber, "selected_option_number")
			if err != nil {
				return nil, err
			}
			question.SelectedOptionNumber = &selected
		}
		entry.Answer = &promptpb.AnswerBatchEntry_QuestionAnswer{QuestionAnswer: question}
	}
	request := &promptpb.AnswerBatchRequest{
		SessionId: transcriptPromptSessionID(prompt),
		StepId:    transcriptPromptStepID(prompt),
		Entries:   []*promptpb.AnswerBatchEntry{entry},
	}
	if err := protoapi.Validate(request); err != nil {
		return nil, fmt.Errorf("convert prompt answer: %w", err)
	}
	return func(ctx context.Context) error {
		_, err := a.control.AnswerPromptBatch(ctx, request)
		return err
	}, nil
}

func newTranscriptPromptKey(prompt *transcriptpb.Prompt) (transcriptPromptKey, error) {
	sessionID, err := runtimeids.ParseSessionID(transcriptPromptSessionID(prompt))
	if err != nil {
		return transcriptPromptKey{}, errors.New("prompt answer session id is required")
	}
	stepID, err := runtimeids.ParseStepID(transcriptPromptStepID(prompt))
	if err != nil {
		return transcriptPromptKey{}, errors.New("prompt answer step id is required")
	}
	rawToolCallID := transcriptPromptToolCallID(prompt)
	if strings.TrimSpace(rawToolCallID) == "" || strings.TrimSpace(rawToolCallID) != rawToolCallID {
		return transcriptPromptKey{}, errors.New("prompt answer tool call id is required without surrounding whitespace")
	}
	return transcriptPromptKey{
		sessionID:  sessionID,
		stepID:     stepID,
		toolCallID: clientui.ToolCallID(rawToolCallID),
	}, nil
}

func newActivePromptAnswerDelivery(
	key transcriptPromptKey,
	generation uint64,
	cancel context.CancelFunc,
) (*activePromptAnswerDelivery, error) {
	if key.sessionID.IsZero() || strings.TrimSpace(string(key.toolCallID)) == "" {
		return nil, errors.New("prompt answer delivery key is required")
	}
	if key.stepID.IsZero() {
		return nil, errors.New("prompt answer delivery step id is required")
	}
	if generation == 0 {
		return nil, errors.New("prompt answer delivery generation is required")
	}
	if cancel == nil {
		return nil, errors.New("prompt answer delivery cancellation is required")
	}
	return &activePromptAnswerDelivery{key: key, generation: generation, cancel: cancel}, nil
}

func (d *activePromptAnswerDelivery) cancelPending() {
	if d != nil {
		d.cancel()
	}
}

func (d *activePromptAnswerDelivery) matches(key transcriptPromptKey, generation uint64) bool {
	return d != nil && d.key == key && d.generation == generation
}

func clonePromptAnswer(answer clientui.PromptAnswer) clientui.PromptAnswer {
	if answer.SelectedOptionNumber != nil {
		selected := *answer.SelectedOptionNumber
		answer.SelectedOptionNumber = &selected
	}
	if answer.Approval != nil {
		approval := *answer.Approval
		answer.Approval = &approval
	}
	return answer
}

func cloneTranscriptPromptForAsk(prompt *transcriptpb.Prompt) *transcriptpb.Prompt {
	return proto.CloneOf(prompt)
}
