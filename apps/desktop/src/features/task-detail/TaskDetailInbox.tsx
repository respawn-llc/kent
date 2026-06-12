import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

import type { AttentionItem, TaskDetail } from "../../api";
import { Island } from "../../ui";
import { ApprovalBox, QuestionBox } from "./TaskDetailAttention";
import type { useTaskMutations } from "./useTaskDetailData";

export function TaskInbox({
  currentVersion,
  detail,
  disabled,
  focusFirstQuestion = false,
  mutations,
}: Readonly<{
  currentVersion: number;
  detail: TaskDetail;
  disabled: boolean;
  focusFirstQuestion?: boolean | undefined;
  mutations: ReturnType<typeof useTaskMutations>;
}>) {
  const firstQuestionID = focusFirstQuestion
    ? (detail.attention.find((item) => item.kind === "question")?.id ?? "")
    : "";
  return (
    <>
      {detail.attention.map((item) => (
        <InboxItem
          attention={item}
          currentVersion={currentVersion}
          disabled={disabled}
          focusOnMount={item.id === firstQuestionID}
          key={item.id}
          mutations={mutations}
          taskId={detail.id}
          transitions={detail.transitions}
        />
      ))}
    </>
  );
}

function InboxItem({
  attention,
  currentVersion,
  disabled,
  focusOnMount,
  mutations,
  taskId,
  transitions,
}: Readonly<{
  attention: AttentionItem;
  currentVersion: number;
  disabled: boolean;
  focusOnMount: boolean;
  mutations: ReturnType<typeof useTaskMutations>;
  taskId: string;
  transitions: TaskDetail["transitions"];
}>) {
  const { t } = useTranslation();
  const focusTargetRef = useRef<HTMLDivElement | null>(null);
  const scrolledRef = useRef(false);

  useEffect(() => {
    if (!focusOnMount || scrolledRef.current) {
      return;
    }
    scrolledRef.current = true;
    const cancelScroll = scheduleScroll(() => {
      focusTargetRef.current?.scrollIntoView({ block: "start", behavior: "auto" });
    });
    return () => {
      cancelScroll();
    };
  }, [focusOnMount]);

  if (attention.kind === "question") {
    return (
      <div ref={focusTargetRef}>
        <QuestionBox attention={attention} disabled={disabled} mutations={mutations} taskId={taskId} />
      </div>
    );
  }
  if (attention.kind === "approval") {
    return (
      <div ref={focusTargetRef}>
        <ApprovalBox
          attention={attention}
          currentVersion={currentVersion}
          disabled={disabled}
          mutations={mutations}
          transitions={transitions}
        />
      </div>
    );
  }
  return (
    <div ref={focusTargetRef}>
      <Island aria-label={attention.kind || t("task.inbox")} className="grid gap-[var(--space-2)]">
        <h3 className="m-0">{attention.kind || t("task.inbox")}</h3>
        <p className="m-0">{attention.message}</p>
      </Island>
    </div>
  );
}

function scheduleScroll(callback: () => void): () => void {
  if (typeof window !== "undefined" && typeof window.requestAnimationFrame === "function") {
    const frame = window.requestAnimationFrame(callback);
    return () => {
      window.cancelAnimationFrame(frame);
    };
  }
  const timeout = setTimeout(callback, 0);
  return () => {
    clearTimeout(timeout);
  };
}
