import { useId, useState } from "react";
import { useTranslation } from "react-i18next";

import type { AttentionItem, TaskTransition } from "../../api";
import { Button, TextArea } from "../../ui";
import { cx } from "../../ui/classes";
import { usePendingAsks } from "./useTaskDetailData";
import type { useTaskMutations } from "./useTaskDetailData";

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
  const [answer, setAnswer] = useState("");
  const [selectedOption, setSelectedOption] = useState(0);
  const groupName = useId();

  async function submit(): Promise<void> {
    const freeformAnswer = selectedOption === 0 ? answer : "";
    await mutations.answerQuestion.mutateAsync({
      clientRequestID: `gui-question-${attention.askID}-${Date.now().toString()}`,
      taskID: taskId,
      runID: attention.runID,
      askID: attention.askID,
      selectedOptionNumber: selectedOption,
      freeformAnswer,
    });
    setAnswer("");
    setSelectedOption(0);
  }

  return (
    <form
      className="grid gap-[var(--space-3)] rounded-[var(--radius-l)] border border-[var(--color-warning)] bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)] p-[var(--space-3)]"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <h3>{t("task.question")}</h3>
      {question !== undefined && question.length > 0 ? <p>{question}</p> : null}
      <fieldset className="m-0 grid gap-[var(--space-2)] border-0 p-0">
        <legend className="sr-only">{t("task.optionNumber")}</legend>
        {(pendingAsk?.suggestions ?? []).map((suggestion, optionIndex) => (
          <label
            className={cx(
              "rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-2)] text-left text-[var(--color-on-island)]",
              selectedOption === optionIndex + 1 &&
                "border-[var(--color-primary)] bg-[color-mix(in_srgb,var(--color-primary)_14%,transparent)]",
            )}
            key={suggestion}
          >
            <input
              checked={selectedOption === optionIndex + 1}
              className="mr-[var(--space-2)]"
              name={groupName}
              onChange={() => {
                setSelectedOption(optionIndex + 1);
                setAnswer("");
              }}
              type="radio"
            />
            {suggestion} {pendingAsk?.recommendedOptionIndex === optionIndex + 1 ? t("task.recommended") : ""}
          </label>
        ))}
      </fieldset>
      <TextArea
        label={t("task.answer")}
        onChange={(event) => {
          setAnswer(event.target.value);
          if (event.target.value.trim().length > 0) {
            setSelectedOption(0);
          }
        }}
        placeholder={t("task.answerPlaceholder")}
        rows={3}
        value={answer}
      />
      <Button
        disabled={
          disabled ||
          mutations.answerQuestion.isPending ||
          (answer.trim().length === 0 && selectedOption === 0)
        }
        type="submit"
        variant="primary"
      >
        {t("task.submitAnswer")}
      </Button>
    </form>
  );
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
    <section className="grid gap-[var(--space-3)] rounded-[var(--radius-l)] border border-[var(--color-warning)] bg-[color-mix(in_srgb,var(--color-warning)_12%,transparent)] p-[var(--space-3)]">
      <h3>{t("task.approval")}</h3>
      <p>{attention.message}</p>
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
    </section>
  );
}
