import { useQueryClient } from "@tanstack/react-query";
import { useMatchRoute } from "@tanstack/react-router";
import { useId, useLayoutEffect, useMemo, useState } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { errorMessage } from "@/api";
import { useAppNavigation, useAppServices, useStatusController } from "@/app-facade";
import { Dialog, useStableCallback } from "@/ui";
import { WorkflowDeleteConfirmationContent } from "./WorkflowDeleteConfirmationContent";
import { workflowDeleteBlockersMessage, workflowDeleteDialogWidth } from "./workflowDeleteShared";
import { createWorkflowDeleteModel } from "./WorkflowDeleteModel";

export function useWorkflowDeleteLauncher(workflowID: string, onDeleted?: () => void) {
  const { t } = useTranslation();
  const { api } = useAppServices();
  const navigation = useAppNavigation();
  const client = useQueryClient();
  const matchRoute = useMatchRoute();
  const { push, dismiss } = useStatusController();
  const noticeID = useId();
  const onCompletionError = useStableCallback((error: unknown) => {
    push({
      id: "workflow-delete-committed-notify-error",
      tone: "warning",
      title: t("workflowEditor.workflowDeleteTitle"),
      body: t("workflowEditor.workflowDeleteCommittedNotifyError", { message: errorMessage(error) }),
    });
  });
  const model = useMemo(
    () => createWorkflowDeleteModel({ api, client, workflowID, onCompletionError }),
    [api, client, workflowID, onCompletionError],
  );
  const [shown, setShown] = useState<typeof model | null>(null);
  useAtomMount(model.preview);
  useAtomMount(model.deletion);
  const preview = useAtomValue(model.preview);
  const deletion = useAtomValue(model.deletion);
  const open = useAtomSet(model.open, { mode: "value" });
  const confirm = useAtomSet(model.confirm, { mode: "value" });
  const reset = useAtomSet(model.cancel, { mode: "value" });
  useLayoutEffect(
    () => () => {
      dismiss(noticeID);
    },
    [dismiss, model, noticeID],
  );
  const cancel = () => {
    if (deletion.isPending) return;
    setShown(null);
    reset(undefined);
  };
  const complete = async () => {
    onDeleted?.();
    const routeMatches =
      matchRoute({
        to: "/workflows/$workflowId/editor",
        params: { workflowId: workflowID },
        pending: false,
        fuzzy: false,
        includeSearch: false,
      }) !== false ||
      matchRoute({
        to: "/projects/$projectId",
        search: { workflowId: workflowID },
        pending: false,
        fuzzy: false,
        includeSearch: true,
      }) !== false;
    if (routeMatches && (await navigation.openWorkflowLibrary()) === "failed") {
      throw new Error(t("workflowEditor.workflowDeleteNavigationError"));
    }
    push({ id: "workflow-delete-deleted", tone: "success", title: t("workflowEditor.workflowDeleted") });
  };
  const committed = deletion.data?.deleted === true;
  const actionError = deletion.isError
    ? errorMessage(deletion.error)
    : deletion.data !== undefined && !deletion.data.deleted
      ? workflowDeleteBlockersMessage(deletion.data.blockers, t("workflowEditor.workflowDeleteBlocked"))
      : undefined;
  return {
    disabled: committed,
    opening: preview.isPending,
    submitting: deletion.isPending,
    openWorkflowDelete: () => {
      open({
        onOpening: () => {
          setShown(model);
        },
        onError(error, retry) {
          push({
            id: noticeID,
            tone: "danger",
            title: t("workflowEditor.workflowDeleteTitle"),
            body: errorMessage(error),
            actionLabel: t("app.retry"),
            onAction: retry,
          });
        },
      });
    },
    dialog:
      shown !== model || preview.data === undefined || committed
        ? null
        : createPortal(
            <Dialog
              closeLabel={t("app.close")}
              closeDisabled={deletion.isPending}
              onClose={cancel}
              open
              title={t("workflowEditor.workflowDeleteTitle")}
              width={workflowDeleteDialogWidth}
            >
              <WorkflowDeleteConfirmationContent
                actionError={actionError}
                disabled={deletion.isPending}
                impact={preview.data}
                onCancel={cancel}
                onConfirm={() => {
                  confirm({
                    onCommitted: () => {
                      setShown((current) => (current === model ? null : current));
                    },
                    onCompleted: complete,
                  });
                }}
              />
            </Dialog>,
            document.body,
          ),
  } as const;
}
