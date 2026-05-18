import { useCallback } from "react";

import { useAppNavigation } from "../../app/navigation";
import { useAppServices } from "../../app/useAppServices";

export function useOpenTaskDetail() {
  const navigation = useAppNavigation();
  const { nativeBridge } = useAppServices();

  return useCallback(
    (taskId: string, resumeRunId = "", fallback?: () => void) => {
      const openFallback =
        fallback ??
        (() => {
          void navigation.openTask(taskId);
        });
      if (!nativeBridge.capabilities.taskDetailWindow) {
        openFallback();
        return;
      }
      void nativeBridge.taskDetail.openWindow({ resumeRunId, taskId }).catch(() => {
        openFallback();
      });
    },
    [nativeBridge.capabilities.taskDetailWindow, nativeBridge.taskDetail, navigation],
  );
}
