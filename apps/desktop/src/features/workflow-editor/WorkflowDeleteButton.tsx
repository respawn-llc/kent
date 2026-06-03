/* eslint-disable react-refresh/only-export-components -- The reusable delete launcher hook intentionally shares the editor's delete flow with picker menu actions. */
import { useCallback, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Trash2 } from "lucide-react";

import { errorMessage } from "../../api/errors";
import type { WorkflowDeleteImpact, WorkflowDeleteInput } from "../../api";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { useNativeDialogFallback } from "../../app/useNativeDialogFallback";
import { useStatusController } from "../../app/useStatusController";
import { Button, Dialog, NativeDialogWindow } from "../../ui";

const workflowDeleteNativeDialogPath = "/native-dialog/workflow-delete";
const workflowDeleteDialogWidth = 460;

type WorkflowDeleteTarget = Readonly<{
  impact: WorkflowDeleteImpact;
}>;

export function WorkflowDeleteButton({ workflowID }: Readonly<{ workflowID: string }>) {
  const { t } = useTranslation();
  const deleteLauncher = useWorkflowDeleteLauncher(workflowID);

  return (
    <>
      {deleteLauncher.fallback}
      <Button
        aria-label={t("workflowEditor.workflowDelete")}
        className="justify-self-end"
        disabled={deleteLauncher.disabled}
        onClick={() => {
          void deleteLauncher.openWorkflowDelete();
        }}
        size="icon"
        title={t("workflowEditor.workflowDelete")}
        variant="danger"
      >
        <Trash2 aria-hidden="true" className="block" size={18} strokeWidth={1.5} />
      </Button>
    </>
  );
}

export function useWorkflowDeleteLauncher(workflowID: string): Readonly<{
  disabled: boolean;
  fallback: ReactNode;
  openWorkflowDelete: () => Promise<void>;
  opening: boolean;
}> {
  const { t } = useTranslation();
  const { api, nativeBridge } = useAppServices();
  const connection = useConnectionSnapshot();
  const { push } = useStatusController();
  const [opening, setOpening] = useState(false);
  const [fallbackSubmitting, setFallbackSubmitting] = useState(false);
  const fallbackSubmittingRef = useRef(false);
  const disabled = connection.phase !== "connected" || opening;

  const confirmDelete = useCallback(
    async (impact: WorkflowDeleteImpact, close: () => void): Promise<void> => {
      try {
        const response = await api.deleteWorkflow(workflowDeleteInputFromImpact(impact));
        if (!response.deleted) {
          push({
            id: "workflow-delete-blocked",
            tone: "danger",
            title: t("workflowEditor.workflowDeleteBlocked"),
            body: workflowDeleteBlockersMessage(response.blockers, t("workflowEditor.workflowDeleteBlocked")),
          });
          return;
        }
        close();
        await nativeBridge.workflowDeletion.notifyDeleted({ workflowID: impact.workflowID });
      } catch (error) {
        push({
          id: "workflow-delete-error",
          tone: "danger",
          title: t("workflowEditor.workflowDeleteTitle"),
          body: errorMessage(error),
        });
      }
    },
    [api, nativeBridge.workflowDeletion, push, t],
  );

  const confirmFallbackDelete = useCallback(
    async (impact: WorkflowDeleteImpact, close: () => void): Promise<void> => {
      if (fallbackSubmittingRef.current) {
        return;
      }
      fallbackSubmittingRef.current = true;
      setFallbackSubmitting(true);
      try {
        await confirmDelete(impact, close);
      } finally {
        fallbackSubmittingRef.current = false;
        setFallbackSubmitting(false);
      }
    },
    [confirmDelete],
  );

  const deleteDialog = useNativeDialogFallback<WorkflowDeleteTarget>({
    errorNoticeID: "workflow-delete-window-error",
    errorTitle: t("workflowEditor.workflowDeleteWindowError"),
    nativeAvailable: nativeBridge.capabilities.dialogWindows,
    openNative: async (target) => {
      await nativeBridge.dialogs.openWindow(
        workflowDeleteWindowOptions(target.impact, t("workflowEditor.workflowDeleteTitle")),
      );
    },
    renderFallback: (target, close) => (
      <WorkflowDeleteFallbackDialog
        disabled={fallbackSubmitting}
        impact={target.impact}
        onCancel={close}
        onConfirm={() => void confirmFallbackDelete(target.impact, close)}
      />
    ),
  });

  const openWorkflowDelete = useCallback(
    async () =>
      openWorkflowDeleteConfirmation({
        open: deleteDialog.open,
        preview: async (id) => api.previewWorkflowDelete(id),
        setOpening,
        pushError: (message) => {
          push({
            id: "workflow-delete-preview-error",
            tone: "danger",
            title: t("workflowEditor.workflowDeleteTitle"),
            body: message,
          });
        },
        workflowID,
      }),
    [api, deleteDialog.open, push, t, workflowID],
  );

  return {
    disabled,
    fallback: deleteDialog.fallback,
    openWorkflowDelete,
    opening,
  };
}

