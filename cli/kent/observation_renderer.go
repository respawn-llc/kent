package main

import (
	"fmt"
	"io"
	"strings"

	"core/cli/tui"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/serverapi"
)

func writeObservedQuestion(w io.Writer, question serverapi.ObservationQuestion, hint string) {
	if question.Ask != nil {
		fmt.Fprintln(w, question.Ask.Question)
		if len(question.Ask.Suggestions) > 0 {
			fmt.Fprintln(w, questionSuggestionsHeading)
			for i, option := range question.Ask.Suggestions {
				suffix := ""
				if question.Ask.RecommendedOptionIndex != nil && *question.Ask.RecommendedOptionIndex == i+1 {
					suffix = recommendedSuggestionSuffix
				}
				fmt.Fprintf(w, "%d. %s%s\n", i+1, option, suffix)
			}
		}
	} else if question.Approval != nil {
		if len(question.Approval.AccessTargets) > 0 {
			fmt.Fprintln(w, clientui.FormatFileAccessApprovalMarkdown(question.Approval.AccessTargets))
		} else {
			fmt.Fprintln(w, question.Approval.Question)
		}
		fmt.Fprintln(w, questionSuggestionsHeading)
		for i, option := range question.Approval.Options {
			fmt.Fprintf(w, "%d. %s\n", i+1, tui.ApprovalDecisionLabel(option.Decision))
		}
	}
	if strings.TrimSpace(hint) != "" {
		fmt.Fprintf(w, "\nAnswer with: %s\n", hint)
	}
}

func observationQuestionHint(targetArgs []string, question serverapi.ObservationQuestion) string {
	args := append([]string{config.Command, "question", "answer"}, targetArgs...)
	if question.Approval != nil || question.Ask != nil && len(question.Ask.Suggestions) > 0 {
		return commandString(append(args, "--option", "<number>")) + ` [--commentary "optional freeform answer or additions"]`
	}
	return commandString(append(args, "--commentary", "<answer>"))
}

func writeRunWatchResponse(w io.Writer, stderr io.Writer, response *promptpb.LiveWatchSuccess, continueHint string) int {
	if stderr == nil {
		stderr = io.Discard
	}
	if err := protoapi.Validate(response); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var failure *promptpb.LiveWatchFailure
	code := 1
	switch outcome := response.Outcome.Outcome.(type) {
	case *promptpb.LiveWatchOutcome_Question:
		question, err := protoapi.ObservationQuestionFromProto(outcome.Question)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		writeObservedQuestion(w, question, observationQuestionHint([]string{"--session", response.SessionId}, question))
	case *promptpb.LiveWatchOutcome_FinalAnswer:
		emitRunFinalText(w, nil, outcome.FinalAnswer.GetResult(), continueHint)
	case *promptpb.LiveWatchOutcome_NoFinalResult:
		failure = outcome.NoFinalResult
	case *promptpb.LiveWatchOutcome_ExecutionError:
		failure = outcome.ExecutionError
	case *promptpb.LiveWatchOutcome_Interrupted:
		failure = outcome.Interrupted
		code = 130
	}
	if failure != nil {
		fmt.Fprintln(w, failure.Reason)
		if failure.Diagnostic != nil {
			fmt.Fprintln(w, *failure.Diagnostic)
		}
		return code
	}
	return 0
}
