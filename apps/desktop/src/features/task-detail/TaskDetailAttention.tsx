import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import type { AttentionItem, TaskTransition } from "../../api";
import { Button, Island, RadioGroup, RadioGroupItem } from "../../ui";
import { fieldInputClassName } from "../../ui/Field";
import { cx } from "../../ui/classes";
import { usePendingAsks } from "./useTaskDetailData";
import type { useTaskMutations } from "./useTaskDetailData";

type QuestionSelectionState = Readonly<{
  answer: string;
  askID: string;
  selectedOption: number | null;
  submitted: boolean;
  userSelected: boolean;
}>;

const emptySuggestions: readonly string[] = [];

export function QuestionBox({
  attention,
  disabled,
  mutations,
  taskId,
}: Readonly<{
  attention: AttentionItem;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  taskId: string;
}>) {
  const { t } = useTranslation();
  const asks = usePendingAsks(attention.sessionID);
  const pendingAsk = asks.data?.find((ask) => ask.askID === attention.askID);
  const question = attention.message.length > 0 ? attention.message : pendingAsk?.question;
  const suggestions = attention.suggestions.length > 0 ? attention.suggestions : pendingAsk?.suggestions ?? emptySuggestions;
  const recommendedOptionSource =
    attention.suggestions.length > 0 ? attention.recommendedOptionIndex : pendingAsk?.recommendedOptionIndex ?? 0;
  const recommendedOption = recommendedOptionNumber(suggestions, recommendedOptionSource);

  return (
    <Island aria-label={t("task.question")}>
      <QuestionForm
        answerQuestion={mutations.answerQuestion}
        attention={attention}
        disabled={disabled}
        question={question}
        recommendedOption={recommendedOption}
        suggestions={suggestions}
        taskId={taskId}
      />
    </Island>
  );
}

function QuestionForm({
  answerQuestion,
  attention,
  disabled,
  question,
  recommendedOption,
  suggestions,
  taskId,
}: Readonly<{
  answerQuestion: ReturnType<typeof useTaskMutations>["answerQuestion"];
  attention: AttentionItem;
  disabled: boolean;
  question: string | undefined;
  recommendedOption: number | null;
  suggestions: readonly string[];
  taskId: string;
}>) {
  const { t } = useTranslation();
  const [selectionState, setSelectionState] = useState<QuestionSelectionState>(() =>
    emptyQuestionSelection(attention.askID),
  );
  const selection = selectionForAsk(selectionState, attention.askID);
  const selectedOption = selection.userSelected ? selection.selectedOption : recommendedOption;
  const answer = selection.answer;
  const answerID = useId();
  const canSubmit = selectedOption === null ? false : selectedOption > 0 || answer.trim().length > 0;
  const interactionDisabled = disabled || answerQuestion.isPending || selection.submitted;
  const submitDisabled = interactionDisabled || !canSubmit;
  const radioValue = selectedOption === null ? "" : selectedOption.toString();

  async function submit(): Promise<void> {
    const selectedOptionNumber = selectedOption ?? 0;
    await answerQuestion.mutateAsync({
      clientRequestID: `gui-question-${attention.askID}-${Date.now().toString()}`,
      taskID: taskId,
      runID: attention.runID,
      askID: attention.askID,
      selectedOptionNumber,
      freeformAnswer: answer,
    });
    setSelectionState({ answer: "", askID: attention.askID, selectedOption: null, submitted: true, userSelected: true });
  }

  return (
    <form
      className="grid gap-[var(--space-3)]"
      onSubmit={(event) => {
        event.preventDefault();
        if (canSubmit && !interactionDisabled) {
          void submit();
        }
      }}
    >
      {question !== undefined && question.length > 0 ? <p className="m-0">{question}</p> : null}
      <fieldset className="m-0 border-0 p-0">
        <legend className="sr-only">{t("task.optionNumber")}</legend>
        <RadioGroup
          aria-label={t("task.optionNumber")}
          disabled={interactionDisabled}
          onValueChange={(value) => {
            const nextOption = Number(value);
            setSelectionState({
              answer,
              askID: attention.askID,
              selectedOption: nextOption,
              submitted: false,
              userSelected: true,
            });
          }}
          value={radioValue}
        >
          {suggestions.map((suggestion, optionIndex) => (
            <QuestionOption
              disabled={interactionDisabled}
              key={`${optionIndex.toString()}:${suggestion}`}
              recommended={recommendedOption === optionIndex + 1}
              text={suggestion}
              value={(optionIndex + 1).toString()}
            />
          ))}
          <QuestionOption
            disabled={interactionDisabled}
            recommended={false}
            text={t("task.neitherOption")}
            value="0"
          />
        </RadioGroup>
      </fieldset>
      <textarea
        aria-label={t("task.commentary")}
        className={cx(fieldInputClassName, "min-h-24")}
        disabled={interactionDisabled}
        id={answerID}
        onChange={(event) => {
          setSelectionState({
            answer: event.target.value,
            askID: attention.askID,
            selectedOption,
            submitted: false,
            userSelected: selection.userSelected,
          });
        }}
        placeholder={t("task.answerPlaceholder")}
        rows={3}
        value={answer}
      />
      <Button disabled={submitDisabled} type="submit" variant="primary">
        {t("task.submitAnswer")}
      </Button>
    </form>
  );
}