export function WorkflowDeleteWindowRoute({ impact }: WorkflowDeleteTarget) {
  const { t } = useTranslation();
  const { api, nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const [actionError, setActionError] = useState("");
  const [committed, setCommitted] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const confirmDelete = useCallback(async (): Promise<void> => {
    if (submittingRef.current || committed) {
      return;
    }
    submittingRef.current = true;
    setActionError("");
    setSubmitting(true);
    try {
      const response = await api.deleteWorkflow(workflowDeleteInputFromImpact(impact));
      if (!response.deleted) {
        setActionError(
          workflowDeleteBlockersMessage(response.blockers, t("workflowEditor.workflowDeleteBlocked")),
        );
        submittingRef.current = false;
        setSubmitting(false);
        return;
      }
      setCommitted(true);
      try {
        await nativeBridge.workflowDeletion.notifyDeleted({ workflowID: impact.workflowID });
      } catch (error) {
        const message = t("workflowEditor.workflowDeleteCommittedNotifyError", {
          message: errorMessage(error),
        });
        setActionError(message);
        push({
          id: "workflow-delete-committed-notify-error",
          tone: "warning",
          title: t("workflowEditor.workflowDeleteWindowError"),
          body: message,
        });
        submittingRef.current = false;
        setSubmitting(false);
        return;
      }
      try {
        await nativeBridge.window.closeCurrent();
      } catch (error) {
        const message = t("workflowEditor.workflowDeleteCommittedCloseError", {
          message: errorMessage(error),
        });
        setActionError(message);
        push({
          id: "workflow-delete-committed-close-error",
          tone: "warning",
          title: t("workflowEditor.workflowDeleteWindowError"),
          body: message,
        });
        submittingRef.current = false;
        setSubmitting(false);
      }
    } catch (error) {
      setActionError(errorMessage(error));
      push({
        id: "workflow-delete-window-error",
        tone: "danger",
        title: t("workflowEditor.workflowDeleteWindowError"),
        body: errorMessage(error),
      });
      submittingRef.current = false;
      setSubmitting(false);
    }
  }, [api, committed, impact, nativeBridge.workflowDeletion, nativeBridge.window, push, t]);

  return (
    <NativeDialogWindow
      contentMaxWidth={`${workflowDeleteDialogWidth.toString()}px`}
      title={t("workflowEditor.workflowDeleteTitle")}
    >
      <WorkflowDeleteConfirmationContent
        actionError={actionError}
        committed={committed}
        disabled={submitting}
        impact={impact}
        onCancel={() => {
          void nativeBridge.window.closeCurrent();
        }}
        onConfirm={() => void confirmDelete()}
      />
    </NativeDialogWindow>
  );
}

function WorkflowDeleteFallbackDialog({
  impact,
  disabled,
  onCancel,
  onConfirm,
}: Readonly<{
  impact: WorkflowDeleteImpact;
  disabled: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}>) {
  const { t } = useTranslation();
  return (
    <Dialog
      closeLabel={t("app.close")}
      onClose={onCancel}
      open
      style={{ width: `min(${workflowDeleteDialogWidth.toString()}px, calc(100vw - 32px))` }}
      title={t("workflowEditor.workflowDeleteTitle")}
    >
      <WorkflowDeleteConfirmationContent
        disabled={disabled}
        impact={impact}
        onCancel={onCancel}
        onConfirm={onConfirm}
      />
    </Dialog>
  );
}

function WorkflowDeleteConfirmationContent({
  actionError,
  committed = false,
  disabled = false,
  impact,
  onCancel,
  onConfirm,
}: Readonly<{
  actionError?: string | undefined;
  committed?: boolean | undefined;
  disabled?: boolean | undefined;
  impact: WorkflowDeleteImpact;
  onCancel: () => void;
  onConfirm: () => void;
}>) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-[var(--space-3)]">
      <p className="m-0 text-sm text-[var(--color-on-island)]">{t("workflowEditor.workflowDeleteBody")}</p>
      {actionError === undefined || actionError.length === 0 ? null : (
        <p className="m-0 whitespace-pre-wrap text-sm text-[var(--color-error)]">{actionError}</p>
      )}
      <ul className="m-0 grid gap-[var(--space-1)] p-0 text-sm text-[var(--color-muted)]">
        <li className="list-none">
          {t("workflowEditor.workflowDeleteProjects", { count: impact.projectCount })}
        </li>
        <li className="list-none">{t("workflowEditor.workflowDeleteLinks", { count: impact.linkCount })}</li>
        <li className="list-none">{t("workflowEditor.workflowDeleteTasks", { count: impact.taskCount })}</li>
      </ul>
      {committed ? (
        <Button className="justify-self-end" onClick={onCancel}>
          {t("app.close")}
        </Button>
      ) : (
        <div className="grid grid-cols-2 gap-[var(--space-2)]">
          <Button className="w-full" disabled={disabled} onClick={onCancel}>
            {t("app.cancel")}
          </Button>
          <Button className="w-full" disabled={disabled} onClick={onConfirm} variant="danger">
            {t("workflowEditor.workflowDeleteConfirm")}
          </Button>
        </div>
      )}
    </div>
  );
}

