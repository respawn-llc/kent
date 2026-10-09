import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { useAtomSet } from "@effect/atom-react";
import { useAppServices, useStatusController } from "@/app-facade";
import { desktopChatEnabled } from "@/shared/feature-flags";
import { TaskDetailSurface } from "./TaskDetailSurface";
import { useExactTaskDetailDeleteDismissal } from "./taskDetailDismissal";
import { createTaskDetailChatOpening, TaskDetailChatOpeningContext } from "./TaskDetailChatOpening";

/**
 * Full-bleed native-window host for a popped-out task detail. Unlike the padded
 * dialog shell used by forms, the surface ({@link TaskDetailSurface} via
 * `TaskDetailList`) already owns its own padding and scrolling, so this shell adds
 * only the glass fill, a top drag strip, and the titlebar inset that keeps the
 * macOS traffic lights off the content — exactly how the in-app sidebar hosts it.
 */
export function TaskDetailWindowRoute({ taskID }: Readonly<{ taskID: string }>) {
  const services = useAppServices();
  const { nativeBridge } = services;
  const client = useQueryClient();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const model = useMemo(
    () => createTaskDetailChatOpening({ client, services, push, t }),
    [client, services, push, t],
  );
  const open = useAtomSet(model.open);
  const openSessionChat =
    desktopChatEnabled && nativeBridge.capabilities.dialogWindows
      ? async (target: Parameters<typeof open>[0]) => {
          open(target);
        }
      : undefined;
  const onDeleteDismiss = useExactTaskDetailDeleteDismissal(taskID, async () => {
    await nativeBridge.window.closeCurrent();
  });
  return (
    <main className="window-glass-fill grid h-screen w-screen grid-rows-[minmax(0,1fr)] overflow-hidden pt-[var(--native-titlebar-height)]">
      <div
        className="app-region-drag fixed inset-x-0 top-0 h-[var(--native-titlebar-height)]"
        data-tauri-drag-region
      />
      <div className="app-region-no-drag min-h-0 overflow-hidden">
        <TaskDetailChatOpeningContext.Provider value={model}>
          <TaskDetailSurface
            enabled
            onDeleteDismiss={onDeleteDismiss}
            taskId={taskID}
            openSessionChat={openSessionChat}
          />
        </TaskDetailChatOpeningContext.Provider>
      </div>
    </main>
  );
}
