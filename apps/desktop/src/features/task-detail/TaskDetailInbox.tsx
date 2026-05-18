import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { AttentionItem, TaskDetail, TaskRun } from "../../api";
import { Badge, Button } from "../../ui";
import { ApprovalBox, QuestionBox } from "./TaskDetailAttention";
import type { useTaskMutations } from "./useTaskDetailData";

export function TaskInbox({
  activeRuns,
  currentGraphRevision,
  detail,
  disabled,
  mutations,
  resumeRunId,
}: Readonly<{
  activeRuns: readonly TaskRun[];
  currentGraphRevision: number;
  detail: TaskDetail;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  resumeRunId: string;
}>) {
  const { t } = useTranslation();
  return (
    <section
      aria-label={t("task.inbox")}
      className="grid max-h-[32vh] min-h-[132px] gap-[var(--space-3)] overflow-auto rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-1)] p-[var(--space-3)] hide-scrollbar"
    >
      <div className="flex flex-wrap items-center justify-between gap-[var(--space-2)]">
        <h3 className="m-0">{t("task.inbox")}</h3>
        {detail.attention.length > 0 ? <Badge tone="warning">{detail.attention.length}</Badge> : null}
      </div>
      <TaskActions
        activeRuns={activeRuns}
        detail={detail}
        disabled={disabled}
        mutations={mutations}
        resumeRunId={resumeRunId}
      />
      {detail.attention.length === 0 ? (
        <p className="m-0 text-[var(--color-muted)]">{t("task.noInbox")}</p>
      ) : null}
      {detail.attention.map((item) => (
        <InboxItem
          attention={item}
          currentGraphRevision={currentGraphRevision}
          disabled={disabled}
          key={item.id}
          mutations={mutations}
          taskId={detail.id}
          transitions={detail.transitions}
        />
      ))}
    </section>
  );
}

function InboxItem({
  attention,
  currentGraphRevision,
  disabled,
  mutations,
  taskId,
  transitions,
}: Readonly<{
  attention: AttentionItem;
  currentGraphRevision: number;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  taskId: string;
  transitions: TaskDetail["transitions"];
}>) {
  const { t } = useTranslation();
  if (attention.kind === "question") {
    return <QuestionBox attention={attention} disabled={disabled} mutations={mutations} taskId={taskId} />;
  }
  if (attention.kind === "approval") {
    return (
      <ApprovalBox
        attention={attention}
        currentGraphRevision={currentGraphRevision}
        disabled={disabled}
        mutations={mutations}
        transitions={transitions}
      />
    );
  }
  return (
    <article className="grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-2)] p-[var(--space-3)]">
      <Badge tone="warning">{attention.kind || t("task.inbox")}</Badge>
      <p className="m-0">{attention.message}</p>
    </article>
  );
}

function TaskActions({
  activeRuns,
  detail,
  disabled,
  mutations,
  resumeRunId,
}: Readonly<{
  activeRuns: readonly TaskRun[];
  detail: TaskDetail;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  resumeRunId: string;
}>) {
  const { t } = useTranslation();
  const [confirmCancel, setConfirmCancel] = useState(false);
  const resumeID = resumeRunId.length > 0 ? resumeRunId : detail.actions.resumeRunID;

  return (
    <div className="flex flex-wrap items-center gap-[var(--space-2)]">
      {detail.actions.canResume ? (
        <Button
          disabled={disabled}
          onClick={() => {
            void mutations.resume.mutateAsync(resumeID);
          }}
          variant="primary"
        >
          {t("board.resume")}
        </Button>
      ) : null}
      {activeRuns.map((run) => (
        <Button
          disabled={disabled}
          key={run.id}
          onClick={() => {
            void mutations.interrupt.mutateAsync(run.id);
          }}
          variant="secondary"
        >
          {t("board.interrupt")} <span className="font-mono">{run.id}</span>
        </Button>
      ))}
      {detail.actions.canCancel ? (
        <Button
          disabled={disabled}
          onClick={() => {
            setConfirmCancel(true);
          }}
          variant="danger"
        >
          {t("task.cancel")}
        </Button>
      ) : null}
      {confirmCancel ? (
        <div className="basis-full grid gap-[var(--space-2)] rounded-[var(--radius-l)] border border-[var(--color-outline)] bg-[var(--color-island-2)] p-[var(--space-3)]">
          <strong>{t("task.cancelConfirmTitle")}</strong>
          <p className="m-0">{t("task.cancelConfirmBody")}</p>
          <Button
            disabled={disabled}
            onClick={() => {
              void mutations.cancel.mutateAsync();
            }}
            variant="danger"
          >
            {t("app.confirm")}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
