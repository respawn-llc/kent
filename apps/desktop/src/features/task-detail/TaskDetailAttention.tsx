import { useTranslation } from "react-i18next";

import type { ApprovalAttentionItem, ApprovalSnapshot, InterruptedCurrentNodeAttentionItem } from "@/api";
import { errorMessage } from "@/api";
import { useAppServices } from "@/app-facade";
import { writeClipboardText } from "@/shared/native-clipboard";
import { WorkflowEdgeRouteGraphic } from "@/shared/workflow-edge";
import { Button, Island, showStatusToast } from "@/ui";
import { TaskResumeButton } from "./TaskResumeButton";
import { TaskDetailCopyableValue } from "./TaskDetailCopyableValue";
import { taskDetailIslandRadius } from "./taskDetailIslandStyles";
import type { TaskDetailLifecycle } from "./TaskDetailLifecycleActions";
import { WorkflowParameterOutput, type WorkflowOutputRoot } from "@/shared/workflow-parameter-output";
import { Copy } from "lucide-react";

export { QuestionBox } from "./TaskDetailQuestionForm";

export function ApprovalBox({
  attention,
  mutations,
  outputRoot,
}: Readonly<{
  attention: ApprovalAttentionItem;
  mutations: TaskDetailLifecycle;
  outputRoot: WorkflowOutputRoot;
}>) {
  const { t } = useTranslation();
  const snapshot = attention.approvalSnapshot;
  function approve(): void {
    mutations.approve(attention.approvalID);
  }
  return (
    <>
      <Island
        aria-label={t("task.approval")}
        className="grid gap-[var(--space-2)] p-[var(--space-2)]"
        level={1}
        radius={taskDetailIslandRadius}
        unpadded
      >
        <div className="grid gap-[var(--space-2)]">
          <div
            className="flex min-w-0 items-center gap-[var(--space-2)]"
            data-testid="task-approval-route-action-row"
          >
            <WorkflowEdgeRouteGraphic
              className="-ml-[var(--space-2)]"
              contextMode=""
              layout="compact"
              neutralArrow
              sourceLabel={snapshot.sourceNodeName}
              targetLabel={approvalTargetLabel(snapshot, t)}
            />
            <span className="min-w-0 flex-1" />
            <Button
              className="shrink-0"
              disabled={mutations.approvalPending}
              onClick={approve}
              variant="primary"
            >
              {t("task.approve")}
            </Button>
          </div>
          {snapshot.commentary.length > 0 ? (
            <p className="m-0 whitespace-pre-wrap text-sm text-[var(--color-muted)]">{snapshot.commentary}</p>
          ) : null}
          <ApprovalOutputValues outputValues={snapshot.outputValues} root={outputRoot} />
          {snapshot.choiceAvailability === "unavailable" ? (
            <p className="m-0 text-sm text-[var(--color-warning)]">{t("task.approvalChoiceUnavailable")}</p>
          ) : null}
        </div>
      </Island>
    </>
  );
}

export function InterruptedCurrentNodeBox({
  attention,
  canResume,
}: Readonly<{
  attention: InterruptedCurrentNodeAttentionItem;
  canResume: boolean;
}>) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const detailJSON = attention.detailJSON;
  return (
    <Island
      aria-label={t("task.interrupted")}
      className="grid gap-[var(--space-2)] p-[var(--space-4)]"
      level={1}
      radius={taskDetailIslandRadius}
      unpadded
    >
      <strong>{t("task.interrupted")}</strong>
      {attention.message !== null ? (
        <p className="m-0 text-sm text-[var(--color-muted)]">{attention.message}</p>
      ) : null}
      {detailJSON !== null ? (
        <Button
          onClick={() => {
            void writeClipboardText(detailJSON, nativeBridge)
              .then(() => {
                showStatusToast({
                  id: "task-interruption-detail-copied",
                  title: t("task.interruptionDetailCopied"),
                  tone: "success",
                });
              })
              .catch((cause: unknown) => {
                showStatusToast({
                  id: "task-interruption-detail-copy-failed",
                  title: t("task.interruptionDetailCopyFailed"),
                  body: errorMessage(cause),
                  tone: "danger",
                });
              });
          }}
          variant="secondary"
        >
          {t("task.copyInterruptionDetail")}
        </Button>
      ) : null}
      {canResume ? <TaskResumeButton /> : null}
    </Island>
  );
}

function ApprovalOutputValues({
  outputValues,
  root,
}: Readonly<{ outputValues: Readonly<Record<string, string>>; root: WorkflowOutputRoot }>) {
  const { t } = useTranslation();
  const entries = Object.entries(outputValues);
  if (entries.length === 0) {
    return <p className="m-0 text-sm text-[var(--color-muted)]">{t("app.none")}</p>;
  }
  return (
    <div className="grid gap-[var(--space-2)]">
      {entries.map(([name, value], index) => (
        <div className="grid gap-[var(--space-2)]" key={name}>
          <div className="grid gap-[var(--space-1)]">
            <strong className="text-sm">{name}</strong>
            <WorkflowParameterOutput value={value} root={root} />
            <TaskDetailCopyableValue
              className="justify-self-start"
              clipboardValue={value}
              kind={{ kind: "transition_output", outputName: name }}
            >
              <Copy size={14} />
            </TaskDetailCopyableValue>
          </div>
          {index < entries.length - 1 ? (
            <div className="px-[var(--space-2)]">
              <div className="h-px w-full bg-[var(--color-outline)]" />
            </div>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function approvalTargetLabel(
  snapshot: ApprovalSnapshot,
  fallback: ReturnType<typeof useTranslation>["t"],
): string {
  const labels = snapshot.targets.map((target) => target.displayName.trim());
  return labels.join(", ") || fallback("task.targetUnavailable");
}
