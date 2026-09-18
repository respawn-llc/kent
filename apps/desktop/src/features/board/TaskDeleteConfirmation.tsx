import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";

import { errorMessage } from "@/api";
import { invalidateAllTaskSearches } from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { useStatusController } from "@/app-facade";
import { NativeDialogWindow } from "@/shared/native-dialog";
import { TaskDeleteConfirmationContent, taskDeleteDialogWidth } from "@/shared/task-delete";
import type { TaskDeleteTarget } from "./taskDeleteConfirmationModel";
import { useBoardTaskDeletion, useBoardTaskDeletions } from "./BoardTaskDeletion";

export function TaskDeleteWindowRoute({ taskID }: TaskDeleteTarget) {
  const { t } = useTranslation();
  const { nativeBridge } = useAppServices();
  const queryClient = useQueryClient();
  const { push } = useStatusController();
  const deletions = useBoardTaskDeletions();
  const deletion = useBoardTaskDeletion(deletions, taskID);

  function confirmDelete(): void {
    deletion.submit({
      onDeleted: async () => {
        await invalidateAllTaskSearches(queryClient);
        try {
          await nativeBridge.window.closeCurrent();
        } catch (error) {
          push({
            id: "task-delete-window-close-error",
            tone: "danger",
            title: t("board.deleteTaskWindowCloseError"),
            body: errorMessage(error),
          });
        }
      },
      onError: (error) => {
        push({
          id: "task-delete-window-error",
          tone: "danger",
          title: t("board.deleteTaskWindowError"),
          body: errorMessage(error),
        });
      },
    });
  }

  return (
    <NativeDialogWindow
      contentMaxWidth={`${taskDeleteDialogWidth.toString()}px`}
      title={t("board.deleteTaskTitle")}
    >
      <TaskDeleteConfirmationContent
        actionError={deletion.error === null ? null : errorMessage(deletion.error)}
        disabled={deletion.isPending || deletion.isSuccess}
        onCancel={() => {
          void nativeBridge.window.closeCurrent();
        }}
        onConfirm={confirmDelete}
      />
    </NativeDialogWindow>
  );
}
