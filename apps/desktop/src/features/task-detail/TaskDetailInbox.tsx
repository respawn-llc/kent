import { useTranslation } from "react-i18next";

import type { AttentionItem, TaskDetail } from "../../api";
import { Badge, Island } from "../../ui";
import { ApprovalBox, QuestionBox } from "./TaskDetailAttention";
import type { useTaskMutations } from "./useTaskDetailData";

export function TaskInbox({
  currentVersion,
  detail,
  disabled,
  mutations,
}: Readonly<{
  currentVersion: number;
  detail: TaskDetail;
  disabled: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
}>) {
  const { t } = useTranslation();
  return (
    <Island aria-label={t("task.inbox")} className="grid gap-[var(--space-3)]">
      <div className="flex flex-wrap items-center justify-between gap-[var(--space-2)]">
        <h3 className="m-0">{t("task.inbox")}</h3>
        {detail.attention.length > 0 ? <Badge tone="warning">{detail.attention.length}</Badge> : null}
      </div>
      {detail.attention.map((item) => (
        <InboxItem
          attention={item}
          currentVersion={currentVersion}
          disabled={disabled}
          key={item.id}
          mutations={mutations}
          taskId={detail.id}
          transitions={detail.transitions}
        />
      ))}
    </Island>
  );
}

function InboxItem({
  attention,
  currentVersion,
  disabled,
  mutations,
  taskId,
  transitions,
}: Readonly<{
  attention: AttentionItem;
  currentVersion: number;
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
        currentVersion={currentVersion}
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