async function openWorkflowDeleteConfirmation({
  open,
  preview,
  setOpening,
  pushError,
  workflowID,
}: Readonly<{
  open: (target: WorkflowDeleteTarget) => Promise<void>;
  preview: (workflowID: string) => Promise<WorkflowDeleteImpact>;
  setOpening: (opening: boolean) => void;
  pushError: (message: string) => void;
  workflowID: string;
}>): Promise<void> {
  setOpening(true);
  try {
    await open({ impact: await preview(workflowID) });
  } catch (error) {
    pushError(errorMessage(error));
  } finally {
    setOpening(false);
  }
}

function workflowDeleteInputFromImpact(impact: WorkflowDeleteImpact): WorkflowDeleteInput {
  return {
    confirmed: true,
    expectedLinkCount: impact.linkCount,
    expectedProjectCount: impact.projectCount,
    expectedTaskCount: impact.taskCount,
    expectedVersion: impact.version,
    workflowID: impact.workflowID,
  };
}

function workflowDeleteBlockersMessage(blockers: readonly { message: string }[], fallback: string): string {
  const messages = blockers.map((blocker) => blocker.message).filter((message) => message.length > 0);
  return messages.length === 0 ? fallback : messages.join("\n");
}

function workflowDeleteWindowOptions(impact: WorkflowDeleteImpact, title: string) {
  return {
    initialHeight: 300,
    initialWidth: workflowDeleteDialogWidth,
    label: `workflow-delete-${impact.workflowID}`,
    params: workflowDeleteSearchParams(impact),
    route: workflowDeleteNativeDialogPath,
    title,
  };
}

function workflowDeleteSearchParams(impact: WorkflowDeleteImpact): Readonly<Record<string, string>> {
  return {
    active_run_count: impact.activeRunCount.toString(),
    blocked_task_count: impact.blockedTaskCount.toString(),
    default_replacement_project_count: impact.defaultReplacementProjectCount.toString(),
    link_count: impact.linkCount.toString(),
    project_count: impact.projectCount.toString(),
    runnable_run_count: impact.runnableRunCount.toString(),
    task_count: impact.taskCount.toString(),
    version: impact.version.toString(),
    workflow_id: impact.workflowID,
  };
}
