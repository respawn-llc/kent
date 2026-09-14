import { type ReactNode, type RefCallback, useCallback, useEffect, useId, useRef } from "react";
import { useTranslation } from "react-i18next";

import {
  errorMessage,
  type ApprovalDecision,
  type FileAccessTarget,
  type QuestionAttentionItem,
} from "@/api";
import type { QuestionAnswerInput } from "@/api";
import { useTextFieldSubmitShortcut } from "@/app-facade";
import {
  Button,
  RadioGroup,
  PromptOptionRow as QuestionOption,
  PromptAccessTargets,
  showStatusToast,
  StaticMarkdown,
} from "@/ui";
import { cx, fieldInputClassNameForRadius } from "@/ui";
import { approvalDecisionLabel } from "@/shared/prompt-presentation";
import type { QuestionAnswerMutation } from "./TaskDetailQuestionAnswer";
import type { PromptPrimaryControl } from "./PromptPrimaryControlRegistry";
import { taskDetailIslandRadius } from "./taskDetailIslandStyles";
import {
  withApprovalQuestionDecision,
  withOrdinaryQuestionOption,
  withQuestionCommentary,
  type QuestionPresentation,
  type QuestionSelectionState,
} from "./TaskDetailQuestionState";

const neitherRadioValue = "neither";

export function QuestionFormView({
  answerQuestion,
  attention,
  disabled,
  onSelectionStateChange,
  presentation,
  registerPrimaryControl,
  selectionState,
}: Readonly<{
  answerQuestion: QuestionAnswerMutation;
  attention: QuestionAttentionItem;
  disabled: boolean;
  onSelectionStateChange: (selection: QuestionSelectionState) => void;
  presentation: QuestionPresentation;
  registerPrimaryControl?: ((control: PromptPrimaryControl) => () => void) | undefined;
  selectionState: QuestionSelectionState;
}>) {
  if (attention.question.kind === "approval") {
    return (
      <ApprovalQuestionForm
        accessTargets={attention.question.accessTargets}
        answerQuestion={answerQuestion}
        approvalDecisions={attention.question.approvalDecisions}
        attention={attention}
        disabled={disabled}
        onSelectionStateChange={onSelectionStateChange}
        question={presentation.question}
        registerPrimaryControl={registerPrimaryControl}
        selectionState={selectionState}
      />
    );
  }
  return (
    <OrdinaryQuestionForm
      answerQuestion={answerQuestion}
      attention={attention}
      disabled={disabled}
      onSelectionStateChange={onSelectionStateChange}
      question={presentation.question}
      recommendedOption={presentation.recommendedOption}
      registerPrimaryControl={registerPrimaryControl}
      selectionState={selectionState}
      suggestions={presentation.suggestions}
    />
  );
}

function OrdinaryQuestionForm({
  answerQuestion,
  attention,
  disabled,
  onSelectionStateChange,
  question,
  recommendedOption,
  registerPrimaryControl,
  selectionState,
  suggestions,
}: Readonly<{
  answerQuestion: QuestionAnswerMutation;
  attention: QuestionAttentionItem;
  disabled: boolean;
  onSelectionStateChange: (selection: QuestionSelectionState) => void;
  question: string | undefined;
  recommendedOption: number | null;
  registerPrimaryControl?: ((control: PromptPrimaryControl) => () => void) | undefined;
  selectionState: QuestionSelectionState;
  suggestions: readonly string[];
}>) {
  const { t } = useTranslation();
  const selection = selectionState;
  const selectedOption = selection.selectedOption;
  const answer = selection.answer;
  const answerID = useId();
  const primaryControlRef = usePrimaryControlRef(registerPrimaryControl);
  // A real option can submit on its own; otherwise any typed freeform answer is
  // submittable, including freeform-only asks where no option is selected.
  const canSubmit = (selectedOption !== null && selectedOption > 0) || answer.trim().length > 0;
  const interactionDisabled = disabled || answerQuestion.isPending;
  const selectedNeither = selection.provenance === "explicit" && selectedOption === null;
  const radioValue = selectedNeither
    ? neitherRadioValue
    : selectedOption === null
      ? ""
      : suggestionRadioValue(selectedOption);

  async function submit(): Promise<void> {
    await submitQuestionAnswer({
      answerQuestion,
      attention,
      failureTitle: t("states.error"),
      input: () => ({
        kind: "ordinary",
        toolCallID: attention.question.toolCallID,
        sessionID: attention.question.sessionID,
        stepID: attention.question.stepID,
        selectedOptionNumber: selectedOption,
        freeformAnswer: answer,
      }),
      selection,
    });
  }

  return (
    <QuestionFormFrame
      answer={answer}
      answerID={answerID}
      answerRef={suggestions.length === 0 ? primaryControlRef : undefined}
      canSubmit={canSubmit}
      interactionDisabled={interactionDisabled}
      onAnswerChange={(nextAnswer) => {
        onSelectionStateChange(withQuestionCommentary(selection, nextAnswer));
      }}
      onRadioValueChange={(value) => {
        onSelectionStateChange(
          withOrdinaryQuestionOption(selection, selectedOptionFromRadioValue(value, suggestions)),
        );
      }}
      onSubmit={submit}
      optionGroup={
        suggestions.length > 0 ? (
          <>
            {suggestions.map((suggestion, optionIndex) => (
              <QuestionOption
                disabled={interactionDisabled}
                key={`${optionIndex.toString()}:${suggestion}`}
                primaryControlRef={optionIndex === 0 ? primaryControlRef : undefined}
                recommended={recommendedOption === optionIndex + 1}
                text={suggestion}
                value={suggestionRadioValue(optionIndex + 1)}
              />
            ))}
            <QuestionOption
              disabled={interactionDisabled}
              recommended={false}
              text={t("task.neitherOption")}
              value={neitherRadioValue}
            />
          </>
        ) : undefined
      }
      question={question}
      radioValue={radioValue}
      submitting={answerQuestion.isPending}
    />
  );
}