function QuestionOption({
  disabled,
  recommended,
  text,
  value,
}: Readonly<{
  disabled: boolean;
  recommended: boolean;
  text: string;
  value: string;
}>) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <div
      className={cx(
        "flex items-start gap-[var(--space-2)] text-left text-[var(--color-on-island)]",
        disabled && "opacity-60",
      )}
    >
      <RadioGroupItem className="mt-1" disabled={disabled} id={id} value={value} />
      <label
        className={cx("min-w-0 flex-1 cursor-pointer", recommended && "font-bold text-[var(--color-primary)]")}
        htmlFor={id}
      >
        {text}
        {recommended ? <span className="ml-[var(--space-2)] text-xs font-bold">({t("task.recommended")})</span> : null}
      </label>
    </div>
  );
}

function recommendedOptionNumber(suggestions: readonly string[], recommendedOptionIndex: number): number | null {
  return recommendedOptionIndex >= 1 && recommendedOptionIndex <= suggestions.length ? recommendedOptionIndex : null;
}

function emptyQuestionSelection(askID: string): QuestionSelectionState {
  return { answer: "", askID, selectedOption: null, submitted: false, userSelected: false };
}

function selectionForAsk(selection: QuestionSelectionState, askID: string): QuestionSelectionState {
  return selection.askID === askID ? selection : emptyQuestionSelection(askID);
}

export function ApprovalBox({
  attention,
  currentVersion,
  disabled,
  mutations,
  transitions,
}: Readonly<{
  attention: AttentionItem;
  currentVersion: number;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  transitions: readonly TaskTransition[];
}>) {
  const { t } = useTranslation();
  const transition = transitions.find((item) => item.id === attention.taskTransitionID);
  const stale = transition !== undefined && transition.version !== currentVersion;
  return (
    <Island aria-label={t("task.approval")} className="grid gap-[var(--space-3)]">
      <h3 className="m-0">{t("task.approval")}</h3>
      <p className="m-0">{attention.message}</p>
      {transition !== undefined ? (
        <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-[var(--space-3)] gap-y-[var(--space-2)]">
          <dt>{t("task.approvalSnapshot")}</dt>
          <dd>
            {transition.sourceNodeName} · {transition.transitionName || transition.transitionID}
          </dd>
          {transition.edges.length > 0 ? (
            <>
              <dt>{t("task.targetNodes")}</dt>
              <dd>{transition.edges.map((edge) => edge.targetNodeName).join(", ")}</dd>
            </>
          ) : null}
          {transition.commentary.length > 0 ? (
            <>
              <dt>{t("task.commentary")}</dt>
              <dd>{transition.commentary}</dd>
            </>
          ) : null}
          <dt>{t("task.outputValues")}</dt>
          <dd>
            {Object.entries(transition.outputValues)
              .map(([key, value]) => `${key}: ${value}`)
              .join("\n") || t("app.none")}
          </dd>
          <dt>{t("app.version")}</dt>
          <dd>{transition.version}</dd>
          {stale ? (
            <>
              <dt>{t("task.staleApproval")}</dt>
              <dd>{t("task.staleApprovalBody")}</dd>
            </>
          ) : null}
        </dl>
      ) : (
        <p>{t("task.unavailableSnapshot")}</p>
      )}
      <Button
        disabled={disabled || mutations.approve.isPending}
        onClick={() => void mutations.approve.mutateAsync(attention.taskTransitionID)}
        variant="primary"
      >
        {t("task.approve")}
      </Button>
    </Island>
  );
}
