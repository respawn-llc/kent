import { useState } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage } from "../../api/errors";
import { useAppServices } from "../../app/useAppServices";
import { Button, Dialog, NativeDialogWindow } from "../../ui";
import type {
  WorkflowDeleteConfirmationCounts,
  WorkflowDeleteConfirmationWindowTarget,
} from "./workflowDeleteConfirmationModel";

export function WorkflowDeleteConfirmationFallbackDialog({
  counts,
  onCancel,
  onConfirm,
}: Readonly<{
  counts: WorkflowDeleteConfirmationCounts;
  onCancel: () => void;
  onConfirm: () => void;
}>) {
  const { t } = useTranslation();
  return (
    <Dialog
      closeLabel={t("app.close")}
      onClose={onCancel}
      open
      style={{ width: "min(420px, calc(100vw - 32px))" }}
      title={t("workflowEditor.deleteCascadeTitle")}
    >
      <WorkflowDeleteConfirmationContent counts={counts} onCancel={onCancel} onConfirm={onConfirm} />
    </Dialog>
  );
}

export function WorkflowDeleteConfirmationWindowRoute({
  counts,
  requestID,
}: WorkflowDeleteConfirmationWindowTarget) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const [actionError, setActionError] = useState("");
  return (
    <NativeDialogWindow contentMaxWidth="420px" title={t("workflowEditor.deleteCascadeTitle")}>
      <WorkflowDeleteConfirmationContent
        actionError={actionError}
        counts={counts}
        onCancel={() => {
          setActionError("");
          void nativeBridge.window.closeCurrent().catch((error: unknown) => {
            setActionError(errorMessage(error));
          });
        }}
        onConfirm={() => {
          setActionError("");
          void confirmWorkflowGraphDelete(nativeBridge, requestID).catch((error: unknown) => {
            setActionError(errorMessage(error));
          });
        }}
      />
    </NativeDialogWindow>
  );
}

function WorkflowDeleteConfirmationContent({
  actionError,
  counts,
  onCancel,
  onConfirm,
}: Readonly<{
  actionError?: string | undefined;
  counts: WorkflowDeleteConfirmationCounts;
  onCancel: () => void;
  onConfirm: () => void;
}>) {
  const { t } = useTranslation();
  return (
    <div className="grid gap-[var(--space-3)]">
      <p className="m-0 text-sm text-[var(--color-on-island)]">
        {t("workflowEditor.deleteCascadeBody")}
      </p>
      {actionError === undefined || actionError.length === 0 ? null : (
        <p className="m-0 text-sm text-[var(--color-error)]">{actionError}</p>
      )}
      <ul className="m-0 grid gap-[var(--space-1)] p-0 text-sm text-[var(--color-muted)]">
        <li className="list-none">{t("workflowEditor.deleteCascadeNodes", { count: counts.nodeCount })}</li>
        <li className="list-none">{t("workflowEditor.deleteCascadeEdges", { count: counts.edgeCount })}</li>
        <li className="list-none">
          {t("workflowEditor.deleteCascadeTransitionGroups", { count: counts.transitionGroupCount })}
        </li>
      </ul>
      <div className="grid grid-cols-2 gap-[var(--space-2)]">
        <Button className="w-full" onClick={onCancel} variant="secondary">
          {t("app.cancel")}
        </Button>
        <Button className="w-full" onClick={onConfirm} variant="danger">
          {t("workflowEditor.deleteCascadeConfirm")}
        </Button>
      </div>
    </div>
  );
}

async function confirmWorkflowGraphDelete(
  nativeBridge: ReturnType<typeof useAppServices>["nativeBridge"],
  requestID: string,
): Promise<void> {
  await nativeBridge.workflowEditor.confirmGraphDelete({ requestID });
  await nativeBridge.window.closeCurrent();
}