function suggestionRadioValue(optionNumber: number): string {
  return `suggestion:${optionNumber.toString()}`;
}

function selectedOptionFromRadioValue(value: string, suggestions: readonly string[]): number | null {
  if (value === neitherRadioValue) {
    return null;
  }
  const optionIndex = suggestions.findIndex(
    (_suggestion, index) => suggestionRadioValue(index + 1) === value,
  );
  if (optionIndex < 0) {
    throw new Error(`Unknown ordinary-question radio value: ${value}`);
  }
  return optionIndex + 1;
}

function ApprovalQuestionForm({
  accessTargets,
  answerQuestion,
  approvalDecisions,
  attention,
  disabled,
  onSelectionStateChange,
  question,
  registerPrimaryControl,
  selectionState,
}: Readonly<{
  accessTargets: readonly FileAccessTarget[];
  answerQuestion: QuestionAnswerMutation;
  approvalDecisions: readonly ApprovalDecision[];
  attention: QuestionAttentionItem;
  disabled: boolean;
  onSelectionStateChange: (selection: QuestionSelectionState) => void;
  question: string | undefined;
  registerPrimaryControl?: ((control: PromptPrimaryControl) => () => void) | undefined;
  selectionState: QuestionSelectionState;
}>) {
  const { t } = useTranslation();
  const selection = selectionState;
  const selectedDecision = selectedApprovalDecisionFor(approvalDecisions, selection);
  const answer = selection.answer;
  const answerID = useId();
  const primaryControlRef = usePrimaryControlRef(registerPrimaryControl);
  const canSubmit = selectedDecision !== null && (selectedDecision !== "deny" || answer.trim().length > 0);
  const interactionDisabled = disabled || answerQuestion.isPending;

  async function submit(): Promise<void> {
    if (selectedDecision === null) {
      return;
    }
    await submitQuestionAnswer({
      answerQuestion,
      attention,
      failureTitle: t("states.error"),
      input: () => ({
        kind: "approval",
        toolCallID: attention.question.toolCallID,
        sessionID: attention.question.sessionID,
        stepID: attention.question.stepID,
        decision: selectedDecision,
        commentary: answer,
      }),
      selection,
    });
  }

  return (
    <QuestionFormFrame
      accessTargets={accessTargets}
      answer={answer}
      answerID={answerID}
      canSubmit={canSubmit}
      interactionDisabled={interactionDisabled}
      onAnswerChange={(nextAnswer) => {
        onSelectionStateChange(withQuestionCommentary(selection, nextAnswer));
      }}
      onRadioValueChange={(value) => {
        onSelectionStateChange(
          withApprovalQuestionDecision(selection, approvalDecisionForValue(approvalDecisions, value)),
        );
      }}
      onSubmit={submit}
      optionGroup={approvalDecisions.map((decision, decisionIndex) => (
        <QuestionOption
          disabled={interactionDisabled}
          key={decision}
          primaryControlRef={decisionIndex === 0 ? primaryControlRef : undefined}
          recommended={false}
          text={approvalDecisionLabel(decision, t)}
          value={decision}
        />
      ))}
      question={question}
      radioValue={selectedDecision ?? ""}
      submitting={answerQuestion.isPending}
    />
  );
}

function QuestionFormFrame({
  accessTargets,
  answer,
  answerID,
  answerRef,
  canSubmit,
  interactionDisabled,
  onAnswerChange,
  onRadioValueChange,
  onSubmit,
  optionGroup,
  question,
  radioValue,
  submitting,
}: Readonly<{
  accessTargets?: readonly FileAccessTarget[] | undefined;
  answer: string;
  answerID: string;
  answerRef?: RefCallback<HTMLTextAreaElement> | undefined;
  canSubmit: boolean;
  interactionDisabled: boolean;
  onAnswerChange: (answer: string) => void;
  onRadioValueChange: (value: string) => void;
  onSubmit: () => Promise<void>;
  optionGroup?: ReactNode;
  question: string | undefined;
  radioValue: string;
  submitting: boolean;
}>) {
  const { t } = useTranslation();
  const submitDisabled = interactionDisabled || !canSubmit;
  const hasAccessTargets = accessTargets !== undefined && accessTargets.length > 0;
  const formShortcut = useTextFieldSubmitShortcut({
    available: !submitDisabled,
    kind: "form",
  });
  return (
    <form
      className="grid gap-[var(--space-2)]"
      onKeyDown={formShortcut}
      onSubmit={(event) => {
        event.preventDefault();
        if (canSubmit && !interactionDisabled) {
          void onSubmit();
        }
      }}
    >
      {accessTargets === undefined ? null : <PromptAccessTargets targets={accessTargets} />}
      {!hasAccessTargets && question !== undefined && question.length > 0 ? (
        <div className="min-w-0 text-[var(--color-on-island)]">
          <StaticMarkdown value={question} />
        </div>
      ) : null}
      {optionGroup === undefined ? null : (
        <fieldset className="m-0 border-0 p-0">
          <legend className="sr-only">{t("task.optionNumber")}</legend>
          <RadioGroup
            aria-label={t("task.optionNumber")}
            disabled={interactionDisabled}
            onValueChange={onRadioValueChange}
            value={radioValue}
          >
            {optionGroup}
          </RadioGroup>
        </fieldset>
      )}
      <textarea
        aria-label={t("task.commentary")}
        className={cx(fieldInputClassNameForRadius(taskDetailIslandRadius), "min-h-24")}
        disabled={interactionDisabled}
        id={answerID}
        onChange={(event) => {
          onAnswerChange(event.target.value);
        }}
        placeholder={t("task.answerPlaceholder")}
        ref={answerRef}
        rows={3}
        value={answer}
      />
      <Button aria-busy={submitting} disabled={submitDisabled} type="submit" variant="primary">
        {submitting ? t("task.submittingAnswer") : t("task.submitAnswer")}
      </Button>
    </form>
  );
}

function usePrimaryControlRef(
  register: ((control: PromptPrimaryControl) => () => void) | undefined,
): RefCallback<HTMLElement> {
  const unregisterRef = useRef<(() => void) | undefined>(undefined);
  useEffect(
    () => () => {
      unregisterRef.current?.();
      unregisterRef.current = undefined;
    },
    [],
  );
  return useCallback(
    (element) => {
      unregisterRef.current?.();
      unregisterRef.current = undefined;
      if (element !== null && register !== undefined) {
        unregisterRef.current = register({
          focusPrimary(options) {
            element.focus(options);
          },
        });
      }
    },
    [register],
  );
}

async function submitQuestionAnswer({
  answerQuestion,
  attention,
  failureTitle,
  input,
  selection,
}: Readonly<{
  answerQuestion: QuestionAnswerMutation;
  attention: QuestionAttentionItem;
  failureTitle: string;
  input: () => QuestionAnswerInput;
  selection: QuestionSelectionState;
}>): Promise<void> {
  try {
    await answerQuestion.mutateAsync(input(), { attention, selection });
  } catch (error: unknown) {
    showStatusToast({
      body: errorMessage(error),
      id: `task-question-answer-failed:${attention.question.toolCallID}`,
      title: failureTitle,
      tone: "danger",
    });
  }
}

function selectedApprovalDecisionFor(
  decisions: readonly ApprovalDecision[],
  selection: QuestionSelectionState,
): ApprovalDecision | null {
  return approvalDecisionForValue(decisions, selection.approvalDecision);
}

function approvalDecisionForValue(
  decisions: readonly ApprovalDecision[],
  value: string | null,
): ApprovalDecision | null {
  if (value === null) {
    return null;
  }
  return decisions.find((decision) => decision === value) ?? null;
}
